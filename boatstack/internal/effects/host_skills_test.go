package effects

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/ports"
)

func TestHostSkillProjectionExposesExactlyThreeOperationsPerInteractiveHost(t *testing.T) {
	// control-law: enabled-hosts-receive-exactly-three-canonical-operation-skills
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
		if counts[host] != 3 {
			t.Fatalf("%s discovered %d operation skills, want exactly 3", host, counts[host])
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
			"complete apply response and stderr", "authority-bearing `FRONTIER`", "Never synthesize missing\nauthority",
		} {
			if !strings.Contains(value, contract) {
				t.Fatalf("%s is missing authority contract %q", path, contract)
			}
		}
	}
}

func TestHostSkillProjectionFailsClosedOnUnmanagedCollision(t *testing.T) {
	// control-law: unmanaged-host-file-cannot-be-overwritten-by-installation
	repository := t.TempDir()
	path := filepath.Join(repository, ".agents", "skills", "boatstack-run", "SKILL.md")
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
	gemini := filepath.Join(repository, ".gemini", "skills", "boatstack-run", "SKILL.md")
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
	if _, err := os.Stat(filepath.Join(repository, ".gemini", "skills", "boatstack-run", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("disabled managed Gemini skill still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repository, ".agents", "skills", "boatstack-run", "SKILL.md")); err != nil {
		t.Fatalf("enabled Codex skill was removed: %v", err)
	}
	if raw, err := os.ReadFile(unmanaged); err != nil || string(raw) != "unrelated\n" {
		t.Fatalf("unmanaged neighboring skill changed: %q %v", raw, err)
	}
}

func applyMutationsForTest(t *testing.T, mutations []ports.ResourceMutation) {
	t.Helper()
	prepared := &preparedEffect{mutations: mutations}
	if _, err := prepared.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}
