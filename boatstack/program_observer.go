package boatstack

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/operatorstack/boatstack/boatstack/control"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/ports"
)

type programObserver struct {
	base    ports.Observer
	program control.ControlProgram
}

// ComponentRuntimeError preserves a bounded protocol error classification and
// message without granting a component control over recovery policy.
type ComponentRuntimeError struct {
	Component string
	Operation string
	Class     string
	Message   string
}

func (e ComponentRuntimeError) Error() string {
	return fmt.Sprintf("%s %s reported %s: %s", e.Component, e.Operation, e.Class, e.Message)
}

func (o programObserver) Observe(ctx context.Context, request ports.ObservationRequest) (model.Observation, error) {
	observation, err := o.base.Observe(ctx, request)
	if err != nil {
		return model.Observation{}, err
	}
	flow := o.program.Flow()
	if flow.Manifest.RuntimeMode == control.FlowRuntimeProtocol {
		if flow.Runtime == nil {
			return model.Observation{}, fmt.Errorf("primary flow %q observer is unavailable", flow.Identity.ID)
		}
		projection := observation
		projection.ProgramFingerprint = o.program.Fingerprint()
		snapshot, encodeErr := json.Marshal(projection)
		if encodeErr != nil {
			return model.Observation{}, fmt.Errorf("encode bounded primary-flow observation: %w", encodeErr)
		}
		response, invokeErr := flow.Runtime.InvokeFlow(ctx, control.FlowRequest{
			ProtocolVersion: control.FlowProtocolVersion, Operation: control.FlowObserveOperation,
			FlowID: flow.Identity.ID, FlowVersion: flow.Identity.Version,
			ProgramFingerprint: o.program.Fingerprint(), CorrelationID: request.Invocation.Correlation,
			RepositoryRoot: request.Invocation.InvokingPath, Snapshot: snapshot, Settings: flow.Manifest.Settings,
		})
		if invokeErr != nil {
			return model.Observation{}, fmt.Errorf("primary flow %q observation failed: %w", flow.Identity.ID, invokeErr)
		}
		if err := validateFlowResponse(flow, control.FlowObserveOperation, request.Invocation.Correlation, response); err != nil {
			return model.Observation{}, err
		}
		declared := make(map[string]bool, len(flow.Manifest.Facts))
		for _, id := range flow.Manifest.Facts {
			declared[id] = true
		}
		observation.FlowFacts = make(map[string]model.Fact[string], len(declared))
		for _, fact := range response.Facts {
			if !declared[fact.ID] {
				return model.Observation{}, fmt.Errorf("primary flow %q returned undeclared fact %q", flow.Identity.ID, fact.ID)
			}
			if _, exists := observation.FlowFacts[fact.ID]; exists {
				return model.Observation{}, fmt.Errorf("primary-flow fact %q was returned more than once", fact.ID)
			}
			if !fact.Status.Valid() || fact.Fingerprint == "" {
				return model.Observation{}, fmt.Errorf("primary flow %q returned invalid fact %q", flow.Identity.ID, fact.ID)
			}
			observation.FlowFacts[fact.ID] = model.Fact[string]{
				Status: fact.Status, Value: fact.Value, Detail: fact.Detail,
				Evidence: []model.Evidence{{Source: "primary-flow:" + flow.Identity.ID, Fingerprint: fact.Fingerprint, ObservedAt: observation.ObservedAt}},
			}
			delete(declared, fact.ID)
		}
		if len(declared) != 0 {
			return model.Observation{}, fmt.Errorf("primary flow %q omitted required observed facts", flow.Identity.ID)
		}
	}
	// Extension observers receive the same core-plus-flow projection. Facts
	// returned by one extension are collected into the final snapshot but are
	// never exposed to another observer based on invocation order.
	observation.ExtensionFacts = nil
	extensionProjection := observation
	for _, extension := range o.program.Extensions() {
		if len(extension.Manifest.Facts) == 0 {
			continue
		}
		// Repository-selected executable extensions remain inert until the
		// observed configuration and recorded program identity prove that this
		// exact composition has already crossed the Kernel admission boundary.
		if extension.Manifest.ExecutableSHA256 != "" && !observation.ExecutableRuntimeAdmitted(o.program.Fingerprint()) {
			continue
		}
		if extension.Runtime == nil {
			return model.Observation{}, fmt.Errorf("extension %q observer is unavailable", extension.Identity.ID)
		}
		projection := extensionProjection
		projection.ProgramFingerprint = o.program.Fingerprint()
		snapshot, err := json.Marshal(projection)
		if err != nil {
			return model.Observation{}, fmt.Errorf("encode bounded extension observation: %w", err)
		}
		response, err := extension.Runtime.Invoke(ctx, control.ExtensionRequest{
			ProtocolVersion: control.ExtensionProtocolVersion, Operation: control.ExtensionObserveOperation,
			ExtensionID: extension.Identity.ID, ExtensionVersion: extension.Identity.Version,
			ProgramFingerprint: o.program.Fingerprint(), CorrelationID: request.Invocation.Correlation,
			RepositoryRoot: request.Invocation.InvokingPath, Snapshot: snapshot, Settings: extension.Manifest.Settings,
		})
		if err != nil {
			return model.Observation{}, fmt.Errorf("extension %q observation failed: %w", extension.Identity.ID, err)
		}
		if err := validateExtensionResponse(extension, control.ExtensionObserveOperation, request.Invocation.Correlation, response); err != nil {
			return model.Observation{}, err
		}
		declared := make(map[string]bool, len(extension.Manifest.Facts))
		for _, id := range extension.Manifest.Facts {
			declared[id] = true
		}
		if observation.ExtensionFacts == nil {
			observation.ExtensionFacts = map[string]model.Fact[string]{}
		}
		for _, fact := range response.Facts {
			if !declared[fact.ID] {
				return model.Observation{}, fmt.Errorf("extension %q returned undeclared fact %q", extension.Identity.ID, fact.ID)
			}
			if _, exists := observation.ExtensionFacts[fact.ID]; exists {
				return model.Observation{}, fmt.Errorf("extension fact %q was returned more than once", fact.ID)
			}
			if !fact.Status.Valid() || fact.Fingerprint == "" {
				return model.Observation{}, fmt.Errorf("extension %q returned invalid fact %q", extension.Identity.ID, fact.ID)
			}
			observation.ExtensionFacts[fact.ID] = model.Fact[string]{
				Status: fact.Status, Value: fact.Value, Detail: fact.Detail,
				Evidence: []model.Evidence{{Source: "extension:" + extension.Identity.ID, Fingerprint: fact.Fingerprint, ObservedAt: observation.ObservedAt}},
			}
			delete(declared, fact.ID)
		}
		if len(declared) != 0 {
			return model.Observation{}, fmt.Errorf("extension %q omitted required observed facts", extension.Identity.ID)
		}
	}
	if request.VerifyTransitionID != "" {
		transition, ok := o.program.RuntimeRegistry().Lookup(request.VerifyTransitionID)
		if ok && transition.Origin.Kind == catalog.OriginPrimaryFlow && flow.Manifest.RuntimeMode == control.FlowRuntimeProtocol {
			snapshot, encodeErr := json.Marshal(observation)
			if encodeErr != nil {
				return model.Observation{}, encodeErr
			}
			response, invokeErr := flow.Runtime.InvokeFlow(ctx, control.FlowRequest{
				ProtocolVersion: control.FlowProtocolVersion, Operation: control.FlowVerifyOperation,
				FlowID: flow.Identity.ID, FlowVersion: flow.Identity.Version,
				ProgramFingerprint: o.program.Fingerprint(), CorrelationID: request.Invocation.Correlation,
				RepositoryRoot: request.Invocation.InvokingPath, TransitionID: transition.ID, Snapshot: snapshot, Settings: flow.Manifest.Settings,
			})
			if invokeErr != nil {
				return model.Observation{}, invokeErr
			}
			if err := validateFlowResponse(flow, control.FlowVerifyOperation, request.Invocation.Correlation, response); err != nil {
				return model.Observation{}, err
			}
			if response.Verified == nil || !*response.Verified {
				return model.Observation{}, fmt.Errorf("primary-flow verifier %q rejected the postcondition", transition.Verifier)
			}
		}
		if ok && transition.Origin.Kind == catalog.OriginExtension {
			extension, exists := o.program.ExtensionByID(transition.Origin.ID)
			if !exists || extension.Runtime == nil {
				return model.Observation{}, fmt.Errorf("extension verifier %q is unavailable", transition.Verifier)
			}
			snapshot, err := json.Marshal(observation)
			if err != nil {
				return model.Observation{}, err
			}
			response, err := extension.Runtime.Invoke(ctx, control.ExtensionRequest{
				ProtocolVersion: control.ExtensionProtocolVersion, Operation: control.ExtensionVerifyOperation,
				ExtensionID: extension.Identity.ID, ExtensionVersion: extension.Identity.Version,
				ProgramFingerprint: o.program.Fingerprint(), CorrelationID: request.Invocation.Correlation,
				RepositoryRoot: request.Invocation.InvokingPath, TransitionID: transition.ID, Snapshot: snapshot, Settings: extension.Manifest.Settings,
			})
			if err != nil {
				return model.Observation{}, err
			}
			if err := validateExtensionResponse(extension, control.ExtensionVerifyOperation, request.Invocation.Correlation, response); err != nil {
				return model.Observation{}, err
			}
			if response.Verified == nil || !*response.Verified {
				return model.Observation{}, fmt.Errorf("extension verifier %q rejected the postcondition", transition.Verifier)
			}
		}
	}
	return observation, nil
}

