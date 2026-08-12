package effects

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
)

func TestHostSkillProjectionExposesOnlyKernelMaintenance(t *testing.T) {
	// control-law: repository-flow-entries-not-kernel-modes
	files := desiredHostSkillFiles([]string{"cli", "cursor", "codex", "claude", "gemini", "mcp"})
	counts := map[string]int{}
	for path, raw := range files {
		for _, mode := range hostSkillModes {
			if strings.Contains(path, mode.Slug) && strings.HasSuffix(path, "SKILL.md") || strings.HasSuffix(path, mode.Slug+".md") {
				if !strings.Contains(string(raw), "name: "+mode.Slug) || !strings.Contains(string(raw), mode.Target) {
					t.Fatalf("%s does not bind %s to %s", path, mode.Slug, mode.Target)
				}
			}
		}
		switch {
		case strings.HasPrefix(path, ".agents/") && strings.HasSuffix(path, "/SKILL.md"):
			counts["codex"]++
		case strings.HasPrefix(path, ".claude/"):
			counts["claude"]++
		case strings.HasPrefix(path, ".gemini/"):
			counts["gemini"]++
		case strings.HasPrefix(path, ".cursor/"):
			counts["cursor"]++
		}
	}
	for _, host := range []string{"codex", "claude", "gemini", "cursor"} {
		if counts[host] != 1 {
			t.Fatalf("%s discovered %d kernel operation skills, want exactly 1", host, counts[host])
		}
	}
}

func TestHostSkillProjectionPreservesAuthorityBoundaries(t *testing.T) {
	// control-law: operation-trigger-selects-target-without-broadening-authority
	for path, raw := range desiredHostSkillFiles([]string{"cursor", "codex", "claude", "gemini"}) {
		value := string(raw)
		if strings.HasSuffix(path, "openai.yaml") {
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

func TestHostSkillProjectionDoesNotClaimDeliveryEntries(t *testing.T) {
	// control-law: kernel-skill-projection-cannot-invent-repository-flow-entries
	files := desiredHostSkillFiles([]string{"cursor", "codex", "claude", "gemini"})
	for path, raw := range files {
		if strings.HasSuffix(path, "openai.yaml") {
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

func TestHostSkillProjectionInventoriesEveryDriverPath(t *testing.T) {
	// control-law: generated-driver-event-slice-is-complete
	for path, raw := range desiredHostSkillFiles([]string{"cursor", "codex", "claude", "gemini"}) {
		if strings.HasSuffix(path, "openai.yaml") {
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

func TestHostSkillProjectionFailsClosedOnUnmanagedCollision(t *testing.T) {
	// control-law: unmanaged-host-file-cannot-be-overwritten-by-installation
	repository := t.TempDir()
	path := filepath.Join(repository, ".agents", "skills", "boatstack-update", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("user owned\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareHostSkillMutations(repository, []string{"codex"}); err == nil || !strings.Contains(err.Error(), "unmanaged file collides") {
		t.Fatalf("collision was not rejected: %v", err)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "user owned\n" {
		t.Fatalf("collision mutated user file: %q %v", raw, err)
	}
}

func TestHostSkillProjectionRejectsManagedDrift(t *testing.T) {
	// control-law: manifest-binds-update-bytes
	repository := t.TempDir()
	mutations, err := prepareHostSkillMutations(repository, []string{"codex", "gemini"})
	if err != nil {
		t.Fatal(err)
	}
	applyMutationsForTest(t, mutations)
	gemini := filepath.Join(repository, ".gemini", "skills", "boatstack-update", "SKILL.md")
	if err := os.WriteFile(gemini, []byte("drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareHostSkillMutations(repository, []string{"codex"}); err == nil || !strings.Contains(err.Error(), "changed outside") {
		t.Fatalf("managed drift was not rejected: %v", err)
	}
}

func TestHostSkillProjectionRemovesOnlyManagedDisabledHosts(t *testing.T) {
	// control-law: host-removal-deletes-only-manifest-owned-projections
	repository := t.TempDir()
	mutations, err := prepareHostSkillMutations(repository, []string{"codex", "gemini"})
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
	mutations, err = prepareHostSkillMutations(repository, []string{"codex"})
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
