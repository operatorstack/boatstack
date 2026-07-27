package boatstack

import (
	"strings"
	"testing"
)

// control-law: guard-never-prescribes-what-it-would-deny
// control-law: solution-set-derives-from-guard-declarations
//
// Denial-basis conformance for the solution set. A denial that only states the
// law leaves a weaker model looping on the same blocked call — the sibling
// harness's paid canary recorded exactly that trajectory: sixteen no-progress
// repair attempts escalating into a protected-boundary write. Every denial
// therefore carries computed picks, and these sweeps hold that carrier total
// (every category enumerates or is a documented exception) and closed (every
// pick passes the guard's own text laws).

// denialCategoryInventory is one representative finding per category
// denialFor distinguishes, including the operation-* and generic fallthroughs.
// Extend it together with denialFor; the totality sweep fails a category that
// is neither enumerated nor excepted.
var denialCategoryInventory = []SafetyFinding{
	{Category: "malformed-tool-input", Reason: "empty-command", Source: "hook"},
	{Category: "workflow-state-invalid", NextOperation: "discard-delivery", BlockingFeature: "stale", Source: "delivery-state"},
	{Category: "workflow-state-tamper", Source: "delivery-state", AttemptedPath: ".git/boatstack/deliveries/demo/state.json"},
	{Category: "workflow-phase-bypass", Source: "planning-state", WorkflowStage: "DRAFT_PLAN", NextOperation: "plan-gate", BlockingFeature: "demo"},
	{Category: "workflow-phase-bypass", Source: "planning-state", WorkflowStage: "NOT_STARTED", NextOperation: "planning-write", AttemptedPath: ".product-loop/features/demo/plan.md"},
	{Category: "workflow-publication-bypass", BlockingFeature: "demo", BlockingSlice: "s1", Source: "tool-input"},
	{Category: "operation-in-flight", OperationID: "op_1", OperationState: "RUNNING", Source: "operation-state"},
	{Category: "operation-already-succeeded", OperationID: "op_2", OperationState: "SUCCEEDED", Source: "operation-state"},
	{Category: "operation-reconciliation-required", OperationID: "op_3", Source: "operation-state"},
	{Category: "operation-retry-exhausted", OperationID: "op_4", Source: "operation-state"},
	{Category: "operation-state-invalid", Source: "operation-state"},
	{Category: "git-history-destruction", Source: "command"},
	{Category: "workspace-sync-bypass", Source: "command"},
	{Category: "filesystem-destruction", Source: "command"},
	{Category: "database-destruction", Source: "command"},
	{Category: "infrastructure-destruction", Source: "command"},
	{Category: "external-resource-destruction", Source: "tool-input"},
	{Category: "unsupported-host", Source: "hook"},
	{Category: "unresolved-repository", Source: "hook"},
	{Category: "symlink-entrypoint", Source: "entry.sh"},
}

// Totality: every denial category yields a non-empty solution set or sits on
// the documented exception list with a reason.
func TestEveryDenialCategoryEnumeratesOrIsExcepted(t *testing.T) {
	for _, finding := range denialCategoryInventory {
		set := enumerateDenialSolutions(".", "claude", finding)
		if reason, excepted := denialSolutionExceptions[finding.Category]; excepted {
			if reason == "" {
				t.Errorf("exception for %s must carry a reason", finding.Category)
			}
			if len(set.Options) != 0 {
				t.Errorf("%s is excepted but enumerates options — remove the stale exception", finding.Category)
			}
			continue
		}
		if len(set.Options) == 0 {
			t.Errorf("category %s enumerates no options and is not a documented exception", finding.Category)
		}
	}
}

// Closure: every denial pick, after owed-input substitution, passes the
// text-level guard laws — the managed-state path law and the destruction
// classifier. The guard never hands out a command it would then deny as text.
func TestDenialSolutionCommandsPassTheTextGuards(t *testing.T) {
	for _, finding := range denialCategoryInventory {
		set := enumerateDenialSolutions(".", "claude", finding)
		for _, option := range set.Options {
			line := substituteOwedFlags(option.CommandLine())
			if deliveryStatePathPattern.MatchString(line) && !isPureReadOnlyCommand(line) && !approvedUpdatePublisherPattern.MatchString(line) {
				t.Errorf("%s: pick %q names managed state the guard would deny", finding.Category, line)
			}
			if findings := classifySafetyText(line, "command", commandExecutesLiveSQL(line)); len(findings) > 0 {
				t.Errorf("%s: pick %q trips the text guard: %+v", finding.Category, line, findings)
			}
			if option.AutoDerivable != (len(option.RequiresHumanInput) == 0) {
				t.Errorf("%s: AutoDerivable must equal owed-input emptiness: %+v", finding.Category, option)
			}
			for _, owed := range option.RequiresHumanInput {
				for _, arg := range option.Args {
					if arg == owed {
						t.Errorf("%s: owed flag %s fabricated into Args: %+v", finding.Category, owed, option)
					}
				}
			}
		}
	}
}

