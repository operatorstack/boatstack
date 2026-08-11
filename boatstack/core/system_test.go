package core_test

import (
	"context"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/core"
)

func TestManifestOwnsOnlyOperationalCapabilities(t *testing.T) {
	// control-law: core-system-declarations-exclude-primary-flow-policy
	manifest, err := core.System().CoreManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ID != core.ID || manifest.Version != core.Version || len(manifest.Transitions) != 33 {
		t.Fatalf("CoreSystem identity/count = %s@%s/%d", manifest.ID, manifest.Version, len(manifest.Transitions))
	}
	for _, transition := range manifest.Transitions {
		id := string(transition.ID)
		if !hasPrefix(id, "engagement.", "invocation.", "repository.", "runtime.", "configuration.", "installation.", "catalog.", "goal.", "recovery.", "external.") {
			t.Errorf("CoreSystem owns delivery-flow transition %s", id)
		}
	}
}

func hasPrefix(value string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}
