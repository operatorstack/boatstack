// Package softwaredelivery binds the domain-neutral Control Program IR to
// Boatstack's trusted software-delivery operators.
package softwaredelivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	"github.com/operatorstack/boatstack/boatstack/delivery"
	"github.com/operatorstack/boatstack/boatstack/flow/standard"
)

const BindingPrefix = "software-delivery/"
const DelegationPrefix = BindingPrefix + "delegation/"
const ParameterResolverPrefix = BindingPrefix

type Resolver struct {
	transitions map[string]delivery.Transition
}

func NewResolver(ctx context.Context) (Resolver, error) {
	manifest, err := standard.Definition().RuntimeManifest(ctx)
	if err != nil {
		return Resolver{}, err
	}
	transitions := make(map[string]delivery.Transition, len(manifest.Transitions))
	for _, transition := range manifest.Transitions {
		transitions[string(transition.ID)] = transition
	}
	acceptedWork, err := acceptedWorkTransitions(transitions)
	if err != nil {
		return Resolver{}, err
	}
	for _, transition := range acceptedWork {
		transitions[string(transition.ID)] = transition
	}
	return Resolver{transitions: transitions}, nil
}

func (r Resolver) ResolveOperator(reference, version string) (controlprogram.ResolvedOperator, error) {
	id, ok := strings.CutPrefix(reference, BindingPrefix)
	if !ok || id == "" {
		return controlprogram.ResolvedOperator{}, fmt.Errorf("unknown trusted binding %q", reference)
	}
	transition, ok := r.transitions[id]
	if !ok {
		return controlprogram.ResolvedOperator{}, fmt.Errorf("unknown software-delivery operator %q", id)
	}
	if version != strconv.Itoa(transition.Version) {
		return controlprogram.ResolvedOperator{}, fmt.Errorf("operator %q requires binding version %d", id, transition.Version)
	}
	transition.ExecutionContext = executionContextFor(transition)
	outputs := projectOperatorOutputs(transition)
	stateInputs := projectOperatorStateInputs(transition)
	receiptInputs := projectOperatorReceiptInputs(transition)
	fingerprint, err := transitionFingerprint(transition, outputs, stateInputs, receiptInputs)
	if err != nil {
		return controlprogram.ResolvedOperator{}, err
	}
	capabilities := make([]string, len(transition.RequiredCapabilities))
	for index, value := range transition.RequiredCapabilities {
		capabilities[index] = string(value)
	}
	anyOf := authorityStrings(transition.Authority)
	allOf := authorityStrings(transition.AuthorityAll)
	effectSet := map[string]bool{}
	if transition.Effect != "" {
		effectSet[string(transition.Effect)] = true
	}
	for _, values := range [][]delivery.EffectID{transition.LocalEffects, transition.ExternalEffects} {
		for _, value := range values {
			effectSet[string(value)] = true
		}
	}
	effects := make([]string, 0, len(effectSet))
	for value := range effectSet {
		effects = append(effects, value)
	}
	sort.Strings(effects)
	return controlprogram.ResolvedOperator{
		Fingerprint: fingerprint, Capabilities: capabilities,
		Authority: controlprogram.AuthorityRequirement{AnyOf: anyOf, AllOf: allOf}, Effects: effects,
		Verifier: transition.Verifier, Recovery: string(transition.Interruption.Recovery), StateEffect: projectStateEffect(transition.StateEffect),
		ExecutionContext: executionContextFor(transition),
		Parameters:       projectOperatorParameters(transition),
		Outputs:          outputs,
		StateInputs:      stateInputs,
		ReceiptInputs:    receiptInputs,
	}, nil
}

