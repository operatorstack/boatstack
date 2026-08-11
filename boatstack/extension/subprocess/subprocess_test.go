package subprocess

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/control"
	"github.com/operatorstack/boatstack/boatstack/core"
	"github.com/operatorstack/boatstack/boatstack/flow/standard"
)

func fixtureManifest(t *testing.T, id string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(control.ExtensionManifest{
		ID: id, Version: "1.0.0", ProtocolVersion: control.ExtensionProtocolVersion,
		SettingsSchema: json.RawMessage(`{"type":"object"}`), Facts: []string{id + ".present"},
		PrivacyClassification: "metadata-only", TelemetryClassification: "transition-receipt",
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func fixtureExtension(t *testing.T) *Extension {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the language-neutral Python fixture is exercised on POSIX; the Go protocol implementation remains platform-tested")
	}
	if _, err := os.Stat("/usr/bin/python3"); err != nil {
		t.Skip("/usr/bin/python3 is unavailable")
	}
	path, err := filepath.Abs(filepath.Join("testdata", "reference_extension.py"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	extension, err := New(Config{
		ID: "fixture.echo", Version: "1.0.0", Executable: path, SHA256: hex.EncodeToString(digest[:]),
		Manifest: fixtureManifest(t, "fixture.echo"),
		Limits:   control.SubprocessLimits{Deadline: 30 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	return extension
}

func pythonFixture(t *testing.T, mutate func(string) string) *Extension {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the language-neutral Python fixture is exercised on POSIX")
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "reference_extension.py"))
	if err != nil {
		t.Fatal(err)
	}
	content := []byte(mutate(string(raw)))
	path := filepath.Join(exactPath(t, t.TempDir()), "extension.py")
	if err := os.WriteFile(path, content, 0o700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	extension, err := New(Config{
		ID: "fixture.echo", Version: "1.0.0", Executable: path, SHA256: hex.EncodeToString(digest[:]),
		Manifest: fixtureManifest(t, "fixture.echo"),
		Limits:   control.SubprocessLimits{Deadline: 30 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	return extension
}

func exactPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestPythonFixtureImplementsStrictLanguageNeutralProtocol(t *testing.T) {
	// control-law: subprocess-extension-is-exact-bounded-and-environment-clean
	t.Setenv("BOATSTACK_TEST_SECRET", "must-not-cross")
	extension := fixtureExtension(t)
	manifest, err := extension.ExtensionManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ID != "fixture.echo" || manifest.ExecutableSHA256 == "" || len(manifest.Facts) != 1 {
		t.Fatalf("manifest = %#v", manifest)
	}
	response, err := extension.Invoke(context.Background(), control.ExtensionRequest{
		ProtocolVersion: 1, Operation: control.ExtensionObserveOperation, ExtensionID: "fixture.echo", ExtensionVersion: "1.0.0", CorrelationID: "observe-fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Facts) != 1 || response.Facts[0].Value != "clean" {
		t.Fatalf("subprocess inherited arbitrary environment or lost fact: %#v", response)
	}
}

func TestExecutableDriftFailsBeforeInvocation(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("testdata", "reference_extension.py"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(exactPath(t, t.TempDir()), "extension.py")
	if err := os.WriteFile(path, source, 0o700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(source)
	extension, err := New(Config{ID: "fixture.echo", Version: "1.0.0", Executable: path, SHA256: hex.EncodeToString(digest[:]), Manifest: fixtureManifest(t, "fixture.echo")})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(source, []byte("\n# drift\n")...), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err = extension.Invoke(context.Background(), control.ExtensionRequest{ProtocolVersion: 1, Operation: control.ExtensionObserveOperation, ExtensionID: "fixture.echo", ExtensionVersion: "1.0.0", CorrelationID: "drift"})
	if err == nil || !strings.Contains(err.Error(), "fingerprint drifted") {
		t.Fatalf("drift error = %v", err)
	}
}

func TestInvocationExecutesTheExactVerifiedBytesAfterPathReplacement(t *testing.T) {
	// control-law: executable-identity-is-bound-to-the-bytes-actually-executed
	if runtime.GOOS == "windows" {
		t.Skip("POSIX atomic replacement semantics are exercised here")
	}
	extension := pythonFixture(t, func(source string) string { return source })
	replacement := []byte("#!/bin/sh\nexit 42\n")
	extension.beforeStart = func() {
		temporary := extension.config.Executable + ".replacement"
		if err := os.WriteFile(temporary, replacement, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(temporary, extension.config.Executable); err != nil {
			t.Fatal(err)
		}
	}
	response, err := extension.Invoke(context.Background(), control.ExtensionRequest{
		ProtocolVersion: 1, Operation: control.ExtensionObserveOperation, ExtensionID: "fixture.echo", ExtensionVersion: "1.0.0", CorrelationID: "atomic-replacement",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Facts) != 1 || response.Facts[0].Value != "clean" {
		t.Fatalf("response = %#v", response)
	}
}

func TestDeclarativeManifestDoesNotStartExecutable(t *testing.T) {
	// control-law: program-compilation-never-executes-repository-selected-code
	if runtime.GOOS == "windows" {
		t.Skip("POSIX script fixture")
	}
	directory := exactPath(t, t.TempDir())
	marker := filepath.Join(directory, "started")
	content := []byte("#!/bin/sh\ntouch \"" + marker + "\"\n")
	path := filepath.Join(directory, "extension.sh")
	if err := os.WriteFile(path, content, 0o700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	extension, err := New(Config{ID: "fixture.echo", Version: "1.0.0", Executable: path, SHA256: hex.EncodeToString(digest[:]), Manifest: fixtureManifest(t, "fixture.echo")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.Compile(context.Background(), control.CompileRequest{
		KernelVersion: "test-kernel", Core: core.System(), Runtime: standard.Definition(), Extensions: []control.Extension{extension},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("declarative manifest started executable: %v", err)
	}
}

func TestExecutableCannotBecomeASymlinkAfterConstruction(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink replacement semantics are exercised on POSIX")
	}
	source, err := os.ReadFile(filepath.Join("testdata", "reference_extension.py"))
	if err != nil {
		t.Fatal(err)
	}
	directory := exactPath(t, t.TempDir())
	path := filepath.Join(directory, "extension.py")
	replacement := filepath.Join(directory, "replacement.py")
	if err := os.WriteFile(path, source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replacement, source, 0o700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(source)
	extension, err := New(Config{ID: "fixture.echo", Version: "1.0.0", Executable: path, SHA256: hex.EncodeToString(digest[:]), Manifest: fixtureManifest(t, "fixture.echo")})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(replacement, path); err != nil {
		t.Fatal(err)
	}
	_, err = extension.Invoke(context.Background(), control.ExtensionRequest{ProtocolVersion: 1, Operation: control.ExtensionObserveOperation, ExtensionID: "fixture.echo", ExtensionVersion: "1.0.0", CorrelationID: "symlink-drift"})
	if err == nil || !strings.Contains(err.Error(), "drifted to a symlink") {
		t.Fatalf("symlink drift error = %v", err)
	}
}

func TestDeadlineAndOutputBoundsFailClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX script fixtures are not executable on Windows")
	}
	for _, fixture := range []struct {
		name, body, want string
		deadline         time.Duration
		stdout           int64
	}{
		{"deadline", "#!/bin/sh\nsleep 1\n", "deadline exceeded", 10 * time.Millisecond, 1024},
		{"stdout", "#!/bin/sh\nprintf '%02048d' 1\n", "output exceeded", 30 * time.Second, 64},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(exactPath(t, t.TempDir()), "fixture.sh")
			if err := os.WriteFile(path, []byte(fixture.body), 0o700); err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256([]byte(fixture.body))
			extension, err := New(Config{ID: "fixture.echo", Version: "1.0.0", Executable: path, SHA256: hex.EncodeToString(digest[:]), Manifest: fixtureManifest(t, "fixture.echo"), Limits: control.SubprocessLimits{Deadline: fixture.deadline, StdoutBytes: fixture.stdout, StderrBytes: 64}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = extension.Invoke(context.Background(), control.ExtensionRequest{ProtocolVersion: 1, Operation: control.ExtensionObserveOperation, ExtensionID: "fixture.echo", ExtensionVersion: "1.0.0", CorrelationID: fixture.name})
			if err == nil || !strings.Contains(err.Error(), fixture.want) {
				t.Fatalf("error = %v, want %q", err, fixture.want)
			}
		})
	}
}

func TestBoundedBufferCancelsAtTheFirstOverflow(t *testing.T) {
	// control-law: output-bound-observation-terminates-the-subprocess
	cancelled := 0
	buffer := &boundedBuffer{limit: 4, cancel: func() { cancelled++ }}
	value := []byte("overflow")
	written, err := buffer.Write(value)
	if err != nil {
		t.Fatal(err)
	}
	if written != len(value) || !buffer.exceeded || cancelled != 1 || string(buffer.Bytes()) != "over" {
		t.Fatalf("write = %d, exceeded = %v, cancelled = %d, bytes = %q", written, buffer.exceeded, cancelled, buffer.Bytes())
	}
	if _, err := buffer.Write(value); err != nil {
		t.Fatal(err)
	}
	if cancelled != 1 {
		t.Fatalf("cancel called %d times, want once", cancelled)
	}
}

func TestStrictJSONRejectsUnknownTrailingAndWrongOperationPayloads(t *testing.T) {
	// control-law: subprocess-response-is-strict-correlated-and-operation-typed
	cases := map[string]func(string) string{
		"unknown-field": func(source string) string {
			return strings.Replace(source, "json.dump(response, sys.stdout, separators=(\",\", \":\"))", "response[\"unknown_field\"] = True\njson.dump(response, sys.stdout, separators=(\",\", \":\"))", 1)
		},
		"trailing-json": func(source string) string {
			return strings.Replace(source, "json.dump(response, sys.stdout, separators=(\",\", \":\"))", "json.dump(response, sys.stdout, separators=(\",\", \":\"))\nprint(\"{}\", end=\"\")", 1)
		},
		"wrong-payload": func(source string) string {
			return strings.Replace(source, "elif operation == \"observe\":", "elif operation == \"observe\":\n    response[\"verified\"] = True", 1)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			extension := pythonFixture(t, mutate)
			_, err := extension.Invoke(context.Background(), control.ExtensionRequest{
				ProtocolVersion: 1, Operation: control.ExtensionObserveOperation, ExtensionID: "fixture.echo", ExtensionVersion: "1.0.0", CorrelationID: name,
			})
			if err == nil {
				t.Fatalf("%s response was accepted", name)
			}
		})
	}
}
