package plant

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/ports"
)

type observerClock struct{ now time.Time }

func (c observerClock) Now() time.Time { return c.now }

func runGit(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}

func TestObserverBindsVerifiedRuntimeToExecutingBinary(t *testing.T) {
	// control-law: stale-runtime-selection-cannot-authorize-managed-effects
	repository := t.TempDir()
	runGit(t, repository, "init", "-q")
	runGit(t, repository, "config", "user.email", "boatstack@example.invalid")
	runGit(t, repository, "config", "user.name", "Boatstack Test")
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", "README.md")
	runGit(t, repository, "commit", "-q", "-m", "fixture")

	resolver, err := NewResolver(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := resolver.ResolveInvocation(context.Background(), repository, "cli", "runtime-binding")
	if err != nil {
		t.Fatal(err)
	}
	layout, _, err := resolver.ResolveLayout(context.Background(), invocation)
	if err != nil {
		t.Fatal(err)
	}
	state := durable.Default(invocation, time.Unix(100, 0).UTC())
	state.Runtime = model.RuntimeVerified
	state.RuntimePath = invocation.RuntimePath
	state.RuntimeFingerprint = invocation.RuntimeFingerprint
	state.RuntimeSource = "fixture-revision"
	raw, err := durable.EncodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(layout.StatePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.StatePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	observer, err := NewObserver(resolver, observerClock{now: time.Unix(200, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	observed, err := observer.Observe(context.Background(), ports.ObservationRequest{Invocation: invocation})
	if err != nil {
		t.Fatal(err)
	}
	if observed.Runtime.Value != model.RuntimeVerified {
		t.Fatalf("matching executing runtime observed as %s", observed.Runtime.Value)
	}

	state.RuntimePath = filepath.Join(t.TempDir(), "stale-runtime")
	if err := os.WriteFile(state.RuntimePath, mustRead(t, invocation.RuntimePath), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err = durable.EncodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.StatePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	observed, err = observer.Observe(context.Background(), ports.ObservationRequest{Invocation: invocation})
	if err != nil {
		t.Fatal(err)
	}
	if observed.Runtime.Value != model.RuntimeWrongSource {
		t.Fatalf("alternate runtime path observed as %s, want %s", observed.Runtime.Value, model.RuntimeWrongSource)
	}
}

func TestDoubleStarMatchesRootAndNestedPaths(t *testing.T) {
	for _, test := range []struct {
		pattern string
		name    string
		want    bool
	}{
		{pattern: "**/*.go", name: "main.go", want: true},
		{pattern: "**/*.go", name: "internal/kernel/main.go", want: true},
		{pattern: "migrations/**", name: "migrations/001.sql", want: true},
		{pattern: "migrations/**", name: "docs/migrations/001.sql", want: false},
	} {
		got, err := doublestarMatch(test.pattern, test.name)
		if err != nil {
			t.Fatalf("match %q against %q: %v", test.pattern, test.name, err)
		}
		if got != test.want {
			t.Fatalf("match %q against %q = %v, want %v", test.pattern, test.name, got, test.want)
		}
	}
}

func TestObserverDerivesHighRiskChangeFromCommittedAndWorkingTreePaths(t *testing.T) {
	repository := t.TempDir()
	runGit(t, repository, "init", "-q")
	runGit(t, repository, "config", "user.email", "boatstack@example.invalid")
	runGit(t, repository, "config", "user.name", "Boatstack Test")
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", "README.md")
	runGit(t, repository, "commit", "-q", "-m", "fixture")
	runGit(t, repository, "branch", "-M", "main")
	runGit(t, repository, "checkout", "-q", "-b", "feature")
	if err := os.MkdirAll(filepath.Join(repository, "migrations"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "migrations", "001.sql"), []byte("select 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", "migrations/001.sql")
	runGit(t, repository, "commit", "-q", "-m", "migration")

	resolver, err := NewResolver(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	observer, err := NewObserver(resolver, observerClock{now: time.Unix(200, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	highRisk, err := observer.highRiskChange(context.Background(), repository, "main", []string{"migrations/**"})
	if err != nil {
		t.Fatal(err)
	}
	if !highRisk {
		t.Fatal("committed high-risk path was not derived")
	}

	if err := os.MkdirAll(filepath.Join(repository, "billing"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "billing", "rate plan.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	highRisk, err = observer.highRiskChange(context.Background(), repository, "main", []string{"billing/**"})
	if err != nil {
		t.Fatal(err)
	}
	if !highRisk {
		t.Fatal("untracked high-risk path containing spaces was not derived")
	}
}

func TestProductFingerprintExcludesGeneratedProofButIncludesConfiguration(t *testing.T) {
	status := strings.Join([]string{
		"?? .boatstack/evidence/delivery/build.json",
		" M .boatstack/project.json",
		" M src/main.go",
		"",
	}, "\x00")
	canonical := canonicalProductStatus(status)
	if strings.Contains(canonical, ".boatstack/evidence/") {
		t.Fatalf("generated evidence remained in product fingerprint: %q", canonical)
	}
	for _, required := range []string{".boatstack/project.json", "src/main.go"} {
		if !strings.Contains(canonical, required) {
			t.Fatalf("product fingerprint omitted %s: %q", required, canonical)
		}
	}
}

func TestRecoveryAttemptsExhaustToEscalationOnly(t *testing.T) {
	// control-law: recovery-retry-budget-is-derived-and-finite-across-restarts
	root := t.TempDir()
	originalID := "adm-interrupted"
	pending := map[string]any{
		"schema_version":   2,
		"transition_id":    "plan.create",
		"transition_class": "owned-local",
		"status":           "recovery-required",
		"reason":           "simulated interruption",
		"admission": map[string]any{
			"id":           originalID,
			"source_phase": "ACTIVE",
			"invocation":   map[string]any{"correlation_id": "prior-process"},
		},
	}
	writeJSON := func(name string, value any) {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeJSON(originalID+".pending", pending)
	for attempt := 1; attempt <= 3; attempt++ {
		aborted := map[string]any{
			"schema_version":   2,
			"transition_id":    "recovery.rollback",
			"transition_class": "recovery",
			"status":           "aborted",
			"admission": map[string]any{
				"id":         "adm-recovery-attempt",
				"parameters": []map[string]string{{"name": "transaction_id", "value": originalID}},
			},
		}
		writeJSON("attempt-"+string(rune('0'+attempt))+".aborted", aborted)
		observed, err := pendingJournalEvidence(root, "new-process", time.Unix(500, 0).UTC())
		if err != nil {
			t.Fatal(err)
		}
		wantBudget := 3 - attempt
		if observed.Recovery.BudgetRemaining != wantBudget {
			t.Fatalf("attempt %d budget=%d, want %d", attempt, observed.Recovery.BudgetRemaining, wantBudget)
		}
		if attempt == 3 {
			if len(observed.Recovery.Permitted) != 1 || observed.Recovery.Permitted[0] != "recovery.escalate" {
				t.Fatalf("exhausted recovery permitted=%v, want escalation only", observed.Recovery.Permitted)
			}
		}
	}
}

func TestInterruptedRecoveryAttemptCollapsesToEscalatableTransactionGroup(t *testing.T) {
	// control-law: recovery-of-recovery-does-not-create-an-unselectable-conflict
	root := t.TempDir()
	write := func(name string, value any) {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	originalID := "adm-original"
	write(originalID+".pending", map[string]any{
		"schema_version": 2, "transition_id": "plan.create", "transition_class": "owned-local", "status": "recovery-required",
		"admission": map[string]any{"id": originalID, "source_phase": "ACTIVE", "invocation": map[string]any{"correlation_id": "old-process"}},
	})
	write("adm-nested.pending", map[string]any{
		"schema_version": 2, "transition_id": "recovery.rollback", "transition_class": "recovery", "status": "verifying",
		"admission": map[string]any{
			"id": "adm-nested", "source_phase": "RECOVERY", "invocation": map[string]any{"correlation_id": "old-process"},
			"parameters": []map[string]string{{"name": "transaction_id", "value": originalID}},
		},
	})
	observed, err := pendingJournalEvidence(root, "restart", time.Unix(600, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if observed.Conflicting || !observed.Found || observed.Recovery.TransactionID != originalID {
		t.Fatalf("nested recovery was not grouped: %#v", observed)
	}
	if observed.Recovery.BudgetRemaining != 2 || len(observed.Recovery.Permitted) != 1 || observed.Recovery.Permitted[0] != "recovery.escalate" {
		t.Fatalf("nested recovery contract=%#v, want budget 2 escalation-only", observed.Recovery)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
