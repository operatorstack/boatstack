package controlprogram

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
)

var semanticID = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)
var semanticReference = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)

type Compiled struct {
	Document    Document
	Canonical   []byte
	Fingerprint string
}

func Load(source io.Reader, resolver BindingResolver) (Compiled, error) {
	raw, err := readLimited(source, 16<<20, "CONTROL_PROGRAM_INVALID: input exceeds 16 MiB")
	if err != nil {
		return Compiled{}, err
	}
	if err := rejectDuplicateKeys(raw); err != nil {
		return Compiled{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var document Document
	if err := decoder.Decode(&document); err != nil {
		return Compiled{}, fmt.Errorf("CONTROL_PROGRAM_INVALID: %w", err)
	}
	if err := requireEOF(decoder); err != nil {
		return Compiled{}, err
	}
	return Compile(document, resolver)
}

func readLimited(source io.Reader, limit int64, oversized string) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(source, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("%s", oversized)
	}
	return raw, nil
}

func Compile(document Document, resolver BindingResolver) (Compiled, error) {
	if document.SchemaVersion != SchemaVersion {
		return Compiled{}, invalid("schema_version", "unsupported schema")
	}
	if !validID(document.Program.ID) || document.Program.Version == "" {
		return Compiled{}, invalid("program", "id and version are required")
	}
	var err error
	if document.Declarations.Capabilities, err = normalizedReferenceSet("declarations.capabilities", document.Declarations.Capabilities); err != nil {
		return Compiled{}, err
	}
	if document.Declarations.Authorities, err = normalizedReferenceSet("declarations.authorities", document.Declarations.Authorities); err != nil {
		return Compiled{}, err
	}
	if document.Declarations.Effects, err = normalizedReferenceSet("declarations.effects", document.Declarations.Effects); err != nil {
		return Compiled{}, err
	}
	if document.Declarations.Verifiers, err = normalizedReferenceSet("declarations.verifiers", document.Declarations.Verifiers); err != nil {
		return Compiled{}, err
	}
	if document.Declarations.InputResolvers, err = normalizedReferenceSet("declarations.input_resolvers", document.Declarations.InputResolvers); err != nil {
		return Compiled{}, err
	}

	facets := map[string]Facet{}
	for index := range document.Facets {
		facet := &document.Facets[index]
		if !validID(facet.ID) || (facet.Kind != "enum" && facet.Kind != "string" && facet.Kind != "boolean") {
			return Compiled{}, invalid(fmt.Sprintf("facets[%d]", index), "invalid facet")
		}
		if _, exists := facets[facet.ID]; exists {
			return Compiled{}, invalid("facets", "duplicate "+facet.ID)
		}
		facet.Values, err = normalizedValues("facets."+facet.ID+".values", facet.Values)
		if err != nil {
			return Compiled{}, err
		}
		if facet.Kind == "enum" && len(facet.Values) == 0 {
			return Compiled{}, invalid("facets."+facet.ID, "enum values are required")
		}
		if facet.Kind != "enum" && len(facet.Values) != 0 {
			return Compiled{}, invalid("facets."+facet.ID, "values are allowed only for enum facets")
		}
		facets[facet.ID] = *facet
	}
	if len(facets) == 0 {
		return Compiled{}, invalid("facets", "at least one facet is required")
	}
	sort.Slice(document.Facets, func(i, j int) bool { return document.Facets[i].ID < document.Facets[j].ID })

	if err := normalizeEvidence(&document, facets); err != nil {
		return Compiled{}, err
	}
	operators, err := normalizeOperators(&document, facets, resolver)
	if err != nil {
		return Compiled{}, err
	}
	if err := normalizeTransitions(&document, facets, operators); err != nil {
		return Compiled{}, err
	}
	if err := normalizeTargetsAndEntries(&document, facets); err != nil {
		return Compiled{}, err
	}

	semantic := stripDescriptions(document)
	canonical, err := json.Marshal(semantic)
	if err != nil {
		return Compiled{}, err
	}
	digest := sha256.Sum256(canonical)
	fingerprint := hex.EncodeToString(digest[:])
	pretty, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return Compiled{}, err
	}
	pretty = append(pretty, '\n')
	return Compiled{Document: document, Canonical: pretty, Fingerprint: fingerprint}, nil
}

