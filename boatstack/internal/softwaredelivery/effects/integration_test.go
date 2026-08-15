package effects_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	boatstack "github.com/operatorstack/boatstack/boatstack"
	"github.com/operatorstack/boatstack/boatstack/core"
	"github.com/operatorstack/boatstack/boatstack/delivery"
	"github.com/operatorstack/boatstack/boatstack/extension/releasenote"
	"github.com/operatorstack/boatstack/boatstack/flow/standard"
	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/effects"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/engine"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/plant"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/supervisor"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
	"github.com/operatorstack/boatstack/boatstack/internal/testprogram"
)

type fixedClock struct{ value time.Time }

const testProgramFingerprint = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

var testProgramIdentity = protocol.ProgramIdentity{ID: "standard", Version: "test", Fingerprint: testProgramFingerprint}

func testObjectiveContracts() catalog.ObjectiveContracts {
	manifest, err := standard.Definition().RuntimeManifest(context.Background())
	if err != nil {
		panic(err)
	}
	contracts, err := catalog.NewObjectiveContracts(manifest.ObjectiveContracts, nil)
	if err != nil {
		panic(err)
	}
	return contracts
}

func testProgram() delivery.ControlProgram {
	program, err := delivery.Compile(context.Background(), delivery.CompileRequest{
		KernelVersion: boatstack.Version, Core: core.System(), Runtime: standard.Definition(),
	})
	if err != nil {
		panic(err)
	}
	return program
}

func prescribeEngine(t *testing.T, ctx context.Context, kernel engine.Engine, request engine.ApplyRequest) engine.ApplyRequest {
	t.Helper()
	request.ControlBundle = testControlBundle(t, request.Invocation.InvokingPath, request.Requested, request.Parameters)
	resolve := request.ResolveRequest
	resolve.Parameters = request.Parameters
	resolution, err := kernel.Resolve(ctx, resolve)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Decision.Kind != supervisor.DecisionPrescribed || resolution.Prescription.ID == "" {
		t.Fatalf("resolution did not produce an exact prescription: %#v", resolution.Decision)
	}
	request.Prescription = resolution.Prescription
	return request
}

func prescribeSurface(t *testing.T, ctx context.Context, kernel boatstack.DeliveryController, request surfaces.Request) surfaces.Request {
	t.Helper()
	request.ControlBundle = testControlBundle(t, request.Repository, request.TransitionID, request.Parameters)
	request.ControlBundleFingerprint = request.ControlBundle.Source.Fingerprint
	resolve := request
	resolve.Operation = surfaces.OperationResolve
	resolve.FlowID = ""
	resolve.Prescription = protocol.Prescription{}
	response, err := kernel.Handle(ctx, resolve)
	if err != nil {
		t.Fatal(err)
	}
	if response.Decision == nil || response.Decision.Kind != supervisor.DecisionPrescribed || response.Prescription == nil {
		t.Fatalf("resolution did not produce an exact prescription: %#v", response.Decision)
	}
	request.Prescription = *response.Prescription
	return request
}

func testControlBundle(t *testing.T, repository string, transitionID catalog.TransitionID, parameters protocol.Parameters) *boatstackruntime.ControlBundleContract {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repository, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{"README.md": raw}
	var sourcePin *boatstackruntime.Pin
	if pinRaw, readErr := os.ReadFile(boatstackruntime.PinPath(repository)); readErr == nil {
		files[".boatstack/runtime.json"] = pinRaw
		pin, decodeErr := boatstackruntime.DecodePin(pinRaw)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		sourcePin = &pin
	} else if !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
	source, err := boatstackruntime.NewControlBundleSnapshot(files)
	if err != nil {
		t.Fatal(err)
	}
	var target *boatstackruntime.ControlBundleSnapshot
	targetRevision := ""
	switch transitionID {
	case "workspace.cut":
		baseRef, _ := parameters.Get("base_ref")
		command := exec.Command("git", "rev-parse", "--verify", baseRef+"^{commit}")
		command.Dir = repository
		output, resolveErr := command.Output()
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		targetRevision = strings.TrimSpace(string(output))
		copy := source
		target = &copy
	case "workspace.cleanup", "workspace.reap", "workspace.reconcile":
		copy := source
		target = &copy
	}
	var targetPin *boatstackruntime.Pin
	if target != nil {
		targetPin = sourcePin
	}
	contract, err := boatstackruntime.NewControlBundleContractWithPins(source, target, targetRevision, sourcePin, targetPin)
	if err != nil {
		t.Fatal(err)
	}
	return &contract
}

func (c fixedClock) Now() time.Time { return c.value }

func installTestRuntime(t *testing.T, executable string, raw []byte) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(boatstackruntime.HomeEnvironment, home)
	identity := boatstackruntime.Identity{Version: boatstack.Version, SHA256: digestBytes(raw), SourceRevision: "fixture"}
	if _, err := boatstackruntime.InstallExecutable(executable, home, identity); err != nil {
		t.Fatal(err)
	}
	return identity.Version
}

func run(t *testing.T, directory, name string, arguments ...string) {
	t.Helper()
	command := exec.Command(name, arguments...)
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, arguments, err, output)
	}
}

func testRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	run(t, repository, "git", "init", "-q")
	run(t, repository, "git", "config", "user.email", "boatstack@example.invalid")
	run(t, repository, "git", "config", "user.name", "Boatstack Test")
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, repository, "git", "add", "README.md")
	run(t, repository, "git", "commit", "-q", "-m", "fixture")
	return repository
}

