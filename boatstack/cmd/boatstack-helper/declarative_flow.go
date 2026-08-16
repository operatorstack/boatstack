package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	softwareflow "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery"
	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/effects"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/plant"
	"github.com/operatorstack/boatstack/boatstack/invocation"
)

const declarativeRunSchemaRevision = 2

type declarativeTransitionReceipt struct {
	ID                    string                         `json:"id"`
	TransitionID          string                         `json:"transition_id"`
	InvocationFingerprint string                         `json:"invocation_fingerprint"`
	PriorStateRevision    uint64                         `json:"prior_state_revision"`
	ResultStateRevision   uint64                         `json:"result_state_revision"`
	Parameters            []invocation.ResolvedParameter `json:"parameters"`
	HumanActor            string                         `json:"human_actor,omitempty"`
	Fingerprint           string                         `json:"fingerprint"`
}

type declarativeRunState struct {
	SchemaRevision     int                           `json:"schema_revision"`
	RunID              string                        `json:"run_id"`
	ProgramFingerprint string                        `json:"program_fingerprint"`
	EntryID            string                        `json:"entry_id"`
	TargetID           string                        `json:"target_id"`
	StateRevision      uint64                        `json:"state_revision"`
	EntryInputs        map[string]string             `json:"entry_inputs"`
	Facts              map[string]string             `json:"facts"`
	LastTransition     string                        `json:"last_transition,omitempty"`
	LastInvocation     string                        `json:"last_invocation,omitempty"`
	LastReceipt        *declarativeTransitionReceipt `json:"last_receipt,omitempty"`
	Fingerprint        string                        `json:"fingerprint"`
}

type declarativeRuntimeContext struct {
	compiled                  controlprogram.Compiled
	entry                     controlprogram.Entry
	state                     declarativeRunState
	statePath                 string
	store                     invocation.Store
	executionScopeFingerprint string
}

func tryRunDeclarativeFlow(ctx context.Context, options commandOptions) (bool, error) {
	compiled, err := loadCurrentFlowArtifact(ctx, options.repository, options.programID)
	if err != nil {
		return false, err
	}
	declarative, err := declarativeFlow(compiled.Document)
	if err != nil || !declarative {
		return false, err
	}
	if err := validateDeclarativeFlow(compiled); err != nil {
		return true, err
	}
	return true, runDeclarativeFlow(ctx, compiled, options)
}

func loadCurrentFlowArtifact(ctx context.Context, repository, programID string) (controlprogram.Compiled, error) {
	repository, err := filepath.Abs(repository)
	if err != nil {
		return controlprogram.Compiled{}, err
	}
	repository, err = filepath.EvalSymlinks(repository)
	if err != nil {
		return controlprogram.Compiled{}, err
	}
	raw, err := os.ReadFile(filepath.Join(repository, ".boatstack", "flows", programID+".flow.ir.json"))
	if err != nil {
		return controlprogram.Compiled{}, fmt.Errorf("FLOW_ARTIFACT_REQUIRED: %w", err)
	}
	artifact, err := controlprogram.LoadArtifact(bytes.NewReader(raw))
	if err != nil {
		return controlprogram.Compiled{}, err
	}
	resolver, err := softwareflow.NewResolver(ctx)
	if err != nil {
		return controlprogram.Compiled{}, err
	}
	return controlprogram.CheckArtifact(repository, artifact, flowCompilerVersion, resolver, generateSoftwareFlowSkills)
}

