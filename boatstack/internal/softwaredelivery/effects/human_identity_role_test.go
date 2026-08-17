package effects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

func TestConfigurationMutationPreservesAdmittedProgramRole(t *testing.T) {
	write := func(name, identity string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), name+".json")
		raw := []byte(`{"schema_version":5,"identity":` + identity + `,"project":{"name":"fixture","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli"],"projections":[]}`)
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	state := durable.State{ProgramHumanIdentityRole: "release-manager"}
	transition := catalog.Transition{ID: "configuration.mutate"}
	for _, identity := range []string{
		`{"default":"developer","roles":{"developer":{"kind":"literal","value":"operator"},"release-manager":{"kind":"literal","value":"release-operator"}}}`,
		`{"default":"developer","roles":{"developer":{"kind":"literal","value":"operator"},"release-manager":{"kind":"literal","value":"rotated-release-operator"}}}`,
	} {
		path := write("allowed", identity)
		admission := protocol.Admission{Parameters: protocol.Parameters{{Name: "config_path", Value: path}}}
		if err := verifyCandidateConfigurationPreservesAdmittedRole(state, admission, transition); err != nil {
			t.Fatalf("descriptor-preserving candidate rejected: %v", err)
		}
	}
	removed := write("removed", `{"default":"developer","roles":{"developer":{"kind":"literal","value":"operator"}}}`)
	err := verifyCandidateConfigurationPreservesAdmittedRole(state, protocol.Admission{Parameters: protocol.Parameters{{Name: "config_path", Value: removed}}}, transition)
	if err == nil || !strings.Contains(err.Error(), "PROJECT_CONFIG_ADMITTED_HUMAN_IDENTITY_UNBOUND") {
		t.Fatalf("removed role result = %v", err)
	}
}