func normalizeEvidence(document *Document, facets map[string]Facet) error {
	seen := map[string]bool{}
	for i := range document.Evidence {
		value := document.Evidence[i]
		if !validID(value.ID) || !validID(value.Kind) || facets[value.Subject].ID == "" {
			return invalid(fmt.Sprintf("evidence[%d]", i), "invalid evidence relation")
		}
		if seen[value.ID] {
			return invalid("evidence", "duplicate "+value.ID)
		}
		seen[value.ID] = true
	}
	sort.Slice(document.Evidence, func(i, j int) bool { return document.Evidence[i].ID < document.Evidence[j].ID })
	return nil
}

func normalizeOperators(document *Document, facets map[string]Facet, resolver BindingResolver) (map[string]Operator, error) {
	seen := map[string]Operator{}
	for i := range document.Operators {
		op := &document.Operators[i]
		var expectedBinding *Operator
		if !validID(op.ID) || seen[op.ID].ID != "" {
			return nil, invalid(fmt.Sprintf("operators[%d].id", i), "invalid or duplicate operator")
		}
		if op.Binding != nil {
			if resolver == nil {
				return nil, invalid("operators."+op.ID+".binding", "no binding resolver is available")
			}
			compiledBinding := op.Binding.Fingerprint != ""
			if !compiledBinding && hasInlineSemantics(*op) {
				return nil, invalid("operators."+op.ID, "trusted bindings cannot be overridden")
			}
			resolved, err := resolver.ResolveOperator(op.Binding.Reference, op.Binding.Version)
			if err != nil {
				return nil, invalid("operators."+op.ID+".binding", err.Error())
			}
			if len(resolved.Fingerprint) != 64 {
				return nil, invalid("operators."+op.ID+".binding", "binding fingerprint is invalid")
			}
			if compiledBinding {
				if op.Binding.Fingerprint != resolved.Fingerprint {
					return nil, invalid("operators."+op.ID+".binding", "binding fingerprint drift")
				}
				expected := Operator{ID: op.ID, Binding: &OperatorBinding{Reference: op.Binding.Reference, Version: op.Binding.Version, Fingerprint: resolved.Fingerprint}, Capabilities: resolved.Capabilities, Authority: resolved.Authority, Effects: resolved.Effects, Verifier: resolved.Verifier, Recovery: resolved.Recovery, StateEffect: &resolved.StateEffect}
				expectedBinding = &expected
			} else {
				op.Binding.Fingerprint = resolved.Fingerprint
				op.Capabilities, op.Authority, op.Effects = resolved.Capabilities, resolved.Authority, resolved.Effects
				op.Verifier, op.Recovery, op.StateEffect = resolved.Verifier, resolved.Recovery, &resolved.StateEffect
			}
		}
		var err error
		if op.Capabilities, err = normalizedReferenceSet("operators."+op.ID+".capabilities", op.Capabilities); err != nil {
			return nil, err
		}
		if op.Authority, err = normalizedReferenceSet("operators."+op.ID+".authority", op.Authority); err != nil {
			return nil, err
		}
		if op.Effects, err = normalizedReferenceSet("operators."+op.ID+".effects", op.Effects); err != nil {
			return nil, err
		}
		if op.Binding != nil {
			document.Declarations.Capabilities = union(document.Declarations.Capabilities, op.Capabilities)
			document.Declarations.Authorities = union(document.Declarations.Authorities, op.Authority)
			document.Declarations.Effects = union(document.Declarations.Effects, op.Effects)
			document.Declarations.Verifiers = union(document.Declarations.Verifiers, []string{op.Verifier})
		}
		if missing := firstUndeclared(op.Capabilities, document.Declarations.Capabilities); missing != "" {
			return nil, invalid("operators."+op.ID+".capabilities", "undeclared "+missing)
		}
		if missing := firstUndeclared(op.Authority, document.Declarations.Authorities); missing != "" {
			return nil, invalid("operators."+op.ID+".authority", "undeclared "+missing)
		}
		if missing := firstUndeclared(op.Effects, document.Declarations.Effects); missing != "" {
			return nil, invalid("operators."+op.ID+".effects", "undeclared "+missing)
		}
		if op.Verifier == "" || !contains(document.Declarations.Verifiers, op.Verifier) {
			return nil, invalid("operators."+op.ID+".verifier", "undeclared verifier")
		}
		if len(op.Effects) != 0 && op.Recovery == "" {
			return nil, invalid("operators."+op.ID+".recovery", "effectful operator requires recovery")
		}
		if op.StateEffect == nil {
			return nil, invalid("operators."+op.ID+".state_effect", "state effect is required")
		}
		if err := normalizeStateEffect(op.StateEffect, facets); err != nil {
			return nil, invalid("operators."+op.ID+".state_effect", err.Error())
		}
		if expectedBinding != nil {
			expectedBinding.Capabilities, _ = normalizedReferenceSet("binding.capabilities", expectedBinding.Capabilities)
			expectedBinding.Authority, _ = normalizedReferenceSet("binding.authority", expectedBinding.Authority)
			expectedBinding.Effects, _ = normalizedReferenceSet("binding.effects", expectedBinding.Effects)
			_ = normalizeStateEffect(expectedBinding.StateEffect, facets)
			if !sameOperatorSemantics(*op, *expectedBinding) {
				return nil, invalid("operators."+op.ID, "compiled binding semantics drift")
			}
		}
		seen[op.ID] = *op
	}
	var err error
	if document.Declarations.Capabilities, err = normalizedReferenceSet("declarations.capabilities", document.Declarations.Capabilities); err != nil {
		return nil, err
	}
	if document.Declarations.Authorities, err = normalizedReferenceSet("declarations.authorities", document.Declarations.Authorities); err != nil {
		return nil, err
	}
	if document.Declarations.Effects, err = normalizedReferenceSet("declarations.effects", document.Declarations.Effects); err != nil {
		return nil, err
	}
	if document.Declarations.Verifiers, err = normalizedReferenceSet("declarations.verifiers", document.Declarations.Verifiers); err != nil {
		return nil, err
	}
	if len(seen) == 0 {
		return nil, invalid("operators", "at least one operator is required")
	}
	sort.Slice(document.Operators, func(i, j int) bool { return document.Operators[i].ID < document.Operators[j].ID })
	return seen, nil
}

