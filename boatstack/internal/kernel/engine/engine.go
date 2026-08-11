package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/supervisor"
)

type Engine struct {
	registry           catalog.Registry
	control            supervisor.Supervisor
	programFingerprint string
	observer           ports.Observer
	clock              ports.Clock
	locker             ports.Locker
	journal            ports.Journal
	effects            ports.EffectDriver
	receipts           ports.ReceiptStore
}

func New(registry catalog.Registry, contracts catalog.GoalContracts, programFingerprint string, observer ports.Observer, clock ports.Clock, locker ports.Locker, journal ports.Journal, effects ports.EffectDriver, receipts ports.ReceiptStore) (Engine, error) {
	if registry.Len() == 0 || len(contracts) == 0 || len(programFingerprint) != 64 || observer == nil || clock == nil || locker == nil || journal == nil || effects == nil || receipts == nil {
		return Engine{}, fmt.Errorf("kernel engine requires registry, goal contracts, observer, clock, locker, journal, effects, and receipt store")
	}
	return Engine{registry: registry, control: supervisor.New(registry, contracts), programFingerprint: programFingerprint, observer: observer, clock: clock, locker: locker, journal: journal, effects: effects, receipts: receipts}, nil
}

func (e Engine) canonicalize(observation model.Observation) (model.Snapshot, error) {
	return model.CanonicalizeForProgram(observation, e.programFingerprint)
}

type ResolveRequest struct {
	Invocation model.InvocationContext
	Goal       model.Goal
	Authority  protocol.AuthorityBundle
	Requested  catalog.TransitionID
}

type Resolution struct {
	Snapshot model.Snapshot
	Goal     model.Goal
	Decision supervisor.Decision
}

func (e Engine) Resolve(ctx context.Context, request ResolveRequest) (Resolution, error) {
	if err := request.Invocation.Validate(false); err != nil {
		return unresolvedResolution(request.Goal, "invocation identity is invalid"), err
	}
	observation, err := e.observer.Observe(ctx, ports.ObservationRequest{Invocation: request.Invocation})
	if err != nil {
		return unresolvedResolution(request.Goal, "required observation failed"), fmt.Errorf("observe plant: %w", err)
	}
	snapshot, err := e.canonicalize(observation)
	if err != nil {
		return unresolvedResolution(request.Goal, "canonical observation is invalid"), fmt.Errorf("canonicalize observation: %w", err)
	}
	if snapshot.Invocation != request.Invocation {
		return Resolution{}, fmt.Errorf("observer returned a different invocation identity")
	}
	goal := request.Goal
	if err := goal.Validate(); err != nil {
		if snapshot.Goal.Status != model.FactKnown {
			return Resolution{}, fmt.Errorf("no valid requested or configured goal: %w", err)
		}
		goal = snapshot.Goal.Value
	}
	now := e.clock.Now()
	if err := request.Authority.Validate(now); err != nil {
		return Resolution{}, err
	}
	decision := e.control.Resolve(snapshot, goal, request.Authority.Set(now), request.Requested)
	return Resolution{Snapshot: snapshot, Goal: goal, Decision: decision}, nil
}

func unresolvedResolution(goal model.Goal, reason string) Resolution {
	return Resolution{Goal: goal, Decision: supervisor.Decision{Kind: supervisor.DecisionUnresolved, Reason: reason}}
}

type ApplyRequest struct {
	ResolveRequest
	FlowID            string
	Parameters        protocol.Parameters
	IdempotencyKey    string
	AdmissionLifetime time.Duration
}

type ApplyResult struct {
	Source    model.Snapshot
	Target    model.Snapshot
	Goal      model.Goal
	Decision  supervisor.Decision
	Admission protocol.Admission
	Receipt   protocol.TransitionReceipt
	Replayed  bool
}

type DecisionError struct{ Decision supervisor.Decision }

func (e DecisionError) Error() string {
	return fmt.Sprintf("kernel decision %s: %s", e.Decision.Kind, e.Decision.Reason)
}

