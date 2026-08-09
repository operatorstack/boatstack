package boatstack

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

// A law states what is admissible; a weak model cannot always derive a
// compliant action from that statement. The solution set is the law compiled
// into its concrete admissible actions, so a caller PICKS a legal move instead
// of deriving one. It is computed from the same declarations the guard
// enforces — the transition registry, the guard's stage-verb tables, the
// planning prescription layer — never from a hand-written list, so it cannot
// drift from the law (the oracle-as-advisor rule: the checker is exposed as a
// constructive advisor at the authoring boundary, not only as a terminal gate).
// control-law: solution-set-derives-from-guard-declarations

// solutionSetCap bounds the structured payload; solutionSetTextCap bounds the
// plain-text rendering so the response contract stays scannable.
const (
	solutionSetCap     = 8
	solutionSetTextCap = 3
)

// SolutionSet is the computed enumeration of admissible next commands from a
// flow position. Options are ordered most-productive first: mutations by total
// remaining flow cost through the move, then observations.
type SolutionSet struct {
	Basis     string                  `json:"basis"`
	Stage     string                  `json:"stage,omitempty"`
	State     deliverycontrol.StateID `json:"state,omitempty"`
	Options   []PrescribedCommand     `json:"options"`
	Truncated bool                    `json:"truncated,omitempty"`
}

// enumerateFlowSolutions computes the solution set for a flow position: the
// delivery graph's out-edges when the oracle resolves the state, the guard's
// pre-activation stage tables when it does not.
func enumerateFlowSolutions(repo string, status NextStatus, next FlowNext) SolutionSet {
	if next.Resolved {
		return enumerateDeliverySolutions(repo, status, next.State)
	}
	return enumeratePlanningSolutions(repo, status, next)
}

// alternativesFor is the FlowNext carrier hook: the full solution set minus the
// single Prescribed primary. Advisory only — it adds no verb the registry or
// the guard tables do not already admit.
func alternativesFor(repo string, status NextStatus, next FlowNext) []PrescribedCommand {
	set := enumerateFlowSolutions(repo, status, next)
	if next.Prescribed == nil {
		return set.Options
	}
	primary := prescriptionKey(*next.Prescribed)
	options := make([]PrescribedCommand, 0, len(set.Options))
	for _, option := range set.Options {
		if prescriptionKey(option) == primary {
			continue
		}
		options = append(options, option)
	}
	return options
}

// enumerateDeliverySolutions enumerates from a resolved delivery state: every
// registry out-edge that can be assembled faithfully, ordered by the total
// remaining cost through the edge (edge cost + shortest path from its target to
// the goal, unreachable targets last, ties by transition ID), then the observe
// rows admissible from this state. ignore-delivery is deliberately absent: it
// is a policy filter over ResolveNext, not a move on the delivery walk.
func enumerateDeliverySolutions(repo string, status NextStatus, state deliverycontrol.StateID) SolutionSet {
	set := SolutionSet{Basis: "flow-position", State: state}
	graph := deliverycontrol.RegistryGraph(deliverycontrol.DefaultFlowCostWeights())

	type scoredEdge struct {
		edge      deliverycontrol.Edge
		total     int
		reachable bool
	}
	edges := make([]scoredEdge, 0)
	for _, edge := range graph.Out(state) {
		scored := scoredEdge{edge: edge}
		if path := graph.ShortestFlow(edge.To, flowGoal); path.Resolution == deliverycontrol.Resolved {
			scored.reachable = true
			scored.total = edge.Cost + path.Cost
		}
		edges = append(edges, scored)
	}
	sort.SliceStable(edges, func(i, j int) bool {
		if edges[i].reachable != edges[j].reachable {
			return edges[i].reachable
		}
		if edges[i].total != edges[j].total {
			return edges[i].total < edges[j].total
		}
		return edges[i].edge.Transition < edges[j].edge.Transition
	})
	for _, scored := range edges {
		if cmd, ok := prescribeCommand(repo, status.Feature, status, scored.edge.Transition); ok {
			appendSolution(&set, *cmd)
		}
	}

	for _, descriptor := range deliverycontrol.Transitions() {
		if descriptor.To != "" {
			continue
		}
		if descriptor.CostClass != deliverycontrol.CostObserve && descriptor.CostClass != deliverycontrol.CostQuery {
			continue
		}
		if descriptor.From != nil && !transitionAccepts(descriptor, state) {
			continue
		}
		if cmd, ok := prescribeObserve(repo, status.Feature, descriptor); ok {
			appendSolution(&set, *cmd)
		}
	}
	return set
}

