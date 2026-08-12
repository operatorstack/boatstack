package effects

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

func ownershipState() durable.State {
	state := durable.Default(model.InvocationContext{RepositoryID: "repo", GitCommonID: "git", WorktreeID: "worktree"}, time.Unix(100, 0).UTC())
	state.ProgramFingerprint = strings.Repeat("a", 64)
	return state
}

func transitionFixture(id catalog.TransitionID, origin catalog.OriginKind, runtime bool) catalog.Transition {
	owned := []model.StateFacet{model.StateFacetControl}
	switch id {
	case "installation.update":
		owned = append(owned, model.StateFacetInstallation)
	case "catalog.reconcile":
		owned = append(owned, model.StateFacetProgram)
	case "objective.bind":
		owned = append(owned, model.StateFacetProduct)
	}
	return catalog.Transition{
		ID: id, Class: catalog.EventOwnedLocal, RuntimeExecution: runtime,
		Origin:      catalog.TransitionOrigin{Kind: origin, ID: "fixture", Version: "1", ManifestFingerprint: "manifest"},
		OwnedFacets: owned, StateEffect: catalog.StateEffect{Kind: catalog.StateEffectAssignments},
	}
}

func requireOwnedChange(t *testing.T, transition catalog.Transition, before, after durable.State, wantError bool) {
	t.Helper()
	changed, err := changedStateFacets([2]durable.State{before, after})
	if err == nil {
		_, err = validateTransitionStateFacets(transition, changed)
	}
	if wantError && (err == nil || !strings.Contains(err.Error(), "FACET_OWNERSHIP_VIOLATION")) {
		t.Fatalf("expected ownership refusal, got %v", err)
	}
	if !wantError && err != nil {
		t.Fatal(err)
	}
}

func TestStateFacetIsolationMatrix(t *testing.T) {
	base := ownershipState()
	knownObjective := model.Objective{ID: "objective", Kind: model.ObjectiveOpenPR, DeliveryID: "delivery"}
	fixtures := []struct {
		name       string
		transition catalog.Transition
		mutate     func(*durable.State)
		refused    bool
	}{
		{"installation owns installation", transitionFixture("installation.update", catalog.OriginCoreSystem, false), func(s *durable.State) { s.RuntimeVersion = "v2" }, false},
		{"installation owns control", transitionFixture("installation.update", catalog.OriginCoreSystem, false), func(s *durable.State) { s.Revision++ }, false},
		{"installation cannot mutate product", transitionFixture("installation.update", catalog.OriginCoreSystem, false), func(s *durable.State) { s.Objective = knownObjective }, true},
		{"program reconcile owns program", transitionFixture("catalog.reconcile", catalog.OriginCoreSystem, false), func(s *durable.State) { s.ProgramFingerprint = strings.Repeat("b", 64) }, false},
		{"program reconcile owns control", transitionFixture("catalog.reconcile", catalog.OriginCoreSystem, false), func(s *durable.State) { s.Revision++ }, false},
		{"program reconcile cannot mutate product", transitionFixture("catalog.reconcile", catalog.OriginCoreSystem, false), func(s *durable.State) { s.Objective = knownObjective }, true},
		{"product owns product", transitionFixture("objective.bind", catalog.OriginCoreSystem, false), func(s *durable.State) { s.Objective = knownObjective }, false},
		{"product owns control", transitionFixture("objective.bind", catalog.OriginCoreSystem, false), func(s *durable.State) { s.Revision++ }, false},
		{"product cannot mutate installation", transitionFixture("objective.bind", catalog.OriginCoreSystem, false), func(s *durable.State) { s.RuntimeVersion = "v2" }, true},
		{"control cannot synthesize engagement", transitionFixture("recovery.escalate", catalog.OriginCoreSystem, false), func(s *durable.State) { s.Engagement = model.EngagementActive }, true},
		{"repository write cannot bypass product ownership", transitionFixture("repository-program/write", catalog.OriginControlProgram, true), func(s *durable.State) { s.Objective = knownObjective }, true},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			after := base
			fixture.mutate(&after)
			requireOwnedChange(t, fixture.transition, base, after, fixture.refused)
		})
	}
}

