package controlprogram_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	boatstack "github.com/operatorstack/boatstack/boatstack"
	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	"github.com/operatorstack/boatstack/boatstack/core"
	"github.com/operatorstack/boatstack/boatstack/delivery"
	softwareflow "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery"
	"github.com/operatorstack/boatstack/boatstack/internal/hostprojection"
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

func TestSoftwareDeliverySugarIsByteAndProjectionEquivalent(t *testing.T) {
	// control-law: software-delivery-sugar-derives-only-canonical-wiring
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate frontend fixtures")
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
	compile := func(name string) []byte {
		t.Helper()
		raw, err := exec.Command(frontend, filepath.Join(moduleRoot, "testdata", "control-programs", name)).CombinedOutput()
		if err != nil {
			t.Fatalf("compile %s: %v\n%s", name, err, raw)
		}
		return raw
	}
	manualRaw := compile("product-delivery-planning-package-manual.flow.ts")
	helperRaw := compile("product-delivery-planning-package.flow.ts")
	if !bytes.Equal(manualRaw, helperRaw) {
		t.Fatalf("manual and helper raw IR differ\nmanual: %s\nhelper: %s", manualRaw, helperRaw)
	}
	resolver, err := softwareflow.NewResolver(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assets := controlprogram.RepositoryAssetResolver{Repository: repositoryRoot}
	manual, err := controlprogram.LoadWithAssets(bytes.NewReader(manualRaw), resolver, assets)
	if err != nil {
		t.Fatal(err)
	}
	helper, err := controlprogram.LoadWithAssets(bytes.NewReader(helperRaw), resolver, assets)
	if err != nil {
		t.Fatal(err)
	}
	if manual.Fingerprint != helper.Fingerprint || !reflect.DeepEqual(manual.Document, helper.Document) {
		t.Fatalf("canonical programs differ: manual=%s helper=%s", manual.Fingerprint, helper.Fingerprint)
	}
	manualProjections, err := softwareflow.GenerateProjections(manual, hostprojection.CanonicalIDs())
	if err != nil {
		t.Fatal(err)
	}
	helperProjections, err := softwareflow.GenerateProjections(helper, hostprojection.CanonicalIDs())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(manualProjections, helperProjections) {
		t.Fatalf("manual and helper generated projections differ")
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
		{"product-delivery-planning-package.flow.ts", 2, 22},
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
				referenceRaw, readErr := os.ReadFile(filepath.Join(moduleRoot, "testdata", "control-programs", "product-delivery-planning-package.raw.json"))
				if readErr != nil {
					t.Fatal(readErr)
				}
				reference, referenceErr := controlprogram.LoadWithAssets(bytes.NewReader(referenceRaw), resolver, controlprogram.RepositoryAssetResolver{Repository: filepath.Dir(moduleRoot)})
				if referenceErr != nil {
					t.Fatal(referenceErr)
				}
				if compiled.Fingerprint != reference.Fingerprint {
					t.Fatalf("checked product-delivery IR is stale: frontend=%s checked=%s", compiled.Fingerprint, reference.Fingerprint)
				}
				var packageProducer, publicationProducer controlprogram.ParameterProducer
				publicationStateInputDeclared, publicationReceiptInputDeclared := false, false
				for _, operator := range compiled.Document.Operators {
					if operator.ID == "publication.observe" && len(operator.StateInputs) == 1 && operator.StateInputs[0].Parameter == "publication_id" && operator.StateInputs[0].Facet == "publication_id" {
						publicationStateInputDeclared = true
					}
					if operator.ID == "publication.observe" && len(operator.ReceiptInputs) == 1 && operator.ReceiptInputs[0].Parameter == "publication_id" && operator.ReceiptInputs[0].Transition == "publication.execute" && operator.ReceiptInputs[0].Field == "publication_id" && !operator.ReceiptInputs[0].Guaranteed {
						publicationReceiptInputDeclared = true
					}
				}
				for _, transition := range compiled.Document.Transitions {
					for _, parameter := range transition.Parameters {
						if parameter.Producer.Kind == controlprogram.ParameterSourceHostInput && (transition.ID != "delivery.slice.advance" || parameter.Parameter != "slice_id") {
							t.Fatalf("deterministic parameter requires human text input: %s/%s", transition.ID, parameter.Parameter)
						}
						if transition.ID == "planning.package.approve" && parameter.Parameter == "package_fingerprint" {
							packageProducer = parameter.Producer
						}
						if transition.ID == "publication.observe" && parameter.Parameter == "publication_id" {
							publicationProducer = parameter.Producer
						}
					}
				}
				if packageProducer.Kind != controlprogram.ParameterSourceTrustedResolver || packageProducer.Binding == nil || packageProducer.Binding.Reference != "software-delivery/admitted-planning-package-fingerprint" {
					t.Fatalf("planning package producer = %#v", packageProducer)
				}
				if publicationProducer.Kind != controlprogram.ParameterSourceStateOrReceipt || publicationProducer.Facet != "publication_id" || publicationProducer.Transition != "publication.execute" || publicationProducer.Field != "publication_id" {
					t.Fatalf("publication identity producer = %#v", publicationProducer)
				}
				if !publicationStateInputDeclared || !publicationReceiptInputDeclared {
					t.Fatal("publication identity producer lacks exact trusted alternative provenance")
				}
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

func TestDomainNeutralInvocationFixtureCompilesAndMissingProducerFails(t *testing.T) {
	// control-law: generic invocation completeness does not depend on the
	// software-delivery authoring package.
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate invocation fixtures")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
	frontend := filepath.Join(filepath.Dir(moduleRoot), "node_modules", ".bin", "boatstack-flow-frontend")
	if runtime.GOOS == "windows" {
		frontend += ".cmd"
	}
	if _, err := os.Stat(frontend); err != nil {
		t.Skip("Flow frontend dependencies are not installed")
	}
	compile := func(name string) ([]byte, error) {
		return exec.Command(frontend, filepath.Join(moduleRoot, "testdata", "control-programs", name)).CombinedOutput()
	}
	raw, err := compile("incident-response-invocation.flow.ts")
	if err != nil {
		t.Fatalf("compile domain-neutral fixture: %v\n%s", err, raw)
	}
	compiled, err := controlprogram.Load(bytes.NewReader(raw), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := softwareflow.GenerateProjections(compiled, []hostprojection.ID{hostprojection.Codex, hostprojection.Claude}); err != nil {
		t.Fatalf("generate domain-neutral entry drivers: %v", err)
	}
	missingRaw, err := compile("incident-response-invocation-missing.flow.ts")
	if err != nil {
		t.Fatalf("lower negative fixture: %v\n%s", err, missingRaw)
	}
	if _, err := controlprogram.Load(bytes.NewReader(missingRaw), nil); err == nil || !strings.Contains(err.Error(), "CONTROL_PROGRAM_INVOCATION_INCOMPLETE") {
		t.Fatalf("missing producer result = %v", err)
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

func TestTypeScriptFrontendKeepsDeclarativeExpressionBoundary(t *testing.T) {
	// control-law: software-delivery-sugar-does-not-widen-the-frontend-language
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate frontend")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
	frontend := filepath.Join(filepath.Dir(moduleRoot), "node_modules", ".bin", "boatstack-flow-frontend")
	if runtime.GOOS == "windows" {
		frontend += ".cmd"
	}
	if _, err := os.Stat(frontend); err != nil {
		t.Skip("Flow frontend dependencies are not installed")
	}
	cases := []struct {
		name    string
		source  string
		message string
	}{
		{"object spread", "const base = {};\nexport default defineFlow({ ...base });\n", "explicit property assignments"},
		{"array spread", "const values = [];\nexport default defineFlow({ facets: [...values] });\n", "Flow expression is not declarative"},
		{"property call", "export default defineFlow.call(undefined, {});\n", "named trusted SDK imports"},
		{"callback and map", "const values = [];\nexport default defineFlow({ facets: values.map((value) => value) });\n", "named trusted SDK imports"},
		{"local function", "function local() { return {}; }\nexport default defineFlow(local());\n", "trusted imports and one default export"},
		{"computed property", "export default defineFlow({ [\"id\"]: \"example\" });\n", "static identifiers or literals"},
		{"arbitrary statement", "if (true) {}\nexport default defineFlow({});\n", "trusted imports and one default export"},
		{"mutation", "const value = {};\nvalue.id = \"example\";\nexport default defineFlow(value);\n", "trusted imports and one default export"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			source := filepath.Join(directory, "invalid.flow.ts")
			content := "import { defineFlow } from \"@operatorstack/boatstack\";\n" + test.source
			if err := os.WriteFile(source, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			output, err := exec.Command(frontend, source).CombinedOutput()
			if err == nil || !strings.Contains(string(output), test.message) {
				t.Fatalf("frontend result = %v\n%s", err, output)
			}
		})
	}
}

func TestSoftwareDeliverySugarLeavesUnknownResolversForTrustedInputValidation(t *testing.T) {
	// control-law: trusted-input-boundary-remains-the-resolver-registry
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate frontend")
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
	source := filepath.Join(directory, "unknown-resolver.flow.ts")
	content := `import { defineFlow } from "@operatorstack/boatstack";
import { softwareDelivery } from "@operatorstack/boatstack-software-delivery";
export default defineFlow(softwareDelivery({
  id: "unknown-resolver",
  version: "1",
  lifecycle: [{ id: "plan.abandon", priority: 31 }],
  targets: [{ id: "done", predicate: { true: true } }],
  entries: [{ id: "run", target: "done", inputs: [{ id: "value", type: "text", required: true, resolver: "unknown.resolver" }] }],
}));
`
	if err := os.WriteFile(source, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.Command(frontend, source).CombinedOutput()
	if err != nil {
		t.Fatalf("helper must emit the unknown reference: %v\n%s", err, raw)
	}
	resolver, err := softwareflow.NewResolver(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := controlprogram.Load(bytes.NewReader(raw), resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Document.Declarations.InputResolvers) != 1 || compiled.Document.Declarations.InputResolvers[0] != "unknown.resolver" {
		t.Fatalf("helper altered unknown resolver declaration: %#v", compiled.Document.Declarations.InputResolvers)
	}
	if _, err := softwareflow.PlanInboxForEntry(compiled.Document.Entries[0]); err == nil || !strings.Contains(err.Error(), softwareflow.PlanInboxResolver) {
		t.Fatalf("unknown resolver trusted-boundary result = %v", err)
	}
}

func TestSoftwareDeliverySugarBindsAdditionalWorkThroughProductionCompiler(t *testing.T) {
	// control-law: helper-declared-work-is-explicitly-bound-before-compilation
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate frontend")
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
	source := filepath.Join(directory, "additional-work.flow.ts")
	content := `import { defineFlow, foregroundWork, instructionAsset, workArtifact } from "@operatorstack/boatstack";
import { softwareDelivery } from "@operatorstack/boatstack-software-delivery";
const implementation = foregroundWork({
  id: "implementation",
  instructions: instructionAsset("implementation.md"),
  inputs: [],
  outputs: [workArtifact({ id: "result", path: "result.md", media_type: "text/markdown", required: true })],
});
export default defineFlow(softwareDelivery({
  id: "additional-work",
  version: "1",
  lifecycle: [{ id: "plan.activate", priority: 50, work: "implementation" }],
  work: [implementation],
  targets: [{ id: "done", predicate: { true: true } }],
  entries: [{ id: "run", target: "done" }],
}));
`
	if err := os.WriteFile(source, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "implementation.md"), []byte("Implement the approved plan.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.Command(frontend, source).CombinedOutput()
	if err != nil {
		t.Fatalf("compile helper with additional work: %v\n%s", err, raw)
	}
	resolver, err := softwareflow.NewResolver(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := controlprogram.LoadWithAssets(
		bytes.NewReader(raw),
		resolver,
		controlprogram.RepositoryAssetResolver{Repository: directory},
	)
	if err != nil {
		t.Fatalf("load helper with additional work: %v", err)
	}
	if len(compiled.Document.Work) != 1 || compiled.Document.Work[0].ID != "implementation" {
		t.Fatalf("compiled work = %#v", compiled.Document.Work)
	}
	if len(compiled.Document.Transitions) != 1 || compiled.Document.Transitions[0].Work != "implementation" {
		t.Fatalf("compiled transitions = %#v", compiled.Document.Transitions)
	}
}
