// Package conformance provides a reusable verifier for kernel domains.
package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
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
	Observation kernel.Observation
	Effects     map[string]int
	Receipts    []kernel.Receipt
	CommitCount int
}

// Scenario maps domain-specific fixture operations onto domain-neutral laws.
// The suite never infers these roles from transition or operation names.
type Scenario struct {
	InstanceID            string
	Objective             kernel.Objective
	RevisedObjective      kernel.Objective
	ConflictingObjective  kernel.Objective
	AlternateProgram      kernel.Program
	Authority             kernel.Authority
	BindTransition        string
	AdvanceTransitions    []string
	MaintenanceTransition string
	RecoveryTransition    string
	RecoveryCapability    kernel.Capability
	ExtraCapability       kernel.Capability
	ChangeObservation     func()
	RebindObjective       func(kernel.Objective)
	BumpStateRevision     func()
	RetargetProgram       func(kernel.ProgramIdentity)
	AdvanceClock          func(time.Duration)
	IndependentLocker     func() kernel.Locker
	VerifyCommitted       func(Snapshot, Snapshot, kernel.Receipt) error
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
	t.Run("state_revision_invalidates_prescription_before_effects", suite.stateRevisionInvalidatesPrescription)
	t.Run("program_fingerprint_invalidates_prescription_before_effects", suite.programFingerprintInvalidatesPrescription)
	t.Run("stale_prescription_precedes_effects", suite.stalePrescriptionPrecedesEffects)
	t.Run("authority_denial_fails_closed", suite.authorityDenialFailsClosed)
	t.Run("future_authority_fails_closed", suite.futureAuthorityFailsClosed)
	t.Run("expired_authority_fails_closed", suite.expiredAuthorityFailsClosed)
	t.Run("authority_expiry_invalidates_prescription_before_effects", suite.authorityExpiryInvalidatesPrescription)
	t.Run("capability_classifier_cannot_be_weakened", suite.capabilityClassifierCannotBeWeakened)
	t.Run("targeted_and_untargeted_share_one_relation", suite.targetedAndUntargetedShareRelation)
	t.Run("interrupted_operator_requires_explicit_recovery", suite.interruptedOperatorRequiresExplicitRecovery)
	t.Run("recovery_authority_fails_closed", suite.recoveryAuthorityFailsClosed)
	t.Run("process_panic_requires_explicit_recovery", suite.processPanicRequiresExplicitRecovery)
	t.Run("atomic_commit_failure_requires_recovery_without_duplicate_effect", suite.atomicCommitFailureRequiresRecovery)
	t.Run("failed_recovery_preserves_original_obligation", suite.failedRecoveryPreservesOriginalObligation)
	t.Run("prescription_cannot_replay_across_instances", suite.prescriptionCannotReplayAcrossInstances)
	t.Run("concurrent_apply_commits_once_before_loser_effect", suite.concurrentApplyCommitsOnce)
	t.Run("commit_compare_and_swap_rejects_intervening_state", suite.commitCompareAndSwapRejectsIntervention)
	t.Run("program_reaches_marked_state", suite.programReachesMarkedState)
}

func (suite KernelConformance) objectiveBinding(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupUnbound)
	before := fixture.Scenario.Snapshot()
	receipt := resolveAndApply(t, runtime, fixture.Program, fixture.Scenario, fixture.Scenario.BindTransition, &fixture.Scenario.Objective)
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
	resolveAndApply(t, runtime, fixture.Program, fixture.Scenario, fixture.Scenario.MaintenanceTransition, &fixture.Scenario.ConflictingObjective)
	after := fixture.Scenario.Snapshot()
	if before.State.ObjectiveBinding != nil || after.State.ObjectiveBinding != nil {
		t.Fatal("control-law objective-absence: maintenance synthesized an objective binding")
	}
}

func (suite KernelConformance) maintenancePreservesExactBinding(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupMaintenanceBound)
	before := fixture.Scenario.Snapshot()
	resolveAndApply(t, runtime, fixture.Program, fixture.Scenario, fixture.Scenario.MaintenanceTransition, &fixture.Scenario.ConflictingObjective)
	after := fixture.Scenario.Snapshot()
	if before.State.ObjectiveBinding == nil || after.State.ObjectiveBinding == nil || *after.State.ObjectiveBinding != *before.State.ObjectiveBinding {
		t.Fatal("control-law objective-preservation: maintenance changed the exact binding")
	}
}

func (suite KernelConformance) objectiveRevisionInvalidatesPrescription(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupBound)
	transition := fixture.Scenario.AdvanceTransitions[0]
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	fixture.Scenario.RebindObjective(fixture.Scenario.RevisedObjective)
	before := fixture.Scenario.Snapshot()
	request.Objective = &fixture.Scenario.RevisedObjective
	_, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	after := fixture.Scenario.Snapshot()
	if unchangedErr := refusedApplyMutationError(before, after, transition); !kernel.IsStale(err) || unchangedErr != nil {
		t.Fatalf("control-law objective-revision-freshness: error=%v mutation=%v before=%#v after=%#v", err, unchangedErr, before, after)
	}
}

