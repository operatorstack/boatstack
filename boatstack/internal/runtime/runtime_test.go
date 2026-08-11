package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureIdentity(version, source string, raw []byte) Identity {
	sum := sha256.Sum256(raw)
	return Identity{Version: version, SHA256: hex.EncodeToString(sum[:]), SourceRevision: source}
}

func writeCandidate(t *testing.T, raw []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "candidate")
	if err := os.WriteFile(path, raw, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writePin(t *testing.T, repository string, identity Identity) {
	t.Helper()
	raw, err := EncodePin(NewPin(identity, strings.Repeat("a", 64), 2))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(PinPath(repository)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(PinPath(repository), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoriesResolveIndependentExactRuntimePins(t *testing.T) {
	// control-law: repository-runtime-selection-is-exact-and-isolated
	home := t.TempDir()
	t.Setenv(HomeEnvironment, home)
	oldRaw, newRaw := []byte("old-runtime"), []byte("new-runtime")
	oldIdentity := fixtureIdentity("v1.0.0", "old-source", oldRaw)
	newIdentity := fixtureIdentity("v2.0.0", "new-source", newRaw)
	oldPath, err := InstallExecutable(writeCandidate(t, oldRaw), home, oldIdentity)
	if err != nil {
		t.Fatal(err)
	}
	newPath, err := InstallExecutable(writeCandidate(t, newRaw), home, newIdentity)
	if err != nil {
		t.Fatal(err)
	}
	repositoryA, repositoryB := t.TempDir(), t.TempDir()
	writePin(t, repositoryA, oldIdentity)
	writePin(t, repositoryB, newIdentity)
	resolvedA, _, err := ResolvePinnedExecutable(repositoryA)
	if err != nil {
		t.Fatal(err)
	}
	resolvedB, _, err := ResolvePinnedExecutable(repositoryB)
	if err != nil {
		t.Fatal(err)
	}
	if resolvedA != oldPath || resolvedB != newPath {
		t.Fatalf("resolved A=%s B=%s; want A=%s B=%s", resolvedA, resolvedB, oldPath, newPath)
	}
	thirdRaw := []byte("available-but-not-admitted")
	thirdIdentity := fixtureIdentity("v3.0.0", "third-source", thirdRaw)
	if _, err := InstallExecutable(writeCandidate(t, thirdRaw), home, thirdIdentity); err != nil {
		t.Fatal(err)
	}
	resolvedAAfter, _, _ := ResolvePinnedExecutable(repositoryA)
	resolvedBAfter, _, _ := ResolvePinnedExecutable(repositoryB)
	if resolvedAAfter != oldPath || resolvedBAfter != newPath {
		t.Fatal("installing an available candidate changed a repository selection")
	}
}

func TestMissingPinnedRuntimeFailsClosedWithoutLatestFallback(t *testing.T) {
	// control-law: missing-exact-runtime-never-falls-through-to-another-version
	home := t.TempDir()
	t.Setenv(HomeEnvironment, home)
	availableRaw := []byte("available")
	available := fixtureIdentity("v9.0.0", "available-source", availableRaw)
	if _, err := InstallExecutable(writeCandidate(t, availableRaw), home, available); err != nil {
		t.Fatal(err)
	}
	missing := fixtureIdentity("v1.0.0", "missing-source", []byte("missing"))
	repository := t.TempDir()
	writePin(t, repository, missing)
	if _, _, err := ResolvePinnedExecutable(repository); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("missing pin resolution error = %v", err)
	}
}

func TestNestedUninitializedRepositoryCannotInheritParentPin(t *testing.T) {
	// control-law: repository-runtime-selection-never-crosses-a-git-boundary
	home := t.TempDir()
	t.Setenv(HomeEnvironment, home)
	raw := []byte("parent-runtime")
	identity := fixtureIdentity("v1.0.0", "parent-source", raw)
	if _, err := InstallExecutable(writeCandidate(t, raw), home, identity); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	writePin(t, parent, identity)
	nested := filepath.Join(parent, "nested")
	if err := os.MkdirAll(filepath.Join(nested, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ResolvePinnedExecutable(nested); err == nil || !strings.Contains(err.Error(), "has no Boatstack runtime pin") {
		t.Fatalf("nested repository resolution error = %v", err)
	}
}

func TestRuntimeStoreIsImmutableAndScalesIndependentlyOfSelection(t *testing.T) {
	// control-law: immutable-runtime-count-does-not-affect-exact-lookup
	home := t.TempDir()
	t.Setenv(HomeEnvironment, home)
	var selected Identity
	var selectedPath string
	var selectedRaw []byte
	for index := 0; index < 50; index++ {
		raw := []byte(fmt.Sprintf("runtime-%02d", index))
		identity := fixtureIdentity(fmt.Sprintf("v1.0.%02d", index), "source", raw)
		path, err := InstallExecutable(writeCandidate(t, raw), home, identity)
		if err != nil {
			t.Fatal(err)
		}
		if index == 23 {
			selected, selectedPath, selectedRaw = identity, path, append([]byte(nil), raw...)
		}
	}
	repository := t.TempDir()
	writePin(t, repository, selected)
	resolved, _, err := ResolvePinnedExecutable(repository)
	if err != nil || resolved != selectedPath {
		t.Fatalf("50-version resolution = %s, %v", resolved, err)
	}
	if err := os.WriteFile(selectedPath, []byte("mutated"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallExecutable(writeCandidate(t, selectedRaw), home, selected); err == nil {
		t.Fatal("immutable runtime collision was overwritten")
	}
}

func TestRuntimePinSurvivesRepositoryMoveWithoutHostPath(t *testing.T) {
	// control-law: repository-pin-is-portable-identity-not-host-location
	home := t.TempDir()
	t.Setenv(HomeEnvironment, home)
	raw := []byte("portable")
	identity := fixtureIdentity("v4.0.0", "portable-source", raw)
	expected, err := InstallExecutable(writeCandidate(t, raw), home, identity)
	if err != nil {
		t.Fatal(err)
	}
	original, moved := t.TempDir(), t.TempDir()
	writePin(t, original, identity)
	pinRaw, _ := os.ReadFile(PinPath(original))
	if strings.Contains(string(pinRaw), home) {
		t.Fatal("repository pin contains a host-local path")
	}
	if err := os.MkdirAll(filepath.Dir(PinPath(moved)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(PinPath(moved), pinRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, _, err := ResolvePinnedExecutable(moved)
	if err != nil || resolved != expected {
		t.Fatalf("moved repository resolution = %s, %v", resolved, err)
	}
}
