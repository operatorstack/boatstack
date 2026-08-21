package softwaredelivery_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	"github.com/operatorstack/boatstack/boatstack/core"
	"github.com/operatorstack/boatstack/boatstack/delivery"
	softwareflow "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/supervisor"
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
			{ID: "configuration", Kind: "string"}, {ID: "runtime", Kind: "string"}, {ID: "publication_id", Kind: "string"},
		},
		Operators:   []controlprogram.Operator{{ID: "publication.observe", Binding: &controlprogram.OperatorBinding{Reference: "software-delivery/publication.observe", Version: "1"}}},
		Transitions: []controlprogram.Transition{{ID: "publication.observe", Operator: "publication.observe", Guard: guard, Target: controlprogram.Predicate{True: &truth}, Priority: 77, Parameters: publicationIDStateParameter(guard)}},
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

func publicationIDStateParameter(_ controlprogram.Predicate) []controlprogram.TransitionParameterBinding {
	available := controlprogram.Predicate{Fact: &controlprogram.FactPredicate{Facet: "publication_id", Statuses: []string{"known"}}}
	return []controlprogram.TransitionParameterBinding{{Parameter: "publication_id", Producer: controlprogram.ParameterProducer{
		Kind: controlprogram.ParameterSourceState, Facet: "publication_id", AvailableWhen: &available,
	}}}
}

func fact(facet, value string) controlprogram.Predicate {
	return controlprogram.Predicate{Fact: &controlprogram.FactPredicate{Facet: facet, Statuses: []string{"known"}, Values: []string{value}}}
}

func compiledPackageLifecycle(t *testing.T, targetID string, target controlprogram.Predicate, transitionIDs ...string) (controlprogram.Compiled, softwareflow.Resolver) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "control-programs", "product-delivery-planning-package.raw.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document controlprogram.Document
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	selected := make(map[string]bool, len(transitionIDs))
	for _, id := range transitionIDs {
		selected[id] = true
	}
	operators := document.Operators[:0]
	for _, operator := range document.Operators {
		if selected[operator.ID] {
			operators = append(operators, operator)
		}
	}
	document.Operators = operators
	transitions := document.Transitions[:0]
	for _, transition := range document.Transitions {
		if selected[transition.ID] {
			transitions = append(transitions, transition)
		}
	}
	document.Transitions = transitions
	if !selected[softwareflow.WorkPackageAdmit] {
		document.Work = nil
	}
	document.Targets = []controlprogram.Target{{ID: targetID, Predicate: target}}
	document.Entries = document.Entries[:1]
	document.Entries[0].ID = "accept"
	document.Entries[0].Target = targetID
	resolver, err := softwareflow.NewResolver(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	mutated, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	repositoryRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := controlprogram.LoadWithAssets(bytes.NewReader(mutated), resolver, controlprogram.RepositoryAssetResolver{Repository: repositoryRoot})
	if err != nil {
		t.Fatal(err)
	}
	return compiled, resolver
}

