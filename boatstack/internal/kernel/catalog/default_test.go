package catalog

import (
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

func TestDefaultRegistryIsTheCompleteRuntimeCatalog(t *testing.T) {
	// control-law: one-runtime-registry-owns-every-managed-event
	registry := Default()
	if got := registry.Len(); got != DefaultTransitionCount {
		t.Fatalf("registry contains %d transitions, want %d", got, DefaultTransitionCount)
	}
	counts := map[EventClass]int{}
	for _, transition := range registry.All() {
		counts[transition.Class]++
		if transition.Controllable() && transition.Effect == "" {
			t.Fatalf("controllable transition %q has no effect", transition.ID)
		}
		if !transition.Controllable() && (transition.Effect != "" || len(transition.OwnedResources) != 0) {
			t.Fatalf("observed event %q owns an effect", transition.ID)
		}
	}
	if counts[EventObservedExternal] != 13 {
		t.Fatalf("observed external events = %d, want 13", counts[EventObservedExternal])
	}
}

func TestExternalEffectsRequireProviderAndHumanOrAutonomyAuthority(t *testing.T) {
	transition, _ := Default().Lookup("publication.execute")
	if (AuthoritySet{AuthorityHuman: true}).Satisfies(transition.Authority, transition.AuthorityAll) {
		t.Fatal("human authority substituted for provider authority")
	}
	if (AuthoritySet{AuthorityProvider: true}).Satisfies(transition.Authority, transition.AuthorityAll) {
		t.Fatal("provider authority substituted for human/autonomy authority")
	}
	if !(AuthoritySet{AuthorityHuman: true, AuthorityProvider: true}).Satisfies(transition.Authority, transition.AuthorityAll) {
		t.Fatal("complete external authority set was rejected")
	}
}

func TestEveryControllableTransitionNamesAnExecutableRecoveryOperator(t *testing.T) {
	registry := Default()
	for _, transition := range registry.All() {
		if !transition.Controllable() {
			continue
		}
		recovery, ok := registry.Lookup(transition.Interruption.Recovery)
		if !ok || recovery.Class != EventRecovery {
			t.Fatalf("transition %s recovery=%s is not executable", transition.ID, transition.Interruption.Recovery)
		}
		if transition.Interruption.Recovery == "recovery.enter" {
			t.Fatalf("transition %s references deleted synthetic recovery entry", transition.ID)
		}
	}
}

func TestRegistryGraphEveryDeclaredPhaseCanReachMarkedOutcome(t *testing.T) {
	// control-law: every-reachable-managed-state-is-coreachable
	registry := Default()
	edges := map[model.ProtocolPhase][]model.ProtocolPhase{}
	states := map[model.ProtocolPhase]bool{}
	for _, transition := range registry.All() {
		for _, source := range transition.SourcePhases {
			states[source] = true
			for _, target := range transition.TargetPhases {
				edges[source] = append(edges[source], target)
				states[target] = true
			}
		}
	}
	marked := map[model.ProtocolPhase]bool{model.PhaseFrontier: true, model.PhaseTerminal: true, model.PhaseAbandoned: true}
	for state := range states {
		if !canReachMarked(state, edges, marked, map[model.ProtocolPhase]bool{}) {
			t.Errorf("phase %s has no catalog path to a marked outcome", state)
		}
	}
}

func canReachMarked(state model.ProtocolPhase, edges map[model.ProtocolPhase][]model.ProtocolPhase, marked, visiting map[model.ProtocolPhase]bool) bool {
	if marked[state] {
		return true
	}
	if visiting[state] {
		return false
	}
	visiting[state] = true
	defer delete(visiting, state)
	for _, next := range edges[state] {
		if canReachMarked(next, edges, marked, visiting) {
			return true
		}
	}
	return false
}