func (r Resolver) ResolveParameterResolver(reference, version string) (controlprogram.ResolvedParameterResolver, error) {
	if version != "1" {
		return controlprogram.ResolvedParameterResolver{}, fmt.Errorf("parameter resolver %q requires binding version 1", reference)
	}
	value := controlprogram.ResolvedParameterResolver{
		OutputType:     controlprogram.ValueTypeDefinition{Kind: "string"},
		SourceKind:     controlprogram.ParameterSourceTrustedResolver,
		StabilityScope: "invocation",
	}
	switch {
	case reference == ParameterResolverPrefix+"admitted-work-package-fingerprint":
		value.Dependencies = []string{"work-package-fingerprint", "durable-state"}
	case strings.HasPrefix(reference, planningPackagePlanOutputResolverPrefix):
		if value := strings.TrimPrefix(reference, planningPackagePlanOutputResolverPrefix); !workPackageSegment.MatchString(value) {
			return controlprogram.ResolvedParameterResolver{}, fmt.Errorf("unknown software-delivery parameter resolver %q", reference)
		}
		value.Dependencies = []string{"compiled-planning-package-binding"}
	case reference == ParameterResolverPrefix+"repository-default-branch":
		value.Dependencies = []string{"repository", "verified-configuration"}
	case reference == ParameterResolverPrefix+"delivery-branch":
		value.Dependencies = []string{"repository", "delivery_id", "repository-policy"}
	case reference == ParameterResolverPrefix+"managed-worktree-destination":
		value.Dependencies = []string{"repository", "git-common", "run_id", "delivery_id", "source-worktree"}
	case reference == ParameterResolverPrefix+"current-source-revision":
		value.Dependencies = []string{"repository", "committed-head"}
	case strings.HasPrefix(reference, ParameterResolverPrefix+"gate-evidence-path/"):
		if gateEvidenceInputPath(strings.TrimPrefix(reference, ParameterResolverPrefix+"gate-evidence-path/")) == "" {
			return controlprogram.ResolvedParameterResolver{}, fmt.Errorf("unknown software-delivery parameter resolver %q", reference)
		}
		value.Dependencies = []string{"repository", "delivery_id", "gate-evidence"}
	case strings.HasPrefix(reference, ParameterResolverPrefix+"gate-evidence-fingerprint/"):
		if gateEvidenceInputPath(strings.TrimPrefix(reference, ParameterResolverPrefix+"gate-evidence-fingerprint/")) == "" {
			return controlprogram.ResolvedParameterResolver{}, fmt.Errorf("unknown software-delivery parameter resolver %q", reference)
		}
		value.Dependencies = []string{"repository", "delivery_id", "gate-evidence"}
	case reference == ParameterResolverPrefix+"visual-evidence-manifest-path":
		value.Dependencies = []string{"repository", "delivery_id", "visual-evidence"}
	case reference == ParameterResolverPrefix+"visual-evidence-privacy-receipt":
		value.Dependencies = []string{"repository", "delivery_id", "visual-evidence"}
	case reference == ParameterResolverPrefix+"publication-body-path":
		value.Dependencies = []string{"repository", "delivery_id", "publication-body"}
	case reference == ParameterResolverPrefix+"publication-body-sha256":
		value.Dependencies = []string{"repository", "delivery_id", "publication-body"}
	default:
		return controlprogram.ResolvedParameterResolver{}, fmt.Errorf("unknown software-delivery parameter resolver %q", reference)
	}
	payload := struct {
		Reference string                                   `json:"reference"`
		Version   string                                   `json:"version"`
		Metadata  controlprogram.ResolvedParameterResolver `json:"metadata"`
	}{Reference: reference, Version: version, Metadata: value}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return controlprogram.ResolvedParameterResolver{}, err
	}
	digest := sha256.Sum256(encoded)
	value.Fingerprint = hex.EncodeToString(digest[:])
	return value, nil
}

func (r Resolver) ResolveValueValidator(reference, version string) (controlprogram.ResolvedValueValidator, error) {
	return controlprogram.ResolvedValueValidator{}, fmt.Errorf("unknown software-delivery value validator %q at version %q", reference, version)
}