func (suite KernelConformance) stateRevisionInvalidatesPrescription(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupBound)
	transition := fixture.Scenario.AdvanceTransitions[0]
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	fixture.Scenario.BumpStateRevision()
	before := fixture.Scenario.Snapshot()
	_, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	after := fixture.Scenario.Snapshot()
	if unchangedErr := refusedApplyMutationError(before, after, transition); !kernel.IsStale(err) || unchangedErr != nil {
		t.Fatalf("control-law state-revision-freshness: error=%v mutation=%v before=%#v after=%#v", err, unchangedErr, before, after)
	}
}

func (suite KernelConformance) programFingerprintInvalidatesPrescription(t *testing.T) {
	t.Run("executable_mismatch", func(t *testing.T) {
		fixture, runtime := suite.fresh(t, SetupBound)
		transition := fixture.Scenario.AdvanceTransitions[0]
		request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
		alternate, err := kernel.NewRuntime(fixture.Scenario.AlternateProgram, fixture.Domain, fixture.Operator, fixture.CapabilityClassifier, fixture.Store, fixture.Locker, fixture.Clock)
		if err != nil {
			t.Fatal(err)
		}
		before := fixture.Scenario.Snapshot()
		_, err = alternate.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
		after := fixture.Scenario.Snapshot()
		if unchangedErr := refusedApplyMutationError(before, after, transition); err == nil || unchangedErr != nil {
			t.Fatalf("control-law executable-program-freshness: error=%v mutation=%v before=%#v after=%#v", err, unchangedErr, before, after)
		}
	})
	t.Run("prescription_fingerprint", func(t *testing.T) {
		fixture, runtime := suite.fresh(t, SetupBound)
		transition := fixture.Scenario.AdvanceTransitions[0]
		request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
		fixture.Scenario.RetargetProgram(fixture.Scenario.AlternateProgram.Identity())
		alternate, err := kernel.NewRuntime(fixture.Scenario.AlternateProgram, fixture.Domain, fixture.Operator, fixture.CapabilityClassifier, fixture.Store, fixture.Locker, fixture.Clock)
		if err != nil {
			t.Fatal(err)
		}
		before := fixture.Scenario.Snapshot()
		_, err = alternate.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
		after := fixture.Scenario.Snapshot()
		if unchangedErr := refusedApplyMutationError(before, after, transition); !kernel.IsStale(err) || unchangedErr != nil {
			t.Fatalf("control-law prescription-program-freshness: error=%v mutation=%v before=%#v after=%#v", err, unchangedErr, before, after)
		}
	})
}

func (suite KernelConformance) stalePrescriptionPrecedesEffects(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupBound)
	transition := fixture.Scenario.AdvanceTransitions[0]
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	before := fixture.Scenario.Snapshot()
	fixture.Scenario.ChangeObservation()
	before = fixture.Scenario.Snapshot()
	_, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	after := fixture.Scenario.Snapshot()
	if unchangedErr := refusedApplyMutationError(before, after, transition); !kernel.IsStale(err) || unchangedErr != nil {
		t.Fatalf("control-law prescription-freshness: error=%v mutation=%v before=%#v after=%#v", err, unchangedErr, before, after)
	}
}

func (suite KernelConformance) authorityDenialFailsClosed(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupBound)
	transition := fixture.Scenario.AdvanceTransitions[0]
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	before := fixture.Scenario.Snapshot()
	resolution, err := resolveWithoutMutation(context.Background(), runtime, fixture.Scenario, kernel.ResolveRequest{InstanceID: fixture.Scenario.InstanceID, Objective: &fixture.Scenario.Objective, Requested: transition})
	if err != nil || resolution.Decision.Kind != kernel.Frontier {
		t.Fatalf("control-law authority-denial: decision=%#v error=%v", resolution.Decision, err)
	}
	request.Authority = kernel.Authority{}
	_, err = runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	after := fixture.Scenario.Snapshot()
	if unchangedErr := refusedApplyMutationError(before, after, transition); !kernel.IsStale(err) || unchangedErr != nil {
		t.Fatalf("control-law authority-denial: apply error=%v mutation=%v before=%#v after=%#v", err, unchangedErr, before, after)
	}
}

func (suite KernelConformance) futureAuthorityFailsClosed(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupBound)
	transition := fixture.Scenario.AdvanceTransitions[0]
	authority := fixture.Scenario.Authority
	authority.Receipts = append([]kernel.AuthorityReceipt(nil), authority.Receipts...)
	authority.Receipts[0].IssuedAt = fixture.Clock.Now().Add(time.Second)
	resolution, err := resolveWithoutMutation(context.Background(), runtime, fixture.Scenario, kernel.ResolveRequest{InstanceID: fixture.Scenario.InstanceID, Objective: &fixture.Scenario.Objective, Authority: authority, Requested: transition})
	if err != nil || resolution.Decision.Kind != kernel.Refused {
		t.Fatalf("control-law authority-time-validity: decision=%#v error=%v", resolution.Decision, err)
	}
}

func (suite KernelConformance) expiredAuthorityFailsClosed(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupBound)
	transition := fixture.Scenario.AdvanceTransitions[0]
	authority := fixture.Scenario.Authority
	authority.Receipts = append([]kernel.AuthorityReceipt(nil), authority.Receipts...)
	authority.Receipts[0].ExpiresAt = fixture.Clock.Now()
	resolution, err := resolveWithoutMutation(context.Background(), runtime, fixture.Scenario, kernel.ResolveRequest{InstanceID: fixture.Scenario.InstanceID, Objective: &fixture.Scenario.Objective, Authority: authority, Requested: transition})
	if err != nil || resolution.Decision.Kind != kernel.Refused {
		t.Fatalf("control-law expired-authority: decision=%#v error=%v", resolution.Decision, err)
	}
}

