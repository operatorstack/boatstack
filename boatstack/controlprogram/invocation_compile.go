package controlprogram

import (
	"encoding/json"
	"fmt"
	"sort"
)

var parameterSourceKinds = map[ParameterSourceKind]bool{
	ParameterSourceEntryInput: true, ParameterSourceState: true,
	ParameterSourceReceipt: true, ParameterSourceWorkOutput: true,
	ParameterSourceTrustedResolver: true, ParameterSourceHostInput: true,
}

func normalizeOperatorParameters(op *Operator, resolver BindingResolver) error {
	seen := map[string]bool{}
	for i := range op.Parameters {
		parameter := &op.Parameters[i]
		field := fmt.Sprintf("operators.%s.parameters[%d]", op.ID, i)
		if !validID(parameter.ID) || seen[parameter.ID] {
			return invalid(field+".id", "invalid or duplicate parameter")
		}
		seen[parameter.ID] = true
		if err := normalizeValueType(&parameter.Type, resolver, field+".type"); err != nil {
			return err
		}
		if len(parameter.AllowedSources) == 0 {
			return invalid(field+".allowed_sources", "at least one source kind is required")
		}
		sort.Slice(parameter.AllowedSources, func(i, j int) bool { return parameter.AllowedSources[i] < parameter.AllowedSources[j] })
		for j, kind := range parameter.AllowedSources {
			if !parameterSourceKinds[kind] || (j > 0 && parameter.AllowedSources[j-1] == kind) {
				return invalid(field+".allowed_sources", "unknown or duplicate source kind")
			}
		}
		var err error
		parameter.Authority.AnyOf, err = normalizedReferenceSet(field+".authority.any_of", parameter.Authority.AnyOf)
		if err != nil {
			return err
		}
		parameter.Authority.AllOf, err = normalizedReferenceSet(field+".authority.all_of", parameter.Authority.AllOf)
		if err != nil {
			return err
		}
	}
	sort.Slice(op.Parameters, func(i, j int) bool { return op.Parameters[i].ID < op.Parameters[j].ID })
	return nil
}

func normalizeValueType(value *ValueTypeDefinition, resolver BindingResolver, field string) error {
	switch value.Kind {
	case "string":
		if value.Minimum != nil || value.Maximum != nil || value.Schema != nil {
			return invalid(field, "string type contains fields owned by another type")
		}
		if err := resolveValidator(&value.Validator, resolver, *value, field+".validator"); err != nil {
			return err
		}
	case "boolean":
		if value.Validator != nil || value.Minimum != nil || value.Maximum != nil || value.Schema != nil {
			return invalid(field, "boolean type does not accept validator or bounds")
		}
	case "integer":
		if value.Validator != nil || value.Schema != nil {
			return invalid(field, "integer type contains fields owned by another type")
		}
		if value.Minimum != nil && value.Maximum != nil && *value.Minimum > *value.Maximum {
			return invalid(field, "integer minimum exceeds maximum")
		}
	case "json":
		if value.Validator != nil || value.Minimum != nil || value.Maximum != nil {
			return invalid(field, "json type contains fields owned by another type")
		}
		if err := resolveValidator(&value.Schema, resolver, *value, field+".schema"); err != nil {
			return err
		}
	default:
		return invalid(field+".kind", "must be string, boolean, integer, or json")
	}
	return nil
}

func resolveValidator(binding **TrustedValidatorBinding, resolver BindingResolver, parameterType ValueTypeDefinition, field string) error {
	if *binding == nil {
		return nil
	}
	value := *binding
	if !semanticReference.MatchString(value.Reference) || value.Version == "" || resolver == nil {
		return invalid(field, "trusted validator binding requires a resolver")
	}
	resolved, err := resolver.ResolveValueValidator(value.Reference, value.Version)
	if err != nil {
		return invalid(field, err.Error())
	}
	if len(resolved.Fingerprint) != 64 || resolved.Type.Kind != parameterType.Kind {
		return invalid(field, "trusted validator type or fingerprint is invalid")
	}
	if value.Fingerprint != "" && value.Fingerprint != resolved.Fingerprint {
		return invalid(field, "trusted validator fingerprint drift")
	}
	value.Fingerprint = resolved.Fingerprint
	return nil
}

