package boatstack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

func amendmentPlanDocument(t *testing.T, feature, sourcePlan string) []byte {
	t.Helper()
	plan := twoSlicePlan()
	plan["feature_id"] = feature
	plan["source_plan_path"] = sourcePlan
	plan["spec_path"] = "feature-spec.md"
	plan["acceptance_criteria"].([]any)[0].(map[string]any)["text"] = "amended observable result"
	value, err := MarshalJSON(plan)
	if err != nil {
		t.Fatal(err)
	}
	return []byte("# Amended structured plan\n\n" + planMarkerStart + "\n```json\n" + strings.TrimSpace(string(value)) + "\n```\n" + planMarkerEnd + "\n")
}

func allowLifecyclePlanningHealth(t *testing.T) {
	t.Helper()
	previousBootstrap := bootstrapInstallationHealth
	previousPlanning := planningInstallationHealth
	bootstrapInstallationHealth = func(string) error { return nil }
	planningInstallationHealth = func(string) error { return nil }
	t.Cleanup(func() {
		bootstrapInstallationHealth = previousBootstrap
		planningInstallationHealth = previousPlanning
	})
}

func TestLifecycleAuthorityMakesRequirementAmendmentReachable(t *testing.T) {
	allowLifecyclePlanningHealth(t)
	repo, feature := activateTwoSliceDelivery(t)
	observation, _, err := RecordChangeObservation(ChangeObservationOptions{
		Repo: repo, Feature: feature, Message: "acceptance criteria changed",
		SourceStage: "build", Expected: "original result", Actual: "amended result",
		Classification: "requirement_amendment",
	})
	if err != nil {
		t.Fatal(err)
	}

	required, err := ResolveLifecycleSnapshot(repo, feature)
	if err != nil {
		t.Fatal(err)
	}
	if required.State != deliverycontrol.StateAmendmentRequired || required.ObservationID != observation.ID {
		t.Fatalf("requirement amendment projected as %+v", required)
	}
	next, err := nextForDelivery(repo, feature)
	if err != nil {
		t.Fatal(err)
	}
	if next.ObservedStage != string(deliverycontrol.StateAmendmentRequired) || next.NextOperation != "amend-plan" {
		t.Fatalf("requirement amendment has no owned planning transition: %+v", next)
	}

	if err := os.MkdirAll(filepath.Join(repo, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	sourcePlan := "docs/amendment-source.md"
	if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(sourcePlan)), []byte("# Accepted amendment\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	document := amendmentPlanDocument(t, feature, sourcePlan)
	prescription, err := ResolvePlanningBootstrap(BootstrapOptions{
		Repo: repo, Feature: feature, SourcePlan: sourcePlan, Artifact: "plan.md",
		Shell: BootstrapShellPOSIX, Document: document,
	})
	if err != nil {
		t.Fatal(err)
	}
	if prescription.Disposition != "AMEND_ACTIVE" || prescription.LifecycleSHA256 != required.Fingerprint || prescription.PreviousPlanLock != required.PlanLockSHA256 {
		t.Fatalf("amendment bootstrap was not bound to current lifecycle authority: %+v", prescription)
	}
	for _, host := range []string{"cursor", "claude", "codex", "gemini"} {
		if output, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: planningHookInput(t, host, prescription.PlanningEnvelope)}); denied {
			t.Fatalf("%s denied the canonical amendment planning envelope: %s", host, output)
		}
	}
	if _, err := WritePlanningArtifact(PlanningWriteOptions{
		Repo: repo, Feature: feature, Artifact: "plan.md", Content: document,
		SourcePlan: sourcePlan, SourcePlanSHA256: prescription.SourcePlanSHA256,
		ExpectedLifecycleSHA256: prescription.LifecycleSHA256,
		ExpectedPlanLockSHA256:  prescription.PreviousPlanLock,
		ExpectedObservation:     prescription.ObservationID,
	}); err != nil {
		t.Fatal(err)
	}

	drafted, err := ResolveLifecycleSnapshot(repo, feature)
	if err != nil {
		t.Fatal(err)
	}
	if drafted.State != deliverycontrol.StateAmendmentDrafted {
		t.Fatalf("amended plan did not enter approval state: %+v", drafted)
	}
	planPath := filepath.Join(repo, ".product-loop", "features", feature, "plan.md")
	check, err := CheckPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := PlanningBaselineForPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := RecordApproval(ApprovalRecordOptions{
		Repo:     repo,
		PlanPath: planPath, ApprovedBy: "Test Human", ApprovedAt: "2026-08-10T12:00:00Z",
		Fingerprint: check.Fingerprint, BaselineDiffSHA256: baseline.DiffSHA256,
		ExpectedLifecycleSHA256: drafted.Fingerprint, ExpectedPlanLockSHA256: drafted.PlanLockSHA256,
		ExpectedObservation: drafted.ObservationID,
	}); err != nil {
		t.Fatal(err)
	}

	approved, err := ResolveLifecycleSnapshot(repo, feature)
	if err != nil {
		t.Fatal(err)
	}
	if approved.State != deliverycontrol.StateAmendmentApproved || !approved.ApprovalCurrent {
		t.Fatalf("approved amendment was not projected as activatable: %+v", approved)
	}
	directory := filepath.Dir(planPath)
	stateWithChangedObservation, err := LoadDeliveryState(repo, feature)
	if err != nil {
		t.Fatal(err)
	}
	stateWithChangedObservation.ActiveObservationID = "CHG-999"
	if err := saveDeliveryState(repo, stateWithChangedObservation); err != nil {
		t.Fatal(err)
	}
	if err := ActivatePlan(ActivationOptions{
		Repo:     repo,
		PlanPath: planPath, ApprovalPath: filepath.Join(directory, "approval.md"),
		OutDir: filepath.Join(directory, "compiled"), OutputPath: filepath.Join(directory, "plan.lock.json"),
		SourceCommit: runGit(t, repo, "rev-parse", "HEAD"),
	}); err == nil || !strings.Contains(err.Error(), "not currently approved") {
		t.Fatalf("activation accepted approval for a different amendment observation: %v", err)
	}
	stateWithChangedObservation.ActiveObservationID = observation.ID
	if err := saveDeliveryState(repo, stateWithChangedObservation); err != nil {
		t.Fatal(err)
	}
	if err := ActivatePlan(ActivationOptions{
		Repo:     repo,
		PlanPath: planPath, ApprovalPath: filepath.Join(directory, "approval.md"),
		OutDir: filepath.Join(directory, "compiled"), OutputPath: filepath.Join(directory, "plan.lock.json"),
		SourceCommit: runGit(t, repo, "rev-parse", "HEAD"),
	}); err != nil {
		t.Fatal(err)
	}

	active, err := ResolveLifecycleSnapshot(repo, feature)
	if err != nil {
		t.Fatal(err)
	}
	if active.State != deliverycontrol.StateBuild || active.Mode != "NORMAL" || active.ObservationID != "" {
		t.Fatalf("activation did not restore ordinary delivery: %+v", active)
	}
	state, err := CurrentDeliveryState(repo, feature)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.PreviousPlanLocks) != 1 || state.PreviousPlanLocks[0] != required.PlanLockSHA256 {
		t.Fatalf("reactivation lost prior plan authority: %+v", state.PreviousPlanLocks)
	}
}

