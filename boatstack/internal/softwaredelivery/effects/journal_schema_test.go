package effects

import (
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

func TestInstallationUpdateKeepsCurrentJournalSchema(t *testing.T) {
	if protocol.JournalSchemaVersion != 9 {
		t.Fatalf("journal schema = %d, want current schema 9 for target-bound transaction records", protocol.JournalSchemaVersion)
	}
}
