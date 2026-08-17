package controlprogram_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	"github.com/operatorstack/boatstack/boatstack/internal/hostprojection"
)

type delegationResolver struct {
	fingerprint string
	authorities []string
	delegable   bool
}

type invocationResolver struct {
	delegationResolver
	operators          map[string]controlprogram.ResolvedOperator
	parameterResolvers map[string]controlprogram.ResolvedParameterResolver
	validators         map[string]controlprogram.ResolvedValueValidator
}

func (r invocationResolver) ResolveOperator(reference, version string) (controlprogram.ResolvedOperator, error) {
	if version != "1" {
		return controlprogram.ResolvedOperator{}, os.ErrNotExist
	}
	value, ok := r.operators[reference]
	if !ok {
		return controlprogram.ResolvedOperator{}, os.ErrNotExist
	}
	return value, nil
}

func (r invocationResolver) ResolveParameterResolver(reference, version string) (controlprogram.ResolvedParameterResolver, error) {
	if version != "1" {
		return controlprogram.ResolvedParameterResolver{}, os.ErrNotExist
	}
	value, ok := r.parameterResolvers[reference]
	if !ok {
		return controlprogram.ResolvedParameterResolver{}, os.ErrNotExist
	}
	return value, nil
}

func (r invocationResolver) ResolveValueValidator(reference, version string) (controlprogram.ResolvedValueValidator, error) {
	if version != "1" {
		return controlprogram.ResolvedValueValidator{}, os.ErrNotExist
	}
	value, ok := r.validators[reference]
	if !ok {
		return controlprogram.ResolvedValueValidator{}, os.ErrNotExist
	}
	return value, nil
}

func (r delegationResolver) ResolveOperator(string, string) (controlprogram.ResolvedOperator, error) {
	return controlprogram.ResolvedOperator{}, nil
}

func (r delegationResolver) ResolveDelegation(reference, version string) (controlprogram.ResolvedDelegation, error) {
	if reference != "incident/delegation/autonomy" || version != "1" {
		return controlprogram.ResolvedDelegation{}, os.ErrNotExist
	}
	return controlprogram.ResolvedDelegation{Fingerprint: r.fingerprint, Authorities: r.authorities, Delegable: r.delegable}, nil
}

func (r delegationResolver) ResolveParameterResolver(string, string) (controlprogram.ResolvedParameterResolver, error) {
	return controlprogram.ResolvedParameterResolver{}, os.ErrNotExist
}

func (r delegationResolver) ResolveValueValidator(string, string) (controlprogram.ResolvedValueValidator, error) {
	return controlprogram.ResolvedValueValidator{}, os.ErrNotExist
}

func incidentProgram() controlprogram.Document {
	mitigated := "mitigated"
	return controlprogram.Document{
		Schema: controlprogram.SchemaName, SchemaRevision: controlprogram.SchemaRevision,
		Program:     controlprogram.Program{ID: "incident-response", Version: "1", Description: "human text"},
		Description: "incident control program",
		Declarations: controlprogram.Declarations{
			Capabilities: []string{"service.restart"}, Authorities: []string{"incident-commander"},
			Effects: []string{"service.restart"}, Verifiers: []string{"healthcheck"}, InputResolvers: []string{"incident.input"},
		},
		Facets: []controlprogram.Facet{
			{ID: "service", Kind: "enum", Values: []string{"healthy", "degraded"}, Description: "service health"},
			{ID: "incident", Kind: "enum", Values: []string{"open", "mitigated"}},
		},
		Evidence: []controlprogram.Evidence{{ID: "healthcheck", Subject: "service", Kind: "observation", Description: "observed health"}},
		Operators: []controlprogram.Operator{{
			ID: "restart", Capabilities: []string{"service.restart"}, Authority: controlprogram.AuthorityRequirement{AnyOf: []string{"incident-commander"}},
			Effects: []string{"service.restart"}, Verifier: "healthcheck", Recovery: "restart",
			Description: "restart the service", ExecutionContext: "preserve",
			StateEffect: &controlprogram.StateEffect{Kind: "assignments", Assignments: []controlprogram.StateAssignment{{Facet: "incident", Value: &mitigated}}},
		}},
		Transitions: []controlprogram.Transition{{
			ID: "restart", Operator: "restart", Priority: 10,
			Guard: fact("incident", "open"), Target: fact("incident", "mitigated"), Description: "restart service",
		}},
		Targets: []controlprogram.Target{{ID: "mitigated", Predicate: fact("incident", "mitigated"), Description: "incident mitigated"}},
		Entries: []controlprogram.Entry{{ID: "respond", Target: "mitigated", Description: "respond to incident", Inputs: []controlprogram.EntryInput{{ID: "incident", Type: "json", Required: true, Resolver: "incident.input", Config: json.RawMessage(`{"b":2,"a":1}`)}}}},
	}
}

func parameterProgram() controlprogram.Document {
	document := incidentProgram()
	document.Declarations.Authorities = append(document.Declarations.Authorities, "human")
	document.Operators[0].Parameters = []controlprogram.OperatorParameter{{
		ID: "channel", Type: controlprogram.ValueTypeDefinition{Kind: "string"}, Required: true,
		AllowedSources: []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceHostInput},
		Authority:      controlprogram.AuthorityRequirement{AnyOf: []string{"human"}},
	}}
	document.Transitions[0].Parameters = []controlprogram.TransitionParameterBinding{{
		Parameter: "channel",
		Producer: controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceHostInput, Request: &controlprogram.HostInputRequest{
			ID: "channel", Description: "Select the response channel.", Authorities: []string{"human"}, Scope: "transition",
		}},
	}}
	return document
}

