// Package conformance provides a reusable verifier for kernel domains.
package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/kernel"
)

// Setup identifies the domain state required by one conformance case.
type Setup string

const (
	SetupUnbound            Setup = "unbound"
	SetupBound              Setup = "bound"
	SetupMaintenanceAbsent  Setup = "maintenance-absent"
	SetupMaintenanceBound   Setup = "maintenance-bound"
	SetupConcurrentSameBase Setup = "concurrent-same-base"
)

// Snapshot exposes only deterministic test evidence. It is not kernel state.
type Snapshot struct {
	State       kernel.ControlState
	Effects     map[string]int
	Receipts    []kernel.Receipt
	CommitCount int
}

// Scenario maps domain-specific fixture operations onto domain-neutral laws.
// The suite never infers these roles from transition or operation names.
type Scenario struct {
	InstanceID            string
	Objective             kernel.Objective
	ConflictingObjective  kernel.Objective
	Authority             kernel.Authority
	BindTransition        string
	AdvanceTransitions    []string
	MaintenanceTransition string
	RecoveryTransition    string
	ExtraCapability       kernel.Capability
	ChangeObservation     func()
	RebindObjective       func(kernel.Objective)
	InterruptNextOperator func()
	PanicNextOperator     func()
	FailNextCommit        func()
	RetargetInstance      func(string)
	Snapshot              func() Snapshot
}

// KernelConformance binds kernel ports to explicit scenario roles. New must
// return an isolated fixture for every requested Setup.
type KernelConformance struct {
	Domain               kernel.Domain
	Operator             kernel.Operator
	CapabilityClassifier kernel.CapabilityClassifier
	Store                kernel.Store
	Locker               kernel.Locker
	Clock                kernel.Clock
	Program              kernel.Program
	Scenario             Scenario
	New                  func(testing.TB, Setup) KernelConformance
}

// Run exercises the kernel control laws against fresh domain fixtures.
func (suite KernelConformance) Run(t *testing.T) {
	t.Helper()

	t.Run("objective_binding", suite.objectiveBinding)
	t.Run("objective_absence", suite.objectiveAbsence)
	t.Run("maintenance_preserves_exact_binding", suite.maintenancePreservesExactBinding)
	t.Run("objective_revision_invalidates_prescription_before_effects", suite.objectiveRevisionInvalidatesPrescription)
	t.Run("stale_prescription_precedes_effects", suite.stalePrescriptionPrecedesEffects)
	t.Run("authority_denial_fails_closed", suite.authorityDenialFailsClosed)
	t.Run("future_authority_fails_closed", suite.futureAuthorityFailsClosed)
	t.Run("capability_classifier_cannot_be_weakened", suite.capabilityClassifierCannotBeWeakened)
	t.Run("targeted_and_untargeted_share_one_relation", suite.targetedAndUntargetedShareRelation)
	t.Run("interrupted_operator_requires_explicit_recovery", suite.interruptedOperatorRequiresExplicitRecovery)
	t.Run("process_panic_requires_explicit_recovery", suite.processPanicRequiresExplicitRecovery)
	t.Run("atomic_commit_failure_requires_recovery_without_duplicate_effect", suite.atomicCommitFailureRequiresRecovery)
	t.Run("failed_recovery_preserves_original_obligation", suite.failedRecoveryPreservesOriginalObligation)
	t.Run("prescription_cannot_replay_across_instances", suite.prescriptionCannotReplayAcrossInstances)
	t.Run("concurrent_apply_commits_once_before_loser_effect", suite.concurrentApplyCommitsOnce)
	t.Run("program_reaches_marked_state", suite.programReachesMarkedState)
}

