package effects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

func TestPlanningPackageAdmitApprovePromoteUsesExactWorkEvidence(t *testing.T) {
	// control-law: planning-package promotion requires exact work and approval lineage
	repository := t.TempDir()
	layout := ports.ControllerLayout{RepositoryRoot: repository}
	objective := model.Objective{ID: "objective", TargetID: model.ObjectiveOpenPR, DeliveryID: "delivery"}
	now := time.Unix(100, 0).UTC()
	plan := "# Approved implementation plan\n"
	feature := "# Feature specification\n"
	work := &protocol.WorkEvidence{
		RequestFingerprint: strings.Repeat("a", 64), ResultFingerprint: strings.Repeat("b", 64),
		Outputs: []protocol.WorkOutputEvidence{
			{ID: "plan", Path: "plan.md", MediaType: "text/markdown", SHA256: sha256Bytes([]byte(plan)), Size: int64(len(plan)), Content: plan},
			{ID: "feature-spec", Path: "feature-spec.md", MediaType: "text/markdown", SHA256: sha256Bytes([]byte(feature)), Size: int64(len(feature)), Content: feature},
		},
	}
	state := durable.State{Plan: model.PlanAbsent, Delivery: model.DeliveryUninitialized, Phase: model.PhaseObserved, Terminal: model.TerminalNonterminal}
	admit := catalog.Transition{ID: "planning.package.admit", TargetPhases: []model.ProtocolPhase{model.PhaseActive}, StateEffect: catalog.StateEffect{Kind: catalog.StateEffectNative, NativeHandler: "planning-package-admit"}}
	admission := protocol.Admission{ID: "adm-admit", Objective: objective, Work: work, IssuedAt: now}
	if err := applyStateTransition(&state, admission, admit); err != nil {
		t.Fatal(err)
	}
	mutations, err := prepareArtifacts(layout, admission, admit, &state)
	if err != nil {
		t.Fatal(err)
	}
	installFixtureMutations(t, mutations)
	manifest, _, err := loadPlanningPackageManifest(filepath.Join(repository, ".boatstack"), "delivery")
	if err != nil {
		t.Fatal(err)
	}
	if state.Plan != model.PlanPackageValid || state.PlanFingerprint != sha256Bytes([]byte(plan)) || manifest.WorkResultFingerprint != work.ResultFingerprint {
		t.Fatalf("admitted state=%#v manifest=%#v", state, manifest)
	}

	approve := catalog.Transition{ID: "planning.package.approve", TargetPhases: []model.ProtocolPhase{model.PhaseActive}, StateEffect: catalog.StateEffect{Kind: catalog.StateEffectNative, NativeHandler: "planning-package-approve"}}
	admission = protocol.Admission{
		ID: "adm-approve", Objective: objective, IssuedAt: now.Add(time.Minute), Parameters: protocol.Parameters{{Name: "package_fingerprint", Value: manifest.Fingerprint}},
		Authority: protocol.AuthorityBundle{Receipts: []protocol.AuthorityReceipt{{ID: "auth", Class: catalog.AuthorityHuman, Subject: "reviewer"}}},
	}
	featurePath := filepath.Join(repository, ".boatstack", "planning-packages", "delivery", "feature-spec.md")
	if err := os.WriteFile(featurePath, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareArtifacts(layout, admission, approve, &state); err == nil || !strings.Contains(err.Error(), `output "feature-spec" changed after admission`) {
		t.Fatalf("changed non-plan output approval result = %v", err)
	}
	if err := os.WriteFile(featurePath, []byte(feature), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := applyStateTransition(&state, admission, approve); err != nil {
		t.Fatal(err)
	}
	mutations, err = prepareArtifacts(layout, admission, approve, &state)
	if err != nil {
		t.Fatal(err)
	}
	installFixtureMutations(t, mutations)
	if state.Plan != model.PlanPackageApproved || state.ApprovalFingerprint == "" {
		t.Fatalf("approved package state=%#v", state)
	}

	promote := catalog.Transition{ID: "planning.package.promote", TargetPhases: []model.ProtocolPhase{model.PhaseActive}, StateEffect: catalog.StateEffect{Kind: catalog.StateEffectNative, NativeHandler: "planning-package-promote"}}
	admission = protocol.Admission{ID: "adm-promote", Objective: objective, IssuedAt: now.Add(2 * time.Minute)}
	if err := applyStateTransition(&state, admission, promote); err != nil {
		t.Fatal(err)
	}
	mutations, err = prepareArtifacts(layout, admission, promote, &state)
	if err != nil {
		t.Fatal(err)
	}
	installFixtureMutations(t, mutations)
	canonicalPlan, err := os.ReadFile(filepath.Join(repository, ".boatstack", "plans", "delivery.source"))
	if err != nil {
		t.Fatal(err)
	}
	if state.Plan != model.PlanApproved || string(canonicalPlan) != plan {
		t.Fatalf("promoted state=%#v plan=%q", state, canonicalPlan)
	}
}

func TestPlanningPackageApprovalRejectsFingerprintDrift(t *testing.T) {
	// control-law: approval cannot cross planning-package manifest drift
	repository := t.TempDir()
	manifestRoot := filepath.Join(repository, ".boatstack", "planning-packages", "delivery")
	if err := os.MkdirAll(manifestRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	plan := []byte("p")
	planFingerprint := sha256Bytes(plan)
	manifest := planningPackageManifest{SchemaVersion: 1, DeliveryID: "delivery", WorkRequestFingerprint: strings.Repeat("a", 64), WorkResultFingerprint: strings.Repeat("b", 64), PlanFingerprint: planFingerprint, Outputs: []planningPackageOutput{{ID: "plan", Path: "plan.md", MediaType: "text/markdown", SHA256: planFingerprint, Size: int64(len(plan))}}}
	identityRaw, _ := encodeJSON(manifest)
	manifest.Fingerprint = sha256Bytes(identityRaw)
	raw, _ := encodeJSON(manifest)
	if err := os.WriteFile(filepath.Join(manifestRoot, "manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manifestRoot, "plan.md"), plan, 0o600); err != nil {
		t.Fatal(err)
	}
	state := durable.State{Plan: model.PlanPackageApproved}
	_, err := prepareArtifacts(ports.ControllerLayout{RepositoryRoot: repository}, protocol.Admission{
		ID: "adm", Objective: model.Objective{DeliveryID: "delivery"}, Parameters: protocol.Parameters{{Name: "package_fingerprint", Value: strings.Repeat("d", 64)}},
	}, catalog.Transition{ID: "planning.package.approve"}, &state)
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("fingerprint drift result = %v", err)
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
		if err := os.WriteFile(mutation.Path, mutation.Target, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
