package controlprogram_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	boatstack "github.com/operatorstack/boatstack/boatstack"
	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	"github.com/operatorstack/boatstack/boatstack/core"
	"github.com/operatorstack/boatstack/boatstack/delivery"
	softwareflow "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery"
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

func TestRepositoryOwnedSoftwareDeliveryFlowsShareOneRuntime(t *testing.T) {
	// control-law: repositories-own-entry-target-and-transition-policy
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate frontend fixtures")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
	frontend := filepath.Join(filepath.Dir(moduleRoot), "node_modules", ".bin", "boatstack-flow-frontend")
	if runtime.GOOS == "windows" {
		frontend += ".cmd"
	}
	if _, err := os.Stat(frontend); err != nil {
		t.Skip("Flow frontend dependencies are not installed")
	}
	resolver, err := softwareflow.NewResolver(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		fixture     string
		entries     int
		transitions int
	}{
		{"product-delivery-a.flow.ts", 1, 1},
		{"product-delivery-b.flow.ts", 2, 2},
		{"product-delivery-c.flow.ts", 1, 6},
		{"product-delivery-planning-package.flow.ts", 1, 21},
	}
	for _, test := range cases {
		t.Run(test.fixture, func(t *testing.T) {
			source := filepath.Join(moduleRoot, "testdata", "control-programs", test.fixture)
			frontendRaw, commandErr := exec.Command(frontend, source).CombinedOutput()
			if commandErr != nil {
				t.Fatalf("compile repository Flow: %v\n%s", commandErr, frontendRaw)
			}
			compiled, compileErr := controlprogram.LoadWithAssets(bytes.NewReader(frontendRaw), resolver, controlprogram.RepositoryAssetResolver{Repository: filepath.Dir(moduleRoot)})
			if compileErr != nil {
				t.Fatal(compileErr)
			}
			definition, definitionErr := softwareflow.NewDefinition(compiled, resolver)
			if definitionErr != nil {
				t.Fatal(definitionErr)
			}
			manifest, manifestErr := definition.RuntimeManifest(context.Background())
			if manifestErr != nil {
				t.Fatal(manifestErr)
			}
			if len(compiled.Document.Entries) != test.entries || len(manifest.Transitions) != test.transitions {
				t.Fatalf("entries=%d transitions=%d", len(compiled.Document.Entries), len(manifest.Transitions))
			}
			if test.fixture == "product-delivery-planning-package.flow.ts" {
				if _, compileErr := delivery.Compile(context.Background(), delivery.CompileRequest{KernelVersion: boatstack.Version, Core: core.System(), Runtime: definition, Settings: map[string]string{"fixture": test.fixture}}); compileErr != nil {
					t.Fatalf("compile repository Flow runtime: %v", compileErr)
				}
			}
		})
	}
}

func TestDomainNeutralFrontendDeclaresForegroundWorkWithoutSoftwareDelivery(t *testing.T) {
	// control-law: foreground work is a domain-neutral requirement rather than a delivery effect
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate foreground work fixture")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
	repositoryRoot := filepath.Dir(moduleRoot)
	frontend := filepath.Join(repositoryRoot, "node_modules", ".bin", "boatstack-flow-frontend")
	if runtime.GOOS == "windows" {
		frontend += ".cmd"
	}
	if _, err := os.Stat(frontend); err != nil {
		t.Skip("Flow frontend dependencies are not installed")
	}
	source := filepath.Join(moduleRoot, "testdata", "control-programs", "incident-response-work.flow.ts")
	raw, err := exec.Command(frontend, source).CombinedOutput()
	if err != nil {
		t.Fatalf("compile foreground work fixture: %v\n%s", err, raw)
	}
	compiled, err := controlprogram.LoadWithAssets(bytes.NewReader(raw), nil, controlprogram.RepositoryAssetResolver{Repository: repositoryRoot})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Document.Work) != 1 || compiled.Document.Transitions[0].Work != "diagnose" || compiled.Document.Work[0].Instructions.Content == "" || compiled.Document.Work[0].Outputs[0].Schema.Content == "" {
		t.Fatalf("compiled foreground work = %#v", compiled.Document.Work)
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
		"export default { schema: 'control-program', schema_revision: 1 };\n"
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
