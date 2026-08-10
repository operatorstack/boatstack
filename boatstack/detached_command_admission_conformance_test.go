package boatstack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func detachedPolicyReadyFixture(t *testing.T) (string, WorkspaceContext, FlowNext) {
	t.Helper()
	repo := detachedTestRepo(t, "https://github.com/acme/detached-command-admission.git")
	// Exercise the real macOS failure shape: the trusted helper path contains a
	// space and therefore must survive rendering and literal parsing unchanged.
	t.Setenv(stateRootEnv, filepath.Join(t.TempDir(), "Application Support"))
	invalidateWorkspaceCache()
	embeddedFeatureForDetach(t, repo, "feature-one", "")
	result, err := AttachDetached(AttachOptions{Repo: repo})
	if err != nil || result.VerificationStatus != "VERIFIED" {
		t.Fatalf("attach detached fixture: %+v %v", result, err)
	}
	workspace, err := ResolveWorkspaceContext(repo)
	if err != nil || workspace.Mode != SupervisionDetached {
		t.Fatalf("resolve detached workspace: %+v %v", workspace, err)
	}
	next, err := NextControl(repo, "feature-one")
	if err != nil || next.Prescribed == nil || next.Prescribed.Verb != "activate-plan" {
		t.Fatalf("policy-ready fixture did not prescribe activation: %+v %v", next, err)
	}
	return repo, workspace, next
}

// Positive and relation conformance for control-law:
// guard-never-denies-an-owned-transition.
func TestDetachedPrescribedActivationPassesEveryHostGuard(t *testing.T) {
	repo, _, next := detachedPolicyReadyFixture(t)
	command := next.Prescribed.CommandLine()
	words, complete := literalCommandWords(strings.TrimPrefix(command, "& "))
	if !complete || len(words) < 2 || words[0] != next.Prescribed.Program || !strings.Contains(words[0], "Application Support") {
		t.Fatalf("fixture lost the path-with-spaces witness: %q", command)
	}
	if findings := ClassifyCommand(repo, command); len(findings) != 0 {
		t.Fatalf("guard denied its exact detached activation prescription: %+v\n%s", findings, command)
	}
	for _, host := range []string{"cursor", "claude", "codex", "gemini"} {
		t.Run(host, func(t *testing.T) {
			if output, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: planningHookInput(t, host, command)}); denied {
				t.Fatalf("%s denied the exact owned transition: %s", host, output)
			}
		})
	}
}

// Negative, bypass, and failure-state conformance for control-law:
// guard-never-denies-an-owned-transition. Only the exact bound helper and its
// same-workspace paths receive semantic admission; lexical lookalikes retain the
// controller-state tamper denial.
func TestDetachedCommandAdmissionRejectsUnownedAndWrongStageForms(t *testing.T) {
	repo, workspace, next := detachedPolicyReadyFixture(t)
	valid := next.Prescribed.CommandLine()
	otherRoot := filepath.Join(filepath.Dir(workspace.controlRoot), "ffffffffffffffff")
	otherHelper := filepath.Join(otherRoot, productLoopDirName, "bin", helperName())
	otherOutput := filepath.Join(otherRoot, productLoopDirName, "features", "feature-one", "plan.lock.json")
	otherRepo := t.TempDir()
	wrongStage := PrescribedCommand{
		Program: workspace.HelperPath(), Verb: "record-approval",
		Args: []string{"--plan", filepath.Join(workspace.FeatureDir("feature-one"), "plan.md")},
	}.CommandLine()

	tests := []struct {
		name     string
		command  string
		category string
	}{
		{"sibling helper", strings.Replace(valid, posixPlanningWord(workspace.HelperPath()), posixPlanningWord(otherHelper), 1), "workflow-state-tamper"},
		{"mixed controller roots", strings.Replace(valid, posixPlanningWord(filepath.Join(workspace.FeatureDir("feature-one"), "plan.lock.json")), posixPlanningWord(otherOutput), 1), "workflow-state-tamper"},
		{"conflicting repository flags", valid + " --repo " + posixPlanningWord(otherRepo), "workflow-state-tamper"},
		{"conflicting feature flags", valid + " --feature feature-two", "workflow-state-tamper"},
		{"compound command", valid + " ; echo bypass", "workflow-state-tamper"},
		{"wrong stage", wrongStage, "workflow-phase-bypass"},
		{"raw controller deletion", "rm -rf " + posixPlanningWord(workspace.controlRoot), "workflow-state-tamper"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			findings := ClassifyCommand(repo, test.command)
			if len(findings) == 0 || findings[0].Category != test.category {
				t.Fatalf("%s escaped the boundary: %+v\n%s", test.name, findings, test.command)
			}
		})
	}
	if status, err := ResolveNext(repo, "feature-one"); err != nil || status.ObservedStage != "POLICY_READY" {
		t.Fatalf("denials changed protected workflow state: %+v %v", status, err)
	}
}