func (r Resolver) ResolveDelegation(reference, version string) (controlprogram.ResolvedDelegation, error) {
	authority, ok := strings.CutPrefix(reference, DelegationPrefix)
	if !ok || authority != string(delivery.AuthorityAutonomy) || version != "1" {
		return controlprogram.ResolvedDelegation{}, fmt.Errorf("unknown or nondelegable software-delivery delegation %q", reference)
	}
	payload := struct {
		Reference   string   `json:"reference"`
		Version     string   `json:"version"`
		Authorities []string `json:"authorities"`
	}{Reference: reference, Version: version, Authorities: []string{authority}}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return controlprogram.ResolvedDelegation{}, err
	}
	digest := sha256.Sum256(encoded)
	return controlprogram.ResolvedDelegation{Fingerprint: hex.EncodeToString(digest[:]), Authorities: payload.Authorities, Delegable: true}, nil
}

func authorityStrings(values []delivery.AuthorityClass) []string {
	result := make([]string, len(values))
	result = result[:0]
	for _, value := range values {
		if value != delivery.AuthorityNone {
			result = append(result, string(value))
		}
	}
	sort.Strings(result)
	return result
}

func executionContextFor(transition delivery.Transition) string {
	if transition.ExecutionContext == "advance" {
		return "advance"
	}
	return "preserve"
}

func (r Resolver) Transition(reference string) (delivery.Transition, bool) {
	id, ok := strings.CutPrefix(reference, BindingPrefix)
	if !ok {
		return delivery.Transition{}, false
	}
	transition, ok := r.transitions[id]
	transition.TargetIDs = append([]delivery.TargetID(nil), transition.TargetIDs...)
	transition.SourceConditions = append([]delivery.FacetCondition(nil), transition.SourceConditions...)
	transition.TargetConditions = append([]delivery.FacetCondition(nil), transition.TargetConditions...)
	transition.RequiredCapabilities = append([]delivery.Capability(nil), transition.RequiredCapabilities...)
	transition.Authority = append([]delivery.AuthorityClass(nil), transition.Authority...)
	transition.AuthorityAll = append([]delivery.AuthorityClass(nil), transition.AuthorityAll...)
	return transition, ok
}

func projectStateEffect(value delivery.StateEffect) controlprogram.StateEffect {
	result := controlprogram.StateEffect{Kind: string(value.Kind), NativeHandler: value.NativeHandler}
	for _, precondition := range value.Preconditions {
		result.Preconditions = append(result.Preconditions, controlprogram.StatePrecondition{Facet: precondition.Facet, Values: append([]string(nil), precondition.Values...)})
	}
	for _, assignment := range value.Assignments {
		projected := controlprogram.StateAssignment{Facet: assignment.Facet, Value: assignment.Value}
		if assignment.ValueFrom.Parameter != "" || assignment.ValueFrom.Admission != "" || assignment.ValueFrom.Invocation != "" {
			projected.ValueFrom = &controlprogram.ValueReference{Parameter: assignment.ValueFrom.Parameter, Admission: assignment.ValueFrom.Admission, Invocation: assignment.ValueFrom.Invocation}
		}
		result.Assignments = append(result.Assignments, projected)
	}
	return result
}