func (suite KernelConformance) authorityExpiryInvalidatesPrescription(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupBound)
	transition := fixture.Scenario.AdvanceTransitions[0]
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	fixture.Scenario.AdvanceClock(2 * time.Hour)
	before := fixture.Scenario.Snapshot()
	_, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	after := fixture.Scenario.Snapshot()
	if unchangedErr := refusedApplyMutationError(before, after, transition); err == nil || unchangedErr != nil {
		t.Fatalf("control-law apply-time-authority-expiry: error=%v mutation=%v before=%#v after=%#v", err, unchangedErr, before, after)
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
	authority := withoutCapability(fixture.Scenario.Authority, fixture.Scenario.ExtraCapability)
	resolution, err := resolveWithoutMutation(context.Background(), runtime, fixture.Scenario, kernel.ResolveRequest{InstanceID: fixture.Scenario.InstanceID, Objective: &fixture.Scenario.Objective, Authority: authority, Requested: transition})
	if err != nil || resolution.Decision.Kind != kernel.Frontier {
		t.Fatalf("control-law capability-non-weakening: decision=%#v error=%v", resolution.Decision, err)
	}
}

func (suite KernelConformance) targetedAndUntargetedShareRelation(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupBound)
	ctx := context.Background()
	untargetedRequest := kernel.ResolveRequest{InstanceID: fixture.Scenario.InstanceID, Objective: &fixture.Scenario.Objective, Authority: fixture.Scenario.Authority}
	untargeted, err := resolveWithoutMutation(ctx, runtime, fixture.Scenario, untargetedRequest)
	if err != nil || untargeted.Decision.Kind != kernel.Prescribed || untargeted.Prescription == nil {
		t.Fatalf("control-law canonical-relation: untargeted=%#v error=%v", untargeted.Decision, err)
	}
	targetedRequest := untargetedRequest
	targetedRequest.Requested = untargeted.Decision.Transition
	targeted, err := resolveWithoutMutation(ctx, runtime, fixture.Scenario, targetedRequest)
	if err != nil || targeted.Decision.Kind != kernel.Prescribed || targeted.Prescription == nil || targeted.Prescription.ID != untargeted.Prescription.ID {
		t.Fatalf("control-law canonical-relation: targeted=%#v error=%v", targeted.Decision, err)
	}
	applyAndRequireCommit(t, runtime, fixture.Program, fixture.Scenario, targetedRequest, *targeted.Prescription)
}

func (suite KernelConformance) interruptedOperatorRequiresExplicitRecovery(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupBound)
	transition := fixture.Scenario.AdvanceTransitions[0]
	fixture.Scenario.InterruptNextOperator()
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	before := fixture.Scenario.Snapshot()
	_, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	interrupted := fixture.Scenario.Snapshot()
	if attemptErr := unresolvedAttemptError(before, interrupted, prescription); !kernel.IsRecoveryRequired(err) || attemptErr != nil || effectCount(interrupted, transition) != effectCount(before, transition)+1 {
		t.Fatalf("control-law explicit-recovery: state=%#v error=%v attempt=%v", interrupted, err, attemptErr)
	}
	recovery := resolveAndApply(t, runtime, fixture.Program, fixture.Scenario, fixture.Scenario.RecoveryTransition, &fixture.Scenario.Objective)
	after := fixture.Scenario.Snapshot()
	if after.State.Recovery != nil || recovery.TransitionID != fixture.Scenario.RecoveryTransition {
		t.Fatalf("control-law explicit-recovery: recovery did not settle: %#v", after.State)
	}
}

func (suite KernelConformance) recoveryAuthorityFailsClosed(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupBound)
	transition := fixture.Scenario.AdvanceTransitions[0]
	fixture.Scenario.InterruptNextOperator()
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	beforeAttempt := fixture.Scenario.Snapshot()
	_, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	interrupted := fixture.Scenario.Snapshot()
	if attemptErr := unresolvedAttemptError(beforeAttempt, interrupted, prescription); !kernel.IsRecoveryRequired(err) || attemptErr != nil {
		t.Fatalf("control-law recovery-authority setup: error=%v attempt=%v", err, attemptErr)
	}
	limited := withoutCapability(fixture.Scenario.Authority, fixture.Scenario.RecoveryCapability)
	resolution, err := resolveWithoutMutation(context.Background(), runtime, fixture.Scenario, kernel.ResolveRequest{InstanceID: fixture.Scenario.InstanceID, Objective: &fixture.Scenario.Objective, Authority: limited})
	afterDenied := fixture.Scenario.Snapshot()
	if err != nil || resolution.Decision.Kind != kernel.Frontier || !reflect.DeepEqual(afterDenied, interrupted) {
		t.Fatalf("control-law recovery-authority denial: decision=%#v error=%v before=%#v after=%#v", resolution.Decision, err, interrupted, afterDenied)
	}
	resolveAndApply(t, runtime, fixture.Program, fixture.Scenario, fixture.Scenario.RecoveryTransition, &fixture.Scenario.Objective)
}