func normalizeInvocationCompleteness(document *Document, operators map[string]Operator, work map[string]WorkContract, facets map[string]Facet, resolver BindingResolver) error {
	entries := map[string]map[string]EntryInput{}
	for _, entry := range document.Entries {
		inputs := map[string]EntryInput{}
		for _, input := range entry.Inputs {
			inputs[input.ID] = input
		}
		entries[entry.ID] = inputs
	}
	transitions := map[string]Transition{}
	for _, transition := range document.Transitions {
		transitions[transition.ID] = transition
	}
	for i := range document.Transitions {
		transition := &document.Transitions[i]
		operator := operators[transition.Operator]
		contracts := map[string]OperatorParameter{}
		for _, parameter := range operator.Parameters {
			contracts[parameter.ID] = parameter
		}
		seen := map[string]bool{}
		dependencies := map[string][]string{}
		for j := range transition.Parameters {
			binding := &transition.Parameters[j]
			field := fmt.Sprintf("transitions.%s.parameters[%d]", transition.ID, j)
			contract, exists := contracts[binding.Parameter]
			if !exists {
				return invocationIncomplete(transition.ID, binding.Parameter, "is not declared by the trusted operator")
			}
			if seen[binding.Parameter] {
				return invocationIncomplete(transition.ID, binding.Parameter, "has multiple producers")
			}
			seen[binding.Parameter] = true
			if !containsSource(contract.AllowedSources, binding.Producer.Kind) {
				return invocationIncomplete(transition.ID, binding.Parameter, fmt.Sprintf("does not allow producer kind %q", binding.Producer.Kind))
			}
			deps, err := normalizeProducer(&binding.Producer, contract, *transition, entries, work, facets, transitions, resolver, field)
			if err != nil {
				return err
			}
			dependencies[binding.Parameter] = deps
		}
		for _, contract := range operator.Parameters {
			if contract.Required && !seen[contract.ID] {
				return invocationIncomplete(transition.ID, contract.ID, "has no producer")
			}
		}
		if cycle := parameterDependencyCycle(dependencies); cycle != "" {
			return invocationIncomplete(transition.ID, cycle, "participates in a producer dependency cycle")
		}
		sort.Slice(transition.Parameters, func(i, j int) bool { return transition.Parameters[i].Parameter < transition.Parameters[j].Parameter })
	}
	return nil
}

