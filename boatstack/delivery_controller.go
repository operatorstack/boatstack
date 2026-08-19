package boatstack

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	"github.com/operatorstack/boatstack/boatstack/delivery"
	"github.com/operatorstack/boatstack/boatstack/internal/buildinfo"
	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/effects"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/engine"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/foregroundwork"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/humanidentity"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/plant"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/supervisor"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
)

var (
	Version         = buildinfo.Version
	SourceCommit    = buildinfo.SourceCommit
	ChecksumsSHA256 = buildinfo.ChecksumsSHA256
)

// DeliveryController is the deterministic mechanism facade over one immutable compiled
// ControlProgram and its concrete plant/effect ports. It owns no independent
// delivery-flow policy or durable lifecycle state.
type DeliveryController struct {
	program  delivery.ControlProgram
	registry catalog.Registry
	resolver plant.Resolver
	observer ports.Observer
	engine   engine.Engine
	clock    effects.Clock
	work     foregroundwork.Manager
}

// TargetSatisfied reports whether the exact compiled program marks the
// objective in the supplied canonical snapshot. Callers use this after a
// committed apply so runtime-owned authorization can end without requiring a
// second resolve.
func (k DeliveryController) TargetSatisfied(snapshot *model.Snapshot, objective model.Objective) bool {
	return snapshot != nil && k.program.RuntimeObjectiveContracts().Matches(*snapshot, objective)
}

func NewDeliveryController(externalStateRoot string, program delivery.ControlProgram) (DeliveryController, error) {
	if program.Fingerprint() == "" || program.TransitionCount() == 0 {
		return DeliveryController{}, fmt.Errorf("DeliveryController requires an immutable compiled ControlProgram")
	}
	clock := effects.Clock{}
	resolver, err := plant.NewResolver(externalStateRoot)
	if err != nil {
		return DeliveryController{}, err
	}
	baseObserver, err := plant.NewObserver(resolver, clock)
	if err != nil {
		return DeliveryController{}, err
	}
	observer := programObserver{base: baseObserver, program: program}
	locker, err := effects.NewLocker(resolver)
	if err != nil {
		return DeliveryController{}, err
	}
	workManager, err := foregroundwork.NewManager(resolver, locker, clock, effects.NewRuntimeStore())
	if err != nil {
		return DeliveryController{}, err
	}
	journal, err := effects.NewJournal(resolver, clock)
	if err != nil {
		return DeliveryController{}, err
	}
	receipts, err := effects.NewReceiptStore(resolver, clock)
	if err != nil {
		return DeliveryController{}, err
	}
	programHumanIdentityRole, err := compiledProgramHumanIdentityRole(program)
	if err != nil {
		return DeliveryController{}, err
	}
	baseDriver, err := effects.NewProgramDriver(resolver, clock, effects.NewNativeBoundary(), program.ResourceOwnership(), programHumanIdentityRole)
	if err != nil {
		return DeliveryController{}, err
	}
	driver := programEffectDriver{base: baseDriver, program: program, resolver: resolver, clock: clock}
	registry := program.RuntimeRegistry()
	summary := program.Summary()
	runtimeEngine, err := engine.New(registry, program.RuntimeObjectiveContracts(), protocol.ProgramIdentity{ID: summary.ProgramID, Version: summary.ProgramVersion, Fingerprint: summary.ProgramFingerprint}, observer, clock, locker, journal, driver, receipts)
	if err != nil {
		return DeliveryController{}, err
	}
	return DeliveryController{program: program, registry: registry, resolver: resolver, observer: observer, engine: runtimeEngine, clock: clock, work: workManager}, nil
}

func compiledProgramHumanIdentityRole(program delivery.ControlProgram) (string, error) {
	settings := program.ProgramRuntime().Manifest.Settings
	if len(settings) == 0 {
		return "", nil
	}
	var value struct {
		Role string `json:"human_identity_role"`
	}
	if err := json.Unmarshal(settings, &value); err != nil {
		return "", fmt.Errorf("decode compiled program human identity role: %w", err)
	}
	if value.Role != "" {
		if err := humanidentity.ValidateRole(value.Role); err != nil {
			return "", fmt.Errorf("compiled program human identity role: %w", err)
		}
	}
	return value.Role, nil
}

