package controlprogram_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
)

func TestTypeScriptDSLAndRawIRHaveOneCanonicalFingerprint(t *testing.T) {
	// control-law: every-language-frontend-lowers-to-one-canonical-program
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate frontend fixture")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
	repositoryRoot := filepath.Dir(moduleRoot)
	frontend := filepath.Join(repositoryRoot, "node_modules", ".bin", "boatstack-flow-frontend")
	if runtime.GOOS == "windows" {
		frontend += ".cmd"
	}
	if _, err := os.Stat(frontend); err != nil {
		if os.Getenv("BOATSTACK_REQUIRE_FLOW_FRONTEND") == "1" {
			t.Fatalf("required Flow frontend is unavailable: %v", err)
		}
		t.Skip("Flow frontend dependencies are not installed")
	}
	source := filepath.Join(moduleRoot, "testdata", "control-programs", "incident-response.flow.ts")
	command := exec.Command(frontend, source)
	frontendRaw, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("compile TypeScript fixture: %v\n%s", err, frontendRaw)
	}
	rawPath := filepath.Join(moduleRoot, "testdata", "control-programs", "incident-response.raw.json")
	raw, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	fromTypeScript, err := controlprogram.Load(bytes.NewReader(frontendRaw), nil)
	if err != nil {
		t.Fatal(err)
	}
	fromRaw, err := controlprogram.Load(bytes.NewReader(raw), nil)
	if err != nil {
		t.Fatal(err)
	}
	if fromTypeScript.Fingerprint != fromRaw.Fingerprint {
		t.Fatalf("frontend fingerprints differ: %s != %s", fromTypeScript.Fingerprint, fromRaw.Fingerprint)
	}
}
