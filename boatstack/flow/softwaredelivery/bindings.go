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
	fingerprint, err := transitionFingerprint(transition)
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
	}, nil
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

func transitionFingerprint(value delivery.Transition) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}