type StaleAdmissionError struct{ Err error }

func (e StaleAdmissionError) Error() string { return "stale admission: " + e.Err.Error() }
func (e StaleAdmissionError) Unwrap() error { return e.Err }

type PostconditionError struct {
	Transition catalog.TransitionID
	Recovery   catalog.TransitionID
}

func (e PostconditionError) Error() string {
	return fmt.Sprintf("transition %q did not establish its target predicate; recovery %q is required", e.Transition, e.Recovery)
}

type ExternalOutcomeUnknownError struct {
	Transition catalog.TransitionID
	Recovery   catalog.TransitionID
}

type ReplayRecoveryError struct{ ReceiptID string }

func (e ReplayRecoveryError) Error() string {
	return fmt.Sprintf("idempotency receipt %q exists, but its transaction state still requires recovery", e.ReceiptID)
}

func (e ExternalOutcomeUnknownError) Error() string {
	return fmt.Sprintf("transition %q has an unknown external outcome; reconcile with %q", e.Transition, e.Recovery)
}

func (e Engine) Apply(ctx context.Context, request ApplyRequest) (result ApplyResult, returnErr error) {
	if request.FlowID == "" {
		return result, fmt.Errorf("kernel apply requires flow identity")
	}
	if request.IdempotencyKey != "" {
		prior, ok, err := e.receipts.FindByIdempotency(ctx, request.Invocation, request.IdempotencyKey)
		if err != nil {
			return result, fmt.Errorf("check supplied idempotency key: %w", err)
		}
		if ok {
			if err := validateReplayRequest(prior, request, e.programFingerprint); err != nil {
				return result, err
			}
			observation, observeErr := e.observer.Observe(ctx, ports.ObservationRequest{Invocation: request.Invocation})
			if observeErr != nil {
				return result, observeErr
			}
			snapshot, canonicalErr := e.canonicalize(observation)
			if canonicalErr != nil {
				return result, canonicalErr
			}
			if !replayStateSettled(snapshot) {
				return result, ReplayRecoveryError{ReceiptID: prior.ID}
			}
			result.Source, result.Target, result.Goal, result.Receipt, result.Replayed = snapshot, snapshot, request.Goal, prior, true
			if result.Goal.Validate() != nil && snapshot.Goal.Status == model.FactKnown {
				result.Goal = snapshot.Goal.Value
			}
			return result, nil
		}
	}
	if request.AdmissionLifetime <= 0 {
		request.AdmissionLifetime = 2 * time.Minute
	}
	resolution, err := e.Resolve(ctx, request.ResolveRequest)
	result.Source, result.Goal, result.Decision = resolution.Snapshot, resolution.Goal, resolution.Decision
	if err != nil {
		return result, err
	}
	request.Goal = resolution.Goal
	if resolution.Decision.Kind != supervisor.DecisionPrescribed || resolution.Decision.Transition == nil {
		return result, DecisionError{Decision: resolution.Decision}
	}
	transition := *resolution.Decision.Transition
	now := e.clock.Now()
	admission, err := protocol.NewAdmission(resolution.Snapshot, request.Goal, transition, request.Authority, request.Parameters, now, request.AdmissionLifetime)
	if err != nil {
		return result, err
	}
	result.Admission = admission
	if request.IdempotencyKey != "" && request.IdempotencyKey != admission.IdempotencyKey {
		return result, fmt.Errorf("supplied idempotency key does not match the exact admitted request")
	}
	if err := e.receipts.Bind(ctx, request.FlowID, admission); err != nil {
		return result, err
	}
	defer e.receipts.Unbind(request.FlowID)
	if prior, ok, err := e.receipts.FindByIdempotency(ctx, request.Invocation, admission.IdempotencyKey); err != nil {
		return result, fmt.Errorf("check idempotency: %w", err)
	} else if ok {
		if err := validateReplayRequest(prior, request, e.programFingerprint); err != nil {
			return result, err
		}
		observation, observeErr := e.observer.Observe(ctx, ports.ObservationRequest{Invocation: request.Invocation})
		if observeErr != nil {
			return result, observeErr
		}
		current, canonicalErr := e.canonicalize(observation)
		if canonicalErr != nil {
			return result, canonicalErr
		}
		if !replayStateSettled(current) {
			return result, ReplayRecoveryError{ReceiptID: prior.ID}
		}
		result.Target, result.Receipt, result.Replayed = current, prior, true
		return result, nil
	}

	lock, err := e.locker.Acquire(ctx, request.Invocation, transition.OwnedResources)
	if err != nil {
		return result, fmt.Errorf("acquire effect lock: %w", err)
	}
	defer func() {
		if releaseErr := lock.Release(); releaseErr != nil && returnErr == nil {
			returnErr = fmt.Errorf("release effect lock: %w", releaseErr)
		}
	}()

	lockedObservation, err := e.observer.Observe(ctx, ports.ObservationRequest{Invocation: request.Invocation})
	if err != nil {
		return result, err
	}
	lockedSnapshot, err := e.canonicalize(lockedObservation)
	if err != nil {
		return result, err
	}
	if prior, ok, findErr := e.receipts.FindByIdempotency(ctx, request.Invocation, admission.IdempotencyKey); findErr != nil {
		return result, fmt.Errorf("check locked idempotency: %w", findErr)
	} else if ok {
		if err := validateReplayRequest(prior, request, e.programFingerprint); err != nil {
			return result, err
		}
		if !replayStateSettled(lockedSnapshot) {
			return result, ReplayRecoveryError{ReceiptID: prior.ID}
		}
		result.Target, result.Receipt, result.Replayed = lockedSnapshot, prior, true
		return result, nil
	}
	if err := admission.ValidateCurrent(lockedSnapshot, request.Goal, transition, e.clock.Now()); err != nil {
		return result, StaleAdmissionError{Err: err}
	}
	if err := e.journal.Begin(ctx, admission, transition); err != nil {
		return result, fmt.Errorf("begin transaction journal: %w", err)
	}
	abort := func(reason string, cause error) error {
		abortErr := e.journal.Abort(ctx, admission.ID, reason)
		if abortErr != nil {
			return errors.Join(cause, fmt.Errorf("abort transaction journal: %w", abortErr))
		}
		return cause
	}
	requireRecovery := func(reason string, cause error) error {
		recoveryErr := e.journal.RequireRecovery(ctx, admission.ID, reason)
		if recoveryErr != nil {
			return errors.Join(cause, fmt.Errorf("mark transaction recovery-required: %w", recoveryErr))
		}
		return cause
	}
	if err := e.journal.Mark(ctx, admission.ID, "executing"); err != nil {
		return result, abort("journal mark failed", err)
	}
	prepared, err := e.effects.Prepare(ctx, admission, transition)
	if err != nil {
		return result, abort("effect preparation failed", err)
	}
	if err := e.journal.Stage(ctx, admission.ID, prepared.Manifest()); err != nil {
		return result, abort("effect staging journal failed", err)
	}
	startedAt := e.clock.Now()
	effectResult, effectErr := prepared.Execute(ctx)
	if effectErr != nil {
		rollbackErr := prepared.Rollback(ctx)
		if rollbackErr != nil {
			return result, requireRecovery("effect and rollback failed", errors.Join(effectErr, rollbackErr))
		}
		return result, abort("effect failed and rolled back", effectErr)
	}
	if err := e.journal.Mark(ctx, admission.ID, "verifying"); err != nil {
		return result, requireRecovery("journal mark failed after effect", err)
	}
	verificationInvocation := request.Invocation
	if transferred, ok := prepared.VerificationInvocation(); ok {
		verificationInvocation = transferred
	}
	targetObservation, err := e.observer.Observe(ctx, ports.ObservationRequest{Invocation: verificationInvocation, IgnoreAdmissionID: admission.ID, VerifyTransitionID: transition.ID})
	if err != nil {
		return result, requireRecovery("target observation failed after effect", err)
	}
	target, err := e.canonicalize(targetObservation)
	if err != nil {
		return result, requireRecovery("target canonicalization failed after effect", err)
	}
	result.Target = target
	if !sameInvocation(target.Invocation, admission.Invocation, transition) {
		return result, requireRecovery("target identity changed", PostconditionError{Transition: transition.ID, Recovery: transition.Interruption.Recovery})
	}
	if !transition.TargetMatches(target) {
		if effectResult.Settlement == ports.EffectUnknown {
			unknown := ExternalOutcomeUnknownError{Transition: transition.ID, Recovery: transition.Interruption.Recovery}
			return result, requireRecovery("external outcome unknown", unknown)
		}
		rollbackErr := prepared.Rollback(ctx)
		postcondition := PostconditionError{Transition: transition.ID, Recovery: transition.Interruption.Recovery}
		if rollbackErr != nil {
			return result, requireRecovery("postcondition and rollback failed", errors.Join(postcondition, rollbackErr))
		}
		return result, abort("postcondition failed and effect rolled back", postcondition)
	}
	sequence, err := e.receipts.NextSequence(ctx, request.FlowID)
	if err != nil {
		return result, requireRecovery("sequence allocation failed after verified effect", err)
	}
	completedAt := e.clock.Now()
	receipt, err := protocol.NewReceipt(request.FlowID, sequence, admission, transition, target, startedAt, completedAt, protocol.OutcomeSucceeded, "")
	if err != nil {
		return result, requireRecovery("receipt construction failed after verified effect", err)
	}
	if err := e.receipts.Append(ctx, receipt); err != nil {
		return result, requireRecovery("receipt append failed after verified effect", err)
	}
	if err := e.journal.Commit(ctx, receipt); err != nil {
		return result, requireRecovery("journal commit failed after receipt", err)
	}
	result.Receipt = receipt
	return result, nil
}

