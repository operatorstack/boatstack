package catalog

import (
	"fmt"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

type StateFacetPolicy struct {
	Reads  []model.StateFacet
	Writes []model.StateFacet
}

var allStateFacets = []model.StateFacet{
	model.StateFacetInstallation,
	model.StateFacetProgram,
	model.StateFacetControl,
	model.StateFacetProduct,
}

var controlStateFacets = []model.StateFacet{model.StateFacetControl}
var productStateFacets = []model.StateFacet{model.StateFacetControl, model.StateFacetProduct}
var installationStateFacets = []model.StateFacet{model.StateFacetControl, model.StateFacetInstallation}
var installationProgramStateFacets = []model.StateFacet{model.StateFacetControl, model.StateFacetInstallation, model.StateFacetProgram}
var programStateFacets = []model.StateFacet{model.StateFacetControl, model.StateFacetProgram}

// DurableStateFacetPolicy is kernel-owned. A repository program's manifest
// cannot grant itself access to installation or program durable state.
func DurableStateFacetPolicy(transition Transition) (StateFacetPolicy, error) {
	if transition.Class == EventObservedExternal {
		return StateFacetPolicy{Reads: append([]model.StateFacet(nil), allStateFacets...)}, nil
	}
	writes, known := durableStateWritesForID(transition.ID)
	if known {
		return StateFacetPolicy{Reads: append([]model.StateFacet(nil), allStateFacets...), Writes: writes}, nil
	}
	switch transition.Origin.Kind {
	case OriginControlProgram:
		if transition.RuntimeExecution {
			return StateFacetPolicy{Reads: append([]model.StateFacet(nil), allStateFacets...), Writes: append([]model.StateFacet(nil), controlStateFacets...)}, nil
		}
		return StateFacetPolicy{Reads: append([]model.StateFacet(nil), allStateFacets...), Writes: append([]model.StateFacet(nil), productStateFacets...)}, nil
	case OriginExtension:
		return StateFacetPolicy{Reads: append([]model.StateFacet(nil), allStateFacets...), Writes: append([]model.StateFacet(nil), controlStateFacets...)}, nil
	case OriginCoreSystem:
		return StateFacetPolicy{}, fmt.Errorf("core transition %q has no durable state facet policy", transition.ID)
	default:
		return StateFacetPolicy{}, fmt.Errorf("transition %q has no valid durable state facet policy origin", transition.ID)
	}
}

// DurableStateWritesForRecovery returns the write envelope for a transition
// recorded in an interrupted journal. Unknown repository-defined transitions
// fail closed to control bookkeeping only.
func DurableStateWritesForRecovery(id TransitionID) []model.StateFacet {
	if writes, ok := durableStateWritesForID(id); ok {
		return writes
	}
	return append([]model.StateFacet(nil), controlStateFacets...)
}

func durableStateWritesForID(id TransitionID) ([]model.StateFacet, bool) {
	switch id {
	case "runtime.hydrate", "runtime.replace", "runtime.reconcile", "installation.update":
		return append([]model.StateFacet(nil), installationStateFacets...), true
	case "installation.initialize", "installation.reconcile-update":
		return append([]model.StateFacet(nil), installationProgramStateFacets...), true
	case "repository.attach", "catalog.reconcile":
		return append([]model.StateFacet(nil), programStateFacets...), true
	case "invocation.rebind", "configuration.initialize", "configuration.mutate", "configuration.reconcile", "recovery.escalate":
		return append([]model.StateFacet(nil), controlStateFacets...), true
	case "engagement.begin", "engagement.renew", "engagement.release", "repository.detach", "goal.configure",
		"plan.create", "plan.validate", "plan.approve", "plan.activate", "plan.amend", "plan.approve-amendment", "plan.invalidate", "plan.abandon",
		"workspace.cut", "workspace.sync", "workspace.activate", "workspace.publish", "workspace.cleanup", "workspace.reap", "workspace.abandon", "workspace.reconcile",
		"gate.build.record", "gate.test.record", "gate.review.record", "gate.change.record", "gate.journey.record",
		"evidence.visual.attach", "evidence.approval.revoke", "delivery.slice.advance",
		"publication.preview", "publication.execute", "publication.observe", "publication.reconcile", "publication.correct", "publication.abandon":
		return append([]model.StateFacet(nil), productStateFacets...), true
	case "recovery.resume", "recovery.rollback":
		return append([]model.StateFacet(nil), controlStateFacets...), true
	case "external.files-changed", "external.head-changed", "external.branch-changed", "external.runtime-disappeared", "external.configuration-drifted", "external.lease-expired", "external.host-interrupted", "external.ci-completed", "external.pr-opened", "external.pr-updated", "external.pr-closed", "external.pr-merged", "external.provider-unavailable":
		return nil, true
	default:
		return nil, false
	}
}