// enumeratePlanningSolutions enumerates from a pre-activation or blocked stage:
// the stage's prescribed primary, the other mutation verbs the guard's
// stageMutationVerbs table admits there, and the planning-relevant read-only
// helpers. Every mutation verb comes from the SAME table the guard checks, so
// this list is closed under guard admission by construction.
func enumeratePlanningSolutions(repo string, status NextStatus, next FlowNext) SolutionSet {
	set := SolutionSet{Basis: "flow-position", Stage: status.ObservedStage}
	if next.Prescribed != nil {
		appendSolution(&set, *next.Prescribed)
	}
	for _, verb := range stageMutationVerbs[status.ObservedStage] {
		// workspace-cut gains authority from a successful plan check, not from
		// DRAFT_PLAN alone. Keep it out of the alternative set unless ResolveNext
		// selected that exact transition for the current validated package.
		if verb == "workspace-cut" && status.NextOperation != "workspace-cut" {
			continue
		}
		if cmd, ok := prescribePlanningVerb(repo, status, verb); ok {
			appendSolution(&set, *cmd)
		}
	}
	// The stage-independent recovery verbs relevant to a blocked plan, then the
	// position observers. Both are admitted at every stage by the guard tables.
	if status.ObservedStage == "INVALID_STATE" {
		if cmd, ok := prescribePlanningVerb(repo, status, "repair-state"); ok {
			appendSolution(&set, *cmd)
		}
	}
	if descriptor, ok := deliverycontrol.Transition(deliverycontrol.TransitionID("delivery.next")); ok {
		if cmd, ok := prescribeObserve(repo, "", descriptor); ok {
			appendSolution(&set, *cmd)
		}
	}
	appendSolution(&set, PrescribedCommand{
		Verb: "doctor", Args: repoFlagArgs(repo), AutoDerivable: true,
		Transition: MarkerRecoveryDoctor,
	})
	return set
}

// prescribePlanningVerb assembles the faithful command for one admissible
// pre-activation verb. Shapes shared with prescribePlanning go through the
// same builders; anything it cannot assemble faithfully is omitted, never
// guessed.
func prescribePlanningVerb(repo string, status NextStatus, verb string) (*PrescribedCommand, bool) {
	repoArgs := repoFlagArgs(repo)
	featureDir := planningFeatureDir(repo, status.Feature)
	var cmd *PrescribedCommand
	switch verb {
	case "planning-write":
		cmd = &PrescribedCommand{Verb: verb, Args: repoArgs, Transition: MarkerPlanningWrite}
		if status.Feature != "" {
			cmd.Args = append(cmd.Args, "--feature", status.Feature)
		} else {
			cmd.RequiresHumanInput = append(cmd.RequiresHumanInput, "--feature")
		}
		// The artifact name and its Markdown (stdin) are authored content — owed.
		cmd.RequiresHumanInput = append(cmd.RequiresHumanInput, "--artifact")
		cmd.RequiresHumanInput = append(cmd.RequiresHumanInput, planningMarkdownInput)
	case "record-approval":
		if status.Feature == "" {
			return nil, false
		}
		cmd = &PrescribedCommand{
			Verb: verb,
			Args: []string{"--plan", filepath.Join(featureDir, "plan.md")},
			// Approval facts are a human act; never fabricated.
			RequiresHumanInput: []string{"--approved-by", "--approved-at", "--fingerprint"},
			Transition:         MarkerPlanningApproval,
		}
	case "activate-plan":
		if status.Feature == "" {
			return nil, false
		}
		cmd = buildActivatePlan(featureDir, status.ObservedStage)
	case "workspace-cut":
		if status.Feature == "" {
			return nil, false
		}
		cmd = buildWorkspaceCut(repoArgs, status.Feature)
	case "repair-state":
		cmd = &PrescribedCommand{Verb: verb, Args: repoArgs, Transition: MarkerRecoveryRepair}
		if status.Feature != "" {
			cmd.Args = append(cmd.Args, "--feature", status.Feature)
		}
	default:
		return nil, false
	}
	cmd.AutoDerivable = len(cmd.RequiresHumanInput) == 0
	return cmd, true
}