func normalizeTransitions(document *Document, facets map[string]Facet, operators map[string]Operator) error {
	seen := map[string]bool{}
	for i := range document.Transitions {
		value := &document.Transitions[i]
		if !validID(value.ID) || seen[value.ID] || operators[value.Operator].ID == "" {
			return invalid(fmt.Sprintf("transitions[%d]", i), "invalid transition or operator reference")
		}
		seen[value.ID] = true
		if err := normalizePredicate(&value.Guard, facets); err != nil {
			return invalid("transitions."+value.ID+".guard", err.Error())
		}
		if err := normalizePredicate(&value.Target, facets); err != nil {
			return invalid("transitions."+value.ID+".target", err.Error())
		}
	}
	if len(seen) == 0 {
		return invalid("transitions", "at least one transition is required")
	}
	for _, operator := range operators {
		if operator.Binding == nil && len(operator.Effects) != 0 && !seen[operator.Recovery] {
			return invalid("operators."+operator.ID+".recovery", "unknown recovery transition "+operator.Recovery)
		}
	}
	sort.Slice(document.Transitions, func(i, j int) bool { return document.Transitions[i].ID < document.Transitions[j].ID })
	return nil
}

func normalizeTargetsAndEntries(document *Document, facets map[string]Facet) error {
	targets := map[string]bool{}
	for i := range document.Targets {
		value := &document.Targets[i]
		if !validID(value.ID) || targets[value.ID] {
			return invalid(fmt.Sprintf("targets[%d].id", i), "invalid or duplicate target")
		}
		targets[value.ID] = true
		if err := normalizePredicate(&value.Predicate, facets); err != nil {
			return invalid("targets."+value.ID, err.Error())
		}
	}
	if len(targets) == 0 {
		return invalid("targets", "at least one target is required")
	}
	sort.Slice(document.Targets, func(i, j int) bool { return document.Targets[i].ID < document.Targets[j].ID })
	entries := map[string]bool{}
	for i := range document.Entries {
		entry := &document.Entries[i]
		if !validID(entry.ID) || entries[entry.ID] || !targets[entry.Target] {
			return invalid(fmt.Sprintf("entries[%d]", i), "invalid entry or target reference")
		}
		entries[entry.ID] = true
		inputs := map[string]bool{}
		for j := range entry.Inputs {
			input := &entry.Inputs[j]
			if !validID(input.ID) || !validID(input.Type) || inputs[input.ID] || (input.Resolver != "" && !contains(document.Declarations.InputResolvers, input.Resolver)) || (len(input.Config) != 0 && !json.Valid(input.Config)) {
				return invalid(fmt.Sprintf("entries.%s.inputs[%d]", entry.ID, j), "invalid input or resolver")
			}
			if len(input.Config) != 0 {
				if err := rejectDuplicateKeys(input.Config); err != nil {
					return invalid(fmt.Sprintf("entries.%s.inputs[%d].config", entry.ID, j), err.Error())
				}
				var decoded any
				if err := json.Unmarshal(input.Config, &decoded); err != nil {
					return invalid(fmt.Sprintf("entries.%s.inputs[%d].config", entry.ID, j), err.Error())
				}
				canonical, err := json.Marshal(decoded)
				if err != nil {
					return err
				}
				input.Config = canonical
			}
			inputs[input.ID] = true
		}
		sort.Slice(entry.Inputs, func(i, j int) bool { return entry.Inputs[i].ID < entry.Inputs[j].ID })
	}
	if len(entries) == 0 {
		return invalid("entries", "at least one entry is required")
	}
	sort.Slice(document.Entries, func(i, j int) bool { return document.Entries[i].ID < document.Entries[j].ID })
	return nil
}

