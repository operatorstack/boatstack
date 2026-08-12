package durable

import (
	"reflect"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

func facetFixture() State {
	return Default(model.InvocationContext{RepositoryID: "repo", GitCommonID: "git", WorktreeID: "worktree"}, time.Unix(100, 0).UTC())
}

func TestEveryDurableStateFieldHasOneFacetOwner(t *testing.T) {
	if err := ValidateStateFieldFacets(); err != nil {
		t.Fatal(err)
	}
	if got, want := len(StateFieldFacets()), reflect.TypeOf(State{}).NumField(); got != want {
		t.Fatalf("classified fields=%d, want %d", got, want)
	}
}

func TestChangedFacetsPreservesExactDomainValues(t *testing.T) {
	before := facetFixture()
	before.ProgramFingerprint = "program-a"
	before.RuntimeVersion = "runtime-a"
	before.Objective = model.Objective{ID: "objective-a", Kind: model.ObjectiveOpenPR, DeliveryID: "delivery-a"}
	after := before
	after.RuntimeVersion = "runtime-b"
	after.Revision++
	facets, err := ChangedFacets(before, after)
	if err != nil {
		t.Fatal(err)
	}
	want := []model.StateFacet{model.StateFacetControl, model.StateFacetInstallation}
	if !reflect.DeepEqual(facets, want) {
		t.Fatalf("changed facets=%v, want %v", facets, want)
	}
	if after.ProgramFingerprint != before.ProgramFingerprint || after.Objective != before.Objective {
		t.Fatal("facet classification changed legacy program or product values")
	}
}