func normalizeProducer(producer *ParameterProducer, contract OperatorParameter, transition Transition, entries map[string]map[string]EntryInput, work map[string]WorkContract, facets map[string]Facet, transitions map[string]Transition, resolver BindingResolver, field string) ([]string, error) {
	if !parameterSourceKinds[producer.Kind] {
		return nil, invocationIncomplete(transition.ID, contract.ID, "has an unknown producer kind")
	}
	canonicalFields, err := json.Marshal(producer)
	if err != nil {
		return nil, err
	}
	_ = canonicalFields
	if err := rejectProducerExtraneousFields(*producer, field); err != nil {
		return nil, err
	}
	switch producer.Kind {
	case ParameterSourceEntryInput:
		if !validID(producer.Input) {
			return nil, invocationIncomplete(transition.ID, contract.ID, "references an invalid entry input")
		}
		for entryID, inputs := range entries {
			input, ok := inputs[producer.Input]
			if !ok || !compatibleEntryInputType(input.Type, contract.Type.Kind) {
				return nil, invocationIncomplete(transition.ID, contract.ID, fmt.Sprintf("entry-input %q is unavailable or incompatible for reachable entry %q", producer.Input, entryID))
			}
		}
	case ParameterSourceState:
		facet, ok := facets[producer.Facet]
		if !ok || producer.AvailableWhen == nil || !compatibleFacetType(facet.Kind, contract.Type.Kind) {
			return nil, invocationIncomplete(transition.ID, contract.ID, "has an invalid state producer")
		}
		if err := normalizePredicate(producer.AvailableWhen, facets); err != nil {
			return nil, invocationIncomplete(transition.ID, contract.ID, "has an invalid state availability predicate: "+err.Error())
		}
		if !predicateImplies(transition.Guard, *producer.AvailableWhen) {
			return nil, invocationIncomplete(transition.ID, contract.ID, "state availability is not implied by the transition guard")
		}
	case ParameterSourceReceipt:
		prior, ok := transitions[producer.Transition]
		if !ok || !validID(producer.Field) || prior.Priority >= transition.Priority || !predicateImplies(transition.Guard, prior.Target) {
			return nil, invocationIncomplete(transition.ID, contract.ID, "receipt is not guaranteed before the consuming transition")
		}
	case ParameterSourceWorkOutput:
		contractWork, ok := work[producer.Work]
		if !ok {
			return nil, invocationIncomplete(transition.ID, contract.ID, "references unknown foreground work")
		}
		found := false
		for _, output := range contractWork.Outputs {
			if output.ID == producer.Output {
				found = output.Required && compatibleWorkOutputType(output.MediaType, contract.Type.Kind)
			}
		}
		if !found {
			return nil, invocationIncomplete(transition.ID, contract.ID, "references an optional, unknown, or incompatible work output")
		}
	case ParameterSourceTrustedResolver:
		if producer.Binding == nil || resolver == nil || !semanticReference.MatchString(producer.Binding.Reference) || producer.Binding.Version == "" {
			return nil, invocationIncomplete(transition.ID, contract.ID, "has an invalid trusted resolver binding")
		}
		resolved, resolveErr := resolver.ResolveParameterResolver(producer.Binding.Reference, producer.Binding.Version)
		if resolveErr != nil {
			return nil, invocationIncomplete(transition.ID, contract.ID, "trusted resolver is unknown: "+resolveErr.Error())
		}
		if len(resolved.Fingerprint) != 64 || resolved.SourceKind != ParameterSourceTrustedResolver || !sameValueType(resolved.OutputType, contract.Type) {
			return nil, invocationIncomplete(transition.ID, contract.ID, "trusted resolver metadata is incompatible")
		}
		if producer.Binding.Fingerprint != "" && producer.Binding.Fingerprint != resolved.Fingerprint {
			return nil, invocationIncomplete(transition.ID, contract.ID, "trusted resolver fingerprint drift")
		}
		if !authoritySatisfies(resolved.Authority, contract.Authority) {
			return nil, invocationIncomplete(transition.ID, contract.ID, "trusted resolver weakens parameter authority")
		}
		producer.Binding.Fingerprint = resolved.Fingerprint
		return append([]string(nil), resolved.Dependencies...), nil
	case ParameterSourceHostInput:
		if producer.Request == nil || !validID(producer.Request.ID) || producer.Request.Description == "" || producer.Request.Scope != "transition" {
			return nil, invocationIncomplete(transition.ID, contract.ID, "has an invalid host-input request")
		}
		var normalizeErr error
		producer.Request.Authorities, normalizeErr = normalizedReferenceSet(field+".request.authorities", producer.Request.Authorities)
		if normalizeErr != nil || !authorityListSatisfies(producer.Request.Authorities, contract.Authority) {
			return nil, invocationIncomplete(transition.ID, contract.ID, "host-input request weakens parameter authority")
		}
	}
	return nil, nil
}