func TestHumanIdentityRoleIsOptionalGenericProgramSemantics(t *testing.T) {
	withoutRole, err := controlprogram.Compile(incidentProgram(), nil)
	if err != nil {
		t.Fatal(err)
	}
	withRoleDocument := incidentProgram()
	withRoleDocument.Program.HumanIdentity = "incident-commander"
	withRole, err := controlprogram.Compile(withRoleDocument, nil)
	if err != nil {
		t.Fatal(err)
	}
	changedRoleDocument := withRoleDocument
	changedRoleDocument.Program.HumanIdentity = "release-manager"
	changedRole, err := controlprogram.Compile(changedRoleDocument, nil)
	if err != nil {
		t.Fatal(err)
	}
	if withoutRole.Fingerprint == withRole.Fingerprint || withRole.Fingerprint == changedRole.Fingerprint {
		t.Fatal("human identity role did not participate in ProgramFingerprint")
	}
	invalid := incidentProgram()
	invalid.Program.HumanIdentity = "Release Manager"
	if _, err := controlprogram.Compile(invalid, nil); err == nil || !strings.Contains(err.Error(), "program.human_identity") {
		t.Fatalf("invalid role result = %v", err)
	}
}

func TestInvocationCompletenessRequiresExactlyOneAdmissibleProducer(t *testing.T) {
	// control-law: required-transition-parameters-have-exactly-one-admissible-producer-before-publication
	if _, err := controlprogram.Compile(parameterProgram(), nil); err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		mutate  func(*controlprogram.Document)
		witness string
	}{
		"missing": {func(value *controlprogram.Document) { value.Transitions[0].Parameters = nil }, "has no producer"},
		"duplicate": {func(value *controlprogram.Document) {
			value.Transitions[0].Parameters = append(value.Transitions[0].Parameters, value.Transitions[0].Parameters[0])
		}, "multiple producers"},
		"unknown-parameter": {func(value *controlprogram.Document) { value.Transitions[0].Parameters[0].Parameter = "missing" }, "not declared"},
		"disallowed-kind": {func(value *controlprogram.Document) {
			value.Transitions[0].Parameters[0].Producer = controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceEntryInput, Input: "incident"}
		}, "does not allow producer kind"},
		"wrong-source-type": {func(value *controlprogram.Document) {
			value.Operators[0].Parameters[0].Type = controlprogram.ValueTypeDefinition{Kind: "integer"}
			value.Operators[0].Parameters[0].AllowedSources = []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceEntryInput}
			value.Operators[0].Parameters[0].Authority = controlprogram.AuthorityRequirement{}
			value.Transitions[0].Parameters[0].Producer = controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceEntryInput, Input: "incident"}
		}, "unavailable or incompatible"},
		"authority-weakening": {func(value *controlprogram.Document) {
			value.Transitions[0].Parameters[0].Producer.Request.Authorities = nil
		}, "weakens parameter authority"},
	} {
		t.Run(name, func(t *testing.T) {
			value := parameterProgram()
			test.mutate(&value)
			if _, err := controlprogram.Compile(value, nil); err == nil || !strings.Contains(err.Error(), test.witness) {
				t.Fatalf("result = %v, want %q", err, test.witness)
			}
		})
	}
}

func TestInvocationCompletenessRequiresAuthorityReceiptProducer(t *testing.T) {
	// control-law: producer metadata cannot stand in for an authority receipt
	// attached to the admitted parameter value.
	for name, test := range map[string]struct {
		authority controlprogram.AuthorityRequirement
		producer  controlprogram.ParameterProducer
	}{
		"entry-input-any-of":      {controlprogram.AuthorityRequirement{AnyOf: []string{"human"}}, controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceEntryInput, Input: "incident"}},
		"state-all-of":            {controlprogram.AuthorityRequirement{AllOf: []string{"human"}}, controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceState, Facet: "service", AvailableWhen: func() *controlprogram.Predicate { value := fact("service", "degraded"); return &value }()}},
		"receipt-any-of":          {controlprogram.AuthorityRequirement{AnyOf: []string{"human"}}, controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceReceipt, Transition: "observe-channel", Field: "channel"}},
		"work-output-all-of":      {controlprogram.AuthorityRequirement{AllOf: []string{"human"}}, controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceWorkOutput, Work: "plan", Output: "channel"}},
		"trusted-resolver-any-of": {controlprogram.AuthorityRequirement{AnyOf: []string{"human"}}, controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceTrustedResolver, Binding: &controlprogram.ParameterResolverBinding{Reference: "incident/channel", Version: "1"}}},
	} {
		t.Run(name, func(t *testing.T) {
			value := parameterProgram()
			value.Operators[0].Parameters[0].Authority = test.authority
			value.Operators[0].Parameters[0].AllowedSources = []controlprogram.ParameterSourceKind{test.producer.Kind}
			value.Transitions[0].Parameters[0].Producer = test.producer
			if _, err := controlprogram.Compile(value, nil); err == nil || !strings.Contains(err.Error(), "requires an authority-receipt-producing host-input producer") {
				t.Fatalf("result = %v", err)
			}
		})
	}
}

func TestInvocationCompletenessRejectsUntrustedReceiptAndUnknownResolver(t *testing.T) {
	receiptBound := parameterProgram()
	receiptBound.Operators[0].Parameters[0].Authority = controlprogram.AuthorityRequirement{}
	receiptBound.Operators[0].Parameters[0].AllowedSources = []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceReceipt}
	priorOperator := receiptBound.Operators[0]
	priorOperator.ID, priorOperator.Parameters = "observe-channel", nil
	receiptBound.Operators = append(receiptBound.Operators, priorOperator)
	truth := true
	receiptBound.Transitions = append([]controlprogram.Transition{{
		ID: "observe-channel", Operator: "observe-channel", Guard: controlprogram.Predicate{True: &truth}, Target: fact("service", "healthy"), Priority: 1,
	}}, receiptBound.Transitions...)
	receiptBound.Transitions[1].Parameters[0].Producer = controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceReceipt, Transition: "observe-channel", Field: "channel"}
	if _, err := controlprogram.Compile(receiptBound, nil); err == nil || !strings.Contains(err.Error(), "receipt availability is not guaranteed by the trusted operator binding") {
		t.Fatalf("untrusted receipt result = %v", err)
	}

	unknown := parameterProgram()
	unknown.Operators[0].Parameters[0].Authority = controlprogram.AuthorityRequirement{}
	unknown.Operators[0].Parameters[0].AllowedSources = []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceTrustedResolver}
	unknown.Transitions[0].Parameters[0].Producer = controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceTrustedResolver, Binding: &controlprogram.ParameterResolverBinding{Reference: "incident/missing", Version: "1"}}
	if _, err := controlprogram.Compile(unknown, invocationResolver{}); err == nil || !strings.Contains(err.Error(), "trusted resolver is unknown") {
		t.Fatalf("unknown resolver result = %v", err)
	}
}

