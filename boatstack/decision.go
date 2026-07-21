package boatstack

type DecisionOperator string

const (
	OperatorInfer    DecisionOperator = "infer"
	OperatorQuery    DecisionOperator = "query"
	OperatorVerify   DecisionOperator = "verify"
	OperatorReject   DecisionOperator = "reject"
	OperatorEscalate DecisionOperator = "escalate"
)

type EvidenceLevel string

const (
	EvidenceVerified    EvidenceLevel = "verified"
	EvidenceSupported   EvidenceLevel = "supported"
	EvidenceAbsent      EvidenceLevel = "absent"
	EvidenceConflicting EvidenceLevel = "conflicting"
)

type PremiseStatus string

const (
	PremiseUnknown PremiseStatus = "unknown"
	PremiseValid   PremiseStatus = "valid"
	PremiseInvalid PremiseStatus = "invalid"
)

type PlanDecisionInput struct {
	DecisionKind       string
	IsMaterial         bool
	RepositoryEvidence []EvidenceRecord
	EvidenceLevel      EvidenceLevel
	PremiseStatus      PremiseStatus
}

type DecisionResolution struct {
	Operator      DecisionOperator
	RuleID        string
	Reason        string
	Evidence      []EvidenceRecord
	EvidenceLevel EvidenceLevel
	PremiseStatus PremiseStatus
}

func ResolvePlanDecision(input PlanDecisionInput) DecisionResolution {
	resolution := DecisionResolution{
		Evidence:      input.RepositoryEvidence,
		EvidenceLevel: input.EvidenceLevel,
		PremiseStatus: input.PremiseStatus,
	}

	if input.PremiseStatus == PremiseInvalid {
		resolution.Operator = OperatorReject
		resolution.RuleID = "invalid-premise-rejected"
		resolution.Reason = "planning premise is not supported"
		return resolution
	}

	if input.EvidenceLevel == EvidenceConflicting {
		resolution.Operator = OperatorEscalate
		resolution.RuleID = "conflicting-evidence-escalated"
		resolution.Reason = "repository evidence conflicts"
		return resolution
	}

	if input.EvidenceLevel == EvidenceVerified {
		// Assuming verified evidence is sufficient to resolve the decision
		resolution.Operator = OperatorInfer
		resolution.RuleID = "verified-evidence-inferred"
		resolution.Reason = "verified repository evidence resolves the decision"
		return resolution
	}

	if input.EvidenceLevel == EvidenceSupported {
		resolution.Operator = OperatorVerify
		resolution.RuleID = "supported-evidence-requires-verification"
		resolution.Reason = "evidence is supported but requires independent verification"
		return resolution
	}

	if input.IsMaterial && input.EvidenceLevel == EvidenceAbsent {
		resolution.Operator = OperatorQuery
		resolution.RuleID = "material-intent-requires-human"
		resolution.Reason = "material product intent requires human input"
		return resolution
	}

	resolution.Operator = OperatorEscalate
	resolution.RuleID = "unresolved-uncertainty-escalated"
	resolution.Reason = "unresolved uncertainty requires escalation"
	return resolution
}
