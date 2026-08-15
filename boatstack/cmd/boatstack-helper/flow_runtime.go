package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	"github.com/operatorstack/boatstack/boatstack/core"
	softwareflow "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery"
	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/delegation"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/effects"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/foregroundwork"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/plant"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
	"github.com/operatorstack/boatstack/boatstack/invocation"
	general "github.com/operatorstack/boatstack/boatstack/kernel"
)

var flowSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func bindFlowEntry(ctx context.Context, options commandOptions) (commandOptions, error) {
	if options.programID == "" && options.entryID == "" {
		return options, nil
	}
	if !flowSegment.MatchString(options.programID) || !flowSegment.MatchString(options.entryID) {
		return commandOptions{}, fmt.Errorf("FLOW_ENTRY_INVALID: --flow and --entry require semantic identifiers")
	}
	if len(options.parameters) != 0 && !options.maintenanceParameterSurface {
		return commandOptions{}, fmt.Errorf("FLOW_PARAMETER_BYPASS: repository Flow parameters must come from compiled producer declarations")
	}
	repository, err := filepath.Abs(options.repository)
	if err != nil {
		return commandOptions{}, err
	}
	repository, err = filepath.EvalSymlinks(repository)
	if err != nil {
		return commandOptions{}, err
	}
	// Preserve the exact root used to validate and compile the Flow. Downstream
	// control-bundle verification rejects relative or symlinked repository
	// paths, including the generated command's ordinary "--repo ." form.
	options.repository = repository
	artifactPath := filepath.Join(repository, ".boatstack", "flows", options.programID+".flow.ir.json")
	artifactRaw, err := os.ReadFile(artifactPath)
	if err != nil {
		return commandOptions{}, fmt.Errorf("FLOW_ARTIFACT_REQUIRED: %w", err)
	}
	artifact, err := controlprogram.LoadArtifact(bytes.NewReader(artifactRaw))
	if err != nil {
		return commandOptions{}, err
	}
	if artifact.Program.Program.ID != options.programID {
		return commandOptions{}, fmt.Errorf("FLOW_PROGRAM_MISMATCH: selected %q but artifact declares %q", options.programID, artifact.Program.Program.ID)
	}
	resolver, err := softwareflow.NewResolver(ctx)
	if err != nil {
		return commandOptions{}, err
	}
	compiled, err := controlprogram.CheckArtifact(repository, artifact, flowCompilerVersion, resolver, generateSoftwareFlowSkills)
	if err != nil {
		return commandOptions{}, err
	}
	if options.flowProgramFingerprint != "" && options.flowProgramFingerprint != compiled.Fingerprint {
		return commandOptions{}, fmt.Errorf("FLOW_PROGRAM_DRIFT: run fingerprint does not match the current artifact")
	}
	options.flowProgramFingerprint = compiled.Fingerprint
	objective, err := softwareflow.ObjectiveForEntry(ctx, compiled, resolver, options.entryID)
	if err != nil {
		return commandOptions{}, err
	}
	entry, ok := findEntry(compiled.Document.Entries, options.entryID)
	if !ok {
		return commandOptions{}, fmt.Errorf("FLOW_ENTRY_UNKNOWN: %s", options.entryID)
	}
	options, err = bindActiveFlowContext(ctx, repository, options, objective)
	if err != nil {
		return commandOptions{}, err
	}
	plan, deliveryID, err := resolveBoundPlan(repository, entry, objective, options)
	if err != nil {
		return commandOptions{}, err
	}
	planFingerprint := ""
	if plan != "" {
		planRaw, readErr := os.ReadFile(plan)
		if readErr != nil {
			return commandOptions{}, fmt.Errorf("FLOW_INPUT_REQUIRED: read selected plan: %w", readErr)
		}
		planDigest := sha256.Sum256(planRaw)
		planFingerprint = hex.EncodeToString(planDigest[:])
		options, err = bindSelectedPlanRun(options, repository, compiled.Fingerprint, deliveryID, planFingerprint)
		if err != nil {
			return commandOptions{}, err
		}
	} else if options.runID == "" {
		return commandOptions{}, fmt.Errorf("FLOW_ACTIVE_RUN_INVALID: active abandonment has no committed run identity")
	}
	options.workInputs = map[string]protocol.WorkInputValue{}
	for _, input := range entry.Inputs {
		if plan != "" {
			options.workInputs[input.ID] = protocol.WorkInputValue{Value: plan, Fingerprint: planFingerprint}
		}
	}
	options.repository = repository
	if options.targetID == "" {
		options.targetID = string(objective.TargetID)
	}
	if options.trustedObjectiveClass == "" {
		options.trustedObjectiveClass = string(objective.TrustedClass)
	}
	if options.deliveryID == "" {
		options.deliveryID = deliveryID
	}
	expectedObjectiveID := "objective-" + options.programID + "-" + options.entryID + "-" + deliveryID
	if options.objectiveID == "" {
		options.objectiveID = expectedObjectiveID
	}
	if options.targetID != string(objective.TargetID) || options.trustedObjectiveClass != string(objective.TrustedClass) || options.deliveryID != deliveryID || options.objectiveID != expectedObjectiveID {
		return commandOptions{}, fmt.Errorf("FLOW_CONTEXT_MISMATCH: objective or delivery changed across the run")
	}
	bundle, bundleFingerprint, err := bindControlBundle(ctx, repository, "", nil)
	if err != nil {
		return commandOptions{}, err
	}
	options.controlBundle = bundle
	options.controlBundleFingerprint = bundleFingerprint
	// Program reconciliation is an installation-authority transition. It must
	// not consume or create product delegation because its accepted effect
	// changes the exact bundle to which product delegation is bound.
	if entry.Delegation != nil && options.transitionID != "installation.reconcile-update" {
		contextResolver, resolverErr := plant.NewResolver("")
		if resolverErr != nil {
			return commandOptions{}, resolverErr
		}
		host := options.host
		if host == "" {
			host = "cli"
		}
		invocation, invocationErr := contextResolver.ResolveInvocation(ctx, repository, host, "flow-delegation-request")
		if invocationErr != nil {
			return commandOptions{}, invocationErr
		}
		description := entry.Description
		if description == "" {
			description = fmt.Sprintf("Run %s/%s to %s", options.programID, options.entryID, objective.TargetID)
		}
		delegationRequest := delegation.Request{
			RunID: options.runID, ProgramID: options.programID, ProgramFingerprint: compiled.Fingerprint,
			ControlBundleFingerprint: bundleFingerprint,
			EntryID:                  options.entryID, TargetID: string(objective.TargetID), ObjectiveID: options.objectiveID, DeliveryID: deliveryID,
			InputFingerprints: []string{planFingerprint}, RepositoryID: invocation.RepositoryID, GitCommonID: invocation.GitCommonID,
			InitialWorktreeID: invocation.WorktreeID, InitialRef: invocation.Ref,
			BindingFingerprint: entry.Delegation.Fingerprint, RequestedAuthorities: append([]string(nil), entry.Delegation.Authorities...),
			Description: description,
		}
		layout, _, layoutErr := contextResolver.ResolveLayout(ctx, invocation)
		if layoutErr != nil {
			return commandOptions{}, layoutErr
		}
		recordPath, pathErr := delegation.Path(layout.FlowRoot, options.runID)
		if pathErr != nil {
			return commandOptions{}, pathErr
		}
		if record, loadErr := delegation.Load(recordPath); loadErr == nil {
			bound := record.Request
			inputDrift := !options.activeFlowBound && strings.Join(bound.InputFingerprints, "\x00") != strings.Join(delegationRequest.InputFingerprints, "\x00")
			if bound.RunID != delegationRequest.RunID || bound.ProgramID != delegationRequest.ProgramID || bound.ProgramFingerprint != delegationRequest.ProgramFingerprint || bound.ControlBundleFingerprint != delegationRequest.ControlBundleFingerprint || bound.EntryID != delegationRequest.EntryID || bound.TargetID != delegationRequest.TargetID || bound.ObjectiveID != delegationRequest.ObjectiveID || bound.DeliveryID != delegationRequest.DeliveryID || inputDrift || bound.RepositoryID != delegationRequest.RepositoryID || bound.GitCommonID != delegationRequest.GitCommonID || bound.BindingFingerprint != delegationRequest.BindingFingerprint || strings.Join(bound.RequestedAuthorities, "\x00") != strings.Join(delegationRequest.RequestedAuthorities, "\x00") || bound.Description != delegationRequest.Description {
				return commandOptions{}, fmt.Errorf("DELEGATION_DRIFT: current Flow context does not match the authorized request (bundle %s, authorized %s)", delegationRequest.ControlBundleFingerprint, bound.ControlBundleFingerprint)
			}
			delegationRequest = bound
		} else if !os.IsNotExist(loadErr) {
			return commandOptions{}, loadErr
		}
		fingerprint, fingerprintErr := delegationRequest.Fingerprint()
		if fingerprintErr != nil {
			return commandOptions{}, fingerprintErr
		}
		options.delegationBindingFingerprint = entry.Delegation.Fingerprint
		options.delegationRequestFingerprint = fingerprint
		options.delegationAuthorities = append(options.delegationAuthorities[:0], entry.Delegation.Authorities...)
		options.delegationDescription = description
		options.delegationRequest = delegationRequest
	}
	if options.transitionID != "" {
		_, repositoryTransition := findCompiledTransition(compiled.Document.Transitions, options.transitionID)
		if repositoryTransition {
			options, err = materializeFlowInvocation(ctx, compiled, entry, options, options.controlBundle)
			if err != nil || options.inputRequest != nil {
				return options, err
			}
		} else if err := bindInternalFlowContextParameters(ctx, &options); err != nil {
			return commandOptions{}, err
		}
		parameters, parseErr := parseParameters(options.parameters)
		if parseErr != nil {
			return commandOptions{}, parseErr
		}
		bundle, bundleFingerprint, err = bindControlBundle(ctx, repository, catalog.TransitionID(options.transitionID), parameters)
		if err != nil {
			return commandOptions{}, err
		}
		options.controlBundle, options.controlBundleFingerprint = bundle, bundleFingerprint
		if repositoryTransition && options.invocationEvidence == nil {
			return commandOptions{}, fmt.Errorf("FLOW_INVOCATION_INCOMPLETE: materialization produced no ready evidence")
		}
		if repositoryTransition {
			boundEvidence, bindErr := invocation.BindControlBundle(*options.invocationEvidence, bundle.Fingerprint)
			if bindErr != nil {
				return commandOptions{}, bindErr
			}
			options.invocationEvidence = &boundEvidence
		}
	}
	return options, nil
}

