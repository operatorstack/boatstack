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