func TestAmendmentPlanningPrescriptionRejectsLifecycleDrift(t *testing.T) {
	allowLifecyclePlanningHealth(t)
	repo, feature := activateTwoSliceDelivery(t)
	if _, _, err := RecordChangeObservation(ChangeObservationOptions{
		Repo: repo, Feature: feature, Message: "requirements changed", SourceStage: "build",
		Classification: "requirement_amendment",
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	sourcePlan := "docs/amendment-source.md"
	if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(sourcePlan)), []byte("# Accepted amendment\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	document := amendmentPlanDocument(t, feature, sourcePlan)
	prescription, err := ResolvePlanningBootstrap(BootstrapOptions{
		Repo: repo, Feature: feature, SourcePlan: sourcePlan, Artifact: "plan.md",
		Shell: BootstrapShellPOSIX, Document: document,
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := LoadDeliveryState(repo, feature)
	if err != nil {
		t.Fatal(err)
	}
	state.ActiveObservationID = "CHG-999"
	if err := saveDeliveryState(repo, state); err != nil {
		t.Fatal(err)
	}
	_, err = WritePlanningArtifact(PlanningWriteOptions{
		Repo: repo, Feature: feature, Artifact: "plan.md", Content: document,
		SourcePlan: sourcePlan, SourcePlanSHA256: prescription.SourcePlanSHA256,
		ExpectedLifecycleSHA256: prescription.LifecycleSHA256,
		ExpectedPlanLockSHA256:  prescription.PreviousPlanLock,
		ExpectedObservation:     prescription.ObservationID,
	})
	if err == nil || !strings.Contains(err.Error(), "lifecycle changed") {
		t.Fatalf("stale lifecycle prescription was not rejected: %v", err)
	}
}

func TestInvalidActivePlanUsesTheSameOwnedRewritePath(t *testing.T) {
	allowLifecyclePlanningHealth(t)
	repo, feature := activateTwoSliceDelivery(t)
	observation, _, err := RecordChangeObservation(ChangeObservationOptions{
		Repo: repo, Feature: feature, Message: "active plan is structurally invalid",
		SourceStage: "build", Classification: "plan_invalid",
	})
	if err != nil {
		t.Fatal(err)
	}
	invalid, err := ResolveLifecycleSnapshot(repo, feature)
	if err != nil {
		t.Fatal(err)
	}
	if invalid.State != deliverycontrol.StatePlanInvalid || invalid.ObservationID != observation.ID {
		t.Fatalf("invalid active plan did not enter the owned rewrite state: %+v", invalid)
	}
	if err := os.MkdirAll(filepath.Join(repo, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	sourcePlan := "docs/plan-repair-source.md"
	if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(sourcePlan)), []byte("# Corrected plan intent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	document := amendmentPlanDocument(t, feature, sourcePlan)
	prescription, err := ResolvePlanningBootstrap(BootstrapOptions{
		Repo: repo, Feature: feature, SourcePlan: sourcePlan, Artifact: "plan.md",
		Shell: BootstrapShellPOSIX, Document: document,
	})
	if err != nil {
		t.Fatal(err)
	}
	if prescription.Disposition != "AMEND_ACTIVE" || prescription.LifecycleState != string(deliverycontrol.StatePlanInvalid) {
		t.Fatalf("invalid active plan did not receive a lifecycle-bound rewrite prescription: %+v", prescription)
	}
	if _, err := WritePlanningArtifact(PlanningWriteOptions{
		Repo: repo, Feature: feature, Artifact: "plan.md", Content: document,
		SourcePlan: sourcePlan, SourcePlanSHA256: prescription.SourcePlanSHA256,
		ExpectedLifecycleSHA256: prescription.LifecycleSHA256,
		ExpectedPlanLockSHA256:  prescription.PreviousPlanLock,
		ExpectedObservation:     prescription.ObservationID,
	}); err != nil {
		t.Fatal(err)
	}
	drafted, err := ResolveLifecycleSnapshot(repo, feature)
	if err != nil {
		t.Fatal(err)
	}
	if drafted.State != deliverycontrol.StateAmendmentDrafted {
		t.Fatalf("corrected active plan did not enter the common approval path: %+v", drafted)
	}
}