func runDeclarativeFlow(ctx context.Context, compiled controlprogram.Compiled, options commandOptions) error {
	if len(options.parameters) != 0 {
		return fmt.Errorf("FLOW_PARAMETER_BYPASS: repository Flow parameters must come from compiled producer declarations")
	}
	entry, ok := findEntry(compiled.Document.Entries, options.entryID)
	if !ok {
		return fmt.Errorf("FLOW_ENTRY_UNKNOWN: %s", options.entryID)
	}
	repository, err := filepath.Abs(options.repository)
	if err != nil {
		return err
	}
	repository, err = filepath.EvalSymlinks(repository)
	if err != nil {
		return err
	}
	lease, err := boatstackruntime.AcquireFlowProjectionLease(repository)
	if err != nil {
		return err
	}
	defer lease.Release()
	runtimeContext, err := loadDeclarativeRuntimeContext(ctx, repository, compiled, entry, options)
	if err != nil {
		return err
	}
	if predicateSatisfied(targetPredicate(compiled.Document.Targets, entry.Target), runtimeContext.state.Facts) {
		return encodeDeclarativeResult(map[string]any{
			"kind": "terminal", "run_id": runtimeContext.state.RunID, "program_fingerprint": compiled.Fingerprint,
			"entry_id": entry.ID, "target_id": entry.Target, "state_revision": runtimeContext.state.StateRevision,
			"receipt": runtimeContext.state.LastReceipt,
		}, options.format)
	}
	transition, operator, ok := selectDeclarativeTransition(compiled.Document, runtimeContext.state.Facts)
	if !ok {
		return encodeDeclarativeResult(map[string]any{
			"kind": "blocked", "code": "FLOW_NO_ADMISSIBLE_TRANSITION", "run_id": runtimeContext.state.RunID,
			"program_fingerprint": compiled.Fingerprint, "entry_id": entry.ID, "target_id": entry.Target,
		}, options.format)
	}
	result, materializationContext, err := materializeDeclarativeInvocation(runtimeContext, transition, operator)
	if err != nil {
		return err
	}
	if result.Blocker != nil {
		return encodeDeclarativeResult(map[string]any{
			"kind": "blocked", "code": result.Blocker.Code, "detail": result.Blocker.Detail,
			"run_id": runtimeContext.state.RunID, "program_fingerprint": compiled.Fingerprint,
		}, options.format)
	}
	if result.Request != nil {
		if err := runtimeContext.store.SaveRequest(*result.Request); err != nil {
			return err
		}
		return encodeDeclarativeResult(map[string]any{
			"kind": "suspended", "code": result.Request.Code, "run_id": runtimeContext.state.RunID,
			"program_fingerprint": compiled.Fingerprint, "entry_id": entry.ID, "target_id": entry.Target,
			"transition_id": transition.ID, "request": result.Request,
		}, options.format)
	}
	if result.Ready == nil {
		return fmt.Errorf("FLOW_INVOCATION_INCOMPLETE: declarative materialization produced no evidence")
	}
	if err := requireDeclarativeAuthority(transition, operator, options.humanActor); err != nil {
		return encodeDeclarativeResult(map[string]any{
			"kind": "blocked", "code": "AUTHORITY_REQUIRED", "detail": err.Error(),
			"run_id": runtimeContext.state.RunID, "program_fingerprint": compiled.Fingerprint,
			"entry_id": entry.ID, "target_id": entry.Target, "transition_id": transition.ID,
		}, options.format)
	}

	// Re-read receipts and rematerialize while the run lock is held. The first
	// result is a candidate; only this current evidence may cross the state
	// mutation boundary.
	materializationContext.InputReceipts, err = runtimeContext.store.LoadReceipts(runtimeContext.state.RunID, transition.ID)
	if err != nil {
		return err
	}
	fresh, err := invocation.Materialize(operator.Parameters, transition.Parameters, materializationContext, nil)
	if err != nil {
		return err
	}
	if fresh.Ready == nil || fresh.Ready.InvocationFingerprint != result.Ready.InvocationFingerprint {
		return fmt.Errorf("INVOCATION_DRIFT: invocation changed before declarative state effect")
	}
	priorRevision := runtimeContext.state.StateRevision
	candidate := runtimeContext.state
	if err := applyDeclarativeAssignments(&candidate, *operator.StateEffect, fresh.Ready.Parameters); err != nil {
		return err
	}
	if !predicateSatisfied(transition.Target, candidate.Facts) {
		return fmt.Errorf("DECLARATIVE_VERIFICATION_FAILED: transition %q did not establish its compiled target", transition.ID)
	}
	candidate.StateRevision++
	candidate.LastTransition = transition.ID
	candidate.LastInvocation = fresh.Ready.InvocationFingerprint
	receipt := declarativeTransitionReceipt{
		TransitionID: transition.ID, InvocationFingerprint: fresh.Ready.InvocationFingerprint,
		PriorStateRevision: priorRevision, ResultStateRevision: candidate.StateRevision,
		Parameters: append([]invocation.ResolvedParameter(nil), fresh.Ready.Parameters...), HumanActor: strings.TrimSpace(options.humanActor),
	}
	receipt.ID = "receipt-" + digestDeclarative(receipt)[:24]
	receipt.Fingerprint = digestDeclarative(receipt)
	candidate.LastReceipt = &receipt
	if err := saveDeclarativeRun(runtimeContext.statePath, candidate); err != nil {
		return err
	}
	runtimeContext.state = candidate
	terminal := predicateSatisfied(targetPredicate(compiled.Document.Targets, entry.Target), runtimeContext.state.Facts)
	kind := "continued"
	if terminal {
		kind = "terminal"
	}
	return encodeDeclarativeResult(map[string]any{
		"kind": kind, "run_id": runtimeContext.state.RunID, "program_fingerprint": compiled.Fingerprint,
		"entry_id": entry.ID, "target_id": entry.Target, "transition_id": transition.ID,
		"state_revision": runtimeContext.state.StateRevision, "invocation": fresh.Ready,
		"receipt": runtimeContext.state.LastReceipt,
	}, options.format)
}