func TestInvocationCompletenessAcceptsOnlyExactTrustedReceiptProvenance(t *testing.T) {
	// control-law: state predicates cannot stand in for a committed receipt;
	// the consumer binding must guarantee the exact producer transition field.
	document := incidentProgram()
	document.Operators = []controlprogram.Operator{
		{ID: "observe-channel", Binding: &controlprogram.OperatorBinding{Reference: "incident/observe-channel", Version: "1"}},
		{ID: "restart", Binding: &controlprogram.OperatorBinding{Reference: "incident/restart", Version: "1"}},
	}
	truth := true
	document.Transitions = []controlprogram.Transition{
		{ID: "observe-channel", Operator: "observe-channel", Guard: controlprogram.Predicate{True: &truth}, Target: fact("service", "healthy"), Priority: 1},
		{ID: "restart", Operator: "restart", Guard: fact("service", "healthy"), Target: fact("incident", "mitigated"), Priority: 10, Parameters: []controlprogram.TransitionParameterBinding{{
			Parameter: "channel", Producer: controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceReceipt, Transition: "observe-channel", Field: "channel"},
		}}},
	}
	mitigated := "mitigated"
	healthy := "healthy"
	resolver := invocationResolver{operators: map[string]controlprogram.ResolvedOperator{
		"incident/observe-channel": {
			Fingerprint: strings.Repeat("a", 64), Verifier: "healthcheck", ExecutionContext: "preserve",
			StateEffect: controlprogram.StateEffect{Kind: "assignments", Assignments: []controlprogram.StateAssignment{{Facet: "service", Value: &healthy}}},
			Outputs:     []controlprogram.OperatorOutput{{ID: "channel", Type: controlprogram.ValueTypeDefinition{Kind: "string"}}},
		},
		"incident/restart": {
			Fingerprint: strings.Repeat("b", 64), Capabilities: []string{"service.restart"}, Effects: []string{"service.restart"}, Verifier: "healthcheck", Recovery: "restart", ExecutionContext: "preserve",
			StateEffect: controlprogram.StateEffect{Kind: "assignments", Assignments: []controlprogram.StateAssignment{{Facet: "incident", Value: &mitigated}}},
			Parameters:  []controlprogram.OperatorParameter{{ID: "channel", Type: controlprogram.ValueTypeDefinition{Kind: "string"}, Required: true, AllowedSources: []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceReceipt}}},
		},
	}}
	if _, err := controlprogram.Compile(clone(t, document), resolver); err == nil || !strings.Contains(err.Error(), "receipt availability is not guaranteed by the trusted operator binding") {
		t.Fatalf("missing trusted receipt input result = %v", err)
	}
	consumer := resolver.operators["incident/restart"]
	consumer.ReceiptInputs = []controlprogram.OperatorReceiptInput{{Parameter: "channel", Transition: "observe-channel", Field: "channel", Guaranteed: true}}
	resolver.operators["incident/restart"] = consumer
	compiled, err := controlprogram.Compile(document, resolver)
	if err != nil {
		t.Fatal(err)
	}
	tampered := clone(t, compiled.Document)
	for index := range tampered.Operators {
		if tampered.Operators[index].ID == "restart" {
			tampered.Operators[index].ReceiptInputs[0].Field = "other"
		}
	}
	if _, err := controlprogram.Compile(tampered, resolver); err == nil || !strings.Contains(err.Error(), "compiled binding semantics drift") {
		t.Fatalf("receipt-input provenance drift result = %v", err)
	}
}

func TestInvocationCompletenessChecksEntryAndStateAvailabilityPerEntry(t *testing.T) {
	entryBound := parameterProgram()
	entryBound.Operators[0].Parameters[0].Authority = controlprogram.AuthorityRequirement{}
	entryBound.Entries[0].Inputs[0].Type = "string"
	entryBound.Operators[0].Parameters[0].AllowedSources = []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceEntryInput}
	entryBound.Transitions[0].Parameters[0].Producer = controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceEntryInput, Input: "incident"}
	if _, err := controlprogram.Compile(entryBound, nil); err != nil {
		t.Fatal(err)
	}
	missing := clone(t, entryBound)
	missing.Entries = append(missing.Entries, controlprogram.Entry{ID: "alternate", Target: "mitigated"})
	if _, err := controlprogram.Compile(missing, nil); err == nil || !strings.Contains(err.Error(), `reachable entry "alternate"`) {
		t.Fatalf("missing reachable input result = %v", err)
	}

	stateBound := parameterProgram()
	stateBound.Operators[0].Parameters[0].Authority = controlprogram.AuthorityRequirement{}
	stateBound.Operators[0].Parameters[0].AllowedSources = []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceState}
	truth := true
	alwaysAvailable := controlprogram.Predicate{True: &truth}
	stateBound.Transitions[0].Parameters[0].Producer = controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceState, Facet: "service", AvailableWhen: &alwaysAvailable}
	if _, err := controlprogram.Compile(stateBound, nil); err == nil || !strings.Contains(err.Error(), "does not prove the produced facet is known") {
		t.Fatalf("unproved state producer result = %v", err)
	}
	availability := fact("service", "degraded")
	stateBound.Transitions[0].Parameters[0].Producer = controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceState, Facet: "service", AvailableWhen: &availability}
	if _, err := controlprogram.Compile(stateBound, nil); err == nil || !strings.Contains(err.Error(), "state availability is not implied") {
		t.Fatalf("unimplied state producer result = %v", err)
	}
	stateBound.Transitions[0].Guard = controlprogram.Predicate{All: []controlprogram.Predicate{stateBound.Transitions[0].Guard, availability}}
	if _, err := controlprogram.Compile(stateBound, nil); err != nil {
		t.Fatal(err)
	}
}