func projectOperatorParameters(transition delivery.Transition) []controlprogram.OperatorParameter {
	result := make([]controlprogram.OperatorParameter, 0, len(transition.Parameters))
	for _, parameter := range transition.Parameters {
		allowed := []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceHostInput}
		switch {
		case transition.ID == WorkPackageApprove && parameter.Name == "package_fingerprint":
			allowed = []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceTrustedResolver}
		case transition.ID == PlanningPackagePromote && parameter.Name == "plan_output":
			allowed = []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceTrustedResolver}
		case transition.ID == "workspace.cut":
			allowed = []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceTrustedResolver}
		case strings.HasPrefix(string(transition.ID), "gate.") && strings.HasSuffix(string(transition.ID), ".record"):
			allowed = []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceTrustedResolver}
		case transition.ID == "evidence.visual.attach":
			allowed = []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceTrustedResolver}
		case transition.ID == "delivery.slice.advance" && parameter.Name == "source_revision":
			allowed = []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceTrustedResolver}
		case transition.ID == "publication.preview" && (parameter.Name == "base_ref" || parameter.Name == "body_path"):
			allowed = []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceTrustedResolver}
		case transition.ID == "publication.preview" && parameter.Name == "head_ref":
			allowed = []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceState}
		case transition.ID == "publication.execute" && parameter.Name == "preview_fingerprint":
			allowed = []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceState}
		case transition.ID == "publication.observe" && parameter.Name == "publication_id":
			allowed = []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceState, controlprogram.ParameterSourceStateOrReceipt}
		case transition.ID == "publication.correct" && parameter.Name == "publication_id":
			allowed = []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceState}
		case transition.ID == "publication.correct" && (parameter.Name == "body_path" || parameter.Name == "body_sha256"):
			allowed = []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceTrustedResolver}
		case (transition.ID == "workspace.reconcile" || transition.ID == "publication.reconcile") && parameter.Name == "transaction_id":
			allowed = []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceState}
		case transition.ID == "publication.reconcile" && parameter.Name == "publication_id":
			allowed = []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceState}
		case parameter.Name == "branch":
			allowed = []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceState}
		}
		result = append(result, controlprogram.OperatorParameter{
			ID: parameter.Name, Type: controlprogram.ValueTypeDefinition{Kind: "string"}, Required: parameter.Required, Secret: parameter.Secret,
			AllowedSources: allowed,
		})
	}
	return result
}

func projectOperatorOutputs(transition delivery.Transition) []controlprogram.OperatorOutput {
	if transition.ID == "publication.execute" {
		return []controlprogram.OperatorOutput{{ID: "publication_id", Type: controlprogram.ValueTypeDefinition{Kind: "string"}}}
	}
	return nil
}

func projectOperatorStateInputs(transition delivery.Transition) []controlprogram.OperatorStateInput {
	known := func(parameter, facet string) controlprogram.OperatorStateInput {
		return controlprogram.OperatorStateInput{
			Parameter: parameter, Facet: facet,
			AvailableWhen: controlprogram.Predicate{Fact: &controlprogram.FactPredicate{Facet: facet, Statuses: []string{"known"}}},
		}
	}
	switch transition.ID {
	case "workspace.activate", "workspace.sync", "workspace.publish":
		return []controlprogram.OperatorStateInput{known("branch", "workspace_branch")}
	case "publication.preview":
		return []controlprogram.OperatorStateInput{known("head_ref", "workspace_branch")}
	case "publication.execute":
		return []controlprogram.OperatorStateInput{known("preview_fingerprint", "preview_fingerprint")}
	case "publication.observe", "publication.correct":
		return []controlprogram.OperatorStateInput{known("publication_id", "publication_id")}
	case "workspace.reconcile":
		return []controlprogram.OperatorStateInput{known("transaction_id", RecoveryTransactionFacet)}
	case "publication.reconcile":
		return []controlprogram.OperatorStateInput{known("transaction_id", RecoveryTransactionFacet)}
	default:
		return nil
	}
}

func projectOperatorReceiptInputs(transition delivery.Transition) []controlprogram.OperatorReceiptInput {
	if transition.ID == "publication.observe" {
		return []controlprogram.OperatorReceiptInput{{Parameter: "publication_id", Transition: "publication.execute", Field: "publication_id"}}
	}
	return nil
}

func transitionFingerprint(value delivery.Transition, outputs []controlprogram.OperatorOutput, stateInputs []controlprogram.OperatorStateInput, receiptInputs []controlprogram.OperatorReceiptInput) (string, error) {
	encoded, err := json.Marshal(struct {
		Transition    delivery.Transition                   `json:"transition"`
		Outputs       []controlprogram.OperatorOutput       `json:"outputs,omitempty"`
		StateInputs   []controlprogram.OperatorStateInput   `json:"state_inputs,omitempty"`
		ReceiptInputs []controlprogram.OperatorReceiptInput `json:"receipt_inputs,omitempty"`
	}{Transition: value, Outputs: outputs, StateInputs: stateInputs, ReceiptInputs: receiptInputs})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}
