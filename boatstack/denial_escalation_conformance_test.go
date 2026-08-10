package boatstack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// control-law: repeated-denials-escalate-to-solutions
//
// Repetition of one denial is the signal that the stated law is not reaching
// the model (the sibling harness's paid canary: sixteen identical no-progress
// repair attempts, then a protected-boundary write). A repeat denial escalates
// its corrective INFORMATION — the full solution set plus a fresh diagnostic
// probe — never its severity or its admissibility. Boundaries held here:
// the per-worktree ledger (recordDenial/resetDenialLedger), the HookDecision
// deny/allow wiring, and the escalated rendering. The constitutional corpus
// floor is held separately by safety_corpus_test.go and is untouched: this law
// changes what a denial says, never what is denied.

func tamperEvent(path string) []byte {
	return []byte(fmt.Sprintf(
		`{"hook_event_name":"PreToolUse","tool_name":"Write","tool_input":{"file_path":%q,"content":"{}"}}`, path))
}

// Positive: the third identical denial escalates — the rendering carries the
// repeat notice and the fresh-probe prescription; the first two do not.
func TestThirdIdenticalDenialEscalates(t *testing.T) {
	repo := safetyTestRepo(t)
	engageHookFixture(t, repo)
	event := tamperEvent(".git/boatstack/deliveries/demo/state.json")
	for attempt := 1; attempt <= denialEscalationThreshold; attempt++ {
		output, denied := HookDecision(SafetyHookOptions{Host: "claude", Repo: repo, Input: event})
		if !denied {
			t.Fatalf("attempt %d: tamper write must be denied", attempt)
		}
		escalated := strings.Contains(string(output), "This denial repeated")
		if attempt < denialEscalationThreshold && escalated {
			t.Fatalf("attempt %d escalated before the threshold:\n%s", attempt, output)
		}
		if attempt == denialEscalationThreshold {
			if !escalated {
				t.Fatalf("attempt %d must escalate:\n%s", attempt, output)
			}
			if !strings.Contains(string(output), fmt.Sprintf("repeated %d times", denialEscalationThreshold)) {
				t.Fatalf("escalation must carry the repeat count:\n%s", output)
			}
			if !strings.Contains(string(output), ".product-loop/boatstack doctor") {
				t.Fatalf("escalation must prescribe the fresh diagnostic:\n%s", output)
			}
		}
	}
}

// Negative/reset: an ALLOWED mutation-capable call is forward progress and
// clears the ledger — the next denial starts unescalated.
func TestAllowedMutationResetsTheLedger(t *testing.T) {
	repo := safetyTestRepo(t)
	engageHookFixture(t, repo)
	event := tamperEvent(".git/boatstack/deliveries/demo/state.json")
	for i := 0; i < denialEscalationThreshold-1; i++ {
		if _, denied := HookDecision(SafetyHookOptions{Host: "claude", Repo: repo, Input: event}); !denied {
			t.Fatal("tamper write must be denied")
		}
	}
	allowed := []byte(fmt.Sprintf(
		`{"hook_event_name":"PreToolUse","tool_name":"Write","tool_input":{"file_path":%q,"content":"export const x = 1"}}`,
		filepath.Join(repo, "src", "app.ts")))
	if _, denied := HookDecision(SafetyHookOptions{Host: "claude", Repo: repo, Input: allowed}); denied {
		t.Fatal("ordinary product write must be allowed")
	}
	output, denied := HookDecision(SafetyHookOptions{Host: "claude", Repo: repo, Input: event})
	if !denied {
		t.Fatal("tamper write must still be denied after the reset")
	}
	if strings.Contains(string(output), "This denial repeated") {
		t.Fatalf("ledger must reset after allowed progress:\n%s", output)
	}
}

