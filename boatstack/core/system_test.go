package core_test

import (
	"context"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/core"
	"github.com/operatorstack/boatstack/boatstack/delivery"
)

func TestManifestOwnsOnlyOperationalCapabilities(t *testing.T) {
	// control-law: core-system-declarations-exclude-control-program-policy
	manifest, err := core.System().CoreManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ID != core.ID || manifest.Version != core.Version || len(manifest.Transitions) != 33 {
		t.Fatalf("CoreSystem identity/count = %s@%s/%d", manifest.ID, manifest.Version, len(manifest.Transitions))
	}
	for _, transition := range manifest.Transitions {
		id := string(transition.ID)
		if !hasPrefix(id, "engagement.", "invocation.", "repository.", "runtime.", "configuration.", "installation.", "catalog.", "objective.", "recovery.", "external.") {
			t.Errorf("CoreSystem owns delivery-flow transition %s", id)
		}
	}
}

func TestInstallationInitializationAcceptsExplicitlyDelegatedAutonomy(t *testing.T) {
	// control-law: exact run delegation may bootstrap verified local state
	manifest, err := core.System().CoreManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, transition := range manifest.Transitions {
		if transition.ID != "installation.initialize" {
			continue
		}
		if len(transition.Authority) != 2 || transition.Authority[0] != delivery.AuthorityHuman || transition.Authority[1] != delivery.AuthorityAutonomy {
			t.Fatalf("installation.initialize authority alternatives = %v", transition.Authority)
		}
		return
	}
	t.Fatal("installation.initialize is absent")
}

func TestObjectiveBindingIsTargetNeutral(t *testing.T) {
	// control-law: core-binds-only-domain-admitted-objectives-without-owning-domain-vocabulary
	manifest, err := core.System().CoreManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, transition := range manifest.Transitions {
		if transition.ID != "objective.bind" {
			continue
		}
		if len(transition.TargetIDs) != 0 {
			t.Fatalf("objective.bind owns domain targets %v", transition.TargetIDs)
		}
		return
	}
	t.Fatal("objective.bind is absent")
}

func TestEngagementBeginningIsTargetNeutral(t *testing.T) {
	// control-law: core-engages-only-domain-admitted-objectives-without-owning-domain-vocabulary
	manifest, err := core.System().CoreManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, transition := range manifest.Transitions {
		if transition.ID != "engagement.begin" {
			continue
		}
		if len(transition.TargetIDs) != 0 {
			t.Fatalf("engagement.begin owns domain targets %v", transition.TargetIDs)
		}
		return
	}
	t.Fatal("engagement.begin is absent")
}

func hasPrefix(value string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}
