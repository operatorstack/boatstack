package effects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

func TestConfigurationMutationPreservesAdmittedProgramRole(t *testing.T) {
	candidate := func(identity string) protocol.ProjectConfig {
		t.Helper()
		raw := []byte(`{"schema_version":5,"identity":` + identity + `,"project":{"name":"fixture","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli"],"projections":[]}`)
		config, _, err := protocol.ProjectConfigFingerprint(raw)
		if err != nil {
			t.Fatal(err)
		}
		return config
	}
	state := durable.State{ProgramHumanIdentityRole: "release-manager"}
	for _, identity := range []string{
		`{"default":"developer","roles":{"developer":{"kind":"literal","value":"operator"},"release-manager":{"kind":"literal","value":"release-operator"}}}`,
		`{"default":"developer","roles":{"developer":{"kind":"literal","value":"operator"},"release-manager":{"kind":"literal","value":"rotated-release-operator"}}}`,
	} {
		if err := verifyCandidateConfigurationPreservesAdmittedRole(state, candidate(identity)); err != nil {
			t.Fatalf("descriptor-preserving candidate rejected: %v", err)
		}
	}
	removed := candidate(`{"default":"developer","roles":{"developer":{"kind":"literal","value":"operator"}}}`)
	err := verifyCandidateConfigurationPreservesAdmittedRole(state, removed)
	if err == nil || !strings.Contains(err.Error(), "PROJECT_CONFIG_ADMITTED_HUMAN_IDENTITY_UNBOUND") {
		t.Fatalf("removed role result = %v", err)
	}
}

func TestConfigurationMutationChecksRoleOnHashBoundInstalledBytes(t *testing.T) {
	repository := t.TempDir()
	source := filepath.Join(t.TempDir(), "candidate.json")
	withRole := []byte(`{"schema_version":5,"identity":{"default":"developer","roles":{"developer":{"kind":"literal","value":"operator"},"release-manager":{"kind":"literal","value":"release-operator"}}},"project":{"name":"fixture","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli"],"projections":[]}`)
	withoutRole := []byte(`{"schema_version":5,"identity":{"default":"developer","roles":{"developer":{"kind":"literal","value":"operator"}}},"project":{"name":"fixture","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli"],"projections":[]}`)
	if err := os.WriteFile(source, withRole, 0o600); err != nil {
		t.Fatal(err)
	}
	_, expected, err := protocol.ProjectConfigFingerprint(withoutRole)
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce the old split-read race: the role-preserving bytes are visible
	// first, then the admitted hash-bound bytes replace them before artifact
	// preparation. The single-snapshot implementation must reject the latter.
	if err := os.WriteFile(source, withoutRole, 0o600); err != nil {
		t.Fatal(err)
	}
	state := durable.State{ProgramHumanIdentityRole: "release-manager"}
	mutations, err := prepareArtifacts(ports.ControllerLayout{RepositoryRoot: repository, ConfigPath: filepath.Join(repository, ".boatstack", "project.json")}, protocol.Admission{
		Parameters: protocol.Parameters{{Name: "config_path", Value: source}, {Name: "config_sha256", Value: expected}},
	}, catalog.Transition{ID: "configuration.mutate"}, &state)
	if err == nil || !strings.Contains(err.Error(), "PROJECT_CONFIG_ADMITTED_HUMAN_IDENTITY_UNBOUND") || len(mutations) != 0 {
		t.Fatalf("hash-bound role-removing candidate = mutations=%#v err=%v", mutations, err)
	}
}
