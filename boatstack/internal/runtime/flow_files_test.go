package runtime

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func resolvedTemporaryRepository(t *testing.T) string {
	t.Helper()
	repository, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return repository
}

func TestFlowProjectionRejectsRepositoryParentSymlink(t *testing.T) {
	// control-law: generated-projections-never-traverse-repository-symlinks
	repository := resolvedTemporaryRepository(t)
	external := resolvedTemporaryRepository(t)
	if err := os.Symlink(external, filepath.Join(repository, ".agents")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	target := filepath.Join(repository, ".agents", "skills", "product-delivery-run", "SKILL.md")
	err := ApplyFlowProjection(repository, []ProjectionWrite{{Path: target, Content: []byte("generated"), Mode: 0o644}}, nil)
	if err == nil || !strings.Contains(err.Error(), "repository symlink") {
		t.Fatalf("symlink projection result = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(external, "skills", "product-delivery-run", "SKILL.md")); !os.IsNotExist(statErr) {
		t.Fatalf("projection escaped repository: %v", statErr)
	}
}

func TestFlowProjectionStagesAllOutputsBeforeReplacement(t *testing.T) {
	// control-law: artifact-and-skills-replace-as-one-recoverable-projection
	if runtime.GOOS == "windows" {
		t.Skip("directory permission failure is not portable to Windows")
	}
	repository := resolvedTemporaryRepository(t)
	artifactDirectory := filepath.Join(repository, ".boatstack", "flows")
	artifactPath := filepath.Join(artifactDirectory, "product-delivery.flow.ir.json")
	retiredPath := filepath.Join(repository, ".agents", "skills", "old-run", "SKILL.md")
	newSkillPath := filepath.Join(repository, ".agents", "skills", "product-delivery-run", "SKILL.md")
	for path, content := range map[string][]byte{artifactPath: []byte("old artifact"), retiredPath: []byte("old skill")} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(artifactDirectory, 0o555); err != nil {
		t.Fatal(err)
	}
	writes := []ProjectionWrite{
		{Path: newSkillPath, Content: []byte("new skill"), Mode: 0o644},
		{Path: artifactPath, Content: []byte("new artifact"), Mode: 0o644},
	}
	err := ApplyFlowProjection(repository, writes, []string{retiredPath})
	if restoreErr := os.Chmod(artifactDirectory, 0o755); restoreErr != nil {
		t.Fatal(restoreErr)
	}
	if err == nil {
		t.Skip("filesystem did not enforce the staging permission failure")
	}
	for path, expected := range map[string]string{artifactPath: "old artifact", retiredPath: "old skill"} {
		actual, readErr := os.ReadFile(path)
		if readErr != nil || string(actual) != expected {
			t.Fatalf("prior projection %s changed: %q, %v", path, actual, readErr)
		}
	}
	if _, statErr := os.Stat(newSkillPath); !os.IsNotExist(statErr) {
		t.Fatalf("new skill became visible before artifact staging: %v", statErr)
	}
	if err := ApplyFlowProjection(repository, writes, []string{retiredPath}); err != nil {
		t.Fatal(err)
	}
	for path, expected := range map[string]string{artifactPath: "new artifact", newSkillPath: "new skill"} {
		actual, readErr := os.ReadFile(path)
		if readErr != nil || string(actual) != expected {
			t.Fatalf("committed projection %s = %q, %v", path, actual, readErr)
		}
	}
	if _, statErr := os.Stat(retiredPath); !os.IsNotExist(statErr) {
		t.Fatalf("retired skill remains after successful retry: %v", statErr)
	}
}
