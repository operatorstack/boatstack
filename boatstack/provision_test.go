package boatstack

import (
	"os"
	"path/filepath"
	"testing"
)

func writeProjectConfig(t *testing.T, repo string, mutate func(*ProjectConfig)) {
	t.Helper()
	runGit(t, repo, "init", "-b", "main")
	config := testConfig()
	config.Project.DefaultBranch = "main"
	if mutate != nil {
		mutate(&config)
	}
	value, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".product-loop"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".product-loop", "project.json"), value, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetectFrontendStackReadsManifestAndTooling(t *testing.T) {
	repo := t.TempDir()
	manifest := `{
		"scripts": {"dev": "vite", "test": "vitest"},
		"dependencies": {"react": "^18.0.0", "vite": "^5.0.0"},
		"devDependencies": {"@playwright/test": "^1.40.0"}
	}`
	if err := os.WriteFile(filepath.Join(repo, "package.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "pnpm-lock.yaml"), []byte("lockfileVersion: '9.0'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stack := detectFrontendStack(repo)
	if stack.Framework != "vite" || stack.PackageManager != "pnpm" || stack.DevCommand != "pnpm run dev" || !stack.HasPlaywright {
		t.Fatalf("unexpected stack facts: %#v", stack)
	}
}

func TestDetectFrontendStackReportsNoneWithoutManifest(t *testing.T) {
	stack := detectFrontendStack(t.TempDir())
	if stack.Framework != "none" || stack.PackageManager != "none" || stack.DevCommand != "" {
		t.Fatalf("empty repo should report no frontend stack: %#v", stack)
	}
}

func TestProvisionGuideReportsAvailableWhenCommandResolves(t *testing.T) {
	repo := t.TempDir()
	writeProjectConfig(t, repo, func(config *ProjectConfig) {
		config.Project.Commands["visual"] = "pnpm run capture:visual"
	})
	guide, err := CapabilityProvisionGuide(repo, "visual")
	if err != nil {
		t.Fatal(err)
	}
	if guide.Tier != "available" || !guide.Available || guide.ResolvedCommand != "pnpm run capture:visual" {
		t.Fatalf("resolved capability should be available: %#v", guide)
	}
}

func TestProvisionGuideTailorsStepsToDetectedStack(t *testing.T) {
	repo := t.TempDir()
	writeProjectConfig(t, repo, nil) // no visual command → must provision
	manifest := `{"scripts": {"dev": "next dev", "test": "jest"}, "dependencies": {"next": "^14.0.0"}}`
	if err := os.WriteFile(filepath.Join(repo, "package.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	guide, err := CapabilityProvisionGuide(repo, "visual")
	if err != nil {
		t.Fatal(err)
	}
	if guide.Tier != "provision" || guide.Available {
		t.Fatalf("missing command should require provisioning: %#v", guide)
	}
	if guide.Stack.Framework != "next" || guide.SuggestedCommand != "npm run capture:visual" {
		t.Fatalf("guide did not reflect the detected stack: %#v", guide)
	}
	if len(guide.Contract) == 0 || len(guide.Steps) == 0 {
		t.Fatalf("provision guide must ship a contract and steps: %#v", guide)
	}
}

func TestProvisionGuideReportsUnsupportedWithoutFrontend(t *testing.T) {
	repo := t.TempDir()
	writeProjectConfig(t, repo, nil) // no command, no package.json
	guide, err := CapabilityProvisionGuide(repo, "visual")
	if err != nil {
		t.Fatal(err)
	}
	if guide.Tier != "unsupported" || guide.SuggestedCommand != "" {
		t.Fatalf("a backend-only repo should be unsupported, never forced: %#v", guide)
	}
}

func TestRegisterCapabilityCommandSyncsSourceAndExport(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	config := testConfig()
	config.Project.DefaultBranch = "main"
	raw, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	// Seed a canonical source plus its generated export, as a real install would.
	if err := os.WriteFile(filepath.Join(repo, ".boatstack-project.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	bundle, err := BuildExportBundle(".boatstack-project.json", config, raw, "boatstack")
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteExport(repo, bundle.Files); err != nil {
		t.Fatal(err)
	}

	result, err := RegisterCapabilityCommand(repo, "visual", "pnpm run capture:visual")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "source-and-export" || result.Alias != "visual" {
		t.Fatalf("unexpected registration outcome: %#v", result)
	}
	// Both the source and the generated export must now resolve the command.
	source, _, err := LoadConfig(filepath.Join(repo, ".boatstack-project.json"))
	if err != nil {
		t.Fatal(err)
	}
	generated, _, err := LoadConfig(filepath.Join(repo, ".product-loop", "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	if source.Project.Commands["visual"] != "pnpm run capture:visual" || generated.Project.Commands["visual"] != "pnpm run capture:visual" {
		t.Fatalf("source and export drifted: source=%q export=%q", source.Project.Commands["visual"], generated.Project.Commands["visual"])
	}
	// And provisioning must now report the capability as available.
	guide, err := CapabilityProvisionGuide(repo, "visual")
	if err != nil {
		t.Fatal(err)
	}
	if guide.Tier != "available" {
		t.Fatalf("registration did not make the capability available: %#v", guide)
	}
}

func TestRegisterCapabilityCommandFallsBackToGeneratedConfig(t *testing.T) {
	repo := t.TempDir()
	writeProjectConfig(t, repo, nil) // generated project.json only, no source
	result, err := RegisterCapabilityCommand(repo, "visual", "npm run capture:visual")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "generated-only" {
		t.Fatalf("expected generated-only registration: %#v", result)
	}
	generated, _, err := LoadConfig(filepath.Join(repo, ".product-loop", "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	if generated.Project.Commands["visual"] != "npm run capture:visual" {
		t.Fatalf("command was not persisted: %#v", generated.Project.Commands)
	}
}