func (k DeliveryController) Handle(ctx context.Context, request surfaces.Request) (surfaces.Response, error) {
	response := surfaces.Response{SchemaVersion: surfaces.SchemaVersion, Operation: request.Operation, ProgramID: request.ProgramID, EntryID: request.EntryID, RunID: request.FlowID, Invocation: request.InvocationEvidence}
	if err := request.Validate(k.clock.Now()); err != nil {
		response.Error = err.Error()
		return response, err
	}
	if request.InputRequest != nil {
		response.InputRequest = request.InputRequest
		return response, nil
	}
	if request.Operation == surfaces.OperationCatalog {
		response.Catalog = k.registry.All()
		return response, nil
	}
	if request.Operation == surfaces.OperationRecover {
		transition, ok := k.registry.Lookup(request.TransitionID)
		if !ok || transition.Class != catalog.EventRecovery {
			err := fmt.Errorf("recover operation requires a recovery transition from the compiled control program")
			response.Error = err.Error()
			return response, err
		}
	}
	invocation, err := k.resolver.ResolveInvocation(ctx, request.Repository, request.Host, request.CorrelationID)
	if err != nil {
		response.Error = err.Error()
		return response, err
	}
	if request.ControlBundleRevision != "" {
		layout, _, layoutErr := k.resolver.ResolveLayout(ctx, invocation)
		if layoutErr != nil {
			response.Error = layoutErr.Error()
			return response, layoutErr
		}
		if revisionErr := boatstackruntime.VerifyCurrentControlBundleRevision(ctx, layout.RepositoryRoot, request.ControlBundleRevision, request.ControlBundle.Source); revisionErr != nil {
			response.Error = revisionErr.Error()
			return response, revisionErr
		}
	}
	if request.RepositoryAuthority {
		request.Authority, err = k.deriveRepositoryAuthority(ctx, invocation, request.Authority)
		if err != nil {
			response.Error = err.Error()
			return response, err
		}
	}
	switch request.Operation {
	case surfaces.OperationResolve, surfaces.OperationExplain:
		explain := request.Operation == surfaces.OperationExplain
		resolveRequest := engine.ResolveRequest{Invocation: invocation, Objective: request.Objective, Authority: request.Authority, Parameters: request.Parameters, Requested: request.TransitionID, Trace: explain, ControlBundle: request.ControlBundle, ControlBundleRevision: request.ControlBundleRevision, InvocationEvidence: request.InvocationEvidence}
		resolution, resolveErr := k.engine.Resolve(ctx, resolveRequest)
		if !explain && resolveErr == nil && resolution.Decision.Kind == supervisor.DecisionCandidate && resolution.Decision.Transition != nil && resolution.Decision.Transition.Work != nil {
			if verifyErr := k.verifyCommittedWorkInputs(ctx, invocation, request.FlowID, request.ProgramID, request.EntryID, resolution.Objective, resolution.Snapshot, *resolution.Decision.Transition, request.WorkInputs); verifyErr != nil {
				response.Error = verifyErr.Error()
				return response, verifyErr
			}
			record, workErr := k.work.Ensure(ctx, invocation, request.FlowID, request.ProgramID, request.EntryID, resolution.Objective, resolution.Snapshot, *resolution.Decision.Transition, request.WorkInputs)
			if workErr != nil {
				response.Error = workErr.Error()
				return response, workErr
			}
			response.Work = &record
			if record.Status == foregroundwork.StatusCompleted && record.Result != nil {
				resolveRequest.Work = record.Result
				resolution, resolveErr = k.engine.Resolve(ctx, resolveRequest)
			}
		}
		response.Objective, response.Decision, response.Trace = resolution.Objective, &resolution.Decision, resolution.Trace
		if !explain && resolution.Prescription.ID != "" {
			response.Prescription = &resolution.Prescription
			response.Admission = &resolution.Admission
		}
		if !explain && resolution.Snapshot.Fingerprint != "" {
			response.Snapshot = &resolution.Snapshot
		}
		if !explain && response.Work == nil {
			response.Question = surfaces.QuestionFor(request.FlowID, resolution.Snapshot.Fingerprint, resolution.Decision)
		}
		if !explain && response.Work == nil && response.Question == nil && request.FlowID != "" && len(resolution.Decision.Candidates) == 1 {
			if transition, ok := k.registry.Lookup(resolution.Decision.Candidates[0]); ok {
				questionDecision := supervisor.Decision{Kind: supervisor.DecisionCandidate, Transition: &transition}
				response.Question = surfaces.QuestionFor(request.FlowID, resolution.Snapshot.Fingerprint, questionDecision)
			}
		}
		if explain {
			response.ProgramChange = programChangeFor(&resolution.Snapshot)
		} else {
			response.ProgramChange = programChangeFor(response.Snapshot)
		}
		if resolveErr != nil {
			response.Error = resolveErr.Error()
			return response, resolveErr
		}
		return response, nil
	case surfaces.OperationApply, surfaces.OperationRecover:
		var work *protocol.WorkEvidence
		if transition, ok := k.registry.Lookup(request.TransitionID); ok && transition.Work != nil {
			record, workErr := k.work.Show(ctx, invocation, request.FlowID, transition.Work.ID)
			if workErr != nil {
				response.Error = workErr.Error()
				return response, workErr
			}
			if record.Status != foregroundwork.StatusCompleted || record.Result == nil {
				err := fmt.Errorf("transition %q requires completed foreground work %q", transition.ID, transition.Work.ID)
				response.Error = err.Error()
				return response, err
			}
			work, response.Work = record.Result, &record
		}
		result, applyErr := k.engine.Apply(ctx, engine.ApplyRequest{
			ResolveRequest: engine.ResolveRequest{Invocation: invocation, Objective: request.Objective, Authority: request.Authority, Requested: request.TransitionID, Work: work, ControlBundle: request.ControlBundle, ControlBundleRevision: request.ControlBundleRevision, InvocationEvidence: request.InvocationEvidence},
			FlowID:         request.FlowID, Prescription: request.Prescription, Parameters: request.Parameters, IdempotencyKey: request.IdempotencyKey, AdmissionLifetime: 2 * time.Minute,
		})
		response.Prescription = &request.Prescription
		response.Objective = result.Objective
		if result.Target.Fingerprint != "" {
			response.Snapshot = &result.Target
		} else if result.Source.Fingerprint != "" {
			response.Snapshot = &result.Source
		}
		if result.Decision.Kind != "" {
			response.Decision = &result.Decision
		}
		if result.Admission.ID != "" {
			response.Admission = &result.Admission
		}
		if result.Receipt.ID != "" {
			response.Receipt = &result.Receipt
		}
		response.ProgramChange = programChangeFor(response.Snapshot)
		response.Replayed = result.Replayed
		if applyErr != nil {
			response.Error = applyErr.Error()
			return response, applyErr
		}
		return response, nil
	case surfaces.OperationWorkShow, surfaces.OperationWorkInputRequired, surfaces.OperationWorkAnswer, surfaces.OperationWorkComplete, surfaces.OperationWorkBlock:
		var record foregroundwork.Record
		var workErr error
		switch request.Operation {
		case surfaces.OperationWorkShow:
			record, workErr = k.work.Show(ctx, invocation, request.FlowID, request.WorkID)
		case surfaces.OperationWorkInputRequired:
			record, workErr = k.work.InputRequired(ctx, invocation, request.FlowID, request.WorkID, request.WorkQuestionPrompt, request.WorkQuestionSchema)
		case surfaces.OperationWorkAnswer:
			record, workErr = k.work.Answer(ctx, invocation, request.FlowID, request.WorkID, request.WorkQuestionID, request.WorkAnswer)
		case surfaces.OperationWorkComplete:
			record, workErr = k.work.Complete(ctx, invocation, request.FlowID, request.WorkID)
		case surfaces.OperationWorkBlock:
			record, workErr = k.work.Block(ctx, invocation, request.FlowID, request.WorkID, request.WorkBlockReason)
		}
		if workErr != nil {
			response.Error = workErr.Error()
			return response, workErr
		}
		response.Work = &record
		return response, nil
	case surfaces.OperationDoctor:
		summary := k.program.Summary()
		extensionIDs := make([]string, 0, len(summary.Extensions))
		for _, extension := range summary.Extensions {
			extensionIDs = append(extensionIDs, extension.ID+"@"+extension.Version)
		}
		report := surfaces.DoctorReport{
			KernelVersion: summary.KernelVersion, CoreSystemID: summary.Core.ID, CoreSystemVersion: summary.Core.Version,
			ProgramID: summary.ProgramID, ProgramVersion: summary.ProgramVersion,
			CoreTransitionCount: summary.CoreTransitionCount, RuntimeTransitionCount: summary.RuntimeTransitionCount,
			ExtensionTransitionCount: summary.ExtensionTransitionCount, TransitionCount: summary.TotalTransitionCount,
			EnabledExtensions: extensionIDs, ProgramFingerprint: summary.ProgramFingerprint,
		}
		observation, observeErr := k.observer.Observe(ctx, ports.ObservationRequest{Invocation: invocation, Capabilities: request.Authority.GrantedCapabilities(k.clock.Now())})
		if observeErr != nil {
			report.Healthy, report.Detail = false, observeErr.Error()
			response.Doctor = &report
			response.Error = observeErr.Error()
			return response, observeErr
		}
		snapshot, canonicalErr := model.CanonicalizeForProgram(observation, k.program.Fingerprint())
		if canonicalErr != nil {
			report.Healthy, report.Detail = false, canonicalErr.Error()
			response.Doctor = &report
			response.Error = canonicalErr.Error()
			return response, canonicalErr
		}
		response.Snapshot = &snapshot
		response.ProgramChange = programChangeFor(response.Snapshot)
		health := model.ProjectOperationalHealth(snapshot)
		report.UnresolvedProgramDrift = snapshot.Program.Status != model.FactKnown || snapshot.Program.Value == model.ProgramDrift
		report.RuntimeHealthy = health.RuntimeVerified
		report.RecoveryRequired = health.RecoveryRequired
		for _, transitionID := range []catalog.TransitionID{"installation.update", "installation.reconcile-update"} {
			if transition, ok := k.registry.Lookup(transitionID); ok && transition.SourceMatches(snapshot) {
				report.UpdateReady = true
			}
		}
		report.Healthy = k.registry.Len() == summary.TotalTransitionCount && !report.UnresolvedProgramDrift && report.RuntimeHealthy && report.UpdateReady && !report.RecoveryRequired
		report.Snapshot = snapshot.Fingerprint
		report.Detail = "DeliveryController, observation, and compiled control program are valid"
		if report.UnresolvedProgramDrift {
			report.Detail = supervisor.ReasonProgramDrift
		} else if report.RecoveryRequired {
			report.Detail = "an interrupted transaction requires exact recovery"
		} else if !report.RuntimeHealthy {
			report.Detail = "runtime or managed launcher is not verified"
		} else if !report.UpdateReady {
			report.Detail = "no structurally admissible installation update continuation"
		}
		response.Doctor = &report
		return response, nil
	case surfaces.OperationEvents:
		layout, _, layoutErr := k.resolver.ResolveLayout(ctx, invocation)
		if layoutErr != nil {
			return response, layoutErr
		}
		events, readErr := readEvents(layout.EventPath)
		if readErr != nil {
			response.Error = readErr.Error()
			return response, readErr
		}
		response.Events = events
		return response, nil
	case surfaces.OperationGuard:
		observation, observeErr := k.observer.Observe(ctx, ports.ObservationRequest{Invocation: invocation, Capabilities: request.Authority.GrantedCapabilities(k.clock.Now())})
		if observeErr != nil {
			response.Error = observeErr.Error()
			return response, observeErr
		}
		snapshot, canonicalErr := model.CanonicalizeForProgram(observation, k.program.Fingerprint())
		if canonicalErr != nil {
			response.Error = canonicalErr.Error()
			return response, canonicalErr
		}
		intent := surfaces.ClassifyCommandIntent(request.Command)
		guard := supervisor.New(k.registry, k.program.RuntimeObjectiveContracts()).Guard(snapshot, intent)
		response.Snapshot, response.Guard = &snapshot, &guard
		return response, nil
	default:
		return response, fmt.Errorf("unsupported surface operation %q", request.Operation)
	}
}