func (suite KernelConformance) processPanicRequiresExplicitRecovery(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupBound)
	transition := fixture.Scenario.AdvanceTransitions[0]
	fixture.Scenario.PanicNextOperator()
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	before := fixture.Scenario.Snapshot()
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_, _ = runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	}()
	after := fixture.Scenario.Snapshot()
	if attemptErr := unresolvedAttemptError(before, after, prescription); recovered == nil || attemptErr != nil || effectCount(after, transition) != effectCount(before, transition)+1 {
		t.Fatalf("control-law panic-recovery: panic=%v snapshot=%#v attempt=%v", recovered, after, attemptErr)
	}
	resolution, err := resolveWithoutMutation(context.Background(), runtime, fixture.Scenario, kernel.ResolveRequest{InstanceID: fixture.Scenario.InstanceID, Objective: &fixture.Scenario.Objective, Authority: fixture.Scenario.Authority})
	if err != nil || resolution.Decision.Kind != kernel.Prescribed || resolution.Decision.Transition != fixture.Scenario.RecoveryTransition || effectCount(fixture.Scenario.Snapshot(), transition) != effectCount(before, transition)+1 {
		t.Fatalf("control-law panic-no-replay: decision=%#v error=%v", resolution.Decision, err)
	}
}

func (suite KernelConformance) atomicCommitFailureRequiresRecovery(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupBound)
	transition := fixture.Scenario.AdvanceTransitions[0]
	fixture.Scenario.FailNextCommit()
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	before := fixture.Scenario.Snapshot()
	_, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	after := fixture.Scenario.Snapshot()
	if attemptErr := unresolvedAttemptError(before, after, prescription); !kernel.IsRecoveryRequired(err) || attemptErr != nil || effectCount(after, transition) != effectCount(before, transition)+1 {
		t.Fatalf("control-law atomic-commit-recovery: snapshot=%#v error=%v attempt=%v", after, err, attemptErr)
	}
	resolution, resolveErr := resolveWithoutMutation(context.Background(), runtime, fixture.Scenario, kernel.ResolveRequest{InstanceID: fixture.Scenario.InstanceID, Objective: &fixture.Scenario.Objective, Authority: fixture.Scenario.Authority})
	if resolveErr != nil || resolution.Decision.Kind != kernel.Prescribed || resolution.Decision.Transition != fixture.Scenario.RecoveryTransition || effectCount(fixture.Scenario.Snapshot(), transition) != effectCount(before, transition)+1 {
		t.Fatalf("control-law no-duplicate-effect: decision=%#v error=%v", resolution.Decision, resolveErr)
	}
}

func (suite KernelConformance) failedRecoveryPreservesOriginalObligation(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupBound)
	transition := fixture.Scenario.AdvanceTransitions[0]
	fixture.Scenario.InterruptNextOperator()
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	before := fixture.Scenario.Snapshot()
	_, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	interrupted := fixture.Scenario.Snapshot()
	if attemptErr := unresolvedAttemptError(before, interrupted, prescription); !kernel.IsRecoveryRequired(err) || attemptErr != nil || effectCount(interrupted, transition) != effectCount(before, transition)+1 {
		t.Fatalf("control-law recovery-retry: initial state=%#v error=%v attempt=%v", interrupted.State, err, attemptErr)
	}
	original := *interrupted.State.Recovery
	recoveryRequest, recoveryPrescription := resolve(t, runtime, fixture.Scenario, fixture.Scenario.RecoveryTransition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	fixture.Scenario.FailNextCommit()
	_, err = runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: recoveryRequest, Prescription: recoveryPrescription})
	failed := fixture.Scenario.Snapshot()
	if attemptErr := unresolvedAttemptError(interrupted, failed, recoveryPrescription); !kernel.IsRecoveryRequired(err) || attemptErr != nil || failed.State.Recovery == nil || *failed.State.Recovery != original || effectCount(failed, transition) != effectCount(interrupted, transition) {
		t.Fatalf("control-law recovery-obligation-preservation: state=%#v error=%v attempt=%v", failed.State, err, attemptErr)
	}
	retryRequest, retryPrescription, resolveErr := resolveUntargetedRecovery(context.Background(), runtime, fixture.Scenario)
	if resolveErr != nil {
		t.Fatalf("control-law recovery-retry selection: %v", resolveErr)
	}
	applyAndRequireCommit(t, runtime, fixture.Program, fixture.Scenario, retryRequest, retryPrescription)
	settled := fixture.Scenario.Snapshot()
	if settled.State.Recovery != nil || effectCount(settled, transition) != effectCount(interrupted, transition) {
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
	before = fixture.Scenario.Snapshot()
	request.InstanceID = other
	_, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	after := fixture.Scenario.Snapshot()
	if unchangedErr := refusedApplyMutationError(before, after, transition); !kernel.IsStale(err) || unchangedErr != nil {
		t.Fatalf("control-law prescription-instance-binding: error=%v mutation=%v snapshot=%#v", err, unchangedErr, after)
	}
}

func (suite KernelConformance) concurrentApplyCommitsOnce(t *testing.T) {
	fixture, resolver := suite.fresh(t, SetupConcurrentSameBase)
	if err := concurrentApplyError(fixture, resolver); err != nil {
		t.Fatalf("control-law concurrent-cas: %v", err)
	}
}