func validateReplayRequest(prior protocol.TransitionReceipt, request ApplyRequest, programFingerprint string) error {
	if prior.ProgramFingerprint != programFingerprint {
		return fmt.Errorf("idempotency receipt belongs to a different control program")
	}
	if prior.FlowID != request.FlowID {
		return fmt.Errorf("idempotency receipt belongs to flow %q, not %q", prior.FlowID, request.FlowID)
	}
	if request.Goal.Validate() == nil && (prior.GoalID != request.Goal.ID || prior.GoalKind != request.Goal.Kind || prior.DeliveryID != request.Goal.DeliveryID) {
		return fmt.Errorf("idempotency receipt belongs to a different configured goal")
	}
	if request.Requested != "" && prior.TransitionID != request.Requested {
		return fmt.Errorf("idempotency receipt belongs to transition %q, not %q", prior.TransitionID, request.Requested)
	}
	return nil
}

func replayStateSettled(snapshot model.Snapshot) bool {
	return snapshot.Phase.Status == model.FactKnown && snapshot.Phase.Value != model.PhaseRecovery &&
		snapshot.Recovery.Status == model.FactKnown && snapshot.Recovery.Value == model.RecoveryNone &&
		snapshot.Transaction.Status == model.FactKnown && snapshot.Transaction.Value == model.TransactionNone
}

func sameInvocation(target, source model.InvocationContext, transition catalog.Transition) bool {
	if target == source {
		return true
	}
	if transition.AllowsWorktreeTransfer {
		return target.RepositoryID == source.RepositoryID && target.GitCommonID == source.GitCommonID &&
			target.ControllerID == source.ControllerID && target.Topology == source.Topology &&
			target.Host == source.Host && target.Correlation == source.Correlation
	}
	if transition.AllowsIdentityRebind {
		return target.RepositoryID == source.RepositoryID && target.GitCommonID == source.GitCommonID &&
			target.WorktreeID == source.WorktreeID && target.Ref == source.Ref && target.InvokingPath == source.InvokingPath &&
			target.Host == source.Host && target.Correlation == source.Correlation
	}
	return false
}
