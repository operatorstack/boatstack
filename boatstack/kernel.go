package boatstack

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/operatorstack/boatstack/boatstack/control"
	"github.com/operatorstack/boatstack/boatstack/internal/buildinfo"
	"github.com/operatorstack/boatstack/boatstack/internal/effects"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/engine"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/supervisor"
	"github.com/operatorstack/boatstack/boatstack/internal/plant"
	"github.com/operatorstack/boatstack/boatstack/internal/surfaces"
)

var (
	Version         = buildinfo.Version
	SourceCommit    = buildinfo.SourceCommit
	ChecksumsSHA256 = buildinfo.ChecksumsSHA256
)

// Kernel is the deterministic mechanism facade over one immutable compiled
// ControlProgram and its concrete plant/effect ports. It owns no independent
// delivery-flow policy or durable lifecycle state.
type Kernel struct {
	program  control.ControlProgram
	registry catalog.Registry
	resolver plant.Resolver
	observer ports.Observer
	engine   engine.Engine
	clock    effects.Clock
}

func NewKernel(externalStateRoot string, program control.ControlProgram) (Kernel, error) {
	if program.Fingerprint() == "" || program.TransitionCount() == 0 {
		return Kernel{}, fmt.Errorf("Kernel requires an immutable compiled ControlProgram")
	}
	clock := effects.Clock{}
	resolver, err := plant.NewResolver(externalStateRoot)
	if err != nil {
		return Kernel{}, err
	}
	baseObserver, err := plant.NewObserver(resolver, clock)
	if err != nil {
		return Kernel{}, err
	}
	observer := programObserver{base: baseObserver, program: program}
	locker, err := effects.NewLocker(resolver)
	if err != nil {
		return Kernel{}, err
	}
	journal, err := effects.NewJournal(resolver, clock)
	if err != nil {
		return Kernel{}, err
	}
	receipts, err := effects.NewReceiptStore(resolver, clock)
	if err != nil {
		return Kernel{}, err
	}
	baseDriver, err := effects.NewProgramDriver(resolver, clock, effects.NewNativeBoundary(), program.ResourceOwnership())
	if err != nil {
		return Kernel{}, err
	}
	driver := programEffectDriver{base: baseDriver, program: program, resolver: resolver, clock: clock}
	registry := program.RuntimeRegistry()
	runtimeEngine, err := engine.New(registry, program.RuntimeGoalContracts(), program.Fingerprint(), observer, clock, locker, journal, driver, receipts)
	if err != nil {
		return Kernel{}, err
	}
	return Kernel{program: program, registry: registry, resolver: resolver, observer: observer, engine: runtimeEngine, clock: clock}, nil
}

func (k Kernel) Handle(ctx context.Context, request surfaces.Request) (surfaces.Response, error) {
	response := surfaces.Response{SchemaVersion: surfaces.SchemaVersion, Operation: request.Operation}
	if err := request.Validate(k.clock.Now()); err != nil {
		response.Error = err.Error()
		return response, err
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
	if request.RepositoryAuthority {
		request.Authority, err = k.deriveRepositoryAuthority(ctx, invocation, request.Authority)
		if err != nil {
			response.Error = err.Error()
			return response, err
		}
	}
	switch request.Operation {
	case surfaces.OperationResolve:
		resolution, resolveErr := k.engine.Resolve(ctx, engine.ResolveRequest{Invocation: invocation, Goal: request.Goal, Authority: request.Authority, Parameters: request.Parameters, Requested: request.TransitionID})
		response.Goal, response.Decision = resolution.Goal, &resolution.Decision
		if resolution.Prescription.ID != "" {
			response.Prescription = &resolution.Prescription
			response.Admission = &resolution.Admission
		}
		if resolution.Snapshot.Fingerprint != "" {
			response.Snapshot = &resolution.Snapshot
		}
		response.ProgramChange = programChangeFor(response.Snapshot)
		if resolveErr != nil {
			response.Error = resolveErr.Error()
			return response, resolveErr
		}
		return response, nil
	case surfaces.OperationApply, surfaces.OperationRecover:
		result, applyErr := k.engine.Apply(ctx, engine.ApplyRequest{
			ResolveRequest: engine.ResolveRequest{Invocation: invocation, Goal: request.Goal, Authority: request.Authority, Requested: request.TransitionID},
			FlowID:         request.FlowID, Prescription: request.Prescription, Parameters: request.Parameters, IdempotencyKey: request.IdempotencyKey, AdmissionLifetime: 2 * time.Minute,
		})
		response.Prescription = &request.Prescription
		response.Goal = result.Goal
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
		observation, observeErr := k.observer.Observe(ctx, ports.ObservationRequest{Invocation: invocation})
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
		report.Detail = "Kernel, observation, and compiled control program are valid"
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
		observation, observeErr := k.observer.Observe(ctx, ports.ObservationRequest{Invocation: invocation})
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
		guard := supervisor.New(k.registry, k.program.RuntimeGoalContracts()).Guard(snapshot, intent)
		response.Snapshot, response.Guard = &snapshot, &guard
		return response, nil
	default:
		return response, fmt.Errorf("unsupported surface operation %q", request.Operation)
	}
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

func (k Kernel) deriveRepositoryAuthority(ctx context.Context, invocation model.InvocationContext, bundle protocol.AuthorityBundle) (protocol.AuthorityBundle, error) {
	for _, receipt := range bundle.Receipts {
		if receipt.Class == catalog.AuthorityRepository {
			return protocol.AuthorityBundle{}, fmt.Errorf("repository authority must be derived once by Kernel")
		}
	}
	observation, err := k.observer.Observe(ctx, ports.ObservationRequest{Invocation: invocation})
	if err != nil {
		return protocol.AuthorityBundle{}, err
	}
	snapshot, err := model.CanonicalizeForProgram(observation, k.program.Fingerprint())
	if err != nil {
		return protocol.AuthorityBundle{}, err
	}
	return protocol.DeriveRepositoryAuthority(snapshot, bundle, k.clock.Now())
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