func (suite KernelConformance) commitCompareAndSwapRejectsIntervention(t *testing.T) {
	fixture, resolver := suite.fresh(t, SetupBound)
	if err := commitCompareAndSwapError(fixture, resolver); err != nil {
		t.Fatalf("control-law commit-cas: %v", err)
	}
}

func commitCompareAndSwapError(fixture KernelConformance, resolver kernel.Runtime) error {
	transition := fixture.Scenario.AdvanceTransitions[0]
	request := kernel.ResolveRequest{InstanceID: fixture.Scenario.InstanceID, Objective: &fixture.Scenario.Objective, Authority: fixture.Scenario.Authority, Requested: transition}
	resolution, err := resolveWithoutMutation(context.Background(), resolver, fixture.Scenario, request)
	if err != nil || resolution.Decision.Kind != kernel.Prescribed || resolution.Prescription == nil {
		return fmt.Errorf("resolve commit-CAS transition: decision=%#v error=%v", resolution.Decision, err)
	}
	operator := afterExecuteOperator{Operator: fixture.Operator, after: fixture.Scenario.BumpStateRevision}
	runtime, err := kernel.NewRuntime(fixture.Program, fixture.Domain, operator, fixture.CapabilityClassifier, fixture.Store, fixture.Locker, fixture.Clock)
	if err != nil {
		return err
	}
	before := fixture.Scenario.Snapshot()
	_, applyErr := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: *resolution.Prescription})
	after := fixture.Scenario.Snapshot()
	if !kernel.IsRecoveryRequired(applyErr) {
		return fmt.Errorf("intervening state was not rejected at commit: %v", applyErr)
	}
	if after.CommitCount != before.CommitCount || !receiptHistoryEqual(after.Receipts, before.Receipts) || effectCount(after, transition) != effectCount(before, transition)+1 {
		return fmt.Errorf("commit refusal changed committed history or effect evidence: before=%#v after=%#v", before, after)
	}
	if after.State.Mode != before.State.Mode || after.State.Recovery == nil || after.State.Revision != before.State.Revision+2 {
		return fmt.Errorf("commit refusal did not preserve the intervening unresolved state: before=%#v after=%#v", before.State, after.State)
	}
	return nil
}

type afterExecuteOperator struct {
	kernel.Operator
	after func()
}

func (o afterExecuteOperator) Execute(ctx context.Context, operation kernel.Operation) (kernel.Effect, error) {
	effect, err := o.Operator.Execute(ctx, operation)
	if err == nil {
		o.after()
	}
	return effect, err
}

func concurrentApplyError(fixture KernelConformance, resolver kernel.Runtime) error {
	transition := fixture.Scenario.AdvanceTransitions[0]
	request := kernel.ResolveRequest{InstanceID: fixture.Scenario.InstanceID, Objective: &fixture.Scenario.Objective, Authority: fixture.Scenario.Authority, Requested: transition}
	resolution, err := resolveWithoutMutation(context.Background(), resolver, fixture.Scenario, request)
	if err != nil || resolution.Decision.Kind != kernel.Prescribed || resolution.Prescription == nil {
		return fmt.Errorf("resolve concurrent transition: decision=%#v error=%v", resolution.Decision, err)
	}
	prescription := *resolution.Prescription
	before := fixture.Scenario.Snapshot()
	storeBarrier := newTwoPartyBarrier()
	domainBarrier := newTwoPartyBarrier()
	store := &sameBaseLoadStore{Store: fixture.Store, barrier: storeBarrier}
	domain := &sameBaseObservationDomain{Domain: fixture.Domain, barrier: domainBarrier}
	runtimeA, err := kernel.NewRuntime(fixture.Program, domain, fixture.Operator, fixture.CapabilityClassifier, store, fixture.Scenario.IndependentLocker(), fixture.Clock)
	if err != nil {
		return err
	}
	runtimeB, err := kernel.NewRuntime(fixture.Program, domain, fixture.Operator, fixture.CapabilityClassifier, store, fixture.Scenario.IndependentLocker(), fixture.Clock)
	if err != nil {
		return err
	}
	start := make(chan struct{})
	type applyResult struct {
		receipt kernel.Receipt
		err     error
	}
	results := make(chan applyResult, 2)
	for _, runtime := range []kernel.Runtime{runtimeA, runtimeB} {
		runtime := runtime
		go func() {
			<-start
			result := applyResult{}
			defer func() {
				if recovered := recover(); recovered != nil {
					storeBarrier.cancel()
					domainBarrier.cancel()
					result.err = fmt.Errorf("concurrent Apply panicked: %v", recovered)
				}
				results <- result
			}()
			result.receipt, result.err = runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
			if result.err != nil {
				storeBarrier.cancel()
				domainBarrier.cancel()
			}
		}()
	}
	close(start)
	successes, refusals := 0, 0
	var winner kernel.Receipt
	for range 2 {
		result := <-results
		if result.err == nil {
			successes++
			winner = result.receipt
		} else {
			refusals++
		}
	}
	after := fixture.Scenario.Snapshot()
	if successes != 1 || refusals != 1 || after.CommitCount != before.CommitCount+1 || len(after.Receipts) != len(before.Receipts)+1 || effectCount(after, transition) != effectCount(before, transition)+1 {
		return fmt.Errorf("success/refusal=%d/%d snapshot=%#v", successes, refusals, after)
	}
	if err := committedOutcomeError(fixture.Program, fixture.Scenario, before, after, prescription, winner); err != nil {
		return fmt.Errorf("durable outcome: %w", err)
	}
	return nil
}