func (suite KernelConformance) objectiveBinding(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupUnbound)
	before := fixture.Scenario.Snapshot()
	receipt := resolveAndApply(t, runtime, fixture.Scenario, fixture.Scenario.BindTransition, &fixture.Scenario.Objective)
	after := fixture.Scenario.Snapshot()
	if after.State.ObjectiveBinding == nil || !after.State.ObjectiveBinding.Matches(fixture.Scenario.Objective) {
		t.Fatal("control-law objective-binding: apply did not bind the exact objective")
	}
	if after.CommitCount != before.CommitCount+1 || len(after.Receipts) != len(before.Receipts)+1 || receipt.ObjectiveBinding == nil {
		t.Fatalf("control-law objective-binding: commit/receipt evidence is incomplete: %#v", after)
	}
}

func (suite KernelConformance) objectiveAbsence(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupMaintenanceAbsent)
	before := fixture.Scenario.Snapshot()
	resolveAndApply(t, runtime, fixture.Scenario, fixture.Scenario.MaintenanceTransition, &fixture.Scenario.ConflictingObjective)
	after := fixture.Scenario.Snapshot()
	if before.State.ObjectiveBinding != nil || after.State.ObjectiveBinding != nil {
		t.Fatal("control-law objective-absence: maintenance synthesized an objective binding")
	}
}

func (suite KernelConformance) maintenancePreservesExactBinding(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupMaintenanceBound)
	before := fixture.Scenario.Snapshot()
	resolveAndApply(t, runtime, fixture.Scenario, fixture.Scenario.MaintenanceTransition, &fixture.Scenario.ConflictingObjective)
	after := fixture.Scenario.Snapshot()
	if before.State.ObjectiveBinding == nil || after.State.ObjectiveBinding == nil || *after.State.ObjectiveBinding != *before.State.ObjectiveBinding {
		t.Fatal("control-law objective-preservation: maintenance changed the exact binding")
	}
}

func (suite KernelConformance) objectiveRevisionInvalidatesPrescription(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupBound)
	transition := fixture.Scenario.AdvanceTransitions[0]
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	before := fixture.Scenario.Snapshot()
	fixture.Scenario.RebindObjective(fixture.Scenario.ConflictingObjective)
	request.Objective = &fixture.Scenario.ConflictingObjective
	_, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	after := fixture.Scenario.Snapshot()
	if !kernel.IsStale(err) || effectCount(after, transition) != effectCount(before, transition) || after.CommitCount != before.CommitCount {
		t.Fatalf("control-law objective-revision-freshness: error=%v before=%#v after=%#v", err, before, after)
	}
}

func (suite KernelConformance) stalePrescriptionPrecedesEffects(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupBound)
	transition := fixture.Scenario.AdvanceTransitions[0]
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	before := fixture.Scenario.Snapshot()
	fixture.Scenario.ChangeObservation()
	_, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	after := fixture.Scenario.Snapshot()
	if !kernel.IsStale(err) || effectCount(after, transition) != effectCount(before, transition) || after.CommitCount != before.CommitCount {
		t.Fatalf("control-law prescription-freshness: error=%v before=%#v after=%#v", err, before, after)
	}
}

func (suite KernelConformance) authorityDenialFailsClosed(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupBound)
	transition := fixture.Scenario.AdvanceTransitions[0]
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	before := fixture.Scenario.Snapshot()
	resolution, err := runtime.Resolve(context.Background(), kernel.ResolveRequest{InstanceID: fixture.Scenario.InstanceID, Objective: &fixture.Scenario.Objective, Requested: transition})
	if err != nil || resolution.Decision.Kind != kernel.Frontier {
		t.Fatalf("control-law authority-denial: decision=%#v error=%v", resolution.Decision, err)
	}
	request.Authority = kernel.Authority{}
	_, err = runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	after := fixture.Scenario.Snapshot()
	if !kernel.IsStale(err) || effectCount(after, transition) != effectCount(before, transition) || after.CommitCount != before.CommitCount {
		t.Fatalf("control-law authority-denial: apply error=%v before=%#v after=%#v", err, before, after)
	}
}