func (k DeliveryController) verifyCommittedWorkInputs(ctx context.Context, invocation model.InvocationContext, flowID, programID, entryID string, objective model.Objective, snapshot model.Snapshot, transition catalog.Transition, inputs map[string]protocol.WorkInputValue) error {
	if transition.Work == nil {
		return nil
	}
	var layout ports.ControllerLayout
	layoutResolved := false
	for _, input := range transition.Work.Inputs {
		if input.Producer.Kind != controlprogram.ParameterSourceWorkOutput {
			continue
		}
		value, found := inputs[transition.Work.ID+"/"+input.ID]
		if !found || value.WorkOutput == nil {
			return fmt.Errorf("FLOW_WORK_EVIDENCE_STALE: work %q input %q lacks committed producer provenance", transition.Work.ID, input.ID)
		}
		producer, err := uniqueRuntimeWorkProducer(k.registry.All(), input.Producer.Work)
		if err != nil {
			return err
		}
		if !layoutResolved {
			var current model.InvocationContext
			layout, current, err = k.resolver.ResolveLayout(ctx, invocation)
			if err != nil {
				return err
			}
			invocation = current
			layoutResolved = true
		}
		committed, applicable, err := effects.FindApplicableCommittedWorkOutput(layout, effects.CommittedWorkOutputSelector{
			FlowID: flowID, ProgramID: programID, ProgramFingerprint: snapshot.ProgramFingerprint, EntryID: entryID,
			Objective: objective, Invocation: invocation, TransitionID: producer.ID, Work: *producer.Work,
			OutputID: input.Producer.Output, MaximumRevision: snapshot.StateRevision,
		})
		if err != nil {
			return fmt.Errorf("FLOW_WORK_EVIDENCE_STALE: work %q input %q: %w", transition.Work.ID, input.ID, err)
		}
		if !applicable || !matchesCommittedWorkInput(value, committed) {
			return fmt.Errorf("FLOW_WORK_EVIDENCE_STALE: work %q input %q does not match an applicable committed producer result", transition.Work.ID, input.ID)
		}
	}
	return nil
}

