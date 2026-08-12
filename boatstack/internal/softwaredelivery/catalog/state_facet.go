package catalog

import (
	"fmt"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
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

var declaredStateFieldFacets = map[string]model.StateFacet{
	"program_fingerprint":    model.StateFacetProgram,
	"phase":                  model.StateFacetControl,
	"engagement":             model.StateFacetProduct,
	"delivery":               model.StateFacetProduct,
	"workspace":              model.StateFacetProduct,
	"plan":                   model.StateFacetProduct,
	"configuration":          model.StateFacetControl,
	"runtime":                model.StateFacetInstallation,
	"publication":            model.StateFacetProduct,
	"verification":           model.StateFacetProduct,
	"recovery":               model.StateFacetControl,
	"transaction":            model.StateFacetControl,
	"terminal":               model.StateFacetProduct,
	"source_revision":        model.StateFacetProduct,
	"worktree_fingerprint":   model.StateFacetProduct,
	"config_fingerprint":     model.StateFacetControl,
	"runtime_version":        model.StateFacetInstallation,
	"runtime_fingerprint":    model.StateFacetInstallation,
	"runtime_source":         model.StateFacetInstallation,
	"workspace_branch":       model.StateFacetProduct,
	"workspace_path":         model.StateFacetProduct,
	"workspace_base_ref":     model.StateFacetProduct,
	"workspace_source_path":  model.StateFacetProduct,
	"workspace_source_id":    model.StateFacetProduct,
	"workspace_source_ref":   model.StateFacetProduct,
	"transaction_id":         model.StateFacetControl,
	"transaction_transition": model.StateFacetControl,
	"recovery_cause":         model.StateFacetControl,
	"recovery_source_phase":  model.StateFacetControl,
	"recovery_resumption":    model.StateFacetControl,
	"recovery_budget":        model.StateFacetControl,
}

func DeclaredStateFieldFacet(field string) (model.StateFacet, bool) {
	facet, ok := declaredStateFieldFacets[field]
	return facet, ok
}

func ValidDeclaredStateLiteral(field, value string) bool {
	switch field {
	case "phase":
		return model.ProtocolPhase(value).Valid()
	case "engagement":
		return model.EngagementState(value).Valid()
	case "delivery":
		return model.DeliveryState(value).Valid()
	case "workspace":
		return model.WorkspaceState(value).Valid()
	case "plan":
		return model.PlanState(value).Valid()
	case "configuration":
		return model.ConfigurationState(value).Valid()
	case "runtime":
		return model.RuntimeState(value).Valid()
	case "publication":
		return model.PublicationState(value).Valid()
	case "verification":
		return model.VerificationState(value).Valid()
	case "recovery":
		return model.RecoveryState(value).Valid()
	case "transaction":
		return model.TransactionState(value).Valid()
	case "terminal":
		return model.TerminalStatus(value).Valid()
	case "recovery_source_phase", "recovery_resumption":
		return value == "" || model.ProtocolPhase(value).Valid()
	case "recovery_budget":
		return value == "0"
	default:
		return true
	}
}

func declaredStateResolverFacet(field string) (model.FacetName, bool) {
	switch field {
	case "engagement":
		return model.FacetEngagement, true
	case "delivery":
		return model.FacetDelivery, true
	case "workspace":
		return model.FacetWorkspace, true
	case "plan":
		return model.FacetPlan, true
	case "configuration":
		return model.FacetConfiguration, true
	case "runtime":
		return model.FacetRuntime, true
	case "publication":
		return model.FacetPublication, true
	case "verification":
		return model.FacetVerification, true
	case "recovery":
		return model.FacetRecovery, true
	case "transaction":
		return model.FacetTransaction, true
	case "terminal":
		return model.FacetTerminal, true
	default:
		return "", false
	}
}

// DurableStateFacetPolicy projects the domain declaration into the kernel's
// OwnedFacets law. Repository-authored programs may own control and product
// state, but cannot grant themselves installation or program state.
func DurableStateFacetPolicy(transition Transition) (StateFacetPolicy, error) {
	if transition.Class == EventObservedExternal {
		return StateFacetPolicy{Reads: append([]model.StateFacet(nil), allStateFacets...)}, nil
	}
	writes, err := model.NormalizeStateFacets(string(transition.ID)+".owned_facets", transition.OwnedFacets)
	if err != nil || len(writes) == 0 {
		return StateFacetPolicy{}, fmt.Errorf("%s: controllable transition requires valid owned facets: %v", transition.ID, err)
	}
	switch transition.Origin.Kind {
	case OriginControlProgram:
		for _, facet := range writes {
			if facet == model.StateFacetInstallation || facet == model.StateFacetProgram {
				return StateFacetPolicy{}, fmt.Errorf("%s: control program cannot own %q durable state", transition.ID, facet)
			}
		}
	case OriginExtension:
		for _, facet := range writes {
			if facet != model.StateFacetControl {
				return StateFacetPolicy{}, fmt.Errorf("%s: extension cannot own %q durable state", transition.ID, facet)
			}
		}
	case OriginCoreSystem:
	default:
		return StateFacetPolicy{}, fmt.Errorf("transition %q has no valid durable state facet policy origin", transition.ID)
	}
	return StateFacetPolicy{Reads: append([]model.StateFacet(nil), allStateFacets...), Writes: writes}, nil
}