func rejectProducerExtraneousFields(value ParameterProducer, field string) error {
	invalidFields := false
	switch value.Kind {
	case ParameterSourceEntryInput:
		invalidFields = value.Facet != "" || value.AvailableWhen != nil || value.Transition != "" || value.Field != "" || value.Work != "" || value.Output != "" || value.Binding != nil || value.Request != nil
	case ParameterSourceState:
		invalidFields = value.Input != "" || value.Transition != "" || value.Field != "" || value.Work != "" || value.Output != "" || value.Binding != nil || value.Request != nil
	case ParameterSourceReceipt:
		invalidFields = value.Input != "" || value.Facet != "" || value.AvailableWhen != nil || value.Work != "" || value.Output != "" || value.Binding != nil || value.Request != nil
	case ParameterSourceWorkOutput:
		invalidFields = value.Input != "" || value.Facet != "" || value.AvailableWhen != nil || value.Transition != "" || value.Field != "" || value.Binding != nil || value.Request != nil
	case ParameterSourceTrustedResolver:
		invalidFields = value.Input != "" || value.Facet != "" || value.AvailableWhen != nil || value.Transition != "" || value.Field != "" || value.Work != "" || value.Output != "" || value.Request != nil
	case ParameterSourceHostInput:
		invalidFields = value.Input != "" || value.Facet != "" || value.AvailableWhen != nil || value.Transition != "" || value.Field != "" || value.Work != "" || value.Output != "" || value.Binding != nil
	}
	if invalidFields {
		return invalid(field, "producer contains fields owned by another source kind")
	}
	return nil
}

func invocationIncomplete(transition, parameter, detail string) error {
	return fmt.Errorf("CONTROL_PROGRAM_INVOCATION_INCOMPLETE: transition %q parameter %q %s", transition, parameter, detail)
}

func containsSource(values []ParameterSourceKind, wanted ParameterSourceKind) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func sameValueType(left, right ValueTypeDefinition) bool {
	leftRaw, leftErr := json.Marshal(left)
	rightRaw, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftRaw) == string(rightRaw)
}

func compatibleEntryInputType(input, wanted string) bool {
	if input == wanted {
		return true
	}
	return wanted == "string" && (input == "markdown-file" || input == "text" || input == "string")
}

func compatibleFacetType(facet, wanted string) bool {
	if facet == "enum" {
		facet = "string"
	}
	return facet == wanted
}

func compatibleWorkOutputType(mediaType, wanted string) bool {
	if mediaType == "application/json" {
		return wanted == "json"
	}
	return wanted == "string"
}

func predicateImplies(guard, condition Predicate) bool {
	wanted, _ := json.Marshal(condition)
	actual, _ := json.Marshal(guard)
	if string(wanted) == string(actual) {
		return true
	}
	for _, child := range guard.All {
		encoded, _ := json.Marshal(child)
		if string(encoded) == string(wanted) {
			return true
		}
	}
	return false
}

func authorityListSatisfies(actual []string, required AuthorityRequirement) bool {
	set := map[string]bool{}
	for _, value := range actual {
		set[value] = true
	}
	for _, value := range required.AllOf {
		if !set[value] {
			return false
		}
	}
	if len(required.AnyOf) == 0 {
		return true
	}
	for _, value := range required.AnyOf {
		if set[value] {
			return true
		}
	}
	return false
}

func authoritySatisfies(actual, required AuthorityRequirement) bool {
	combined := append(append([]string(nil), actual.AnyOf...), actual.AllOf...)
	return authorityListSatisfies(combined, required)
}

func parameterDependencyCycle(graph map[string][]string) string {
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) string
	visit = func(node string) string {
		if visiting[node] {
			return node
		}
		if visited[node] {
			return ""
		}
		visiting[node] = true
		for _, dependency := range graph[node] {
			if _, local := graph[dependency]; local {
				if cycle := visit(dependency); cycle != "" {
					return cycle
				}
			}
		}
		visiting[node], visited[node] = false, true
		return ""
	}
	for node := range graph {
		if cycle := visit(node); cycle != "" {
			return cycle
		}
	}
	return ""
}
