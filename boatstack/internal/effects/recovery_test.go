package effects

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/flow/standard"
	"github.com/operatorstack/boatstack/boatstack/internal/buildinfo"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/engine"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/plant"
	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/testprogram"
)

type recoveryClock struct{ value time.Time }

const testProgramFingerprint = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func testGoalContracts() catalog.GoalContracts {
	manifest, err := standard.Definition().RuntimeManifest(context.Background())
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

func TestRestartRecoveryRestoresPriorStateAndCommitsRecoveryRevision(t *testing.T) {
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
	home := t.TempDir()
	t.Setenv(boatstackruntime.HomeEnvironment, home)
	runtimeIdentity := boatstackruntime.Identity{Version: buildinfo.Version, SHA256: sha256Bytes(runtimeRaw), SourceRevision: "recovery-fixture"}
	if _, err := boatstackruntime.InstallExecutable(executable, home, runtimeIdentity); err != nil {
		t.Fatal(err)
	}
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
		{Name: "source_revision", Value: "recovery-fixture"}, {Name: "runtime_version", Value: runtimeIdentity.Version}, {Name: "runtime_sha256", Value: sha256Bytes(runtimeRaw)},
		{Name: "config_path", Value: configPath}, {Name: "config_sha256", Value: configFingerprint},
	}
	capabilities, err := protocol.ProjectCapabilities(initial, transition, authority, clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	prescription, err := protocol.NewPrescription(initial, transition, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := protocol.NewAdmission(initial, goal, transition, prescription, authority, parameters, clock.Now(), time.Minute)
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
	recoveryRequest := engine.ApplyRequest{
		ResolveRequest: engine.ResolveRequest{Invocation: restartedInvocation, Goal: goal, Authority: authority, Requested: "recovery.rollback"},
		FlowID:         "flow-recovery", Parameters: protocol.Parameters{{Name: "transaction_id", Value: admission.ID}}, AdmissionLifetime: time.Minute,
	}
	recoveryResolve := recoveryRequest.ResolveRequest
	recoveryResolve.Parameters = recoveryRequest.Parameters
	resolvedRecovery, err := restartedEngine.Resolve(ctx, recoveryResolve)
	if err != nil {
		t.Fatal(err)
	}
	recoveryRequest.Prescription = resolvedRecovery.Prescription
	result, err := restartedEngine.Apply(ctx, recoveryRequest)
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.PriorStateRevision != resolvedRecovery.Prescription.ExpectedStateRevision ||
		result.Receipt.ResultingStateRevision != resolvedRecovery.Prescription.ExpectedStateRevision+1 {
		t.Fatalf("recovery receipt did not advance exactly once: %#v", result.Receipt)
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
	stateRaw, err := os.ReadFile(layout.StatePath)
	if err != nil {
		t.Fatalf("rollback did not commit its recovery revision: %v", err)
	}
	recoveredState, err := durable.DecodeState(stateRaw)
	if err != nil || recoveredState.Revision != result.Receipt.ResultingStateRevision {
		t.Fatalf("rollback state revision is not receipt-bound: state=%#v err=%v receipt=%#v", recoveredState, err, result.Receipt)
	}
	if recoveredState.Goal.Validate() == nil || recoveredState.Runtime != model.RuntimeAbsent || recoveredState.Configuration != model.ConfigurationUnsupported {
		t.Fatalf("rollback created product intent or retained initialized state: %#v", recoveredState)
	}
	if _, err := os.Stat(boatstackruntime.PinPath(repository)); !os.IsNotExist(err) {
		t.Fatalf("rollback did not restore the absent repository runtime pin: %v", err)
	}
}
