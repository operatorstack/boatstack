package boatstack

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

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
	Version         = "v2.0.0-dev"
	SourceCommit    = "unknown"
	ChecksumsSHA256 = "development"
)

// V2Kernel is a product facade over the authoritative engine and its concrete
// plant/effect ports. It owns no independent durable or lifecycle state.
type V2Kernel struct {
	registry catalog.Registry
	resolver plant.Resolver
	observer plant.Observer
	engine   engine.Engine
	clock    effects.Clock
}

func NewV2Kernel(externalStateRoot string) (V2Kernel, error) {
	clock := effects.Clock{}
	resolver, err := plant.NewResolver(externalStateRoot)
	if err != nil {
		return V2Kernel{}, err
	}
	observer, err := plant.NewObserver(resolver, clock)
	if err != nil {
		return V2Kernel{}, err
	}
	locker, err := effects.NewLocker(resolver)
	if err != nil {
		return V2Kernel{}, err
	}
	journal, err := effects.NewJournal(resolver, clock)
	if err != nil {
		return V2Kernel{}, err
	}
	receipts, err := effects.NewReceiptStore(resolver, clock)
	if err != nil {
		return V2Kernel{}, err
	}
	driver, err := effects.NewDriver(resolver, clock, effects.NewNativeBoundary())
	if err != nil {
		return V2Kernel{}, err
	}
	registry := catalog.Default()
	runtimeEngine, err := engine.New(registry, observer, clock, locker, journal, driver, receipts)
	if err != nil {
		return V2Kernel{}, err
	}
	return V2Kernel{registry: registry, resolver: resolver, observer: observer, engine: runtimeEngine, clock: clock}, nil
}

func (k V2Kernel) Handle(ctx context.Context, request surfaces.Request) (surfaces.Response, error) {
	response := surfaces.Response{SchemaVersion: surfaces.SchemaVersion, Operation: request.Operation}
	if err := request.Validate(k.clock.Now()); err != nil {
		response.Error = err.Error()
		return response, err
	}
	if request.Operation == surfaces.OperationCatalog {
		response.Catalog = k.registry.All()
		return response, nil
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
		resolution, resolveErr := k.engine.Resolve(ctx, engine.ResolveRequest{Invocation: invocation, Goal: request.Goal, Authority: request.Authority, Requested: request.TransitionID})
		if resolveErr != nil {
			response.Error = resolveErr.Error()
			return response, resolveErr
		}
		response.Goal, response.Snapshot, response.Decision = resolution.Goal, &resolution.Snapshot, &resolution.Decision
		return response, nil
	case surfaces.OperationApply, surfaces.OperationRecover:
		result, applyErr := k.engine.Apply(ctx, engine.ApplyRequest{
			ResolveRequest: engine.ResolveRequest{Invocation: invocation, Goal: request.Goal, Authority: request.Authority, Requested: request.TransitionID},
			FlowID:         request.FlowID, Parameters: request.Parameters, IdempotencyKey: request.IdempotencyKey, AdmissionLifetime: 2 * time.Minute,
		})
		response.Goal, response.Snapshot = result.Goal, &result.Target
		if result.Decision.Kind != "" {
			response.Decision = &result.Decision
		}
		if result.Admission.ID != "" {
			response.Admission = &result.Admission
		}
		if result.Receipt.ID != "" {
			response.Receipt = &result.Receipt
		}
		response.Replayed = result.Replayed
		if applyErr != nil {
			response.Error = applyErr.Error()
			if result.Target.Fingerprint == "" {
				response.Snapshot = &result.Source
			}
			return response, applyErr
		}
		return response, nil
	case surfaces.OperationDoctor:
		observation, observeErr := k.observer.Observe(ctx, ports.ObservationRequest{Invocation: invocation})
		if observeErr != nil {
			response.Doctor = &surfaces.DoctorReport{Healthy: false, TransitionCount: k.registry.Len(), Detail: observeErr.Error()}
			response.Error = observeErr.Error()
			return response, observeErr
		}
		snapshot, canonicalErr := model.Canonicalize(observation)
		if canonicalErr != nil {
			response.Doctor = &surfaces.DoctorReport{Healthy: false, TransitionCount: k.registry.Len(), Detail: canonicalErr.Error()}
			response.Error = canonicalErr.Error()
			return response, canonicalErr
		}
		response.Snapshot = &snapshot
		response.Doctor = &surfaces.DoctorReport{Healthy: k.registry.Len() == catalog.DefaultTransitionCount, TransitionCount: k.registry.Len(), Snapshot: snapshot.Fingerprint, Detail: "V2 kernel, observation, and catalog are valid"}
		return response, nil
	case surfaces.OperationEvents:
		layout, _, layoutErr := k.resolver.ResolveLayout(ctx, invocation)
		if layoutErr != nil {
			return response, layoutErr
		}
		events, readErr := readV2Events(layout.EventPath)
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
		snapshot, canonicalErr := model.Canonicalize(observation)
		if canonicalErr != nil {
			response.Error = canonicalErr.Error()
			return response, canonicalErr
		}
		intent := surfaces.ClassifyCommandIntent(request.Command)
		guard := supervisor.New(k.registry).Guard(snapshot, intent)
		response.Snapshot, response.Guard = &snapshot, &guard
		return response, nil
	default:
		return response, fmt.Errorf("unsupported surface operation %q", request.Operation)
	}
}

func (k V2Kernel) deriveRepositoryAuthority(ctx context.Context, invocation model.InvocationContext, bundle protocol.AuthorityBundle) (protocol.AuthorityBundle, error) {
	for _, receipt := range bundle.Receipts {
		if receipt.Class == catalog.AuthorityRepository {
			return protocol.AuthorityBundle{}, fmt.Errorf("repository authority must be derived once by the V2 kernel")
		}
	}
	observation, err := k.observer.Observe(ctx, ports.ObservationRequest{Invocation: invocation})
	if err != nil {
		return protocol.AuthorityBundle{}, err
	}
	snapshot, err := model.Canonicalize(observation)
	if err != nil {
		return protocol.AuthorityBundle{}, err
	}
	return protocol.DeriveRepositoryAuthority(snapshot, bundle, k.clock.Now())
}

func readV2Events(path string) ([]map[string]any, error) {
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
