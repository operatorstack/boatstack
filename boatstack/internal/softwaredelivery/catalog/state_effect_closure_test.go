package catalog

import (
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

func TestDeclarativeClosureRejectsClearedCompositeEvidence(t *testing.T) {
	tests := []struct {
		name       string
		transition Transition
	}{
		{
			name: "verified runtime source with cleared source identity",
			transition: closureTransition(
				knownSource(model.FacetRuntime, string(model.RuntimeVerified)),
				literalAssignment("runtime", string(model.RuntimeVerified)),
				literalAssignment("runtime_source", ""),
			),
		},
		{
			name: "verified configuration source with cleared fingerprint",
			transition: closureTransition(
				knownSource(model.FacetConfiguration, string(model.ConfigurationVerified)),
				literalAssignment("configuration", string(model.ConfigurationVerified)),
				literalAssignment("config_fingerprint", ""),
			),
		},
		{
			name: "managed workspace source with cleared source reference",
			transition: closureTransition(
				knownSource(model.FacetWorkspace, string(model.WorkspaceActive)),
				literalAssignment("workspace", string(model.WorkspaceActive)),
				literalAssignment("workspace_source_ref", ""),
			),
		},
		{
			name: "recovery source with cleared cause",
			transition: closureTransition(
				knownSource(model.FacetRecovery, string(model.RecoveryResumable)),
				literalAssignment("recovery", string(model.RecoveryEscalated)),
				literalAssignment("recovery_cause", ""),
			),
		},
		{
			name: "transaction source with cleared identity",
			transition: closureTransition(
				knownSource(model.FacetTransaction, string(model.TransactionStaged)),
				literalAssignment("transaction", string(model.TransactionStaged)),
				literalAssignment("transaction_id", ""),
			),
		},
		{
			name: "established terminal with active resulting phase",
			transition: closureTransition(
				FacetCondition{},
				literalAssignment("terminal", string(model.TerminalEstablished)),
				literalAssignment("phase", string(model.PhaseActive)),
			),
		},
		{
			name: "recovery phase with cleared recovery classification",
			transition: closureTransition(
				knownSource(model.FacetRecovery, string(model.RecoveryResumable)),
				literalAssignment("phase", string(model.PhaseRecovery)),
				literalAssignment("recovery", string(model.RecoveryNone)),
			),
		},
		{
			name: "preserved phase outside declared targets",
			transition: func() Transition {
				value := closureTransition(FacetCondition{})
				value.SourcePhases = []model.ProtocolPhase{model.PhaseObserved, model.PhaseActive}
				value.TargetPhases = []model.ProtocolPhase{model.PhaseActive}
				return value
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateDeclarativeStateClosure(test.transition, assignedFields(test.transition)); err == nil {
				t.Fatal("composite durable evidence could be cleared by an admitted assignment")
			}
		})
	}
}

func TestDeclarativeClosurePreservesValidCompositeEvidence(t *testing.T) {
	tests := []Transition{
		closureTransition(knownSource(model.FacetRuntime, string(model.RuntimeVerified)), literalAssignment("runtime", string(model.RuntimeVerified))),
		closureTransition(knownSource(model.FacetConfiguration, string(model.ConfigurationVerified)), literalAssignment("configuration", string(model.ConfigurationVerified))),
		closureTransition(knownSource(model.FacetWorkspace, string(model.WorkspaceActive)), literalAssignment("workspace", string(model.WorkspaceActive))),
		closureTransition(knownSource(model.FacetRecovery, string(model.RecoveryResumable)), literalAssignment("recovery", string(model.RecoveryEscalated))),
		closureTransition(knownSource(model.FacetTransaction, string(model.TransactionStaged)), literalAssignment("transaction", string(model.TransactionStaged))),
		closureTransition(knownSource(model.FacetTerminal, string(model.TerminalEstablished)), literalAssignment("terminal", string(model.TerminalEstablished))),
	}
	tests[len(tests)-1].SourcePhases = []model.ProtocolPhase{model.PhaseTerminal}
	tests[len(tests)-1].TargetPhases = []model.ProtocolPhase{model.PhaseTerminal}
	for index, transition := range tests {
		if err := validateDeclarativeStateClosure(transition, assignedFields(transition)); err != nil {
			t.Fatalf("valid preserved composite evidence %d was rejected: %v", index, err)
		}
	}
}

func TestDeclarativeAssignmentsRejectApplyTimeOnlyValueConstraints(t *testing.T) {
	tests := []StateAssignment{
		{Facet: "recovery_source_phase", ValueFrom: StateValueReference{Parameter: "phase"}},
		{Facet: "recovery_resumption", ValueFrom: StateValueReference{Parameter: "phase"}},
		{Facet: "recovery_budget", ValueFrom: StateValueReference{Parameter: "budget"}},
		literalAssignment("program_fingerprint", "short"),
		{Facet: "program_fingerprint", ValueFrom: StateValueReference{Parameter: "fingerprint"}},
	}
	for _, assignment := range tests {
		transition := Transition{ID: "test.transition"}
		if err := validateDeterministicAssignment(transition, assignment); err == nil {
			t.Fatalf("apply-time-only constraint for %q reached execution", assignment.Facet)
		}
	}

	transition := Transition{ID: "test.transition"}
	assignment := StateAssignment{Facet: "program_fingerprint", ValueFrom: StateValueReference{Admission: "expected_program_fingerprint"}}
	if err := validateDeterministicAssignment(transition, assignment); err != nil {
		t.Fatalf("admission-bound program fingerprint was rejected: %v", err)
	}
}

func TestStateAssignmentMustSatisfyEveryTargetCondition(t *testing.T) {
	assignment := literalAssignment("delivery", string(model.DeliveryPublished))
	transition := Transition{
		TargetConditions: []FacetCondition{
			{Facet: model.FacetDelivery, Statuses: []model.FactStatus{model.FactKnown}, Values: []string{string(model.DeliveryPublished)}},
			{Facet: model.FacetDelivery, Statuses: []model.FactStatus{model.FactKnown}, Values: []string{string(model.DeliveryTerminal)}},
		},
	}
	if stateAssignmentMatchesTarget(transition, assignment) {
		t.Fatal("assignment matched only the first of two target conditions")
	}
}

func closureTransition(source FacetCondition, assignments ...StateAssignment) Transition {
	transition := Transition{
		ID: "test.transition", SourcePhases: []model.ProtocolPhase{model.PhaseActive}, TargetPhases: []model.ProtocolPhase{model.PhaseActive},
		StateEffect: StateEffect{Kind: StateEffectAssignments, Assignments: assignments},
	}
	if source.Facet != "" {
		transition.SourceConditions = []FacetCondition{source}
	}
	return transition
}

func knownSource(facet model.FacetName, values ...string) FacetCondition {
	return FacetCondition{Facet: facet, Statuses: []model.FactStatus{model.FactKnown}, Values: values}
}

func literalAssignment(facet, value string) StateAssignment {
	return StateAssignment{Facet: facet, Value: &value}
}

func assignedFields(transition Transition) map[string]bool {
	result := make(map[string]bool, len(transition.StateEffect.Assignments))
	for _, assignment := range transition.StateEffect.Assignments {
		result[assignment.Facet] = true
	}
	return result
}