func requireDeclarativeAuthority(transition controlprogram.Transition, operator controlprogram.Operator, humanActor string) error {
	providedHuman := strings.TrimSpace(humanActor) != ""
	if len(operator.Authority.AnyOf) != 0 && !providedHuman {
		return fmt.Errorf("operator %q requires one of %s", operator.ID, strings.Join(operator.Authority.AnyOf, ", "))
	}
	if containsString(operator.Authority.AllOf, "human") && !providedHuman {
		return fmt.Errorf("operator %q requires human authority", operator.ID)
	}
	if containsString(transition.Requires.Authorities, "human") && !providedHuman {
		return fmt.Errorf("transition %q requires human authority", transition.ID)
	}
	return nil
}

func loadDeclarativeRuntimeContext(ctx context.Context, repository string, compiled controlprogram.Compiled, entry controlprogram.Entry, options commandOptions) (declarativeRuntimeContext, error) {
	resolver, err := plant.NewResolver("")
	if err != nil {
		return declarativeRuntimeContext{}, err
	}
	host := options.host
	if host == "" {
		host = "cli"
	}
	invoking, err := resolver.ResolveInvocation(ctx, repository, host, "declarative-flow")
	if err != nil {
		return declarativeRuntimeContext{}, err
	}
	layout, invoking, err := resolver.ResolveLayout(ctx, invoking)
	if err != nil {
		return declarativeRuntimeContext{}, err
	}
	scope, err := flowExecutionScopeFingerprint(invoking)
	if err != nil {
		return declarativeRuntimeContext{}, err
	}
	provided, err := parseNamedValues(options.entryInputs)
	if err != nil {
		return declarativeRuntimeContext{}, fmt.Errorf("FLOW_INPUT_INVALID: %w", err)
	}
	runID := options.runID
	if runID == "" {
		if err := validateDeclarativeEntryInputs(entry, provided, true); err != nil {
			return declarativeRuntimeContext{}, err
		}
		runID = declarativeRunID(scope, compiled.Fingerprint, entry.ID, provided)
	} else if !flowSegment.MatchString(runID) {
		return declarativeRuntimeContext{}, fmt.Errorf("FLOW_CONTEXT_MISMATCH: run identity is invalid")
	}
	statePath := filepath.Join(layout.FlowRoot, "declarative", compiled.Document.Program.ID, entry.ID, runID+".json")
	state, err := loadDeclarativeRun(statePath)
	if os.IsNotExist(err) {
		if len(provided) == 0 {
			return declarativeRuntimeContext{}, fmt.Errorf("FLOW_INPUT_REQUIRED: a new declarative run requires its entry inputs")
		}
		state = declarativeRunState{SchemaRevision: declarativeRunSchemaRevision, RunID: runID, ProgramFingerprint: compiled.Fingerprint, EntryID: entry.ID, TargetID: entry.Target, StateRevision: 1, EntryInputs: provided, Facts: map[string]string{}}
		if err := saveDeclarativeRun(statePath, state); err != nil {
			return declarativeRuntimeContext{}, err
		}
	} else if err != nil {
		return declarativeRuntimeContext{}, err
	}
	if state.SchemaRevision != declarativeRunSchemaRevision || state.RunID != runID || state.ProgramFingerprint != compiled.Fingerprint || state.EntryID != entry.ID || state.TargetID != entry.Target || state.StateRevision == 0 || state.EntryInputs == nil || state.Facts == nil {
		return declarativeRuntimeContext{}, fmt.Errorf("FLOW_CONTEXT_MISMATCH: declarative run identity or schema changed")
	}
	if state.LastReceipt != nil {
		receipt := *state.LastReceipt
		fingerprint := receipt.Fingerprint
		receipt.Fingerprint = ""
		if fingerprint == "" || fingerprint != digestDeclarative(receipt) || state.LastTransition != state.LastReceipt.TransitionID || state.LastInvocation != state.LastReceipt.InvocationFingerprint || state.StateRevision != state.LastReceipt.ResultStateRevision {
			return declarativeRuntimeContext{}, fmt.Errorf("FLOW_CONTEXT_MISMATCH: declarative transition receipt is invalid")
		}
	}
	if len(provided) != 0 && !equalStringMaps(provided, state.EntryInputs) {
		return declarativeRuntimeContext{}, fmt.Errorf("FLOW_CONTEXT_MISMATCH: entry inputs changed across the run")
	}
	if err := validateDeclarativeEntryInputs(entry, state.EntryInputs, false); err != nil {
		return declarativeRuntimeContext{}, err
	}
	return declarativeRuntimeContext{
		compiled: compiled, entry: entry, state: state, statePath: statePath,
		store: invocation.Store{Root: layout.FlowRoot, Writer: effects.NewRuntimeStore()}, executionScopeFingerprint: scope,
	}, nil
}

