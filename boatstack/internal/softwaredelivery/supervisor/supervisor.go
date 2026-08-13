package supervisor

import (
	"fmt"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	general "github.com/operatorstack/boatstack/boatstack/kernel"
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
	ReasonProgramDrift              = "compiled control program drift requires explicit reconciliation"
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
	contracts catalog.ObjectiveContracts
}

func New(registry catalog.Registry, contracts catalog.ObjectiveContracts) Supervisor {
	return Supervisor{registry: registry, contracts: contracts}
}

func (s Supervisor) Resolve(snapshot model.Snapshot, objective model.Objective, authority catalog.AuthoritySet, requested catalog.TransitionID) Decision {
	base := Decision{SnapshotFingerprint: snapshot.Fingerprint}
	objectiveAbsent := snapshot.Objective.Status == model.FactAbsent
	objectiveProvided := objective.ID != "" || objective.TargetID != "" || objective.TrustedClass != "" || objective.DeliveryID != ""
	if (objectiveProvided && objective.Validate() != nil) || (!objectiveProvided && !objectiveAbsent) || snapshot.Fingerprint == "" {
		base.Kind, base.Reason = DecisionUnresolved, "objective or canonical snapshot is invalid"
		return base
	}
	if objectiveProvided && !s.contracts.Accepts(objective) {
		base.Kind, base.Reason = DecisionRefused, "objective target and trusted class do not match the compiled program"
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
		if !ok || (!transition.Policy.ReconcilesProgram && !permittedProgramDriftRecovery(snapshot, transition)) {
			base.Kind, base.Reason = DecisionUnresolved, ReasonProgramDrift
			return base
		}
	}
	if snapshot.ConfigurationPolicy.Status == model.FactKnown && !hostEnabled(snapshot.ConfigurationPolicy.Value.Hosts, snapshot.Invocation.Host) {
		base.Kind, base.Reason = DecisionRefused, fmt.Sprintf("host %q is not enabled by repository policy", snapshot.Invocation.Host)
		return base
	}
	marked := s.contracts.Matches(snapshot, objective)
	admissible := s.registry.Admissible(snapshot, objective)
	if snapshot.Objective.Status == model.FactKnown && snapshot.Objective.Value != objective {
		filtered := admissible[:0]
		for _, candidate := range admissible {
			if candidate.Policy.BindsRequestedObjective {
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
	}
	byID := make(map[string]catalog.Transition, len(admissible))
	candidates := make([]general.RelationCandidate, 0, len(admissible))
	for _, transition := range admissible {
		if allowed, _ := policyAllows(snapshot, transition); !allowed {
			continue
		}
		all, any := relationAuthority(snapshot, transition)
		id := string(transition.ID)
		byID[id] = transition
		candidates = append(candidates, general.RelationCandidate{
			ID: id, Rank: transition.SelectionClass.Rank(), Priority: transition.Priority,
			Selectable:  transition.ImplicitlySelectable() && !targetAlreadySatisfied(snapshot, objective, transition),
			RequiredAll: all, RequiredAny: any,
		})
	}
	noCandidate := general.Unresolved
	if snapshot.Phase.Value == model.PhaseRecovery || snapshot.Phase.Value == model.PhaseUnresolved {
		noCandidate = general.Blocked
	}
	relation := general.Relate(general.RelationInput{
		Requested: string(requested), Marked: marked, NoCandidate: noCandidate,
		Candidates: candidates, Available: availableAuthority(authority),
	})
	base.Kind = mapDecisionKind(relation.Kind)
	base.Reason = relation.Reason
	for _, candidate := range relation.Candidates {
		base.Candidates = append(base.Candidates, catalog.TransitionID(candidate))
	}
	if relation.Kind == general.Prescribed {
		transition := byID[relation.Transition]
		base.Transition = &transition
	}
	return base
}

func mapDecisionKind(kind general.DecisionKind) DecisionKind {
	switch kind {
	case general.Prescribed:
		return DecisionPrescribed
	case general.Marked:
		return DecisionTerminal
	case general.Frontier:
		return DecisionFrontier
	case general.Blocked:
		return DecisionBlocked
	case general.Refused:
		return DecisionRefused
	default:
		return DecisionUnresolved
	}
}

func availableAuthority(authority catalog.AuthoritySet) []general.Capability {
	result := make([]general.Capability, 0, len(authority))
	for class, present := range authority {
		if present && class != catalog.AuthorityNone {
			result = append(result, authorityCapability(class))
		}
	}
	return result
}

func relationAuthority(snapshot model.Snapshot, transition catalog.Transition) (all, any []general.Capability) {
	unrestricted := false
	for _, class := range transition.Authority {
		if class == catalog.AuthorityNone {
			unrestricted = true
			continue
		}
		any = append(any, authorityCapability(class))
	}
	if unrestricted {
		any = nil
	}
	for _, class := range transition.AuthorityAll {
		if class != catalog.AuthorityNone {
			all = append(all, authorityCapability(class))
		}
	}
	if transition.Policy.AuthorityRule != "" && snapshot.ConfigurationPolicy.Status != model.FactKnown {
		all = append(all, general.Capability("authority.verified-configuration-policy"))
	}
	if transition.Policy.AuthorityRule == "plan-approval" && snapshot.ConfigurationPolicy.Status == model.FactKnown && snapshot.ConfigurationPolicy.Value.PlanApproval == "human" {
		all = append(all, authorityCapability(catalog.AuthorityHuman))
	}
	if transition.Policy.AuthorityRule == "independent-high-risk-review" && snapshot.ConfigurationPolicy.Status == model.FactKnown {
		policy := snapshot.ConfigurationPolicy.Value
		if policy.IndependentReviewForHighRisk && policy.HighRiskChange {
			all = append(all, authorityCapability(catalog.AuthorityHuman))
		}
	}
	return all, any
}

func authorityCapability(class catalog.AuthorityClass) general.Capability {
	return general.Capability("authority." + string(class))
}

func permittedProgramDriftRecovery(snapshot model.Snapshot, transition catalog.Transition) bool {
	if transition.Class != catalog.EventRecovery || snapshot.Phase.Status != model.FactKnown || snapshot.Phase.Value != model.PhaseRecovery || snapshot.RecoveryInfo.Status != model.FactKnown {
		return false
	}
	for _, permitted := range snapshot.RecoveryInfo.Value.Permitted {
		if permitted == string(transition.ID) {
			return true
		}
	}
	return false
}

func targetAlreadySatisfied(snapshot model.Snapshot, objective model.Objective, transition catalog.Transition) bool {
	if transition.Policy.RechecksExternalState {
		return false
	}
	if transition.Policy.BindsRequestedObjective {
		return snapshot.Objective.Status == model.FactKnown && snapshot.Objective.Value == objective
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