// Relation conformance for control-law: active-delivery-effects-are-supervised.
// Boatstack's own read-only detached observations are not external effects and
// must never create a competing generic operation receipt.
func TestDetachedReadOnlyHelperIsReceiptFreeDuringActiveDelivery(t *testing.T) {
	repo, workspace, _ := detachedPolicyReadyFixture(t)
	directory := workspace.FeatureDir("feature-one")
	lockPath := filepath.Join(directory, "plan.lock.json")
	if err := os.WriteFile(lockPath, []byte("lock\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lockHash, err := SHA256File(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveDeliveryState(repo, DeliveryState{
		SchemaVersion: deliveryStateSchemaVersion, Feature: "feature-one", PlanLockHash: lockHash,
		ActiveIndex: 0, Slices: []DeliverySlice{{ID: "delivery", Title: "Delivery", Status: "BUILD", BaseBranch: "main", HeadBranch: "main"}},
	}); err != nil {
		t.Fatal(err)
	}
	command := PrescribedCommand{
		Program: workspace.HelperPath(), Verb: "next-status",
		Args: []string{"--repo", repo, "--feature", "feature-one"},
	}.CommandLine()
	for _, host := range []string{"cursor", "claude", "codex", "gemini"} {
		if output, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: planningHookInput(t, host, command)}); denied {
			t.Fatalf("%s denied a detached read-only observation: %s", host, output)
		}
		status, statusErr := ResolveOperationStatus(repo, "")
		if statusErr != nil || status.Operation != nil {
			t.Fatalf("%s created a generic operation receipt: %+v %v", host, status, statusErr)
		}
	}
}

// Positive, negative, and relation conformance for control-law:
// guard-never-denies-an-owned-transition. The opt-in driver is an owned
// coordinator only when its feature scope is explicit; the helper retains the
// inner AutoDerivable and transition-allowlist gates.
func TestDetachedFlowExecuteCoordinatorRequiresExplicitFeature(t *testing.T) {
	repo, workspace, _ := detachedPolicyReadyFixture(t)
	command := PrescribedCommand{
		Program: workspace.HelperPath(), Verb: "flow",
		Args: []string{"next", "--repo", repo, "--feature", "feature-one", "--execute"},
	}.CommandLine()
	if findings := ClassifyCommand(repo, command); len(findings) != 0 {
		t.Fatalf("guard denied the exact feature-scoped flow coordinator: %+v", findings)
	}
	for _, host := range []string{"cursor", "claude", "codex", "gemini"} {
		if output, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: planningHookInput(t, host, command)}); denied {
			t.Fatalf("%s denied the exact feature-scoped flow coordinator: %s", host, output)
		}
	}

	unscoped := strings.Replace(command, " --feature feature-one", "", 1)
	if findings := ClassifyCommand(repo, unscoped); len(findings) == 0 || findings[0].Category != "workflow-state-tamper" {
		t.Fatalf("unscoped execute coordinator crossed the protected boundary: %+v", findings)
	}
	malformed := command + " --unknown"
	if findings := ClassifyCommand(repo, malformed); len(findings) == 0 {
		t.Fatalf("malformed execute coordinator crossed the protected boundary: %+v", findings)
	}
}

// Relation conformance for control-law: guard-never-denies-an-owned-transition.
// Feed the real detached solution set back through the full classifier at each
// live delivery stage so helper-path protection and the workflow oracle cannot
// drift independently again.
func TestDetachedDeliverySolutionSetPassesFullClassifier(t *testing.T) {
	for _, stage := range []string{"BUILD", "TEST_PASSED", "REVIEW_PASSED"} {
		t.Run(stage, func(t *testing.T) {
			repo, workspace, _ := detachedPolicyReadyFixture(t)
			lockPath := filepath.Join(workspace.FeatureDir("feature-one"), "plan.lock.json")
			if err := os.WriteFile(lockPath, []byte("lock\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			lockHash, err := SHA256File(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := saveDeliveryState(repo, DeliveryState{
				SchemaVersion: deliveryStateSchemaVersion, Feature: "feature-one", PlanLockHash: lockHash,
				ActiveIndex: 0, Slices: []DeliverySlice{{ID: "delivery", Title: "Delivery", Status: stage, BaseBranch: "main", HeadBranch: "main"}},
			}); err != nil {
				t.Fatal(err)
			}
			next, err := NextControl(repo, "feature-one")
			if err != nil {
				t.Fatal(err)
			}
			options := append([]PrescribedCommand{}, next.Alternatives...)
			if next.Prescribed != nil {
				options = append(options, *next.Prescribed)
			}
			if len(options) == 0 {
				t.Fatalf("%s exposed no legal solution", stage)
			}
			for _, option := range options {
				if option.Program == "gh" {
					continue
				}
				command := substituteOwedFlags(option.CommandLine())
				if findings := ClassifyCommand(repo, command); len(findings) != 0 {
					t.Errorf("%s guard denied its prescribed %q: %+v", stage, command, findings)
				}
			}
		})
	}
}
