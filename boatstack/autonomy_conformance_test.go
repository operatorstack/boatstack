package boatstack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func eligibleAutonomyDecision() AutonomyDecision {
	return AutonomyDecision{
		ID: "Q-1", Question: "Which existing helper should this use?", Resolution: "RESOLVED_BY_POLICY",
		SelectedOption: "1a", Options: []AutonomyOption{{ID: "1a", Text: "Reuse helper", Recommended: true}, {ID: "1b", Text: "Inline logic"}},
		WithinSpec: true, Reversible: true, EvidenceIDs: []string{"ev_1"},
		Verification: AutonomyVerification{Run: "go test ./...", Oracle: "existing conformance test passes"},
		Rationale:    "The repository already owns the helper.",
	}
}

// control-law: autonomy-receipt-binds-policy-activation-to-plan-repository-and-branch
func TestAutonomyReceiptOverridesHumanPlanGateOnlyForExactPlan(t *testing.T) {
	root := t.TempDir()
	_, _, planPath := writePlanInputs(t, root, true)
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "Boatstack Test")
	runGit(t, root, "config", "user.email", "boatstack@example.invalid")
	runGit(t, root, "remote", "add", "origin", "https://example.invalid/operatorstack/example.git")
	writeActivationConfig(t, root, true)
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "record planning inputs")
	autonomyPath := filepath.Join(root, "autonomy.md")
	receipt, err := RecordAutonomy(AutonomyRecordOptions{Repo: root, PlanPath: planPath, Target: RunTargetVerified, OutputPath: autonomyPath})
	if err != nil {
		t.Fatal(err)
	}
	compiled := filepath.Join(root, "compiled")
	lockPath := filepath.Join(root, "plan.lock.json")
	if err := ActivatePlan(ActivationOptions{Repo: root, PlanPath: planPath, AutonomyPath: autonomyPath, OutDir: compiled, OutputPath: lockPath, SourceCommit: "test"}); err != nil {
		t.Fatal(err)
	}
	value, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	lock := map[string]any{}
	if err := json.Unmarshal(value, &lock); err != nil {
		t.Fatal(err)
	}
	if stringValue(lock["authorization_mode"]) != "policy" || stringValue(lock["autonomy_fingerprint"]) != receipt.Fingerprint || stringValue(lock["run_target"]) != "verified" {
		t.Fatalf("activation did not bind scoped policy authority: %#v", lock)
	}
	planBytes, _ := os.ReadFile(planPath)
	if err := os.WriteFile(planPath, append([]byte("changed\n"), planBytes...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckAutonomyReceipt(autonomyPath, PlanCheck{Plan: validPlan(), PlanPath: planPath, Fingerprint: "changed"}, root, RunTargetVerified, ""); err == nil {
		t.Fatal("changed plan retained autonomous authority")
	}
}

// control-law: autonomy-receipt-binds-policy-activation-to-plan-repository-and-branch
func TestAutonomyReceiptRequiresFreshWorkspaceBranch(t *testing.T) {
	root := workspaceRepo(t, defaultWorkspace())
	runGit(t, root, "remote", "add", "origin", "https://example.invalid/operatorstack/example.git")
	_, _, planPath := writePlanInputs(t, root, true)
	if _, err := RecordAutonomy(AutonomyRecordOptions{Repo: root, PlanPath: planPath, Target: RunTargetVerified}); err == nil || !strings.Contains(err.Error(), "workspace-cut") {
		t.Fatalf("pre-cut autonomy should name the workspace transition, got %v", err)
	}
}

// control-law: autonomous-pr-authority-correlates-one-repository-branch-and-action
func TestAutonomyReceiptRejectsDifferentPRAction(t *testing.T) {
	root := t.TempDir()
	_, _, planPath := writePlanInputs(t, root, true)
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "Boatstack Test")
	runGit(t, root, "config", "user.email", "boatstack@example.invalid")
	runGit(t, root, "remote", "add", "origin", "https://example.invalid/operatorstack/example.git")
	writeActivationConfig(t, root, true)
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "record planning inputs")
	check, err := CheckPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	receipt := AutonomyReceipt{SchemaVersion: 1, Feature: stringValue(check.Plan["feature_id"]), Target: RunTargetPR, Repository: "https://example.invalid/operatorstack/example.git", Branch: "main", PRAction: "open", PlanPath: planPath, PlanFingerprint: check.Fingerprint, Decisions: []AutonomyDecision{}, Evidence: []EvidenceRecord{}}
	receipt.Fingerprint, err = autonomyFingerprint(receipt)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := MarshalJSON(receipt)
	path := filepath.Join(root, "autonomy.md")
	content := []byte("# Boatstack autonomous run\n\n" + autonomyMarkerStart + "\n```json\n" + string(body) + "```\n" + autonomyMarkerEnd + "\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckAutonomyReceipt(path, check, root, RunTargetPR, "open"); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckAutonomyReceipt(path, check, root, RunTargetPR, "update"); err == nil || !strings.Contains(err.Error(), "open, not update") {
		t.Fatalf("changed PR action retained authority: %v", err)
	}
}

func autonomyPlan(decision AutonomyDecision) map[string]any {
	return map[string]any{"autonomy_decisions": []AutonomyDecision{decision}}
}

