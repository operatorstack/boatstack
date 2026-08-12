package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/supervisor"
	general "github.com/operatorstack/boatstack/boatstack/kernel"
)

type Engine struct {
	registry catalog.Registry
	control  supervisor.Supervisor
	program  protocol.ProgramIdentity
	observer ports.Observer
	clock    ports.Clock
	locker   ports.Locker
	journal  ports.Journal
	effects  ports.EffectDriver
	receipts ports.ReceiptStore
}

func New(registry catalog.Registry, contracts catalog.ObjectiveContracts, program protocol.ProgramIdentity, observer ports.Observer, clock ports.Clock, locker ports.Locker, journal ports.Journal, effects ports.EffectDriver, receipts ports.ReceiptStore) (Engine, error) {
	if registry.Len() == 0 || len(contracts) == 0 || program.Validate() != nil || observer == nil || clock == nil || locker == nil || journal == nil || effects == nil || receipts == nil {
		return Engine{}, fmt.Errorf("kernel engine requires registry, objective contracts, observer, clock, locker, journal, effects, and receipt store")
	}
	return Engine{registry: registry, control: supervisor.New(registry, contracts), program: program, observer: observer, clock: clock, locker: locker, journal: journal, effects: effects, receipts: receipts}, nil
}

func (e Engine) canonicalize(observation model.Observation) (model.Snapshot, error) {
	return model.CanonicalizeForProgram(observation, e.program.Fingerprint)
}

type ResolveRequest struct {
	Invocation model.InvocationContext
	Objective  model.Objective
	Authority  protocol.AuthorityBundle
	Parameters protocol.Parameters
	Requested  catalog.TransitionID
}

type Resolution struct {
	Snapshot     model.Snapshot
	Objective    model.Objective
	Decision     supervisor.Decision
	Prescription protocol.Prescription
	Admission    protocol.Admission
}