type twoPartyBarrier struct {
	mu       sync.Mutex
	arrivals int
	ready    chan struct{}
	canceled chan struct{}
	once     sync.Once
}

func newTwoPartyBarrier() *twoPartyBarrier {
	return &twoPartyBarrier{ready: make(chan struct{}), canceled: make(chan struct{})}
}

func (b *twoPartyBarrier) wait() error {
	b.mu.Lock()
	b.arrivals++
	if b.arrivals == 2 {
		close(b.ready)
	}
	complete := b.arrivals >= 2
	b.mu.Unlock()
	if complete {
		return nil
	}
	select {
	case <-b.ready:
		return nil
	case <-b.canceled:
		b.mu.Lock()
		complete = b.arrivals >= 2
		b.mu.Unlock()
		if complete {
			return nil
		}
		return fmt.Errorf("concurrent admission was canceled")
	}
}

func (b *twoPartyBarrier) cancel() {
	b.once.Do(func() { close(b.canceled) })
}

type sameBaseLoadStore struct {
	kernel.Store
	barrier *twoPartyBarrier
}

func (s *sameBaseLoadStore) Load(ctx context.Context, instanceID string) (kernel.ControlState, error) {
	state, err := s.Store.Load(ctx, instanceID)
	barrierErr := s.barrier.wait()
	if err != nil {
		return kernel.ControlState{}, err
	}
	if barrierErr != nil {
		return kernel.ControlState{}, barrierErr
	}
	return state, nil
}

type sameBaseObservationDomain struct {
	kernel.Domain
	barrier *twoPartyBarrier
}

func (d *sameBaseObservationDomain) Observe(ctx context.Context, instanceID string) (kernel.Observation, error) {
	observation, err := d.Domain.Observe(ctx, instanceID)
	barrierErr := d.barrier.wait()
	if err != nil {
		return kernel.Observation{}, err
	}
	if barrierErr != nil {
		return kernel.Observation{}, barrierErr
	}
	return observation, nil
}

func (suite KernelConformance) programReachesMarkedState(t *testing.T) {
	fixture, runtime := suite.fresh(t, SetupUnbound)
	before := fixture.Scenario.Snapshot()
	receipts, err := runUntargetedToMarked(context.Background(), runtime, fixture.Program, fixture.Scenario, len(fixture.Scenario.AdvanceTransitions)+1)
	if err != nil {
		t.Fatal(err)
	}
	for _, receipt := range receipts {
		if receipt.Verification != "satisfied" || receipt.AttemptStateRevision != receipt.PriorStateRevision+1 || receipt.ResultStateRevision != receipt.AttemptStateRevision+1 {
			t.Fatalf("control-law receipt-completeness: %#v", receipt)
		}
	}
	after := fixture.Scenario.Snapshot()
	if !fixture.Program.Marked(after.State.Mode) || len(after.Receipts) != len(before.Receipts)+len(receipts) {
		t.Fatalf("control-law marked-reachability: snapshot=%#v", after)
	}
}

