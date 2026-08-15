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

	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

type observerClock struct{ now time.Time }

func (c observerClock) Now() time.Time { return c.now }

func TestObserverValidatesAdmittedPlanningPackageWithoutPrematurePlanPromotion(t *testing.T) {
	// control-law: an admitted package is verified from its exact manifest while the canonical approved plan remains absent
	repository := t.TempDir()
	deliveryID := "delivery"
	root := filepath.Join(repository, ".boatstack", "planning-packages", deliveryID)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	plan := []byte("# Proposed plan\n")
	planFingerprint := hashBytes(plan)
	manifest := observedPlanningPackageManifest{
		SchemaVersion: 1, DeliveryID: deliveryID, WorkRequestFingerprint: strings.Repeat("a", 64), WorkResultFingerprint: strings.Repeat("b", 64),
		PlanFingerprint: planFingerprint, Outputs: []observedPlanningPackageOutput{{ID: "plan", Path: "plan.md", MediaType: "text/markdown", SHA256: planFingerprint, Size: int64(len(plan))}},
	}
	identityRaw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	manifest.Fingerprint = hashBytes(append(identityRaw, '\n'))
	manifestRaw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	manifestRaw = append(manifestRaw, '\n')
	if err := os.WriteFile(filepath.Join(root, "plan.md"), plan, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), manifestRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	state := durable.State{
		Plan: model.PlanValid, PlanFingerprint: planFingerprint, PlanningPackageFingerprint: manifest.Fingerprint,
		Objective: model.Objective{ID: "objective", TargetID: model.ObjectiveOpenPR, DeliveryID: deliveryID},
	}
	evidence, valid, err := observePlanningPackage(ports.ControllerLayout{RepositoryRoot: repository}, state, time.Unix(100, 0).UTC())
	if err != nil || !valid || len(evidence) != 2 {
		t.Fatalf("planning package observation valid=%t evidence=%#v err=%v", valid, evidence, err)
	}
	if _, err := os.Stat(filepath.Join(repository, ".boatstack", "plans", deliveryID+".source")); !os.IsNotExist(err) {
		t.Fatalf("admission prematurely created a canonical plan: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "plan.md"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, valid, err := observePlanningPackage(ports.ControllerLayout{RepositoryRoot: repository}, state, time.Unix(101, 0).UTC()); err != nil || valid {
		t.Fatalf("tampered planning package valid=%t err=%v", valid, err)
	}
}

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
	home := t.TempDir()
	t.Setenv(boatstackruntime.HomeEnvironment, home)
	identity := boatstackruntime.Identity{Version: resolver.runtimeVersion, SHA256: resolver.runtimeFingerprint, SourceRevision: "fixture-revision"}
	installed, err := boatstackruntime.InstallExecutable(resolver.runtimePath, home, identity)
	if err != nil {
		t.Fatal(err)
	}
	resolver.runtimePath = installed
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
	state.RuntimeVersion = invocation.RuntimeVersion
	state.RuntimeFingerprint = invocation.RuntimeFingerprint
	state.RuntimeSource = "fixture-revision"
	state.ProgramFingerprint = strings.Repeat("a", 64)
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
	pinRaw, err := boatstackruntime.EncodePin(boatstackruntime.NewPin(identity, state.ProgramFingerprint, durable.StateSchemaVersion))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(boatstackruntime.PinPath(repository)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(boatstackruntime.PinPath(repository), pinRaw, 0o644); err != nil {
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

	observer.resolver.runtimeFingerprint = strings.Repeat("b", 64)
	observed, err = observer.Observe(context.Background(), ports.ObservationRequest{Invocation: invocation})
	if err != nil {
		t.Fatal(err)
	}
	if observed.Runtime.Value != model.RuntimeWrongSource {
		t.Fatalf("different runtime identity observed as %s, want %s", observed.Runtime.Value, model.RuntimeWrongSource)
	}
}

func TestObserverAdmitsExactCommittedPinOnlyForAbsentState(t *testing.T) {
	// control-law: exact-committed-pin-is-bootstrap-evidence-not-controller-state
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
	home := t.TempDir()
	t.Setenv(boatstackruntime.HomeEnvironment, home)
	identity := boatstackruntime.Identity{Version: resolver.runtimeVersion, SHA256: resolver.runtimeFingerprint, SourceRevision: "fixture-revision"}
	installed, err := boatstackruntime.InstallExecutable(resolver.runtimePath, home, identity)
	if err != nil {
		t.Fatal(err)
	}
	resolver.runtimePath = installed
	invocation, err := resolver.ResolveInvocation(context.Background(), repository, "cli", "fresh-clone")
	if err != nil {
		t.Fatal(err)
	}
	pinRaw, err := boatstackruntime.EncodePin(boatstackruntime.NewPin(identity, strings.Repeat("a", 64), durable.StateSchemaVersion))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(boatstackruntime.PinPath(repository)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(boatstackruntime.PinPath(repository), pinRaw, 0o644); err != nil {
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
	if observed.Runtime.Value != model.RuntimeAbsent {
		t.Fatalf("exact pin with absent controller state observed as %s, want absent", observed.Runtime.Value)
	}
	if err := os.Remove(installed); err != nil {
		t.Fatal(err)
	}
	observed, err = observer.Observe(context.Background(), ports.ObservationRequest{Invocation: invocation})
	if err != nil {
		t.Fatal(err)
	}
	if observed.Runtime.Value != model.RuntimeStale {
		t.Fatalf("pin with missing immutable runtime observed as %s, want stale", observed.Runtime.Value)
	}

	conflicting := boatstackruntime.NewPin(identity, strings.Repeat("a", 64), durable.StateSchemaVersion)
	conflicting.Version = "v-conflicting"
	conflictingRaw, err := boatstackruntime.EncodePin(conflicting)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(boatstackruntime.PinPath(repository), conflictingRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	observed, err = observer.Observe(context.Background(), ports.ObservationRequest{Invocation: invocation})
	if err != nil {
		t.Fatal(err)
	}
	if observed.Runtime.Value != model.RuntimeConflicting {
		t.Fatalf("candidate-mismatched pin observed as %s, want conflicting", observed.Runtime.Value)
	}
}

func TestDoubleStarMatchesRootAndNestedPaths(t *testing.T) {
	for _, test := range []struct {
		pattern string
		name    string
		want    bool
	}{
		{pattern: "**/*.go", name: "main.go", want: true},
		{pattern: "**/*.go", name: "internal/softwaredelivery/main.go", want: true},
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

func TestObserverMarksApprovalByteSubstitutionStale(t *testing.T) {
	// control-law: an approval remains authoritative only while its exact admitted bytes remain present.
	repository := t.TempDir()
	planPath := filepath.Join(repository, ".boatstack", "plans", "delivery.source")
	approvalPath := filepath.Join(repository, ".boatstack", "approvals", "delivery.json")
	if err := os.MkdirAll(filepath.Dir(planPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(approvalPath), 0o700); err != nil {
		t.Fatal(err)
	}
	planRaw := []byte("# Approved plan\n")
	approvalRaw := []byte(`{"schema_version":1,"delivery_id":"delivery","plan_fingerprint":"pending","actor":"reviewer","admission_id":"adm-1","approved_at":"2026-01-01T00:00:00Z"}`)
	if err := os.WriteFile(planPath, planRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	_, planFingerprint, _, err := fileEvidence(planPath, "plan", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	approvalRaw = []byte(`{"schema_version":1,"delivery_id":"delivery","plan_fingerprint":"` + planFingerprint + `","actor":"reviewer","admission_id":"adm-1","approved_at":"2026-01-01T00:00:00Z"}`)
	if err := os.WriteFile(approvalPath, approvalRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	_, approvalFingerprint, _, err := fileEvidence(approvalPath, "approval", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	state := durable.State{
		Plan: model.PlanApproved, Verification: model.VerificationCurrent, Terminal: model.TerminalNonterminal,
		Objective:       model.Objective{ID: "objective", TargetID: model.ObjectiveOpenPR, DeliveryID: "delivery"},
		PlanFingerprint: planFingerprint, ApprovalFingerprint: approvalFingerprint,
	}
	if err := os.WriteFile(approvalPath, []byte(`{"schema_version":1,"delivery_id":"delivery","plan_fingerprint":"`+planFingerprint+`","actor":"substitute","admission_id":"adm-1","approved_at":"2026-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, _, terminal, _, _, err := observeRepositoryArtifacts(ports.ControllerLayout{RepositoryRoot: repository}, state, time.Unix(2, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if plan != model.PlanStale || terminal != model.TerminalStale {
		t.Fatalf("substituted approval observed as plan=%s terminal=%s", plan, terminal)
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
		"schema_version":   protocol.JournalSchemaVersion,
		"transition_id":    "plan.create",
		"transition_class": "owned-local",
		"status":           "recovery-required",
		"reason":           "simulated interruption",
		"admission": map[string]any{
			"id":                           originalID,
			"expected_program_fingerprint": strings.Repeat("a", 64),
			"source_phase":                 "ACTIVE",
			"invocation":                   map[string]any{"correlation_id": "prior-process"},
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
			"schema_version":   protocol.JournalSchemaVersion,
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
		if observed.ProgramFingerprint != strings.Repeat("a", 64) {
			t.Fatalf("pending program fingerprint=%q", observed.ProgramFingerprint)
		}
		if attempt == 3 {
			if len(observed.Recovery.Permitted) != 1 || observed.Recovery.Permitted[0] != "recovery.escalate" {
				t.Fatalf("exhausted recovery permitted=%v, want escalation only", observed.Recovery.Permitted)
			}
		}
	}
}

func TestRecoveryWithoutStagedManifestCannotPrescribeResume(t *testing.T) {
	// control-law: recovery selection cannot promise a replay that prepare will reject
	permitted := recoveryContract("plan.create", false, false, 3)
	for _, transition := range permitted {
		if transition == "recovery.resume" {
			t.Fatalf("unstaged recovery permits resume: %v", permitted)
		}
	}
	if len(permitted) != 2 || permitted[0] != "recovery.rollback" || permitted[1] != "recovery.escalate" {
		t.Fatalf("unstaged recovery contract = %v", permitted)
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
		"schema_version": protocol.JournalSchemaVersion, "transition_id": "plan.create", "transition_class": "owned-local", "status": "recovery-required",
		"admission": map[string]any{"id": originalID, "expected_program_fingerprint": strings.Repeat("a", 64), "source_phase": "ACTIVE", "invocation": map[string]any{"correlation_id": "old-process"}},
	})
	write("adm-nested.pending", map[string]any{
		"schema_version": protocol.JournalSchemaVersion, "transition_id": "recovery.rollback", "transition_class": "recovery", "status": "verifying",
		"admission": map[string]any{
			"id": "adm-nested", "expected_program_fingerprint": strings.Repeat("a", 64), "source_phase": "RECOVERY", "invocation": map[string]any{"correlation_id": "old-process"},
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

func TestPublicationCandidateRequiresCurrentCommittedPreviewIdentity(t *testing.T) {
	repository := t.TempDir()
	layout := ports.ControllerLayout{RepositoryRoot: repository}
	state := durable.State{
		Publication: model.PublicationCandidate,
		Objective:   model.Objective{DeliveryID: "delivery"},
	}
	path := filepath.Join(repository, ".boatstack", "publication", "delivery.preview.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	oldPreview := []byte(`{"schema_version":1,"delivery_id":"delivery","source_revision":"head","worktree_fingerprint":"worktree"}`)
	if err := os.WriteFile(path, oldPreview, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := currentPublicationState(layout, state, "head", "worktree"); got != model.PublicationNone {
		t.Fatalf("old preview projected as %s", got)
	}
	currentPreview := []byte(`{"schema_version":2,"delivery_id":"delivery","source_revision":"head","worktree_fingerprint":"worktree"}`)
	if err := os.WriteFile(path, currentPreview, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := currentPublicationState(layout, state, "head", "worktree"); got != model.PublicationCandidate {
		t.Fatalf("current preview projected as %s", got)
	}
	if got := currentPublicationState(layout, state, "new-head", "worktree"); got != model.PublicationNone {
		t.Fatalf("stale source preview projected as %s", got)
	}
}
