package durable

import (
	"fmt"
	"reflect"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

var stateFieldFacets = map[string]model.StateFacet{
	"SchemaVersion": model.StateFacetControl, "RepositoryID": model.StateFacetControl, "GitCommonID": model.StateFacetControl, "WorktreeID": model.StateFacetControl,
	"ProgramFingerprint": model.StateFacetProgram,
	"Revision":           model.StateFacetControl, "Phase": model.StateFacetControl,
	"Engagement": model.StateFacetProduct, "Delivery": model.StateFacetProduct, "Workspace": model.StateFacetProduct, "Plan": model.StateFacetProduct,
	"Configuration": model.StateFacetControl,
	"Runtime":       model.StateFacetInstallation,
	"Publication":   model.StateFacetProduct, "Verification": model.StateFacetProduct,
	"Recovery": model.StateFacetControl, "Transaction": model.StateFacetControl,
	"Terminal": model.StateFacetProduct, "Objective": model.StateFacetProduct,
	"SourceRevision": model.StateFacetProduct, "WorktreeFingerprint": model.StateFacetProduct,
	"ConfigFingerprint": model.StateFacetControl, "PlanApprovalPolicy": model.StateFacetControl, "VisualEvidencePolicy": model.StateFacetControl,
	"ExternalEffectPolicy": model.StateFacetControl, "IndependentReview": model.StateFacetControl, "EnabledHosts": model.StateFacetControl,
	"RuntimeVersion": model.StateFacetInstallation, "RuntimeFingerprint": model.StateFacetInstallation, "RuntimeSource": model.StateFacetInstallation,
	"PlanFingerprint": model.StateFacetProduct, "ApprovalFingerprint": model.StateFacetProduct,
	"WorkspaceBranch": model.StateFacetProduct, "WorkspacePath": model.StateFacetProduct, "WorkspaceBaseRef": model.StateFacetProduct,
	"WorkspaceSourcePath": model.StateFacetProduct, "WorkspaceSourceID": model.StateFacetProduct, "WorkspaceSourceRef": model.StateFacetProduct,
	"PublicationID": model.StateFacetProduct, "PublicationURL": model.StateFacetProduct, "PreviewFingerprint": model.StateFacetProduct,
	"TransactionID": model.StateFacetControl, "TransactionTransition": model.StateFacetControl, "RecoveryCause": model.StateFacetControl,
	"RecoverySourcePhase": model.StateFacetControl, "RecoveryResumption": model.StateFacetControl, "RecoveryBudget": model.StateFacetControl,
	"LastTransition": model.StateFacetControl, "Gates": model.StateFacetProduct, "UpdatedAt": model.StateFacetControl,
}

func StateFieldFacets() map[string]model.StateFacet {
	result := make(map[string]model.StateFacet, len(stateFieldFacets))
	for field, facet := range stateFieldFacets {
		result[field] = facet
	}
	return result
}

func ValidateStateFieldFacets() error {
	typeOfState := reflect.TypeOf(State{})
	if len(stateFieldFacets) != typeOfState.NumField() {
		return fmt.Errorf("STATE_FACET_UNCLASSIFIED: durable State has %d fields but %d facet assignments", typeOfState.NumField(), len(stateFieldFacets))
	}
	for index := 0; index < typeOfState.NumField(); index++ {
		field := typeOfState.Field(index)
		facet, ok := stateFieldFacets[field.Name]
		if !ok || !facet.Valid() {
			return fmt.Errorf("STATE_FACET_UNCLASSIFIED: durable State field %s has no valid owner", field.Name)
		}
	}
	return nil
}

func ChangedFacets(before, after State) ([]model.StateFacet, error) {
	if err := ValidateStateFieldFacets(); err != nil {
		return nil, err
	}
	typeOfState := reflect.TypeOf(before)
	beforeValue, afterValue := reflect.ValueOf(before), reflect.ValueOf(after)
	changed := map[model.StateFacet]bool{}
	for index := 0; index < typeOfState.NumField(); index++ {
		if !reflect.DeepEqual(beforeValue.Field(index).Interface(), afterValue.Field(index).Interface()) {
			changed[stateFieldFacets[typeOfState.Field(index).Name]] = true
		}
	}
	result := make([]model.StateFacet, 0, len(changed))
	for facet := range changed {
		result = append(result, facet)
	}
	return model.NormalizeStateFacets("changed state facets", result)
}