func (suite KernelConformance) futureAuthorityFailsClosed(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupBound)
	transition := fixture.Scenario.AdvanceTransitions[0]
	authority := fixture.Scenario.Authority
	authority.Receipts = append([]kernel.AuthorityReceipt(nil), authority.Receipts...)
	authority.Receipts[0].IssuedAt = fixture.Clock.Now().Add(time.Second)
	before := fixture.Scenario.Snapshot()
	resolution, err := runtime.Resolve(context.Background(), kernel.ResolveRequest{InstanceID: fixture.Scenario.InstanceID, Objective: &fixture.Scenario.Objective, Authority: authority, Requested: transition})
	after := fixture.Scenario.Snapshot()
	if err != nil || resolution.Decision.Kind != kernel.Refused || effectCount(after, transition) != effectCount(before, transition) || after.CommitCount != before.CommitCount {
		t.Fatalf("control-law authority-time-validity: decision=%#v error=%v", resolution.Decision, err)
	}
}

func (suite KernelConformance) capabilityClassifierCannotBeWeakened(t *testing.T) {
	fixture := suite.fixture(t, SetupBound)
	transition := fixture.Scenario.AdvanceTransitions[0]
	classifier := strengtheningClassifier{base: fixture.CapabilityClassifier, transitionID: transition, capability: fixture.Scenario.ExtraCapability}
	runtime, err := kernel.NewRuntime(fixture.Program, fixture.Domain, fixture.Operator, classifier, fixture.Store, fixture.Locker, fixture.Clock)
	if err != nil {
		t.Fatal(err)
	}
	before := fixture.Scenario.Snapshot()
	resolution, err := runtime.Resolve(context.Background(), kernel.ResolveRequest{InstanceID: fixture.Scenario.InstanceID, Objective: &fixture.Scenario.Objective, Authority: fixture.Scenario.Authority, Requested: transition})
	after := fixture.Scenario.Snapshot()
	if err != nil || resolution.Decision.Kind != kernel.Frontier || effectCount(after, transition) != effectCount(before, transition) {
		t.Fatalf("control-law capability-non-weakening: decision=%#v error=%v", resolution.Decision, err)
	}
}

func (suite KernelConformance) targetedAndUntargetedShareRelation(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupBound)
	ctx := context.Background()
	untargetedRequest := kernel.ResolveRequest{InstanceID: fixture.Scenario.InstanceID, Objective: &fixture.Scenario.Objective, Authority: fixture.Scenario.Authority}
	untargeted, err := runtime.Resolve(ctx, untargetedRequest)
	if err != nil || untargeted.Decision.Kind != kernel.Prescribed || untargeted.Prescription == nil {
		t.Fatalf("control-law canonical-relation: untargeted=%#v error=%v", untargeted.Decision, err)
	}
	targetedRequest := untargetedRequest
	targetedRequest.Requested = untargeted.Decision.Transition
	targeted, err := runtime.Resolve(ctx, targetedRequest)
	if err != nil || targeted.Decision.Kind != kernel.Prescribed || targeted.Prescription == nil || targeted.Prescription.ID != untargeted.Prescription.ID {
		t.Fatalf("control-law canonical-relation: targeted=%#v error=%v", targeted.Decision, err)
	}
	if _, err := runtime.Apply(ctx, kernel.ApplyRequest{ResolveRequest: targetedRequest, Prescription: *targeted.Prescription}); err != nil {
		t.Fatalf("control-law prescription-soundness: %v", err)
	}
}

func (suite KernelConformance) interruptedOperatorRequiresExplicitRecovery(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupBound)
	transition := fixture.Scenario.AdvanceTransitions[0]
	fixture.Scenario.InterruptNextOperator()
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	_, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	interrupted := fixture.Scenario.Snapshot()
	if !kernel.IsRecoveryRequired(err) || interrupted.State.Recovery == nil || interrupted.State.Recovery.TransitionID != transition || effectCount(interrupted, transition) != 1 {
		t.Fatalf("control-law explicit-recovery: state=%#v error=%v", interrupted, err)
	}
	recovery := resolveAndApply(t, runtime, fixture.Scenario, fixture.Scenario.RecoveryTransition, &fixture.Scenario.Objective)
	after := fixture.Scenario.Snapshot()
	if after.State.Recovery != nil || recovery.TransitionID != fixture.Scenario.RecoveryTransition {
		t.Fatalf("control-law explicit-recovery: recovery did not settle: %#v", after.State)
	}
}

