package effects

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	planningpackage "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery/planningpackage"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

func TestPlanningPackageAdmitApprovePromoteUsesExactV2Snapshot(t *testing.T) {
	// control-law: planning-package promotion requires exact work and approval lineage
	repository := t.TempDir()
	layout := ports.ControllerLayout{RepositoryRoot: repository}
	now := time.Unix(100, 0).UTC()
	plan := []byte("# implementation plan\n")
	workContract := &catalog.WorkContract{
		ID: "planning", InstructionPath: "package.md",
		InstructionSHA256: sha256Bytes([]byte("coordinate")), InstructionContent: "coordinate",
		Outputs: []catalog.WorkOutput{{ID: "implementation-plan", Path: "plan.md", MediaType: "text/markdown", Required: true, MaxBytes: 1024}},
	}
	portableWork := planningpackage.WorkContract{ID: workContract.ID, Instructions: planningpackage.Asset{Path: workContract.InstructionPath, SHA256: workContract.InstructionSHA256, Content: workContract.InstructionContent}, Outputs: []planningpackage.WorkOutput{{ID: "implementation-plan", Path: "plan.md", MediaType: "text/markdown", Required: true, MaxBytes: 1024}}}
	contractFingerprint, err := planningpackage.RuntimeWorkFingerprint(portableWork)
	if err != nil {
		t.Fatal(err)
	}
	workContract.Fingerprint = contractFingerprint
	work := &protocol.WorkEvidence{
		RequestID: "work-request", RequestFingerprint: strings.Repeat("a", 64), ResultFingerprint: strings.Repeat("b", 64),
		RunID: "run-proof", ProgramID: "product-delivery", EntryID: "run", ContractID: "planning", ContractFingerprint: contractFingerprint,
		TransitionID: "planning.package.admit", ProgramFingerprint: strings.Repeat("d", 64), ContextFingerprint: strings.Repeat("e", 64), StateRevision: 3,
		RepositoryID: "repo", WorktreeID: "tree", Outputs: []protocol.WorkOutputEvidence{{ID: "implementation-plan", Path: "plan.md", MediaType: "text/markdown", SHA256: sha256Bytes(plan), Size: int64(len(plan)), Content: string(plan)}},
	}
	objective := model.Objective{ID: "objective", TargetID: model.ObjectiveOpenPR, DeliveryID: "delivery"}
	state := durable.State{Plan: model.PlanAbsent, Delivery: model.DeliveryUninitialized, Phase: model.PhaseObserved, Terminal: model.TerminalNonterminal}
	admit := catalog.Transition{ID: "planning.package.admit", Work: workContract, TargetPhases: []model.ProtocolPhase{model.PhaseActive}, StateEffect: catalog.StateEffect{Kind: catalog.StateEffectNative, NativeHandler: "planning-package-admit"}}
	admission := protocol.Admission{ID: "adm-admit", Objective: objective, Work: work, IssuedAt: now, Parameters: protocol.Parameters{{Name: "plan_output", Value: "implementation-plan"}}}
	if err := applyStateTransition(&state, admission, admit); err != nil {
		t.Fatal(err)
	}
	mutations, err := prepareArtifacts(layout, admission, admit, &state)
	if err != nil {
		t.Fatal(err)
	}
	installFixtureMutations(t, mutations)
	result := planningpackage.Verify(repository, "delivery", state.PlanningPackageFingerprint, nil)
	if result.Integrity != planningpackage.Valid || result.Contract != planningpackage.Valid || result.Approval != planningpackage.Missing {
		t.Fatalf("verification=%#v", result)
	}
	packageRoot := filepath.Join(repository, ".boatstack", "planning-packages", "delivery", state.PlanningPackageFingerprint)
	priorManifest, _ := os.ReadFile(filepath.Join(packageRoot, "manifest.json"))
	mutations, err = prepareArtifacts(layout, admission, admit, &state)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range mutations {
		if mutation.PriorExists && !bytes.Equal(mutation.Prior, mutation.Target) {
			t.Fatal("replay changed immutable bytes")
		}
	}

	approve := catalog.Transition{ID: "planning.package.approve", TargetPhases: []model.ProtocolPhase{model.PhaseActive}, StateEffect: catalog.StateEffect{Kind: catalog.StateEffectNative, NativeHandler: "planning-package-approve"}}
	providerFingerprint := strings.Repeat("9", 64)
	approvalAdmission := protocol.Admission{ID: "adm-approve", Objective: objective, IssuedAt: now.Add(time.Minute), Parameters: protocol.Parameters{{Name: "package_fingerprint", Value: state.PlanningPackageFingerprint}}, Authority: protocol.AuthorityBundle{Receipts: []protocol.AuthorityReceipt{
		{ID: "policy", Class: catalog.AuthorityRepository, Subject: "repository", Fingerprint: "policy-proof", IssuedAt: now},
		{ID: "automation", Class: catalog.AuthorityAutonomy, Subject: "automation", Fingerprint: "autonomy-proof", IdentityRole: "developer", IdentityProviderFingerprint: strings.Repeat("8", 64), IssuedAt: now},
		{ID: "auth", Class: catalog.AuthorityHuman, Subject: "reviewer", Fingerprint: "human-proof", IdentityRole: "developer", IdentityProviderFingerprint: providerFingerprint, IssuedAt: now},
	}}}
	if err := applyStateTransition(&state, approvalAdmission, approve); err != nil {
		t.Fatal(err)
	}
	wrongRole := approvalAdmission
	wrongRole.Authority.Receipts = append([]protocol.AuthorityReceipt(nil), approvalAdmission.Authority.Receipts...)
	wrongRole.Authority.Receipts[2].IdentityRole = "release-manager"
	if _, mismatchErr := prepareArtifacts(layout, wrongRole, approve, &state, "developer"); mismatchErr == nil || !strings.Contains(mismatchErr.Error(), "does not match admitted program role") {
		t.Fatalf("mismatched approval role was not rejected: %v", mismatchErr)
	}
	mutations, err = prepareArtifacts(layout, approvalAdmission, approve, &state, "developer")
	if err != nil {
		t.Fatal(err)
	}
	installFixtureMutations(t, mutations)
	if state.ApprovalFingerprint != sha256Bytes(approvalRawAt(t, packageRoot)) {
		t.Fatal("durable approval fingerprint is not the canonical approval file digest")
	}
	result = planningpackage.Verify(repository, "delivery", state.PlanningPackageFingerprint, nil)
	if result.Approval != planningpackage.Valid {
		t.Fatalf("approval verification=%#v", result)
	}
	var recordedApproval planningpackage.Approval
	if err := planningpackage.StrictDecode(approvalRawAt(t, packageRoot), &recordedApproval); err != nil || recordedApproval.Actor != "reviewer" || recordedApproval.IdentityRole != "developer" || recordedApproval.IdentityProviderFingerprint != providerFingerprint {
		t.Fatalf("approval identity provenance=%#v err=%v", recordedApproval, err)
	}

	promote := catalog.Transition{ID: "planning.package.promote", TargetPhases: []model.ProtocolPhase{model.PhaseActive}, StateEffect: catalog.StateEffect{Kind: catalog.StateEffectNative, NativeHandler: "planning-package-promote"}}
	promotion := protocol.Admission{ID: "adm-promote", Objective: objective, IssuedAt: now.Add(2 * time.Minute)}
	if err := applyStateTransition(&state, promotion, promote); err != nil {
		t.Fatal(err)
	}
	originalVerify := verifyPlanningPackage
	swappedRoot := packageRoot + ".swapped"
	replacementPerformed := false
	var replacementBlocked error
	verifyPlanningPackage = func(repository, deliveryID, packageFingerprint string, current *planningpackage.CurrentProgram) planningpackage.Result {
		result := originalVerify(repository, deliveryID, packageFingerprint, current)
		if err := os.Rename(packageRoot, swappedRoot); err != nil {
			replacementBlocked = err
			return result
		}
		replacementPerformed = true
		if err := os.Mkdir(packageRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		return result
	}
	_, raceErr := prepareArtifacts(layout, promotion, promote, &state)
	if replacementPerformed {
		if raceErr == nil || !strings.Contains(raceErr.Error(), "changed during verification") {
			t.Fatalf("package replacement was not rejected: %v", raceErr)
		}
	} else if replacementBlocked == nil || raceErr != nil {
		t.Fatalf("host neither blocked replacement nor completed safe promotion: blocked=%v promotion=%v", replacementBlocked, raceErr)
	}
	verifyPlanningPackage = originalVerify
	if replacementPerformed {
		if err := os.Remove(packageRoot); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(swappedRoot, packageRoot); err != nil {
			t.Fatal(err)
		}
	}
	originalManifest, err := os.ReadFile(filepath.Join(packageRoot, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	verifyPlanningPackage = func(repository, deliveryID, packageFingerprint string, current *planningpackage.CurrentProgram) planningpackage.Result {
		result := originalVerify(repository, deliveryID, packageFingerprint, current)
		if err := os.WriteFile(filepath.Join(packageRoot, "manifest.json"), append(append([]byte(nil), originalManifest...), '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return result
	}
	if _, mutationErr := prepareArtifacts(layout, promotion, promote, &state); mutationErr == nil || !strings.Contains(mutationErr.Error(), "manifest") {
		t.Fatalf("in-place package mutation was not rejected: %v", mutationErr)
	}
	verifyPlanningPackage = originalVerify
	if err := os.WriteFile(filepath.Join(packageRoot, "manifest.json"), originalManifest, 0o644); err != nil {
		t.Fatal(err)
	}
	originalApproval := approvalRawAt(t, packageRoot)
	var substitutedApproval planningpackage.Approval
	if err := planningpackage.StrictDecode(originalApproval, &substitutedApproval); err != nil {
		t.Fatal(err)
	}
	substitutedApproval.Actor = "substitute"
	for index := range substitutedApproval.AuthoritySources {
		substitutedApproval.AuthoritySources[index].Subject = substitutedApproval.Actor
	}
	_, substitutedApprovalRaw, err := planningpackage.SealApproval(substitutedApproval)
	if err != nil {
		t.Fatal(err)
	}
	verifyPlanningPackage = func(repository, deliveryID, packageFingerprint string, current *planningpackage.CurrentProgram) planningpackage.Result {
		result := originalVerify(repository, deliveryID, packageFingerprint, current)
		if err := os.WriteFile(filepath.Join(packageRoot, "approval.json"), substitutedApprovalRaw, 0o644); err != nil {
			t.Fatal(err)
		}
		return result
	}
	if _, substitutionErr := prepareArtifacts(layout, promotion, promote, &state); substitutionErr == nil || !strings.Contains(substitutionErr.Error(), "approval") {
		t.Fatalf("substituted approval fingerprint was not rejected: %v", substitutionErr)
	}
	verifyPlanningPackage = originalVerify
	if err := os.WriteFile(filepath.Join(packageRoot, "approval.json"), originalApproval, 0o644); err != nil {
		t.Fatal(err)
	}
	originalContract, err := os.ReadFile(filepath.Join(packageRoot, "contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	verifyPlanningPackage = func(repository, deliveryID, packageFingerprint string, current *planningpackage.CurrentProgram) planningpackage.Result {
		result := originalVerify(repository, deliveryID, packageFingerprint, current)
		if err := os.WriteFile(filepath.Join(packageRoot, "contract.json"), append(append([]byte(nil), originalContract...), '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return result
	}
	if _, contractErr := prepareArtifacts(layout, promotion, promote, &state); contractErr == nil || !strings.Contains(contractErr.Error(), "verification failed") {
		t.Fatalf("post-verification contract mutation was not rejected: %v", contractErr)
	}
	verifyPlanningPackage = originalVerify
	if err := os.WriteFile(filepath.Join(packageRoot, "contract.json"), originalContract, 0o644); err != nil {
		t.Fatal(err)
	}
	mutations, err = prepareArtifacts(layout, promotion, promote, &state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = prepareArtifacts(layout, promotion, promote, &state); err != nil {
		t.Fatalf("repeated promotion preparation rejected unchanged package: %v", err)
	}
	installFixtureMutations(t, mutations)
	current, _ := os.ReadFile(filepath.Join(repository, ".boatstack", "plans", "delivery.source"))
	if !bytes.Equal(current, plan) {
		t.Fatalf("promoted plan=%q", current)
	}
	workspace := t.TempDir()
	transfers, err := prepareWorkspacePlanTransfer(repository, workspace, "delivery", state.PlanFingerprint, state.ApprovalFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	installFixtureMutations(t, transfers)
	transferredApproval, err := os.ReadFile(filepath.Join(workspace, ".boatstack", "approvals", "delivery.json"))
	if err != nil || !bytes.Equal(transferredApproval, approvalRawAt(t, packageRoot)) {
		t.Fatalf("workspace did not receive exact schema-2 approval: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(packageRoot, "manifest.json"))
	if !bytes.Equal(after, priorManifest) {
		t.Fatal("immutable manifest changed")
	}
}

func approvalRawAt(t *testing.T, packageRoot string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(packageRoot, "approval.json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestPlanningPackageImmutableConflictFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "member")
	if err := os.WriteFile(path, []byte("prior"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := immutablePlanningMutation(path, []byte("changed")); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("conflict=%v", err)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "prior" {
		t.Fatal("conflict changed prior state")
	}
}

func installFixtureMutations(t *testing.T, mutations []ports.ResourceMutation) {
	t.Helper()
	for _, mutation := range mutations {
		if mutation.Delete {
			_ = os.Remove(mutation.Path)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(mutation.Path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(mutation.Path, mutation.Target, os.FileMode(mutation.Mode)); err != nil {
			t.Fatal(err)
		}
	}
}
