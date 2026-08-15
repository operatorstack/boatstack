package softwaredelivery_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	"github.com/operatorstack/boatstack/boatstack/core"
	"github.com/operatorstack/boatstack/boatstack/delivery"
	softwareflow "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

func compiledFlow(t *testing.T, guard controlprogram.Predicate) (controlprogram.Compiled, softwareflow.Resolver) {
	t.Helper()
	resolver, err := softwareflow.NewResolver(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	truth := true
	document := controlprogram.Document{
		Schema: controlprogram.SchemaName, SchemaRevision: controlprogram.SchemaRevision,
		Program: controlprogram.Program{ID: "product-delivery", Version: "1"},
		Facets: []controlprogram.Facet{
			{ID: "publication", Kind: "string"}, {ID: "verification", Kind: "string"},
			{ID: "configuration", Kind: "string"}, {ID: "runtime", Kind: "string"},
		},
		Operators:   []controlprogram.Operator{{ID: "publication.observe", Binding: &controlprogram.OperatorBinding{Reference: "software-delivery/publication.observe", Version: "1"}}},
		Transitions: []controlprogram.Transition{{ID: "publication.observe", Operator: "publication.observe", Guard: guard, Target: controlprogram.Predicate{True: &truth}, Priority: 77, Parameters: hostParameters("publication_id")}},
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

func hostParameters(ids ...string) []controlprogram.TransitionParameterBinding {
	result := make([]controlprogram.TransitionParameterBinding, 0, len(ids))
	for _, id := range ids {
		result = append(result, controlprogram.TransitionParameterBinding{Parameter: id, Producer: controlprogram.ParameterProducer{
			Kind:    controlprogram.ParameterSourceHostInput,
			Request: &controlprogram.HostInputRequest{ID: id, Description: "Provide " + id + ".", Scope: "transition"},
		}})
	}
	return result
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
	objective := model.Objective{ID: "objective", TargetID: "published-pr", TrustedClass: model.ObjectiveOpenPR, DeliveryID: "delivery"}
	bootstrap, ok := program.RuntimeRegistry().Lookup("objective.bind")
	if !ok || !bootstrap.SupportsObjective(objective) || !program.RuntimeObjectiveContracts().Accepts(objective) {
		t.Fatalf("repository target lost trusted bootstrap correlation: transition=%#v found=%v", bootstrap, ok)
	}
	spoofed := objective
	spoofed.TrustedClass = model.ObjectiveApprovedPlan
	if program.RuntimeObjectiveContracts().Accepts(spoofed) {
		t.Fatal("repository target accepted a caller-supplied trusted class")
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

func TestRepositoryAuthorityRequirementIsConjunctive(t *testing.T) {
	// control-law: repository-policy-can-add-a-gate-but-cannot-create-an-authority-alternative
	truth := true
	compiled, resolver := compiledFlow(t, controlprogram.Predicate{True: &truth})
	document := compiled.Document
	document.Declarations.Authorities = append(document.Declarations.Authorities, "human")
	document.Transitions[0].Requires.Authorities = []string{"human"}
	strengthened, err := controlprogram.Compile(document, resolver)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := softwareflow.NewDefinition(strengthened, resolver)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := definition.RuntimeManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	transition := manifest.Transitions[0]
	if len(transition.AuthorityAll) != 1 || transition.AuthorityAll[0] != delivery.AuthorityHuman {
		t.Fatalf("mandatory authority = %#v; alternatives = %#v", transition.AuthorityAll, transition.Authority)
	}
}

func TestPublicationBindingPreservesProviderAsMandatory(t *testing.T) {
	resolver, err := softwareflow.NewResolver(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolver.ResolveOperator("software-delivery/publication.execute", "1")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(resolved.Authority.AnyOf, "human") || !contains(resolved.Authority.AnyOf, "autonomy") || !contains(resolved.Authority.AllOf, "external-provider") {
		t.Fatalf("publication authority = %#v", resolved.Authority)
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func TestRepositoryTargetMustBeImpliedByTrustedPostcondition(t *testing.T) {
	truth := true
	compiled, resolver := compiledFlow(t, controlprogram.Predicate{True: &truth})
	tampered := compiled.Document
	tampered.Transitions[0].Target = fact("configuration", "unverified")
	unsafe, err := controlprogram.Compile(tampered, resolver)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := softwareflow.NewDefinition(unsafe, resolver)
	if err == nil {
		_, err = definition.RuntimeManifest(context.Background())
	}
	if err == nil || !strings.Contains(err.Error(), "target exceeds the trusted binding") {
		t.Fatalf("unestablishable repository target result = %v", err)
	}
}

func TestRepositoryTargetIdentityAndStrengtheningBecomeRuntimeContract(t *testing.T) {
	// control-law: repository-marked-target-is-the-runtime-terminal-contract
	truth := true
	compiled, resolver := compiledFlow(t, controlprogram.Predicate{True: &truth})
	document := compiled.Document
	document.Facets = append(document.Facets, controlprogram.Facet{ID: "release-policy", Kind: "string"})
	document.Targets[0].ID = "release-ready"
	document.Targets[0].Predicate.All = append(document.Targets[0].Predicate.All, fact("release-policy", "satisfied"))
	document.Entries[0].ID, document.Entries[0].Target = "deliver", "release-ready"
	repositoryOwned, err := controlprogram.Compile(document, resolver)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := softwareflow.NewDefinition(repositoryOwned, resolver)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := definition.RuntimeManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.SupportedTargets) != 1 || manifest.SupportedTargets[0] != "release-ready" {
		t.Fatalf("runtime targets = %#v", manifest.SupportedTargets)
	}
	if len(manifest.ObjectiveContracts) != 1 || manifest.ObjectiveContracts[0].TargetID != "release-ready" || len(manifest.ObjectiveContracts[0].Conditions) != 5 {
		t.Fatalf("runtime terminal contracts = %#v", manifest.ObjectiveContracts)
	}
	if len(manifest.Transitions) != 1 || len(manifest.Transitions[0].TargetIDs) != 1 || manifest.Transitions[0].TargetIDs[0] != "release-ready" {
		t.Fatalf("target-conditioned transition = %#v", manifest.Transitions)
	}
}

func TestRepositoryTargetCannotWeakenTrustedTerminalLaw(t *testing.T) {
	// control-law: repository-targets-may-strengthen-but-never-weaken-domain-safety
	truth := true
	compiled, resolver := compiledFlow(t, controlprogram.Predicate{True: &truth})
	document := compiled.Document
	document.Targets[0].Predicate = fact("publication", "open")
	weakened, err := controlprogram.Compile(document, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = softwareflow.NewDefinition(weakened, resolver); err == nil || !strings.Contains(err.Error(), "strengthen exactly one trusted") {
		t.Fatalf("weakened target result = %v", err)
	}
}

func TestRepositoryTransitionMustMatchTrustedBindingIdentity(t *testing.T) {
	truth := true
	compiled, resolver := compiledFlow(t, controlprogram.Predicate{True: &truth})
	aliased := compiled.Document
	aliased.Transitions[0].ID = "observe-alias"
	unsafe, err := controlprogram.Compile(aliased, resolver)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := softwareflow.NewDefinition(unsafe, resolver)
	if err == nil {
		_, err = definition.RuntimeManifest(context.Background())
	}
	if err == nil || !strings.Contains(err.Error(), "does not match trusted binding identity") {
		t.Fatalf("transition alias result = %v", err)
	}
}

func TestForegroundWorkInputsMustExistOnEveryReachableEntry(t *testing.T) {
	// control-law: a selected entry cannot reach work whose inputs it cannot bind
	truth := true
	compiled, resolver := compiledFlow(t, controlprogram.Predicate{True: &truth})
	document := compiled.Document
	document.Entries = []controlprogram.Entry{
		{ID: "run", Target: "published-pr", Inputs: []controlprogram.EntryInput{{ID: "plan", Type: "markdown-file"}}},
		{ID: "retry", Target: "published-pr"},
	}
	instructions := "Inspect the exact repository plan."
	digest := sha256.Sum256([]byte(instructions))
	document.Work = []controlprogram.WorkContract{{
		ID: "planning", Instructions: controlprogram.WorkAsset{Path: "planning.md", SHA256: hex.EncodeToString(digest[:]), Content: instructions},
		Inputs:  []controlprogram.WorkInput{{ID: "plan", EntryInput: "plan"}},
		Outputs: []controlprogram.WorkOutput{{ID: "result", Path: "result.md", MediaType: "text/markdown", Required: true}},
	}}
	document.Transitions[0].Work = "planning"
	unsafe, err := controlprogram.Compile(document, resolver)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := softwareflow.NewDefinition(unsafe, resolver)
	if err == nil {
		_, err = definition.RuntimeManifest(context.Background())
	}
	if err == nil || !strings.Contains(err.Error(), `reachable entry "retry" does not declare it`) {
		t.Fatalf("reachable missing input result = %v", err)
	}
}

func TestRepositoryTransitionCannotWidenTrustedTargetIDs(t *testing.T) {
	truth := true
	compiled, resolver := compiledFlow(t, controlprogram.Predicate{True: &truth})
	widened := compiled.Document
	widened.Facets = append(widened.Facets, controlprogram.Facet{ID: "plan", Kind: "string"})
	widened.Targets = []controlprogram.Target{{ID: "approved-plan", Predicate: fact("plan", "approved")}}
	widened.Entries = []controlprogram.Entry{{ID: "autoplan", Target: "approved-plan"}}
	unsafe, err := controlprogram.Compile(widened, resolver)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := softwareflow.NewDefinition(unsafe, resolver)
	if err == nil {
		_, err = definition.RuntimeManifest(context.Background())
	}
	if err == nil || !strings.Contains(err.Error(), "supports none of the declared entry targets") {
		t.Fatalf("widened objective result = %v", err)
	}
}

func TestAbandonmentEntryMakesTrustedAbandonmentObjectiveProgress(t *testing.T) {
	truth := true
	resolver, err := softwareflow.NewResolver(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	document := controlprogram.Document{
		Schema: controlprogram.SchemaName, SchemaRevision: controlprogram.SchemaRevision,
		Program: controlprogram.Program{ID: "product-delivery", Version: "1"},
		Facets: []controlprogram.Facet{
			{ID: "publication", Kind: "string"}, {ID: "verification", Kind: "string"},
			{ID: "configuration", Kind: "string"}, {ID: "runtime", Kind: "string"},
			{ID: "delivery", Kind: "string"}, {ID: "workspace", Kind: "string"},
		},
		Operators: []controlprogram.Operator{
			{ID: "publication.observe", Binding: &controlprogram.OperatorBinding{Reference: "software-delivery/publication.observe", Version: "1"}},
			{ID: "plan.abandon", Binding: &controlprogram.OperatorBinding{Reference: "software-delivery/plan.abandon", Version: "1"}},
		},
		Transitions: []controlprogram.Transition{
			{ID: "publication.observe", Operator: "publication.observe", Guard: controlprogram.Predicate{True: &truth}, Target: controlprogram.Predicate{True: &truth}, Priority: 77, Parameters: hostParameters("publication_id")},
			{ID: "plan.abandon", Operator: "plan.abandon", Guard: controlprogram.Predicate{True: &truth}, Target: controlprogram.Predicate{True: &truth}, Priority: 31},
		},
		Targets: []controlprogram.Target{
			{ID: "published-pr", Predicate: controlprogram.Predicate{All: []controlprogram.Predicate{fact("verification", "current"), fact("configuration", "verified"), fact("runtime", "verified"), fact("publication", "open")}}},
			{ID: "safely-abandoned", Predicate: controlprogram.Predicate{All: []controlprogram.Predicate{fact("delivery", "discarded"), {Fact: &controlprogram.FactPredicate{Facet: "workspace", Statuses: []string{"known"}, Values: []string{"abandoned", "absent"}}}}}},
		},
		Entries: []controlprogram.Entry{{ID: "run", Target: "published-pr"}, {ID: "abandon", Target: "safely-abandoned"}},
	}
	compiled, err := controlprogram.Compile(document, resolver)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := softwareflow.NewDefinition(compiled, resolver)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := definition.RuntimeManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, transition := range manifest.Transitions {
		if transition.ID == "plan.abandon" {
			if transition.SelectionClass != delivery.SelectionProgramProgress || len(transition.TargetIDs) != 1 || transition.TargetIDs[0] != delivery.ObjectiveAbandoned {
				t.Fatalf("abandonment transition = %#v", transition)
			}
			if transition.Priority != 31 {
				t.Fatalf("priority = %d, want 31", transition.Priority)
			}
			return
		}
	}
	t.Fatal("trusted plan.abandon transition was not selected")
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
