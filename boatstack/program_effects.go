package boatstack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/operatorstack/boatstack/boatstack/control"
	"github.com/operatorstack/boatstack/boatstack/internal/effects"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
)

type programEffectDriver struct {
	base     ports.EffectDriver
	program  control.ControlProgram
	resolver ports.InvocationResolver
	clock    ports.Clock
}

func (d programEffectDriver) Prepare(ctx context.Context, admission protocol.Admission, transition catalog.Transition) (ports.PreparedEffect, error) {
	if err := protocol.ValidateEffectCapabilities(admission, transition); err != nil {
		return nil, err
	}
	if transition.RuntimeExecution {
		if err := protocol.RequireCapability(admission.EffectiveCapabilities, catalog.CapabilityCommandExecute, "component runtime "+transition.Origin.ID); err != nil {
			return nil, err
		}
	}
	if transition.Origin.Kind == catalog.OriginCoreSystem {
		return d.base.Prepare(ctx, admission, transition)
	}
	if transition.Origin.Kind == catalog.OriginControlProgram {
		flow := d.program.ProgramRuntime()
		if flow.Manifest.RuntimeMode == control.ProgramRuntimeNative {
			return d.base.Prepare(ctx, admission, transition)
		}
		if flow.Runtime == nil {
			return nil, fmt.Errorf("control-program runtime %q is unavailable", flow.Identity.ID)
		}
		parameters, err := json.Marshal(admission.Parameters)
		if err != nil {
			return nil, err
		}
		request := control.ProgramRuntimeRequest{
			ProtocolVersion: control.ProgramRuntimeProtocolVersion, ProgramID: flow.Identity.ID, ProgramVersion: flow.Identity.Version,
			ProgramFingerprint: admission.ExpectedProgramFingerprint, CorrelationID: admission.Invocation.Correlation,
			RepositoryRoot: admission.Invocation.InvokingPath, TransitionID: transition.ID, Parameters: parameters, Settings: flow.Manifest.Settings,
			Capabilities: append([]control.Capability(nil), admission.EffectiveCapabilities...),
		}
		if transition.Class == catalog.EventOwnedExternal {
			prepared, err := effects.NewExtensionExternalPrepared(func(executionContext context.Context) (ports.EffectResult, error) {
				request.Operation = control.ProgramExecuteExternalOperation
				response, invokeErr := flow.Runtime.InvokeProgram(executionContext, request)
				if invokeErr != nil {
					return ports.EffectResult{}, invokeErr
				}
				if err := validateProgramRuntimeResponse(flow, request.Operation, request.CorrelationID, response); err != nil {
					return ports.EffectResult{}, err
				}
				return decodeExtensionSettlement(flow.Identity.ID, response.ExternalResult)
			}, admission, transition)
			if err != nil {
				return nil, err
			}
			return effects.BindStateRevision(ctx, prepared, d.resolver, d.clock, admission, transition)
		}
		operation := control.ProgramPlanLocalEffectOperation
		if transition.Class == catalog.EventRecovery {
			operation = control.ProgramRecoverOperation
		}
		request.Operation = operation
		response, invokeErr := flow.Runtime.InvokeProgram(ctx, request)
		if invokeErr != nil {
			return nil, invokeErr
		}
		if err := validateProgramRuntimeResponse(flow, operation, admission.Invocation.Correlation, response); err != nil {
			return nil, err
		}
		if err := validateProgramWrites(d.program, transition, flow.Identity.ID, response.Writes); err != nil {
			return nil, err
		}
		prepared, err := effects.NewFlowLocalPrepared(admission.Invocation.InvokingPath, flow.Identity.ID, response.Writes, admission, transition)
		if err != nil {
			return nil, err
		}
		return effects.BindStateRevision(ctx, prepared, d.resolver, d.clock, admission, transition)
	}
	extension, ok := d.program.ExtensionByID(transition.Origin.ID)
	if !ok || extension.Runtime == nil {
		return nil, fmt.Errorf("extension runtime %q is unavailable", transition.Origin.ID)
	}
	parameters, err := json.Marshal(admission.Parameters)
	if err != nil {
		return nil, err
	}
	baseRequest := control.ExtensionRequest{
		ProtocolVersion: control.ExtensionProtocolVersion, ExtensionID: extension.Identity.ID, ExtensionVersion: extension.Identity.Version,
		ProgramFingerprint: admission.ExpectedProgramFingerprint, CorrelationID: admission.Invocation.Correlation,
		RepositoryRoot: admission.Invocation.InvokingPath, TransitionID: transition.ID, Parameters: parameters, Settings: extension.Manifest.Settings,
		Capabilities: append([]control.Capability(nil), admission.EffectiveCapabilities...),
	}
	if transition.Class == catalog.EventOwnedExternal {
		prepared, err := effects.NewExtensionExternalPrepared(func(executionContext context.Context) (ports.EffectResult, error) {
			request := baseRequest
			request.Operation = control.ExtensionExecuteExternalOperation
			response, invokeErr := extension.Runtime.Invoke(executionContext, request)
			if invokeErr != nil {
				return ports.EffectResult{}, invokeErr
			}
			if err := validateExtensionResponse(extension, request.Operation, request.CorrelationID, response); err != nil {
				return ports.EffectResult{}, err
			}
			return decodeExtensionSettlement(extension.Identity.ID, response.ExternalResult)
		}, admission, transition)
		if err != nil {
			return nil, err
		}
		return effects.BindStateRevision(ctx, prepared, d.resolver, d.clock, admission, transition)
	}
	operation := control.ExtensionPlanLocalEffectOperation
	if transition.Class == catalog.EventRecovery {
		operation = control.ExtensionRecoverOperation
	}
	baseRequest.Operation = operation
	response, err := extension.Runtime.Invoke(ctx, baseRequest)
	if err != nil {
		return nil, err
	}
	if err := validateExtensionResponse(extension, operation, admission.Invocation.Correlation, response); err != nil {
		return nil, err
	}
	if err := validateProgramWrites(d.program, transition, extension.Identity.ID, response.Writes); err != nil {
		return nil, err
	}
	prepared, err := effects.NewExtensionLocalPrepared(admission.Invocation.InvokingPath, extension.Identity.ID, response.Writes, admission, transition)
	if err != nil {
		return nil, err
	}
	return effects.BindStateRevision(ctx, prepared, d.resolver, d.clock, admission, transition)
}