func bindInternalFlowContextParameters(ctx context.Context, options *commandOptions) error {
	manifest, err := core.System().CoreManifest(ctx)
	if err != nil {
		return err
	}
	parameters, err := parseParameters(options.parameters)
	if err != nil {
		return err
	}
	for _, transition := range manifest.Transitions {
		if string(transition.ID) != options.transitionID {
			continue
		}
		declared := map[string]bool{}
		for _, parameter := range transition.Parameters {
			declared[parameter.Name] = true
			value := ""
			switch parameter.Name {
			case "target_id":
				value = options.targetID
			case "delivery_id":
				value = options.deliveryID
			}
			if value != "" {
				if err := bindFlowContextParameter(options, parameters, parameter.Name, value); err != nil {
					return err
				}
			}
		}
		for _, parameter := range parameters {
			if !declared[parameter.Name] {
				return fmt.Errorf("FLOW_PARAMETER_BYPASS: internal transition %s does not declare parameter %s", options.transitionID, parameter.Name)
			}
		}
		return nil
	}
	return fmt.Errorf("FLOW_TRANSITION_UNKNOWN: %s", options.transitionID)
}

func bindFlowContextParameter(options *commandOptions, parameters protocol.Parameters, name, value string) error {
	if actual, exists := parameters.Get(name); exists {
		if actual != value {
			return fmt.Errorf("FLOW_INPUT_MISMATCH: parameter %s conflicts with the entry-resolved value", name)
		}
		return nil
	}
	options.parameters = append(options.parameters, name+"="+value)
	return nil
}