func materializeDeclarativeInvocation(runtimeContext declarativeRuntimeContext, transition controlprogram.Transition, operator controlprogram.Operator) (invocation.Result, invocation.Context, error) {
	entryInputs := map[string]invocation.Value{}
	for _, input := range runtimeContext.entry.Inputs {
		value, ok := runtimeContext.state.EntryInputs[input.ID]
		if ok {
			entryInputs[input.ID] = invocation.Value{Type: controlprogram.ValueTypeDefinition{Kind: "string"}, Canonical: value, Provenance: "entry-input", ProducerFingerprint: digestDeclarative(input)}
		}
	}
	stateValues := map[string]invocation.Value{}
	for facet, value := range runtimeContext.state.Facts {
		stateValues[facet] = invocation.Value{Type: controlprogram.ValueTypeDefinition{Kind: "string"}, Canonical: value, Provenance: "state", ProducerFingerprint: digestDeclarative(map[string]string{"facet": facet, "value": value})}
	}
	contextFingerprint := digestDeclarative(struct {
		RunID         string            `json:"run_id"`
		Program       string            `json:"program"`
		Entry         string            `json:"entry"`
		Target        string            `json:"target"`
		Transition    string            `json:"transition"`
		StateRevision uint64            `json:"state_revision"`
		Inputs        map[string]string `json:"inputs"`
		Facts         map[string]string `json:"facts"`
	}{runtimeContext.state.RunID, runtimeContext.compiled.Fingerprint, runtimeContext.entry.ID, runtimeContext.entry.Target, transition.ID, runtimeContext.state.StateRevision, runtimeContext.state.EntryInputs, runtimeContext.state.Facts})
	receipts, err := runtimeContext.store.LoadReceipts(runtimeContext.state.RunID, transition.ID)
	if err != nil {
		return invocation.Result{}, invocation.Context{}, err
	}
	materializationContext := invocation.Context{
		RunID: runtimeContext.state.RunID, ProgramFingerprint: runtimeContext.compiled.Fingerprint,
		ExecutionProgramFingerprint: runtimeContext.compiled.Fingerprint,
		EntryID:                     runtimeContext.entry.ID, TargetID: runtimeContext.entry.Target, TransitionID: transition.ID,
		StateRevision: runtimeContext.state.StateRevision, ContextFingerprint: contextFingerprint,
		ExecutionScopeFingerprint: runtimeContext.executionScopeFingerprint, EntryInputs: entryInputs,
		State: stateValues, Receipts: map[string]invocation.Value{}, WorkOutputs: map[string]invocation.Value{}, InputReceipts: receipts,
	}
	result, err := invocation.Materialize(operator.Parameters, transition.Parameters, materializationContext, nil)
	return result, materializationContext, err
}

func selectDeclarativeTransition(document controlprogram.Document, facts map[string]string) (controlprogram.Transition, controlprogram.Operator, bool) {
	operators := map[string]controlprogram.Operator{}
	for _, operator := range document.Operators {
		operators[operator.ID] = operator
	}
	transitions := append([]controlprogram.Transition(nil), document.Transitions...)
	sort.Slice(transitions, func(i, j int) bool {
		if transitions[i].Priority != transitions[j].Priority {
			return transitions[i].Priority < transitions[j].Priority
		}
		return transitions[i].ID < transitions[j].ID
	})
	for _, transition := range transitions {
		if predicateSatisfied(transition.Guard, facts) && !predicateSatisfied(transition.Target, facts) {
			return transition, operators[transition.Operator], true
		}
	}
	return controlprogram.Transition{}, controlprogram.Operator{}, false
}

