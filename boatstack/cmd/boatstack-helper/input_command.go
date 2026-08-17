package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	softwareflow "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery"
	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/effects"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/humanidentity"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/plant"
	"github.com/operatorstack/boatstack/boatstack/invocation"
)

type flowInputOptions struct {
	repository         string
	programID          string
	entryID            string
	runID              string
	requestFingerprint string
	answerPath         string
	human              string
	host               string
	format             string
	reason             string
}

func runFlowInput(arguments []string) error {
	if len(arguments) == 0 {
		return fmt.Errorf("usage: boatstack flow input <show|answer|supersede|block> [flags]")
	}
	action := arguments[0]
	flags := flag.NewFlagSet("flow input "+action, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	options := flowInputOptions{repository: ".", host: "cli", format: "json"}
	flags.StringVar(&options.repository, "repo", options.repository, "repository containing the active Flow")
	flags.StringVar(&options.programID, "flow", "", "repository Control Program identity")
	flags.StringVar(&options.entryID, "entry", "", "named Flow entry")
	flags.StringVar(&options.runID, "run-id", "", "opaque active run identity")
	flags.StringVar(&options.requestFingerprint, "request-fingerprint", "", "exact transition-input request fingerprint")
	flags.StringVar(&options.answerPath, "answer", "", "JSON answer object path")
	flags.StringVar(&options.human, "human", "", "human actor recording the answer")
	flags.StringVar(&options.host, "host", options.host, "driver host identity")
	flags.StringVar(&options.format, "format", options.format, "json")
	flags.StringVar(&options.reason, "reason", "", "semantic rejection reason for a new immutable request generation")
	if err := flags.Parse(arguments[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected flow input arguments: %s", strings.Join(flags.Args(), " "))
	}
	if action == "block" {
		return fmt.Errorf("TRANSITION_INPUT_BLOCKED: no input receipt was recorded")
	}
	if action != "show" && action != "answer" && action != "supersede" {
		return fmt.Errorf("unknown flow input action %q", action)
	}
	if options.programID == "" || options.entryID == "" || options.runID == "" || options.requestFingerprint == "" {
		return fmt.Errorf("--flow, --entry, --run-id, and --request-fingerprint are required")
	}
	repository, err := filepath.Abs(options.repository)
	if err != nil {
		return err
	}
	repository, err = filepath.EvalSymlinks(repository)
	if err != nil {
		return err
	}
	options.repository = repository
	lease, err := boatstackruntime.AcquireFlowProjectionLease(repository)
	if err != nil {
		return err
	}
	defer lease.Release()
	ctx := context.Background()
	compiled, store, runtimeContext, err := loadFlowInputContext(ctx, options)
	if err != nil {
		return err
	}
	request, err := store.FindRequest(options.runID, options.requestFingerprint)
	if err != nil {
		return err
	}
	if request.ProgramFingerprint != compiled.Fingerprint || request.EntryID != options.entryID {
		return fmt.Errorf("FLOW_INPUT_REQUEST_MISMATCH: request does not belong to the selected program and entry")
	}
	if request.ControlBundleFingerprint != runtimeContext.controlBundle.Fingerprint {
		return fmt.Errorf("HUMAN_IDENTITY_DRIFT: input request bundle %s does not match current verified bundle %s", request.ControlBundleFingerprint, runtimeContext.controlBundle.Fingerprint)
	}
	if request.AuthorityContextFingerprint != runtimeContext.humanIdentity.ProviderFingerprint {
		return fmt.Errorf("HUMAN_IDENTITY_DRIFT: input request authority context %s does not match current verified identity provider %s", request.AuthorityContextFingerprint, runtimeContext.humanIdentity.ProviderFingerprint)
	}
	if action == "show" {
		receipts, loadErr := store.LoadReceipts(options.runID, request.TransitionID)
		if loadErr != nil {
			return loadErr
		}
		return encodeFlowInputResult(map[string]any{"request": request, "receipts": receipts, "human_identity": runtimeContext.humanIdentity}, options.format)
	}
	if action == "supersede" {
		if options.reason == "" || options.human == "" || options.host == "" {
			return fmt.Errorf("--reason, --human, and --host are required")
		}
		if err := humanidentity.ValidateActor(options.human); err != nil {
			return err
		}
		if runtimeContext.executionScopeFingerprint != request.ExecutionScopeFingerprint {
			return fmt.Errorf("FLOW_INPUT_REQUEST_MISMATCH: execution scope changed after suspension")
		}
		requestContext := invocation.Context{
			RunID: request.RunID, ProgramFingerprint: request.ProgramFingerprint, ExecutionProgramFingerprint: request.ExecutionProgramFingerprint,
			EntryID: request.EntryID, TargetID: request.TargetID, TransitionID: request.TransitionID, StateRevision: request.StateRevision,
			ContextFingerprint: request.ContextFingerprint, ControlBundleFingerprint: request.ControlBundleFingerprint,
			AuthorityContextFingerprint: request.AuthorityContextFingerprint, ExecutionScopeFingerprint: request.ExecutionScopeFingerprint,
		}
		latest, found, latestErr := store.LatestRequest(requestContext)
		if latestErr != nil {
			return latestErr
		}
		if !found || latest.Fingerprint != request.Fingerprint {
			return fmt.Errorf("FLOW_INPUT_REQUEST_SUPERSEDED: only the latest request generation can be superseded")
		}
		receipts, loadErr := store.LoadReceipts(request.RunID, request.TransitionID)
		if loadErr != nil {
			return loadErr
		}
		for _, parameter := range request.Parameters {
			if _, answered := receipts[parameter.ID+"@"+request.Fingerprint]; !answered {
				return fmt.Errorf("FLOW_INPUT_SUPERSESSION_UNANSWERED: request has no immutable answer for parameter %s", parameter.ID)
			}
		}
		next, supersedeErr := invocation.SupersedeRequest(request, options.reason, options.human, options.host, time.Now().UTC())
		if supersedeErr != nil {
			return supersedeErr
		}
		if saveErr := store.SaveRequest(next); saveErr != nil {
			return saveErr
		}
		return encodeFlowInputResult(map[string]any{
			"prior_request_fingerprint": request.Fingerprint, "request": next, "status": "superseded",
		}, options.format)
	}
	if options.answerPath == "" || options.human == "" || options.host == "" {
		return fmt.Errorf("--answer, --human, and --host are required")
	}
	if err := humanidentity.ValidateActor(options.human); err != nil {
		return err
	}
	answers, err := loadFlowInputAnswers(options.answerPath)
	if err != nil {
		return err
	}
	receipts, err := recordFlowInputAnswers(store, compiled, request, runtimeContext, answers, options.human, options.host)
	if err != nil {
		return err
	}
	return encodeFlowInputResult(map[string]any{"request_fingerprint": request.Fingerprint, "receipts": receipts, "status": "recorded"}, options.format)
}

type flowInputRuntimeContext struct {
	executionScopeFingerprint string
	controlBundle             boatstackruntime.ControlBundleSnapshot
	humanIdentity             humanidentity.Presentation
}

func loadFlowInputContext(ctx context.Context, options flowInputOptions) (controlprogram.Compiled, invocation.Store, flowInputRuntimeContext, error) {
	raw, err := os.ReadFile(filepath.Join(options.repository, ".boatstack", "flows", options.programID+".flow.ir.json"))
	if err != nil {
		return controlprogram.Compiled{}, invocation.Store{}, flowInputRuntimeContext{}, fmt.Errorf("FLOW_ARTIFACT_REQUIRED: %w", err)
	}
	artifact, err := controlprogram.LoadArtifact(bytes.NewReader(raw))
	if err != nil {
		return controlprogram.Compiled{}, invocation.Store{}, flowInputRuntimeContext{}, err
	}
	bindingResolver, err := softwareflow.NewResolver(ctx)
	if err != nil {
		return controlprogram.Compiled{}, invocation.Store{}, flowInputRuntimeContext{}, err
	}
	compiled, err := checkArtifactForCurrentProject(options.repository, artifact, bindingResolver)
	if err != nil {
		return controlprogram.Compiled{}, invocation.Store{}, flowInputRuntimeContext{}, err
	}
	resolver, err := plant.NewResolver("")
	if err != nil {
		return controlprogram.Compiled{}, invocation.Store{}, flowInputRuntimeContext{}, err
	}
	invoking, err := resolver.ResolveInvocation(ctx, options.repository, options.host, "flow-input-"+options.runID)
	if err != nil {
		return controlprogram.Compiled{}, invocation.Store{}, flowInputRuntimeContext{}, err
	}
	layout, invoking, err := resolver.ResolveLayout(ctx, invoking)
	if err != nil {
		return controlprogram.Compiled{}, invocation.Store{}, flowInputRuntimeContext{}, err
	}
	executionScopeFingerprint, err := flowExecutionScopeFingerprint(invoking)
	if err != nil {
		return controlprogram.Compiled{}, invocation.Store{}, flowInputRuntimeContext{}, err
	}
	controlBundle, _, err := bindControlBundle(ctx, options.repository, "", nil)
	if err != nil {
		return controlprogram.Compiled{}, invocation.Store{}, flowInputRuntimeContext{}, err
	}
	presentation, err := humanIdentityPresentationForRepositoryBound(ctx, options.repository, options.host, "flow-input-"+options.runID, controlBundle.Source, nil)
	if err != nil {
		return controlprogram.Compiled{}, invocation.Store{}, flowInputRuntimeContext{}, err
	}
	return compiled, invocation.Store{Root: layout.FlowRoot, Writer: effects.NewRuntimeStore()}, flowInputRuntimeContext{
		executionScopeFingerprint: executionScopeFingerprint, controlBundle: controlBundle.Source, humanIdentity: presentation,
	}, nil
}

func loadFlowInputAnswers(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var values map[string]any
	if err := decoder.Decode(&values); err != nil {
		return nil, fmt.Errorf("decode input answers: %w", err)
	}
	if nested, ok := values["answers"].(map[string]any); ok && len(values) == 1 {
		values = nested
	}
	result := make(map[string]string, len(values))
	for name, value := range values {
		switch typed := value.(type) {
		case string:
			result[name] = typed
		case bool, json.Number, map[string]any, []any:
			encoded, encodeErr := json.Marshal(typed)
			if encodeErr != nil {
				return nil, encodeErr
			}
			result[name] = string(encoded)
		default:
			return nil, fmt.Errorf("answer %q has an unsupported null value", name)
		}
	}
	return result, nil
}

func recordFlowInputAnswers(store invocation.Store, compiled controlprogram.Compiled, request invocation.InputRequest, runtimeContext flowInputRuntimeContext, answers map[string]string, actor, host string) ([]invocation.InputReceipt, error) {
	if runtimeContext.executionScopeFingerprint != request.ExecutionScopeFingerprint {
		return nil, fmt.Errorf("FLOW_INPUT_REQUEST_MISMATCH: execution scope changed after suspension")
	}
	if runtimeContext.humanIdentity.ProviderFingerprint != request.AuthorityContextFingerprint {
		return nil, fmt.Errorf("HUMAN_IDENTITY_DRIFT: input request identity provider changed after suspension")
	}
	transition, ok := findCompiledTransition(compiled.Document.Transitions, request.TransitionID)
	if !ok {
		return nil, fmt.Errorf("FLOW_TRANSITION_UNKNOWN: %s", request.TransitionID)
	}
	operator, ok := findCompiledOperator(compiled.Document.Operators, transition.Operator)
	if !ok {
		return nil, fmt.Errorf("FLOW_OPERATOR_UNKNOWN: %s", transition.Operator)
	}
	contracts := map[string]controlprogram.OperatorParameter{}
	for _, contract := range operator.Parameters {
		contracts[contract.ID] = contract
	}
	producers := map[string]controlprogram.ParameterProducer{}
	for _, binding := range transition.Parameters {
		producers[binding.Parameter] = binding.Producer
	}
	requested := map[string]invocation.RequestedParameter{}
	for _, parameter := range request.Parameters {
		requested[parameter.ID] = parameter
	}
	if len(answers) != len(requested) {
		return nil, fmt.Errorf("FLOW_INPUT_ANSWER_INCOMPLETE: answer must contain exactly the requested parameter IDs")
	}
	prior, err := store.LoadReceipts(request.RunID, request.TransitionID)
	if err != nil {
		return nil, err
	}
	result := make([]invocation.InputReceipt, 0, len(answers))
	for parameterID, value := range answers {
		requestedParameter, requestedOK := requested[parameterID]
		contract, contractOK := contracts[parameterID]
		producer, producerOK := producers[parameterID]
		if !requestedOK || !contractOK || !producerOK || producer.Kind != controlprogram.ParameterSourceHostInput || producer.Request == nil {
			return nil, fmt.Errorf("FLOW_INPUT_ANSWER_UNKNOWN: %s", parameterID)
		}
		if requestedParameter.Secret {
			return nil, fmt.Errorf("FLOW_SECRET_STORE_UNAVAILABLE: parameter %s requires a trusted secret store", parameterID)
		}
		if err := invocation.ValidateAnswer(contract, value, ""); err != nil {
			return nil, fmt.Errorf("FLOW_INPUT_ANSWER_INVALID: parameter %s: %w", parameterID, err)
		}
		if existing, exists := prior[parameterID+"@"+request.Fingerprint]; exists {
			if existing.Value != value || existing.RequestFingerprint != request.Fingerprint {
				return nil, fmt.Errorf("FLOW_INPUT_ANSWER_CONFLICT: parameter %s already has a different receipt", parameterID)
			}
			result = append(result, existing)
			continue
		}
		receipt, sealErr := invocation.SealReceipt(invocation.InputReceipt{
			RunID: request.RunID, ProgramFingerprint: request.ProgramFingerprint, ExecutionProgramFingerprint: request.ExecutionProgramFingerprint,
			EntryID: request.EntryID, TargetID: request.TargetID,
			TransitionID: request.TransitionID, ParameterID: parameterID, Type: contract.Type, Value: value,
			ProducerFingerprint: invocation.ProducerFingerprint(producer), RequestFingerprint: request.Fingerprint,
			StateRevision: request.StateRevision, ContextFingerprint: request.ContextFingerprint, ControlBundleFingerprint: request.ControlBundleFingerprint,
			AuthorityContextFingerprint: request.AuthorityContextFingerprint, ExecutionScopeFingerprint: runtimeContext.executionScopeFingerprint,
			Actor: actor, Host: host, AuthorityReceipts: []string{"human:" + actor}, Scope: "transition",
		})
		if sealErr != nil {
			return nil, sealErr
		}
		if err := store.SaveReceipt(receipt); err != nil {
			return nil, err
		}
		result = append(result, receipt)
	}
	return result, nil
}

func findCompiledTransition(values []controlprogram.Transition, id string) (controlprogram.Transition, bool) {
	for _, value := range values {
		if value.ID == id {
			return value, true
		}
	}
	return controlprogram.Transition{}, false
}

func findCompiledOperator(values []controlprogram.Operator, id string) (controlprogram.Operator, bool) {
	for _, value := range values {
		if value.ID == id {
			return value, true
		}
	}
	return controlprogram.Operator{}, false
}

func encodeFlowInputResult(value any, format string) error {
	if format != "json" {
		return fmt.Errorf("flow input currently requires --format json")
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
