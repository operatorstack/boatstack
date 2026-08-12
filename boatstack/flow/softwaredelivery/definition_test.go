package softwaredelivery_test

import (
	"context"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	"github.com/operatorstack/boatstack/boatstack/core"
	"github.com/operatorstack/boatstack/boatstack/delivery"
	softwareflow "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery"
)

func compiledFlow(t *testing.T, guard controlprogram.Predicate) (controlprogram.Compiled, softwareflow.Resolver) {
	t.Helper()
	resolver, err := softwareflow.NewResolver(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	truth := true
	document := controlprogram.Document{
		SchemaVersion: controlprogram.SchemaVersion,
		Program:       controlprogram.Program{ID: "product-delivery", Version: "1"},
		Facets: []controlprogram.Facet{
			{ID: "publication", Kind: "string"}, {ID: "verification", Kind: "string"},
			{ID: "configuration", Kind: "string"}, {ID: "runtime", Kind: "string"},
		},
		Operators:   []controlprogram.Operator{{ID: "publication.observe", Binding: &controlprogram.OperatorBinding{Reference: "software-delivery/publication.observe", Version: "1"}}},
		Transitions: []controlprogram.Transition{{ID: "publication.observe", Operator: "publication.observe", Guard: guard, Target: controlprogram.Predicate{True: &truth}, Priority: 77}},
		Targets: []controlprogram.Target{{ID: "published-pr", Predicate: controlprogram.Predicate{All: []controlprogram.Predicate{
			fact("verification", "current"), fact("configuration", "verified"), fact("runtime", "verified"), fact("publication", "open"),
		}}}},
		Entries: []controlprogram.Entry{{ID: "run", Target: "published-pr"}},
	}
	compiled, err := controlprogram.Compile(document, resolver)
	if err != nil {
		t.Fatal(err)
	}
	return compiled, resolver
}

func fact(facet, value string) controlprogram.Predicate {
	return controlprogram.Predicate{Fact: &controlprogram.FactPredicate{Facet: facet, Statuses: []string{"known"}, Values: []string{value}}}
}

func TestTrustedFlowLowersThroughStandardStateEffectBoundary(t *testing.T) {
	// control-law: repository-flow-selects-trusted-semantics-without-redeclaring-native-effects
	truth := true
	compiled, resolver := compiledFlow(t, controlprogram.Predicate{True: &truth})
	definition, err := softwareflow.NewDefinition(compiled, resolver)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := definition.RuntimeManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ID != "boatstack.standard" || len(manifest.Transitions) != 1 || manifest.Transitions[0].StateEffect.NativeHandler != "publication-observe" {
		t.Fatalf("lowered manifest = %#v", manifest)
	}
	program, err := delivery.Compile(context.Background(), delivery.CompileRequest{KernelVersion: "v2.0.0", Core: core.System(), Runtime: definition, Settings: map[string]string{"repo": "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	if program.Summary().RuntimeTransitionCount != 1 {
		t.Fatalf("runtime transition count = %d", program.Summary().RuntimeTransitionCount)
	}
}

func TestRepositoryGuardCanOnlyStrengthenTrustedBinding(t *testing.T) {
	compiled, resolver := compiledFlow(t, fact("publication", "candidate"))
	definition, err := softwareflow.NewDefinition(compiled, resolver)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := definition.RuntimeManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Transitions[0].SourceConditions) < 2 {
		t.Fatal("strengthened condition was not appended")
	}
	invalid := compiled.Document
	invalid.Transitions[0].Guard = controlprogram.Predicate{Any: []controlprogram.Predicate{fact("publication", "candidate"), fact("publication", "open")}}
	nonConjunctive, err := controlprogram.Compile(invalid, resolver)
	if err != nil {
		t.Fatal(err)
	}
	definition, err = softwareflow.NewDefinition(nonConjunctive, resolver)
	if err == nil {
		_, err = definition.RuntimeManifest(context.Background())
	}
	if err == nil || !strings.Contains(err.Error(), "does not strengthen") {
		t.Fatalf("non-conjunctive guard result = %v", err)
	}
}

func TestCompiledBindingDriftFailsClosed(t *testing.T) {
	truth := true
	compiled, resolver := compiledFlow(t, controlprogram.Predicate{True: &truth})
	drifted := compiled.Document
	drifted.Operators[0].Binding.Fingerprint = strings.Repeat("a", 64)
	if _, err := controlprogram.Compile(drifted, resolver); err == nil || !strings.Contains(err.Error(), "drift") {
		t.Fatalf("binding drift result = %v", err)
	}
	capabilityOverride := compiled.Document
	capabilityOverride.Operators[0].Capabilities = append(capabilityOverride.Operators[0].Capabilities, "merge")
	if _, err := controlprogram.Compile(capabilityOverride, resolver); err == nil {
		t.Fatal("capability escalation was accepted")
	}
	effectOverride := compiled.Document
	tampered := *effectOverride.Operators[0].StateEffect
	tampered.NativeHandler = "incompatible-owner"
	effectOverride.Operators[0].StateEffect = &tampered
	if _, err := controlprogram.Compile(effectOverride, resolver); err == nil {
		t.Fatal("trusted state-effect ownership override was accepted")
	}
}
