package effects

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/hostprojection"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
)

func TestHostProjectionProjectionExposesOnlyKernelMaintenance(t *testing.T) {
	// control-law: repository-flow-entries-not-kernel-modes
	files := desiredHostProjectionFiles(hostprojection.CanonicalIDs())
	counts := map[string]int{}
	for path, raw := range files {
		for _, mode := range hostProjectionModes {
			if strings.Contains(path, mode.Slug) && strings.HasSuffix(path, "SKILL.md") || strings.HasSuffix(path, mode.Slug+".md") {
				if !strings.Contains(string(raw), "name: "+mode.Slug) || !strings.Contains(string(raw), mode.Target) {
					t.Fatalf("%s does not bind %s to %s", path, mode.Slug, mode.Target)
				}
			}
		}
		switch {
		case strings.HasPrefix(path, ".agents/") && strings.HasSuffix(path, "/SKILL.md"):
			counts["codex"]++
		case strings.HasPrefix(path, ".claude/") && strings.HasSuffix(path, "/SKILL.md"):
			counts["claude"]++
		case strings.HasPrefix(path, ".gemini/") && strings.HasSuffix(path, "/SKILL.md"):
			counts["gemini"]++
		case strings.HasPrefix(path, ".cursor/") && strings.HasSuffix(path, ".md"):
			counts["cursor"]++
		}
	}
	for _, host := range []string{"codex", "claude", "gemini", "cursor"} {
		if counts[host] != 1 {
			t.Fatalf("%s discovered %d kernel operation skills, want exactly 1", host, counts[host])
		}
	}
}

func TestMaintenanceProjectionSelectionCoversAllAndNone(t *testing.T) {
	all, manifestRaw, err := ProjectedHostProjectionFiles(hostprojection.CanonicalIDs())
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 9 {
		t.Fatalf("all maintenance files = %d, want 9: %v", len(all), all)
	}
	for _, id := range hostprojection.CanonicalIDs() {
		paths, _ := hostprojection.MaintenancePaths(id)
		for _, path := range paths {
			raw, exists := all[path]
			if !exists {
				t.Fatalf("%s projection is missing %s", id, path)
			}
			if !strings.HasSuffix(path, ".gitattributes") && !strings.HasSuffix(path, "openai.yaml") && !strings.Contains(string(raw), "--host "+string(id)) {
				t.Fatalf("%s does not bind its matching host", path)
			}
		}
	}
	var manifest hostProjectionManifest
	if err := decodeHostProjectionManifest(manifestRaw, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Projections) != 4 || len(manifest.ProjectionSelectionFingerprint) != 64 {
		t.Fatalf("manifest selection = %#v", manifest)
	}
	for _, id := range hostprojection.CanonicalIDs() {
		selected, selectedManifestRaw, selectedErr := ProjectedHostProjectionFiles([]hostprojection.ID{id})
		paths, _ := hostprojection.MaintenancePaths(id)
		if selectedErr != nil || len(selected) != len(paths) {
			t.Fatalf("%s-only maintenance projection = %v, %v", id, selected, selectedErr)
		}
		var selectedManifest hostProjectionManifest
		if decodeErr := decodeHostProjectionManifest(selectedManifestRaw, &selectedManifest); decodeErr != nil || len(selectedManifest.Projections) != 1 || selectedManifest.Projections[0] != string(id) {
			t.Fatalf("%s-only manifest = %#v, %v", id, selectedManifest, decodeErr)
		}
	}
	none, noneManifestRaw, err := ProjectedHostProjectionFiles([]hostprojection.ID{})
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("empty selection generated files: %v", none)
	}
	var noneManifest hostProjectionManifest
	if err := decodeHostProjectionManifest(noneManifestRaw, &noneManifest); err != nil || len(noneManifest.Projections) != 0 || len(noneManifest.Files) != 0 {
		t.Fatalf("empty manifest = %#v, %v", noneManifest, err)
	}
}

func TestHostProjectionProjectionPreservesAuthorityBoundaries(t *testing.T) {
	// control-law: operation-trigger-selects-target-without-broadening-authority
	for path, raw := range desiredHostProjectionFiles(hostprojection.CanonicalIDs()) {
		value := string(raw)
		if strings.HasSuffix(path, "openai.yaml") || strings.HasSuffix(path, ".gitattributes") {
			continue
		}
		for _, contract := range []string{
			"authority-free\n`FRONTIER`", "command-scoped context", "every `next`, `apply`, `recover`, and re-resolution",
			"requested authority sources separately from currently\nmaterialized authority receipts",
			"complete apply response and stderr", "authority-bearing `FRONTIER`", "Never synthesize missing\nauthority", "`CANDIDATE`", "immediately preceding `PRESCRIBED`",
			"every requested authority source is materialized\nor conclusively rejected against the post-receipt state",
			"prescription ID, expected state revision", "expected program fingerprint, expected snapshot fingerprint", "correlation\nunchanged",
			"`STALE_PRESCRIPTION`", "discard the prescription, and re-resolve once",
		} {
			if !strings.Contains(value, contract) {
				t.Fatalf("%s is missing authority contract %q", path, contract)
			}
		}
	}
}