func (suite KernelConformance) processPanicRequiresExplicitRecovery(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupBound)
	transition := fixture.Scenario.AdvanceTransitions[0]
	fixture.Scenario.PanicNextOperator()
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_, _ = runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	}()
	after := fixture.Scenario.Snapshot()
	if recovered == nil || after.State.Recovery == nil || after.State.Recovery.TransitionID != transition || effectCount(after, transition) != 1 {
		t.Fatalf("control-law panic-recovery: panic=%v snapshot=%#v", recovered, after)
	}
	resolution, err := runtime.Resolve(context.Background(), kernel.ResolveRequest{InstanceID: fixture.Scenario.InstanceID, Objective: &fixture.Scenario.Objective, Authority: fixture.Scenario.Authority})
	if err != nil || resolution.Decision.Kind != kernel.Prescribed || resolution.Decision.Transition != fixture.Scenario.RecoveryTransition || effectCount(fixture.Scenario.Snapshot(), transition) != 1 {
		t.Fatalf("control-law panic-no-replay: decision=%#v error=%v", resolution.Decision, err)
	}
}

func (suite KernelConformance) atomicCommitFailureRequiresRecovery(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupBound)
	transition := fixture.Scenario.AdvanceTransitions[0]
	fixture.Scenario.FailNextCommit()
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	_, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	after := fixture.Scenario.Snapshot()
	if !kernel.IsRecoveryRequired(err) || after.State.Recovery == nil || after.CommitCount != 0 || len(after.Receipts) != 0 || effectCount(after, transition) != 1 {
		t.Fatalf("control-law atomic-commit-recovery: snapshot=%#v error=%v", after, err)
	}
	resolution, resolveErr := runtime.Resolve(context.Background(), kernel.ResolveRequest{InstanceID: fixture.Scenario.InstanceID, Objective: &fixture.Scenario.Objective, Authority: fixture.Scenario.Authority})
	if resolveErr != nil || resolution.Decision.Kind != kernel.Prescribed || resolution.Decision.Transition != fixture.Scenario.RecoveryTransition || effectCount(fixture.Scenario.Snapshot(), transition) != 1 {
		t.Fatalf("control-law no-duplicate-effect: decision=%#v error=%v", resolution.Decision, resolveErr)
	}
}

func (suite KernelConformance) failedRecoveryPreservesOriginalObligation(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupBound)
	transition := fixture.Scenario.AdvanceTransitions[0]
	fixture.Scenario.InterruptNextOperator()
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	_, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	interrupted := fixture.Scenario.Snapshot()
	if !kernel.IsRecoveryRequired(err) || interrupted.State.Recovery == nil {
		t.Fatalf("control-law recovery-retry: initial state=%#v error=%v", interrupted.State, err)
	}
	original := *interrupted.State.Recovery
	recoveryRequest, recoveryPrescription := resolve(t, runtime, fixture.Scenario, fixture.Scenario.RecoveryTransition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	fixture.Scenario.FailNextCommit()
	_, err = runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: recoveryRequest, Prescription: recoveryPrescription})
	failed := fixture.Scenario.Snapshot()
	if !kernel.IsRecoveryRequired(err) || failed.State.Recovery == nil || *failed.State.Recovery != original || effectCount(failed, transition) != 1 {
		t.Fatalf("control-law recovery-obligation-preservation: state=%#v error=%v", failed.State, err)
	}
	resolveAndApply(t, runtime, fixture.Scenario, fixture.Scenario.RecoveryTransition, &fixture.Scenario.Objective)
	settled := fixture.Scenario.Snapshot()
	if settled.State.Recovery != nil || effectCount(settled, transition) != 1 {
		t.Fatalf("control-law recovery-retry: state=%#v", settled.State)
	}
}

