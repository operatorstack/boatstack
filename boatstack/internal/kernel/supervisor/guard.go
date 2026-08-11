package supervisor

import (
	"fmt"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

type IntentClass string

const (
	IntentOrdinary      IntentClass = "ordinary"
	IntentManagedBypass IntentClass = "managed-bypass"
	IntentDestructive   IntentClass = "destructive"
)

type CommandIntent struct {
	Class       IntentClass          `json:"class"`
	Operation   string               `json:"operation"`
	Fingerprint string               `json:"fingerprint"`
	Transition  catalog.TransitionID `json:"transition,omitempty"`
}

func (i CommandIntent) Validate() error {
	if i.Fingerprint == "" || i.Operation == "" {
		return fmt.Errorf("command intent requires operation and fingerprint")
	}
	switch i.Class {
	case IntentOrdinary, IntentDestructive:
		if i.Transition != "" {
			return fmt.Errorf("%s intent cannot name a managed transition", i.Class)
		}
	case IntentManagedBypass:
		if i.Transition == "" {
			return fmt.Errorf("managed-bypass intent requires a transition")
		}
	default:
		return fmt.Errorf("invalid command intent class %q", i.Class)
	}
	return nil
}

type GuardDecision struct {
	Allowed            bool                 `json:"allowed"`
	Intent             CommandIntent        `json:"intent"`
	RequiredTransition catalog.TransitionID `json:"required_transition,omitempty"`
	Reason             string               `json:"reason"`
}

// Guard is the only hook safety law. Hosts provide raw commands to the surface
// classifier and consume this result; they do not reconstruct engagement or
// lifecycle state themselves.
func (s Supervisor) Guard(snapshot model.Snapshot, intent CommandIntent) GuardDecision {
	decision := GuardDecision{Intent: intent}
	if err := intent.Validate(); err != nil || snapshot.Fingerprint == "" {
		decision.Reason = "command intent or canonical snapshot is invalid"
		return decision
	}
	if intent.Class == IntentDestructive {
		decision.Reason = "high-confidence destructive operation requires an explicit registered recovery or cleanup transition"
		return decision
	}
	if intent.Class == IntentOrdinary {
		decision.Allowed = true
		decision.Reason = "ordinary repository operation is outside managed effect authority"
		return decision
	}
	decision.RequiredTransition = intent.Transition
	if snapshot.Engagement.Status != model.FactKnown {
		decision.Reason = "managed-effect engagement is unresolved"
		return decision
	}
	if snapshot.Engagement.Value == model.EngagementCommand || snapshot.Engagement.Value == model.EngagementActive || snapshot.Phase.Value == model.PhaseRecovery {
		decision.Reason = "managed effect must be requested through kernel admission"
		return decision
	}
	decision.Allowed = true
	decision.Reason = "Boatstack is dormant; direct non-destructive repository operation remains inert"
	return decision
}
