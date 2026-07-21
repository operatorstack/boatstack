package boatstack

import "testing"

func TestResolvePlanDecision(t *testing.T) {
	tests := []struct {
		name     string
		input    PlanDecisionInput
		expected DecisionOperator
	}{
		{
			name: "invalid premise rejects",
			input: PlanDecisionInput{
				PremiseStatus: PremiseInvalid,
			},
			expected: OperatorReject,
		},
		{
			name: "conflicting evidence escalates",
			input: PlanDecisionInput{
				PremiseStatus: PremiseValid,
				EvidenceLevel: EvidenceConflicting,
			},
			expected: OperatorEscalate,
		},
		{
			name: "verified evidence infers",
			input: PlanDecisionInput{
				PremiseStatus: PremiseValid,
				EvidenceLevel: EvidenceVerified,
			},
			expected: OperatorInfer,
		},
		{
			name: "supported evidence verifies",
			input: PlanDecisionInput{
				PremiseStatus: PremiseValid,
				EvidenceLevel: EvidenceSupported,
			},
			expected: OperatorVerify,
		},
		{
			name: "absent evidence for material intent queries",
			input: PlanDecisionInput{
				PremiseStatus: PremiseValid,
				IsMaterial:    true,
				EvidenceLevel: EvidenceAbsent,
			},
			expected: OperatorQuery,
		},
		{
			name: "unknown state escalates",
			input: PlanDecisionInput{
				PremiseStatus: PremiseUnknown,
				EvidenceLevel: EvidenceAbsent,
				IsMaterial:    false,
			},
			expected: OperatorEscalate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolution := ResolvePlanDecision(tt.input)
			if resolution.Operator != tt.expected {
				t.Errorf("expected operator %s, got %s", tt.expected, resolution.Operator)
			}
		})
	}
}
