package effects

import (
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

func TestInstallationUpdateKeepsCurrentJournalSchema(t *testing.T) {
	if protocol.JournalSchemaVersion != 8 {
		t.Fatalf("journal schema = %d, want current schema 8 for in-flight installation updates", protocol.JournalSchemaVersion)
	}
}