func TestInvocationCompletenessProvesWorkOutputProducerPrecedence(t *testing.T) {
	// control-law: selected work-output consumers identify one completed producer
	base := incidentWorkProgram()
	consumerOperator := base.Operators[0]
	consumerOperator.ID = "dispatch"
	consumerOperator.Parameters = []controlprogram.OperatorParameter{{
		ID: "diagnosis", Type: controlprogram.ValueTypeDefinition{Kind: "string"}, Required: true,
		AllowedSources: []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceWorkOutput},
	}}
	base.Operators = append(base.Operators, consumerOperator)
	producerTarget := base.Transitions[0].Target
	base.Transitions = append(base.Transitions, controlprogram.Transition{
		ID: "dispatch", Operator: "dispatch", Priority: 20, Guard: producerTarget,
		Target: fact("incident", "mitigated"), Parameters: []controlprogram.TransitionParameterBinding{{
			Parameter: "diagnosis", Producer: controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceWorkOutput, Work: "diagnose", Output: "diagnosis"},
		}},
	})
	if _, err := controlprogram.Compile(base, nil); err != nil {
		t.Fatal(err)
	}

	ambiguous := clone(t, base)
	var duplicateProducer controlprogram.Transition
	for _, candidate := range ambiguous.Transitions {
		if candidate.Work == "diagnose" {
			duplicateProducer = candidate
			break
		}
	}
	duplicateProducer.ID = "diagnose-again"
	var duplicateOperator controlprogram.Operator
	for _, candidate := range ambiguous.Operators {
		if candidate.ID == duplicateProducer.Operator {
			duplicateOperator = candidate
			break
		}
	}
	duplicateOperator.ID = "diagnose-again"
	duplicateProducer.Operator = duplicateOperator.ID
	ambiguous.Operators = append(ambiguous.Operators, duplicateOperator)
	ambiguous.Transitions = append(ambiguous.Transitions, duplicateProducer)
	if _, err := controlprogram.Compile(ambiguous, nil); err == nil || !strings.Contains(err.Error(), "exactly one producer transition") {
		t.Fatalf("ambiguous work producer result = %v", err)
	}

	shadowed := clone(t, base)
	producerPriority := 0
	for _, candidate := range shadowed.Transitions {
		if candidate.Work == "diagnose" {
			producerPriority = candidate.Priority
		}
	}
	for index := range shadowed.Transitions {
		if shadowed.Transitions[index].ID == "dispatch" {
			shadowed.Transitions[index].Priority = producerPriority
		}
	}
	if _, err := controlprogram.Compile(shadowed, nil); err == nil || !strings.Contains(err.Error(), "not guaranteed before") {
		t.Fatalf("shadowed work producer result = %v", err)
	}

	unproved := clone(t, base)
	truth := true
	for index := range unproved.Transitions {
		if unproved.Transitions[index].ID == "dispatch" {
			unproved.Transitions[index].Guard = controlprogram.Predicate{True: &truth}
		}
	}
	if _, err := controlprogram.Compile(unproved, nil); err == nil || !strings.Contains(err.Error(), "not guaranteed before") {
		t.Fatalf("unproved work producer result = %v", err)
	}
}

func TestInvocationCompletenessRejectsResolverDriftCyclesAndValidatorOverride(t *testing.T) {
	fingerprintA, fingerprintB := strings.Repeat("a", 64), strings.Repeat("b", 64)
	resolver := invocationResolver{
		parameterResolvers: map[string]controlprogram.ResolvedParameterResolver{
			"incident/channel": {Fingerprint: fingerprintA, OutputType: controlprogram.ValueTypeDefinition{Kind: "string"}, SourceKind: controlprogram.ParameterSourceTrustedResolver, Authority: controlprogram.AuthorityRequirement{AnyOf: []string{"human"}}, Dependencies: []string{"channel"}, StabilityScope: "invocation"},
		},
		validators: map[string]controlprogram.ResolvedValueValidator{
			"incident/channel-validator": {Fingerprint: fingerprintB, Type: controlprogram.ValueTypeDefinition{Kind: "string"}},
		},
	}
	value := parameterProgram()
	value.Operators[0].Parameters[0].Authority = controlprogram.AuthorityRequirement{}
	value.Operators[0].Parameters[0].AllowedSources = []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceTrustedResolver}
	value.Transitions[0].Parameters[0].Producer = controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceTrustedResolver, Binding: &controlprogram.ParameterResolverBinding{Reference: "incident/channel", Version: "1"}}
	if _, err := controlprogram.Compile(value, resolver); err == nil || !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("resolver cycle result = %v", err)
	}
	resolver.parameterResolvers["incident/channel"] = controlprogram.ResolvedParameterResolver{Fingerprint: fingerprintA, OutputType: controlprogram.ValueTypeDefinition{Kind: "string"}, SourceKind: controlprogram.ParameterSourceTrustedResolver, Authority: controlprogram.AuthorityRequirement{AnyOf: []string{"human"}}, StabilityScope: "invocation"}
	compiled, err := controlprogram.Compile(value, resolver)
	if err != nil {
		t.Fatal(err)
	}
	compiled.Document.Transitions[0].Parameters[0].Producer.Binding.Fingerprint = fingerprintB
	if _, err := controlprogram.Compile(compiled.Document, resolver); err == nil || !strings.Contains(err.Error(), "fingerprint drift") {
		t.Fatalf("resolver drift result = %v", err)
	}

	validated := parameterProgram()
	validated.Operators[0].Parameters[0].Type.Validator = &controlprogram.TrustedValidatorBinding{Reference: "incident/channel-validator", Version: "1"}
	compiled, err = controlprogram.Compile(validated, resolver)
	if err != nil {
		t.Fatal(err)
	}
	compiled.Document.Operators[0].Parameters[0].Type.Validator.Fingerprint = fingerprintA
	if _, err := controlprogram.Compile(compiled.Document, resolver); err == nil || !strings.Contains(err.Error(), "fingerprint drift") {
		t.Fatalf("validator override result = %v", err)
	}
}

