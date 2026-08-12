package catalog

import (
	"fmt"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

func validateDeclarativeStateClosure(t Transition, assigned map[string]bool) error {
	if t.StateEffect.Kind != StateEffectAssignments {
		return nil
	}
	if journalDerivedCoreRecovery(t) {
		return nil
	}
	if !preservedPhaseIsDeclared(t) {
		return fmt.Errorf("%s: declarative state effect can preserve a source phase outside its target phases", t.ID)
	}
	configurationVerified := resultGuaranteesValues(t, "configuration", string(model.ConfigurationVerified))
	sourceConfigurationVerified := sourceGuaranteesValues(t, "configuration", string(model.ConfigurationVerified))
	if configurationVerified && !sourceConfigurationVerified {
		return fmt.Errorf("%s: declarative verified configuration requires an already verified source; initialization requires a native handler", t.ID)
	}
	if configurationVerified && !assignmentsPreserveOrProduceNonEmpty(t, sourceConfigurationVerified, "config_fingerprint") {
		return fmt.Errorf("%s: declarative verified configuration cannot clear its fingerprint", t.ID)
	}
	runtimeVerified := resultGuaranteesValues(t, "runtime", string(model.RuntimeVerified))
	sourceRuntimeVerified := sourceGuaranteesValues(t, "runtime", string(model.RuntimeVerified))
	if runtimeVerified && !assignmentsPreserveOrProduceNonEmpty(t, sourceRuntimeVerified, "runtime_version", "runtime_fingerprint", "runtime_source") {
		return fmt.Errorf("%s: declarative verified runtime requires version, fingerprint, and source assignments", t.ID)
	}
	managedWorkspace := []string{string(model.WorkspaceCut), string(model.WorkspaceActive), string(model.WorkspacePublished), string(model.WorkspaceLanded), string(model.WorkspaceAttentionRequired), string(model.WorkspaceAbandoned)}
	resultWorkspaceManaged := resultGuaranteesValues(t, "workspace", managedWorkspace...)
	sourceWorkspaceManaged := sourceGuaranteesValues(t, "workspace", managedWorkspace...)
	if resultWorkspaceManaged && !assignmentsPreserveOrProduceNonEmpty(t, sourceWorkspaceManaged, "workspace_path", "workspace_branch", "workspace_source_path", "workspace_source_id", "workspace_source_ref") {
		return fmt.Errorf("%s: declarative managed workspace requires complete destination and source identity", t.ID)
	}
	resultRecoveryNonEmpty := resultGuaranteesValues(t, "recovery", nonEmptyRecoveryValues()...)
	sourceRecoveryNonEmpty := sourceGuaranteesValues(t, "recovery", nonEmptyRecoveryValues()...)
	if resultRecoveryNonEmpty &&
		(!assignmentsPreserveOrProduceNonEmpty(t, sourceRecoveryNonEmpty, "transaction_id", "recovery_cause", "recovery_source_phase", "recovery_resumption") || (!sourceRecoveryNonEmpty && !assigned["recovery_budget"])) {
		return fmt.Errorf("%s: declarative recovery state requires complete recovery context", t.ID)
	}
	resultTransactionNonEmpty := resultGuaranteesValues(t, "transaction", nonEmptyTransactionValues()...)
	sourceTransactionNonEmpty := sourceGuaranteesValues(t, "transaction", nonEmptyTransactionValues()...)
	if resultTransactionNonEmpty && !assignmentsPreserveOrProduceNonEmpty(t, sourceTransactionNonEmpty, "transaction_id", "transaction_transition") {
		return fmt.Errorf("%s: declarative transaction state requires complete transaction context", t.ID)
	}
	if resultGuaranteesValues(t, "terminal", string(model.TerminalEstablished)) && !resultGuaranteesPhase(t, model.PhaseTerminal, model.PhaseAbandoned) {
		return fmt.Errorf("%s: declarative established terminal state requires a terminal or abandoned target phase", t.ID)
	}
	if resultGuaranteesPhase(t, model.PhaseRecovery) && !resultRecoveryNonEmpty {
		return fmt.Errorf("%s: declarative recovery phase requires a non-empty recovery classification", t.ID)
	}
	return nil
}

func validateDeterministicAssignment(t Transition, assignment StateAssignment) error {
	switch assignment.Facet {
	case "recovery_source_phase", "recovery_resumption", "recovery_budget":
		if assignment.Value == nil {
			return fmt.Errorf("%s: state-effect assignment %q requires a literal constrained value", t.ID, assignment.Facet)
		}
	case "program_fingerprint":
		if assignment.Value != nil {
			if *assignment.Value != "" && len(*assignment.Value) != 64 {
				return fmt.Errorf("%s: state-effect assignment %q requires an empty or 64-character fingerprint", t.ID, assignment.Facet)
			}
			return nil
		}
		if assignment.ValueFrom.Admission != "expected_program_fingerprint" {
			return fmt.Errorf("%s: state-effect assignment %q requires the admitted program fingerprint", t.ID, assignment.Facet)
		}
	}
	return nil
}

func preservedPhaseIsDeclared(t Transition) bool {
	if _, assigned := assignmentLiteral(t, "phase"); assigned {
		return true
	}
	for _, source := range t.SourcePhases {
		if !t.DeclaresTargetPhase(source) {
			return false
		}
	}
	return len(t.SourcePhases) > 0
}

func journalDerivedCoreRecovery(t Transition) bool {
	// Core recovery replay restores the complete state from the bound journal
	// at the dedicated recovery boundary rather than through assignments.
	return t.Class == EventRecovery && !t.RuntimeExecution && len(t.StateEffect.Assignments) == 0 && containsString(t.OwnedResources, "recovery-journal")
}

func nonEmptyRecoveryValues() []string {
	return []string{
		string(model.RecoveryResumable), string(model.RecoveryRollback), string(model.RecoveryCompensation),
		string(model.RecoveryReconcile), string(model.RecoveryEscalated),
	}
}

func nonEmptyTransactionValues() []string {
	return []string{
		string(model.TransactionStaged), string(model.TransactionLocalApplied), string(model.TransactionExternalUncertain),
		string(model.TransactionVerifying), string(model.TransactionCommitted), string(model.TransactionCompensating),
	}
}

func assignmentLiteral(t Transition, facet string) (string, bool) {
	for _, assignment := range t.StateEffect.Assignments {
		if assignment.Facet == facet && assignment.Value != nil {
			return *assignment.Value, true
		}
	}
	return "", false
}

func resultGuaranteesValues(t Transition, facet string, values ...string) bool {
	if value, assigned := assignmentLiteral(t, facet); assigned {
		return containsString(values, value)
	}
	return sourceGuaranteesValues(t, facet, values...)
}

func assignmentsPreserveOrProduceNonEmpty(t Transition, sourceCompositeValid bool, facets ...string) bool {
	for _, facet := range facets {
		assigned := false
		for _, assignment := range t.StateEffect.Assignments {
			if assignment.Facet != facet {
				continue
			}
			assigned = true
			if assignment.Value != nil && *assignment.Value == "" {
				return false
			}
			break
		}
		if !assigned && !sourceCompositeValid {
			return false
		}
	}
	return true
}

func resultGuaranteesPhase(t Transition, phases ...model.ProtocolPhase) bool {
	if value, assigned := assignmentLiteral(t, "phase"); assigned {
		for _, phase := range phases {
			if value == string(phase) {
				return true
			}
		}
		return false
	}
	if len(t.SourcePhases) == 0 {
		return false
	}
	for _, source := range t.SourcePhases {
		found := false
		for _, phase := range phases {
			if source == phase {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func sourceGuaranteesValues(t Transition, field string, values ...string) bool {
	facet, ok := DeclaredStateResolverFacet(field)
	if !ok {
		return false
	}
	allowed := make(map[string]bool, len(values))
	for _, value := range values {
		allowed[value] = true
	}
	for _, condition := range t.SourceConditions {
		if condition.Facet != facet || len(condition.Statuses) != 1 || condition.Statuses[0] != model.FactKnown || len(condition.Values) == 0 {
			continue
		}
		for _, value := range condition.Values {
			if !allowed[value] {
				return false
			}
		}
		return true
	}
	return false
}