// Relation: different denial keys never cross-escalate, and a read-only
// allowed call does NOT reset the ledger (only mutation progress does).
func TestDenialKeysAreIsolated(t *testing.T) {
	repo := safetyTestRepo(t)
	engageHookFixture(t, repo)
	tamper := SafetyFinding{Category: "workflow-state-tamper", Source: "delivery-state"}
	phase := SafetyFinding{Category: "workflow-phase-bypass", WorkflowStage: "DRAFT_PLAN", Source: "planning-state"}
	if got := recordDenial(repo, tamper); got != 1 {
		t.Fatalf("first tamper count = %d, want 1", got)
	}
	if got := recordDenial(repo, tamper); got != 2 {
		t.Fatalf("second tamper count = %d, want 2", got)
	}
	if got := recordDenial(repo, phase); got != 1 {
		t.Fatalf("a different category must count from 1, got %d", got)
	}
	// Same category at a different stage is a different key.
	other := SafetyFinding{Category: "workflow-phase-bypass", WorkflowStage: "APPROVED", Source: "planning-state"}
	if got := recordDenial(repo, other); got != 1 {
		t.Fatalf("same category at a different stage must count from 1, got %d", got)
	}
	// A read-only allowed call must not reset: HookDecision only resets on
	// mutation-capable allows, so the tamper key keeps its count.
	readonly := []byte(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git status"}}`)
	if _, denied := HookDecision(SafetyHookOptions{Host: "claude", Repo: repo, Input: readonly}); denied {
		t.Fatal("git status must be allowed")
	}
	if got := recordDenial(repo, tamper); got != 3 {
		t.Fatalf("read-only allow must not reset the ledger; tamper count = %d, want 3", got)
	}
}

// Failure-state: bookkeeping degrades, the denial never does. A corrupt ledger
// starts fresh; an unresolvable repository still yields a usable count; the
// escalated rendering is identical in admissibility to the unescalated one.
func TestLedgerFailuresDegradeCalmly(t *testing.T) {
	repo := safetyTestRepo(t)
	path, ok := denialLedgerPath(repo)
	if !ok {
		t.Fatal("ledger path must resolve in a git fixture")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	finding := SafetyFinding{Category: "workflow-state-tamper", Source: "delivery-state"}
	if got := recordDenial(repo, finding); got != 1 {
		t.Fatalf("corrupt ledger must start fresh, got %d", got)
	}
	if got := recordDenial(t.TempDir(), finding); got < 1 {
		t.Fatalf("an unresolvable ledger must still return a usable count, got %d", got)
	}
}

// Bypass/bound: the ledger is pruned to its key bound, oldest first, so a
// category-spraying session cannot grow unbounded per-worktree state.
func TestLedgerStaysBounded(t *testing.T) {
	repo := safetyTestRepo(t)
	for i := 0; i < denialLedgerMaxKeys+8; i++ {
		recordDenial(repo, SafetyFinding{Category: fmt.Sprintf("category-%02d", i), Source: "test"})
	}
	path, _ := denialLedgerPath(repo)
	ledger := loadDenialLedger(path)
	if len(ledger.Counts) > denialLedgerMaxKeys {
		t.Fatalf("ledger holds %d keys, bound is %d", len(ledger.Counts), denialLedgerMaxKeys)
	}
}

// Rendering: escalation lifts the pick cap to the full structured set and the
// structured payload carries escalated/repeat_count; severity is unchanged.
func TestEscalatedRenderingLiftsTheCapNotTheSeverity(t *testing.T) {
	finding := SafetyFinding{
		Category: "workflow-phase-bypass", Source: "planning-state",
		WorkflowStage: "DRAFT_PLAN", NextOperation: "plan-gate", BlockingFeature: "demo",
	}
	calm := denialWithOptions(".", "claude", finding)
	finding.RepeatCount = denialEscalationThreshold
	escalated := denialWithOptions(".", "claude", finding)
	if !escalated.Escalated || escalated.Severity != calm.Severity || escalated.Category != calm.Category {
		t.Fatalf("escalation must change information only: %+v vs %+v", escalated, calm)
	}
	if len(escalated.Options) != len(calm.Options) {
		t.Fatalf("the option SET is identical; only the rendered cap lifts")
	}
	if escalated.optionTextLimit() <= calm.optionTextLimit() && len(calm.Options) > calm.optionTextLimit() {
		t.Fatal("escalated rendering must show more of the set")
	}
	structured := escalated.Structured()
	if structured["escalated"] != true || structured["repeat_count"] != denialEscalationThreshold {
		t.Fatalf("structured payload must carry escalation: %v", structured)
	}
	if _, present := calm.Structured()["escalated"]; present {
		t.Fatal("unescalated payload must not carry the escalation keys")
	}
}
