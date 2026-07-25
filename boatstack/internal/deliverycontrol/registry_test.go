package deliverycontrol

import "testing"

// control-law: deliverycontrol-registry-well-formed
// The single declaration must be internally consistent: unique ids, declared
// states on every edge, valid kinds/cost classes, and a defined cost weight for
// every class a transition uses.
func TestRegistryWellFormed(t *testing.T) {
	states := map[StateID]bool{}
	for _, s := range States() {
		states[s] = true
	}
	kinds := map[TransitionKind]bool{}
	for _, k := range AllKinds() {
		kinds[k] = true
	}
	classes := map[TransitionCostClass]bool{}
	for _, c := range AllCostClasses() {
		classes[c] = true
	}
	weights := DefaultFlowCostWeights()

	seen := map[TransitionID]bool{}
	for _, tr := range Transitions() {
		if tr.ID == "" {
			t.Errorf("transition with empty ID: %+v", tr)
		}
		if seen[tr.ID] {
			t.Errorf("duplicate transition ID %q", tr.ID)
		}
		seen[tr.ID] = true
		if !kinds[tr.Kind] {
			t.Errorf("%s: undeclared kind %q", tr.ID, tr.Kind)
		}
		if !classes[tr.CostClass] {
			t.Errorf("%s: undeclared cost class %q", tr.ID, tr.CostClass)
		}
		if _, ok := weights.Cost(tr.CostClass); !ok {
			t.Errorf("%s: cost class %q has no weight", tr.ID, tr.CostClass)
		}
		for _, from := range tr.From {
			if !states[from] {
				t.Errorf("%s: undeclared From state %q", tr.ID, from)
			}
		}
		if tr.To != "" && !states[tr.To] {
			t.Errorf("%s: undeclared To state %q", tr.ID, tr.To)
		}
		if tr.HandlerRef == "" {
			t.Errorf("%s: empty HandlerRef", tr.ID)
		}
	}
	if len(seen) == 0 {
		t.Fatal("registry is empty")
	}
}

// The cmg model only makes friction expensive; if friction ever costs no more
// than a move, regret vanishes and the whole model is meaningless.
func TestFrictionCostsMoreThanAMove(t *testing.T) {
	w := DefaultFlowCostWeights()
	move, ok := w.Cost(CostObserve)
	if !ok {
		t.Fatal("observe cost undefined")
	}
	friction, ok := w.Cost(CostFriction)
	if !ok {
		t.Fatal("friction cost undefined")
	}
	if friction <= move {
		t.Errorf("friction (%d) must cost more than a move (%d)", friction, move)
	}
}

func TestSliceStatusStatesAreDeclared(t *testing.T) {
	declared := map[StateID]bool{}
	for _, s := range States() {
		declared[s] = true
	}
	for _, s := range SliceStatusStates() {
		if !declared[s] {
			t.Errorf("slice-status state %q is not in States()", s)
		}
	}
}