func (suite KernelConformance) prescriptionCannotReplayAcrossInstances(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupBound)
	transition := fixture.Scenario.AdvanceTransitions[0]
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	before := fixture.Scenario.Snapshot()
	other := fixture.Scenario.InstanceID + "-other"
	fixture.Scenario.RetargetInstance(other)
	request.InstanceID = other
	_, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	after := fixture.Scenario.Snapshot()
	if !kernel.IsStale(err) || effectCount(after, transition) != effectCount(before, transition) || after.CommitCount != before.CommitCount {
		t.Fatalf("control-law prescription-instance-binding: error=%v snapshot=%#v", err, after)
	}
}

func (suite KernelConformance) concurrentApplyCommitsOnce(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupConcurrentSameBase)
	transition := fixture.Scenario.AdvanceTransitions[0]
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
			results <- err
		}()
	}
	close(start)
	successes, refusals := 0, 0
	for range 2 {
		if err := <-results; err == nil {
			successes++
		} else {
			refusals++
		}
	}
	after := fixture.Scenario.Snapshot()
	if successes != 1 || refusals != 1 || after.CommitCount != 1 || len(after.Receipts) != 1 || effectCount(after, transition) != 1 {
		t.Fatalf("control-law concurrent-cas: success/refusal=%d/%d snapshot=%#v", successes, refusals, after)
	}
}

func (suite KernelConformance) programReachesMarkedState(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupUnbound)
	receipts, err := runUntargetedToMarked(context.Background(), runtime, fixture.Scenario, len(fixture.Scenario.AdvanceTransitions)+1)
	if err != nil {
		t.Fatal(err)
	}
	for _, receipt := range receipts {
		if receipt.Verification != "satisfied" || receipt.AttemptStateRevision != receipt.PriorStateRevision+1 || receipt.ResultStateRevision != receipt.AttemptStateRevision+1 {
			t.Fatalf("control-law receipt-completeness: %#v", receipt)
		}
	}
	after := fixture.Scenario.Snapshot()
	if !fixture.Program.Marked(after.State.Mode) || len(after.Receipts) != len(receipts) {
		t.Fatalf("control-law marked-reachability: snapshot=%#v", after)
	}
}

func runUntargetedToMarked(ctx context.Context, runtime kernel.Runtime, scenario Scenario, maxTransitions int) ([]kernel.Receipt, error) {
	request := kernel.ResolveRequest{InstanceID: scenario.InstanceID, Objective: &scenario.Objective, Authority: scenario.Authority}
	seen := map[string]bool{}
	receipts := make([]kernel.Receipt, 0, maxTransitions)
	for step := 0; ; step++ {
		resolution, err := runtime.Resolve(ctx, request)
		if err != nil {
			return nil, fmt.Errorf("control-law marked-reachability: resolve step %d: %w", step, err)
		}
		if resolution.Decision.Kind == kernel.Marked {
			return receipts, nil
		}
		if resolution.Decision.Kind != kernel.Prescribed || resolution.Prescription == nil {
			return nil, fmt.Errorf("control-law marked-reachability: untargeted step %d returned %s: %s", step, resolution.Decision.Kind, resolution.Decision.Reason)
		}
		identity, err := progressIdentity(resolution)
		if err != nil {
			return nil, fmt.Errorf("control-law marked-reachability: identify step %d: %w", step, err)
		}
		if seen[identity] {
			return nil, fmt.Errorf("control-law marked-reachability: untargeted resolution repeated control state before marked at step %d", step)
		}
		seen[identity] = true
		if step == maxTransitions {
			return nil, fmt.Errorf("control-law marked-reachability: untargeted resolution exceeded %d transitions before marked", maxTransitions)
		}
		receipt, err := runtime.Apply(ctx, kernel.ApplyRequest{ResolveRequest: request, Prescription: *resolution.Prescription})
		if err != nil {
			return nil, fmt.Errorf("control-law marked-reachability: apply %s: %w", resolution.Decision.Transition, err)
		}
		receipts = append(receipts, receipt)
	}
}

