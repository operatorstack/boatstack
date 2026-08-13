package standard_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/flow/standard"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/supervisor"
	"github.com/operatorstack/boatstack/boatstack/internal/testprogram"
)

func historicalObjectiveContracts() catalog.ObjectiveContracts {
	manifest, err := standard.Definition().RuntimeManifest(context.Background())
	if err != nil {
		panic(err)
	}
	contracts, err := catalog.NewObjectiveContracts(manifest.ObjectiveContracts, nil)
	if err != nil {
		panic(err)
	}
	return contracts
}

type historicalCorpus struct {
	SchemaVersion int                 `json:"schema_version"`
	FixtureCount  int                 `json:"fixture_count"`
	Fixtures      []historicalFixture `json:"fixtures"`
}

type historicalFixture struct {
	Name                       string                   `json:"name"`
	InitialPlantFacts          map[string]string        `json:"initial_plant_facts"`
	CanonicalObservation       map[string]string        `json:"canonical_observation"`
	RequestedObjective         model.Objective          `json:"requested_objective"`
	Event                      catalog.TransitionID     `json:"event"`
	Authority                  []catalog.AuthorityClass `json:"authority"`
	ExpectedDecision           supervisor.DecisionKind  `json:"expected_decision"`
	ExpectedAdmittedTransition catalog.TransitionID     `json:"expected_admitted_transition"`
	ExpectedPostcondition      map[string]string        `json:"expected_postcondition"`
	ForbiddenTransition        catalog.TransitionID     `json:"forbidden_transition"`
	SourceProvenance           []string                 `json:"source_provenance"`
	FailureClass               string                   `json:"failure_class"`
}

func loadHistoricalCorpus(t *testing.T) historicalCorpus {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/scenarios/historical.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus historicalCorpus
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.SchemaVersion != 1 || corpus.FixtureCount != len(corpus.Fixtures) || len(corpus.Fixtures) < 18 {
		t.Fatalf("invalid corpus header: schema=%d count=%d fixtures=%d", corpus.SchemaVersion, corpus.FixtureCount, len(corpus.Fixtures))
	}
	return corpus
}

func snapshotFromFixture(t *testing.T, fixture historicalFixture) model.Snapshot {
	t.Helper()
	facts := fixture.CanonicalObservation
	evidence := model.Evidence{Source: "historical:" + fixture.Name, Fingerprint: "fixture-" + fixture.Name, ObservedAt: time.Unix(100, 0).UTC()}
	invocation := model.InvocationContext{
		RepositoryID: "repo", GitCommonID: "git-common", WorktreeID: "worktree", Ref: facts["ref"], ControllerID: "controller",
		InvokingPath: filepath.Join(t.TempDir(), "fixture", "repository"), Topology: model.Topology(facts["topology"]), Host: "corpus", Correlation: "correlation-" + fixture.Name,
	}
	observation := model.Observation{
		SchemaVersion: model.SnapshotSchemaVersion, StateRevision: 1, Invocation: invocation,
		Phase: model.Known(model.ProtocolPhase(facts["phase"]), evidence), Engagement: model.Known(model.EngagementState(facts["engagement"]), evidence),
		Delivery: model.Known(model.DeliveryState(facts["delivery"]), evidence), Workspace: model.Known(model.WorkspaceState(facts["workspace"]), evidence),
		Plan: model.Known(model.PlanState(facts["plan"]), evidence), Configuration: model.Known(model.ConfigurationState(facts["configuration"]), evidence),
		ConfigurationPolicy: model.Known(model.ConfigurationPolicy{PlanApproval: "human", VisualEvidence: "optional", ExternalEffectAuthority: "human-or-autonomy-plus-provider", Hosts: []string{"cli", "corpus"}}, evidence),
		Runtime:             model.Known(model.RuntimeState(facts["runtime"]), evidence), Publication: model.Known(model.PublicationState(facts["publication"]), evidence),
		Verification: model.Known(model.VerificationState(facts["verification"]), evidence), Recovery: model.Known(model.RecoveryState(facts["recovery"]), evidence),
		Transaction: model.Known(model.TransactionState(facts["transaction"]), evidence), Terminal: model.Known(model.TerminalStatus(facts["terminal"]), evidence),
		Objective: model.Known(fixture.RequestedObjective, evidence), RecoveryInfo: model.Absent[model.RecoveryContext]("none", evidence),
		TransactionInfo: model.Absent[model.TransactionContext]("none", evidence), ObservedAt: time.Unix(100, 0).UTC(),
	}
	if observation.Phase.Value == model.PhaseRecovery {
		observation.RecoveryInfo = model.Known(model.RecoveryContext{
			TransactionID: "adm-historical", Cause: fixture.FailureClass, SourcePhase: model.PhaseActive,
			Permitted: []string{"recovery.resume", "recovery.rollback", "recovery.escalate"}, BudgetRemaining: 2, Resumption: model.PhaseActive,
		}, evidence)
		observation.TransactionInfo = model.Known(model.TransactionContext{ID: "adm-historical", TransitionID: "workspace.sync", Status: "recovery-required"}, evidence)
	}
	snapshot, err := model.Canonicalize(observation)
	if err != nil {
		t.Fatalf("%s: canonicalize: %v", fixture.Name, err)
	}
	return snapshot
}