func normalizeStateEffect(effect *StateEffect, facets map[string]Facet) error {
	if effect.Kind != "assignments" && effect.Kind != "native" {
		return fmt.Errorf("invalid kind")
	}
	if effect.Kind == "native" {
		if effect.NativeHandler == "" || len(effect.Assignments) != 0 {
			return fmt.Errorf("native effect requires only a handler")
		}
	} else if effect.NativeHandler != "" || len(effect.Assignments) == 0 {
		return fmt.Errorf("assignment effect requires assignments")
	}
	seenPre, seenAssign := map[string]bool{}, map[string]bool{}
	for i := range effect.Preconditions {
		value := &effect.Preconditions[i]
		if facets[value.Facet].ID == "" || seenPre[value.Facet] {
			return fmt.Errorf("invalid precondition facet")
		}
		seenPre[value.Facet] = true
		var err error
		value.Values, err = normalizedValues("precondition.values", value.Values)
		if err != nil || len(value.Values) == 0 {
			return fmt.Errorf("invalid precondition values")
		}
		facet := facets[value.Facet]
		if facet.Kind == "enum" {
			for _, item := range value.Values {
				if !contains(facet.Values, item) {
					return fmt.Errorf("precondition value %q is not declared by facet %q", item, value.Facet)
				}
			}
		}
	}
	for i := range effect.Assignments {
		value := &effect.Assignments[i]
		if facets[value.Facet].ID == "" || seenAssign[value.Facet] {
			return fmt.Errorf("invalid assignment facet")
		}
		seenAssign[value.Facet] = true
		if (value.Value == nil) == (value.ValueFrom == nil) {
			return fmt.Errorf("assignment requires exactly one value source")
		}
		if value.Value != nil {
			facet := facets[value.Facet]
			if facet.Kind == "enum" && !contains(facet.Values, *value.Value) {
				return fmt.Errorf("assignment value %q is not declared by facet %q", *value.Value, value.Facet)
			}
		}
		if value.ValueFrom != nil {
			count := 0
			for _, source := range []string{value.ValueFrom.Parameter, value.ValueFrom.Admission, value.ValueFrom.Invocation} {
				if source != "" {
					count++
				}
			}
			if count != 1 {
				return fmt.Errorf("assignment reference requires exactly one source")
			}
		}
	}
	sort.Slice(effect.Preconditions, func(i, j int) bool { return effect.Preconditions[i].Facet < effect.Preconditions[j].Facet })
	sort.Slice(effect.Assignments, func(i, j int) bool { return effect.Assignments[i].Facet < effect.Assignments[j].Facet })
	return nil
}

