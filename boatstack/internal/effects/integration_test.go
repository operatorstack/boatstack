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
	"github.com/operatorstack/boatstack/boatstack/internal/effects"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/engine"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/plant"
	"github.com/operatorstack/boatstack/boatstack/internal/surfaces"
)

type fixedClock struct{ value time.Time }

func (c fixedClock) Now() time.Time { return c.value }

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
	kernel, err := engine.New(catalog.Default(), observer, clock, locker, journal, driver, receipts)
	if err != nil {
		t.Fatal(err)
	}
	goal := model.Goal{ID: "goal-1", Kind: model.GoalVerified, DeliveryID: "delivery-1"}
	authority := protocol.AuthorityBundle{Receipts: []protocol.AuthorityReceipt{{
		ID: "authority-1", Class: catalog.AuthorityHuman, Subject: invocation.RepositoryID, Fingerprint: "human-fingerprint",
		IssuedAt: clock.Now().Add(-time.Minute), ExpiresAt: clock.Now().Add(time.Hour),
	}}}
	result, err := kernel.Apply(ctx, engine.ApplyRequest{
		ResolveRequest: engine.ResolveRequest{Invocation: invocation, Goal: goal, Authority: authority, Requested: "repository.attach"},
		FlowID:         "flow-1", Parameters: protocol.Parameters{{Name: "topology", Value: string(model.TopologyDetached)}, {Name: "config_authority", Value: "repository"}}, AdmissionLifetime: time.Minute,
	})
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
			t.Errorf("expected V2 artifact %s: %v", path, err)
		}
	}
}

