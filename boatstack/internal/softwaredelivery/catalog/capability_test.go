package catalog

import (
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

func TestCapabilityVocabularyFailsClosed(t *testing.T) {
	if _, err := NormalizeCapabilities("test", []Capability{"production.nuke"}); err == nil {
		t.Fatal("unknown capability was ignored")
	}
	if _, err := NormalizeCapabilities("test", []Capability{CapabilityRepositoryWrite, CapabilityRepositoryWrite}); err == nil {
		t.Fatal("duplicate capability was accepted")
	}
}

func TestKernelEffectClassificationCannotBeWeakenedByTransitionDeclaration(t *testing.T) {
	// control-law: repository-authored requirements cannot under-classify a kernel effect
	transition := Transition{
		ID: "program/publish", Class: EventOwnedExternal, Effect: "publication.execute",
		OwnedFacets:          []model.StateFacet{model.StateFacetControl, model.StateFacetProduct},
		RequiredCapabilities: []Capability{CapabilityRepositoryWrite},
		DeclaredCapabilities: []Capability{CapabilityRepositoryWrite, CapabilityCommandExecute, CapabilityProductMutate, CapabilityPublicationPublish},
	}
	required := NewCapabilitySet(RequiredCapabilities(transition)...)
	for _, capability := range []Capability{CapabilityRepositoryWrite, CapabilityCommandExecute, CapabilityProductMutate, CapabilityPublicationPublish} {
		if !required[capability] {
			t.Errorf("publication.execute omitted kernel capability %q", capability)
		}
	}
}

func TestProductOwnershipRequiresProductMutationForArbitraryEffect(t *testing.T) {
	transition := Transition{
		ID: "program/advance", Class: EventOwnedLocal, Effect: "acme.advance",
		OwnedFacets:          []model.StateFacet{model.StateFacetControl, model.StateFacetProduct},
		RequiredCapabilities: []Capability{CapabilityRepositoryWrite},
	}
	required := NewCapabilitySet(RequiredCapabilities(transition)...)
	if !required[CapabilityProductMutate] {
		t.Fatalf("product-owning arbitrary effect was under-classified: %v", required.Sorted())
	}
}

func TestCapabilityClassesHaveNoImplicitHierarchy(t *testing.T) {
	granted := AuthorityCapabilities(AuthoritySet{AuthorityProvider: true})
	if !granted[CapabilityPublicationPublish] || granted[CapabilityPublicationPrepare] || granted[CapabilityRepositoryWrite] {
		t.Fatalf("provider capability mapping widened implicitly: %v", granted.Sorted())
	}
	granted = AuthorityCapabilities(AuthoritySet{AuthorityRepository: true})
	if granted[CapabilityPublicationPublish] || granted[CapabilityHumanApprove] {
		t.Fatalf("repository authority gained publication or approval: %v", granted.Sorted())
	}
}