func TestMaintenancePreservesAbsentAndKnownObjectiveExactly(t *testing.T) {
	transition := transitionFixture("installation.update", catalog.OriginCoreSystem, false)
	transition.Policy.ObjectiveScope = catalog.ObjectiveScopeOptionalPreserve
	transition.TargetPhases = []model.ProtocolPhase{model.PhaseDormant}
	for _, objective := range []model.Objective{{}, {ID: "known", Kind: model.ObjectiveOpenPR, DeliveryID: "delivery"}} {
		state := ownershipState()
		state.Objective = objective
		admission := protocol.Admission{ObjectiveStatus: model.FactAbsent}
		if objective.Validate() == nil {
			admission.Objective, admission.ObjectiveStatus = objective, model.FactKnown
		}
		if err := applyStateTransition(&state, admission, transition); err != nil {
			t.Fatal(err)
		}
		if state.Objective != objective {
			t.Fatalf("objective changed from %#v to %#v", objective, state.Objective)
		}
	}
}

func TestRecoveryCannotReplayFacetOutsideInterruptedTransition(t *testing.T) {
	before := ownershipState()
	after := before
	after.Objective = model.Objective{ID: "invented", Kind: model.ObjectiveApprovedPlan, DeliveryID: "invented"}
	prior, _ := durable.EncodeState(before)
	target, _ := durable.EncodeState(after)
	record := journalRecord{TransitionID: "installation.update", AllowedStateFacets: []model.StateFacet{model.StateFacetControl, model.StateFacetInstallation}, Mutations: []ports.ResourceMutation{{Path: "/controller/state.json", PriorExists: true, Prior: prior, Target: target, StateFacets: []model.StateFacet{model.StateFacetControl, model.StateFacetProduct}}}}
	if _, err := recoveryStateFacets(record, "recovery.resume", model.InvocationContext{}, nil); err == nil || !strings.Contains(err.Error(), "FACET_OWNERSHIP_VIOLATION") {
		t.Fatalf("recovery accepted product contamination: %v", err)
	}
}

func TestRecoveryRefusesUnclassifiedDurableMutation(t *testing.T) {
	state := ownershipState()
	raw, _ := durable.EncodeState(state)
	record := journalRecord{TransitionID: "installation.update", AllowedStateFacets: []model.StateFacet{model.StateFacetControl, model.StateFacetInstallation}, Mutations: []ports.ResourceMutation{{Path: "/controller/state.json", PriorExists: true, Prior: raw, Target: raw}}}
	if _, err := recoveryStateFacets(record, "recovery.resume", model.InvocationContext{}, nil); err == nil || !strings.Contains(err.Error(), "STATE_FACET_UNCLASSIFIED") {
		t.Fatalf("recovery accepted an unclassified state mutation: %v", err)
	}
}

func TestRecoveryReportsActualStateDeltaInsteadOfInterruptedEnvelope(t *testing.T) {
	invocation := model.InvocationContext{RepositoryID: "repo", GitCommonID: "git", WorktreeID: "worktree"}
	before := durable.Default(invocation, time.Unix(100, 0).UTC())
	staged := before
	staged.Plan = model.PlanDraft
	staged.Revision++
	staged.LastTransition = "plan.create"
	staged.UpdatedAt = time.Unix(101, 0).UTC()
	prior, _ := durable.EncodeState(before)
	target, _ := durable.EncodeState(staged)
	record := journalRecord{TransitionID: "plan.create", AllowedStateFacets: []model.StateFacet{model.StateFacetControl, model.StateFacetProduct}, Mutations: []ports.ResourceMutation{{
		Path: "/controller/state.json", PriorExists: true, Prior: prior, Target: target,
		StateFacets: []model.StateFacet{model.StateFacetControl, model.StateFacetProduct},
	}}}

	recovered := before
	recovered.Revision++
	recovered.LastTransition = "recovery.rollback"
	recovered.UpdatedAt = time.Unix(102, 0).UTC()
	recoveredRaw, _ := durable.EncodeState(recovered)
	mutations := []ports.ResourceMutation{{Path: "/controller/state.json", PriorExists: true, Prior: prior, Target: recoveredRaw}}
	changed, err := recoveryStateFacets(record, "recovery.rollback", invocation, mutations)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(changed, []model.StateFacet{model.StateFacetControl}) {
		t.Fatalf("recovery facets = %v, want actual control-only delta", changed)
	}
}

func TestJournalRejectsReceiptFacetMismatch(t *testing.T) {
	err := validateCommittedMutationFacts(catalog.EventOwnedLocal, []ports.ResourceMutation{{StateFacets: []model.StateFacet{model.StateFacetControl}}}, []model.StateFacet{model.StateFacetProduct}, nil)
	if err == nil || !strings.Contains(err.Error(), "do not match staged mutation facets") {
		t.Fatalf("receipt/state facet mismatch accepted: %v", err)
	}
}
