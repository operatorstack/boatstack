package effects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPriorJournalSchemaRequiresExplicitReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "adm-prior.pending")
	raw := []byte(`{"schema_version":7,"admission":{"id":"adm-prior"},"transition_id":"plan.create","transition_class":"owned-local","status":"begun"}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := readJournal(path); err == nil || !strings.Contains(err.Error(), "invalid transaction journal") {
		t.Fatalf("read prior journal schema error = %v, want explicit invalid journal refusal", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(raw) {
		t.Fatalf("prior journal changed during refusal:\n got %s\nwant %s", got, raw)
	}
}
