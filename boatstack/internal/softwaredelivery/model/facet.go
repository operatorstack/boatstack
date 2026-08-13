package model

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// FacetName is the closed inventory of values that may affect admissibility.
// Adding a controlling fact requires adding it here, which makes completeness
// tests fail until the catalog classifies it.
type FacetName string

const (
	FacetPhase               FacetName = "phase"
	FacetProgram             FacetName = "program"
	FacetTopology            FacetName = "topology"
	FacetEngagement          FacetName = "engagement"
	FacetDelivery            FacetName = "delivery"
	FacetWorkspace           FacetName = "workspace"
	FacetPlan                FacetName = "plan"
	FacetConfiguration       FacetName = "configuration"
	FacetConfigurationPolicy FacetName = "configuration-policy"
	FacetRuntime             FacetName = "runtime"
	FacetPublication         FacetName = "publication"
	FacetVerification        FacetName = "verification"
	FacetRecovery            FacetName = "recovery"
	FacetTransaction         FacetName = "transaction"
	FacetRecoveryInfo        FacetName = "recovery-info"
	FacetTransactionInfo     FacetName = "transaction-info"
	FacetTerminal            FacetName = "terminal"
	FacetObjective           FacetName = "objective"
)

var controllingFacets = []FacetName{
	FacetPhase, FacetProgram, FacetTopology, FacetEngagement, FacetDelivery, FacetWorkspace,
	FacetPlan, FacetConfiguration, FacetConfigurationPolicy, FacetRuntime, FacetPublication,
	FacetVerification, FacetRecovery, FacetTransaction, FacetRecoveryInfo,
	FacetTransactionInfo, FacetTerminal, FacetObjective,
}

var namespacedFacet = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+){2,}$`)

func controllingFacet(facet FacetName) bool {
	for _, candidate := range controllingFacets {
		if facet == candidate {
			return true
		}
	}
	return false
}

func ControllingFacets() []FacetName { return append([]FacetName(nil), controllingFacets...) }

func (f FacetName) Valid() bool {
	return controllingFacet(f) || namespacedFacet.MatchString(string(f))
}

// Facet returns the status and canonical scalar value used by catalog
// predicates. Composite contexts are represented by stable, sorted identity
// fields; their evidence remains in the snapshot fingerprint.
func (s Snapshot) Facet(name FacetName) (FactStatus, string, bool) {
	switch name {
	case FacetPhase:
		return s.Phase.Status, string(s.Phase.Value), true
	case FacetProgram:
		return s.Program.Status, string(s.Program.Value), true
	case FacetTopology:
		return FactKnown, string(s.Invocation.Topology), true
	case FacetEngagement:
		return s.Engagement.Status, string(s.Engagement.Value), true
	case FacetDelivery:
		return s.Delivery.Status, string(s.Delivery.Value), true
	case FacetWorkspace:
		return s.Workspace.Status, string(s.Workspace.Value), true
	case FacetPlan:
		return s.Plan.Status, string(s.Plan.Value), true
	case FacetConfiguration:
		return s.Configuration.Status, string(s.Configuration.Value), true
	case FacetConfigurationPolicy:
		value := s.ConfigurationPolicy.Value.Canonical()
		return s.ConfigurationPolicy.Status, strings.Join([]string{value.PlanApproval, value.VisualEvidence, value.ExternalEffectAuthority, fmt.Sprint(value.IndependentReviewForHighRisk), fmt.Sprint(value.HighRiskChange), strings.Join(value.Hosts, ",")}, "|"), true
	case FacetRuntime:
		return s.Runtime.Status, string(s.Runtime.Value), true
	case FacetPublication:
		return s.Publication.Status, string(s.Publication.Value), true
	case FacetVerification:
		return s.Verification.Status, string(s.Verification.Value), true
	case FacetRecovery:
		return s.Recovery.Status, string(s.Recovery.Value), true
	case FacetTransaction:
		return s.Transaction.Status, string(s.Transaction.Value), true
	case FacetRecoveryInfo:
		value := s.RecoveryInfo.Value
		return s.RecoveryInfo.Status, strings.Join([]string{value.TransactionID, value.Cause, string(value.SourcePhase), string(value.Resumption)}, "|"), true
	case FacetTransactionInfo:
		value := s.TransactionInfo.Value
		resources := append([]string(nil), value.ResourceDigests...)
		sort.Strings(resources)
		return s.TransactionInfo.Status, strings.Join([]string{value.ID, value.TransitionID, value.Status, strings.Join(resources, ","), fmt.Sprint(value.ExternalPossible)}, "|"), true
	case FacetTerminal:
		return s.Terminal.Status, string(s.Terminal.Value), true
	case FacetObjective:
		value := s.Objective.Value
		return s.Objective.Status, strings.Join([]string{value.ID, string(value.TargetID), string(value.TrustedObjectiveClass()), value.DeliveryID, value.EvidenceFingerprint, fmt.Sprint(value.FrontierIsStop)}, "|"), true
	default:
		if fact, ok := s.ProgramFacts[string(name)]; ok {
			return fact.Status, fact.Value, true
		}
		fact, ok := s.ExtensionFacts[string(name)]
		if !ok {
			return "", "", false
		}
		return fact.Status, fact.Value, true
	}
}