func normalizePredicate(value *Predicate, facets map[string]Facet) error {
	variants := 0
	if len(value.All) != 0 {
		variants++
	}
	if len(value.Any) != 0 {
		variants++
	}
	if value.Not != nil {
		variants++
	}
	if value.Fact != nil {
		variants++
	}
	if value.True != nil {
		variants++
	}
	if variants != 1 {
		return fmt.Errorf("predicate must contain exactly one AST node")
	}
	children := value.All
	if len(value.Any) != 0 {
		children = value.Any
	}
	for i := range children {
		if err := normalizePredicate(&children[i], facets); err != nil {
			return err
		}
	}
	if len(value.All) != 0 {
		value.All = children
		sortPredicates(value.All)
	}
	if len(value.Any) != 0 {
		value.Any = children
		sortPredicates(value.Any)
	}
	if value.Not != nil {
		return normalizePredicate(value.Not, facets)
	}
	if value.Fact != nil {
		facet, ok := facets[value.Fact.Facet]
		if !ok {
			return fmt.Errorf("unknown facet %q", value.Fact.Facet)
		}
		var err error
		value.Fact.Statuses, err = normalizedValues("predicate.statuses", value.Fact.Statuses)
		if err != nil {
			return err
		}
		for _, status := range value.Fact.Statuses {
			if !contains([]string{"absent", "ambiguous", "conflicting", "known", "stale", "unknown"}, status) {
				return fmt.Errorf("unknown fact status %q", status)
			}
		}
		value.Fact.Values, err = normalizedValues("predicate.values", value.Fact.Values)
		if err != nil {
			return err
		}
		if facet.Kind == "enum" {
			for _, item := range value.Fact.Values {
				if !contains(facet.Values, item) {
					return fmt.Errorf("unknown value %q for facet %q", item, facet.ID)
				}
			}
		}
	}
	return nil
}

func sortPredicates(values []Predicate) {
	sort.Slice(values, func(i, j int) bool {
		left, _ := json.Marshal(values[i])
		right, _ := json.Marshal(values[j])
		return bytes.Compare(left, right) < 0
	})
}

func stripDescriptions(value Document) Document {
	value.Facets = append([]Facet(nil), value.Facets...)
	value.Evidence = append([]Evidence(nil), value.Evidence...)
	value.Operators = append([]Operator(nil), value.Operators...)
	value.Transitions = append([]Transition(nil), value.Transitions...)
	value.Targets = append([]Target(nil), value.Targets...)
	value.Entries = append([]Entry(nil), value.Entries...)
	value.Description, value.Program.Description = "", ""
	for i := range value.Facets {
		value.Facets[i].Description = ""
	}
	for i := range value.Evidence {
		value.Evidence[i].Description = ""
	}
	for i := range value.Operators {
		value.Operators[i].Description = ""
	}
	for i := range value.Transitions {
		value.Transitions[i].Description = ""
	}
	for i := range value.Targets {
		value.Targets[i].Description = ""
	}
	for i := range value.Entries {
		value.Entries[i].Description = ""
	}
	return value
}

func hasInlineSemantics(value Operator) bool {
	return len(value.Capabilities) != 0 || len(value.Authority) != 0 || len(value.Effects) != 0 || value.Verifier != "" || value.Recovery != "" || value.StateEffect != nil
}
func sameOperatorSemantics(left, right Operator) bool {
	left.Description, right.Description = "", ""
	leftRaw, leftErr := json.Marshal(left)
	rightRaw, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftRaw, rightRaw)
}
func validID(value string) bool { return semanticID.MatchString(value) }
func contains(values []string, wanted string) bool {
	i := sort.SearchStrings(values, wanted)
	return i < len(values) && values[i] == wanted
}
func firstUndeclared(values, declarations []string) string {
	for _, value := range values {
		if !contains(declarations, value) {
			return value
		}
	}
	return ""
}
func normalizedSet(field string, values []string) ([]string, error) {
	for _, value := range values {
		if !validID(value) {
			return nil, invalid(field, "invalid declaration "+value)
		}
	}
	return normalizedValues(field, values)
}
func normalizedReferenceSet(field string, values []string) ([]string, error) {
	for _, value := range values {
		if !semanticReference.MatchString(value) {
			return nil, invalid(field, "invalid declaration "+value)
		}
	}
	return normalizedValues(field, values)
}
func normalizedValues(field string, values []string) ([]string, error) {
	out := append([]string(nil), values...)
	sort.Strings(out)
	for i, v := range out {
		if v == "" || (i > 0 && out[i-1] == v) {
			return nil, invalid(field, "empty or duplicate value")
		}
	}
	return out, nil
}
func union(left, right []string) []string {
	seen := make(map[string]bool, len(left)+len(right))
	for _, value := range left {
		seen[value] = true
	}
	for _, value := range right {
		if value != "" {
			seen[value] = true
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
func invalid(field, detail string) error {
	return fmt.Errorf("CONTROL_PROGRAM_INVALID: %s: %s", field, detail)
}
func requireEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return invalid("document", "trailing JSON")
	}
	return nil
}

func rejectDuplicateKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key := keyToken.(string)
				if seen[key] {
					return invalid("document", "duplicate field "+key)
				}
				seen[key] = true
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		}
		return nil
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return invalid("document", "trailing JSON")
	}
	return nil
}
