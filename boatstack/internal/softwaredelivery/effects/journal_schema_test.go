package effects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

func TestJournalRejectsMutableRecoveryFacetEnvelope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "adm-tampered.pending")
	raw := []byte(`{"schema_version":7,"admission":{"id":"adm-tampered"},"transition_id":"plan.create","transition_class":"owned-local","allowed_state_facets":["control","installation","product"],"status":"begun"}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := readJournal(path); err == nil || !strings.Contains(err.Error(), "unknown field \"allowed_state_facets\"") {
		t.Fatalf("mutable recovery envelope error = %v, want strict unknown-field refusal", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(raw) {
		t.Fatalf("tampered journal changed during refusal:\n got %s\nwant %s", got, raw)
	}
}

func TestInstallationUpdateKeepsStableJournalSchema(t *testing.T) {
	if protocol.JournalSchemaVersion != 7 {
		t.Fatalf("journal schema = %d, want stable schema 7 for in-flight installation updates", protocol.JournalSchemaVersion)
	}
}
