package boatstack

import "testing"

func TestResolveCapabilitySelectsRepositoryCommandByAliasPriority(t *testing.T) {
	config := testConfig()
	delete(config.Project.Commands, "visual")
	delete(config.Project.Commands, "screenshot")
	delete(config.Project.Commands, "e2e")

	// Lowest-priority alias still resolves the repository-owned cut.
	config.Project.Commands["e2e"] = "npm run e2e"
	resolution, err := ResolveCapability("visual", config)
	if err != nil {
		t.Fatalf("resolve visual: %v", err)
	}
	if resolution.Kind != "repository-command" || resolution.Command != "npm run e2e" || resolution.Name != "visual" {
		t.Fatalf("alias fallback did not resolve: %#v", resolution)
	}

	// A higher-priority alias wins over a lower one.
	config.Project.Commands["visual"] = "npm run capture:visual"
	resolution, err = ResolveCapability("visual", config)
	if err != nil {
		t.Fatalf("resolve visual: %v", err)
	}
	if resolution.Command != "npm run capture:visual" {
		t.Fatalf("higher-priority alias did not win: %#v", resolution)
	}
}

func TestResolveCapabilityReportsUnavailableWithoutCommand(t *testing.T) {
	config := testConfig()
	delete(config.Project.Commands, "visual")
	delete(config.Project.Commands, "screenshot")
	delete(config.Project.Commands, "e2e")

	resolution, err := ResolveCapability("visual", config)
	if err != nil {
		t.Fatalf("resolve visual: %v", err)
	}
	if resolution.Kind != "unavailable" || resolution.Command != "" {
		t.Fatalf("expected unavailable resolution: %#v", resolution)
	}
}

func TestResolveCapabilityRejectsUnknownCapability(t *testing.T) {
	if _, err := ResolveCapability("does-not-exist", testConfig()); err == nil {
		t.Fatal("expected error for unknown capability")
	}
}

func TestLookupCapabilityExposesRegisteredMetadata(t *testing.T) {
	capability, ok := LookupCapability("visual")
	if !ok {
		t.Fatal("visual capability is not registered")
	}
	if len(capability.CommandAliases) == 0 || capability.RetryClass == "" || len(capability.AdmittedStages) == 0 {
		t.Fatalf("visual capability metadata is incomplete: %#v", capability)
	}
}

// Invariant: a surface-scoped command outranks the global alias, and its
// absence falls back to the global ladder exactly — a repository with one
// global harness keeps serving every surface (zero-value behavior).
func TestResolveCapabilityForSurfacePrefersSurfaceScopedCommand(t *testing.T) {
	config := testConfig()
	delete(config.Project.Commands, "visual")
	delete(config.Project.Commands, "screenshot")
	delete(config.Project.Commands, "e2e")
	config.Project.Commands["visual"] = "npm run capture:visual"
	config.Project.Commands["visual:web"] = "npm run capture:web"

	resolution, err := ResolveCapabilityForSurface("visual", "web", config)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Command != "npm run capture:web" {
		t.Fatalf("surface key did not outrank the global alias: %#v", resolution)
	}
	for _, surface := range []string{"", "ops"} {
		resolution, err = ResolveCapabilityForSurface("visual", surface, config)
		if err != nil {
			t.Fatal(err)
		}
		if resolution.Command != "npm run capture:visual" {
			t.Fatalf("surface %q did not fall back to the global alias: %#v", surface, resolution)
		}
	}
	delete(config.Project.Commands, "visual")
	resolution, err = ResolveCapabilityForSurface("visual", "ops", config)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Kind != "unavailable" {
		t.Fatalf("unregistered surface with no global command must be unavailable: %#v", resolution)
	}
}