func materializeFlowInvocation(ctx context.Context, compiled controlprogram.Compiled, entry controlprogram.Entry, options commandOptions, bundle *boatstackruntime.ControlBundleContract) (commandOptions, error) {
	var transition *controlprogram.Transition
	for index := range compiled.Document.Transitions {
		if compiled.Document.Transitions[index].ID == options.transitionID {
			transition = &compiled.Document.Transitions[index]
			break
		}
	}
	if transition == nil {
		return commandOptions{}, fmt.Errorf("FLOW_TRANSITION_UNKNOWN: %s", options.transitionID)
	}
	var operator *controlprogram.Operator
	for index := range compiled.Document.Operators {
		if compiled.Document.Operators[index].ID == transition.Operator {
			operator = &compiled.Document.Operators[index]
			break
		}
	}
	if operator == nil {
		return commandOptions{}, fmt.Errorf("FLOW_OPERATOR_UNKNOWN: %s", transition.Operator)
	}
	host := options.host
	if host == "" {
		host = "cli"
	}
	if options.correlationID == "" {
		options.correlationID = "flow-" + options.runID
	}
	plantResolver, err := plant.NewResolver("")
	if err != nil {
		return commandOptions{}, err
	}
	invocationContext, err := plantResolver.ResolveInvocation(ctx, options.repository, host, options.correlationID)
	if err != nil {
		return commandOptions{}, err
	}
	layout, invocationContext, err := plantResolver.ResolveLayout(ctx, invocationContext)
	if err != nil {
		return commandOptions{}, err
	}
	state := durable.State{}
	if raw, readErr := os.ReadFile(layout.StatePath); readErr == nil {
		state, err = durable.DecodeState(raw)
		if err != nil {
			return commandOptions{}, fmt.Errorf("FLOW_ACTIVE_RUN_INVALID: decode durable state: %w", err)
		}
	} else if !os.IsNotExist(readErr) {
		return commandOptions{}, readErr
	}
	bundleFingerprint := ""
	if bundle != nil {
		bundleFingerprint = bundle.Fingerprint
	}
	contextFingerprint, err := general.Fingerprint(struct {
		Invocation       model.InvocationContext `json:"invocation"`
		StateRevision    uint64                  `json:"state_revision"`
		Program          string                  `json:"program"`
		ExecutionProgram string                  `json:"execution_program"`
		Entry            string                  `json:"entry"`
		Target           string                  `json:"target"`
		Transition       string                  `json:"transition"`
	}{invocationContext, state.Revision, compiled.Fingerprint, state.ProgramFingerprint, entry.ID, options.targetID, transition.ID})
	if err != nil {
		return commandOptions{}, err
	}
	if len(state.ProgramFingerprint) != 64 {
		return commandOptions{}, fmt.Errorf("FLOW_PROGRAM_UNBOUND: repository transition invocation requires an admitted executable program")
	}
	entryInputs := map[string]invocation.Value{}
	for id, value := range options.workInputs {
		entryInputs[id] = invocation.Value{Type: controlprogram.ValueTypeDefinition{Kind: "string"}, Canonical: value.Value, Provenance: "entry-input:" + value.Fingerprint}
	}
	stateValues := softwareflow.StateParameterValues(state)
	receiptValues := map[string]invocation.Value{}
	if value, ok := stateValues["preview_fingerprint"]; ok {
		receiptValues["publication.preview/preview_fingerprint"] = value
	}
	if value, ok := stateValues["publication_id"]; ok {
		receiptValues["publication.observe/publication_id"] = value
	}
	if value, ok := stateValues["transaction_id"]; ok {
		receiptValues["publication.execute/transaction_id"] = value
	}
	workOutputs := map[string]invocation.Value{}
	workByID := map[string]controlprogram.WorkContract{}
	for _, work := range compiled.Document.Work {
		workByID[work.ID] = work
	}
	for _, binding := range transition.Parameters {
		if binding.Producer.Kind != controlprogram.ParameterSourceWorkOutput {
			continue
		}
		work, declared := workByID[binding.Producer.Work]
		if !declared {
			return commandOptions{}, fmt.Errorf("FLOW_WORK_EVIDENCE_STALE: producer references unknown work %q", binding.Producer.Work)
		}
		record, loadErr := foregroundwork.LoadRecord(layout, options.runID, work.ID)
		if loadErr != nil {
			if os.IsNotExist(loadErr) {
				return commandOptions{}, fmt.Errorf("FLOW_WORK_EVIDENCE_STALE: work %q has no current result", work.ID)
			}
			return commandOptions{}, loadErr
		}
		if record.Status != foregroundwork.StatusCompleted || record.Result == nil {
			return commandOptions{}, fmt.Errorf("FLOW_WORK_EVIDENCE_STALE: work %q is not complete", work.ID)
		}
		if err := validateWorkOutputProducer(record, work, compiled, entry, options, invocationContext); err != nil {
			return commandOptions{}, err
		}
		foundOutput := false
		for _, output := range record.Result.Outputs {
			if output.ID != binding.Producer.Output {
				continue
			}
			foundOutput = true
			kind := "string"
			if output.MediaType == "application/json" {
				kind = "json"
			}
			workOutputs[work.ID+"/"+output.ID] = invocation.Value{Type: controlprogram.ValueTypeDefinition{Kind: kind}, Canonical: output.Content, Provenance: "work-output:" + output.SHA256, ProducerFingerprint: record.Result.ResultFingerprint}
		}
		if !foundOutput {
			return commandOptions{}, fmt.Errorf("FLOW_WORK_EVIDENCE_STALE: work %q lacks output %q", work.ID, binding.Producer.Output)
		}
	}
	store := invocation.Store{Root: layout.FlowRoot, Writer: effects.NewRuntimeStore()}
	inputReceipts, err := store.LoadReceipts(options.runID, transition.ID)
	if err != nil {
		return commandOptions{}, err
	}
	executionScopeFingerprint, err := flowExecutionScopeFingerprint(invocationContext)
	if err != nil {
		return commandOptions{}, err
	}
	materializationContext := invocation.Context{
		RunID: options.runID, ProgramFingerprint: compiled.Fingerprint, ExecutionProgramFingerprint: state.ProgramFingerprint,
		EntryID: entry.ID, TargetID: options.targetID, TransitionID: transition.ID,
		StateRevision: state.Revision, ContextFingerprint: contextFingerprint, ControlBundleFingerprint: bundleFingerprint,
		ExecutionScopeFingerprint: executionScopeFingerprint,
		EntryInputs:               entryInputs, State: stateValues, Receipts: receiptValues, WorkOutputs: workOutputs, InputReceipts: inputReceipts,
	}
	bindingResolver, err := softwareflow.NewResolver(ctx)
	if err != nil {
		return commandOptions{}, err
	}
	result, err := invocation.Materialize(operator.Parameters, transition.Parameters, materializationContext, softwareflow.RuntimeParameterResolver{Context: ctx, Repository: options.repository, DeliveryID: options.deliveryID, Binding: bindingResolver})
	if err != nil {
		return commandOptions{}, err
	}
	if result.Blocker != nil {
		return commandOptions{}, fmt.Errorf("%s: %s", result.Blocker.Code, result.Blocker.Detail)
	}
	options.parameters, options.inputRequest, options.invocationEvidence = nil, result.Request, result.Ready
	if result.Request != nil {
		if err := store.SaveRequest(*result.Request); err != nil {
			return commandOptions{}, err
		}
		return options, nil
	}
	for _, parameter := range result.Ready.Parameters {
		if parameter.SecretReference != "" {
			return commandOptions{}, fmt.Errorf("FLOW_SECRET_STORE_UNAVAILABLE: parameter %s requires a trusted secret store", parameter.Name)
		}
		options.parameters = append(options.parameters, parameter.Name+"="+parameter.Value)
	}
	return options, nil
}

