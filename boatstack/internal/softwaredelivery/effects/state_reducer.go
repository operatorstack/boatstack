package effects

import (
	"fmt"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

func applyStateTransition(state *durable.State, admission protocol.Admission, transition catalog.Transition) error {
	configured := state.Objective.Validate() == nil
	if transition.Policy.ObjectiveScope == catalog.ObjectiveScopeOptionalPreserve {
		if configured && state.Objective != admission.Objective {
			return fmt.Errorf("transition %q must preserve the exact configured product objective", transition.ID)
		}
		if !configured && admission.Objective.Validate() == nil {
			return fmt.Errorf("transition %q cannot create product intent from verified absence", transition.ID)
		}
	} else {
		if configured && state.Objective != admission.Objective && transition.StateEffect.NativeHandler != "objective-bind" {
			return fmt.Errorf("transition %q cannot replace configured objective; use objective.bind", transition.ID)
		}
	}
	state.LastTransition = transition.ID
	if err := applyDeclaredStateEffect(state, admission, transition); err != nil {
		return err
	}
	if !transition.DeclaresTargetPhase(state.Phase) {
		return fmt.Errorf("transition %q reducer produced undeclared target phase %s", transition.ID, state.Phase)
	}
	return nil
}

func applyDeclaredStateEffect(state *durable.State, admission protocol.Admission, transition catalog.Transition) error {
	for _, condition := range transition.StateEffect.Preconditions {
		current, err := stateFacetValue(*state, condition.Facet)
		if err != nil {
			return fmt.Errorf("transition %q state precondition: %w", transition.ID, err)
		}
		matched := false
		for _, value := range condition.Values {
			if current == value {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("transition %q state precondition %q rejected value %q", transition.ID, condition.Facet, current)
		}
	}
	switch transition.StateEffect.Kind {
	case catalog.StateEffectAssignments:
		for _, assignment := range transition.StateEffect.Assignments {
			value, err := assignmentValue(admission, assignment)
			if err != nil {
				return fmt.Errorf("transition %q state assignment %q: %w", transition.ID, assignment.Facet, err)
			}
			if err := assignStateFacet(state, assignment.Facet, value); err != nil {
				return fmt.Errorf("transition %q state assignment: %w", transition.ID, err)
			}
		}
		return nil
	case catalog.StateEffectNative:
		handler, ok := nativeStateHandlers[transition.StateEffect.NativeHandler]
		if !ok {
			return fmt.Errorf("transition %q declares unknown native state handler %q", transition.ID, transition.StateEffect.NativeHandler)
		}
		return handler(state, admission, transition)
	default:
		return fmt.Errorf("transition %q has no valid declared state effect", transition.ID)
	}
}

type nativeStateHandler func(*durable.State, protocol.Admission, catalog.Transition) error

var nativeStateHandlers = map[string]nativeStateHandler{
	"runtime-verified-settled":       applyRuntimeVerifiedSettled,
	"runtime-reconcile":              applyRuntimeReconcile,
	"configuration-verified-settled": applyConfigurationVerifiedSettled,
	"configuration-reconcile":        applyConfigurationReconcile,
	"installation-initialize":        applyInstallationInitialize,
	"installation-reconcile-update":  applyInstallationReconcileUpdate,
	"catalog-reconcile":              applyCatalogReconcile,
	"objective-bind":                 applyObjectiveBind,
	"plan-approve":                   applyPlanApprove,
	"planning-package-admit":         applyPlanningPackageAdmit,
	"planning-package-approve":       applyPlanningPackageApprove,
	"planning-package-promote":       applyPlanningPackagePromote,
	"abandon-delivery":               applyAbandonDelivery,
	"workspace-cleanup":              applyWorkspaceCleanup,
	"workspace-reap":                 applyWorkspaceReap,
	"workspace-reconcile":            applyWorkspaceReconcile,
	"gate-build-record":              gateStateHandler("build"),
	"gate-test-record":               gateStateHandler("test"),
	"gate-review-record":             gateStateHandler("review"),
	"gate-change-record":             gateStateHandler("change"),
	"gate-journey-record":            gateStateHandler("journey"),
	"visual-evidence-attach":         applyVisualEvidence,
	"publication-observe":            applyPublicationObservation,
}

func assignmentValue(admission protocol.Admission, assignment catalog.StateAssignment) (string, error) {
	if assignment.Value != nil {
		return *assignment.Value, nil
	}
	if assignment.ValueFrom.Parameter != "" {
		value, ok := admission.Parameters.Get(assignment.ValueFrom.Parameter)
		if !ok {
			return "", fmt.Errorf("parameter %q is absent", assignment.ValueFrom.Parameter)
		}
		return value, nil
	}
	switch assignment.ValueFrom.Admission {
	case "source_revision":
		return admission.SourceRevision, nil
	case "worktree_fingerprint":
		return admission.WorktreeFingerprint, nil
	case "expected_program_fingerprint":
		return admission.ExpectedProgramFingerprint, nil
	case "":
	default:
		return "", fmt.Errorf("admission value %q is unknown", assignment.ValueFrom.Admission)
	}
	switch assignment.ValueFrom.Invocation {
	case "invoking_path":
		return admission.Invocation.InvokingPath, nil
	case "worktree_id":
		return admission.Invocation.WorktreeID, nil
	case "ref":
		return admission.Invocation.Ref, nil
	case "":
	default:
		return "", fmt.Errorf("invocation value %q is unknown", assignment.ValueFrom.Invocation)
	}
	return "", fmt.Errorf("value source is absent")
}

func stateFacetValue(state durable.State, facet string) (string, error) {
	switch facet {
	case "program_fingerprint":
		return state.ProgramFingerprint, nil
	case "phase":
		return string(state.Phase), nil
	case "engagement":
		return string(state.Engagement), nil
	case "delivery":
		return string(state.Delivery), nil
	case "workspace":
		return string(state.Workspace), nil
	case "plan":
		return string(state.Plan), nil
	case "configuration":
		return string(state.Configuration), nil
	case "runtime":
		return string(state.Runtime), nil
	case "publication":
		return string(state.Publication), nil
	case "verification":
		return string(state.Verification), nil
	case "recovery":
		return string(state.Recovery), nil
	case "transaction":
		return string(state.Transaction), nil
	case "terminal":
		return string(state.Terminal), nil
	case "source_revision":
		return state.SourceRevision, nil
	case "worktree_fingerprint":
		return state.WorktreeFingerprint, nil
	case "config_fingerprint":
		return state.ConfigFingerprint, nil
	case "runtime_version":
		return state.RuntimeVersion, nil
	case "runtime_fingerprint":
		return state.RuntimeFingerprint, nil
	case "runtime_source":
		return state.RuntimeSource, nil
	case "workspace_branch":
		return state.WorkspaceBranch, nil
	case "workspace_path":
		return state.WorkspacePath, nil
	case "workspace_base_ref":
		return state.WorkspaceBaseRef, nil
	case "workspace_source_path":
		return state.WorkspaceSourcePath, nil
	case "workspace_source_id":
		return state.WorkspaceSourceID, nil
	case "workspace_source_ref":
		return state.WorkspaceSourceRef, nil
	case "transaction_id":
		return state.TransactionID, nil
	case "transaction_transition":
		return state.TransactionTransition, nil
	case "recovery_cause":
		return state.RecoveryCause, nil
	case "recovery_source_phase":
		return string(state.RecoverySourcePhase), nil
	case "recovery_resumption":
		return string(state.RecoveryResumption), nil
	case "recovery_budget":
		return fmt.Sprintf("%d", state.RecoveryBudget), nil
	default:
		return "", fmt.Errorf("durable state facet %q is unknown", facet)
	}
}

func assignStateFacet(state *durable.State, facet, value string) error {
	switch facet {
	case "program_fingerprint":
		state.ProgramFingerprint = value
	case "phase":
		state.Phase = model.ProtocolPhase(value)
	case "engagement":
		state.Engagement = model.EngagementState(value)
	case "delivery":
		state.Delivery = model.DeliveryState(value)
	case "workspace":
		state.Workspace = model.WorkspaceState(value)
	case "plan":
		state.Plan = model.PlanState(value)
	case "configuration":
		state.Configuration = model.ConfigurationState(value)
	case "runtime":
		state.Runtime = model.RuntimeState(value)
	case "publication":
		state.Publication = model.PublicationState(value)
	case "verification":
		state.Verification = model.VerificationState(value)
	case "recovery":
		state.Recovery = model.RecoveryState(value)
	case "transaction":
		state.Transaction = model.TransactionState(value)
	case "terminal":
		state.Terminal = model.TerminalStatus(value)
	case "source_revision":
		state.SourceRevision = value
	case "worktree_fingerprint":
		state.WorktreeFingerprint = value
	case "config_fingerprint":
		state.ConfigFingerprint = value
	case "runtime_version":
		state.RuntimeVersion = value
	case "runtime_fingerprint":
		state.RuntimeFingerprint = value
	case "runtime_source":
		state.RuntimeSource = value
	case "workspace_branch":
		state.WorkspaceBranch = value
	case "workspace_path":
		state.WorkspacePath = value
	case "workspace_base_ref":
		state.WorkspaceBaseRef = value
	case "workspace_source_path":
		state.WorkspaceSourcePath = value
	case "workspace_source_id":
		state.WorkspaceSourceID = value
	case "workspace_source_ref":
		state.WorkspaceSourceRef = value
	case "transaction_id":
		state.TransactionID = value
	case "transaction_transition":
		state.TransactionTransition = value
	case "recovery_cause":
		state.RecoveryCause = value
	case "recovery_source_phase":
		state.RecoverySourcePhase = model.ProtocolPhase(value)
	case "recovery_resumption":
		state.RecoveryResumption = model.ProtocolPhase(value)
	case "recovery_budget":
		if value != "0" {
			return fmt.Errorf("recovery_budget currently accepts only the fail-closed zero literal")
		}
		state.RecoveryBudget = 0
	default:
		return fmt.Errorf("durable state facet %q is unknown", facet)
	}
	return nil
}

func applyRuntimeVerifiedSettled(state *durable.State, admission protocol.Admission, _ catalog.Transition) error {
	state.Runtime = model.RuntimeVerified
	state.RuntimeVersion, _ = admission.Parameters.Get("runtime_version")
	state.RuntimeFingerprint, _ = admission.Parameters.Get("runtime_sha256")
	state.RuntimeSource, _ = admission.Parameters.Get("source_revision")
	state.Phase = settledPhase(*state)
	return nil
}

func applyRuntimeReconcile(state *durable.State, admission protocol.Admission, transition catalog.Transition) error {
	if err := applyRuntimeVerifiedSettled(state, admission, transition); err != nil {
		return err
	}
	state.Recovery, state.Transaction = model.RecoveryNone, model.TransactionNone
	clearRecoveryContext(state)
	state.Phase = settledPhase(*state)
	return nil
}

func applyConfigurationVerifiedSettled(state *durable.State, admission protocol.Admission, _ catalog.Transition) error {
	state.Configuration = model.ConfigurationVerified
	state.ConfigFingerprint, _ = admission.Parameters.Get("config_sha256")
	state.Phase = settledPhase(*state)
	return nil
}

func applyConfigurationReconcile(state *durable.State, _ protocol.Admission, _ catalog.Transition) error {
	state.Configuration, state.Recovery, state.Transaction = model.ConfigurationVerified, model.RecoveryNone, model.TransactionNone
	clearRecoveryContext(state)
	state.Phase = settledPhase(*state)
	return nil
}

func applyInstallationInitialize(state *durable.State, admission protocol.Admission, _ catalog.Transition) error {
	state.Runtime = model.RuntimeVerified
	state.RuntimeVersion, _ = admission.Parameters.Get("runtime_version")
	state.RuntimeFingerprint, _ = admission.Parameters.Get("runtime_sha256")
	state.RuntimeSource, _ = admission.Parameters.Get("source_revision")
	state.Configuration = model.ConfigurationVerified
	state.ConfigFingerprint, _ = admission.Parameters.Get("config_sha256")
	state.Phase = model.PhaseObserved
	return nil
}

func applyInstallationReconcileUpdate(state *durable.State, admission protocol.Admission, _ catalog.Transition) error {
	accepted, _ := admission.Parameters.Get("accept_obligation_change")
	if accepted != "true" || admission.PriorProgramFingerprint == "" || admission.ProgramDeltaFingerprint == "" || state.ProgramFingerprint != admission.PriorProgramFingerprint {
		return fmt.Errorf("reconciled installation update must bind and explicitly accept the exact prior-to-candidate program delta")
	}
	state.ProgramFingerprint = admission.ExpectedProgramFingerprint
	state.Runtime = model.RuntimeVerified
	state.RuntimeVersion, _ = admission.Parameters.Get("runtime_version")
	state.RuntimeFingerprint, _ = admission.Parameters.Get("runtime_sha256")
	state.RuntimeSource, _ = admission.Parameters.Get("source_revision")
	return nil
}

func applyCatalogReconcile(state *durable.State, admission protocol.Admission, _ catalog.Transition) error {
	prior, _ := admission.Parameters.Get("prior_program_fingerprint")
	accepted, _ := admission.Parameters.Get("accept_obligation_change")
	if prior == "" || prior != state.ProgramFingerprint || accepted != "true" {
		return fmt.Errorf("catalog reconciliation must bind the prior program and explicitly accept obligation changes")
	}
	state.ProgramFingerprint = admission.ExpectedProgramFingerprint
	return nil
}

func applyObjectiveBind(state *durable.State, admission protocol.Admission, _ catalog.Transition) error {
	wasActive := state.Phase == model.PhaseActive
	kind, _ := admission.Parameters.Get("target_id")
	delivery, _ := admission.Parameters.Get("delivery_id")
	if kind != string(admission.Objective.TargetID) || delivery != admission.Objective.DeliveryID {
		return fmt.Errorf("objective parameters do not match admitted objective")
	}
	if state.Objective.DeliveryID != "" && state.Objective.DeliveryID != admission.Objective.DeliveryID {
		if state.Terminal != model.TerminalEstablished {
			return fmt.Errorf("a different delivery requires the prior delivery to be terminal")
		}
		resetDeliveryState(state)
		wasActive = false
	}
	state.Objective = admission.Objective
	state.Terminal = model.TerminalNonterminal
	state.Phase = model.PhaseObserved
	if state.Recovery == model.RecoveryEscalated {
		state.Phase = model.PhaseFrontier
	} else if wasActive {
		state.Phase = model.PhaseActive
	}
	return nil
}

func resetDeliveryState(state *durable.State) {
	state.Delivery, state.Plan = model.DeliveryUninitialized, model.PlanAbsent
	state.Workspace = model.WorkspaceAbsent
	state.Publication, state.Verification = model.PublicationNone, model.VerificationUnverified
	state.PlanFingerprint, state.PlanningPackageFingerprint, state.ApprovalFingerprint, state.PublicationID, state.PublicationURL, state.PreviewFingerprint = "", "", "", "", "", ""
	state.WorkspaceBranch, state.WorkspacePath, state.WorkspaceBaseRef = "", "", ""
	state.WorkspaceSourcePath, state.WorkspaceSourceID, state.WorkspaceSourceRef = "", "", ""
	state.Gates = nil
}

func applyPlanApprove(state *durable.State, admission protocol.Admission, _ catalog.Transition) error {
	state.Plan, state.Delivery, state.Phase = model.PlanApproved, model.DeliveryApproved, model.PhaseActive
	if admission.Objective.TrustedObjectiveClass() == model.ObjectiveApprovedPlan {
		establishTerminal(state, model.PhaseTerminal)
	}
	return nil
}

func applyPlanningPackageAdmit(state *durable.State, _ protocol.Admission, _ catalog.Transition) error {
	state.Plan, state.Delivery, state.Phase, state.Terminal = model.PlanValid, model.DeliveryPlanning, model.PhaseActive, model.TerminalNonterminal
	return nil
}

func applyPlanningPackageApprove(state *durable.State, _ protocol.Admission, _ catalog.Transition) error {
	state.Plan, state.Delivery, state.Phase, state.Terminal = model.PlanPackageApproved, model.DeliveryPlanning, model.PhaseActive, model.TerminalNonterminal
	return nil
}

func applyPlanningPackagePromote(state *durable.State, admission protocol.Admission, _ catalog.Transition) error {
	state.Plan, state.Delivery, state.Phase = model.PlanApproved, model.DeliveryApproved, model.PhaseActive
	if admission.Objective.TrustedObjectiveClass() == model.ObjectiveApprovedPlan {
		establishTerminal(state, model.PhaseTerminal)
	}
	return nil
}

func applyAbandonDelivery(state *durable.State, _ protocol.Admission, _ catalog.Transition) error {
	state.Delivery = model.DeliveryDiscarded
	if state.Workspace != model.WorkspaceAbsent {
		state.Workspace = model.WorkspaceAbandoned
	}
	state.Recovery, state.Transaction = model.RecoveryNone, model.TransactionNone
	clearRecoveryContext(state)
	establishTerminal(state, model.PhaseAbandoned)
	return nil
}

func applyWorkspaceCleanup(state *durable.State, _ protocol.Admission, _ catalog.Transition) error {
	if state.Workspace != model.WorkspaceLanded && state.Workspace != model.WorkspaceAbandoned {
		return fmt.Errorf("workspace cleanup requires landed or explicitly abandoned state")
	}
	state.Workspace = model.WorkspaceAbsent
	state.Phase = terminalPhase(*state)
	return nil
}

func applyWorkspaceReap(state *durable.State, _ protocol.Admission, _ catalog.Transition) error {
	state.Workspace = model.WorkspaceAbsent
	state.Phase = terminalPhase(*state)
	return nil
}

func applyWorkspaceReconcile(state *durable.State, _ protocol.Admission, _ catalog.Transition) error {
	state.Recovery, state.Transaction, state.Phase = model.RecoveryNone, model.TransactionNone, engagedPhase(*state)
	clearRecoveryContext(state)
	return nil
}

func gateStateHandler(gate string) nativeStateHandler {
	return func(state *durable.State, admission protocol.Admission, _ catalog.Transition) error {
		revision, _ := admission.Parameters.Get("source_revision")
		fingerprint, _ := admission.Parameters.Get("evidence_fingerprint")
		upsertGate(state, durable.GateEvidence{Gate: gate, Revision: revision, Fingerprint: fingerprint})
		state.SourceRevision, state.WorktreeFingerprint = admission.SourceRevision, admission.WorktreeFingerprint
		state.Terminal, state.Delivery = model.TerminalNonterminal, model.DeliveryActive
		state.Verification, state.Phase = model.VerificationCurrent, model.PhaseActive
		if verifiedObjectiveSatisfied(*state, admission.Objective) {
			state.Delivery = model.DeliveryGatesPassed
			establishTerminal(state, model.PhaseTerminal)
		}
		return nil
	}
}

func applyVisualEvidence(state *durable.State, admission protocol.Admission, _ catalog.Transition) error {
	revision, _ := admission.Parameters.Get("source_revision")
	fingerprint, _ := admission.Parameters.Get("privacy_receipt")
	upsertGate(state, durable.GateEvidence{Gate: "visual", Revision: revision, Fingerprint: fingerprint})
	state.SourceRevision, state.WorktreeFingerprint = admission.SourceRevision, admission.WorktreeFingerprint
	state.Terminal = model.TerminalNonterminal
	if admission.Objective.TrustedObjectiveClass() == model.ObjectiveVerified {
		state.Delivery = model.DeliveryActive
	}
	state.Verification, state.Phase = model.VerificationCurrent, model.PhaseActive
	if verifiedObjectiveSatisfied(*state, admission.Objective) {
		state.Delivery = model.DeliveryGatesPassed
		establishTerminal(state, model.PhaseTerminal)
	}
	return nil
}

func applyPublicationObservation(state *durable.State, admission protocol.Admission, _ catalog.Transition) error {
	state.Recovery, state.Transaction = model.RecoveryNone, model.TransactionNone
	clearRecoveryContext(state)
	if state.Publication == model.PublicationUnavailable || state.Publication == model.PublicationConflicting {
		state.Phase = model.PhaseUnresolved
	} else if state.Publication == model.PublicationClosedUnmerged {
		state.Phase = model.PhaseFrontier
	} else if admission.Objective.TrustedObjectiveClass() == model.ObjectiveOpenPR && state.Publication == model.PublicationOpen {
		establishTerminal(state, model.PhaseTerminal)
	} else if admission.Objective.TrustedObjectiveClass() == model.ObjectiveMerged && state.Publication == model.PublicationMerged {
		state.Workspace, state.Delivery = model.WorkspaceLanded, model.DeliveryTerminal
		establishTerminal(state, model.PhaseTerminal)
	} else {
		state.Phase = model.PhaseActive
	}
	return nil
}

func clearRecoveryContext(state *durable.State) {
	state.TransactionID = ""
	state.TransactionTransition = ""
	state.RecoveryCause = ""
	state.RecoverySourcePhase = ""
	state.RecoveryResumption = ""
	state.RecoveryBudget = 0
}

func engagedPhase(state durable.State) model.ProtocolPhase {
	if state.Engagement == model.EngagementActive {
		return model.PhaseActive
	}
	return model.PhaseObserved
}

func settledPhase(state durable.State) model.ProtocolPhase {
	if state.Terminal == model.TerminalEstablished {
		return terminalPhase(state)
	}
	return engagedPhase(state)
}

func terminalPhase(state durable.State) model.ProtocolPhase {
	if state.Terminal == model.TerminalEstablished && state.Delivery == model.DeliveryDiscarded {
		return model.PhaseAbandoned
	}
	if state.Terminal == model.TerminalEstablished {
		return model.PhaseTerminal
	}
	return model.PhaseObserved
}

func establishTerminal(state *durable.State, phase model.ProtocolPhase) {
	state.Terminal, state.Phase = model.TerminalEstablished, phase
	if phase == model.PhaseTerminal {
		state.Delivery = model.DeliveryTerminal
	}
}

func upsertGate(state *durable.State, evidence durable.GateEvidence) {
	for index := range state.Gates {
		if state.Gates[index].Gate == evidence.Gate {
			state.Gates[index] = evidence
			return
		}
	}
	state.Gates = append(state.Gates, evidence)
}

func hasGates(state durable.State, names ...string) bool {
	found := map[string]bool{}
	for _, gate := range state.Gates {
		if gate.Revision == state.SourceRevision {
			found[gate.Gate] = true
		}
	}
	for _, name := range names {
		if !found[name] {
			return false
		}
	}
	return true
}

func verifiedObjectiveSatisfied(state durable.State, objective model.Objective) bool {
	if objective.TrustedObjectiveClass() != model.ObjectiveVerified || !hasGates(state, "build", "test", "review") {
		return false
	}
	return state.VisualEvidencePolicy != "required" || hasGates(state, "visual")
}