// prescribeObserve assembles a read-only observation command for a registry row
// with no target state. Observations never outrank a mutation in the pick list,
// but they must be present — inspecting is the legal move that costs nothing
// when the next mutation is not yet clear.
func prescribeObserve(repo, feature string, descriptor deliverycontrol.TransitionDescriptor) (*PrescribedCommand, bool) {
	if descriptor.CLIVerb == "" {
		return nil, false
	}
	cmd := &PrescribedCommand{Verb: descriptor.CLIVerb, Transition: descriptor.ID, AutoDerivable: true}
	cmd.Args = repoFlagArgs(repo)
	switch descriptor.ID {
	case deliverycontrol.TransitionID("delivery.status"), deliverycontrol.TransitionID("delivery.check_ship"):
		if feature == "" {
			return nil, false
		}
		cmd.Args = append(cmd.Args, "--feature", feature)
	case deliverycontrol.TransitionID("delivery.next"), deliverycontrol.TransitionID("delivery.recovery_status"):
		if feature != "" {
			cmd.Args = append(cmd.Args, "--feature", feature)
		}
	default:
		return nil, false
	}
	return cmd, true
}

// appendSolution adds an option, deduplicating on the rendered verb+args
// identity and enforcing the structured cap.
func appendSolution(set *SolutionSet, option PrescribedCommand) {
	key := prescriptionKey(option)
	for _, existing := range set.Options {
		if prescriptionKey(existing) == key {
			return
		}
	}
	if len(set.Options) >= solutionSetCap {
		set.Truncated = true
		return
	}
	set.Options = append(set.Options, option)
}

// prescriptionKey is the dedup identity of a prescribed command.
func prescriptionKey(p PrescribedCommand) string {
	return p.Verb + "\x00" + strings.Join(p.Args, "\x00")
}

// repoFlagArgs mirrors the prescription layer's convention: --repo appears only
// when it differs from the default working directory.
func repoFlagArgs(repo string) []string {
	if repo != "" && repo != "." {
		return []string{"--repo", repo}
	}
	return nil
}

// transitionAccepts reports whether a registry row's From set contains a state.
func transitionAccepts(descriptor deliverycontrol.TransitionDescriptor, state deliverycontrol.StateID) bool {
	for _, from := range descriptor.From {
		if from == state {
			return true
		}
	}
	return false
}

// solutionGloss names a move's purpose in one STE word or two, keyed by the
// transition family — used only in the compact text rendering.
func solutionGloss(transition deliverycontrol.TransitionID) string {
	switch transition {
	case deliverycontrol.TransitionID("delivery.record_gate_test"):
		return "record test gate"
	case deliverycontrol.TransitionID("delivery.record_gate_review"):
		return "record review gate"
	case deliverycontrol.TransitionID("delivery.record_change"):
		return "rework"
	case deliverycontrol.TransitionID("delivery.publish"):
		return "publish"
	case deliverycontrol.TransitionID("delivery.undo"):
		return "reverse"
	case deliverycontrol.TransitionID("delivery.discard_delivery"), MarkerRecoveryDiscard:
		return "abandon"
	case deliverycontrol.TransitionID("delivery.repair_state"), MarkerRecoveryRepair:
		return "repair"
	case MarkerRecoveryDoctor:
		return "diagnose"
	}
	if strings.HasPrefix(string(transition), "planning.") {
		return "plan"
	}
	return "inspect"
}