func validateWorkOutputProducer(record foregroundwork.Record, work controlprogram.WorkContract, compiled controlprogram.Compiled, entry controlprogram.Entry, options commandOptions, current model.InvocationContext) error {
	contract, err := softwareflow.RuntimeWorkContract(work)
	if err != nil {
		return fmt.Errorf("FLOW_WORK_EVIDENCE_STALE: %w", err)
	}
	request := record.Request
	result := record.Result
	if result == nil || result.Validate() != nil || request.RunID != options.runID || request.ProgramID != compiled.Document.Program.ID || request.ProgramFingerprint != compiled.Fingerprint || request.EntryID != entry.ID || request.Objective.ID != options.objectiveID || string(request.Objective.TargetID) != options.targetID || request.Objective.DeliveryID != options.deliveryID || request.RepositoryID != current.RepositoryID || request.GitCommonID != current.GitCommonID || request.WorktreeID != current.WorktreeID || request.Ref != current.Ref || request.Contract.ID != contract.ID || request.Contract.Fingerprint != contract.Fingerprint || result.ContractID != contract.ID || result.ContractFingerprint != contract.Fingerprint || result.RequestFingerprint != request.Fingerprint || result.RepositoryID != current.RepositoryID || result.WorktreeID != current.WorktreeID {
		return fmt.Errorf("FLOW_WORK_EVIDENCE_STALE: work %q belongs to a different run, program, entry, objective, scope, or contract", work.ID)
	}
	producerTransition := ""
	for _, candidate := range compiled.Document.Transitions {
		if candidate.Work == work.ID {
			if producerTransition != "" {
				return fmt.Errorf("FLOW_WORK_EVIDENCE_STALE: work %q has ambiguous producer transitions", work.ID)
			}
			producerTransition = candidate.ID
		}
	}
	if producerTransition == "" || string(request.TransitionID) != producerTransition || result.TransitionID != request.TransitionID {
		return fmt.Errorf("FLOW_WORK_EVIDENCE_STALE: work %q transition context changed", work.ID)
	}
	expectedInputs := map[string]protocol.WorkInputValue{}
	for _, input := range work.Inputs {
		value, ok := options.workInputs[input.EntryInput]
		if !ok {
			return fmt.Errorf("FLOW_WORK_EVIDENCE_STALE: work %q entry input %q is unavailable", work.ID, input.EntryInput)
		}
		expectedInputs[input.ID] = value
	}
	if len(request.Inputs) != len(expectedInputs) {
		return fmt.Errorf("FLOW_WORK_EVIDENCE_STALE: work %q input binding changed", work.ID)
	}
	for _, input := range request.Inputs {
		expected, ok := expectedInputs[input.ID]
		if !ok || input.Value != expected.Value || input.Fingerprint != expected.Fingerprint {
			return fmt.Errorf("FLOW_WORK_EVIDENCE_STALE: work %q input binding changed", work.ID)
		}
	}
	return nil
}

