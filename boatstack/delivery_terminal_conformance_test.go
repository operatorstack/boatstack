package boatstack

// control-law: terminal-goal-defaults-to-published-and-hydrates-from-state-then-config
//
// The delivery terminal — the standing goal of the flow — resolves in a fixed
// order: the goal the delivery was ACTIVATED under (state.Goal), then the
// repository config (delivery.terminal), then the published default. The
// default is a hard no-op: with no delivery block (or an explicit
// "published"), every advisory output is identical to the pre-field
// behavior, because a goal this standing is widened only by an explicit
// operator choice, never by an upgrade. Invalid and unreadable inputs
// resolve to the NARROWER published goal (fail-closed direction: the wider
// goal implies more agent-owned steps).
//
// Test classes: positive (config merged → Terminal merged; activation
// snapshots the non-default goal), relation (state.Goal overrides config both
// ways — hysteresis), negative (invalid config value fails validation;
// invalid state.Goal is ignored), bypass (default vs explicit published →
// byte-identical rendering and JSON across the slice lifecycle), failure-state
// (a pre-field state file without goal loads clean and resolves from config).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTerminalConfig(t *testing.T, repo, terminal string) {
	t.Helper()
	config := testConfig()
	if terminal != "" {
		config.Delivery = &DeliveryPolicy{Terminal: terminal}
	}
	value, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".product-loop", "project.json"), value, 0o644); err != nil {
		t.Fatal(err)
	}
}

// Positive: the configured terminal surfaces on the advisory, and only the
// widened goal earns a rendered line.
func TestConfiguredTerminalSurfacesOnAdvisory(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "feature", "BUILD", 0)
	writeTerminalConfig(t, repo, "merged")

	next, err := NextControl(repo, "feature")
	if err != nil {
		t.Fatal(err)
	}
	if next.Terminal != TerminalMerged {
		t.Fatalf("terminal = %q, want merged", next.Terminal)
	}
	if !strings.Contains(FormatFlowNext(next), "Terminal goal: merged") {
		t.Fatal("widened goal must be visible in the rendering")
	}
}

// Relation: the activation snapshot outranks config in BOTH directions — a
// delivery keeps the goal it was started under when config flips mid-flight.
func TestActivationSnapshotOverridesConfig(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "feature", "BUILD", 0)

	// Delivery activated under merged; config later narrowed to published.
	state, err := LoadDeliveryState(repo, "feature")
	if err != nil {
		t.Fatal(err)
	}
	state.Goal = string(TerminalMerged)
	if err := saveDeliveryState(repo, state); err != nil {
		t.Fatal(err)
	}
	writeTerminalConfig(t, repo, "published")
	if got := resolveDeliveryTerminal(repo, "feature"); got != TerminalMerged {
		t.Fatalf("mid-flight narrowing changed the goal: %q", got)
	}

	// Delivery activated under the default; config later widened to merged.
	// The empty snapshot means "resolve from config", so the widening applies.
	state.Goal = ""
	if err := saveDeliveryState(repo, state); err != nil {
		t.Fatal(err)
	}
	writeTerminalConfig(t, repo, "merged")
	if got := resolveDeliveryTerminal(repo, "feature"); got != TerminalMerged {
		t.Fatalf("config terminal not hydrated: %q", got)
	}

	// An invalid snapshot value is ignored, never trusted.
	state.Goal = "deployed"
	if err := saveDeliveryState(repo, state); err != nil {
		t.Fatal(err)
	}
	writeTerminalConfig(t, repo, "")
	if got := resolveDeliveryTerminal(repo, "feature"); got != TerminalPublished {
		t.Fatalf("invalid snapshot must resolve to published: %q", got)
	}
}

// Positive: first activation snapshots the non-default goal onto the new
// delivery state; the default snapshots nothing (byte-stable state files).
func TestActivationSnapshotsNonDefaultGoalOnly(t *testing.T) {
	for _, test := range []struct {
		terminal string
		wantGoal string
	}{
		{"merged", "merged"},
		{"published", ""},
		{"", ""},
	} {
		repo := nextTestRepo(t)
		writeTerminalConfig(t, repo, test.terminal)
		if got := deliveryGoalSnapshot(repo); got != test.wantGoal {
			t.Fatalf("terminal %q: snapshot = %q, want %q", test.terminal, got, test.wantGoal)
		}
	}
}