func TestHostProjectionProjectionDoesNotClaimDeliveryEntries(t *testing.T) {
	// control-law: kernel-skill-projection-cannot-invent-repository-flow-entries
	files := desiredHostProjectionFiles(hostprojection.CanonicalIDs())
	for path, raw := range files {
		if strings.HasSuffix(path, "openai.yaml") || strings.HasSuffix(path, ".gitattributes") {
			continue
		}
		value := string(raw)
		if strings.Contains(value, "boatstack-autoplan") || strings.Contains(value, "boatstack-run") {
			t.Fatalf("%s contains a hard-coded delivery entry", path)
		}
		for _, contract := range []string{
			"request only checksum-verified installation authority",
			"Do not\nrequest or materialize repository, provider, publication, product-delivery, or\nmerge authority",
			"preserve the healthy admitted\nruntime",
			"program-delta fingerprint",
			"Do not accept the delta implicitly",
			"`--accept-program-change`",
			"single atomic\n`installation.reconcile-update` boundary",
			"carry the same human authority\nthrough that rollback",
		} {
			if !strings.Contains(value, contract) {
				t.Fatalf("%s is missing update authority boundary %q", path, contract)
			}
		}
	}
}

func TestHostProjectionProjectionInventoriesEveryDriverPath(t *testing.T) {
	// control-law: generated-driver-event-slice-is-complete
	for path, raw := range desiredHostProjectionFiles(hostprojection.CanonicalIDs()) {
		if strings.HasSuffix(path, "openai.yaml") || strings.HasSuffix(path, ".gitattributes") {
			continue
		}
		value := string(raw)
		for _, pathEvent := range []string{
			"status", "`next`", "`apply`", "`recover`", "materialize", "Re-resolve", "Stop only",
		} {
			if !strings.Contains(value, pathEvent) {
				t.Fatalf("%s omits driver path event %q", path, pathEvent)
			}
		}
	}
}

func TestHostProjectionProjectionFailsClosedOnUnmanagedCollision(t *testing.T) {
	// control-law: unmanaged-host-file-cannot-be-overwritten-by-installation
	repository := t.TempDir()
	path := filepath.Join(repository, ".agents", "skills", "boatstack-update", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("user owned\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareHostProjectionMutations(repository, []hostprojection.ID{hostprojection.Codex}); err == nil || !strings.Contains(err.Error(), "unmanaged file collides") {
		t.Fatalf("collision was not rejected: %v", err)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "user owned\n" {
		t.Fatalf("collision mutated user file: %q %v", raw, err)
	}
}

func TestHostProjectionManifestRejectsUnknownFieldsAndSelectionDrift(t *testing.T) {
	files, raw, err := ProjectedHostProjectionFiles([]hostprojection.ID{hostprojection.Codex})
	if err != nil || len(files) == 0 {
		t.Fatalf("projection fixture = %v, %v", files, err)
	}
	unknown := []byte(strings.Replace(string(raw), `"files"`, `"unknown":true,"files"`, 1))
	var manifest hostProjectionManifest
	if err := decodeHostProjectionManifest(unknown, &manifest); err == nil {
		t.Fatal("unknown manifest field was accepted")
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Projections = []string{}
	forged, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := decodeHostProjectionManifest(forged, &manifest); err == nil {
		t.Fatal("selection fingerprint drift was accepted")
	}
}

func TestHostProjectionProjectionRejectsManagedDrift(t *testing.T) {
	// control-law: manifest-binds-update-bytes
	repository := t.TempDir()
	mutations, err := prepareHostProjectionMutations(repository, []hostprojection.ID{hostprojection.Codex, hostprojection.Gemini})
	if err != nil {
		t.Fatal(err)
	}
	applyMutationsForTest(t, mutations)
	gemini := filepath.Join(repository, ".gemini", "skills", "boatstack-update", "SKILL.md")
	if err := os.WriteFile(gemini, []byte("drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareHostProjectionMutations(repository, []hostprojection.ID{hostprojection.Codex}); err == nil || !strings.Contains(err.Error(), "changed outside") {
		t.Fatalf("managed drift was not rejected: %v", err)
	}
}

func TestHostProjectionProjectionRemovesOnlyManagedDisabledHosts(t *testing.T) {
	// control-law: host-removal-deletes-only-manifest-owned-projections
	repository := t.TempDir()
	if err := os.Mkdir(filepath.Join(repository, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	mutations, err := prepareHostProjectionMutations(repository, []hostprojection.ID{hostprojection.Codex, hostprojection.Gemini})
	if err != nil {
		t.Fatal(err)
	}
	applyMutationsForTest(t, mutations)
	unmanaged := filepath.Join(repository, ".gemini", "skills", "unrelated", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(unmanaged), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unmanaged, []byte("unrelated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mutations, err = prepareHostProjectionMutations(repository, []hostprojection.ID{hostprojection.Codex})
	if err != nil {
		t.Fatal(err)
	}
	applyMutationsForTest(t, mutations)
	if _, err := os.Stat(filepath.Join(repository, ".gemini", "skills", "boatstack-update", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("disabled managed Gemini skill still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repository, ".agents", "skills", "boatstack-update", "SKILL.md")); err != nil {
		t.Fatalf("enabled Codex skill was removed: %v", err)
	}
	if raw, err := os.ReadFile(unmanaged); err != nil || string(raw) != "unrelated\n" {
		t.Fatalf("unmanaged neighboring skill changed: %q %v", raw, err)
	}
}

func applyMutationsForTest(t *testing.T, mutations []ports.ResourceMutation) {
	t.Helper()
	prepared := &preparedEffect{requiredCapabilities: []catalog.Capability{catalog.CapabilityRepositoryWrite}, effectiveCapabilities: []catalog.Capability{catalog.CapabilityRepositoryWrite}, mutations: mutations}
	if _, err := prepared.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}