func fact(facet, value string) controlprogram.Predicate {
	return controlprogram.Predicate{Fact: &controlprogram.FactPredicate{Facet: facet, Statuses: []string{"known"}, Values: []string{value}}}
}

func clone(t *testing.T, value controlprogram.Document) controlprogram.Document {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result controlprogram.Document
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestDomainNeutralIncidentProgramCompiles(t *testing.T) {
	// control-law: generic-control-program-ir-has-no-software-delivery-dependency
	compiled, err := controlprogram.Compile(incidentProgram(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Fingerprint) != 64 || !bytes.Contains(compiled.Canonical, []byte(`"incident-response"`)) {
		t.Fatalf("compiled incident program = %#v", compiled)
	}
	if compiled.Document.Description == "" || compiled.Document.Program.Description == "" ||
		compiled.Document.Facets[1].Description == "" || compiled.Document.Evidence[0].Description == "" ||
		compiled.Document.Operators[0].Description == "" || compiled.Document.Transitions[0].Description == "" ||
		compiled.Document.Targets[0].Description == "" || compiled.Document.Entries[0].Description == "" {
		t.Fatalf("compilation removed declared descriptions: %#v", compiled.Document)
	}
}

func incidentWorkProgram() controlprogram.Document {
	document := incidentProgram()
	instructions := "Inspect the incident and produce the declared diagnosis."
	digest := sha256.Sum256([]byte(instructions))
	document.Work = []controlprogram.WorkContract{{
		ID: "diagnose", Instructions: controlprogram.WorkAsset{Path: "instructions.md", SHA256: hex.EncodeToString(digest[:]), Content: instructions},
		Inputs:      []controlprogram.WorkInput{{ID: "incident", EntryInput: "incident"}},
		Outputs:     []controlprogram.WorkOutput{{ID: "diagnosis", Path: "diagnosis.md", MediaType: "text/markdown", Required: true, MaxBytes: 4096}},
		Description: "presentation only",
	}}
	document.Transitions[0].Work = "diagnose"
	return document
}

func TestForegroundWorkIsDomainNeutralAndFingerprintBound(t *testing.T) {
	// control-law: exact foreground-work semantics contribute to canonical program identity
	base, err := controlprogram.Compile(incidentWorkProgram(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(base.Document.Work) != 1 || base.Document.Transitions[0].Work != "diagnose" {
		t.Fatalf("compiled work = %#v", base.Document.Work)
	}
	description := incidentWorkProgram()
	description.Work[0].Description = "different prose"
	equivalent, err := controlprogram.Compile(description, nil)
	if err != nil || equivalent.Fingerprint != base.Fingerprint {
		t.Fatalf("work description changed executable identity: %v %s != %s", err, equivalent.Fingerprint, base.Fingerprint)
	}
	changed := incidentWorkProgram()
	changed.Work[0].Instructions.Content += " Verify recovery."
	digest := sha256.Sum256([]byte(changed.Work[0].Instructions.Content))
	changed.Work[0].Instructions.SHA256 = hex.EncodeToString(digest[:])
	semantic, err := controlprogram.Compile(changed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if semantic.Fingerprint == base.Fingerprint {
		t.Fatal("instruction asset change preserved executable identity")
	}
}

func TestForegroundWorkRejectsUnboundAssetsInputsAndTransitions(t *testing.T) {
	// control-law: every foreground-work dependency is declared and exactly referenced
	for name, mutate := range map[string]func(*controlprogram.Document){
		"unresolved-asset": func(value *controlprogram.Document) {
			value.Work[0].Instructions.Content, value.Work[0].Instructions.SHA256 = "", ""
		},
		"unknown-entry-input": func(value *controlprogram.Document) { value.Work[0].Inputs[0].EntryInput = "missing" },
		"unreferenced-work":   func(value *controlprogram.Document) { value.Transitions[0].Work = "" },
		"unknown-work":        func(value *controlprogram.Document) { value.Transitions[0].Work = "missing" },
	} {
		t.Run(name, func(t *testing.T) {
			document := incidentWorkProgram()
			mutate(&document)
			if _, err := controlprogram.Compile(document, nil); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}
}

func TestCanonicalFingerprintIgnoresOrderingAndDescriptions(t *testing.T) {
	// control-law: canonical-program-identity-binds-executable-semantics-only
	base, err := controlprogram.Compile(incidentProgram(), nil)
	if err != nil {
		t.Fatal(err)
	}
	equivalent := clone(t, incidentProgram())
	equivalent.Description = "different prose"
	equivalent.Program.Description = "different program prose"
	equivalent.Transitions[0].Description = "different transition prose"
	equivalent.Facets[0], equivalent.Facets[1] = equivalent.Facets[1], equivalent.Facets[0]
	equivalent.Entries[0].Inputs[0].Config = json.RawMessage(`{"a":1,"b":2}`)
	equivalent.Declarations.Capabilities = append([]string(nil), equivalent.Declarations.Capabilities...)
	other, err := controlprogram.Compile(equivalent, nil)
	if err != nil {
		t.Fatal(err)
	}
	if other.Fingerprint != base.Fingerprint {
		t.Fatalf("equivalent fingerprints differ: %s != %s", other.Fingerprint, base.Fingerprint)
	}
	changed := clone(t, incidentProgram())
	changed.Transitions[0].Priority++
	semantic, err := controlprogram.Compile(changed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if semantic.Fingerprint == base.Fingerprint {
		t.Fatal("executable change preserved fingerprint")
	}
}

func TestEntryDiagnosticsAreArtifactBoundButNotExecutableControlLaw(t *testing.T) {
	document := incidentProgram()
	without, err := controlprogram.Compile(document, nil)
	if err != nil {
		t.Fatal(err)
	}
	document.Entries[0].Diagnostics = &controlprogram.EntryDiagnostics{ExplainOnSuspend: true}
	with, err := controlprogram.Compile(document, nil)
	if err != nil {
		t.Fatal(err)
	}
	if with.Fingerprint != without.Fingerprint {
		t.Fatalf("presentation preference changed executable fingerprint: %s != %s", with.Fingerprint, without.Fingerprint)
	}
	if bytes.Equal(with.Canonical, without.Canonical) || !bytes.Contains(with.Canonical, []byte(`"explain_on_suspend": true`)) {
		t.Fatal("diagnostic preference is not preserved in the artifact projection")
	}
}

func TestEntryAuthorityRequirementsAreCanonicalExecutableSemantics(t *testing.T) {
	document := incidentProgram()
	document.Declarations.Authorities = append(document.Declarations.Authorities, "human", "autonomy")
	document.Entries[0].Requires.Authorities = []string{"human", "autonomy"}
	compiled, err := controlprogram.Compile(document, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := compiled.Document.Entries[0].Requires.Authorities; !reflect.DeepEqual(got, []string{"autonomy", "human"}) {
		t.Fatalf("canonical entry authorities = %v", got)
	}
	without := clone(t, document)
	without.Entries[0].Requires.Authorities = nil
	unprotected, err := controlprogram.Compile(without, nil)
	if err != nil {
		t.Fatal(err)
	}
	if unprotected.Fingerprint == compiled.Fingerprint {
		t.Fatal("entry activation requirement did not change ProgramFingerprint")
	}
	duplicate := clone(t, document)
	duplicate.Entries[0].Requires.Authorities = []string{"human", "human"}
	if _, err := controlprogram.Compile(duplicate, nil); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate entry authority result = %v", err)
	}
	undeclared := clone(t, document)
	undeclared.Entries[0].Requires.Authorities = []string{"release-manager"}
	if _, err := controlprogram.Compile(undeclared, nil); err == nil || !strings.Contains(err.Error(), "undeclared") {
		t.Fatalf("undeclared entry authority result = %v", err)
	}
}

func TestDelegationBindingIsResolvedAndFingerprintBound(t *testing.T) {
	// control-law: repository-source-can-request-but-cannot-grant-authority
	document := incidentProgram()
	document.Entries[0].Delegation = &controlprogram.DelegationBinding{Reference: "incident/delegation/autonomy", Version: "1"}
	resolver := delegationResolver{fingerprint: strings.Repeat("d", 64), authorities: []string{"autonomy"}, delegable: true}
	compiled, err := controlprogram.Compile(document, resolver)
	if err != nil {
		t.Fatal(err)
	}
	binding := compiled.Document.Entries[0].Delegation
	if binding.Fingerprint != resolver.fingerprint || len(binding.Authorities) != 1 || binding.Authorities[0] != "autonomy" {
		t.Fatalf("resolved delegation = %#v", binding)
	}
	if _, err := controlprogram.Compile(compiled.Document, delegationResolver{fingerprint: strings.Repeat("e", 64), authorities: []string{"autonomy"}, delegable: true}); err == nil || !strings.Contains(err.Error(), "drift") {
		t.Fatalf("delegation drift result = %v", err)
	}
	if _, err := controlprogram.Compile(document, delegationResolver{fingerprint: strings.Repeat("d", 64), authorities: []string{"autonomy"}, delegable: false}); err == nil || !strings.Contains(err.Error(), "not delegable") {
		t.Fatalf("nondelegable result = %v", err)
	}
}

func TestAuthorityAlgebraAndRepositoryStrengtheningAreExecutableSemantics(t *testing.T) {
	// control-law: alternatives-and-mandatory-authority-never-flatten
	base := incidentProgram()
	base.Declarations.Authorities = []string{"autonomy", "external-provider", "human", "incident-commander"}
	base.Operators[0].Authority = controlprogram.AuthorityRequirement{AnyOf: []string{"human", "autonomy"}, AllOf: []string{"external-provider"}}
	base.Transitions[0].Requires.Authorities = []string{"incident-commander"}
	compiled, err := controlprogram.Compile(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Document.Operators[0].Authority.AnyOf) != 2 || len(compiled.Document.Operators[0].Authority.AllOf) != 1 || len(compiled.Document.Transitions[0].Requires.Authorities) != 1 {
		t.Fatalf("authority semantics = %#v %#v", compiled.Document.Operators[0].Authority, compiled.Document.Transitions[0].Requires)
	}
	changed := clone(t, base)
	changed.Transitions[0].Requires.Authorities = nil
	withoutStrengthening, err := controlprogram.Compile(changed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Fingerprint == withoutStrengthening.Fingerprint {
		t.Fatal("repository authority strengthening did not change executable fingerprint")
	}
}

func TestOnlyTrustedBindingsMayAdvanceExecutionContext(t *testing.T) {
	document := incidentProgram()
	document.Operators[0].ExecutionContext = "advance"
	if _, err := controlprogram.Compile(document, nil); err == nil || !strings.Contains(err.Error(), "trusted bindings") {
		t.Fatalf("untrusted execution advance result = %v", err)
	}
}

func TestStrictLoaderRejectsUnknownAndDuplicateFields(t *testing.T) {
	// control-law: only-the-closed-ir-schema-crosses-the-compiler-boundary
	valid, _ := json.Marshal(incidentProgram())
	unknown := bytes.Replace(valid, []byte(`"schema"`), []byte(`"unknown":true,"schema"`), 1)
	if _, err := controlprogram.Load(bytes.NewReader(unknown), nil); err == nil {
		t.Fatal("unknown field was accepted")
	}
	duplicate := bytes.Replace(valid, []byte(`"schema"`), []byte(`"schema":"control-program","schema"`), 1)
	if _, err := controlprogram.Load(bytes.NewReader(duplicate), nil); err == nil {
		t.Fatal("duplicate field was accepted")
	}
}

func TestGenerationLabelledControlProgramAndArtifactFormatsAreRejected(t *testing.T) {
	legacyProgram := []byte(`{"schema_version":"control-program/v1"}`)
	if _, err := controlprogram.Load(bytes.NewReader(legacyProgram), nil); err == nil {
		t.Fatal("generation-labelled Control Program format was accepted")
	}
	legacyArtifact := []byte(`{"schema_version":1,"compiler_version":"control-program/v1.compiler.1"}`)
	if _, err := controlprogram.LoadArtifact(bytes.NewReader(legacyArtifact)); err == nil {
		t.Fatal("generation-labelled artifact format was accepted")
	}
}

func TestStrictLoadersRejectOversizedTrailingInput(t *testing.T) {
	// control-law: size-limited-loaders-never-treat-truncation-as-eof
	documentRaw, err := json.Marshal(incidentProgram())
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := controlprogram.Compile(incidentProgram(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, artifactRaw, err := controlprogram.NewArtifact(compiled, controlprogram.ArtifactInput{
		CompilerVersion: "compiler-1", SourcePath: "flow.ts", Source: []byte("source"),
		DependencyLockPath: "package-lock.json", DependencyLock: []byte("lock"), Projections: []hostprojection.ID{}, GeneratedProjections: map[string][]byte{},
	})
	if err != nil {
		t.Fatal(err)
	}
	oversized := func(raw []byte, limit int) []byte {
		if len(raw) >= limit {
			t.Fatalf("fixture length %d exceeds limit %d", len(raw), limit)
		}
		result := append([]byte(nil), raw...)
		result = append(result, bytes.Repeat([]byte(" "), limit-len(result))...)
		return append(result, 'x')
	}
	if _, err := controlprogram.Load(bytes.NewReader(oversized(documentRaw, 16<<20)), nil); err == nil || !strings.Contains(err.Error(), "exceeds 16 MiB") {
		t.Fatalf("oversized IR result = %v", err)
	}
	if _, err := controlprogram.LoadArtifact(bytes.NewReader(oversized(artifactRaw, 32<<20))); err == nil || !strings.Contains(err.Error(), "exceeds 32 MiB") {
		t.Fatalf("oversized artifact result = %v", err)
	}
}

func TestCompilerRejectsUndeclaredEffectAndMissingRecovery(t *testing.T) {
	for name, mutate := range map[string]func(*controlprogram.Document){
		"undeclared-effect": func(value *controlprogram.Document) { value.Operators[0].Effects = []string{"undeclared"} },
		"missing-recovery":  func(value *controlprogram.Document) { value.Operators[0].Recovery = "" },
		"invalid-reference": func(value *controlprogram.Document) { value.Transitions[0].Operator = "missing" },
		"recovery-gap":      func(value *controlprogram.Document) { value.Operators[0].Recovery = "missing" },
	} {
		t.Run(name, func(t *testing.T) {
			value := incidentProgram()
			mutate(&value)
			if _, err := controlprogram.Compile(value, nil); err == nil {
				t.Fatal("invalid program compiled")
			}
		})
	}
}

func TestArtifactBindsSourceLockProjectionsAndCompiler(t *testing.T) {
	// control-law: runtime-admits-only-an-exact-source-lock-artifact-projection
	repository := t.TempDir()
	sourcePath, lockPath, skillPath := "flow.ts", "package-lock.json", ".agents/skills/respond/SKILL.md"
	source, lock, skill := []byte("source"), []byte("lock"), []byte("skill")
	for path, content := range map[string][]byte{sourcePath: source, lockPath: lock, skillPath: skill} {
		absolute := filepath.Join(repository, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	compiled, err := controlprogram.Compile(incidentProgram(), nil)
	if err != nil {
		t.Fatal(err)
	}
	artifact, _, err := controlprogram.NewArtifact(compiled, controlprogram.ArtifactInput{
		CompilerVersion: "compiler-1", SourcePath: sourcePath, Source: source, DependencyLockPath: lockPath, DependencyLock: lock,
		Projections: []hostprojection.ID{hostprojection.Codex}, GeneratedProjections: map[string][]byte{skillPath: skill},
	})
	if err != nil {
		t.Fatal(err)
	}
	generate := func(controlprogram.Compiled, []hostprojection.ID) (map[string][]byte, error) {
		return map[string][]byte{skillPath: skill}, nil
	}
	if _, err := controlprogram.CheckArtifact(repository, artifact, "compiler-1", nil, []hostprojection.ID{hostprojection.Codex}, generate); err != nil {
		t.Fatal(err)
	}
	forgedSkill := []byte("forged skill")
	forgedArtifact, _, err := controlprogram.NewArtifact(compiled, controlprogram.ArtifactInput{
		CompilerVersion: "compiler-1", SourcePath: sourcePath, Source: source, DependencyLockPath: lockPath, DependencyLock: lock,
		Projections: []hostprojection.ID{hostprojection.Codex}, GeneratedProjections: map[string][]byte{skillPath: forgedSkill},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, filepath.FromSlash(skillPath)), forgedSkill, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := controlprogram.CheckArtifact(repository, forgedArtifact, "compiler-1", nil, []hostprojection.ID{hostprojection.Codex}, generate); err == nil || !strings.Contains(err.Error(), "derived from compiled program") {
		t.Fatalf("self-consistent forged projection result = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repository, filepath.FromSlash(skillPath)), skill, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, sourcePath), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := controlprogram.CheckArtifact(repository, artifact, "compiler-1", nil, []hostprojection.ID{hostprojection.Codex}, generate); err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("stale source result = %v", err)
	}
}

func TestProjectionSelectionChangesArtifactEnvelopeButNotControlProgram(t *testing.T) {
	// control-law: generated-file-selection-is-nonsemantic-to-the-control-program
	compiled, err := controlprogram.Compile(incidentProgram(), nil)
	if err != nil {
		t.Fatal(err)
	}
	base := controlprogram.ArtifactInput{
		CompilerVersion: "compiler-1", SourcePath: "flow.ts", Source: []byte("same source"),
		DependencyLockPath: "package-lock.json", DependencyLock: []byte("same lock"),
	}
	codex := base
	codex.Projections = []hostprojection.ID{hostprojection.Codex}
	codex.GeneratedProjections = map[string][]byte{".agents/skills/respond/SKILL.md": []byte("codex")}
	all := base
	all.Projections = hostprojection.CanonicalIDs()
	all.GeneratedProjections = map[string][]byte{
		".agents/skills/respond/SKILL.md": []byte("codex"),
		".claude/skills/respond/SKILL.md": []byte("claude"),
		".cursor/commands/respond.md":     []byte("cursor"),
		".gemini/skills/respond/SKILL.md": []byte("gemini"),
	}
	codexArtifact, codexRaw, err := controlprogram.NewArtifact(compiled, codex)
	if err != nil {
		t.Fatal(err)
	}
	allArtifact, allRaw, err := controlprogram.NewArtifact(compiled, all)
	if err != nil {
		t.Fatal(err)
	}
	if codexArtifact.ProgramFingerprint != compiled.Fingerprint || allArtifact.ProgramFingerprint != compiled.Fingerprint {
		t.Fatal("projection selection changed the Control Program fingerprint")
	}
	if codexArtifact.ProjectionSelectionFingerprint == allArtifact.ProjectionSelectionFingerprint {
		t.Fatal("different projection memberships shared a selection fingerprint")
	}
	codexEnvelope, allEnvelope := sha256.Sum256(codexRaw), sha256.Sum256(allRaw)
	if codexEnvelope == allEnvelope {
		t.Fatal("different projection memberships shared an artifact-envelope fingerprint")
	}
}

func TestArtifactCheckCanonicalizesExpectedProjectionOrder(t *testing.T) {
	repository := t.TempDir()
	files := map[string][]byte{
		"flow.ts":                         []byte("source"),
		"package-lock.json":               []byte("lock"),
		".agents/skills/respond/SKILL.md": []byte("codex"),
		".claude/skills/respond/SKILL.md": []byte("claude"),
	}
	for path, raw := range files {
		absolute := filepath.Join(repository, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	compiled, err := controlprogram.Compile(incidentProgram(), nil)
	if err != nil {
		t.Fatal(err)
	}
	generated := map[string][]byte{
		".agents/skills/respond/SKILL.md": files[".agents/skills/respond/SKILL.md"],
		".claude/skills/respond/SKILL.md": files[".claude/skills/respond/SKILL.md"],
	}
	artifact, _, err := controlprogram.NewArtifact(compiled, controlprogram.ArtifactInput{
		CompilerVersion: "compiler-1", SourcePath: "flow.ts", Source: files["flow.ts"],
		DependencyLockPath: "package-lock.json", DependencyLock: files["package-lock.json"],
		Projections: []hostprojection.ID{hostprojection.Codex, hostprojection.Claude}, GeneratedProjections: generated,
	})
	if err != nil {
		t.Fatal(err)
	}
	generate := func(_ controlprogram.Compiled, projections []hostprojection.ID) (map[string][]byte, error) {
		if want := []hostprojection.ID{hostprojection.Claude, hostprojection.Codex}; !reflect.DeepEqual(projections, want) {
			t.Fatalf("generator projections = %v, want canonical %v", projections, want)
		}
		return generated, nil
	}
	if _, err := controlprogram.CheckArtifact(repository, artifact, "compiler-1", nil, []hostprojection.ID{hostprojection.Codex, hostprojection.Claude}, generate); err != nil {
		t.Fatal(err)
	}
}

func TestArtifactRejectsGeneratedPathsOutsideHostProjectionRoots(t *testing.T) {
	compiled, err := controlprogram.Compile(incidentProgram(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = controlprogram.NewArtifact(compiled, controlprogram.ArtifactInput{
		CompilerVersion: "compiler-1", SourcePath: "flow.ts", Source: []byte("source"),
		DependencyLockPath: "package-lock.json", DependencyLock: []byte("lock"),
		Projections: []hostprojection.ID{hostprojection.Codex}, GeneratedProjections: map[string][]byte{"README.md": []byte("delete me")},
	})
	if err == nil {
		t.Fatal("artifact accepted an arbitrary generated deletion path")
	}
}

func TestArtifactBindsExactForegroundWorkAssets(t *testing.T) {
	// control-law: runtime admits only the instruction and schema assets compiled into the artifact
	repository := t.TempDir()
	document := incidentWorkProgram()
	files := map[string][]byte{
		"flow.ts":           []byte("source"),
		"package-lock.json": []byte("lock"),
		"instructions.md":   []byte(document.Work[0].Instructions.Content),
	}
	for path, raw := range files {
		if err := os.WriteFile(filepath.Join(repository, path), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	compiled, err := controlprogram.Compile(document, nil)
	if err != nil {
		t.Fatal(err)
	}
	artifact, _, err := controlprogram.NewArtifact(compiled, controlprogram.ArtifactInput{
		CompilerVersion: "compiler-1", SourcePath: "flow.ts", Source: files["flow.ts"],
		DependencyLockPath: "package-lock.json", DependencyLock: files["package-lock.json"], Projections: []hostprojection.ID{}, GeneratedProjections: map[string][]byte{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controlprogram.CheckArtifact(repository, artifact, "compiler-1", nil, []hostprojection.ID{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "instructions.md"), []byte("different instructions"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := controlprogram.CheckArtifact(repository, artifact, "compiler-1", nil, []hostprojection.ID{}, nil); err == nil || !strings.Contains(err.Error(), "work asset") {
		t.Fatalf("changed work asset result = %v", err)
	}
}