func authoritySet(values []catalog.AuthorityClass) catalog.AuthoritySet {
	result := catalog.AuthoritySet{}
	for _, value := range values {
		result[value] = true
	}
	return result
}

func TestHistoricalFailureCorpusUsesTheRuntimeControlLaw(t *testing.T) {
	// control-law: historical failures bind to executable catalog predicates
	corpus := loadHistoricalCorpus(t)
	registry := testprogram.StandardRegistry()
	control := supervisor.New(registry, historicalObjectiveContracts())
	seenNames := map[string]bool{}
	for _, fixture := range corpus.Fixtures {
		fixture := fixture
		t.Run(fixture.Name, func(t *testing.T) {
			if fixture.Name == "" || fixture.FailureClass == "" || len(fixture.InitialPlantFacts) == 0 || len(fixture.CanonicalObservation) == 0 || len(fixture.SourceProvenance) == 0 || len(fixture.ExpectedPostcondition) == 0 {
				t.Fatal("fixture is missing required historical fields")
			}
			if seenNames[fixture.Name] {
				t.Fatal("duplicate fixture name")
			}
			seenNames[fixture.Name] = true
			if fixture.ExpectedDecision == "IDENTITY_REFUSED" {
				invocation := snapshotIdentityForFixture(t, fixture)
				if err := invocation.Validate(true); err == nil {
					t.Fatal("ambiguous/conflicting effect identity was accepted")
				}
				return
			}
			snapshot := snapshotFromFixture(t, fixture)
			one := control.Resolve(snapshot, fixture.RequestedObjective, authoritySet(fixture.Authority), fixture.Event)
			two := control.Resolve(snapshot, fixture.RequestedObjective, authoritySet(fixture.Authority), fixture.Event)
			if !reflect.DeepEqual(one, two) {
				t.Fatalf("resolution is nondeterministic: %#v != %#v", one, two)
			}
			if one.Kind != fixture.ExpectedDecision {
				t.Fatalf("decision=%s reason=%s, want %s", one.Kind, one.Reason, fixture.ExpectedDecision)
			}
			if fixture.ExpectedAdmittedTransition != "" {
				if one.Transition == nil || one.Transition.ID != fixture.ExpectedAdmittedTransition {
					t.Fatalf("transition=%v, want %s", one.Transition, fixture.ExpectedAdmittedTransition)
				}
				assertPostconditionDeclared(t, *one.Transition, fixture.ExpectedPostcondition)
			}
			if fixture.ForbiddenTransition != "" && one.Transition != nil && one.Transition.ID == fixture.ForbiddenTransition {
				t.Fatalf("forbidden transition %s was prescribed", fixture.ForbiddenTransition)
			}
		})
	}
}

func snapshotIdentityForFixture(t *testing.T, fixture historicalFixture) model.InvocationContext {
	t.Helper()
	return model.InvocationContext{
		RepositoryID: "repo", GitCommonID: "git", Ref: fixture.CanonicalObservation["ref"], ControllerID: "shared-alias",
		InvokingPath: filepath.Join(t.TempDir(), "fixture", "repository"), Topology: model.Topology(fixture.CanonicalObservation["topology"]), Host: "corpus", Correlation: "identity-refusal",
	}
}

func assertPostconditionDeclared(t *testing.T, transition catalog.Transition, expected map[string]string) {
	t.Helper()
	for facet, value := range expected {
		if facet == "preserves" || facet == "terminal" {
			continue
		}
		if facet == "phase" {
			if !transition.DeclaresTargetPhase(model.ProtocolPhase(value)) {
				t.Fatalf("transition %s does not declare target phase %s", transition.ID, value)
			}
			continue
		}
		found := false
		for _, condition := range transition.TargetConditions {
			if string(condition.Facet) != facet {
				continue
			}
			for _, candidate := range condition.Values {
				if candidate == value {
					found = true
				}
			}
		}
		if !found {
			t.Fatalf("transition %s does not declare %s=%s as a target", transition.ID, facet, value)
		}
	}
}

func TestHistoricalCorpusCoversEveryPRFrom172Through185(t *testing.T) {
	corpus := loadHistoricalCorpus(t)
	pattern := regexp.MustCompile(`#(\d+)`)
	covered := map[int]bool{}
	for _, fixture := range corpus.Fixtures {
		for _, source := range fixture.SourceProvenance {
			for _, match := range pattern.FindAllStringSubmatch(source, -1) {
				value, _ := strconv.Atoi(match[1])
				covered[value] = true
			}
		}
	}
	for number := 172; number <= 185; number++ {
		if !covered[number] {
			t.Errorf("PR #%d has no historical Boatstack fixture", number)
		}
	}
}
