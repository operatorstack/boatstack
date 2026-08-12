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
	} else if transition.ID == "objective.bind" {
		state.Objective = admission.Objective
		configured = true
	} else {
		if configured && state.Objective != admission.Objective {
			return fmt.Errorf("transition %q cannot replace configured objective; use objective.bind", transition.ID)
		}
	}
	state.LastTransition = transition.ID
	if transition.ID == "objective.bind" {
		state.Terminal = model.TerminalNonterminal
	}
	switch transition.ID {
	case "engagement.begin":
		state.Phase, state.Engagement = model.PhaseObserved, model.EngagementCommand
	case "engagement.renew":
		state.Phase, state.Engagement = model.PhaseActive, model.EngagementActive
	case "engagement.release":
		state.Phase, state.Engagement = model.PhaseDormant, model.EngagementDormant
	case "invocation.rebind", "repository.attach":
		state.Phase = model.PhaseObserved
	case "repository.detach":
		state.Phase, state.Engagement = model.PhaseDormant, model.EngagementDormant
	case "runtime.hydrate", "runtime.replace":
		state.Runtime = model.RuntimeVerified
		state.RuntimeVersion, _ = admission.Parameters.Get("runtime_version")
		state.RuntimeFingerprint, _ = admission.Parameters.Get("runtime_sha256")
		state.RuntimeSource, _ = admission.Parameters.Get("source_revision")
		state.Phase = settledPhase(*state)
	case "installation.update":
		state.Runtime = model.RuntimeVerified
		state.RuntimeVersion, _ = admission.Parameters.Get("runtime_version")
		state.RuntimeFingerprint, _ = admission.Parameters.Get("runtime_sha256")
		state.RuntimeSource, _ = admission.Parameters.Get("source_revision")
	case "installation.reconcile-update":
		accepted, _ := admission.Parameters.Get("accept_obligation_change")
		if accepted != "true" || admission.PriorProgramFingerprint == "" || admission.ProgramDeltaFingerprint == "" || state.ProgramFingerprint != admission.PriorProgramFingerprint {
			return fmt.Errorf("reconciled installation update must bind and explicitly accept the exact prior-to-candidate program delta")
		}
		state.ProgramFingerprint = admission.ExpectedProgramFingerprint
		state.Runtime = model.RuntimeVerified
		state.RuntimeVersion, _ = admission.Parameters.Get("runtime_version")
		state.RuntimeFingerprint, _ = admission.Parameters.Get("runtime_sha256")
		state.RuntimeSource, _ = admission.Parameters.Get("source_revision")
	case "runtime.reconcile":
		state.Runtime, state.Recovery, state.Transaction = model.RuntimeVerified, model.RecoveryNone, model.TransactionNone
		state.RuntimeVersion, _ = admission.Parameters.Get("runtime_version")
		state.RuntimeFingerprint, _ = admission.Parameters.Get("runtime_sha256")
		state.RuntimeSource, _ = admission.Parameters.Get("source_revision")
		clearRecoveryContext(state)
		state.Phase = settledPhase(*state)
	case "configuration.initialize", "configuration.mutate":
		state.Configuration = model.ConfigurationVerified
		state.ConfigFingerprint, _ = admission.Parameters.Get("config_sha256")
		state.Phase = settledPhase(*state)
	case "configuration.reconcile":
		state.Configuration, state.Recovery, state.Transaction = model.ConfigurationVerified, model.RecoveryNone, model.TransactionNone
		clearRecoveryContext(state)
		state.Phase = settledPhase(*state)
	case "catalog.reconcile":
		prior, _ := admission.Parameters.Get("prior_program_fingerprint")
		accepted, _ := admission.Parameters.Get("accept_obligation_change")
		if prior == "" || prior != state.ProgramFingerprint || accepted != "true" {
			return fmt.Errorf("catalog reconciliation must bind the prior program and explicitly accept obligation changes")
		}
		state.ProgramFingerprint = admission.ExpectedProgramFingerprint
	case "installation.initialize":
		state.Runtime, state.Configuration = model.RuntimeVerified, model.ConfigurationVerified
		state.RuntimeVersion, _ = admission.Parameters.Get("runtime_version")
		state.RuntimeFingerprint, _ = admission.Parameters.Get("runtime_sha256")
		state.RuntimeSource, _ = admission.Parameters.Get("source_revision")
		state.ConfigFingerprint, _ = admission.Parameters.Get("config_sha256")
		state.Phase = model.PhaseObserved
	case "objective.bind":
		wasActive := state.Phase == model.PhaseActive
		kind, _ := admission.Parameters.Get("objective_kind")
		delivery, _ := admission.Parameters.Get("delivery_id")
		if kind != string(admission.Objective.Kind) || delivery != admission.Objective.DeliveryID {
			return fmt.Errorf("objective parameters do not match admitted objective")
		}
		state.Phase = model.PhaseObserved
		if state.Recovery == model.RecoveryEscalated {
			state.Phase = model.PhaseFrontier
		} else if wasActive {
			state.Phase = model.PhaseActive
		}
	case "plan.create":
		state.Plan, state.Delivery, state.Phase, state.Terminal = model.PlanDraft, model.DeliveryPlanning, model.PhaseActive, model.TerminalNonterminal
	case "plan.validate":
		state.Plan, state.Phase, state.Terminal = model.PlanValid, model.PhaseActive, model.TerminalNonterminal
	case "plan.approve":
		state.Plan, state.Delivery, state.Phase = model.PlanApproved, model.DeliveryApproved, model.PhaseActive
		if admission.Objective.Kind == model.ObjectiveApprovedPlan {
			establishTerminal(state, model.PhaseTerminal)
		}
	case "plan.activate":
		state.Plan, state.Delivery, state.Phase = model.PlanLocked, model.DeliveryActive, model.PhaseActive
	case "plan.amend":
		state.Plan, state.Delivery, state.Phase, state.Terminal = model.PlanAmendmentRequired, model.DeliveryAmendment, model.PhaseActive, model.TerminalNonterminal
	case "plan.approve-amendment":
		state.Plan, state.Delivery, state.Phase = model.PlanApproved, model.DeliveryApproved, model.PhaseActive
	case "plan.invalidate":
		state.Plan, state.Delivery, state.Phase, state.Terminal = model.PlanInvalid, model.DeliveryInvalid, model.PhaseFrontier, model.TerminalNonterminal
	case "plan.abandon", "publication.abandon":
		state.Delivery = model.DeliveryDiscarded
		if state.Workspace != model.WorkspaceAbsent {
			state.Workspace = model.WorkspaceAbandoned
		}
		state.Recovery, state.Transaction = model.RecoveryNone, model.TransactionNone
		clearRecoveryContext(state)
		establishTerminal(state, model.PhaseAbandoned)
	case "workspace.abandon":
		state.Delivery, state.Workspace = model.DeliveryDiscarded, model.WorkspaceAbandoned
		state.Recovery, state.Transaction = model.RecoveryNone, model.TransactionNone
		clearRecoveryContext(state)
		establishTerminal(state, model.PhaseAbandoned)
	case "workspace.cut":
		state.Workspace, state.Phase = model.WorkspaceCut, model.PhaseActive
		state.WorkspaceBranch, _ = admission.Parameters.Get("branch")
		state.WorkspaceBaseRef, _ = admission.Parameters.Get("base_ref")
		state.WorkspacePath, _ = admission.Parameters.Get("destination")
		state.WorkspaceSourcePath = admission.Invocation.InvokingPath
		state.WorkspaceSourceID = admission.Invocation.WorktreeID
		state.WorkspaceSourceRef = admission.Invocation.Ref
	case "workspace.sync", "workspace.activate":
		state.Workspace, state.Phase = model.WorkspaceActive, model.PhaseActive
	case "workspace.publish":
		state.Workspace, state.Phase = model.WorkspacePublished, model.PhaseActive
	case "workspace.cleanup":
		if state.Workspace != model.WorkspaceLanded && state.Workspace != model.WorkspaceAbandoned {
			return fmt.Errorf("workspace cleanup requires landed or explicitly abandoned state")
		}
		state.Workspace = model.WorkspaceAbsent
		state.Phase = terminalPhase(*state)
	case "workspace.reap":
		state.Workspace = model.WorkspaceAbsent
		state.Phase = terminalPhase(*state)
	case "workspace.reconcile":
		state.Recovery, state.Transaction, state.Phase = model.RecoveryNone, model.TransactionNone, engagedPhase(*state)
		clearRecoveryContext(state)
	case "gate.build.record", "gate.test.record", "gate.review.record", "gate.change.record", "gate.journey.record":
		gate, _ := standardGateName(transition.ID)
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
	case "evidence.visual.attach":
		revision, _ := admission.Parameters.Get("source_revision")
		fingerprint, _ := admission.Parameters.Get("privacy_receipt")
		upsertGate(state, durable.GateEvidence{Gate: "visual", Revision: revision, Fingerprint: fingerprint})
		state.SourceRevision, state.WorktreeFingerprint = admission.SourceRevision, admission.WorktreeFingerprint
		state.Terminal = model.TerminalNonterminal
		if admission.Objective.Kind == model.ObjectiveVerified {
			state.Delivery = model.DeliveryActive
		}
		state.Verification, state.Phase = model.VerificationCurrent, model.PhaseActive
		if verifiedObjectiveSatisfied(*state, admission.Objective) {
			state.Delivery = model.DeliveryGatesPassed
			establishTerminal(state, model.PhaseTerminal)
		}
	case "evidence.approval.revoke":
		state.Plan, state.Phase, state.Terminal = model.PlanValid, model.PhaseFrontier, model.TerminalNonterminal
	case "delivery.slice.advance":
		state.Delivery, state.Phase = model.DeliveryActive, model.PhaseActive
		state.SourceRevision, state.WorktreeFingerprint = admission.SourceRevision, admission.WorktreeFingerprint
	case "publication.preview":
		state.Publication, state.Phase = model.PublicationCandidate, model.PhaseActive
	case "publication.execute":
		state.Publication, state.Workspace, state.Delivery, state.Phase = model.PublicationPublishedNotLanded, model.WorkspacePublished, model.DeliveryPublished, model.PhaseActive
	case "publication.observe", "publication.reconcile":
		state.Recovery, state.Transaction = model.RecoveryNone, model.TransactionNone
		clearRecoveryContext(state)
		if state.Publication == model.PublicationUnavailable || state.Publication == model.PublicationConflicting {
			state.Phase = model.PhaseUnresolved
		} else if state.Publication == model.PublicationClosedUnmerged {
			state.Phase = model.PhaseFrontier
		} else if admission.Objective.Kind == model.ObjectiveOpenPR && state.Publication == model.PublicationOpen {
			establishTerminal(state, model.PhaseTerminal)
		} else if admission.Objective.Kind == model.ObjectiveMerged && state.Publication == model.PublicationMerged {
			state.Workspace, state.Delivery = model.WorkspaceLanded, model.DeliveryTerminal
			establishTerminal(state, model.PhaseTerminal)
		} else {
			state.Phase = model.PhaseActive
		}
	case "publication.correct":
		state.Publication, state.Delivery = model.PublicationPublishedNotLanded, model.DeliveryPublished
		state.Terminal, state.Phase = model.TerminalNonterminal, model.PhaseActive
	case "recovery.resume":
		state.Recovery, state.Transaction, state.Delivery, state.Phase = model.RecoveryNone, model.TransactionNone, model.DeliveryActive, model.PhaseActive
		clearRecoveryContext(state)
	case "recovery.rollback":
		state.Recovery, state.Transaction, state.Phase = model.RecoveryNone, model.TransactionNone, model.PhaseObserved
		clearRecoveryContext(state)
	case "recovery.escalate":
		state.Recovery, state.Phase = model.RecoveryEscalated, model.PhaseFrontier
		state.Transaction = model.TransactionNone
		state.TransactionID, _ = admission.Parameters.Get("transaction_id")
		state.TransactionTransition = "recovery.escalate"
		state.RecoveryCause = "recovery requires explicit external resolution"
		state.RecoverySourcePhase = model.PhaseRecovery
		state.RecoveryResumption = model.PhaseFrontier
		state.RecoveryBudget = 0
	default:
		return fmt.Errorf("transition %q has no V2 state reducer", transition.ID)
	}
	if !transition.DeclaresTargetPhase(state.Phase) {
		return fmt.Errorf("transition %q reducer produced undeclared target phase %s", transition.ID, state.Phase)
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
	if objective.Kind != model.ObjectiveVerified || !hasGates(state, "build", "test", "review") {
		return false
	}
	return state.VisualEvidencePolicy != "required" || hasGates(state, "visual")
}
