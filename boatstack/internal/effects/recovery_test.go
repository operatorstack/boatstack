package effects

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/flow/standard"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/engine"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/plant"
	"github.com/operatorstack/boatstack/boatstack/internal/testprogram"
)

type recoveryClock struct{ value time.Time }

const testProgramFingerprint = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func testGoalContracts() catalog.GoalContracts {
	manifest, err := standard.Definition().FlowManifest(context.Background())
	if err != nil {
		panic(err)
	}
	contracts, err := catalog.NewGoalContracts(manifest.GoalContracts, nil)
	if err != nil {
		panic(err)
	}
	return contracts
}

func (c recoveryClock) Now() time.Time { return c.value }

func recoveryRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	for _, command := range [][]string{
		{"git", "init", "-q"},
		{"git", "config", "user.email", "boatstack@example.invalid"},
		{"git", "config", "user.name", "Boatstack Test"},
	} {
		cmd := exec.Command(command[0], command[1:]...)
		cmd.Dir = repository
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", command, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("recovery\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, command := range [][]string{{"git", "add", "README.md"}, {"git", "commit", "-q", "-m", "fixture"}} {
		cmd := exec.Command(command[0], command[1:]...)
		cmd.Dir = repository
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", command, err, output)
		}
	}
	return repository
}

func TestRestartRecoveryRollsBackExactPriorBytesAndArchivesJournal(t *testing.T) {
	// control-law: interrupted-local-effect-has-restart-safe-exact-rollback
	ctx := context.Background()
	clock := recoveryClock{value: time.Unix(3000, 0).UTC()}
	repository := recoveryRepository(t)
	resolver, err := plant.NewResolver(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	originalInvocation, err := resolver.ResolveInvocation(ctx, repository, "cli", "before-restart")
	if err != nil {
		t.Fatal(err)
	}
	observer, _ := plant.NewObserver(resolver, clock)
	initialObservation, err := observer.Observe(ctx, ports.ObservationRequest{Invocation: originalInvocation})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := model.CanonicalizeForProgram(initialObservation, testProgramFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	goal := model.Goal{ID: "goal-recovery", Kind: model.GoalVerified, DeliveryID: "delivery-recovery"}
	authority := protocol.AuthorityBundle{Receipts: []protocol.AuthorityReceipt{{
		ID: "recovery-human", Class: catalog.AuthorityHuman, Subject: "fixture", Fingerprint: "recovery-human-fingerprint",
		IssuedAt: clock.Now().Add(-time.Minute), ExpiresAt: clock.Now().Add(time.Hour),
	}}}
	transition, _ := testprogram.StandardRegistry().Lookup("installation.initialize")
	executable, _ := os.Executable()
	executable, _ = filepath.Abs(executable)
	executable, _ = filepath.EvalSymlinks(executable)
	runtimeRaw, _ := os.ReadFile(executable)
	configPath := filepath.Join(t.TempDir(), "project.json")
	configRaw := []byte("{\"schema_version\":2,\"project\":{\"name\":\"recovery\",\"default_branch\":\"main\",\"commands\":{}},\"policy\":{\"plan_approval\":\"human\",\"visual_evidence\":\"optional\"},\"hosts\":[\"cli\"]}\n")
	if err := os.WriteFile(configPath, configRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	_, configFingerprint, err := protocol.ProjectConfigFingerprint(configRaw)
	if err != nil {
		t.Fatal(err)
	}
	parameters := protocol.Parameters{
		{Name: "source_revision", Value: "recovery-fixture"}, {Name: "runtime_path", Value: executable}, {Name: "runtime_sha256", Value: sha256Bytes(runtimeRaw)},
		{Name: "config_path", Value: configPath}, {Name: "config_sha256", Value: configFingerprint},
	}
	admission, err := protocol.NewAdmission(initial, goal, transition, authority, parameters, clock.Now(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	journalBeforeRestart, _ := NewJournal(resolver, clock)
	driver, _ := NewDriver(resolver, clock, NewNativeBoundary())
	if err := journalBeforeRestart.Begin(ctx, admission, transition); err != nil {
		t.Fatal(err)
	}
	if err := journalBeforeRestart.Mark(ctx, admission.ID, "executing"); err != nil {
		t.Fatal(err)
	}
	prepared, err := driver.Prepare(ctx, admission, transition)
	if err != nil {
		t.Fatal(err)
	}
	if err := journalBeforeRestart.Stage(ctx, admission.ID, prepared.Manifest()); err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if err := journalBeforeRestart.RequireRecovery(ctx, admission.ID, "simulated process loss after effect"); err != nil {
		t.Fatal(err)
	}

	restartedInvocation, err := resolver.ResolveInvocation(ctx, repository, "cli", "after-restart")
	if err != nil {
		t.Fatal(err)
	}
	recoveryObservation, err := observer.Observe(ctx, ports.ObservationRequest{Invocation: restartedInvocation})
	if err != nil {
		t.Fatal(err)
	}
	recoverySnapshot, err := model.CanonicalizeForProgram(recoveryObservation, testProgramFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if recoverySnapshot.Phase.Value != model.PhaseRecovery || recoverySnapshot.RecoveryInfo.Value.TransactionID != admission.ID {
		t.Fatalf("restart did not expose exact recovery identity: %#v", recoverySnapshot)
	}

	locker, _ := NewLocker(resolver)
	journalAfterRestart, _ := NewJournal(resolver, clock)
	receipts, _ := NewReceiptStore(resolver, clock)
	restartedEngine, err := engine.New(testprogram.StandardRegistry(), testGoalContracts(), testProgramFingerprint, observer, clock, locker, journalAfterRestart, driver, receipts)
	if err != nil {
		t.Fatal(err)
	}
	result, err := restartedEngine.Apply(ctx, engine.ApplyRequest{
		ResolveRequest: engine.ResolveRequest{Invocation: restartedInvocation, Goal: goal, Authority: authority, Requested: "recovery.rollback"},
		FlowID:         "flow-recovery", Parameters: protocol.Parameters{{Name: "transaction_id", Value: admission.ID}}, AdmissionLifetime: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Target.Phase.Value != model.PhaseDormant || result.Target.Recovery.Value != model.RecoveryNone || result.Target.Goal.Status != model.FactAbsent ||
		result.Receipt.ID == "" || result.Receipt.GoalStatus != model.FactAbsent || result.Receipt.GoalID != "" {
		t.Fatalf("rollback target=%#v receipt=%q", result.Target, result.Receipt.ID)
	}
	layout, _, _ := resolver.ResolveLayout(ctx, restartedInvocation)
	pending := filepath.Join(layout.JournalRoot, admission.ID+".pending")
	recovered := filepath.Join(layout.JournalRoot, admission.ID+".recovered")
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Fatalf("interrupted journal remains pending: %v", err)
	}
	if _, err := os.Stat(recovered); err != nil {
		t.Fatalf("recovered journal missing: %v", err)
	}
	if _, err := os.Stat(layout.StatePath); !os.IsNotExist(err) {
		t.Fatalf("rollback did not restore absent state file: %v", err)
	}
}