func validateFlowResponse(flow control.CompiledFlow, operation control.FlowOperation, correlation string, response control.FlowResponse) error {
	if response.ProtocolVersion != control.FlowProtocolVersion || response.Operation != operation ||
		response.FlowID != flow.Identity.ID || response.FlowVersion != flow.Identity.Version || response.CorrelationID != correlation {
		return fmt.Errorf("primary flow %q returned a mismatched protocol response", flow.Identity.ID)
	}
	if err := control.ValidateFlowOperationResponse(operation, response); err != nil {
		return fmt.Errorf("primary flow %q returned an invalid operation response: %w", flow.Identity.ID, err)
	}
	if response.ErrorClass != "" || response.Error != "" {
		return ComponentRuntimeError{Component: fmt.Sprintf("primary flow %q", flow.Identity.ID), Operation: string(operation), Class: response.ErrorClass, Message: response.Error}
	}
	return nil
}

func validateExtensionResponse(extension control.CompiledExtension, operation control.ExtensionOperation, correlation string, response control.ExtensionResponse) error {
	if response.ProtocolVersion != control.ExtensionProtocolVersion || response.Operation != operation ||
		response.ExtensionID != extension.Identity.ID || response.ExtensionVersion != extension.Identity.Version ||
		response.CorrelationID != correlation {
		return fmt.Errorf("extension %q returned a mismatched protocol response", extension.Identity.ID)
	}
	if err := control.ValidateExtensionOperationResponse(operation, response); err != nil {
		return fmt.Errorf("extension %q returned an invalid operation response: %w", extension.Identity.ID, err)
	}
	if response.ErrorClass != "" || response.Error != "" {
		return ComponentRuntimeError{Component: fmt.Sprintf("extension %q", extension.Identity.ID), Operation: string(operation), Class: response.ErrorClass, Message: response.Error}
	}
	return nil
}
