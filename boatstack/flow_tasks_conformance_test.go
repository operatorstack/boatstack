package boatstack

import "testing"

// control-law: sub-action-respects-plan-dag-and-slice-scope
//
// `flow tasks` (and the sub_action hint on `flow next`) orders the active slice's
// sub-actions from the compiled plan DAG. The contract: the ordering is a valid
// topological order of the intra-slice depends_on edges (Positive); it never
// surfaces a task outside the active slice, nor pulls in a foreign dependency
// (Relation/Bypass); the pointed-at "start here" task has no unmet intra-slice
// dependency (Negative); and a missing DAG or position resolves to nothing, never
// a guessed sub-action (Failure-state).

// A representative plan: two slices, an intentionally out-of-plan-order pair, a
// cross-slice dependency, and a dependency on an earlier slice's task.
func sampleTasks() []FlowTask {
	return []FlowTask{
		{ID: "a2", DependsOn: []string{"a1"}},       // listed before its dependency
		{ID: "a1", DependsOn: nil},                  // root of slice A
		{ID: "b1", DependsOn: []string{"a2"}},       // slice B — later slice
		{ID: "a3", DependsOn: []string{"x0", "a1"}}, // x0 is an earlier slice's (out-of-scope) task
	}
}

var sliceAScope = []string{"a1", "a2", "a3"}

// assertTopoValid checks that every intra-scope dependency of each task appears
// before it in the ordering.
func assertTopoValid(t *testing.T, ordered []FlowTask) {
	t.Helper()
	position := map[string]int{}
	for i, task := range ordered {
		position[task.ID] = i
	}
	for i, task := range ordered {
		for _, dep := range task.DependsOn {
			depPos, ok := position[dep]
			if !ok {
				t.Errorf("task %s retains out-of-scope dependency %s", task.ID, dep)
				continue
			}
			if depPos >= i {
				t.Errorf("task %s at %d precedes its dependency %s at %d", task.ID, i, dep, depPos)
			}
		}
	}
}

// Positive: the ordering is a valid topological order and starts at the slice's
// dependency root, even though the plan listed a2 before a1.
func TestOrderSliceTasksIsTopological(t *testing.T) {
	ordered := orderSliceTasks(sliceAScope, sampleTasks())
	if len(ordered) != 3 {
		t.Fatalf("expected 3 scoped tasks, got %d: %+v", len(ordered), ordered)
	}
	if ordered[0].ID != "a1" {
		t.Errorf("start-here should be the dependency root a1, got %s", ordered[0].ID)
	}
	assertTopoValid(t, ordered)
}

// Relation + Bypass: every ordered task is in the active slice's scope; the
// later-slice task b1 never appears, and the out-of-scope dependency x0 is dropped
// rather than pulling a foreign task into the ordering.
func TestOrderSliceTasksStaysInScope(t *testing.T) {
	scope := map[string]bool{}
	for _, id := range sliceAScope {
		scope[id] = true
	}
	ordered := orderSliceTasks(sliceAScope, sampleTasks())
	for _, task := range ordered {
		if !scope[task.ID] {
			t.Errorf("ordered task %s is outside the active slice scope", task.ID)
		}
		if task.ID == "b1" {
			t.Error("later-slice task b1 must never be ordered for slice A")
		}
		for _, dep := range task.DependsOn {
			if !scope[dep] {
				t.Errorf("task %s kept out-of-scope dependency %s", task.ID, dep)
			}
		}
	}
}

// Negative: the pointed-at start task has no unmet intra-scope dependency (it is a
// legal place to begin), and no later-slice task is ever the start.
func TestOrderSliceTasksStartIsBeginnable(t *testing.T) {
	ordered := orderSliceTasks(sliceAScope, sampleTasks())
	if len(ordered) == 0 {
		t.Fatal("expected a non-empty ordering")
	}
	start := ordered[0]
	if len(start.DependsOn) != 0 {
		t.Errorf("start-here task %s still has unmet intra-scope dependencies %v", start.ID, start.DependsOn)
	}
}

// Failure-state: with no delivery state and no compiled task graph, the reader
// resolves to nothing — never a fabricated ordering.
func TestFlowTasksUnresolvedWithoutDAG(t *testing.T) {
	tasks, err := FlowTasksForActiveSlice(t.TempDir(), "demo")
	if err != nil {
		t.Fatalf("read-only reader must not error on a missing feature: %v", err)
	}
	if tasks.Resolved || len(tasks.Ordered) != 0 || tasks.StartHere != "" {
		t.Errorf("missing DAG must be unresolved with no tasks; got %+v", tasks)
	}
	if tasks.Reason == "" {
		t.Error("an unresolved result must carry a reason")
	}
}
