package catalog

import (
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

func known(facet model.FacetName, values ...string) FacetCondition {
	return FacetCondition{Facet: facet, Statuses: []model.FactStatus{model.FactKnown}, Values: values}
}

func statuses(facet model.FacetName, values ...model.FactStatus) FacetCondition {
	return FacetCondition{Facet: facet, Statuses: values}
}

func requiresEngagement(id TransitionID) bool {
	value := string(id)
	return strings.HasPrefix(value, "plan.") || strings.HasPrefix(value, "workspace.") ||
		strings.HasPrefix(value, "gate.") || strings.HasPrefix(value, "evidence.") ||
		strings.HasPrefix(value, "delivery.") || strings.HasPrefix(value, "publication.") ||
		id == "configuration.mutate" || id == "installation.update"
}

func requiresHealthyConfiguration(id TransitionID) bool {
	if id == "engagement.begin" || id == "engagement.renew" || id == "engagement.release" || id == "installation.update" {
		return true
	}
	value := string(id)
	if strings.HasPrefix(value, "plan.") {
		return id != "plan.invalidate" && id != "plan.abandon"
	}
	if strings.HasPrefix(value, "workspace.") {
		return id != "workspace.abandon" && id != "workspace.reconcile"
	}
	if strings.HasPrefix(value, "gate.") || id == "evidence.visual.attach" || id == "delivery.slice.advance" {
		return true
	}
	return strings.HasPrefix(value, "publication.") && id != "publication.abandon"
}

func requiresHealthyRuntime(id TransitionID) bool {
	if id == "engagement.begin" || id == "engagement.renew" || id == "engagement.release" || id == "configuration.mutate" {
		return true
	}
	value := string(id)
	if strings.HasPrefix(value, "plan.") {
		return id != "plan.invalidate" && id != "plan.abandon"
	}
	if strings.HasPrefix(value, "workspace.") {
		return id != "workspace.abandon" && id != "workspace.reconcile"
	}
	if strings.HasPrefix(value, "gate.") || id == "evidence.visual.attach" || id == "delivery.slice.advance" {
		return true
	}
	return strings.HasPrefix(value, "publication.") && id != "publication.abandon"
}

func usesConfigurationPolicy(id TransitionID) bool {
	switch id {
	case "plan.approve", "plan.approve-amendment", "gate.review.record", "evidence.visual.attach":
		return true
	default:
		return false
	}
}

func sourceConditions(id TransitionID) []FacetCondition {
	switch id {
	case "engagement.begin":
		return []FacetCondition{known(model.FacetEngagement, string(model.EngagementDormant), string(model.EngagementCommand)), known(model.FacetGoal)}
	case "engagement.renew":
		return []FacetCondition{known(model.FacetEngagement, string(model.EngagementActive)), known(model.FacetGoal)}
	case "engagement.release":
		return []FacetCondition{known(model.FacetEngagement, string(model.EngagementActive), string(model.EngagementStale), string(model.EngagementCommand)), known(model.FacetGoal)}
	case "invocation.rebind":
		return []FacetCondition{known(model.FacetTopology)}
	case "repository.attach":
		return []FacetCondition{known(model.FacetTopology, string(model.TopologyEmbedded))}
	case "repository.detach":
		return []FacetCondition{known(model.FacetTopology, string(model.TopologyDetached), string(model.TopologyHybrid))}
	case "runtime.hydrate":
		return []FacetCondition{known(model.FacetRuntime, string(model.RuntimeAbsent), string(model.RuntimeStale), string(model.RuntimeInvalid), string(model.RuntimeConflicting), string(model.RuntimeWrongSource), string(model.RuntimePartiallyPublished))}
	case "runtime.replace", "installation.update":
		return []FacetCondition{known(model.FacetRuntime, string(model.RuntimeVerified), string(model.RuntimeStale), string(model.RuntimeInvalid), string(model.RuntimeConflicting), string(model.RuntimeWrongSource), string(model.RuntimePartiallyPublished))}
	case "runtime.reconcile", "configuration.reconcile", "workspace.reconcile":
		return []FacetCondition{known(model.FacetRecoveryInfo)}
	case "configuration.initialize":
		return []FacetCondition{known(model.FacetConfiguration, string(model.ConfigurationUnsupported), string(model.ConfigurationStale), string(model.ConfigurationConflicting))}
	case "configuration.mutate":
		return []FacetCondition{known(model.FacetConfiguration, string(model.ConfigurationVerified), string(model.ConfigurationStale), string(model.ConfigurationDivergent), string(model.ConfigurationConflicting))}
	case "installation.initialize":
		return []FacetCondition{known(model.FacetRuntime, string(model.RuntimeAbsent), string(model.RuntimeInvalid), string(model.RuntimeStale))}
	case "goal.configure":
		return []FacetCondition{statuses(model.FacetGoal, model.FactKnown, model.FactAbsent)}
	case "plan.create":
		return []FacetCondition{known(model.FacetPlan, string(model.PlanAbsent), string(model.PlanInvalid), string(model.PlanStale))}
	case "plan.validate":
		return []FacetCondition{known(model.FacetPlan, string(model.PlanDraft))}
	case "plan.approve":
		return []FacetCondition{known(model.FacetPlan, string(model.PlanValid))}
	case "plan.activate":
		return []FacetCondition{known(model.FacetPlan, string(model.PlanApproved))}
	case "plan.amend":
		return []FacetCondition{known(model.FacetPlan, string(model.PlanApproved), string(model.PlanLocked), string(model.PlanStale), string(model.PlanAmendmentRequired))}
	case "plan.approve-amendment":
		return []FacetCondition{known(model.FacetPlan, string(model.PlanAmendmentRequired))}
	case "plan.invalidate":
		return []FacetCondition{known(model.FacetPlan, string(model.PlanDraft), string(model.PlanValid), string(model.PlanApproved), string(model.PlanLocked), string(model.PlanStale))}
	case "plan.abandon", "publication.abandon":
		return []FacetCondition{known(model.FacetDelivery, string(model.DeliveryUninitialized), string(model.DeliveryPlanning), string(model.DeliveryApproved), string(model.DeliveryActive), string(model.DeliveryGatesPassed), string(model.DeliveryPublished), string(model.DeliveryAmendment), string(model.DeliveryInvalid), string(model.DeliveryRecovery))}
	case "workspace.cut":
		return []FacetCondition{known(model.FacetWorkspace, string(model.WorkspaceAbsent))}
	case "workspace.sync", "workspace.activate":
		return []FacetCondition{known(model.FacetWorkspace, string(model.WorkspaceCut), string(model.WorkspaceActive))}
	case "workspace.publish":
		return []FacetCondition{known(model.FacetWorkspace, string(model.WorkspaceActive))}
	case "workspace.cleanup", "workspace.reap":
		return []FacetCondition{known(model.FacetWorkspace, string(model.WorkspaceLanded), string(model.WorkspaceAbandoned))}
	case "workspace.abandon":
		return []FacetCondition{known(model.FacetWorkspace, string(model.WorkspaceCut), string(model.WorkspaceActive), string(model.WorkspacePublished), string(model.WorkspaceAttentionRequired))}
	case "gate.build.record", "gate.test.record", "gate.review.record", "gate.change.record", "gate.journey.record":
		return []FacetCondition{known(model.FacetPlan, string(model.PlanLocked)), known(model.FacetDelivery, string(model.DeliveryActive), string(model.DeliveryGatesPassed))}
	case "evidence.visual.attach":
		return []FacetCondition{known(model.FacetDelivery, string(model.DeliveryActive), string(model.DeliveryGatesPassed), string(model.DeliveryPublished))}
	case "evidence.approval.revoke":
		return []FacetCondition{known(model.FacetPlan, string(model.PlanApproved), string(model.PlanLocked))}
	case "delivery.slice.advance":
		return []FacetCondition{known(model.FacetDelivery, string(model.DeliveryActive))}
	case "publication.preview":
		return []FacetCondition{
			known(model.FacetPlan, string(model.PlanLocked)),
			known(model.FacetVerification, string(model.VerificationCurrent)),
			known(model.FacetWorkspace, string(model.WorkspaceActive), string(model.WorkspacePublished)),
		}
	case "publication.execute":
		return []FacetCondition{
			known(model.FacetPublication, string(model.PublicationCandidate)),
			known(model.FacetVerification, string(model.VerificationCurrent)),
			known(model.FacetWorkspace, string(model.WorkspaceActive), string(model.WorkspacePublished)),
		}
	case "publication.observe":
		return []FacetCondition{known(model.FacetPublication, string(model.PublicationCandidate), string(model.PublicationPublishedNotLanded), string(model.PublicationOpen), string(model.PublicationClosedUnmerged), string(model.PublicationUnavailable), string(model.PublicationConflicting))}
	case "publication.reconcile":
		return []FacetCondition{known(model.FacetRecoveryInfo), known(model.FacetPublication)}
	case "publication.correct":
		return []FacetCondition{known(model.FacetPublication, string(model.PublicationOpen)), known(model.FacetVerification, string(model.VerificationCurrent))}
	case "recovery.resume", "recovery.rollback", "recovery.escalate":
		return []FacetCondition{known(model.FacetRecoveryInfo)}
	case "external.files-changed", "external.head-changed":
		return []FacetCondition{known(model.FacetVerification)}
	case "external.branch-changed":
		return []FacetCondition{known(model.FacetWorkspace)}
	case "external.runtime-disappeared":
		return []FacetCondition{known(model.FacetRuntime, string(model.RuntimeVerified), string(model.RuntimeStale))}
	case "external.configuration-drifted":
		return []FacetCondition{known(model.FacetConfiguration, string(model.ConfigurationVerified))}
	case "external.lease-expired":
		return []FacetCondition{known(model.FacetEngagement, string(model.EngagementActive), string(model.EngagementCommand))}
	case "external.host-interrupted":
		return []FacetCondition{known(model.FacetTransaction, string(model.TransactionStaged), string(model.TransactionLocalApplied), string(model.TransactionExternalUncertain), string(model.TransactionVerifying)), known(model.FacetTransactionInfo)}
	case "external.ci-completed":
		return []FacetCondition{known(model.FacetVerification)}
	case "external.pr-opened", "external.pr-updated", "external.pr-closed", "external.pr-merged", "external.provider-unavailable":
		return []FacetCondition{known(model.FacetPublication)}
	default:
		return nil
	}
}

func targetConditions(id TransitionID) []FacetCondition {
	switch id {
	case "engagement.begin":
		return []FacetCondition{known(model.FacetEngagement, string(model.EngagementCommand))}
	case "engagement.renew":
		return []FacetCondition{known(model.FacetEngagement, string(model.EngagementActive))}
	case "engagement.release", "repository.detach":
		return []FacetCondition{known(model.FacetEngagement, string(model.EngagementDormant))}
	case "invocation.rebind":
		return []FacetCondition{known(model.FacetTopology)}
	case "repository.attach":
		return []FacetCondition{known(model.FacetTopology, string(model.TopologyDetached), string(model.TopologyHybrid))}
	case "runtime.hydrate", "runtime.replace", "installation.update":
		return []FacetCondition{known(model.FacetRuntime, string(model.RuntimeVerified))}
	case "runtime.reconcile":
		return []FacetCondition{known(model.FacetRuntime, string(model.RuntimeVerified)), known(model.FacetTransaction, string(model.TransactionNone))}
	case "configuration.initialize", "configuration.mutate":
		return []FacetCondition{known(model.FacetConfiguration, string(model.ConfigurationVerified))}
	case "configuration.reconcile":
		return []FacetCondition{known(model.FacetConfiguration, string(model.ConfigurationVerified)), known(model.FacetTransaction, string(model.TransactionNone))}
	case "installation.initialize":
		return []FacetCondition{known(model.FacetRuntime, string(model.RuntimeVerified)), known(model.FacetConfiguration, string(model.ConfigurationVerified))}
	case "goal.configure":
		return []FacetCondition{known(model.FacetGoal)}
	case "plan.create":
		return []FacetCondition{known(model.FacetPlan, string(model.PlanDraft))}
	case "plan.validate":
		return []FacetCondition{known(model.FacetPlan, string(model.PlanValid))}
	case "plan.approve", "plan.approve-amendment":
		return []FacetCondition{known(model.FacetPlan, string(model.PlanApproved))}
	case "plan.activate":
		return []FacetCondition{known(model.FacetPlan, string(model.PlanLocked))}
	case "plan.amend":
		return []FacetCondition{known(model.FacetPlan, string(model.PlanAmendmentRequired))}
	case "plan.invalidate":
		return []FacetCondition{known(model.FacetPlan, string(model.PlanInvalid))}
	case "plan.abandon", "publication.abandon":
		return []FacetCondition{
			known(model.FacetDelivery, string(model.DeliveryDiscarded)),
			known(model.FacetRecovery, string(model.RecoveryNone)),
			known(model.FacetTransaction, string(model.TransactionNone)),
		}
	case "workspace.cut":
		return []FacetCondition{known(model.FacetWorkspace, string(model.WorkspaceCut))}
	case "workspace.sync", "workspace.activate":
		return []FacetCondition{known(model.FacetWorkspace, string(model.WorkspaceActive))}
	case "workspace.publish":
		return []FacetCondition{known(model.FacetWorkspace, string(model.WorkspacePublished))}
	case "workspace.cleanup", "workspace.reap":
		return []FacetCondition{known(model.FacetWorkspace, string(model.WorkspaceAbsent))}
	case "workspace.abandon":
		return []FacetCondition{
			known(model.FacetWorkspace, string(model.WorkspaceAbandoned)),
			known(model.FacetRecovery, string(model.RecoveryNone)),
			known(model.FacetTransaction, string(model.TransactionNone)),
		}
	case "workspace.reconcile", "recovery.resume", "recovery.rollback":
		return []FacetCondition{known(model.FacetRecovery, string(model.RecoveryNone)), known(model.FacetTransaction, string(model.TransactionNone))}
	case "gate.build.record", "gate.test.record", "gate.review.record", "gate.change.record", "gate.journey.record":
		return []FacetCondition{known(model.FacetVerification, string(model.VerificationCurrent))}
	case "evidence.visual.attach":
		return []FacetCondition{known(model.FacetDelivery)}
	case "evidence.approval.revoke":
		return []FacetCondition{known(model.FacetPlan, string(model.PlanValid))}
	case "delivery.slice.advance":
		return []FacetCondition{known(model.FacetDelivery, string(model.DeliveryActive))}
	case "publication.preview":
		return []FacetCondition{known(model.FacetPublication, string(model.PublicationCandidate))}
	case "publication.execute":
		return []FacetCondition{known(model.FacetPublication, string(model.PublicationPublishedNotLanded))}
	case "publication.observe":
		return []FacetCondition{known(model.FacetPublication, string(model.PublicationOpen), string(model.PublicationMerged), string(model.PublicationClosedUnmerged), string(model.PublicationUnavailable), string(model.PublicationConflicting))}
	case "publication.reconcile":
		return []FacetCondition{known(model.FacetPublication, string(model.PublicationOpen), string(model.PublicationMerged), string(model.PublicationClosedUnmerged), string(model.PublicationUnavailable), string(model.PublicationConflicting)), known(model.FacetTransaction, string(model.TransactionNone))}
	case "publication.correct":
		return []FacetCondition{known(model.FacetPublication, string(model.PublicationPublishedNotLanded))}
	case "recovery.escalate":
		return []FacetCondition{known(model.FacetRecovery, string(model.RecoveryEscalated)), known(model.FacetTransaction, string(model.TransactionNone))}
	case "external.files-changed", "external.head-changed", "external.ci-completed":
		return []FacetCondition{known(model.FacetVerification)}
	case "external.branch-changed":
		return []FacetCondition{known(model.FacetWorkspace)}
	case "external.runtime-disappeared":
		return []FacetCondition{known(model.FacetRuntime, string(model.RuntimeAbsent), string(model.RuntimeStale))}
	case "external.configuration-drifted":
		return []FacetCondition{known(model.FacetConfiguration, string(model.ConfigurationStale), string(model.ConfigurationDivergent), string(model.ConfigurationConflicting))}
	case "external.lease-expired":
		return []FacetCondition{known(model.FacetEngagement, string(model.EngagementDormant), string(model.EngagementStale))}
	case "external.host-interrupted":
		return []FacetCondition{known(model.FacetRecovery)}
	case "external.pr-opened", "external.pr-updated":
		return []FacetCondition{known(model.FacetPublication, string(model.PublicationOpen))}
	case "external.pr-closed":
		return []FacetCondition{known(model.FacetPublication, string(model.PublicationClosedUnmerged), string(model.PublicationMerged))}
	case "external.pr-merged":
		return []FacetCondition{known(model.FacetPublication, string(model.PublicationMerged))}
	case "external.provider-unavailable":
		return []FacetCondition{known(model.FacetPublication, string(model.PublicationUnavailable))}
	default:
		return nil
	}
}

func requiredAuthorities(id TransitionID) []AuthorityClass {
	switch id {
	case "publication.execute", "publication.correct":
		return []AuthorityClass{AuthorityProvider}
	default:
		return nil
	}
}
