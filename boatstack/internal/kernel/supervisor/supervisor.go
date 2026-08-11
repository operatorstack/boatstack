package supervisor

import (
	"fmt"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

type DecisionKind string

const (
	DecisionPrescribed DecisionKind = "PRESCRIBED"
	DecisionCandidate  DecisionKind = "CANDIDATE"
	DecisionTerminal   DecisionKind = "TERMINAL"
	DecisionFrontier   DecisionKind = "FRONTIER"
	DecisionBlocked    DecisionKind = "BLOCKED"
	DecisionRefused    DecisionKind = "REFUSED"
	DecisionUnresolved DecisionKind = "UNRESOLVED"
)

type Decision struct {
	Kind                DecisionKind           `json:"kind"`
	SnapshotFingerprint string                 `json:"snapshot_fingerprint"`
	Transition          *catalog.Transition    `json:"transition,omitempty"`
	Candidates          []catalog.TransitionID `json:"candidates,omitempty"`
	Reason              string                 `json:"reason"`
}

type Supervisor struct {
	registry  catalog.Registry
	contracts catalog.GoalContracts
}

func New(registry catalog.Registry, contracts catalog.GoalContracts) Supervisor {
	return Supervisor{registry: registry, contracts: contracts}
}

func (s Supervisor) Resolve(snapshot model.Snapshot, goal model.Goal, authority catalog.AuthoritySet, requested catalog.TransitionID) Decision {
	base := Decision{SnapshotFingerprint: snapshot.Fingerprint}
	if err := goal.Validate(); err != nil || snapshot.Fingerprint == "" {
		base.Kind, base.Reason = DecisionUnresolved, "goal or canonical snapshot is invalid"
		return base
	}
	if snapshot.Terminal.Status != model.FactKnown || snapshot.Phase.Status != model.FactKnown {
		base.Kind, base.Reason = DecisionUnresolved, "terminal or phase evidence is not known"
		return base
	}
	if snapshot.Program.Status != model.FactKnown {
		base.Kind, base.Reason = DecisionUnresolved, "compiled control program evidence is not known"
		return base
	}
	if snapshot.Program.Value == model.ProgramDrift {
		transition, ok := s.registry.Lookup(requested)
		if !ok || !transition.Policy.ReconcilesProgram {
			base.Kind, base.Reason = DecisionUnresolved, "compiled control program drift requires explicit reconciliation"
			return base
		}
	}
	if snapshot.ConfigurationPolicy.Status == model.FactKnown && !hostEnabled(snapshot.ConfigurationPolicy.Value.Hosts, snapshot.Invocation.Host) {
		base.Kind, base.Reason = DecisionRefused, fmt.Sprintf("host %q is not enabled by repository policy", snapshot.Invocation.Host)
		return base
	}
	if requested == "" && s.contracts.Matches(snapshot, goal) {
		base.Kind, base.Reason = DecisionTerminal, "configured terminal is established by current evidence"
		return base
	}
	admissible := s.registry.Admissible(snapshot, goal)
	if snapshot.Goal.Status == model.FactKnown && snapshot.Goal.Value != goal {
		filtered := admissible[:0]
		for _, candidate := range admissible {
			if candidate.Policy.BindsRequestedGoal {
				filtered = append(filtered, candidate)
			}
		}
		admissible = filtered
	}
	if snapshot.Phase.Value == model.PhaseRecovery {
		filtered := admissible[:0]
		for _, candidate := range admissible {
			if candidate.Class == catalog.EventRecovery {
				filtered = append(filtered, candidate)
			}
		}
		admissible = filtered
	}
	if requested != "" {
		transition, ok := s.registry.Lookup(requested)
		if !ok || !transition.Controllable() {
			base.Kind, base.Reason = DecisionRefused, fmt.Sprintf("transition %q is not a controllable registry event", requested)
			return base
		}
		found := false
		for _, candidate := range admissible {
			if candidate.ID == requested {
				transition, found = candidate, true
				break
			}
		}
		if !found {
			base.Kind, base.Reason = DecisionRefused, fmt.Sprintf("transition %q is not admissible from current snapshot", requested)
			return base
		}
		if allowed, reason := policyAllows(snapshot, transition); !allowed {
			base.Kind, base.Reason = DecisionRefused, reason
			return base
		}
		if !authoritySatisfies(snapshot, transition, authority) {
			base.Kind, base.Reason = DecisionFrontier, fmt.Sprintf("transition %q requires unavailable authority", requested)
			base.Candidates = []catalog.TransitionID{requested}
			return base
		}
		base.Kind, base.Reason, base.Transition = DecisionPrescribed, "requested transition is admissible", &transition
		return base
	}
	selectable := make([]catalog.Transition, 0, len(admissible))
	for _, candidate := range admissible {
		if !candidate.ImplicitlySelectable() || targetAlreadySatisfied(snapshot, goal, candidate) {
			continue
		}
		if allowed, _ := policyAllows(snapshot, candidate); allowed {
			selectable = append(selectable, candidate)
		}
	}
	if len(selectable) == 0 {
		if snapshot.Phase.Value == model.PhaseRecovery || snapshot.Phase.Value == model.PhaseUnresolved {
			base.Kind, base.Reason = DecisionBlocked, "no registered recovery transition is admissible"
			return base
		}
		base.Kind, base.Reason = DecisionUnresolved, "no goal-progressing transition is safely selectable from current evidence"
		return base
	}
	topClass := selectable[0].SelectionClass
	topPriority := selectable[0].Priority
	var top []catalog.Transition
	for _, candidate := range selectable {
		if candidate.SelectionClass == topClass && candidate.Priority == topPriority {
			top = append(top, candidate)
		}
	}
	if len(top) != 1 {
		base.Kind, base.Reason = DecisionFrontier, "several equally preferred transitions remain admissible"
		for _, candidate := range top {
			base.Candidates = append(base.Candidates, candidate.ID)
		}
		return base
	}
	if !authoritySatisfies(snapshot, top[0], authority) {
		base.Kind, base.Reason = DecisionFrontier, "next goal-progressing transition requires unavailable authority"
		base.Candidates = []catalog.TransitionID{top[0].ID}
		return base
	}
	base.Kind, base.Reason, base.Transition = DecisionPrescribed, "deterministic highest-priority transition", &top[0]
	return base
}

func targetAlreadySatisfied(snapshot model.Snapshot, goal model.Goal, transition catalog.Transition) bool {
	if transition.Policy.RechecksExternalState {
		return false
	}
	if transition.Policy.BindsRequestedGoal {
		return snapshot.Goal.Status == model.FactKnown && snapshot.Goal.Value == goal
	}
	if transition.Policy.RequiredWhen == "visual-evidence-required" &&
		(snapshot.ConfigurationPolicy.Status != model.FactKnown || snapshot.ConfigurationPolicy.Value.VisualEvidence != "required") {
		return true
	}
	if transition.Policy.CurrentEvidencePrefix != "" {
		return currentEvidenceRecorded(snapshot, transition.Policy.CurrentEvidencePrefix)
	}
	return transition.TargetMatches(snapshot)
}

func currentEvidenceRecorded(snapshot model.Snapshot, sourcePrefix string) bool {
	if snapshot.Verification.Status != model.FactKnown || snapshot.Verification.Value != model.VerificationCurrent {
		return false
	}
	for _, evidence := range snapshot.Verification.Evidence {
		if strings.HasPrefix(evidence.Source, sourcePrefix) {
			return true
		}
	}
	return false
}

func hostEnabled(hosts []string, host string) bool {
	for _, candidate := range hosts {
		if candidate == host {
			return true
		}
	}
	return false
}

func authoritySatisfies(snapshot model.Snapshot, transition catalog.Transition, authority catalog.AuthoritySet) bool {
	if !authority.Satisfies(transition.Authority, transition.AuthorityAll) {
		return false
	}
	if transition.Policy.AuthorityRule == "plan-approval" {
		if snapshot.ConfigurationPolicy.Status != model.FactKnown {
			return false
		}
		if snapshot.ConfigurationPolicy.Value.PlanApproval == "human" && !authority[catalog.AuthorityHuman] {
			return false
		}
	}
	if transition.Policy.AuthorityRule == "independent-high-risk-review" {
		if snapshot.ConfigurationPolicy.Status != model.FactKnown {
			return false
		}
		policy := snapshot.ConfigurationPolicy.Value
		if policy.IndependentReviewForHighRisk && policy.HighRiskChange && !authority[catalog.AuthorityHuman] {
			return false
		}
	}
	return true
}

func policyAllows(snapshot model.Snapshot, transition catalog.Transition) (bool, string) {
	if transition.Class == catalog.EventRecovery && snapshot.RecoveryInfo.Status == model.FactKnown {
		permitted := false
		for _, candidate := range snapshot.RecoveryInfo.Value.Permitted {
			if candidate == string(transition.ID) {
				permitted = true
				break
			}
		}
		if !permitted {
			return false, fmt.Sprintf("recovery transition %q is not permitted for transaction %q", transition.ID, snapshot.RecoveryInfo.Value.TransactionID)
		}
	}
	if transition.Policy.AvailabilityRule == "" {
		return true, ""
	}
	if snapshot.ConfigurationPolicy.Status != model.FactKnown {
		return false, fmt.Sprintf("transition %q requires known configuration policy", transition.ID)
	}
	if transition.Policy.AvailabilityRule == "visual-evidence-enabled" && snapshot.ConfigurationPolicy.Value.VisualEvidence == "off" {
		return false, "visual evidence is disabled by repository policy"
	}
	return true, ""
}
