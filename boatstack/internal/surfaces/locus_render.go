package surfaces

import (
	"encoding/json"
	"sort"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

type locusEvidence struct {
	Path string `json:"path"`
	Note string `json:"note"`
}

type locusState struct {
	ID     string `json:"id"`
	Marked bool   `json:"marked,omitempty"`
}

type locusEvent struct {
	ID           string `json:"id"`
	Controllable bool   `json:"controllable"`
	Observable   bool   `json:"observable"`
	Basis        string `json:"basis"`
}

type locusTransition struct {
	From     string `json:"from"`
	Event    string `json:"event"`
	To       string `json:"to"`
	Guard    string `json:"guard,omitempty"`
	Evidence []int  `json:"evidence"`
	Basis    string `json:"basis"`
}

type locusSpec struct {
	Description          string     `json:"description"`
	ForbiddenStates      []string   `json:"forbidden_states"`
	ForbiddenTransitions []struct{} `json:"forbidden_transitions"`
}

type locusMutation struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	GuardID string `json:"guard_id"`
}

type locusModel struct {
	SchemaVersion int               `json:"schema_version"`
	ID            string            `json:"id"`
	Subject       string            `json:"subject"`
	Evidence      []locusEvidence   `json:"evidence"`
	States        []locusState      `json:"states"`
	Events        []locusEvent      `json:"events"`
	Transitions   []locusTransition `json:"transitions"`
	Spec          locusSpec         `json:"spec"`
	Targets       struct {
		Selector string `json:"selector"`
	} `json:"targets"`
	Mutation *locusMutation `json:"mutation,omitempty"`
	Unknowns []string       `json:"unknowns"`
}

func RenderCatalogLocusSafety(transitions []catalog.Transition) (string, error) {
	return renderCatalogLocus(transitions, true)
}

func RenderCatalogLocusLiveness(transitions []catalog.Transition) (string, error) {
	return renderCatalogLocus(transitions, false)
}

func renderCatalogLocus(transitions []catalog.Transition, safety bool) (string, error) {
	ordered := append([]catalog.Transition(nil), transitions...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	usedPhases := map[model.ProtocolPhase]bool{}
	for _, transition := range ordered {
		for _, phase := range transition.SourcePhases {
			usedPhases[phase] = true
		}
		for _, phase := range transition.TargetPhases {
			usedPhases[phase] = true
		}
	}
	result := locusModel{
		SchemaVersion: 1,
		ID:            "boatstack-v2-executable-catalog-liveness-v1",
		Subject:       "Finite stable-phase abstraction generated from the executable Boatstack V2 registry. It contains one event for every runtime catalog entry and expands each declared source and target phase set. The 17-facet predicates, operating-system behavior, and external-provider truth remain executable evidence obligations rather than theorem assumptions.",
		Evidence: []locusEvidence{
			{Path: "boatstack/internal/kernel/catalog/default.go", Note: "Executable registry, exact transition count, event classes, phase predicates, authority, and materialization."},
			{Path: "docs/architecture/boatstack-v2-transition-catalog.md", Note: "Generated readable projection from the same runtime registry."},
			{Path: "boatstack/internal/kernel/protocol/admission.go", Note: "Exact admission, authority, parameter, source-revision, provider-request, expiry, and stale-snapshot checks."},
			{Path: "boatstack/internal/kernel/engine/engine.go", Note: "Single apply path across lock, journal, effect, fresh observation, target predicate, receipt, and recovery."},
			{Path: "boatstack/internal/kernel/reducer/reducer.go", Note: "Single executable reducer for every controllable semantic transition."},
			{Path: "boatstack/internal/kernel/catalog/completeness_test.go", Note: "Runtime facet/event classification, writer-boundary inventory, and reducer-completeness refusing tests."},
			{Path: "boatstack/internal/kernel/engine/engine_test.go", Note: "Exact-admission, stale-snapshot, postcondition, interruption, idempotency, and unknown-outcome tests."},
			{Path: "boatstack/internal/effects/prepared.go", Note: "Staged effect ordering, atomic resource application, rollback, and external settlement boundary."},
			{Path: "boatstack/internal/kernel/catalog/historical_test.go", Note: "Historical incidents resolved through the executable runtime supervisor."},
		},
		Spec: locusSpec{
			Description:          "Every reachable stable catalog phase retains a path to terminal, explicit frontier, or safe abandonment.",
			ForbiddenStates:      []string{},
			ForbiddenTransitions: []struct{}{},
		},
		Unknowns: []string{
			"The stable-phase graph is a conservative expansion of declared source and target phase sets; facet combinations and deterministic reducer branches remain executable-test obligations.",
			"External provider state and operating-system crash behavior are represented by declared outcomes and restart tests, not exhaustively observed by Locus.",
		},
	}
	result.Targets.Selector = "marked"
	for _, phase := range model.ProtocolPhases() {
		if !usedPhases[phase] {
			continue
		}
		result.States = append(result.States, locusState{ID: string(phase), Marked: phase.IsCompletionTarget()})
	}
	for _, transition := range ordered {
		result.Events = append(result.Events, locusEvent{
			ID: string(transition.ID), Controllable: transition.Controllable(), Observable: true, Basis: "observed",
		})
		for _, source := range transition.SourcePhases {
			for _, target := range transition.TargetPhases {
				result.Transitions = append(result.Transitions, locusTransition{
					From: string(source), Event: string(transition.ID), To: string(target), Evidence: []int{0, 1, 4}, Basis: "inferred",
				})
			}
		}
	}
	if safety {
		result.ID = "boatstack-v2-executable-catalog-safety-v1"
		result.States = append(result.States, locusState{ID: "UNADMITTED_EFFECT"})
		result.Transitions = append(result.Transitions, locusTransition{
			From: "DORMANT", Event: "publication.execute", To: "UNADMITTED_EFFECT", Guard: "exact-admission", Evidence: []int{2, 3, 6}, Basis: "inferred",
		})
		result.Spec.Description = "Managed publication is unreachable from DORMANT without the exact-admission guard."
		result.Spec.ForbiddenStates = []string{"UNADMITTED_EFFECT"}
		result.Mutation = &locusMutation{ID: "remove-exact-admission", Kind: "remove-guard", GuardID: "exact-admission"}
	}
	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", err
	}
	return string(append(raw, '\n')), nil
}
