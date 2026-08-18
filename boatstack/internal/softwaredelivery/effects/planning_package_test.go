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
	approvalAdmission := protocol.Admission{ID: "adm-approve", Objective: objective, IssuedAt: now.Add(time.Minute), Parameters: protocol.Parameters{{Name: "package_fingerprint", Value: state.PlanningPackageFingerprint}}, Authority: protocol.AuthorityBundle{Receipts: []protocol.AuthorityReceipt{{ID: "auth", Class: catalog.AuthorityHuman, Subject: "reviewer", Fingerprint: strings.Repeat("f", 64), IssuedAt: now}}}}
	if err := applyStateTransition(&state, approvalAdmission, approve); err != nil {
		t.Fatal(err)
	}
	mutations, err = prepareArtifacts(layout, approvalAdmission, approve, &state)
	if err != nil {
		t.Fatal(err)
	}
	installFixtureMutations(t, mutations)
	result = planningpackage.Verify(repository, "delivery", state.PlanningPackageFingerprint, nil)
	if result.Approval != planningpackage.Valid {
		t.Fatalf("approval verification=%#v", result)
	}

	promote := catalog.Transition{ID: "planning.package.promote", TargetPhases: []model.ProtocolPhase{model.PhaseActive}, StateEffect: catalog.StateEffect{Kind: catalog.StateEffectNative, NativeHandler: "planning-package-promote"}}
	promotion := protocol.Admission{ID: "adm-promote", Objective: objective, IssuedAt: now.Add(2 * time.Minute)}
	if err := applyStateTransition(&state, promotion, promote); err != nil {
		t.Fatal(err)
	}
	mutations, err = prepareArtifacts(layout, promotion, promote, &state)
	if err != nil {
		t.Fatal(err)
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