func validateProgramWrites(program control.ControlProgram, transition catalog.Transition, owner string, writes []control.ResourceWrite) error {
	allowed := map[string]bool{}
	for _, resource := range transition.OwnedResources {
		allowed[resource] = true
	}
	ownership := program.ResourceOwnership()
	for _, write := range writes {
		if !allowed[write.Resource] || ownership[write.Resource] != owner {
			return fmt.Errorf("transition %q planned undeclared resource %q", transition.ID, write.Resource)
		}
	}
	return nil
}

func decodeExtensionSettlement(owner string, raw []byte) (ports.EffectResult, error) {
	var result struct {
		Settlement ports.EffectSettlement `json:"settlement"`
		Detail     string                 `json:"detail,omitempty"`
	}
	if err := decodeStrictExtensionJSON(raw, &result); err != nil {
		return ports.EffectResult{}, fmt.Errorf("component %q returned invalid external settlement", owner)
	}
	if result.Settlement != ports.EffectSettled && result.Settlement != ports.EffectUnknown {
		return ports.EffectResult{}, fmt.Errorf("component %q returned invalid external settlement", owner)
	}
	return ports.EffectResult{Settlement: result.Settlement, Detail: result.Detail}, nil
}

func decodeStrictExtensionJSON(raw []byte, target any) error {
	if len(raw) == 0 {
		return fmt.Errorf("empty JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}
