package runtime

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func requireBootstrapDiagnostic(t *testing.T, err error, code string) *BootstrapDiagnostic {
	t.Helper()
	var diagnostic *BootstrapDiagnostic
	if !errors.As(err, &diagnostic) {
		t.Fatalf("error %v is not a bootstrap diagnostic", err)
	}
	if diagnostic.Code != code {
		t.Fatalf("diagnostic code = %q, want %q", diagnostic.Code, code)
	}
	if diagnostic.FlowRunCreated || diagnostic.ManagedStateChanged {
		t.Fatalf("pre-runtime diagnostic claims mutation: %#v", diagnostic)
	}
	return diagnostic
}

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
	_, _, err := ResolvePinnedExecutable(repository)
	diagnostic := requireBootstrapDiagnostic(t, err, CodeRuntimeNotInstalled)
	if diagnostic.RequiredRuntime == nil || diagnostic.RequiredRuntime.Version != missing.Version || diagnostic.RequiredRuntime.SHA256 != missing.SHA256 || diagnostic.RequiredRuntime.SourceRevision != missing.SourceRevision {
		t.Fatalf("required runtime = %#v", diagnostic.RequiredRuntime)
	}
	if diagnostic.Recovery == nil || diagnostic.Recovery.Action != "install-exact-runtime" || !diagnostic.Recovery.RequiresConfirmation || !strings.Contains(diagnostic.Recovery.Command, "BOATSTACK_MODE") || !strings.Contains(diagnostic.Recovery.Command, "hydrate") || !strings.Contains(diagnostic.Recovery.Command, missing.Version) || strings.Contains(diagnostic.Recovery.Command, "latest") || strings.Contains(diagnostic.Recovery.Command, "BOATSTACK_MODE=update") {
		t.Fatalf("recovery = %#v", diagnostic.Recovery)
	}
	if _, statErr := os.Stat(filepath.Join(repository, ".git", "boatstack")); !os.IsNotExist(statErr) {
		t.Fatalf("missing runtime created managed state: %v", statErr)
	}
}

func TestBootstrapDiagnosticsClassifyPinAndRuntimeFailures(t *testing.T) {
	// control-law: every-pre-runtime-failure-is-typed-and-zero-mutation
	home := t.TempDir()
	t.Setenv(HomeEnvironment, home)

	t.Run("missing-pin", func(t *testing.T) {
		repository := t.TempDir()
		if err := os.Mkdir(filepath.Join(repository, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		_, _, err := ResolvePinnedExecutable(repository)
		requireBootstrapDiagnostic(t, err, CodeRuntimePinMissing)
	})

	t.Run("invalid-pin", func(t *testing.T) {
		repository := t.TempDir()
		if err := os.MkdirAll(filepath.Join(repository, ".boatstack"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(PinPath(repository), []byte(`{"schema_version":1,"version":"latest"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, err := ResolvePinnedExecutable(repository)
		requireBootstrapDiagnostic(t, err, CodeRuntimePinInvalid)
	})

	for _, test := range []struct {
		name string
		code string
		make func(string) error
	}{
		{name: "invalid-type", code: CodeRuntimeInvalid, make: func(path string) error { return os.MkdirAll(path, 0o755) }},
		{name: "checksum-mismatch", code: CodeRuntimeChecksumMismatch, make: func(path string) error {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			return os.WriteFile(path, []byte("wrong runtime"), 0o755)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			identity := fixtureIdentity("v1.2.3-"+test.name, "source", []byte("expected runtime"))
			repository := t.TempDir()
			writePin(t, repository, identity)
			path, err := ExecutablePath(home, identity)
			if err != nil {
				t.Fatal(err)
			}
			if err := test.make(path); err != nil {
				t.Fatal(err)
			}
			_, _, err = ResolvePinnedExecutable(repository)
			diagnostic := requireBootstrapDiagnostic(t, err, test.code)
			if diagnostic.Recovery != nil {
				t.Fatalf("unsafe repair command for %s: %#v", test.name, diagnostic.Recovery)
			}
		})
	}
}

func TestBootstrapDiagnosticRenderingPreservesOneEnvelope(t *testing.T) {
	// control-law: text-and-json-hosts-receive-the-same-pre-runtime-decision
	identity := fixtureIdentity("v1.2.3-rc.1", "source", []byte("runtime"))
	diagnostic := runtimeBootstrapDiagnostic(CodeRuntimeNotInstalled, "The repository-pinned Boatstack runtime is not installed.", "/repo", identity, nil)

	var jsonOutput bytes.Buffer
	rendered, err := RenderBootstrapDiagnostic(&jsonOutput, diagnostic, []string{"next", "--format", "json"})
	if err != nil || !rendered {
		t.Fatalf("JSON render = rendered %t, err %v", rendered, err)
	}
	var decoded BootstrapDiagnostic
	if err := json.Unmarshal(jsonOutput.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Schema != BootstrapDiagnosticSchema || decoded.SchemaRevision != BootstrapDiagnosticSchemaRevision || decoded.Code != CodeRuntimeNotInstalled || decoded.Recovery == nil || !decoded.Recovery.RequiresConfirmation {
		t.Fatalf("JSON diagnostic = %#v", decoded)
	}

	var textOutput bytes.Buffer
	rendered, err = RenderBootstrapDiagnostic(&textOutput, diagnostic, []string{"next", "--format=text"})
	if err != nil || !rendered {
		t.Fatalf("text render = rendered %t, err %v", rendered, err)
	}
	for _, expected := range []string{"Blocked:", CodeRuntimeNotInstalled, identity.Version, identity.SHA256, "explicit approval", "No Flow run was created"} {
		if !strings.Contains(textOutput.String(), expected) {
			t.Fatalf("text diagnostic lacks %q: %s", expected, textOutput.String())
		}
	}

	var unrelated bytes.Buffer
	if rendered, err := RenderBootstrapDiagnostic(&unrelated, errors.New("ordinary"), nil); err != nil || rendered || unrelated.Len() != 0 {
		t.Fatalf("ordinary error render = rendered %t, err %v, output %q", rendered, err, unrelated.String())
	}
}

func TestUnreleasedRuntimeIdentityHasNoDownloadCommand(t *testing.T) {
	// control-law: repository-pin-cannot-inject-an-installer-command
	identity := fixtureIdentity("local-development", "source", []byte("runtime"))
	diagnostic := runtimeBootstrapDiagnostic(CodeRuntimeNotInstalled, "missing", "/repo", identity, nil)
	if diagnostic.Recovery != nil {
		t.Fatalf("non-release identity produced recovery command: %#v", diagnostic.Recovery)
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
	_, _, err := ResolvePinnedExecutable(nested)
	requireBootstrapDiagnostic(t, err, CodeRuntimePinMissing)
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