func (e Engine) Resolve(ctx context.Context, request ResolveRequest) (Resolution, error) {
	if err := request.Invocation.Validate(false); err != nil {
		return unresolvedResolution(request.Objective, "invocation identity is invalid"), err
	}
	now := e.clock.Now()
	if err := request.Authority.Validate(now); err != nil {
		return Resolution{}, err
	}
	observation, err := e.observer.Observe(ctx, ports.ObservationRequest{Invocation: request.Invocation, Capabilities: request.Authority.GrantedCapabilities(now)})
	if err != nil {
		return unresolvedResolution(request.Objective, "required observation failed"), fmt.Errorf("observe plant: %w", err)
	}
	snapshot, err := e.canonicalize(observation)
	if err != nil {
		return unresolvedResolution(request.Objective, "canonical observation is invalid"), fmt.Errorf("canonicalize observation: %w", err)
	}
	if snapshot.Invocation != request.Invocation {
		return Resolution{}, fmt.Errorf("observer returned a different invocation identity")
	}
	objective := request.Objective
	if transition, requested := e.registry.Lookup(request.Requested); requested && transition.Policy.ObjectiveScope == catalog.ObjectiveScopeOptionalPreserve {
		objective, err = protocol.ObjectiveForTransition(snapshot, request.Objective, transition)
		if err != nil {
			return Resolution{}, err
		}
	} else if err := objective.Validate(); err != nil {
		switch snapshot.Objective.Status {
		case model.FactKnown:
			objective = snapshot.Objective.Value
		case model.FactAbsent:
			objective = model.Objective{}
		default:
			return Resolution{}, fmt.Errorf("no valid requested or configured objective: %w", err)
		}
	}
	decision := e.control.Resolve(snapshot, objective, request.Authority.Set(now), request.Requested)
	if decision.Kind == supervisor.DecisionPrescribed && decision.Transition != nil {
		objective, err = protocol.ObjectiveForTransition(snapshot, objective, *decision.Transition)
		if err != nil {
			decision.Kind = supervisor.DecisionRefused
			decision.Reason = err.Error()
			decision.Transition = nil
			return Resolution{Snapshot: snapshot, Objective: objective, Decision: decision}, nil
		}
		if applicabilityErr := protocol.ValidateApplicability(snapshot, objective, *decision.Transition, request.Authority, request.Parameters, now); applicabilityErr != nil {
			if protocol.IsMissingParameter(applicabilityErr) {
				decision.Kind = supervisor.DecisionCandidate
				decision.Reason = applicabilityErr.Error() + "; bind the declared parameters and re-resolve this transition"
				decision.Candidates = []catalog.TransitionID{decision.Transition.ID}
			} else {
				decision.Kind = supervisor.DecisionRefused
				decision.Reason = applicabilityErr.Error()
				decision.Transition = nil
			}
		} else {
			capabilities, capabilityErr := protocol.ProjectCapabilities(snapshot, *decision.Transition, request.Authority, now)
			if capabilityErr != nil {
				decision.Kind = supervisor.DecisionRefused
				decision.Reason = capabilityErr.Error()
				decision.Transition = nil
				return Resolution{Snapshot: snapshot, Objective: objective, Decision: decision}, nil
			}
			prescription, prescriptionErr := protocol.NewPrescription(snapshot, *decision.Transition, capabilities)
			if prescriptionErr != nil {
				decision.Kind = supervisor.DecisionUnresolved
				decision.Reason = prescriptionErr.Error()
				decision.Transition = nil
				return Resolution{Snapshot: snapshot, Objective: objective, Decision: decision}, nil
			}
			admission, admissionErr := protocol.NewAdmission(snapshot, objective, *decision.Transition, prescription, request.Authority, request.Parameters, now, 2*time.Minute)
			if admissionErr != nil {
				decision.Kind = supervisor.DecisionUnresolved
				decision.Reason = admissionErr.Error()
				decision.Transition = nil
			} else if _, preflightErr := e.effects.Prepare(ctx, admission, *decision.Transition); preflightErr != nil {
				decision.Kind = supervisor.DecisionUnresolved
				decision.Reason = fmt.Sprintf("transition %q failed deterministic effect preflight: %v", admission.TransitionID, preflightErr)
				decision.Transition = nil
			} else {
				return Resolution{Snapshot: snapshot, Objective: objective, Decision: decision, Prescription: prescription, Admission: admission}, nil
			}
		}
	}
	return Resolution{Snapshot: snapshot, Objective: objective, Decision: decision}, nil
}

func unresolvedResolution(objective model.Objective, reason string) Resolution {
	return Resolution{Objective: objective, Decision: supervisor.Decision{Kind: supervisor.DecisionUnresolved, Reason: reason}}
}

type ApplyRequest struct {
	ResolveRequest
	FlowID            string
	Prescription      protocol.Prescription
	Parameters        protocol.Parameters
	IdempotencyKey    string
	AdmissionLifetime time.Duration
}