func uniqueRuntimeWorkProducer(transitions []catalog.Transition, workID string) (catalog.Transition, error) {
	var producer catalog.Transition
	for _, transition := range transitions {
		if transition.Work == nil || transition.Work.ID != workID {
			continue
		}
		if producer.ID != "" {
			return catalog.Transition{}, fmt.Errorf("FLOW_WORK_EVIDENCE_STALE: work %q has ambiguous producer transitions", workID)
		}
		producer = transition
	}
	if producer.ID == "" {
		return catalog.Transition{}, fmt.Errorf("FLOW_WORK_EVIDENCE_STALE: work %q has no producer transition", workID)
	}
	return producer, nil
}

func matchesCommittedWorkInput(value protocol.WorkInputValue, committed effects.CommittedWorkOutput) bool {
	provenance := value.WorkOutput
	return provenance != nil && value.Value == committed.Output.Content && value.Fingerprint == committed.Output.SHA256 &&
		provenance.ReceiptID == committed.Receipt.ID && provenance.TransitionID == committed.Receipt.TransitionID &&
		provenance.WorkID == committed.Work.ContractID && provenance.OutputID == committed.Output.ID &&
		provenance.ResultFingerprint == committed.Work.ResultFingerprint && provenance.ContractFingerprint == committed.Work.ContractFingerprint &&
		provenance.OutputSHA256 == committed.Output.SHA256
}

