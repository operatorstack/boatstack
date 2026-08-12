package controlprogram_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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

func TestTypeScriptFrontendRejectsRepositoryCodeWithoutExecutingIt(t *testing.T) {
	// control-law: authoring-frontends-parse-repository-declarations-without-module-execution
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate frontend fixture")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
	frontend := filepath.Join(filepath.Dir(moduleRoot), "node_modules", ".bin", "boatstack-flow-frontend")
	if runtime.GOOS == "windows" {
		frontend += ".cmd"
	}
	if _, err := os.Stat(frontend); err != nil {
		t.Skip("Flow frontend dependencies are not installed")
	}
	directory := t.TempDir()
	sentinel := filepath.Join(directory, "executed")
	source := filepath.Join(directory, "unsafe.flow.ts")
	content := "import { writeFileSync } from 'node:fs';\n" +
		"writeFileSync(" + strconv.Quote(sentinel) + ", 'unsafe');\n" +
		"export default { schema_version: 'control-program/v1' };\n"
	if err := os.WriteFile(source, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(frontend, source).CombinedOutput()
	if err == nil || !strings.Contains(string(output), "trusted Boatstack SDKs") {
		t.Fatalf("unsafe frontend result = %v\n%s", err, output)
	}
	if _, statErr := os.Stat(sentinel); !os.IsNotExist(statErr) {
		t.Fatalf("repository module executed: %v", statErr)
	}
}

func TestTypeScriptFrontendRejectsUnboundLocalImports(t *testing.T) {
	// control-law: every-frontend-input-is-bound-or-refused
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate frontend fixture")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
	frontend := filepath.Join(filepath.Dir(moduleRoot), "node_modules", ".bin", "boatstack-flow-frontend")
	if runtime.GOOS == "windows" {
		frontend += ".cmd"
	}
	if _, err := os.Stat(frontend); err != nil {
		t.Skip("Flow frontend dependencies are not installed")
	}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "helper.ts"), []byte("export default {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(directory, "local.flow.ts")
	if err := os.WriteFile(source, []byte("import helper from './helper';\nexport default helper;\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(frontend, source).CombinedOutput()
	if err == nil || !strings.Contains(string(output), "trusted Boatstack SDKs") {
		t.Fatalf("local import result = %v\n%s", err, output)
	}
}