func flowExecutionScopeFingerprint(value model.InvocationContext) (string, error) {
	return general.Fingerprint(struct {
		RepositoryID string `json:"repository_id"`
		GitCommonID  string `json:"git_common_id"`
		WorktreeID   string `json:"worktree_id"`
		Ref          string `json:"ref"`
	}{value.RepositoryID, value.GitCommonID, value.WorktreeID, value.Ref})
}

func populateProjectConfigFingerprint(options *commandOptions) error {
	parameters, err := parseParameters(options.parameters)
	if err != nil {
		return err
	}
	if _, exists := parameters.Get("config_sha256"); exists {
		return nil
	}
	path, exists := parameters.Get("config_path")
	if !exists {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	_, fingerprint, err := protocol.ProjectConfigFingerprint(raw)
	if err != nil {
		return err
	}
	options.parameters = append(options.parameters, "config_sha256="+fingerprint)
	return nil
}

func bindSelectedPlanRun(options commandOptions, repository, programFingerprint, deliveryID, planFingerprint string) (commandOptions, error) {
	if options.activeFlowBound {
		return options, nil
	}
	repositoryIdentity, err := flowRepositoryIdentity(repository)
	if err != nil {
		return commandOptions{}, err
	}
	runID := flowRunID(repositoryIdentity, programFingerprint, options.entryID, deliveryID, planFingerprint)
	if options.runID != "" && options.runID != runID {
		return commandOptions{}, fmt.Errorf("FLOW_RUN_MISMATCH: run ID does not identify the selected plan and repository")
	}
	options.runID = runID
	return options, nil
}

func bindActiveFlowContext(ctx context.Context, repository string, options commandOptions, entryObjective softwareflow.EntryObjective) (commandOptions, error) {
	resolver, err := plant.NewResolver("")
	if err != nil {
		return commandOptions{}, err
	}
	host := options.host
	if host == "" {
		host = "cli"
	}
	invocation, err := resolver.ResolveInvocation(ctx, repository, host, "flow-entry-resume")
	if err != nil {
		common, commonErr := flowRepositoryIdentity(repository)
		if commonErr == nil {
			if _, stateErr := os.Stat(filepath.Join(common, "boatstack")); os.IsNotExist(stateErr) {
				return options, nil
			}
		}
		if _, stateErr := os.Stat(filepath.Join(repository, ".git", "boatstack")); stateErr != nil {
			return options, nil
		}
		return commandOptions{}, err
	}
	layout, _, err := resolver.ResolveLayout(ctx, invocation)
	if err != nil {
		return commandOptions{}, err
	}
	raw, err := os.ReadFile(layout.StatePath)
	if os.IsNotExist(err) {
		return options, nil
	}
	if err != nil {
		return commandOptions{}, fmt.Errorf("FLOW_ACTIVE_RUN_INVALID: read durable state: %w", err)
	}
	state, err := durable.DecodeState(raw)
	if err != nil {
		return commandOptions{}, fmt.Errorf("FLOW_ACTIVE_RUN_INVALID: decode durable state: %w", err)
	}
	active, ok := state.ActiveObjective()
	if !ok {
		return options, nil
	}
	prefix := "objective-" + options.programID + "-" + options.entryID + "-"
	receipt, found, findErr := effects.FindLatestCommittedFlowForObjective(layout, invocation, active, state.Revision)
	if findErr != nil {
		return commandOptions{}, fmt.Errorf("FLOW_ACTIVE_RUN_INVALID: inspect committed flow receipts: %w", findErr)
	}
	if !found || !strings.HasPrefix(receipt.FlowID, "run-") {
		return commandOptions{}, fmt.Errorf("FLOW_ACTIVE_RUN_INVALID: active objective has no committed run identity")
	}
	if active.TargetID == entryObjective.TargetID && strings.HasPrefix(active.ID, prefix) {
		bound, bindErr := bindCommittedActiveRun(options, active, receipt)
		if bindErr != nil {
			return commandOptions{}, bindErr
		}
		return bound, nil
	}
	if entryObjective.TrustedClass == model.ObjectiveAbandoned {
		repositoryIdentity, identityErr := flowRepositoryIdentity(repository)
		if identityErr != nil {
			return commandOptions{}, identityErr
		}
		expectedRunID := flowRunID(repositoryIdentity, options.flowProgramFingerprint, options.entryID, active.DeliveryID, "active-run:"+receipt.FlowID)
		if options.runID != "" && options.runID != expectedRunID {
			return commandOptions{}, fmt.Errorf("FLOW_RUN_MISMATCH: run ID does not identify the active delivery")
		}
		options.runID = expectedRunID
		options.deliveryID, options.activeFlowBound = active.DeliveryID, true
		return options, nil
	}
	return commandOptions{}, fmt.Errorf("FLOW_ACTIVE_RUN_CONFLICT: delivery %q is active under objective %q; abandon it before selecting another inbox plan", active.DeliveryID, active.ID)
}

func bindCommittedActiveRun(options commandOptions, active model.Objective, receipt protocol.TransitionReceipt) (commandOptions, error) {
	if options.runID != "" && options.runID != receipt.FlowID {
		return commandOptions{}, fmt.Errorf("FLOW_RUN_MISMATCH: run ID does not identify the committed active delivery")
	}
	options.runID, options.deliveryID = receipt.FlowID, active.DeliveryID
	options.objectiveID, options.targetID, options.trustedObjectiveClass = active.ID, string(active.TargetID), string(active.TrustedObjectiveClass())
	options.activeFlowBound = true
	return options, nil
}

func bindRPCFlowEntry(ctx context.Context, request surfaces.Request) (surfaces.Request, error) {
	return bindRPCFlowEntryWithMaintenance(ctx, request, false)
}

func bindRPCFlowEntryWithMaintenance(ctx context.Context, request surfaces.Request, maintenanceParameterSurface bool) (surfaces.Request, error) {
	if request.ProgramID == "" && request.EntryID == "" {
		return request, nil
	}
	repositoryTransition, err := repositoryFlowDeclaresTransition(request.Repository, request.ProgramID, string(request.TransitionID))
	if err != nil {
		return surfaces.Request{}, err
	}
	parameterFlags := make([]string, 0, len(request.Parameters))
	for _, parameter := range request.Parameters {
		parameterFlags = append(parameterFlags, parameter.Name+"="+parameter.Value)
	}
	bound, err := bindFlowEntry(ctx, commandOptions{
		repository: request.Repository, host: request.Host, correlationID: request.CorrelationID, programID: request.ProgramID, entryID: request.EntryID,
		flowProgramFingerprint: request.ProgramFingerprint,
		runID:                  request.FlowID, objectiveID: request.Objective.ID, targetID: string(request.Objective.TargetID), trustedObjectiveClass: string(request.Objective.TrustedObjectiveClass()), deliveryID: request.Objective.DeliveryID,
		transitionID: string(request.TransitionID), parameters: parameterFlags, maintenanceParameterSurface: maintenanceParameterSurface && request.TransitionID != "" && !repositoryTransition,
	})
	if err != nil {
		return surfaces.Request{}, err
	}
	parameters, err := parseParameters(bound.parameters)
	if err != nil {
		return surfaces.Request{}, err
	}
	request.Repository = bound.repository
	request.ProgramFingerprint = bound.flowProgramFingerprint
	request.FlowID = bound.runID
	request.Objective.ID = bound.objectiveID
	request.Objective.TargetID = model.TargetID(bound.targetID)
	request.Objective.TrustedClass = model.TargetID(bound.trustedObjectiveClass)
	request.Objective.DeliveryID = bound.deliveryID
	request.Parameters = parameters
	request.DelegationBindingFingerprint = bound.delegationBindingFingerprint
	request.DelegationRequestFingerprint = bound.delegationRequestFingerprint
	request.DelegatedAuthorities = delegationClasses(bound.delegationAuthorities)
	request.WorkInputs = bound.workInputs
	request.ControlBundle = bound.controlBundle
	request.ControlBundleFingerprint = bound.controlBundleFingerprint
	request.InvocationEvidence = bound.invocationEvidence
	request.InputRequest = bound.inputRequest
	return request, nil
}

func repositoryFlowDeclaresTransition(repository, programID, transitionID string) (bool, error) {
	if programID == "" || transitionID == "" {
		return false, nil
	}
	repository, err := filepath.Abs(repository)
	if err != nil {
		return false, err
	}
	repository, err = filepath.EvalSymlinks(repository)
	if err != nil {
		return false, err
	}
	raw, err := os.ReadFile(filepath.Join(repository, ".boatstack", "flows", programID+".flow.ir.json"))
	if err != nil {
		return false, err
	}
	artifact, err := controlprogram.LoadArtifact(bytes.NewReader(raw))
	if err != nil {
		return false, err
	}
	_, found := findCompiledTransition(artifact.Program.Transitions, transitionID)
	return found, nil
}

// bindPrescribedRepositoryInvocation closes the selector-to-invocation gap.
// A repository transition may be selected before its exact transition-specific
// producers are known. That first prescription is candidate evidence only: it
// must be rebound and re-resolved before it can be returned or applied.
func bindPrescribedRepositoryInvocation(ctx context.Context, request surfaces.Request, response surfaces.Response) (surfaces.Request, bool, error) {
	if request.Operation != surfaces.OperationResolve || request.ProgramID == "" || request.InvocationEvidence != nil || response.Prescription == nil {
		return request, false, nil
	}
	transitionID := string(response.Prescription.TransitionID)
	repositoryTransition, err := repositoryFlowDeclaresTransition(request.Repository, request.ProgramID, transitionID)
	if err != nil {
		return surfaces.Request{}, false, err
	}
	if !repositoryTransition {
		return request, false, nil
	}
	if response.Prescription.InvocationFingerprint != "" {
		return surfaces.Request{}, false, fmt.Errorf("FLOW_INVOCATION_INVALID: unmaterialized repository prescription carries invocation identity")
	}
	rebound := request
	rebound.TransitionID = response.Prescription.TransitionID
	rebound.Parameters = nil
	rebound.Prescription = protocol.Prescription{}
	rebound.IdempotencyKey = ""
	rebound.InvocationEvidence = nil
	rebound.InputRequest = nil
	rebound, err = bindRPCFlowEntry(ctx, rebound)
	if err != nil {
		return surfaces.Request{}, false, err
	}
	if rebound.InputRequest == nil && rebound.InvocationEvidence == nil {
		return surfaces.Request{}, false, fmt.Errorf("FLOW_INVOCATION_INCOMPLETE: selected repository transition produced neither an input request nor invocation evidence")
	}
	return rebound, true, nil
}

func resolveBoundPlan(repository string, entry controlprogram.Entry, entryObjective softwareflow.EntryObjective, options commandOptions) (string, string, error) {
	if options.activeFlowBound && entryObjective.TrustedClass == model.ObjectiveAbandoned {
		return "", options.deliveryID, nil
	}
	if options.deliveryID == "" {
		return resolvePlanInput(repository, entry)
	}
	if !flowSegment.MatchString(options.deliveryID) {
		return "", "", fmt.Errorf("FLOW_CONTEXT_MISMATCH: active run requires its delivery identity")
	}
	managed := filepath.Join(repository, ".boatstack", "plans", options.deliveryID+".source")
	if _, err := os.Lstat(managed); err == nil {
		resolved, resolveErr := resolveRegularRepositoryFile(repository, managed, "active run plan")
		return resolved, options.deliveryID, resolveErr
	} else if !os.IsNotExist(err) {
		return "", "", fmt.Errorf("FLOW_INPUT_INVALID: inspect active run plan: %w", err)
	}
	inbox, _, err := resolvePlanInbox(repository, entry)
	if err != nil {
		return "", "", err
	}
	resolved, err := resolveActiveInboxPlan(repository, inbox, options.deliveryID)
	return resolved, options.deliveryID, err
}

func resolveActiveInboxPlan(repository, inbox, deliveryID string) (string, error) {
	entries, err := os.ReadDir(inbox)
	if err != nil {
		return "", fmt.Errorf("FLOW_INPUT_REQUIRED: active run inbox is unavailable: %w", err)
	}
	var candidates []string
	for _, candidate := range entries {
		extension := filepath.Ext(candidate.Name())
		if candidate.Type()&os.ModeSymlink != 0 || candidate.IsDir() || !strings.EqualFold(extension, ".md") || strings.TrimSuffix(candidate.Name(), extension) != deliveryID {
			continue
		}
		info, infoErr := candidate.Info()
		if infoErr == nil && info.Mode().IsRegular() {
			candidates = append(candidates, candidate.Name())
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("FLOW_INPUT_REQUIRED: active run inbox plan %q is unavailable", deliveryID)
	}
	if len(candidates) != 1 {
		return "", fmt.Errorf("FLOW_INPUT_INVALID: active run inbox plan %q is ambiguous", deliveryID)
	}
	return resolveRegularRepositoryFile(repository, filepath.Join(inbox, candidates[0]), "active run inbox plan")
}

func resolveRegularRepositoryFile(repository, path, label string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("FLOW_INPUT_REQUIRED: %s is unavailable: %w", label, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("FLOW_INPUT_INVALID: %s must be a regular non-symlink file", label)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("FLOW_INPUT_INVALID: resolve %s: %w", label, err)
	}
	relative, err := filepath.Rel(repository, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("FLOW_INPUT_INVALID: %s escapes the repository", label)
	}
	return resolved, nil
}

func loadFlowDefinition(ctx context.Context, repository, programID string) (softwareflow.Definition, error) {
	if !flowSegment.MatchString(programID) {
		return softwareflow.Definition{}, fmt.Errorf("FLOW_PROGRAM_INVALID: program identity is not a semantic segment")
	}
	artifactPath := filepath.Join(repository, ".boatstack", "flows", programID+".flow.ir.json")
	raw, err := os.ReadFile(artifactPath)
	if err != nil {
		return softwareflow.Definition{}, err
	}
	artifact, err := controlprogram.LoadArtifact(bytes.NewReader(raw))
	if err != nil {
		return softwareflow.Definition{}, err
	}
	resolver, err := softwareflow.NewResolver(ctx)
	if err != nil {
		return softwareflow.Definition{}, err
	}
	compiled, err := controlprogram.CheckArtifact(repository, artifact, flowCompilerVersion, resolver, generateSoftwareFlowSkills)
	if err != nil {
		return softwareflow.Definition{}, err
	}
	return softwareflow.NewDefinition(compiled, resolver)
}

func resolvePlanInput(repository string, entry controlprogram.Entry) (string, string, error) {
	inbox, config, err := resolvePlanInbox(repository, entry)
	if err != nil {
		return "", "", err
	}
	entries, err := os.ReadDir(inbox)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", fmt.Errorf("PLAN_REQUIRED: no plan exists in %s", config.Path)
		}
		return "", "", err
	}
	var candidates []string
	for _, candidate := range entries {
		if candidate.Type()&os.ModeSymlink != 0 || candidate.IsDir() || !strings.EqualFold(filepath.Ext(candidate.Name()), ".md") {
			continue
		}
		info, infoErr := candidate.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			continue
		}
		name := strings.TrimSuffix(candidate.Name(), filepath.Ext(candidate.Name()))
		if flowSegment.MatchString(name) {
			candidates = append(candidates, candidate.Name())
		}
	}
	sort.Strings(candidates)
	if len(candidates) == 0 {
		return "", "", fmt.Errorf("PLAN_REQUIRED: no eligible Markdown plan exists in %s", config.Path)
	}
	if len(candidates) != 1 {
		return "", "", fmt.Errorf("PLAN_SELECTION_REQUIRED: found %d eligible plans in %s", len(candidates), config.Path)
	}
	deliveryID := strings.TrimSuffix(candidates[0], filepath.Ext(candidates[0]))
	selected := filepath.Join(inbox, candidates[0])
	resolved, err := resolveRegularRepositoryFile(repository, selected, "selected plan")
	if err != nil {
		return "", "", err
	}
	return resolved, deliveryID, nil
}