func runUntargetedToMarked(ctx context.Context, runtime kernel.Runtime, program kernel.Program, scenario Scenario, maxTransitions int) ([]kernel.Receipt, error) {
	request := kernel.ResolveRequest{InstanceID: scenario.InstanceID, Objective: &scenario.Objective, Authority: scenario.Authority}
	seen := map[string]bool{}
	receipts := make([]kernel.Receipt, 0, maxTransitions)
	for step := 0; ; step++ {
		resolution, err := resolveWithoutMutation(ctx, runtime, scenario, request)
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
		before := scenario.Snapshot()
		receipt, err := runtime.Apply(ctx, kernel.ApplyRequest{ResolveRequest: request, Prescription: *resolution.Prescription})
		if err != nil {
			return nil, fmt.Errorf("control-law marked-reachability: apply %s: %w", resolution.Decision.Transition, err)
		}
		if err := committedOutcomeError(program, scenario, before, scenario.Snapshot(), *resolution.Prescription, receipt); err != nil {
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
	if fixture.Domain == nil || fixture.Operator == nil || fixture.CapabilityClassifier == nil || fixture.Store == nil || fixture.Locker == nil || fixture.Clock == nil || fixture.Scenario.Snapshot == nil || fixture.Scenario.ChangeObservation == nil || fixture.Scenario.RebindObjective == nil || fixture.Scenario.BumpStateRevision == nil || fixture.Scenario.RetargetProgram == nil || fixture.Scenario.AdvanceClock == nil || fixture.Scenario.IndependentLocker == nil || fixture.Scenario.VerifyCommitted == nil || fixture.Scenario.InterruptNextOperator == nil || fixture.Scenario.PanicNextOperator == nil || fixture.Scenario.FailNextCommit == nil || fixture.Scenario.RetargetInstance == nil || fixture.Scenario.InstanceID == "" || fixture.Scenario.BindTransition == "" || len(fixture.Scenario.AdvanceTransitions) == 0 || fixture.Scenario.MaintenanceTransition == "" || fixture.Scenario.RecoveryTransition == "" || fixture.Scenario.RecoveryCapability.Validate() != nil || fixture.Scenario.ExtraCapability.Validate() != nil {
		t.Fatal("kernel conformance fixture is incomplete")
	}
	if fixture.Scenario.RevisedObjective.Validate() != nil || fixture.Scenario.RevisedObjective.ID != fixture.Scenario.Objective.ID || fixture.Scenario.RevisedObjective.Revision <= fixture.Scenario.Objective.Revision || fixture.Scenario.RevisedObjective.Fingerprint == fixture.Scenario.Objective.Fingerprint {
		t.Fatal("kernel conformance revised objective must preserve identity while increasing revision and changing fingerprint")
	}
	if fixture.Scenario.AlternateProgram.Validate() != nil || fixture.Scenario.AlternateProgram.ID != fixture.Program.ID || fixture.Scenario.AlternateProgram.Version != fixture.Program.Version || fixture.Scenario.AlternateProgram.Fingerprint == fixture.Program.Fingerprint {
		t.Fatal("kernel conformance alternate program must preserve ID and version while changing fingerprint")
	}
	return fixture
}

func resolve(t testing.TB, runtime kernel.Runtime, scenario Scenario, transition string, objective *kernel.Objective, authority kernel.Authority) (kernel.ResolveRequest, kernel.Prescription) {
	t.Helper()
	request := kernel.ResolveRequest{InstanceID: scenario.InstanceID, Objective: objective, Authority: authority, Requested: transition}
	resolution, err := resolveWithoutMutation(context.Background(), runtime, scenario, request)
	if err != nil || resolution.Decision.Kind != kernel.Prescribed || resolution.Prescription == nil {
		t.Fatalf("resolve %s: decision=%#v error=%v", transition, resolution.Decision, err)
	}
	return request, *resolution.Prescription
}

func resolveAndApply(t testing.TB, runtime kernel.Runtime, program kernel.Program, scenario Scenario, transition string, objective *kernel.Objective) kernel.Receipt {
	t.Helper()
	request, prescription := resolve(t, runtime, scenario, transition, objective, scenario.Authority)
	return applyAndRequireCommit(t, runtime, program, scenario, request, prescription)
}

func applyAndRequireCommit(t testing.TB, runtime kernel.Runtime, program kernel.Program, scenario Scenario, request kernel.ResolveRequest, prescription kernel.Prescription) kernel.Receipt {
	t.Helper()
	before := scenario.Snapshot()
	receipt, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	if err != nil {
		t.Fatalf("apply %s: %v", prescription.TransitionID, err)
	}
	if err := committedOutcomeError(program, scenario, before, scenario.Snapshot(), prescription, receipt); err != nil {
		t.Fatalf("apply %s durable outcome: %v", prescription.TransitionID, err)
	}
	return receipt
}

func committedOutcomeError(program kernel.Program, scenario Scenario, before, after Snapshot, prescription kernel.Prescription, returned kernel.Receipt) error {
	if err := returned.Validate(); err != nil {
		return fmt.Errorf("returned receipt is invalid: %w", err)
	}
	if after.CommitCount != before.CommitCount+1 || len(after.Receipts) != len(before.Receipts)+1 {
		return fmt.Errorf("commit or durable receipt count did not advance exactly once")
	}
	if !receiptHistoryEqual(after.Receipts[:len(before.Receipts)], before.Receipts) {
		return fmt.Errorf("successful Apply changed prior receipt history")
	}
	durable := after.Receipts[len(before.Receipts)]
	if err := durable.Validate(); err != nil {
		return fmt.Errorf("durable receipt is invalid: %w", err)
	}
	if !reflect.DeepEqual(durable, returned) {
		return fmt.Errorf("durable receipt differs from returned receipt")
	}
	if returned.InstanceID != before.State.InstanceID || returned.Program != before.State.Program || returned.PrescriptionID != prescription.ID || returned.TransitionID != prescription.TransitionID {
		return fmt.Errorf("receipt identity differs from the applied prescription and prior state")
	}
	if returned.PriorStateRevision != before.State.Revision || returned.AttemptStateRevision != before.State.Revision+1 || returned.ResultStateRevision != before.State.Revision+2 {
		return fmt.Errorf("receipt revisions differ from the committed state sequence")
	}
	if returned.PriorObservation != before.Observation.Fingerprint || returned.PriorObservation != prescription.ExpectedSnapshotFingerprint {
		return fmt.Errorf("receipt prior observation differs from the prescribed pre-transition observation")
	}
	if returned.AuthorityFingerprint != prescription.AuthorityFingerprint || !reflect.DeepEqual(returned.Capabilities, prescription.RequiredCapabilities) {
		return fmt.Errorf("receipt authority differs from the applied prescription")
	}
	if err := after.Observation.Validate(); err != nil || after.Observation.Fingerprint != returned.ResultObservation {
		return fmt.Errorf("durable receipt result observation differs from the domain")
	}
	transition, ok := program.Transition(returned.TransitionID)
	if !ok {
		return fmt.Errorf("returned receipt transition is absent from program")
	}
	if after.State.InstanceID != returned.InstanceID || after.State.Program != returned.Program || after.State.Revision != returned.ResultStateRevision || after.State.Mode != transition.TargetMode || after.State.Recovery != nil || !reflect.DeepEqual(after.State.ObjectiveBinding, returned.ObjectiveBinding) {
		return fmt.Errorf("durable state differs from winning receipt outcome")
	}
	if err := exactEffectDelta(before.Effects, after.Effects, returned.TransitionID, 1); err != nil {
		return fmt.Errorf("successful Apply effect evidence is invalid: %w", err)
	}
	if err := scenario.VerifyCommitted(before, after, returned); err != nil {
		return fmt.Errorf("independent domain outcome verification failed: %w", err)
	}
	return nil
}

func unresolvedAttemptError(before, after Snapshot, prescription kernel.Prescription) error {
	if after.CommitCount != before.CommitCount || !receiptHistoryEqual(after.Receipts, before.Receipts) {
		return fmt.Errorf("failed attempt changed committed history")
	}
	if err := exactEffectDelta(before.Effects, after.Effects, prescription.TransitionID, 1); err != nil {
		return fmt.Errorf("failed attempt effect evidence is invalid: %w", err)
	}
	if err := after.State.Validate(); err != nil {
		return fmt.Errorf("durable attempt state is invalid: %w", err)
	}
	if after.State.InstanceID != before.State.InstanceID || after.State.Program != before.State.Program || after.State.Mode != before.State.Mode || !reflect.DeepEqual(after.State.ObjectiveBinding, before.State.ObjectiveBinding) || after.State.Revision != before.State.Revision+1 {
		return fmt.Errorf("durable attempt differs from pre-effect control state")
	}
	if before.State.Recovery != nil {
		if !reflect.DeepEqual(after.State.Recovery, before.State.Recovery) {
			return fmt.Errorf("durable attempt changed the existing recovery obligation")
		}
		return nil
	}
	if after.State.Recovery == nil || after.State.Recovery.PrescriptionID != prescription.ID || after.State.Recovery.TransitionID != prescription.TransitionID {
		return fmt.Errorf("durable attempt does not bind the failed prescription")
	}
	return nil
}

func refusedApplyMutationError(before, after Snapshot, _ string) error {
	if err := unchangedSnapshotError(before, after); err != nil {
		return fmt.Errorf("refused Apply mutated control state, history, or effects: %w", err)
	}
	return nil
}

func unchangedSnapshotError(before, after Snapshot) error {
	if !reflect.DeepEqual(after.State, before.State) || !reflect.DeepEqual(after.Observation, before.Observation) || after.CommitCount != before.CommitCount || !receiptHistoryEqual(after.Receipts, before.Receipts) {
		return fmt.Errorf("control state, observation, or committed history changed")
	}
	if err := exactEffectDelta(before.Effects, after.Effects, "", 0); err != nil {
		return fmt.Errorf("effect evidence changed: %w", err)
	}
	return nil
}

func resolveWithoutMutation(ctx context.Context, runtime kernel.Runtime, scenario Scenario, request kernel.ResolveRequest) (kernel.Resolution, error) {
	before := scenario.Snapshot()
	resolution, err := runtime.Resolve(ctx, request)
	if err != nil {
		return resolution, err
	}
	if unchangedErr := unchangedSnapshotError(before, scenario.Snapshot()); unchangedErr != nil {
		return resolution, fmt.Errorf("Resolve mutated deterministic fixture evidence: %w", unchangedErr)
	}
	return resolution, nil
}

func resolveUntargetedRecovery(ctx context.Context, runtime kernel.Runtime, scenario Scenario) (kernel.ResolveRequest, kernel.Prescription, error) {
	request := kernel.ResolveRequest{InstanceID: scenario.InstanceID, Objective: &scenario.Objective, Authority: scenario.Authority}
	resolution, err := resolveWithoutMutation(ctx, runtime, scenario, request)
	if err != nil {
		return request, kernel.Prescription{}, err
	}
	if resolution.Decision.Kind != kernel.Prescribed || resolution.Prescription == nil || resolution.Decision.Transition != scenario.RecoveryTransition {
		return request, kernel.Prescription{}, fmt.Errorf("untargeted recovery selected %q with decision %s, want %q", resolution.Decision.Transition, resolution.Decision.Kind, scenario.RecoveryTransition)
	}
	return request, *resolution.Prescription, nil
}

func exactEffectDelta(before, after map[string]int, transition string, delta int) error {
	keys := make(map[string]struct{}, len(before)+len(after))
	for key := range before {
		keys[key] = struct{}{}
	}
	for key := range after {
		keys[key] = struct{}{}
	}
	for key := range keys {
		expected := before[key]
		if key == transition {
			expected += delta
		}
		if after[key] != expected {
			return fmt.Errorf("transition %q changed from %d to %d, want %d", key, before[key], after[key], expected)
		}
	}
	return nil
}

func receiptHistoryEqual(left, right []kernel.Receipt) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !reflect.DeepEqual(left[index], right[index]) {
			return false
		}
	}
	return true
}

func withoutCapability(authority kernel.Authority, removed kernel.Capability) kernel.Authority {
	filtered := kernel.Authority{Receipts: make([]kernel.AuthorityReceipt, 0, len(authority.Receipts))}
	for _, receipt := range authority.Receipts {
		copy := receipt
		copy.Capabilities = make([]kernel.Capability, 0, len(receipt.Capabilities))
		for _, capability := range receipt.Capabilities {
			if capability != removed {
				copy.Capabilities = append(copy.Capabilities, capability)
			}
		}
		if len(copy.Capabilities) != 0 {
			filtered.Receipts = append(filtered.Receipts, copy)
		}
	}
	return filtered
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