func TestStaleControlBundleStopsBeforeManagedStateOrRuntimePin(t *testing.T) {
	// control-law: control bundle mismatch is a pre-effect blocker
	ctx := context.Background()
	repository := testRepository(t)
	externalRoot := t.TempDir()
	kernel, err := boatstack.NewDeliveryController(externalRoot, testProgram())
	if err != nil {
		t.Fatal(err)
	}
	readme, err := os.ReadFile(filepath.Join(repository, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := boatstackruntime.NewControlBundleSnapshot(map[string][]byte{"README.md": readme})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := boatstackruntime.NewControlBundleContract(snapshot, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("changed after binding\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	executable, _ := os.Executable()
	executable, _ = filepath.Abs(executable)
	executable, _ = filepath.EvalSymlinks(executable)
	runtimeRaw, _ := os.ReadFile(executable)
	runtimeVersion := installTestRuntime(t, executable, runtimeRaw)
	configPath := filepath.Join(t.TempDir(), "project.json")
	configRaw := []byte("{\"schema_version\":2,\"project\":{\"name\":\"bundle\",\"default_branch\":\"main\",\"commands\":{}},\"policy\":{\"plan_approval\":\"human\",\"visual_evidence\":\"optional\"},\"hosts\":[\"cli\"]}\n")
	if err := os.WriteFile(configPath, configRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	request := surfaces.Request{
		SchemaVersion: surfaces.SchemaVersion, Operation: surfaces.OperationResolve, Repository: repository, Host: "cli", CorrelationID: "stale-bundle",
		FlowID: "flow-stale-bundle", TransitionID: "installation.initialize",
		Authority: protocol.AuthorityBundle{Receipts: []protocol.AuthorityReceipt{{ID: "human", Class: catalog.AuthorityHuman, Subject: "operator", Fingerprint: "human-proof", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}}},
		Parameters: protocol.Parameters{
			{Name: "source_revision", Value: "fixture"}, {Name: "runtime_version", Value: runtimeVersion}, {Name: "runtime_sha256", Value: digestBytes(runtimeRaw)},
			{Name: "config_path", Value: configPath}, {Name: "config_sha256", Value: configFingerprint(t, configRaw)},
		},
		ControlBundle: &contract, ControlBundleFingerprint: contract.Source.Fingerprint,
	}
	response, handleErr := kernel.Handle(ctx, request)
	if handleErr != nil {
		t.Fatal(handleErr)
	}
	if response.Decision == nil || response.Decision.Kind != supervisor.DecisionUnresolved || !strings.Contains(response.Decision.Reason, "CONTROL_BUNDLE_STALE") {
		t.Fatalf("stale bundle decision = %#v", response.Decision)
	}
	resolver, err := plant.NewResolver(externalRoot)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := resolver.ResolveInvocation(ctx, repository, "cli", "stale-bundle-check")
	if err != nil {
		t.Fatal(err)
	}
	layout, _, err := resolver.ResolveLayout(ctx, invocation)
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(layout.StatePath); !os.IsNotExist(statErr) {
		t.Fatalf("stale bundle created managed state: %v", statErr)
	}
	if _, statErr := os.Stat(boatstackruntime.PinPath(repository)); !os.IsNotExist(statErr) {
		t.Fatalf("stale bundle created runtime pin: %v", statErr)
	}
}

func TestConcreteBoundaryAppliesAndReceiptsOneTransition(t *testing.T) {
	// control-law: request-to-boundary-to-effect-to-verified-receipt
	ctx := context.Background()
	repository := testRepository(t)
	clock := fixedClock{value: time.Unix(1000, 0).UTC()}
	resolver, err := plant.NewResolver(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := resolver.ResolveInvocation(ctx, repository, "cli", "correlation-1")
	if err != nil {
		t.Fatal(err)
	}
	observer, err := plant.NewObserver(resolver, clock)
	if err != nil {
		t.Fatal(err)
	}
	locker, err := effects.NewLocker(resolver)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := effects.NewJournal(resolver, clock)
	if err != nil {
		t.Fatal(err)
	}
	receipts, err := effects.NewReceiptStore(resolver, clock)
	if err != nil {
		t.Fatal(err)
	}
	driver, err := effects.NewDriver(resolver, clock, effects.NewNativeBoundary())
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := engine.New(testprogram.StandardRegistry(), testObjectiveContracts(), testProgramIdentity, observer, clock, locker, journal, driver, receipts)
	if err != nil {
		t.Fatal(err)
	}
	objective := model.Objective{ID: "objective-1", TargetID: model.ObjectiveVerified, DeliveryID: "delivery-1"}
	authority := protocol.AuthorityBundle{Receipts: []protocol.AuthorityReceipt{{
		ID: "authority-1", Class: catalog.AuthorityHuman, Subject: invocation.RepositoryID, Fingerprint: "human-fingerprint",
		IssuedAt: clock.Now().Add(-time.Minute), ExpiresAt: clock.Now().Add(time.Hour),
	}}}
	request := engine.ApplyRequest{
		ResolveRequest: engine.ResolveRequest{Invocation: invocation, Objective: objective, Authority: authority, Requested: "repository.attach"},
		FlowID:         "flow-1", Parameters: protocol.Parameters{{Name: "topology", Value: string(model.TopologyDetached)}, {Name: "config_authority", Value: "repository"}}, AdmissionLifetime: time.Minute,
	}
	result, err := kernel.Apply(ctx, prescribeEngine(t, ctx, kernel, request))
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.ID == "" || result.Target.Phase.Value != model.PhaseObserved {
		t.Fatalf("result = %#v", result)
	}
	layout, _, err := resolver.ResolveLayout(ctx, invocation)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{layout.StatePath, layout.ReceiptPath, layout.EventPath} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected Boatstack artifact %s: %v", path, err)
		}
	}
}

func TestDeclaredStateEffectAppliesAndReceiptsWithoutTransitionDispatch(t *testing.T) {
	// control-law: a valid repository-authored state declaration does not require a Go reducer case
	ctx := context.Background()
	repository := testRepository(t)
	clock := fixedClock{value: time.Unix(1100, 0).UTC()}
	resolver, err := plant.NewResolver(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := resolver.ResolveInvocation(ctx, repository, "cli", "declared-state")
	if err != nil {
		t.Fatal(err)
	}
	base := testprogram.StandardRegistry()
	transitions := base.All()
	synthetic, declared := base.Lookup("fixture.declared-state")
	if !declared {
		var ok bool
		synthetic, ok = base.Lookup("invocation.rebind")
		if !ok {
			t.Fatal("missing invocation.rebind template")
		}
		synthetic.ID = "fixture.declared-state"
		synthetic.Effect = catalog.EffectID(synthetic.ID)
		synthetic.LocalEffects = []catalog.EffectID{synthetic.Effect}
		synthetic.Prescription.Operation = string(synthetic.ID)
		synthetic.Authority = []catalog.AuthorityClass{catalog.AuthorityHuman}
		synthetic.StateEffect = catalog.StateEffect{Kind: catalog.StateEffectAssignments, Assignments: []catalog.StateAssignment{
			{Facet: "phase", Value: pointer(string(model.PhaseObserved))},
		}}
		transitions = append(transitions, synthetic)
	}
	registry, err := catalog.New(transitions)
	if err != nil {
		t.Fatal(err)
	}
	observer, _ := plant.NewObserver(resolver, clock)
	locker, _ := effects.NewLocker(resolver)
	journal, _ := effects.NewJournal(resolver, clock)
	receipts, _ := effects.NewReceiptStore(resolver, clock)
	driver, _ := effects.NewDriver(resolver, clock, effects.NewNativeBoundary())
	kernel, err := engine.New(registry, testObjectiveContracts(), testProgramIdentity, observer, clock, locker, journal, driver, receipts)
	if err != nil {
		t.Fatal(err)
	}
	objective := model.Objective{ID: "declared-state-objective", TargetID: model.ObjectiveVerified, DeliveryID: "declared-state-delivery"}
	human := protocol.AuthorityBundle{Receipts: []protocol.AuthorityReceipt{{
		ID: "declared-state-human", Class: catalog.AuthorityHuman, Subject: invocation.RepositoryID, Fingerprint: "explicit-human",
		IssuedAt: clock.Now().Add(-time.Minute), ExpiresAt: clock.Now().Add(time.Hour),
	}}}
	attach := engine.ApplyRequest{
		ResolveRequest: engine.ResolveRequest{Invocation: invocation, Objective: objective, Authority: human, Requested: "repository.attach"},
		FlowID:         "declared-state-flow", Parameters: protocol.Parameters{{Name: "topology", Value: string(model.TopologyDetached)}, {Name: "config_authority", Value: "repository"}}, AdmissionLifetime: time.Minute,
	}
	if _, err := kernel.Apply(ctx, prescribeEngine(t, ctx, kernel, attach)); err != nil {
		t.Fatal(err)
	}
	invocation, err = resolver.ResolveInvocation(ctx, repository, "cli", "declared-state-apply")
	if err != nil {
		t.Fatal(err)
	}
	request := engine.ApplyRequest{
		ResolveRequest: engine.ResolveRequest{Invocation: invocation, Objective: objective, Authority: human, Requested: synthetic.ID},
		FlowID:         "declared-state-flow", AdmissionLifetime: time.Minute,
	}
	result, err := kernel.Apply(ctx, prescribeEngine(t, ctx, kernel, request))
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.TransitionID != synthetic.ID || result.Target.Phase.Value != model.PhaseObserved {
		t.Fatalf("declared transition result = %#v", result)
	}
}

func pointer(value string) *string { return &value }

func TestExternalConfigurationAuthorityTransfersAcrossAttachAndDetach(t *testing.T) {
	// control-law: detached-config-authority-selects-the-real-reader-and-writer
	ctx := context.Background()
	repository := testRepository(t)
	externalRoot := t.TempDir()
	kernel, err := boatstack.NewDeliveryController(externalRoot, testProgram())
	if err != nil {
		t.Fatal(err)
	}
	objective := model.Objective{ID: "external-config-objective", TargetID: model.ObjectiveApprovedPlan, DeliveryID: "external-config"}
	now := time.Now().UTC()
	human := protocol.AuthorityBundle{Receipts: []protocol.AuthorityReceipt{{
		ID: "external-config-human", Class: catalog.AuthorityHuman, Subject: "integration", Fingerprint: "explicit-human",
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}}}
	autonomy := protocol.AuthorityBundle{Receipts: []protocol.AuthorityReceipt{{
		ID: "external-config-autonomy", Class: catalog.AuthorityAutonomy, Subject: "integration", Fingerprint: "explicit-delegation",
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}}}
	apply := func(id catalog.TransitionID, authority protocol.AuthorityBundle, repositoryAuthority bool, parameters protocol.Parameters) surfaces.Response {
		t.Helper()
		request := surfaces.Request{
			SchemaVersion: surfaces.SchemaVersion, Operation: surfaces.OperationApply, Repository: repository, Host: "cli",
			CorrelationID: "external-config-" + string(id), FlowID: "flow-external-config", Objective: objective, TransitionID: id,
			Authority: authority, RepositoryAuthority: repositoryAuthority, Parameters: parameters,
		}
		response, handleErr := kernel.Handle(ctx, prescribeSurface(t, ctx, kernel, request))
		if handleErr != nil {
			t.Fatalf("apply %s: %v", id, handleErr)
		}
		return response
	}
	executable, _ := os.Executable()
	executable, _ = filepath.Abs(executable)
	executable, _ = filepath.EvalSymlinks(executable)
	runtimeRaw, _ := os.ReadFile(executable)
	runtimeVersion := installTestRuntime(t, executable, runtimeRaw)
	initialConfig := []byte("{\"schema_version\":2,\"project\":{\"name\":\"external-initial\",\"default_branch\":\"main\",\"commands\":{}},\"policy\":{\"plan_approval\":\"human\",\"visual_evidence\":\"optional\"},\"hosts\":[\"cli\"]}\n")
	initialPath := filepath.Join(t.TempDir(), "initial.json")
	if err := os.WriteFile(initialPath, initialConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	// A continuation may request repository authority before fresh-state
	// initialization. The controller must preserve delegated autonomy without
	// fabricating repository authority, then derive repository authority from
	// the configuration evidence committed by this transition on later steps.
	apply("installation.initialize", autonomy, true, protocol.Parameters{
		{Name: "source_revision", Value: "external-config-fixture"}, {Name: "runtime_version", Value: runtimeVersion}, {Name: "runtime_sha256", Value: digestBytes(runtimeRaw)},
		{Name: "config_path", Value: initialPath}, {Name: "config_sha256", Value: configFingerprint(t, initialConfig)},
	})
	apply("objective.bind", human, false, protocol.Parameters{{Name: "target_id", Value: string(objective.TargetID)}, {Name: "delivery_id", Value: objective.DeliveryID}})
	apply("repository.attach", human, false, protocol.Parameters{{Name: "topology", Value: "detached"}, {Name: "config_authority", Value: "external"}})
	resolver, err := plant.NewResolver(externalRoot)
	if err != nil {
		t.Fatal(err)
	}
	detachedInvocation, err := resolver.ResolveInvocation(ctx, repository, "cli", "inspect-detached")
	if err != nil {
		t.Fatal(err)
	}
	detachedLayout, _, err := resolver.ResolveLayout(ctx, detachedInvocation)
	if err != nil {
		t.Fatal(err)
	}
	if detachedLayout.ConfigAuthority != "external" || detachedLayout.ConfigPath == filepath.Join(repository, ".boatstack", "project.json") {
		t.Fatalf("detached layout did not select external config: %#v", detachedLayout)
	}
	apply("engagement.begin", protocol.AuthorityBundle{}, true, nil)
	updatedConfig := []byte("{\"schema_version\":2,\"project\":{\"name\":\"external-updated\",\"default_branch\":\"main\",\"commands\":{}},\"policy\":{\"plan_approval\":\"human\",\"visual_evidence\":\"optional\"},\"hosts\":[\"cli\"]}\n")
	updatedPath := filepath.Join(t.TempDir(), "updated.json")
	if err := os.WriteFile(updatedPath, updatedConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	apply("configuration.mutate", human, false, protocol.Parameters{{Name: "config_path", Value: updatedPath}, {Name: "config_sha256", Value: configFingerprint(t, updatedConfig)}})
	repositoryConfigPath := filepath.Join(repository, ".boatstack", "project.json")
	if raw, err := os.ReadFile(repositoryConfigPath); err != nil || string(raw) != string(initialConfig) {
		t.Fatalf("external mutation leaked into repository authority before detach: err=%v value=%q", err, raw)
	}
	apply("repository.detach", human, false, nil)
	if raw, err := os.ReadFile(repositoryConfigPath); err != nil || string(raw) != string(updatedConfig) {
		t.Fatalf("detach did not transfer external config authority: err=%v value=%q", err, raw)
	}
	embeddedInvocation, err := resolver.ResolveInvocation(ctx, repository, "cli", "inspect-embedded")
	if err != nil {
		t.Fatal(err)
	}
	embeddedLayout, _, err := resolver.ResolveLayout(ctx, embeddedInvocation)
	if err != nil {
		t.Fatal(err)
	}
	canonicalRepositoryConfig, _ := filepath.EvalSymlinks(repositoryConfigPath)
	if embeddedLayout.ConfigAuthority != "repository" || embeddedLayout.ConfigPath != canonicalRepositoryConfig {
		t.Fatalf("detach did not restore repository config authority: %#v", embeddedLayout)
	}
}

func TestProgramDriftRequiresAtomicInstallationReconciliation(t *testing.T) {
	// control-law: frozen-program-and-runtime-change-only-through-one-exact-human-admission
	ctx := context.Background()
	repository := testRepository(t)
	externalRoot := t.TempDir()
	oldProgram, err := delivery.Compile(ctx, delivery.CompileRequest{
		KernelVersion: boatstack.Version, Core: core.System(), Runtime: standard.Definition(), Extensions: []delivery.Extension{releasenote.Definition()},
	})
	if err != nil {
		t.Fatal(err)
	}
	oldKernel, err := boatstack.NewDeliveryController(externalRoot, oldProgram)
	if err != nil {
		t.Fatal(err)
	}
	objective := model.Objective{ID: "program-drift", TargetID: model.ObjectiveApprovedPlan, DeliveryID: "program-drift"}
	now := time.Now().UTC()
	human := protocol.AuthorityBundle{Receipts: []protocol.AuthorityReceipt{{
		ID: "program-drift-human", Class: catalog.AuthorityHuman, Subject: "operator", Fingerprint: "explicit-program-reconciliation",
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}}}
	executable, _ := os.Executable()
	executable, _ = filepath.Abs(executable)
	executable, _ = filepath.EvalSymlinks(executable)
	runtimeRaw, _ := os.ReadFile(executable)
	runtimeVersion := installTestRuntime(t, executable, runtimeRaw)
	configPath := filepath.Join(t.TempDir(), "project.json")
	configRaw := []byte("{\"schema_version\":2,\"project\":{\"name\":\"drift\",\"default_branch\":\"main\",\"commands\":{}},\"policy\":{\"plan_approval\":\"human\",\"visual_evidence\":\"optional\"},\"hosts\":[\"cli\"]}\n")
	if err := os.WriteFile(configPath, configRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	initializeRequest := surfaces.Request{
		SchemaVersion: surfaces.SchemaVersion, Operation: surfaces.OperationApply, Repository: repository, Host: "cli", CorrelationID: "program-old",
		FlowID: "flow-program-drift", Objective: objective, TransitionID: "installation.initialize", Authority: human,
		Parameters: protocol.Parameters{
			{Name: "source_revision", Value: "program-old"}, {Name: "runtime_version", Value: runtimeVersion}, {Name: "runtime_sha256", Value: digestBytes(runtimeRaw)},
			{Name: "config_path", Value: configPath}, {Name: "config_sha256", Value: configFingerprint(t, configRaw)},
		},
	}
	initialized, err := oldKernel.Handle(ctx, prescribeSurface(t, ctx, oldKernel, initializeRequest))
	if err != nil {
		t.Fatal(err)
	}
	if initialized.Receipt == nil || initialized.Receipt.Program.Fingerprint != oldProgram.Fingerprint() {
		t.Fatalf("initial receipt did not freeze old program: %#v", initialized.Receipt)
	}
	if initialized.Snapshot == nil || initialized.Snapshot.Objective.Status != model.FactAbsent || initialized.Receipt.ObjectiveStatus != model.FactAbsent || initialized.Receipt.ObjectiveID != "" {
		t.Fatalf("installation initialization invented product intent: %#v", initialized)
	}

	newProgram := testProgram()
	newKernel, err := boatstack.NewDeliveryController(externalRoot, newProgram)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := newKernel.Handle(ctx, surfaces.Request{
		SchemaVersion: surfaces.SchemaVersion, Operation: surfaces.OperationResolve, Repository: repository, Host: "cli", CorrelationID: "program-drift-resolve", Objective: objective,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Decision == nil || resolved.Decision.Kind != supervisor.DecisionUnresolved {
		t.Fatalf("program drift decision = %#v", resolved.Decision)
	}

	resolver, err := plant.NewResolver(externalRoot)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := resolver.ResolveInvocation(ctx, repository, "cli", "program-drift-inspect")
	if err != nil {
		t.Fatal(err)
	}
	layout, _, err := resolver.ResolveLayout(ctx, invocation)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(layout.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	request := surfaces.Request{
		SchemaVersion: surfaces.SchemaVersion, Operation: surfaces.OperationApply, Repository: repository, Host: "cli", CorrelationID: "program-drift-reconcile",
		FlowID: "flow-program-drift", Objective: objective, TransitionID: "installation.reconcile-update",
		Parameters: protocol.Parameters{
			{Name: "source_revision", Value: "program-new"}, {Name: "runtime_version", Value: runtimeVersion}, {Name: "runtime_sha256", Value: digestBytes(runtimeRaw)},
			{Name: "accept_obligation_change", Value: "true"},
		},
	}
	frontierRequest := request
	frontierRequest.Operation = surfaces.OperationResolve
	frontier, err := newKernel.Handle(ctx, frontierRequest)
	if err != nil || frontier.Decision == nil || frontier.Decision.Kind != supervisor.DecisionFrontier {
		t.Fatalf("authority-free reconciliation = response %#v error %v", frontier, err)
	}
	afterRejected, _ := os.ReadFile(layout.StatePath)
	if !bytes.Equal(before, afterRejected) {
		t.Fatal("authority-free reconciliation mutated durable state")
	}
	request.Authority = human
	request.Parameters[3].Value = "false"
	if _, err := newKernel.Handle(ctx, request); err == nil {
		t.Fatal("program-changing update without explicit obligation acceptance succeeded")
	}
	afterInvalid, _ := os.ReadFile(layout.StatePath)
	if !bytes.Equal(before, afterInvalid) {
		t.Fatal("invalid reconciliation mutated durable state")
	}
	request.Parameters[3].Value = "true"
	reconciled, err := newKernel.Handle(ctx, prescribeSurface(t, ctx, newKernel, request))
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.Receipt == nil || reconciled.Receipt.Program.Fingerprint != newProgram.Fingerprint() ||
		reconciled.Receipt.PriorProgramFingerprint != oldProgram.Fingerprint() || reconciled.Receipt.ProgramDeltaFingerprint == "" ||
		!reconciled.Receipt.ProgramChangeAccepted || reconciled.Receipt.RuntimeFingerprint != digestBytes(runtimeRaw) ||
		reconciled.Receipt.RuntimeSourceRevision != "program-new" || reconciled.Snapshot == nil ||
		reconciled.Snapshot.Program.Value != model.ProgramCurrent || reconciled.Snapshot.Phase.Value != model.PhaseObserved {
		t.Fatalf("reconciliation did not establish exact program identity: %#v", reconciled)
	}
	if reconciled.Snapshot.Objective.Status != model.FactAbsent || reconciled.Receipt.ObjectiveStatus != model.FactAbsent || reconciled.Receipt.ObjectiveID != "" {
		t.Fatalf("reconcile-update invented product intent: %#v", reconciled)
	}
	pinRaw, err := os.ReadFile(boatstackruntime.PinPath(repository))
	if err != nil {
		t.Fatal(err)
	}
	pin, err := boatstackruntime.DecodePin(pinRaw)
	if err != nil {
		t.Fatal(err)
	}
	if pin.Version != runtimeVersion || pin.SHA256 != digestBytes(runtimeRaw) || pin.SourceRevision != "program-new" || pin.ProgramFingerprint != newProgram.Fingerprint() {
		t.Fatalf("repository runtime pin did not atomically follow reconciliation: %#v", pin)
	}
	afterSuccess, err := os.ReadFile(layout.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newKernel.Handle(ctx, request); err == nil {
		t.Fatal("already reconciled program delta was accepted a second time")
	}
	afterReplay, err := os.ReadFile(layout.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterSuccess, afterReplay) {
		t.Fatal("rejected repeated reconciliation mutated durable state")
	}
	priorState, err := durable.DecodeState(afterSuccess)
	if err != nil {
		t.Fatal(err)
	}
	var legacyState map[string]any
	if err := json.Unmarshal(afterSuccess, &legacyState); err != nil {
		t.Fatal(err)
	}
	legacyState["schema_version"] = float64(durable.StateSchemaVersion - 2)
	delete(legacyState, "planning_package_fingerprint")
	delete(legacyState, "control_bundle_fingerprint")
	legacyRaw, err := json.MarshalIndent(legacyState, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	legacyRaw = append(legacyRaw, '\n')
	if err := os.WriteFile(layout.StatePath, legacyRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	legacyPinPath := boatstackruntime.PinPath(repository)
	legacyPinRaw, err := os.ReadFile(legacyPinPath)
	if err != nil {
		t.Fatal(err)
	}
	legacyPin, err := boatstackruntime.DecodePin(legacyPinRaw)
	if err != nil {
		t.Fatal(err)
	}
	legacyPin.StateSchemaVersion = durable.StateSchemaVersion - 2
	legacyPinRaw, err = boatstackruntime.EncodePin(legacyPin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPinPath, legacyPinRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	updateRequest := surfaces.Request{
		SchemaVersion: surfaces.SchemaVersion, Operation: surfaces.OperationApply, Repository: repository, Host: "cli", CorrelationID: "program-current-update",
		FlowID: "flow-program-drift", Objective: model.Objective{ID: "ignored-command-objective", TargetID: model.ObjectiveOpenPR, DeliveryID: "ignored"},
		TransitionID: "installation.update", Authority: human,
		Parameters: protocol.Parameters{
			{Name: "source_revision", Value: "program-current"}, {Name: "runtime_version", Value: runtimeVersion}, {Name: "runtime_sha256", Value: digestBytes(runtimeRaw)},
		},
	}
	updated, err := newKernel.Handle(ctx, prescribeSurface(t, ctx, newKernel, updateRequest))
	if err != nil {
		t.Fatalf("current-program update after reconciliation: %v", err)
	}
	if updated.Snapshot == nil || updated.Snapshot.Objective.Status != model.FactAbsent || updated.Receipt == nil || updated.Receipt.ObjectiveStatus != model.FactAbsent || updated.Receipt.ObjectiveID != "" {
		t.Fatalf("reconcile to update composition invented product intent: %#v", updated)
	}
	updatedRaw, err := os.ReadFile(layout.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	updatedState, err := durable.DecodeState(updatedRaw)
	if err != nil {
		t.Fatal(err)
	}
	if updatedState.SchemaVersion != durable.StateSchemaVersion || updatedState.Objective != priorState.Objective || updatedState.Engagement != priorState.Engagement ||
		updatedState.Delivery != priorState.Delivery || updatedState.Workspace != priorState.Workspace || updatedState.Plan != priorState.Plan ||
		updatedState.Configuration != priorState.Configuration || updatedState.Publication != priorState.Publication || updatedState.Verification != priorState.Verification ||
		updatedState.Terminal != priorState.Terminal || updatedState.PlanFingerprint != priorState.PlanFingerprint || updatedState.ApprovalFingerprint != priorState.ApprovalFingerprint {
		t.Fatalf("schema-4 update changed existing product facets: before=%#v after=%#v", priorState, updatedState)
	}
	updatedPinRaw, err := os.ReadFile(legacyPinPath)
	if err != nil {
		t.Fatal(err)
	}
	updatedPin, err := boatstackruntime.DecodePin(updatedPinRaw)
	if err != nil {
		t.Fatal(err)
	}
	if updatedPin.StateSchemaVersion != durable.StateSchemaVersion {
		t.Fatalf("runtime update left prior state schema in the pin: %#v", updatedPin)
	}
}

func TestReferenceExtensionUsesKernelAdmissionVerificationAndReceiptPath(t *testing.T) {
	// control-law: extension-obligation-cannot-be-terminal-before-verified-kernel-receipt
	ctx := context.Background()
	repository := testRepository(t)
	if err := os.MkdirAll(filepath.Join(repository, "release-notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "release-notes", "extension.md"), []byte("### Extension\n\nVerifiable user impact.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	program, err := delivery.Compile(ctx, delivery.CompileRequest{
		KernelVersion: boatstack.Version, Core: core.System(), Runtime: standard.Definition(), Extensions: []delivery.Extension{releasenote.Definition()},
	})
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := boatstack.NewDeliveryController(t.TempDir(), program)
	if err != nil {
		t.Fatal(err)
	}
	objective := model.Objective{ID: "extension-receipt", TargetID: model.ObjectiveOpenPR, DeliveryID: "extension-receipt"}
	now := time.Now().UTC()
	authority := func(class catalog.AuthorityClass) protocol.AuthorityBundle {
		fingerprint, subject := "explicit-human", "integration"
		if class == catalog.AuthorityRepository {
			subject = filepath.Join(repository, ".boatstack", "project.json")
			raw, readErr := os.ReadFile(subject)
			if readErr != nil {
				t.Fatal(readErr)
			}
			fingerprint = configFingerprint(t, raw)
		}
		return protocol.AuthorityBundle{Receipts: []protocol.AuthorityReceipt{{
			ID: "extension-" + string(class), Class: class, Subject: subject, Fingerprint: fingerprint,
			IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		}}}
	}
	apply := func(id catalog.TransitionID, authorization protocol.AuthorityBundle, parameters protocol.Parameters) surfaces.Response {
		t.Helper()
		request := surfaces.Request{
			SchemaVersion: surfaces.SchemaVersion, Operation: surfaces.OperationApply, Repository: repository, Host: "cli",
			CorrelationID: "extension-" + string(id), FlowID: "flow-extension-receipt", Objective: objective, TransitionID: id,
			Authority: authorization, Parameters: parameters,
		}
		response, applyErr := kernel.Handle(ctx, prescribeSurface(t, ctx, kernel, request))
		if applyErr != nil {
			t.Fatalf("apply %s: %v", id, applyErr)
		}
		return response
	}
	executable, _ := os.Executable()
	executable, _ = filepath.Abs(executable)
	executable, _ = filepath.EvalSymlinks(executable)
	runtimeRaw, _ := os.ReadFile(executable)
	runtimeVersion := installTestRuntime(t, executable, runtimeRaw)
	configPath := filepath.Join(t.TempDir(), "project.json")
	configRaw := []byte("{\"schema_version\":2,\"project\":{\"name\":\"extension\",\"default_branch\":\"main\",\"commands\":{\"build\":\"go version\",\"test\":\"go version\"}},\"policy\":{\"plan_approval\":\"human\",\"visual_evidence\":\"optional\"},\"hosts\":[\"cli\"]}\n")
	if err := os.WriteFile(configPath, configRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	initialized := apply("installation.initialize", authority(catalog.AuthorityHuman), protocol.Parameters{
		{Name: "source_revision", Value: "extension-fixture"}, {Name: "runtime_version", Value: runtimeVersion}, {Name: "runtime_sha256", Value: digestBytes(runtimeRaw)},
		{Name: "config_path", Value: configPath}, {Name: "config_sha256", Value: configFingerprint(t, configRaw)},
	})
	if initialized.Receipt == nil || initialized.Receipt.AuthorityFingerprint == "" || len(initialized.Receipt.AuthoritySources) != 1 || len(initialized.Receipt.RequiredCapabilities) == 0 || len(initialized.Receipt.GrantedCapabilities) == 0 || len(initialized.Receipt.ExercisedCapabilities) != 0 || len(initialized.Receipt.CommittedEffects) == 0 || initialized.Receipt.Verification.Result != protocol.VerificationSatisfied {
		t.Fatalf("receipt lost capability or authority provenance: %#v", initialized.Receipt)
	}
	apply("objective.bind", authority(catalog.AuthorityHuman), protocol.Parameters{{Name: "target_id", Value: string(objective.TargetID)}, {Name: "delivery_id", Value: objective.DeliveryID}})
	apply("engagement.begin", authority(catalog.AuthorityRepository), nil)
	planPath := filepath.Join(t.TempDir(), "plan.md")
	planRaw := []byte("# Extension plan\n")
	if err := os.WriteFile(planPath, planRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	apply("plan.create", authority(catalog.AuthorityHuman), protocol.Parameters{{Name: "source_path", Value: planPath}, {Name: "delivery_id", Value: objective.DeliveryID}})
	apply("plan.validate", authority(catalog.AuthorityRepository), nil)
	apply("plan.approve", authority(catalog.AuthorityHuman), protocol.Parameters{{Name: "plan_fingerprint", Value: digestBytes(planRaw)}, {Name: "actor", Value: "integration"}})
	apply("plan.activate", authority(catalog.AuthorityHuman), nil)
	head := strings.TrimSpace(commandOutput(t, repository, "git", "rev-parse", "HEAD"))
	gate := func(name string) protocol.Parameters {
		raw, marshalErr := json.Marshal(map[string]any{"schema_version": 1, "gate": name, "source_revision": head, "outcome": "passed", "producer": "integration", "completed_at": now})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		path := filepath.Join(t.TempDir(), name+".json")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return protocol.Parameters{{Name: "source_revision", Value: head}, {Name: "evidence_path", Value: path}, {Name: "evidence_fingerprint", Value: digestBytes(raw)}}
	}
	apply("gate.build.record", authority(catalog.AuthorityRepository), gate("build"))
	apply("gate.test.record", authority(catalog.AuthorityRepository), gate("test"))
	beforeExtension := apply("gate.review.record", authority(catalog.AuthorityRepository), gate("review"))
	if beforeExtension.Snapshot == nil || beforeExtension.Snapshot.ExtensionFacts[releasenote.FactID].Value != "missing" {
		t.Fatalf("extension obligation disappeared before receipt: %#v", beforeExtension.Snapshot)
	}
	next, err := kernel.Handle(ctx, surfaces.Request{
		SchemaVersion: surfaces.SchemaVersion, Operation: surfaces.OperationResolve, Repository: repository, Host: "cli",
		CorrelationID: "extension-next", Objective: objective, Authority: authority(catalog.AuthorityRepository),
	})
	if err != nil {
		t.Fatal(err)
	}
	if next.Decision == nil || next.Decision.Kind != supervisor.DecisionPrescribed || next.Decision.Transition == nil || next.Decision.Transition.ID != releasenote.Transition {
		t.Fatalf("unmet extension obligation decision = %#v", next.Decision)
	}
	completed := apply(releasenote.Transition, authority(catalog.AuthorityRepository), nil)
	if completed.Receipt == nil || completed.Receipt.TransitionID != releasenote.Transition || completed.Receipt.Program.Fingerprint != program.Fingerprint() ||
		completed.Snapshot == nil || completed.Snapshot.ExtensionFacts[releasenote.FactID].Value != "verified" {
		t.Fatalf("extension did not traverse verified receipt path: %#v", completed)
	}
	after, err := kernel.Handle(ctx, surfaces.Request{
		SchemaVersion: surfaces.SchemaVersion, Operation: surfaces.OperationResolve, Repository: repository, Host: "cli",
		CorrelationID: "extension-after", Objective: objective, Authority: authority(catalog.AuthorityRepository),
	})
	if err != nil || after.Decision == nil {
		t.Fatalf("verified extension did not return control to ProgramRuntime: %#v error=%v", after.Decision, err)
	}
	if after.Decision.Transition != nil && after.Decision.Transition.Origin.Kind == catalog.OriginExtension {
		t.Fatalf("verified extension remained selectable: %#v", after.Decision)
	}
	for _, candidate := range after.Decision.Candidates {
		if candidate == releasenote.Transition {
			t.Fatalf("verified extension remained a frontier candidate: %#v", after.Decision)
		}
	}
}

func TestConcreteWorkflowPreservesConfigurationProofAndObjectiveTerminals(t *testing.T) {
	// control-law: successful-writes-remain-independently-verifiable-and-objective-specific
	ctx := context.Background()
	repository := testRepository(t)
	clock := fixedClock{value: time.Unix(2000, 0).UTC()}
	resolver, err := plant.NewResolver(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := resolver.ResolveInvocation(ctx, repository, "cli", "workflow-correlation")
	if err != nil {
		t.Fatal(err)
	}
	observer, _ := plant.NewObserver(resolver, clock)
	locker, _ := effects.NewLocker(resolver)
	journal, _ := effects.NewJournal(resolver, clock)
	receipts, _ := effects.NewReceiptStore(resolver, clock)
	driver, _ := effects.NewDriver(resolver, clock, effects.NewNativeBoundary())
	kernel, err := engine.New(testprogram.StandardRegistry(), testObjectiveContracts(), testProgramIdentity, observer, clock, locker, journal, driver, receipts)
	if err != nil {
		t.Fatal(err)
	}
	authority := func(class catalog.AuthorityClass) protocol.AuthorityBundle {
		fingerprint := "fingerprint-" + string(class)
		subject := "integration"
		if class == catalog.AuthorityRepository {
			subject = filepath.Join(repository, ".boatstack", "project.json")
			raw, readErr := os.ReadFile(subject)
			if readErr != nil {
				t.Fatal(readErr)
			}
			fingerprint = configFingerprint(t, raw)
		}
		return protocol.AuthorityBundle{Receipts: []protocol.AuthorityReceipt{{
			ID: "authority-" + string(class), Class: class, Subject: subject, Fingerprint: fingerprint,
			IssuedAt: clock.Now().Add(-time.Minute), ExpiresAt: clock.Now().Add(time.Hour),
		}}}
	}
	apply := func(objective model.Objective, id catalog.TransitionID, auth protocol.AuthorityBundle, parameters protocol.Parameters) engine.ApplyResult {
		t.Helper()
		request := engine.ApplyRequest{
			ResolveRequest: engine.ResolveRequest{Invocation: invocation, Objective: objective, Authority: auth, Requested: id},
			FlowID:         "flow-workflow", Parameters: parameters, AdmissionLifetime: time.Minute,
		}
		result, applyErr := kernel.Apply(ctx, prescribeEngine(t, ctx, kernel, request))
		if applyErr != nil {
			t.Fatalf("apply %s: %v", id, applyErr)
		}
		return result
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable, _ = filepath.Abs(executable)
	executable, _ = filepath.EvalSymlinks(executable)
	runtimeRaw, _ := os.ReadFile(executable)
	runtimeVersion := installTestRuntime(t, executable, runtimeRaw)
	configPath := filepath.Join(t.TempDir(), "project-v2.json")
	configRaw := []byte("{\"schema_version\":2,\"project\":{\"name\":\"integration\",\"default_branch\":\"main\",\"commands\":{\"build\":\"go version\",\"test\":\"go version\"}},\"policy\":{\"plan_approval\":\"human\",\"visual_evidence\":\"optional\"},\"hosts\":[\"cli\"]}\n")
	if err := os.WriteFile(configPath, configRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	approvedObjective := model.Objective{ID: "objective-approved", TargetID: model.ObjectiveApprovedPlan, DeliveryID: "delivery-workflow"}
	apply(approvedObjective, "installation.initialize", authority(catalog.AuthorityHuman), protocol.Parameters{
		{Name: "source_revision", Value: "integration-revision"}, {Name: "runtime_version", Value: runtimeVersion}, {Name: "runtime_sha256", Value: digestBytes(runtimeRaw)},
		{Name: "config_path", Value: configPath}, {Name: "config_sha256", Value: configFingerprint(t, configRaw)},
	})
	apply(approvedObjective, "objective.bind", authority(catalog.AuthorityHuman), protocol.Parameters{{Name: "target_id", Value: string(approvedObjective.TargetID)}, {Name: "delivery_id", Value: approvedObjective.DeliveryID}})
	apply(approvedObjective, "engagement.begin", authority(catalog.AuthorityRepository), nil)

	updatedConfigPath := filepath.Join(t.TempDir(), "project-v2-updated.json")
	updatedConfig := []byte("{\"schema_version\":2,\"project\":{\"name\":\"integration-updated\",\"default_branch\":\"main\",\"commands\":{\"build\":\"go version\",\"test\":\"go version\"}},\"policy\":{\"plan_approval\":\"human\",\"visual_evidence\":\"optional\"},\"hosts\":[\"cli\"]}\n")
	if err := os.WriteFile(updatedConfigPath, updatedConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	configResult := apply(approvedObjective, "configuration.mutate", authority(catalog.AuthorityHuman), protocol.Parameters{
		{Name: "config_path", Value: updatedConfigPath}, {Name: "config_sha256", Value: configFingerprint(t, updatedConfig)},
	})
	if configResult.Target.Configuration.Value != model.ConfigurationVerified {
		t.Fatalf("configuration mutation invalidated itself: %s", configResult.Target.Configuration.Value)
	}

	planPath := filepath.Join(t.TempDir(), "plan.md")
	planRaw := []byte("# Verified plan\n")
	if err := os.WriteFile(planPath, planRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	apply(approvedObjective, "plan.create", authority(catalog.AuthorityHuman), protocol.Parameters{{Name: "source_path", Value: planPath}, {Name: "delivery_id", Value: approvedObjective.DeliveryID}})
	apply(approvedObjective, "plan.validate", authority(catalog.AuthorityRepository), nil)
	approved := apply(approvedObjective, "plan.approve", authority(catalog.AuthorityHuman), protocol.Parameters{{Name: "plan_fingerprint", Value: digestBytes(planRaw)}, {Name: "actor", Value: "integration-human"}})
	if approved.Target.Terminal.Value != model.TerminalEstablished || approved.Target.Plan.Value != model.PlanApproved {
		t.Fatalf("approved-plan terminal not established: %#v", approved.Target)
	}

	verifiedObjective := model.Objective{ID: "objective-verified", TargetID: model.ObjectiveVerified, DeliveryID: approvedObjective.DeliveryID}
	apply(verifiedObjective, "objective.bind", authority(catalog.AuthorityHuman), protocol.Parameters{{Name: "target_id", Value: string(verifiedObjective.TargetID)}, {Name: "delivery_id", Value: verifiedObjective.DeliveryID}})
	apply(verifiedObjective, "plan.activate", authority(catalog.AuthorityHuman), nil)
	head := strings.TrimSpace(commandOutput(t, repository, "git", "rev-parse", "HEAD"))
	gateParameters := func(name string) protocol.Parameters {
		evidenceRaw, marshalErr := json.Marshal(map[string]any{
			"schema_version": 1, "gate": name, "source_revision": head,
			"outcome": "passed", "producer": "integration", "completed_at": clock.Now(),
		})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		evidencePath := filepath.Join(t.TempDir(), name+".json")
		if writeErr := os.WriteFile(evidencePath, evidenceRaw, 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
		return protocol.Parameters{
			{Name: "source_revision", Value: head}, {Name: "evidence_path", Value: evidencePath},
			{Name: "evidence_fingerprint", Value: digestBytes(evidenceRaw)},
		}
	}
	apply(verifiedObjective, "gate.build.record", authority(catalog.AuthorityRepository), gateParameters("build"))
	apply(verifiedObjective, "gate.test.record", authority(catalog.AuthorityRepository), gateParameters("test"))
	verified := apply(verifiedObjective, "gate.review.record", authority(catalog.AuthorityRepository), gateParameters("review"))
	if verified.Target.Terminal.Value != model.TerminalEstablished || verified.Target.Verification.Value != model.VerificationCurrent || verified.Target.Delivery.Value != model.DeliveryTerminal {
		t.Fatalf("verified terminal not established: %#v", verified.Target)
	}
	if err := os.WriteFile(filepath.Join(repository, ".boatstack", "evidence", verifiedObjective.DeliveryID, "review.json"), []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tamperedObservation, err := observer.Observe(ctx, ports.ObservationRequest{Invocation: invocation})
	if err != nil {
		t.Fatal(err)
	}
	tampered, err := model.Canonicalize(tamperedObservation)
	if err != nil {
		t.Fatal(err)
	}
	if tampered.Verification.Value != model.VerificationStale || tampered.Terminal.Value != model.TerminalStale {
		t.Fatalf("tampered gate remained authoritative: verification=%s terminal=%s", tampered.Verification.Value, tampered.Terminal.Value)
	}
	if tampered.Phase.Value != model.PhaseActive || tampered.Delivery.Value != model.DeliveryActive {
		t.Fatalf("stale terminal had no re-verification path: phase=%s delivery=%s", tampered.Phase.Value, tampered.Delivery.Value)
	}
	reverified := apply(verifiedObjective, "gate.review.record", authority(catalog.AuthorityRepository), gateParameters("review"))
	if reverified.Target.Terminal.Value != model.TerminalEstablished || reverified.Target.Verification.Value != model.VerificationCurrent {
		t.Fatalf("repaired evidence did not re-establish the exact terminal: %#v", reverified.Target)
	}
}

func TestWorkspaceCutTransfersAuthorityToExactDestinationWorktree(t *testing.T) {
	// control-law: workspace-creation-transfers-controller-authority-in-one-transaction
	ctx := context.Background()
	repository := testRepository(t)
	clock := fixedClock{value: time.Unix(3000, 0).UTC()}
	resolver, err := plant.NewResolver(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sourceInvocation, err := resolver.ResolveInvocation(ctx, repository, "cli", "workspace-transfer")
	if err != nil {
		t.Fatal(err)
	}
	observer, _ := plant.NewObserver(resolver, clock)
	locker, _ := effects.NewLocker(resolver)
	journal, _ := effects.NewJournal(resolver, clock)
	receipts, _ := effects.NewReceiptStore(resolver, clock)
	driver, _ := effects.NewDriver(resolver, clock, effects.NewNativeBoundary())
	kernel, err := engine.New(testprogram.StandardRegistry(), testObjectiveContracts(), testProgramIdentity, observer, clock, locker, journal, driver, receipts)
	if err != nil {
		t.Fatal(err)
	}
	objective := model.Objective{ID: "objective-workspace", TargetID: model.ObjectiveMerged, DeliveryID: "delivery-workspace"}
	human := protocol.AuthorityBundle{Receipts: []protocol.AuthorityReceipt{{
		ID: "human-workspace", Class: catalog.AuthorityHuman, Subject: "operator", Fingerprint: "human-workspace-proof",
		IssuedAt: clock.Now().Add(-time.Minute), ExpiresAt: clock.Now().Add(time.Hour),
	}}}
	apply := func(invocation model.InvocationContext, id catalog.TransitionID, authority protocol.AuthorityBundle, parameters protocol.Parameters) engine.ApplyResult {
		t.Helper()
		request := engine.ApplyRequest{
			ResolveRequest: engine.ResolveRequest{Invocation: invocation, Objective: objective, Authority: authority, Requested: id},
			FlowID:         "flow-workspace", Parameters: parameters, AdmissionLifetime: time.Minute,
		}
		result, applyErr := kernel.Apply(ctx, prescribeEngine(t, ctx, kernel, request))
		if applyErr != nil {
			t.Fatalf("apply %s: %v", id, applyErr)
		}
		return result
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable, _ = filepath.Abs(executable)
	executable, _ = filepath.EvalSymlinks(executable)
	runtimeRaw, _ := os.ReadFile(executable)
	runtimeVersion := installTestRuntime(t, executable, runtimeRaw)
	configSource := filepath.Join(t.TempDir(), "project-v2.json")
	configRaw := []byte("{\"schema_version\":2,\"project\":{\"name\":\"workspace\",\"default_branch\":\"main\",\"commands\":{}},\"policy\":{\"plan_approval\":\"human\",\"visual_evidence\":\"optional\"},\"hosts\":[\"cli\"]}\n")
	if err := os.WriteFile(configSource, configRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	apply(sourceInvocation, "installation.initialize", human, protocol.Parameters{
		{Name: "source_revision", Value: "integration-revision"}, {Name: "runtime_version", Value: runtimeVersion}, {Name: "runtime_sha256", Value: digestBytes(runtimeRaw)},
		{Name: "config_path", Value: configSource}, {Name: "config_sha256", Value: configFingerprint(t, configRaw)},
	})
	apply(sourceInvocation, "objective.bind", human, protocol.Parameters{{Name: "target_id", Value: string(objective.TargetID)}, {Name: "delivery_id", Value: objective.DeliveryID}})
	run(t, repository, "git", "add", ".boatstack/project.json", ".boatstack/runtime.json")
	run(t, repository, "git", "commit", "-q", "-m", "install Boatstack configuration")
	repositoryAuthority := func(path string) protocol.AuthorityBundle {
		raw, readErr := os.ReadFile(filepath.Join(path, ".boatstack", "project.json"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		return protocol.AuthorityBundle{Receipts: []protocol.AuthorityReceipt{{
			ID: "repository-workspace", Class: catalog.AuthorityRepository, Subject: filepath.Join(path, ".boatstack", "project.json"), Fingerprint: configFingerprint(t, raw),
			IssuedAt: clock.Now().Add(-time.Minute), ExpiresAt: clock.Now().Add(time.Hour),
		}}}
	}
	apply(sourceInvocation, "engagement.begin", repositoryAuthority(repository), nil)
	destination := filepath.Join(t.TempDir(), "feature-worktree")
	destinationParent, err := filepath.EvalSymlinks(filepath.Dir(destination))
	if err != nil {
		t.Fatal(err)
	}
	canonicalDestination := filepath.Join(destinationParent, filepath.Base(destination))
	cut := apply(sourceInvocation, "workspace.cut", human, protocol.Parameters{
		{Name: "branch", Value: "feature/workspace-transfer"}, {Name: "base_ref", Value: "HEAD"}, {Name: "destination", Value: destination},
	})
	if cut.Target.Invocation.InvokingPath != canonicalDestination || cut.Target.Invocation.WorktreeID == sourceInvocation.WorktreeID || cut.Target.Workspace.Value != model.WorkspaceCut {
		t.Fatalf("workspace authority did not transfer to destination: %#v", cut.Target)
	}

	sourceAfter, err := resolver.ResolveInvocation(ctx, repository, "cli", "source-after-transfer")
	if err != nil {
		t.Fatal(err)
	}
	sourceObservation, err := observer.Observe(ctx, ports.ObservationRequest{Invocation: sourceAfter})
	if err != nil {
		t.Fatal(err)
	}
	if sourceObservation.Phase.Value != model.PhaseDormant || sourceObservation.Engagement.Value != model.EngagementDormant || sourceObservation.Objective.Status != model.FactAbsent {
		t.Fatalf("source checkout retained ambient workflow authority: %#v", sourceObservation)
	}

	destinationInvocation, err := resolver.ResolveInvocation(ctx, canonicalDestination, "cli", "destination-activation")
	if err != nil {
		t.Fatal(err)
	}
	destinationConfigPath := filepath.Join(canonicalDestination, ".boatstack", "project.json")
	destinationConfig, err := os.ReadFile(destinationConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	lineEndingVariant := bytes.ReplaceAll(destinationConfig, []byte("\r\n"), []byte("\n"))
	lineEndingVariant = bytes.ReplaceAll(lineEndingVariant, []byte("\n"), []byte("\r\n"))
	if err := os.WriteFile(destinationConfigPath, lineEndingVariant, 0o600); err != nil {
		t.Fatal(err)
	}
	destinationObservation, err := observer.Observe(ctx, ports.ObservationRequest{Invocation: destinationInvocation})
	if err != nil {
		t.Fatal(err)
	}
	if destinationObservation.Workspace.Value != model.WorkspaceCut || destinationObservation.Objective.Value != objective {
		t.Fatalf("destination did not receive exact controller state: %#v", destinationObservation)
	}
	activated := apply(destinationInvocation, "workspace.activate", repositoryAuthority(canonicalDestination), protocol.Parameters{{Name: "branch", Value: "feature/workspace-transfer"}})
	if activated.Target.Workspace.Value != model.WorkspaceActive || activated.Target.Invocation.WorktreeID != destinationInvocation.WorktreeID {
		t.Fatalf("destination activation failed: %#v", activated.Target)
	}
	if err := os.WriteFile(destinationConfigPath, destinationConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	objective = model.Objective{ID: "objective-workspace-abandon", TargetID: model.ObjectiveAbandoned, DeliveryID: "delivery-workspace"}
	apply(destinationInvocation, "objective.bind", human, protocol.Parameters{{Name: "target_id", Value: string(objective.TargetID)}, {Name: "delivery_id", Value: objective.DeliveryID}})
	abandoned := apply(destinationInvocation, "workspace.abandon", human, protocol.Parameters{{Name: "branch", Value: "feature/workspace-transfer"}})
	if abandoned.Target.Terminal.Value != model.TerminalEstablished || abandoned.Target.Workspace.Value != model.WorkspaceAbandoned {
		t.Fatalf("workspace abandonment did not establish its configured terminal: %#v", abandoned.Target)
	}
	cleaned := apply(destinationInvocation, "workspace.cleanup", human, protocol.Parameters{{Name: "branch", Value: "feature/workspace-transfer"}})
	if cleaned.Target.Invocation.WorktreeID != sourceInvocation.WorktreeID || cleaned.Target.Workspace.Value != model.WorkspaceAbsent || cleaned.Target.Phase.Value != model.PhaseAbandoned {
		t.Fatalf("cleanup did not return verified authority to source checkout: %#v", cleaned.Target)
	}
	if _, err := os.Stat(canonicalDestination); !os.IsNotExist(err) {
		t.Fatalf("workspace destination still exists after verified cleanup: %v", err)
	}
	layout, _, err := resolver.ResolveLayout(ctx, destinationInvocation)
	if err == nil {
		t.Fatal("deleted destination invocation still resolved")
	}
	layout, _, err = resolver.ResolveLayout(ctx, cleaned.Target.Invocation)
	if err != nil {
		t.Fatal(err)
	}
	eventBytes, err := os.ReadFile(layout.EventPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(strings.TrimSpace(string(eventBytes)), "\n")+1 != 8 {
		t.Fatalf("shared flow telemetry lost a cross-worktree transition: %s", eventBytes)
	}
}

func digestBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func configFingerprint(t *testing.T, value []byte) string {
	t.Helper()
	_, fingerprint, err := protocol.ProjectConfigFingerprint(value)
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint
}

func commandOutput(t *testing.T, directory, name string, arguments ...string) string {
	t.Helper()
	command := exec.Command(name, arguments...)
	command.Dir = directory
	value, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(value)
}