func resolvePlanInbox(repository string, entry controlprogram.Entry) (string, softwareflow.PlanInbox, error) {
	config, err := softwareflow.PlanInboxForEntry(entry)
	if err != nil {
		return "", softwareflow.PlanInbox{}, fmt.Errorf("FLOW_INPUT_INVALID: %w", err)
	}
	inbox, err := exactRepositoryPath(repository, filepath.FromSlash(config.Path))
	if err != nil {
		return "", softwareflow.PlanInbox{}, fmt.Errorf("FLOW_INPUT_INVALID: %w", err)
	}
	resolvedInbox, err := filepath.EvalSymlinks(inbox)
	if err != nil && !os.IsNotExist(err) {
		return "", softwareflow.PlanInbox{}, fmt.Errorf("FLOW_INPUT_INVALID: resolve plan inbox: %w", err)
	}
	if err == nil {
		relative, relativeErr := filepath.Rel(repository, resolvedInbox)
		if relativeErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return "", softwareflow.PlanInbox{}, fmt.Errorf("FLOW_INPUT_INVALID: plan inbox escapes the repository")
		}
		inbox = resolvedInbox
	}
	return inbox, config, nil
}

func findEntry(entries []controlprogram.Entry, id string) (controlprogram.Entry, bool) {
	for _, entry := range entries {
		if entry.ID == id {
			return entry, true
		}
	}
	return controlprogram.Entry{}, false
}