// Negative: an explicit invalid enum fails config validation fail-closed.
func TestInvalidTerminalRejectedByValidation(t *testing.T) {
	config := testConfig()
	config.Delivery = &DeliveryPolicy{Terminal: "deployed"}
	if err := ValidateConfig(config); err == nil || !strings.Contains(err.Error(), "delivery.terminal") {
		t.Fatalf("invalid terminal accepted: %v", err)
	}
	config.Delivery = &DeliveryPolicy{}
	if err := ValidateConfig(config); err != nil {
		t.Fatalf("empty terminal must stay legal: %v", err)
	}
}

// Bypass: the default is a hard no-op — for every slice-lifecycle stage, the
// advisory under an absent delivery block is byte-identical (JSON and
// rendering) to an explicit published terminal, and carries no merged
// wording anywhere.
func TestDefaultTerminalIsByteIdenticalToExplicitPublished(t *testing.T) {
	for _, stage := range []string{"BUILD", "TEST_PASSED", "REVIEW_PASSED", "PUBLISHED"} {
		capture := func(terminal string) (string, string) {
			repo := nextTestRepo(t)
			activeIndex := 0
			if stage == "PUBLISHED" {
				activeIndex = 1
			}
			writeNextDelivery(t, repo, "feature", stage, activeIndex)
			writeTerminalConfig(t, repo, terminal)
			if stage == "PUBLISHED" {
				updateRecoveryDelivery(t, repo, "feature", "feat/phase", "https://example.invalid/pr/9", "")
				withRecoveryGh(t, phaseObservationPayload("OPEN", "", "CLEAN", rollupCheckRunPass))
			}
			next, err := NextControl(repo, "feature")
			if err != nil {
				t.Fatal(err)
			}
			// The repo path differs per fixture; blank it out of the compared
			// values so only behavior is compared. JSON escapes Windows path
			// separators, so the escaped form must be blanked too.
			value, err := MarshalJSON(next)
			if err != nil {
				t.Fatal(err)
			}
			escaped, err := json.Marshal(repo)
			if err != nil {
				t.Fatal(err)
			}
			blank := func(s string) string {
				s = strings.ReplaceAll(s, strings.Trim(string(escaped), `"`), "<repo>")
				return strings.ReplaceAll(s, repo, "<repo>")
			}
			return blank(string(value)), blank(FormatFlowNext(next))
		}
		defaultJSON, defaultText := capture("")
		publishedJSON, publishedText := capture("published")
		if defaultJSON != publishedJSON {
			t.Fatalf("stage %s: default and explicit published diverge:\n%s\n---\n%s", stage, defaultJSON, publishedJSON)
		}
		if defaultText != publishedText {
			t.Fatalf("stage %s: rendering diverges:\n%s\n---\n%s", stage, defaultText, publishedText)
		}
		if strings.Contains(defaultText, "merged (delivery.terminal)") {
			t.Fatalf("stage %s: default rendering mentions the widened goal:\n%s", stage, defaultText)
		}
	}
}

// Failure-state: a pre-field state file (no goal key) loads clean and
// resolves from config — the migration law is untouched by the additive
// field.
func TestPreFieldStateResolvesFromConfig(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "feature", "BUILD", 0)
	statePath, err := deliveryStatePath(repo, "feature")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "\"goal\"") {
		t.Fatal("fixture unexpectedly contains a goal key")
	}
	writeTerminalConfig(t, repo, "merged")
	if got := resolveDeliveryTerminal(repo, "feature"); got != TerminalMerged {
		t.Fatalf("pre-field state did not hydrate from config: %q", got)
	}
	if _, err := LoadDeliveryState(repo, "feature"); err != nil {
		t.Fatalf("pre-field state failed to load: %v", err)
	}
}