// Relation: a phase-bypass denial's picks are exactly guard-admitted at the
// finding's own stage — the same closure the flow basis holds, entered through
// the denial door.
func TestPhaseBypassDenialPicksAreGuardAdmitted(t *testing.T) {
	finding := SafetyFinding{
		Category: "workflow-phase-bypass", Source: "planning-state",
		WorkflowStage: "DRAFT_PLAN", NextOperation: "plan-gate", BlockingFeature: "demo",
	}
	set := enumerateDenialSolutions(".", "claude", finding)
	if len(set.Options) == 0 {
		t.Fatal("phase-bypass must enumerate picks")
	}
	for _, option := range set.Options {
		line := substituteOwedFlags(option.CommandLine())
		if !controlledPhaseTransition(line, finding.WorkflowStage) {
			t.Errorf("denial pick %q is not admitted at %s", line, finding.WorkflowStage)
		}
	}
}

// Ownership: a state-tamper denial names the attempted path's declared owner
// verbs from the state-ownership map, and each named verb is a real one the
// map declares for that subtree.
func TestTamperDenialNamesDeclaredOwnerVerbs(t *testing.T) {
	repo := safetyTestRepo(t)
	cases := map[string][]string{
		".git/boatstack/deliveries/demo/state.json":      {"activate-plan", "record-delivery-gate", "record-change", "publish-pr", "repair-state", "discard-delivery"},
		".git/boatstack/updates/v9.9.9/pr-preview.json":  {"prepare-update-pr", "publish-update-pr"},
		".git/boatstack/mutations/v1/abc.json":           {"activate-plan", "undo"},
		".git/boatstack/quarantine/demo/receipt.json":    {"repair-state"},
		"state-root/boatstack/registry.json":             {"attach", "detach"},
		".git/boatstack/visual-evidence/x/manifest.json": {"record-pr-visual-evidence", "capture-evidence", "record-pr-visual-publication"},
		"boatstack/repositories/sample/binding.json":     {"attach", "detach", "activate"},
	}
	for attempted, want := range cases {
		got := tamperOwnerVerbs(repo, attempted)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("owner verbs for %s = %v, want %v", attempted, got, want)
		}
	}
	if got := tamperOwnerVerbs(repo, ""); got != nil {
		t.Errorf("empty attempted path must derive no owners, got %v", got)
	}
	if got := tamperOwnerVerbs(repo, "src/app.ts"); got != nil {
		t.Errorf("unmanaged path must derive no owners, got %v", got)
	}
}

// Rendering: the plain denial carries the capped "You can:" list; the
// structured payload carries the full set additively under schema_version 1.
func TestDenialRenderingCarriesTheSolutionSet(t *testing.T) {
	finding := SafetyFinding{
		Category: "workflow-phase-bypass", Source: "planning-state",
		WorkflowStage: "DRAFT_PLAN", NextOperation: "plan-gate", BlockingFeature: "demo",
	}
	denial := denialWithOptions(".", "claude", finding)
	if len(denial.Options) == 0 {
		t.Fatal("denial must carry options")
	}
	for _, mode := range []RenderMode{RenderPlain, RenderMarkdown, RenderANSI} {
		out := denial.Render(mode)
		if !strings.Contains(strings.ToLower(out), "you can:") {
			t.Errorf("mode %v must render the You can list:\n%s", mode, out)
		}
	}
	plain := denial.Render(RenderPlain)
	if got := strings.Count(plain, "\n  "); got > solutionSetTextCap+1 {
		t.Errorf("plain rendering must cap the pick list, got %d lines:\n%s", got, plain)
	}
	structured := denial.Structured()
	if structured["schema_version"] != 1 {
		t.Fatalf("options are additive; schema_version must stay 1, got %v", structured["schema_version"])
	}
	options, ok := structured["options"].([]map[string]any)
	if !ok || len(options) != len(denial.Options) {
		t.Fatalf("structured options must carry the full set: %v", structured["options"])
	}
	for _, row := range options {
		if row["command_line"] == "" || row["verb"] == "" || row["transition"] == "" {
			t.Errorf("structured option incomplete: %v", row)
		}
	}

	// Ownership derivation resolves per-worktree sample paths, which needs a
	// real Git directory — the projected distribution runs these tests outside
	// any repository, so the tamper case uses the git-backed fixture.
	tamper := denialWithOptions(safetyTestRepo(t), "claude", SafetyFinding{
		Category: "workflow-state-tamper", Source: "delivery-state",
		AttemptedPath: ".git/boatstack/deliveries/demo/state.json",
	})
	if len(tamper.OwnerVerbs) == 0 {
		t.Fatal("tamper denial must derive owner verbs")
	}
	if out := tamper.Render(RenderPlain); !strings.Contains(out, "This path is owned by: activate-plan") {
		t.Errorf("tamper rendering must name the owners:\n%s", out)
	}
	if verbs, ok := tamper.Structured()["owner_verbs"].([]string); !ok || len(verbs) == 0 {
		t.Errorf("structured tamper payload must carry owner_verbs")
	}
}