// control-law: autonomous-decisions-stay-inside-the-declared-low-risk-envelope
func TestAutonomyEligibilityAcceptsOnlyCompleteLowRiskDecision(t *testing.T) {
	decision := eligibleAutonomyDecision()
	got, err := validateAutonomyDecisions(autonomyPlan(decision), map[string]EvidenceRecord{"ev_1": {ID: "ev_1"}})
	if err != nil || len(got) != 1 || got[0].Resolution != "RESOLVED_BY_POLICY" {
		t.Fatalf("eligible decision was not accepted: %#v, %v", got, err)
	}
}

// control-law: autonomous-decisions-stay-inside-the-declared-low-risk-envelope
func TestAutonomyEligibilityRejectsEveryProtectedImpact(t *testing.T) {
	cases := map[string]func(*AutonomyDecision){
		"material":            func(d *AutonomyDecision) { d.Material = true },
		"outside-spec":        func(d *AutonomyDecision) { d.WithinSpec = false },
		"irreversible":        func(d *AutonomyDecision) { d.Reversible = false },
		"public-contract":     func(d *AutonomyDecision) { d.Impact.PublicContract = true },
		"acceptance":          func(d *AutonomyDecision) { d.Impact.AcceptanceCriteria = true },
		"security":            func(d *AutonomyDecision) { d.Impact.Security = true },
		"billing":             func(d *AutonomyDecision) { d.Impact.Billing = true },
		"migration":           func(d *AutonomyDecision) { d.Impact.Migration = true },
		"high-risk":           func(d *AutonomyDecision) { d.Impact.HighRiskPath = true },
		"destructive":         func(d *AutonomyDecision) { d.Impact.Destructive = true },
		"external-target":     func(d *AutonomyDecision) { d.Impact.ExternalTarget = true },
		"missing-evidence":    func(d *AutonomyDecision) { d.EvidenceIDs = nil },
		"missing-oracle":      func(d *AutonomyDecision) { d.Verification.Oracle = "" },
		"wrong-provenance":    func(d *AutonomyDecision) { d.Resolution = "ANSWERED" },
		"wrong-selection":     func(d *AutonomyDecision) { d.SelectedOption = "1b" },
		"two-recommendations": func(d *AutonomyDecision) { d.Options[1].Recommended = true },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			decision := eligibleAutonomyDecision()
			mutate(&decision)
			if _, err := validateAutonomyDecisions(autonomyPlan(decision), map[string]EvidenceRecord{"ev_1": {ID: "ev_1"}}); err == nil {
				t.Fatal("ineligible decision was accepted")
			}
		})
	}
}

// control-law: every-question-resolution-path-reaches-the-shared-decision-boundary
func TestQuestionResolutionPathInventory(t *testing.T) {
	paths := []struct {
		name  string
		input PlanDecisionInput
		want  DecisionOperator
	}{
		{"verified repository fact", PlanDecisionInput{PremiseStatus: PremiseValid, EvidenceLevel: EvidenceVerified}, OperatorInfer},
		{"supported fact", PlanDecisionInput{PremiseStatus: PremiseValid, EvidenceLevel: EvidenceSupported}, OperatorVerify},
		{"material intent", PlanDecisionInput{PremiseStatus: PremiseValid, EvidenceLevel: EvidenceAbsent, IsMaterial: true}, OperatorQuery},
		{"eligible policy choice", PlanDecisionInput{PremiseStatus: PremiseValid, EvidenceLevel: EvidenceAbsent, AutonomyEligible: true}, OperatorPolicy},
		{"invalid premise", PlanDecisionInput{PremiseStatus: PremiseInvalid}, OperatorReject},
		{"conflicting evidence", PlanDecisionInput{PremiseStatus: PremiseValid, EvidenceLevel: EvidenceConflicting}, OperatorEscalate},
		{"unknown uncertainty", PlanDecisionInput{PremiseStatus: PremiseUnknown, EvidenceLevel: EvidenceAbsent}, OperatorEscalate},
	}
	for _, path := range paths {
		t.Run(path.name, func(t *testing.T) {
			if got := ResolvePlanDecision(path.input).Operator; got != path.want {
				t.Fatalf("resolution = %s, want %s", got, path.want)
			}
		})
	}
	if len(paths) != 7 || strings.TrimSpace(string(OperatorPolicy)) == "" {
		t.Fatal("question-resolution inventory is incomplete")
	}
}

// control-law: every-question-resolution-path-reaches-the-shared-decision-boundary
func TestQuestionResolutionEntryPointInventory(t *testing.T) {
	entries := map[string][]string{
		"decision.go":        {"ResolvePlanDecision", "eligible-nonmaterial-policy-resolution"},
		"plan_validation.go": {"validatePlanAutonomy", "validateAutonomyDecisions"},
		"autonomy.go":        {"RecordAutonomy", "CheckAutonomyReceipt", "validateAutonomyDecisions"},
		"export.go":          {"shared decision boundary", "RESOLVED_BY_POLICY"},
		"SKILL.md":           {"RESOLVED_BY_POLICY", "Any failed or unknown condition pauses for the human"},
	}
	for path, snippets := range entries {
		value, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, snippet := range snippets {
			if !strings.Contains(string(value), snippet) {
				t.Fatalf("question-resolution entry %s bypasses boundary marker %q", path, snippet)
			}
		}
	}
}

func TestRunTargetClosedVocabulary(t *testing.T) {
	for _, value := range []string{"plan", "verified", "pr"} {
		if _, err := ParseRunTarget(value); err != nil {
			t.Fatalf("%s: %v", value, err)
		}
	}
	if _, err := ParseRunTarget("merge"); err == nil {
		t.Fatal("merge became an autonomous target")
	}
}