func predicateSatisfied(predicate controlprogram.Predicate, facts map[string]string) bool {
	if predicate.True != nil {
		return *predicate.True
	}
	if predicate.Fact != nil {
		value, known := facts[predicate.Fact.Facet]
		status := "absent"
		if known {
			status = "known"
		}
		if len(predicate.Fact.Statuses) != 0 && !containsString(predicate.Fact.Statuses, status) {
			return false
		}
		return len(predicate.Fact.Values) == 0 || (known && containsString(predicate.Fact.Values, value))
	}
	if len(predicate.All) != 0 {
		for _, child := range predicate.All {
			if !predicateSatisfied(child, facts) {
				return false
			}
		}
		return true
	}
	if len(predicate.Any) != 0 {
		for _, child := range predicate.Any {
			if predicateSatisfied(child, facts) {
				return true
			}
		}
		return false
	}
	return predicate.Not != nil && !predicateSatisfied(*predicate.Not, facts)
}

func targetPredicate(targets []controlprogram.Target, targetID string) controlprogram.Predicate {
	for _, target := range targets {
		if target.ID == targetID {
			return target.Predicate
		}
	}
	return controlprogram.Predicate{}
}

func applyDeclarativeAssignments(state *declarativeRunState, effect controlprogram.StateEffect, parameters []invocation.ResolvedParameter) error {
	values := map[string]string{}
	for _, parameter := range parameters {
		values[parameter.Name] = parameter.Value
	}
	for _, precondition := range effect.Preconditions {
		if !containsString(precondition.Values, state.Facts[precondition.Facet]) {
			return fmt.Errorf("INVOCATION_DRIFT: state precondition %q changed before effect", precondition.Facet)
		}
	}
	for _, assignment := range effect.Assignments {
		if assignment.Value != nil {
			state.Facts[assignment.Facet] = *assignment.Value
			continue
		}
		if assignment.ValueFrom == nil || assignment.ValueFrom.Parameter == "" {
			return fmt.Errorf("FLOW_RUNTIME_INVALID: declarative assignment %q requires a literal or invocation parameter", assignment.Facet)
		}
		value, ok := values[assignment.ValueFrom.Parameter]
		if !ok {
			return fmt.Errorf("FLOW_INVOCATION_INCOMPLETE: assignment parameter %q is absent", assignment.ValueFrom.Parameter)
		}
		state.Facts[assignment.Facet] = value
	}
	return nil
}

func validateDeclarativeEntryInputs(entry controlprogram.Entry, values map[string]string, creating bool) error {
	declared := map[string]controlprogram.EntryInput{}
	for _, input := range entry.Inputs {
		declared[input.ID] = input
		if input.Required && strings.TrimSpace(values[input.ID]) == "" {
			return fmt.Errorf("FLOW_INPUT_REQUIRED: entry %q requires input %q", entry.ID, input.ID)
		}
	}
	for id, value := range values {
		if _, ok := declared[id]; !ok || strings.TrimSpace(value) == "" {
			return fmt.Errorf("FLOW_INPUT_INVALID: entry %q does not declare non-empty input %q", entry.ID, id)
		}
	}
	_ = creating
	return nil
}

func parseNamedValues(values []string) (map[string]string, error) {
	result := map[string]string{}
	for _, item := range values {
		name, value, ok := strings.Cut(item, "=")
		if !ok || name == "" || value == "" || result[name] != "" {
			return nil, fmt.Errorf("entry inputs require unique name=value pairs")
		}
		result[name] = value
	}
	return result, nil
}

func declarativeRunID(scope, program, entry string, inputs map[string]string) string {
	return "run-" + digestDeclarative(struct {
		Scope   string            `json:"scope"`
		Program string            `json:"program"`
		Entry   string            `json:"entry"`
		Inputs  map[string]string `json:"inputs"`
	}{scope, program, entry, inputs})[:32]
}

func loadDeclarativeRun(path string) (declarativeRunState, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return declarativeRunState{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var state declarativeRunState
	if err := decoder.Decode(&state); err != nil {
		return declarativeRunState{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return declarativeRunState{}, fmt.Errorf("declarative run contains trailing JSON")
	}
	identity := state
	identity.Fingerprint = ""
	if state.Fingerprint == "" || state.Fingerprint != digestDeclarative(identity) {
		return declarativeRunState{}, fmt.Errorf("declarative run failed content identity verification")
	}
	return state, nil
}

func saveDeclarativeRun(path string, state declarativeRunState) error {
	state.Fingerprint = ""
	state.Fingerprint = digestDeclarative(state)
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return effects.NewRuntimeStore().WriteAtomic(path, append(raw, '\n'), 0o600)
}

func encodeDeclarativeResult(value any, format string) error {
	if format != "json" {
		return fmt.Errorf("declarative Flow driver requires --format json")
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func digestDeclarative(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func equalStringMaps(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
