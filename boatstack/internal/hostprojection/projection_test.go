package hostprojection

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseRequiresExplicitSubsetAndCanonicalizes(t *testing.T) {
	if _, err := Parse(nil, []string{"cli"}); err == nil || !strings.Contains(err.Error(), "PROJECT_PROJECTIONS_REQUIRED") {
		t.Fatalf("missing projections error = %v", err)
	}
	got, err := Parse([]string{"gemini", "codex"}, []string{"cli", "codex", "gemini"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []ID{Codex, Gemini}; !reflect.DeepEqual(got, want) {
		t.Fatalf("projections = %v, want %v", got, want)
	}
	for _, test := range []struct {
		values []string
		hosts  []string
		code   string
	}{
		{[]string{"cli"}, []string{"cli"}, "PROJECT_PROJECTION_INVALID"},
		{[]string{"codex", "codex"}, []string{"cli", "codex"}, "PROJECT_PROJECTION_INVALID"},
		{[]string{"cursor"}, []string{"cli"}, "PROJECT_PROJECTION_HOST_DISABLED"},
	} {
		if _, err := Parse(test.values, test.hosts); err == nil || !strings.Contains(err.Error(), test.code) {
			t.Fatalf("Parse(%v, %v) error = %v, want %s", test.values, test.hosts, err, test.code)
		}
	}
	if got, err := Parse([]string{}, []string{"cli"}); err != nil || len(got) != 0 {
		t.Fatalf("explicit empty selection = %v, %v", got, err)
	}
}

func TestSelectionFingerprintCanonicalizesOrderAndMembership(t *testing.T) {
	one, err := SelectionFingerprint([]ID{Cursor, Codex})
	if err != nil {
		t.Fatal(err)
	}
	two, _ := SelectionFingerprint([]ID{Codex, Cursor})
	three, _ := SelectionFingerprint([]ID{Codex})
	if one != two || one == three || one != "b86bf1f15f6aace0a6153be663f57dd604c2d6f92df3c6921b593cc6fb4d4370" {
		t.Fatalf("fingerprints one=%s two=%s three=%s", one, two, three)
	}
	if !ValidSHA256(one) || ValidSHA256(strings.ToUpper(one)) || ValidSHA256(strings.Repeat("g", 64)) {
		t.Fatal("lowercase SHA-256 validation accepted a non-canonical digest")
	}
}

func TestFlowPathsAreInjectiveAndStrict(t *testing.T) {
	seen := map[string]bool{}
	for _, id := range CanonicalIDs() {
		for _, slug := range []string{"product-delivery-run", "x0-product-x0-run"} {
			paths, err := FlowPaths(id, slug)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range paths {
				if seen[path] || !ValidFlowPath(path) {
					t.Fatalf("non-injective or invalid path %q", path)
				}
				seen[path] = true
			}
		}
	}
	for _, id := range []ID{Cursor, Gemini} {
		path, content, ok := SharedCheckoutPath(id)
		if !ok || len(content) == 0 || !ValidFlowPath(path) || !IsSharedCheckoutPath(path) {
			t.Fatalf("%s shared checkout path = %q, %q, %v", id, path, content, ok)
		}
	}
	for _, path := range []string{"/tmp/SKILL.md", "../SKILL.md", `.cursor\\commands\\run.md`, ".agents/skills/boatstack-update/SKILL.md", ".gemini/skills//SKILL.md"} {
		if ValidFlowPath(path) {
			t.Fatalf("unsafe path accepted: %q", path)
		}
	}
}

func TestMaintenancePathsAreCanonicalAndInjective(t *testing.T) {
	seen := map[string]ID{}
	for _, id := range CanonicalIDs() {
		paths, err := MaintenancePaths(id)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			if owner, exists := seen[path]; exists {
				t.Fatalf("%s is shared by %s and %s", path, owner, id)
			}
			seen[path] = id
			if !ValidMaintenancePath(path) {
				t.Fatalf("canonical maintenance path rejected: %s", path)
			}
		}
	}
	for _, path := range []string{"legacy.json", ".cursor/commands/../escape.md", ".agents/skills/other/SKILL.md"} {
		if ValidMaintenancePath(path) {
			t.Fatalf("unsafe or unrelated path accepted: %s", path)
		}
	}
}