func packageRecoverySnapshot(t *testing.T, objective model.Objective, workPackage model.Fact[model.WorkPackageState], plan model.PlanState) model.Snapshot {
	t.Helper()
	evidence := model.Evidence{Source: "fixture", Fingerprint: "fixture", ObservedAt: time.Unix(10, 0).UTC()}
	snapshot, err := model.Canonicalize(model.Observation{
		SchemaVersion: model.SnapshotSchemaVersion, StateRevision: 7,
		Invocation: model.InvocationContext{
			RepositoryID: "repo", GitCommonID: "git", WorktreeID: "wt", Ref: "refs/heads/feature",
			ControllerID: "controller", InvokingPath: filepath.Join(t.TempDir(), "repo"), RuntimeVersion: "runtime",
			RuntimePath: filepath.Join(t.TempDir(), "boatstack"), RuntimeFingerprint: "fingerprint",
			Topology: model.TopologyEmbedded, Host: "cursor", Correlation: "correlation",
		},
		Program: model.Known(model.ProgramCurrent, evidence), Phase: model.Known(model.PhaseActive, evidence),
		Engagement: model.Known(model.EngagementActive, evidence), Delivery: model.Known(model.DeliveryPlanning, evidence),
		Workspace: model.Known(model.WorkspaceAbsent, evidence), WorkPackage: workPackage, Plan: model.Known(plan, evidence),
		Configuration: model.Known(model.ConfigurationVerified, evidence), Runtime: model.Known(model.RuntimeVerified, evidence),
		ConfigurationPolicy: model.Known(model.ConfigurationPolicy{PlanApproval: "human", VisualEvidence: "optional", ExternalEffectAuthority: "human-or-autonomy-plus-provider", Hosts: []string{"cli", "cursor"}}, evidence),
		Publication:         model.Known(model.PublicationNone, evidence), Verification: model.Known(model.VerificationUnverified, evidence),
		Recovery: model.Known(model.RecoveryNone, evidence), Transaction: model.Known(model.TransactionNone, evidence),
		RecoveryInfo: model.Absent[model.RecoveryContext]("none", evidence), TransactionInfo: model.Absent[model.TransactionContext]("none", evidence),
		Terminal: model.Known(model.TerminalStale, evidence), Objective: model.Known(objective, evidence),
		ObservedAt: time.Unix(10, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
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
	invalid.Transitions[0].Parameters = publicationIDStateParameter(invalid.Transitions[0].Guard)
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

func TestEntryActivationRequiresTrustedSoftwareDeliveryProducer(t *testing.T) {
	truth := true
	compiled, resolver := compiledFlow(t, controlprogram.Predicate{True: &truth})
	document := compiled.Document
	document.Declarations.Authorities = append(document.Declarations.Authorities, "autonomy")
	document.Entries[0].Requires.Authorities = []string{"autonomy"}
	unsupported, err := controlprogram.Compile(document, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := softwareflow.NewDefinition(unsupported, resolver); err == nil || !strings.Contains(err.Error(), "no trusted software-delivery producer") {
		t.Fatalf("unsupported entry activation result = %v", err)
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

func TestPublicationReconcileBindingDoesNotRequireUncommittedPublicationOutput(t *testing.T) {
	// control-law: recovery inputs survive an interrupted external effect
	resolver, err := softwareflow.NewResolver(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolver.ResolveOperator("software-delivery/publication.reconcile", "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Parameters) != 1 || resolved.Parameters[0].ID != "transaction_id" ||
		len(resolved.StateInputs) != 1 || resolved.StateInputs[0].Parameter != "transaction_id" || resolved.StateInputs[0].Facet != softwareflow.RecoveryTransactionFacet {
		t.Fatalf("publication reconciliation inputs = parameters %#v state %#v", resolved.Parameters, resolved.StateInputs)
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
		Inputs:  []controlprogram.WorkInput{{ID: "plan", Producer: controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceEntryInput, Input: "plan"}}},
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

func TestApprovedWorkPackageEntryResolvesTrustedObjective(t *testing.T) {
	accepted, resolver := compiledPackageLifecycle(t, "approved-package", fact("work-package", "approved"), softwareflow.WorkPackageAdmit, softwareflow.WorkPackageApprove)
	objective, err := softwareflow.ObjectiveForEntry(context.Background(), accepted, resolver, "accept")
	if err != nil {
		t.Fatal(err)
	}
	if objective.TrustedClass != model.ObjectiveApprovedWorkPackage || objective.TargetID != "approved-package" {
		t.Fatalf("accepted-work objective = %#v", objective)
	}
	definition, err := softwareflow.NewDefinition(accepted, resolver)
	if err != nil {
		t.Fatal(err)
	}
	program, err := delivery.Compile(context.Background(), delivery.CompileRequest{
		KernelVersion: "v2.0.0", Core: core.System(), Runtime: definition, Settings: map[string]string{"repo": "fixture"},
	})
	if err != nil {
		t.Fatal(err)
	}
	requested := model.Objective{ID: "accepted-package", TargetID: objective.TargetID, TrustedClass: objective.TrustedClass, DeliveryID: "delivery"}
	evidence := model.Evidence{Source: "fixture", Fingerprint: "fixture", ObservedAt: time.Unix(10, 0).UTC()}
	snapshot, err := model.Canonicalize(model.Observation{
		SchemaVersion: model.SnapshotSchemaVersion, StateRevision: 1,
		Invocation: model.InvocationContext{
			RepositoryID: "repo", GitCommonID: "git", WorktreeID: "wt", Ref: "refs/heads/feature",
			ControllerID: "controller", InvokingPath: filepath.Join(t.TempDir(), "repo"), RuntimeVersion: "runtime",
			RuntimePath: filepath.Join(t.TempDir(), "boatstack"), RuntimeFingerprint: "fingerprint",
			Topology: model.TopologyEmbedded, Host: "cursor", Correlation: "correlation",
		},
		Program: model.Known(model.ProgramCurrent, evidence), Phase: model.Known(model.PhaseObserved, evidence),
		Engagement: model.Known(model.EngagementDormant, evidence), Delivery: model.Known(model.DeliveryUninitialized, evidence),
		Workspace: model.Known(model.WorkspaceAbsent, evidence), Plan: model.Known(model.PlanAbsent, evidence),
		Configuration: model.Known(model.ConfigurationVerified, evidence), Runtime: model.Known(model.RuntimeVerified, evidence),
		ConfigurationPolicy: model.Known(model.ConfigurationPolicy{PlanApproval: "human", VisualEvidence: "optional", ExternalEffectAuthority: "human-or-autonomy-plus-provider", Hosts: []string{"cli", "cursor"}}, evidence),
		Publication:         model.Known(model.PublicationNone, evidence), Verification: model.Known(model.VerificationUnverified, evidence),
		Recovery: model.Known(model.RecoveryNone, evidence), Transaction: model.Known(model.TransactionNone, evidence),
		RecoveryInfo: model.Absent[model.RecoveryContext]("none", evidence), TransactionInfo: model.Absent[model.TransactionContext]("none", evidence),
		Terminal: model.Known(model.TerminalNonterminal, evidence), Objective: model.Absent[model.Objective]("not configured", evidence),
		ObservedAt: time.Unix(10, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	decision := supervisor.New(program.RuntimeRegistry(), program.RuntimeObjectiveContracts()).Resolve(
		snapshot, requested, catalog.AuthoritySet{catalog.AuthorityHuman: true, catalog.AuthorityAutonomy: true}, "",
	)
	if decision.Kind != supervisor.DecisionPrescribed || decision.Transition == nil || decision.Transition.ID != "objective.bind" {
		t.Fatalf("accepted-work objective decision = %#v, want objective.bind", decision)
	}
	snapshot.Objective = model.Known(requested, evidence)
	snapshot, err = model.Canonicalize(snapshot.Observation)
	if err != nil {
		t.Fatal(err)
	}
	decision = supervisor.New(program.RuntimeRegistry(), program.RuntimeObjectiveContracts()).Resolve(
		snapshot, requested, catalog.AuthoritySet{catalog.AuthorityHuman: true, catalog.AuthorityAutonomy: true, catalog.AuthorityRepository: true}, "",
	)
	if decision.Kind != supervisor.DecisionPrescribed || decision.Transition == nil || decision.Transition.ID != "engagement.begin" {
		t.Fatalf("accepted-work engagement decision = %#v, want engagement.begin", decision)
	}
	spoofed := requested
	spoofed.TrustedClass = model.ObjectiveApprovedPlan
	decision = supervisor.New(program.RuntimeRegistry(), program.RuntimeObjectiveContracts()).Resolve(snapshot, spoofed, nil, "")
	if decision.Kind != supervisor.DecisionRefused || decision.Transition != nil {
		t.Fatalf("spoofed accepted-work objective decision = %#v, want REFUSED", decision)
	}
}

func TestRuntimeManifestRejectsIncompleteWorkPackageLifecycles(t *testing.T) {
	tests := []struct {
		name        string
		targetID    string
		target      controlprogram.Predicate
		transitions []string
		want        string
	}{
		{name: "approve only", targetID: "approved-package", target: fact("work-package", "approved"), transitions: []string{softwareflow.WorkPackageApprove}, want: "requires work.package.admit and work.package.approve"},
		{name: "admit only", targetID: "approved-package", target: fact("work-package", "approved"), transitions: []string{softwareflow.WorkPackageAdmit}, want: "requires work.package.admit and work.package.approve"},
		{name: "admit and promote", targetID: "approved-plan", target: fact("plan", "approved"), transitions: []string{softwareflow.WorkPackageAdmit, softwareflow.PlanningPackagePromote}, want: "requires work.package.admit and work.package.approve"},
		{name: "package approval cannot satisfy plan", targetID: "approved-plan", target: fact("plan", "approved"), transitions: []string{softwareflow.WorkPackageAdmit, softwareflow.WorkPackageApprove}, want: "requires planning.package.promote"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			compiled, resolver := compiledPackageLifecycle(t, test.targetID, test.target, test.transitions...)
			definition, err := softwareflow.NewDefinition(compiled, resolver)
			if err == nil {
				_, err = definition.RuntimeManifest(context.Background())
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("incomplete lifecycle result = %v, want %q", err, test.want)
			}
		})
	}
}

func TestStalePackageAndPlanObservationsRetainRecoveryTransitions(t *testing.T) {
	evidence := model.Evidence{Source: "fixture", Fingerprint: "fixture", ObservedAt: time.Unix(10, 0).UTC()}
	tests := []struct {
		name        string
		targetID    string
		target      controlprogram.Predicate
		trusted     model.TargetID
		transitions []string
		workPackage model.Fact[model.WorkPackageState]
		plan        model.PlanState
		want        delivery.TransitionID
	}{
		{
			name: "corrupted generic package is re-admitted", targetID: "approved-package", target: fact("work-package", "approved"),
			trusted: model.ObjectiveApprovedWorkPackage, transitions: []string{softwareflow.WorkPackageAdmit, softwareflow.WorkPackageApprove},
			workPackage: model.Fact[model.WorkPackageState]{Status: model.FactStale, Value: model.WorkPackageAbsent, Evidence: []model.Evidence{evidence}},
			plan:        model.PlanAbsent, want: softwareflow.WorkPackageAdmit,
		},
		{
			name: "corrupted promoted plan is re-promoted", targetID: "approved-plan", target: fact("plan", "approved"),
			trusted: model.ObjectiveApprovedPlan, transitions: []string{softwareflow.WorkPackageAdmit, softwareflow.WorkPackageApprove, softwareflow.PlanningPackagePromote},
			workPackage: model.Known(model.WorkPackageApproved, evidence), plan: model.PlanStale, want: softwareflow.PlanningPackagePromote,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			compiled, resolver := compiledPackageLifecycle(t, test.targetID, test.target, test.transitions...)
			definition, err := softwareflow.NewDefinition(compiled, resolver)
			if err != nil {
				t.Fatal(err)
			}
			program, err := delivery.Compile(context.Background(), delivery.CompileRequest{
				KernelVersion: "v2.0.0", Core: core.System(), Runtime: definition, Settings: map[string]string{"repo": "fixture"},
			})
			if err != nil {
				t.Fatal(err)
			}
			objective := model.Objective{ID: "objective", TargetID: model.TargetID(test.targetID), TrustedClass: test.trusted, DeliveryID: "delivery"}
			snapshot := packageRecoverySnapshot(t, objective, test.workPackage, test.plan)
			decision := supervisor.New(program.RuntimeRegistry(), program.RuntimeObjectiveContracts()).Resolve(
				snapshot, objective, catalog.AuthoritySet{catalog.AuthorityAutonomy: true, catalog.AuthorityRepository: true}, "",
			)
			if decision.Kind != supervisor.DecisionPrescribed || decision.Transition == nil || decision.Transition.ID != test.want {
				t.Fatalf("stale recovery decision = %#v, want %s", decision, test.want)
			}
		})
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
			{ID: "delivery", Kind: "string"}, {ID: "workspace", Kind: "string"}, {ID: "publication_id", Kind: "string"},
		},
		Operators: []controlprogram.Operator{
			{ID: "publication.observe", Binding: &controlprogram.OperatorBinding{Reference: "software-delivery/publication.observe", Version: "1"}},
			{ID: "plan.abandon", Binding: &controlprogram.OperatorBinding{Reference: "software-delivery/plan.abandon", Version: "1"}},
		},
		Transitions: []controlprogram.Transition{
			{ID: "publication.observe", Operator: "publication.observe", Guard: controlprogram.Predicate{True: &truth}, Target: controlprogram.Predicate{True: &truth}, Priority: 77, Parameters: publicationIDStateParameter(controlprogram.Predicate{True: &truth})},
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

func abandonmentWorkDependencyDocument(t *testing.T, producerID string, producerPriority int) controlprogram.Document {
	t.Helper()
	truth := true
	summary := "Summarize the delivery outcome."
	summaryDigest := sha256.Sum256([]byte(summary))
	note := "Publish the closing note from the summary report."
	noteDigest := sha256.Sum256([]byte(note))
	document := controlprogram.Document{
		Schema: controlprogram.SchemaName, SchemaRevision: controlprogram.SchemaRevision,
		Program: controlprogram.Program{ID: "product-delivery", Version: "1"},
		Facets: []controlprogram.Facet{
			{ID: "publication", Kind: "string"}, {ID: "verification", Kind: "string"},
			{ID: "configuration", Kind: "string"}, {ID: "runtime", Kind: "string"},
			{ID: "delivery", Kind: "string"}, {ID: "workspace", Kind: "string"},
			{ID: "publication_id", Kind: "string"}, {ID: "plan", Kind: "string"},
			{ID: "phase", Kind: "string"}, {ID: "terminal", Kind: "string"},
		},
		Operators: []controlprogram.Operator{
			{ID: "publication.observe", Binding: &controlprogram.OperatorBinding{Reference: "software-delivery/publication.observe", Version: "1"}},
			{ID: "plan.abandon", Binding: &controlprogram.OperatorBinding{Reference: "software-delivery/plan.abandon", Version: "1"}},
		},
		Work: []controlprogram.WorkContract{
			{
				ID: "summary", Instructions: controlprogram.WorkAsset{Path: "summary.md", SHA256: hex.EncodeToString(summaryDigest[:]), Content: summary},
				Outputs: []controlprogram.WorkOutput{{ID: "report", Path: "report.md", MediaType: "text/markdown", Required: true}},
			},
			{
				ID: "publish-note", Instructions: controlprogram.WorkAsset{Path: "publish-note.md", SHA256: hex.EncodeToString(noteDigest[:]), Content: note},
				Inputs:  []controlprogram.WorkInput{{ID: "report", Producer: controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceWorkOutput, Work: "summary", Output: "report"}}},
				Outputs: []controlprogram.WorkOutput{{ID: "note", Path: "note.md", MediaType: "text/markdown", Required: true}},
			},
		},
		Transitions: []controlprogram.Transition{
			{ID: "publication.observe", Operator: "publication.observe", Guard: controlprogram.Predicate{True: &truth}, Target: controlprogram.Predicate{True: &truth}, Priority: 77, Work: "publish-note", Parameters: publicationIDStateParameter(controlprogram.Predicate{True: &truth})},
			{ID: "plan.abandon", Operator: "plan.abandon", Guard: controlprogram.Predicate{True: &truth}, Target: controlprogram.Predicate{True: &truth}, Priority: 31},
		},
		Targets: []controlprogram.Target{
			{ID: "published-pr", Predicate: controlprogram.Predicate{All: []controlprogram.Predicate{fact("verification", "current"), fact("configuration", "verified"), fact("runtime", "verified"), fact("publication", "open")}}},
			{ID: "safely-abandoned", Predicate: controlprogram.Predicate{All: []controlprogram.Predicate{fact("delivery", "discarded"), {Fact: &controlprogram.FactPredicate{Facet: "workspace", Statuses: []string{"known"}, Values: []string{"abandoned", "absent"}}}}}},
		},
		Entries: []controlprogram.Entry{{ID: "run", Target: "published-pr"}, {ID: "abandon", Target: "safely-abandoned"}},
	}
	attached := false
	for index := range document.Transitions {
		if document.Transitions[index].ID == producerID {
			document.Transitions[index].Work, document.Transitions[index].Priority = "summary", producerPriority
			attached = true
		}
	}
	if !attached {
		truth := true
		document.Operators = append(document.Operators, controlprogram.Operator{ID: producerID, Binding: &controlprogram.OperatorBinding{Reference: "software-delivery/" + producerID, Version: "1"}})
		document.Transitions = append(document.Transitions, controlprogram.Transition{ID: producerID, Operator: producerID, Guard: controlprogram.Predicate{True: &truth}, Target: controlprogram.Predicate{True: &truth}, Priority: producerPriority, Work: "summary"})
	}
	return document
}

func TestWorkOutputProducerMustCoverConsumerTargets(t *testing.T) {
	// control-law: every objective that can select a work-output consumer must
	// also admit its required producer transition. Compilation proves only
	// predicate and priority ordering, so a program may attach producer work to
	// plan.abandon (safely-abandoned only) and dependent consumer work to
	// publication.observe (published-pr): a published-pr run then selects the
	// consumer, redirects to the missing producer output, targeted resolution
	// refuses plan.abandon for that objective, and the unchanged state
	// re-selects the consumer — a permanent zero-progress path.
	resolver, err := softwareflow.NewResolver(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	uncovered, err := controlprogram.Compile(abandonmentWorkDependencyDocument(t, "plan.abandon", 31), resolver)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := softwareflow.NewDefinition(uncovered, resolver)
	if err == nil {
		_, err = definition.RuntimeManifest(context.Background())
	}
	if err == nil || !strings.Contains(err.Error(), "do not cover consumer targets") {
		t.Fatalf("uncovered work dependency result = %v", err)
	}

	// plan.validate supports every trusted class the consumer supports, so the
	// same dependency with a covering producer must stay admissible.
	covered, err := controlprogram.Compile(abandonmentWorkDependencyDocument(t, "plan.validate", 50), resolver)
	if err != nil {
		t.Fatal(err)
	}
	definition, err = softwareflow.NewDefinition(covered, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = definition.RuntimeManifest(context.Background()); err != nil {
		t.Fatalf("covered work dependency was rejected: %v", err)
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
