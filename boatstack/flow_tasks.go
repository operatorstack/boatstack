package boatstack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FlowTask is one read-only sub-action from the compiled plan's task DAG, scoped
// to a delivery slice. It carries only what is needed to order and name the work;
// it holds no completion state.
type FlowTask struct {
	ID        string   `json:"id"`
	Title     string   `json:"title,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`
}

// FlowTasks is the read-only ordering of the active slice's sub-actions. It adds
// no state and tracks no completion: it orders the plan's tasks by their DAG,
// scoped to the active slice, and points at the one to start. The agent decides
// done-ness. Resolved is false when the flow position or the compiled task graph
// cannot be read faithfully, in which case nothing is ordered or pointed at
// (never a guessed sub-action).
type FlowTasks struct {
	Resolved    bool       `json:"resolved"`
	Feature     string     `json:"feature,omitempty"`
	Slice       string     `json:"slice,omitempty"`
	SliceStatus string     `json:"slice_status,omitempty"`
	Ordered     []FlowTask `json:"ordered,omitempty"`
	StartHere   string     `json:"start_here,omitempty"`
	Reason      string     `json:"reason"`
}

// FlowTasksForActiveSlice reads the compiled task DAG for a feature, scopes it to
// the active delivery slice, and returns the sub-actions in dependency order. It
// is read-only and best-effort on the inputs: a missing delivery state, a fully
// published delivery, or an unreadable task graph resolves to an Unresolved result
// with a reason rather than an error, so it is safe to request at any time.
func FlowTasksForActiveSlice(repo, feature string) (FlowTasks, error) {
	state, err := LoadDeliveryState(repo, feature)
	if err != nil {
		return FlowTasks{Reason: "no readable delivery state for this feature"}, nil
	}
	slice, err := activeDeliverySlice(state)
	if err != nil {
		return FlowTasks{Feature: feature, Reason: err.Error()}, nil
	}
	out := FlowTasks{Feature: feature, Slice: slice.ID, SliceStatus: slice.Status}
	if len(slice.TaskIDs) == 0 {
		out.Reason = "active slice declares no task ids to order"
		return out, nil
	}
	allTasks, ok := readCompiledTasks(repo, feature)
	if !ok {
		out.Reason = "no readable compiled task graph (compiled/tasks.json)"
		return out, nil
	}
	ordered := orderSliceTasks(slice.TaskIDs, allTasks)
	if len(ordered) == 0 {
		out.Reason = "active slice task ids resolve to no compiled tasks"
		return out, nil
	}
	out.Ordered = ordered
	out.StartHere = ordered[0].ID
	out.Resolved = true
	out.Reason = fmt.Sprintf("%d sub-action(s) for slice %s in dependency order", len(ordered), slice.ID)
	return out, nil
}

// readCompiledTasks loads the tasks array from the feature's compiled task graph
// through the shared dual-layout artifact resolver. The boolean is false whenever
// the graph is absent or malformed, so the caller stays Unresolved rather than
// ordering a guess.
func readCompiledTasks(repo, feature string) ([]FlowTask, bool) {
	directory := WorkspaceFor(repo).FeatureDir(feature)
	tasksPath := featureArtifactPath(directory, filepath.Join("compiled", "tasks.json"), "tasks.json")
	raw, err := os.ReadFile(tasksPath)
	if err != nil {
		return nil, false
	}
	var graph struct {
		Tasks []struct {
			ID        string   `json:"id"`
			Title     string   `json:"title"`
			DependsOn []string `json:"depends_on"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(raw, &graph); err != nil {
		return nil, false
	}
	tasks := make([]FlowTask, 0, len(graph.Tasks))
	for _, t := range graph.Tasks {
		if strings.TrimSpace(t.ID) == "" {
			continue
		}
		tasks = append(tasks, FlowTask{ID: t.ID, Title: t.Title, DependsOn: t.DependsOn})
	}
	return tasks, true
}

// orderSliceTasks scopes the plan's tasks to the active slice's task ids and
// returns them in dependency order. Ordering is a Kahn topological sort over the
// intra-scope depends_on edges only — a dependency on an earlier slice's task is
// treated as already satisfied, and a dependency outside the scope never pulls a
// foreign task in. Ties are broken by the tasks' order in the compiled plan, so
// the result is deterministic and plan-faithful. The compile step already
// guarantees the DAG is acyclic; if a cycle somehow remained, the unresolved
// remainder is appended in plan order rather than dropped.
func orderSliceTasks(sliceTaskIDs []string, allTasks []FlowTask) []FlowTask {
	scope := map[string]bool{}
	for _, id := range sliceTaskIDs {
		scope[id] = true
	}

	// Scoped tasks in plan order, with dependencies restricted to the scope.
	var scoped []FlowTask
	planIndex := map[string]int{}
	indegree := map[string]int{}
	dependents := map[string][]string{}
	for _, task := range allTasks {
		if !scope[task.ID] {
			continue
		}
		intra := make([]string, 0, len(task.DependsOn))
		for _, dep := range task.DependsOn {
			if scope[dep] {
				intra = append(intra, dep)
			}
		}
		planIndex[task.ID] = len(scoped)
		indegree[task.ID] = len(intra)
		scoped = append(scoped, FlowTask{ID: task.ID, Title: task.Title, DependsOn: intra})
		for _, dep := range intra {
			dependents[dep] = append(dependents[dep], task.ID)
		}
	}

	emitted := map[string]bool{}
	var ordered []FlowTask
	for len(ordered) < len(scoped) {
		// Among not-yet-emitted tasks with no unmet intra-scope dependency, pick the
		// earliest in plan order.
		next := -1
		for _, task := range scoped {
			if emitted[task.ID] || indegree[task.ID] != 0 {
				continue
			}
			if next == -1 || planIndex[task.ID] < planIndex[scoped[next].ID] {
				next = planIndex[task.ID]
			}
		}
		if next == -1 {
			// Defensive: a residual cycle (should be impossible post-compile). Append
			// the remaining tasks in plan order rather than silently dropping them.
			for _, task := range scoped {
				if !emitted[task.ID] {
					ordered = append(ordered, task)
					emitted[task.ID] = true
				}
			}
			break
		}
		chosen := scoped[next]
		ordered = append(ordered, chosen)
		emitted[chosen.ID] = true
		for _, dependent := range dependents[chosen.ID] {
			indegree[dependent]--
		}
	}
	return ordered
}

// FormatFlowTasks renders the scoped, ordered sub-actions as human-facing lines.
// An unresolved result prints its reason rather than any task, so it is never
// mistaken for an empty plan.
func FormatFlowTasks(tasks FlowTasks) string {
	var b strings.Builder
	if !tasks.Resolved {
		fmt.Fprintf(&b, "Flow tasks: unresolved (%s)\n", tasks.Reason)
		return b.String()
	}
	fmt.Fprintf(&b, "Sub-actions for slice %s (%s), in dependency order:\n", tasks.Slice, tasks.SliceStatus)
	for i, task := range tasks.Ordered {
		marker := "  "
		if task.ID == tasks.StartHere {
			marker = "->"
		}
		title := task.Title
		if title != "" {
			title = " — " + title
		}
		fmt.Fprintf(&b, "%s %d. %s%s\n", marker, i+1, task.ID, title)
	}
	fmt.Fprintf(&b, "Start here: %s\n", tasks.StartHere)
	return b.String()
}