func TestExternalConfigurationAuthorityTransfersAcrossAttachAndDetach(t *testing.T) {
	// control-law: detached-config-authority-selects-the-real-reader-and-writer
	ctx := context.Background()
	repository := testRepository(t)
	externalRoot := t.TempDir()
	kernel, err := boatstack.NewV2Kernel(externalRoot)
	if err != nil {
		t.Fatal(err)
	}
	goal := model.Goal{ID: "external-config-goal", Kind: model.GoalApprovedPlan, DeliveryID: "external-config"}
	now := time.Now().UTC()
	human := protocol.AuthorityBundle{Receipts: []protocol.AuthorityReceipt{{
		ID: "external-config-human", Class: catalog.AuthorityHuman, Subject: "integration", Fingerprint: "explicit-human",
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}}}
	apply := func(id catalog.TransitionID, authority protocol.AuthorityBundle, repositoryAuthority bool, parameters protocol.Parameters) surfaces.Response {
		t.Helper()
		response, handleErr := kernel.Handle(ctx, surfaces.Request{
			SchemaVersion: surfaces.SchemaVersion, Operation: surfaces.OperationApply, Repository: repository, Host: "cli",
			CorrelationID: "external-config-" + string(id), FlowID: "flow-external-config", Goal: goal, TransitionID: id,
			Authority: authority, RepositoryAuthority: repositoryAuthority, Parameters: parameters,
		})
		if handleErr != nil {
			t.Fatalf("apply %s: %v", id, handleErr)
		}
		return response
	}
	executable, _ := os.Executable()
	executable, _ = filepath.Abs(executable)
	executable, _ = filepath.EvalSymlinks(executable)
	runtimeRaw, _ := os.ReadFile(executable)
	initialConfig := []byte("{\"schema_version\":2,\"project\":{\"name\":\"external-initial\",\"default_branch\":\"main\",\"commands\":{}},\"policy\":{\"plan_approval\":\"human\",\"visual_evidence\":\"optional\"},\"hosts\":[\"cli\"]}\n")
	initialPath := filepath.Join(t.TempDir(), "initial.json")
	if err := os.WriteFile(initialPath, initialConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	apply("installation.initialize", human, false, protocol.Parameters{
		{Name: "source_revision", Value: "external-config-fixture"}, {Name: "runtime_path", Value: executable}, {Name: "runtime_sha256", Value: digestBytes(runtimeRaw)},
		{Name: "config_path", Value: initialPath}, {Name: "config_sha256", Value: configFingerprint(t, initialConfig)},
	})
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

func TestConcreteWorkflowPreservesConfigurationProofAndGoalTerminals(t *testing.T) {
	// control-law: successful-writes-remain-independently-verifiable-and-goal-specific
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
	kernel, err := engine.New(catalog.Default(), observer, clock, locker, journal, driver, receipts)
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
	apply := func(goal model.Goal, id catalog.TransitionID, auth protocol.AuthorityBundle, parameters protocol.Parameters) engine.ApplyResult {
		t.Helper()
		result, applyErr := kernel.Apply(ctx, engine.ApplyRequest{
			ResolveRequest: engine.ResolveRequest{Invocation: invocation, Goal: goal, Authority: auth, Requested: id},
			FlowID:         "flow-workflow", Parameters: parameters, AdmissionLifetime: time.Minute,
		})
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
	configPath := filepath.Join(t.TempDir(), "project-v2.json")
	configRaw := []byte("{\"schema_version\":2,\"project\":{\"name\":\"integration\",\"default_branch\":\"main\",\"commands\":{\"build\":\"go version\",\"test\":\"go version\"}},\"policy\":{\"plan_approval\":\"human\",\"visual_evidence\":\"optional\"},\"hosts\":[\"cli\"]}\n")
	if err := os.WriteFile(configPath, configRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	approvedGoal := model.Goal{ID: "goal-approved", Kind: model.GoalApprovedPlan, DeliveryID: "delivery-workflow"}
	apply(approvedGoal, "installation.initialize", authority(catalog.AuthorityHuman), protocol.Parameters{
		{Name: "source_revision", Value: "integration-revision"}, {Name: "runtime_path", Value: executable}, {Name: "runtime_sha256", Value: digestBytes(runtimeRaw)},
		{Name: "config_path", Value: configPath}, {Name: "config_sha256", Value: configFingerprint(t, configRaw)},
	})
	apply(approvedGoal, "engagement.begin", authority(catalog.AuthorityRepository), nil)

	updatedConfigPath := filepath.Join(t.TempDir(), "project-v2-updated.json")
	updatedConfig := []byte("{\"schema_version\":2,\"project\":{\"name\":\"integration-updated\",\"default_branch\":\"main\",\"commands\":{\"build\":\"go version\",\"test\":\"go version\"}},\"policy\":{\"plan_approval\":\"human\",\"visual_evidence\":\"optional\"},\"hosts\":[\"cli\"]}\n")
	if err := os.WriteFile(updatedConfigPath, updatedConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	configResult := apply(approvedGoal, "configuration.mutate", authority(catalog.AuthorityHuman), protocol.Parameters{
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
	apply(approvedGoal, "plan.create", authority(catalog.AuthorityHuman), protocol.Parameters{{Name: "source_path", Value: planPath}, {Name: "delivery_id", Value: approvedGoal.DeliveryID}})
	apply(approvedGoal, "plan.validate", authority(catalog.AuthorityRepository), nil)
	approved := apply(approvedGoal, "plan.approve", authority(catalog.AuthorityHuman), protocol.Parameters{{Name: "plan_fingerprint", Value: digestBytes(planRaw)}, {Name: "actor", Value: "integration-human"}})
	if approved.Target.Terminal.Value != model.TerminalEstablished || approved.Target.Plan.Value != model.PlanApproved {
		t.Fatalf("approved-plan terminal not established: %#v", approved.Target)
	}

	verifiedGoal := model.Goal{ID: "goal-verified", Kind: model.GoalVerified, DeliveryID: approvedGoal.DeliveryID}
	apply(verifiedGoal, "goal.configure", authority(catalog.AuthorityHuman), protocol.Parameters{{Name: "goal_kind", Value: string(verifiedGoal.Kind)}, {Name: "delivery_id", Value: verifiedGoal.DeliveryID}})
	apply(verifiedGoal, "plan.activate", authority(catalog.AuthorityHuman), nil)
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
	apply(verifiedGoal, "gate.build.record", authority(catalog.AuthorityRepository), gateParameters("build"))
	apply(verifiedGoal, "gate.test.record", authority(catalog.AuthorityRepository), gateParameters("test"))
	verified := apply(verifiedGoal, "gate.review.record", authority(catalog.AuthorityRepository), gateParameters("review"))
	if verified.Target.Terminal.Value != model.TerminalEstablished || verified.Target.Verification.Value != model.VerificationCurrent || verified.Target.Delivery.Value != model.DeliveryTerminal {
		t.Fatalf("verified terminal not established: %#v", verified.Target)
	}
	if err := os.WriteFile(filepath.Join(repository, ".boatstack", "evidence", verifiedGoal.DeliveryID, "review.json"), []byte("tampered\n"), 0o644); err != nil {
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
	reverified := apply(verifiedGoal, "gate.review.record", authority(catalog.AuthorityRepository), gateParameters("review"))
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
	kernel, err := engine.New(catalog.Default(), observer, clock, locker, journal, driver, receipts)
	if err != nil {
		t.Fatal(err)
	}
	goal := model.Goal{ID: "goal-workspace", Kind: model.GoalMerged, DeliveryID: "delivery-workspace"}
	human := protocol.AuthorityBundle{Receipts: []protocol.AuthorityReceipt{{
		ID: "human-workspace", Class: catalog.AuthorityHuman, Subject: "operator", Fingerprint: "human-workspace-proof",
		IssuedAt: clock.Now().Add(-time.Minute), ExpiresAt: clock.Now().Add(time.Hour),
	}}}
	apply := func(invocation model.InvocationContext, id catalog.TransitionID, authority protocol.AuthorityBundle, parameters protocol.Parameters) engine.ApplyResult {
		t.Helper()
		result, applyErr := kernel.Apply(ctx, engine.ApplyRequest{
			ResolveRequest: engine.ResolveRequest{Invocation: invocation, Goal: goal, Authority: authority, Requested: id},
			FlowID:         "flow-workspace", Parameters: parameters, AdmissionLifetime: time.Minute,
		})
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
	configSource := filepath.Join(t.TempDir(), "project-v2.json")
	configRaw := []byte("{\"schema_version\":2,\"project\":{\"name\":\"workspace\",\"default_branch\":\"main\",\"commands\":{}},\"policy\":{\"plan_approval\":\"human\",\"visual_evidence\":\"optional\"},\"hosts\":[\"cli\"]}\n")
	if err := os.WriteFile(configSource, configRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	apply(sourceInvocation, "installation.initialize", human, protocol.Parameters{
		{Name: "source_revision", Value: "integration-revision"}, {Name: "runtime_path", Value: executable}, {Name: "runtime_sha256", Value: digestBytes(runtimeRaw)},
		{Name: "config_path", Value: configSource}, {Name: "config_sha256", Value: configFingerprint(t, configRaw)},
	})
	run(t, repository, "git", "add", ".boatstack/project.json")
	run(t, repository, "git", "commit", "-q", "-m", "install V2 configuration")
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
		{Name: "branch", Value: "feature/v2-workspace-transfer"}, {Name: "base_ref", Value: "HEAD"}, {Name: "destination", Value: destination},
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
	if sourceObservation.Phase.Value != model.PhaseDormant || sourceObservation.Engagement.Value != model.EngagementDormant || sourceObservation.Goal.Status != model.FactAbsent {
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
	if destinationObservation.Workspace.Value != model.WorkspaceCut || destinationObservation.Goal.Value != goal {
		t.Fatalf("destination did not receive exact controller state: %#v", destinationObservation)
	}
	activated := apply(destinationInvocation, "workspace.activate", repositoryAuthority(canonicalDestination), protocol.Parameters{{Name: "branch", Value: "feature/v2-workspace-transfer"}})
	if activated.Target.Workspace.Value != model.WorkspaceActive || activated.Target.Invocation.WorktreeID != destinationInvocation.WorktreeID {
		t.Fatalf("destination activation failed: %#v", activated.Target)
	}
	if err := os.WriteFile(destinationConfigPath, destinationConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	goal = model.Goal{ID: "goal-workspace-abandon", Kind: model.GoalAbandoned, DeliveryID: "delivery-workspace"}
	apply(destinationInvocation, "goal.configure", human, protocol.Parameters{{Name: "goal_kind", Value: string(goal.Kind)}, {Name: "delivery_id", Value: goal.DeliveryID}})
	abandoned := apply(destinationInvocation, "workspace.abandon", human, protocol.Parameters{{Name: "branch", Value: "feature/v2-workspace-transfer"}})
	if abandoned.Target.Terminal.Value != model.TerminalEstablished || abandoned.Target.Workspace.Value != model.WorkspaceAbandoned {
		t.Fatalf("workspace abandonment did not establish its configured terminal: %#v", abandoned.Target)
	}
	cleaned := apply(destinationInvocation, "workspace.cleanup", human, protocol.Parameters{{Name: "branch", Value: "feature/v2-workspace-transfer"}})
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
	if strings.Count(strings.TrimSpace(string(eventBytes)), "\n")+1 != 7 {
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