func programChangeFor(snapshot *model.Snapshot) *surfaces.ProgramChange {
	if snapshot == nil || snapshot.Program.Status != model.FactKnown || snapshot.Program.Value != model.ProgramDrift {
		return nil
	}
	delta, err := protocol.ProgramDeltaFingerprint(snapshot.RecordedProgramFingerprint, snapshot.ProgramFingerprint)
	if err != nil {
		return nil
	}
	return &surfaces.ProgramChange{
		PriorProgramFingerprint: snapshot.RecordedProgramFingerprint, CandidateProgramFingerprint: snapshot.ProgramFingerprint,
		ProgramDeltaFingerprint: delta, RequiredTransition: "installation.reconcile-update", AcceptanceFlag: "--accept-program-change",
	}
}

func (k DeliveryController) deriveRepositoryAuthority(ctx context.Context, invocation model.InvocationContext, bundle protocol.AuthorityBundle) (protocol.AuthorityBundle, error) {
	for _, receipt := range bundle.Receipts {
		if receipt.Class == catalog.AuthorityRepository {
			return protocol.AuthorityBundle{}, fmt.Errorf("repository authority must be derived once by DeliveryController")
		}
	}
	observation, err := k.observer.Observe(ctx, ports.ObservationRequest{Invocation: invocation, Capabilities: bundle.GrantedCapabilities(k.clock.Now())})
	if err != nil {
		return protocol.AuthorityBundle{}, err
	}
	snapshot, err := model.CanonicalizeForProgram(observation, k.program.Fingerprint())
	if err != nil {
		return protocol.AuthorityBundle{}, err
	}
	return protocol.DeriveRepositoryAuthorityWhenAvailable(snapshot, bundle, k.clock.Now())
}

func readEvents(path string) ([]map[string]any, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()
	var events []map[string]any
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, scanner.Err()
}