func progressIdentity(resolution kernel.Resolution) (string, error) {
	encoded, err := json.Marshal(struct {
		Mode             string                   `json:"mode"`
		ObjectiveBinding *kernel.ObjectiveBinding `json:"objective_binding,omitempty"`
		Recovery         *kernel.RecoveryState    `json:"recovery,omitempty"`
		Observation      string                   `json:"observation"`
	}{resolution.State.Mode, resolution.State.ObjectiveBinding, resolution.State.Recovery, resolution.Observation.Fingerprint})
	return string(encoded), err
}

func (suite KernelConformance) fresh(t *testing.T, setup Setup) (KernelConformance, kernel.Runtime) {
	t.Helper()
	fixture := suite.fixture(t, setup)
	runtime, err := kernel.NewRuntime(fixture.Program, fixture.Domain, fixture.Operator, fixture.CapabilityClassifier, fixture.Store, fixture.Locker, fixture.Clock)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, runtime
}

func (suite KernelConformance) fixture(t testing.TB, setup Setup) KernelConformance {
	t.Helper()
	if suite.New == nil {
		t.Fatal("kernel conformance requires a fresh fixture factory")
	}
	fixture := suite.New(t, setup)
	if fixture.Domain == nil || fixture.Operator == nil || fixture.CapabilityClassifier == nil || fixture.Store == nil || fixture.Locker == nil || fixture.Clock == nil || fixture.Scenario.Snapshot == nil || fixture.Scenario.ChangeObservation == nil || fixture.Scenario.RebindObjective == nil || fixture.Scenario.InterruptNextOperator == nil || fixture.Scenario.PanicNextOperator == nil || fixture.Scenario.FailNextCommit == nil || fixture.Scenario.RetargetInstance == nil || fixture.Scenario.InstanceID == "" || fixture.Scenario.BindTransition == "" || len(fixture.Scenario.AdvanceTransitions) == 0 || fixture.Scenario.MaintenanceTransition == "" || fixture.Scenario.RecoveryTransition == "" || fixture.Scenario.ExtraCapability.Validate() != nil {
		t.Fatal("kernel conformance fixture is incomplete")
	}
	return fixture
}

func resolve(t testing.TB, runtime kernel.Runtime, scenario Scenario, transition string, objective *kernel.Objective, authority kernel.Authority) (kernel.ResolveRequest, kernel.Prescription) {
	t.Helper()
	request := kernel.ResolveRequest{InstanceID: scenario.InstanceID, Objective: objective, Authority: authority, Requested: transition}
	resolution, err := runtime.Resolve(context.Background(), request)
	if err != nil || resolution.Decision.Kind != kernel.Prescribed || resolution.Prescription == nil {
		t.Fatalf("resolve %s: decision=%#v error=%v", transition, resolution.Decision, err)
	}
	return request, *resolution.Prescription
}

func resolveAndApply(t testing.TB, runtime kernel.Runtime, scenario Scenario, transition string, objective *kernel.Objective) kernel.Receipt {
	t.Helper()
	request, prescription := resolve(t, runtime, scenario, transition, objective, scenario.Authority)
	receipt, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	if err != nil {
		t.Fatalf("apply %s: %v", transition, err)
	}
	return receipt
}

func effectCount(snapshot Snapshot, transition string) int {
	return snapshot.Effects[transition]
}

type strengtheningClassifier struct {
	base         kernel.CapabilityClassifier
	transitionID string
	capability   kernel.Capability
}

func (c strengtheningClassifier) RequiredCapabilities(transition kernel.Transition) ([]kernel.Capability, error) {
	required, err := c.base.RequiredCapabilities(transition)
	if err != nil {
		return nil, err
	}
	if transition.ID == c.transitionID {
		required = append(required, c.capability)
	}
	return required, nil
}

func (s Setup) valid() bool {
	switch s {
	case SetupUnbound, SetupBound, SetupMaintenanceAbsent, SetupMaintenanceBound, SetupConcurrentSameBase:
		return true
	default:
		return false
	}
}

func invalidSetup(setup Setup) error {
	return fmt.Errorf("unknown conformance setup %q", setup)
}
