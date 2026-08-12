package catalog

import (
	"fmt"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

func validateDeclarativeStateClosure(t Transition, assigned map[string]bool) error {
	if t.StateEffect.Kind != StateEffectAssignments {
		return nil
	}
	if assignmentHasLiteral(t, "configuration", string(model.ConfigurationVerified)) &&
		!sourceGuaranteesValues(t, "configuration", string(model.ConfigurationVerified)) {
		return fmt.Errorf("%s: declarative verified configuration requires an already verified source; initialization requires a native handler", t.ID)
	}
	if assignmentHasLiteral(t, "runtime", string(model.RuntimeVerified)) &&
		!sourceGuaranteesValues(t, "runtime", string(model.RuntimeVerified)) &&
		!assignmentsProduceNonEmpty(t, "runtime_version", "runtime_fingerprint", "runtime_source") {
		return fmt.Errorf("%s: declarative verified runtime requires version, fingerprint, and source assignments", t.ID)
	}
	if assignmentHasAnyLiteral(t, "workspace", string(model.WorkspaceCut), string(model.WorkspaceActive), string(model.WorkspacePublished), string(model.WorkspaceLanded), string(model.WorkspaceAttentionRequired), string(model.WorkspaceAbandoned)) &&
		!sourceGuaranteesValues(t, "workspace", string(model.WorkspaceCut), string(model.WorkspaceActive), string(model.WorkspacePublished), string(model.WorkspaceLanded), string(model.WorkspaceAttentionRequired), string(model.WorkspaceAbandoned)) &&
		!assignmentsProduceNonEmpty(t, "workspace_path", "workspace_branch", "workspace_source_path", "workspace_source_id", "workspace_source_ref") {
		return fmt.Errorf("%s: declarative managed workspace requires complete destination and source identity", t.ID)
	}
	if assignmentHasAnyLiteral(t, "recovery", nonEmptyRecoveryValues()...) &&
		!sourceGuaranteesValues(t, "recovery", nonEmptyRecoveryValues()...) &&
		(!assignmentsProduceNonEmpty(t, "transaction_id", "recovery_cause", "recovery_source_phase", "recovery_resumption") || !assigned["recovery_budget"]) {
		return fmt.Errorf("%s: declarative recovery state requires complete recovery context", t.ID)
	}
	if assignmentHasAnyLiteral(t, "transaction", nonEmptyTransactionValues()...) &&
		!sourceGuaranteesValues(t, "transaction", nonEmptyTransactionValues()...) &&
		!assignmentsProduceNonEmpty(t, "transaction_id", "transaction_transition") {
		return fmt.Errorf("%s: declarative transaction state requires complete transaction context", t.ID)
	}
	if assignmentHasLiteral(t, "terminal", string(model.TerminalEstablished)) && !declaresTerminalPhase(t) {
		return fmt.Errorf("%s: declarative established terminal state requires a terminal or abandoned target phase", t.ID)
	}
	if assignmentHasLiteral(t, "phase", string(model.PhaseRecovery)) &&
		!assignmentHasAnyLiteral(t, "recovery", nonEmptyRecoveryValues()...) &&
		!sourceGuaranteesValues(t, "recovery", nonEmptyRecoveryValues()...) {
		return fmt.Errorf("%s: declarative recovery phase requires a non-empty recovery classification", t.ID)
	}
	return nil
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

func assignmentHasLiteral(t Transition, facet, value string) bool {
	for _, assignment := range t.StateEffect.Assignments {
		if assignment.Facet == facet && assignment.Value != nil && *assignment.Value == value {
			return true
		}
	}
	return false
}

func assignmentHasAnyLiteral(t Transition, facet string, values ...string) bool {
	for _, value := range values {
		if assignmentHasLiteral(t, facet, value) {
			return true
		}
	}
	return false
}

func assignmentsProduceNonEmpty(t Transition, facets ...string) bool {
	for _, facet := range facets {
		found := false
		for _, assignment := range t.StateEffect.Assignments {
			if assignment.Facet != facet {
				continue
			}
			found = assignment.Value == nil || *assignment.Value != ""
			break
		}
		if !found {
			return false
		}
	}
	return true
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

func declaresTerminalPhase(t Transition) bool {
	for _, phase := range t.TargetPhases {
		if phase == model.PhaseTerminal || phase == model.PhaseAbandoned {
			return true
		}
	}
	return false
}