func generateSoftwareFlowSkills(compiled controlprogram.Compiled) (map[string][]byte, error) {
	return softwareflow.GenerateSkills(compiled, []string{"codex", "claude"})
}

func flowRepositoryIdentity(repository string) (string, error) {
	marker := filepath.Join(repository, ".git")
	info, err := os.Lstat(marker)
	if err != nil {
		return "", fmt.Errorf("FLOW_REPOSITORY_IDENTITY_REQUIRED: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("FLOW_REPOSITORY_IDENTITY_INVALID: .git is a symlink")
	}
	common := marker
	if !info.IsDir() {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("FLOW_REPOSITORY_IDENTITY_INVALID: .git is not a directory or worktree marker")
		}
		raw, readErr := os.ReadFile(marker)
		if readErr != nil {
			return "", readErr
		}
		line := strings.TrimSpace(string(raw))
		if !strings.HasPrefix(line, "gitdir: ") || strings.Contains(strings.TrimPrefix(line, "gitdir: "), "\n") {
			return "", fmt.Errorf("FLOW_REPOSITORY_IDENTITY_INVALID: invalid worktree Git marker")
		}
		gitDirectory := strings.TrimPrefix(line, "gitdir: ")
		if !filepath.IsAbs(gitDirectory) {
			gitDirectory = filepath.Join(repository, gitDirectory)
		}
		common = gitDirectory
		if rawCommon, commonErr := os.ReadFile(filepath.Join(gitDirectory, "commondir")); commonErr == nil {
			common = strings.TrimSpace(string(rawCommon))
			if !filepath.IsAbs(common) {
				common = filepath.Join(gitDirectory, common)
			}
		} else if !os.IsNotExist(commonErr) {
			return "", commonErr
		}
	}
	common, err = filepath.EvalSymlinks(filepath.Clean(common))
	if err != nil {
		return "", fmt.Errorf("FLOW_REPOSITORY_IDENTITY_INVALID: %w", err)
	}
	if info, err := os.Stat(common); err != nil || !info.IsDir() {
		return "", fmt.Errorf("FLOW_REPOSITORY_IDENTITY_INVALID: Git common directory is unavailable")
	}
	return common, nil
}

func flowRunID(repositoryIdentity, fingerprint, entry, delivery, planFingerprint string) string {
	value := strings.Join([]string{repositoryIdentity, fingerprint, entry, delivery, planFingerprint}, "\x00")
	digest := sha256.Sum256([]byte(value))
	return "run-" + hex.EncodeToString(digest[:16])
}