type ApplyResult struct {
	Source    model.Snapshot
	Target    model.Snapshot
	Objective model.Objective
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

type StalePrescriptionError struct {
	PrescriptionID             string
	ExpectedStateRevision      uint64
	ObservedStateRevision      uint64
	ExpectedProgramFingerprint string
	ObservedProgramFingerprint string
	SnapshotChanged            bool
	ObjectiveBindingChanged    bool
	AuthorityChanged           bool
}

func (e StalePrescriptionError) Error() string {
	facets := make([]string, 0, 3)
	if e.ExpectedStateRevision != e.ObservedStateRevision {
		facets = append(facets, fmt.Sprintf("state revision %d != %d", e.ExpectedStateRevision, e.ObservedStateRevision))
	}
	if e.ExpectedProgramFingerprint != e.ObservedProgramFingerprint {
		facets = append(facets, "control program changed")
	}
	if e.SnapshotChanged {
		facets = append(facets, "admission-relevant snapshot changed")
	}
	if e.ObjectiveBindingChanged {
		facets = append(facets, "objective binding changed")
	}
	if e.AuthorityChanged {
		facets = append(facets, "authority or capability context changed")
	}
	return fmt.Sprintf("STALE_PRESCRIPTION %q: %s; re-resolve before apply", e.PrescriptionID, strings.Join(facets, ", "))
}

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
	if err := request.Prescription.Validate(); err != nil {
		return result, err
	}
	if request.Requested != request.Prescription.TransitionID {
		return result, fmt.Errorf("apply transition does not match prescription")
	}
	if request.IdempotencyKey != "" {
		prior, ok, err := e.receipts.FindByIdempotency(ctx, request.Invocation, request.IdempotencyKey)
		if err != nil {
			return result, fmt.Errorf("check supplied idempotency key: %w", err)
		}
		if ok {
			if err := validateReplayRequest(prior, request, e.program.Fingerprint); err != nil {
				return result, err
			}
			observation, observeErr := e.observer.Observe(ctx, ports.ObservationRequest{Invocation: request.Invocation, Capabilities: request.Authority.GrantedCapabilities(e.clock.Now())})
			if observeErr != nil {
				return result, observeErr
			}
			snapshot, canonicalErr := e.canonicalize(observation)
			if canonicalErr != nil {
				return result, canonicalErr
			}
			if err := validateReplayObjectiveState(prior, snapshot); err != nil {
				return result, err
			}
			if !replayStateSettled(snapshot) {
				return result, ReplayRecoveryError{ReceiptID: prior.ID}
			}
			result.Source, result.Target, result.Objective, result.Receipt, result.Replayed = snapshot, snapshot, request.Objective, prior, true
			if result.Objective.Validate() != nil && snapshot.Objective.Status == model.FactKnown {
				result.Objective = snapshot.Objective.Value
			}
			return result, nil
		}
	}
	if request.AdmissionLifetime <= 0 {
		request.AdmissionLifetime = 2 * time.Minute
	}
	request.ResolveRequest.Parameters = request.Parameters
	resolution, err := e.Resolve(ctx, request.ResolveRequest)
	result.Source, result.Objective, result.Decision = resolution.Snapshot, resolution.Objective, resolution.Decision
	if err != nil {
		return result, err
	}
	if err := validatePrescriptionCurrent(request.Prescription, resolution.Snapshot); err != nil {
		return result, err
	}
	if resolution.Prescription.ID != request.Prescription.ID {
		return result, StalePrescriptionError{
			PrescriptionID: request.Prescription.ID, ExpectedStateRevision: request.Prescription.ExpectedStateRevision,
			ObservedStateRevision: resolution.Snapshot.StateRevision, ExpectedProgramFingerprint: request.Prescription.ExpectedProgramFingerprint,
			ObservedProgramFingerprint: resolution.Snapshot.ProgramFingerprint, SnapshotChanged: request.Prescription.ExpectedSnapshotFingerprint != resolution.Snapshot.Fingerprint,
			AuthorityChanged: true,
		}
	}
	request.Objective = resolution.Objective
	if resolution.Decision.Kind != supervisor.DecisionPrescribed || resolution.Decision.Transition == nil {
		return result, DecisionError{Decision: resolution.Decision}
	}
	transition := *resolution.Decision.Transition
	now := e.clock.Now()
	admission, err := protocol.NewAdmission(resolution.Snapshot, request.Objective, transition, request.Prescription, request.Authority, request.Parameters, now, request.AdmissionLifetime)
	if err != nil {
		return result, err
	}
	result.Admission = admission
	if request.IdempotencyKey != "" && request.IdempotencyKey != admission.IdempotencyKey {
		return result, fmt.Errorf("supplied idempotency key does not match the exact admitted request")
	}
	if request.IdempotencyKey != "" {
		prior, ok, err := e.receipts.FindByIdempotency(ctx, request.Invocation, admission.IdempotencyKey)
		if err != nil {
			return result, fmt.Errorf("check idempotency: %w", err)
		}
		if ok {
			if err := validateReplayRequest(prior, request, e.program.Fingerprint); err != nil {
				return result, err
			}
			observation, observeErr := e.observer.Observe(ctx, ports.ObservationRequest{Invocation: request.Invocation, Capabilities: admission.GrantedCapabilities})
			if observeErr != nil {
				return result, observeErr
			}
			current, canonicalErr := e.canonicalize(observation)
			if canonicalErr != nil {
				return result, canonicalErr
			}
			if err := validateReplayObjectiveState(prior, current); err != nil {
				return result, err
			}
			if !replayStateSettled(current) {
				return result, ReplayRecoveryError{ReceiptID: prior.ID}
			}
			result.Target, result.Receipt, result.Replayed = current, prior, true
			return result, nil
		}
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

	lockedObservation, err := e.observer.Observe(ctx, ports.ObservationRequest{Invocation: request.Invocation, Capabilities: admission.GrantedCapabilities})
	if err != nil {
		return result, err
	}
	lockedSnapshot, err := e.canonicalize(lockedObservation)
	if err != nil {
		return result, err
	}
	if err := validatePrescriptionCurrent(request.Prescription, lockedSnapshot); err != nil {
		return result, err
	}
	if request.IdempotencyKey != "" {
		prior, ok, findErr := e.receipts.FindByIdempotency(ctx, request.Invocation, admission.IdempotencyKey)
		if findErr != nil {
			return result, fmt.Errorf("check locked idempotency: %w", findErr)
		}
		if ok {
			if err := validateReplayRequest(prior, request, e.program.Fingerprint); err != nil {
				return result, err
			}
			if err := validateReplayObjectiveState(prior, lockedSnapshot); err != nil {
				return result, err
			}
			if !replayStateSettled(lockedSnapshot) {
				return result, ReplayRecoveryError{ReceiptID: prior.ID}
			}
			result.Target, result.Receipt, result.Replayed = lockedSnapshot, prior, true
			return result, nil
		}
	}
	if err := admission.ValidateCurrent(lockedSnapshot, request.Objective, transition, e.clock.Now()); err != nil {
		return result, StaleAdmissionError{Err: err}
	}
	if err := protocol.ValidateEffectCapabilities(admission, transition); err != nil {
		return result, err
	}
	if err := e.receipts.Bind(ctx, request.FlowID, admission); err != nil {
		return result, err
	}
	defer e.receipts.Unbind(request.FlowID)
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
	if err := protocol.ValidateEffectCapabilities(admission, transition); err != nil {
		return result, abort("effect capability check failed", err)
	}
	if err := e.journal.Stage(ctx, admission.ID, prepared.Manifest()); err != nil {
		return result, abort("effect staging journal failed", err)
	}
	startedAt := e.clock.Now()
	effectResult, effectErr := prepared.Execute(ctx)
	if effectErr != nil {
		if transition.Class == catalog.EventOwnedExternal {
			unknown := ExternalOutcomeUnknownError{Transition: transition.ID, Recovery: transition.Interruption.Recovery}
			return result, requireRecovery("external effect returned without a provable outcome", errors.Join(unknown, effectErr))
		}
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
	targetObservation, err := e.observer.Observe(ctx, ports.ObservationRequest{Invocation: verificationInvocation, Capabilities: admission.GrantedCapabilities, IgnoreAdmissionID: admission.ID, VerifyTransitionID: transition.ID})
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
	receipt, err := protocol.NewReceipt(request.FlowID, sequence, e.program, admission, transition, target, prepared.ChangedStateFacets(), prepared.CommittedEffects(), nil, startedAt, completedAt)
	if err != nil {
		return result, requireRecovery("receipt construction failed after verified effect", err)
	}
	if err := e.journal.Commit(ctx, receipt); err != nil {
		return result, requireRecovery("transition fact commit failed after verified effect", err)
	}
	// Receipt and event streams are passive projections. The committed journal
	// record above is the canonical idempotency and audit fact.
	_ = e.receipts.Project(ctx, receipt)
	result.Receipt = receipt
	return result, nil
}

func validatePrescriptionCurrent(prescription protocol.Prescription, snapshot model.Snapshot) error {
	if err := prescription.Validate(); err != nil {
		return err
	}
	objectiveBindingFingerprint, err := protocol.ObjectiveBindingFingerprint(snapshot)
	if err != nil {
		return err
	}
	current, err := general.NewFreshness(snapshot.StateRevision, snapshot.ProgramFingerprint, snapshot.Fingerprint, objectiveBindingFingerprint, prescription.AuthorityFingerprint)
	if err != nil {
		return err
	}
	if prescription.Freshness.Check(current) == nil {
		return nil
	}
	snapshotChanged := prescription.ExpectedSnapshotFingerprint != snapshot.Fingerprint
	objectiveBindingChanged := prescription.ExpectedObjectiveBindingFingerprint != objectiveBindingFingerprint
	return StalePrescriptionError{
		PrescriptionID:             prescription.ID,
		ExpectedStateRevision:      prescription.ExpectedStateRevision,
		ObservedStateRevision:      snapshot.StateRevision,
		ExpectedProgramFingerprint: prescription.ExpectedProgramFingerprint,
		ObservedProgramFingerprint: snapshot.ProgramFingerprint,
		SnapshotChanged:            snapshotChanged,
		ObjectiveBindingChanged:    objectiveBindingChanged,
	}
}

func validateReplayRequest(prior protocol.TransitionReceipt, request ApplyRequest, programFingerprint string) error {
	if prior.Program.Fingerprint != programFingerprint {
		return fmt.Errorf("idempotency receipt belongs to a different control program")
	}
	if prior.FlowID != request.FlowID {
		return fmt.Errorf("idempotency receipt belongs to flow %q, not %q", prior.FlowID, request.FlowID)
	}
	if prior.PrescriptionID != request.Prescription.ID {
		return fmt.Errorf("idempotency receipt belongs to a different prescription")
	}
	if prior.ObjectiveScope != catalog.ObjectiveScopeOptionalPreserve && request.Objective.Validate() == nil {
		if prior.ObjectiveID != request.Objective.ID || prior.ObjectiveKind != request.Objective.Kind || prior.DeliveryID != request.Objective.DeliveryID {
			return fmt.Errorf("idempotency receipt belongs to a different configured objective")
		}
	}
	if request.Requested != "" && prior.TransitionID != request.Requested {
		return fmt.Errorf("idempotency receipt belongs to transition %q, not %q", prior.TransitionID, request.Requested)
	}
	return nil
}

func validateReplayObjectiveState(prior protocol.TransitionReceipt, snapshot model.Snapshot) error {
	if prior.ObjectiveScope != catalog.ObjectiveScopeOptionalPreserve {
		return nil
	}
	switch prior.ObjectiveStatus {
	case model.FactKnown:
		if snapshot.Objective.Status != model.FactKnown || snapshot.Objective.Value.ID != prior.ObjectiveID ||
			snapshot.Objective.Value.Kind != prior.ObjectiveKind || snapshot.Objective.Value.DeliveryID != prior.DeliveryID {
			return fmt.Errorf("idempotency receipt objective binding no longer matches current state")
		}
	case model.FactAbsent:
		if snapshot.Objective.Status != model.FactAbsent {
			return fmt.Errorf("idempotency receipt preserved an absent product objective, but current state is %q", snapshot.Objective.Status)
		}
	default:
		return fmt.Errorf("idempotency receipt has invalid preserved objective status %q", prior.ObjectiveStatus)
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
