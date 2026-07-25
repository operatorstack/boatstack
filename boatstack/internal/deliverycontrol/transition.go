// Package deliverycontrol is the shadow declaration of Boatstack's delivery
// state machine: the states an owned delivery moves through and the transitions
// (the moves an agent makes) between them.
//
// It exists to make J_flow — the cost of navigating the deterministic delivery
// workflow the tool already owns — computable, following the split
// J = J_flow + J_coding established in
// ../../notes/delivery-flow-navigation-model.md. This is the "one declaration":
// a single, source-cited registry that later projects two ways that can never
// disagree — a conformance view (every transition names a real handler, every
// state mirrors a real literal) and an optimization view (a weighted graph a
// deterministic oracle scores).
//
// This package is shadow-only. Nothing imports it at runtime; it changes no
// command, gate, authority, verification, evidence, or recovery behavior, and
// the mutation boundary (mutation.go) is preserved unchanged. Later, opt-in
// phases add tracing, an oracle, and an advisory controller on top of this
// declaration.
package deliverycontrol

// StateID is a delivery-flow state. Slice-status states (see SliceStatusStates)
// mirror the string literals stored in DeliverySlice.Status by the real state
// machine; the remaining states name boundary conditions the registry needs to
// describe transitions faithfully (uninitialized, feature-complete, invalid,
// discarded), plus the UNRESOLVED sentinel an oracle must return for unknown
// state rather than fabricate a path.
type StateID string

// TransitionID is the stable, semantic identifier of a delivery transition,
// independent of the CLI verb or Go handler that currently implements it.
type TransitionID string

// TransitionKind classifies what a transition does to delivery state.
type TransitionKind string

const (
	KindObserve            TransitionKind = "observe"             // read-only; no state change
	KindInspect            TransitionKind = "inspect"             // read-only projection/derivation
	KindQuery              TransitionKind = "query"               // read-only decision (may hit the network)
	KindReversibleMutation TransitionKind = "reversible_mutation" // committed but closed under inversion / archived, not destroyed
	KindCommittedMutation  TransitionKind = "committed_mutation"  // advances delivery state
	KindRecovery           TransitionKind = "recovery"            // typed, bounded correction path
)

// TransitionCostClass names the cost bucket a transition draws from. Weights
// live in FlowCostWeights; the cmg model
// (../../notes/delivery-flow-navigation-model.md) prices a normal
// move/observe/inspect at 1 and a denied/blocked committed mutation (friction)
// at 3.
type TransitionCostClass string

const (
	CostObserve  TransitionCostClass = "observe"
	CostInspect  TransitionCostClass = "inspect"
	CostQuery    TransitionCostClass = "query"
	CostMutation TransitionCostClass = "mutation"
	CostRecovery TransitionCostClass = "recovery"
	CostFriction TransitionCostClass = "friction"
)

// TransitionDescriptor is one row of the delivery-flow inventory
// (../../notes/delivery-control-inventory.md), declared once here.
type TransitionDescriptor struct {
	ID         TransitionID
	From       []StateID // source states; empty means "from an uninitialized/any delivery" (see Note)
	To         StateID   // resulting delivery-flow state; "" when the transition changes no slice status
	Kind       TransitionKind
	CostClass  TransitionCostClass
	Reversible bool
	HandlerRef string // name of the real exported boatstack function that performs it
	CLIVerb    string // the boatstack-helper subcommand that invokes it ("" if none)
	Note       string // guard / authority / recovery summary, from the inventory
}

// AllKinds enumerates the valid transition kinds so conformance checks can
// reject a descriptor that names an undeclared kind.
func AllKinds() []TransitionKind {
	return []TransitionKind{KindObserve, KindInspect, KindQuery, KindReversibleMutation, KindCommittedMutation, KindRecovery}
}

// AllCostClasses enumerates the valid cost classes for the same reason.
func AllCostClasses() []TransitionCostClass {
	return []TransitionCostClass{CostObserve, CostInspect, CostQuery, CostMutation, CostRecovery, CostFriction}
}
