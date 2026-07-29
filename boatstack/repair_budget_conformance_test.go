package boatstack

import (
	"encoding/json"
	"strings"
	"testing"
)

// control-law: repair-authority-is-class-specific-and-mechanism-sensitive
func TestTypedRepairBudgetsAndDuplicateMechanisms(t *testing.T) {
	repo, feature := activateTwoSliceDelivery(t)
	for _, class := range []string{"implementation_repair", "verification_repair"} {
		for attempt := 1; attempt <= 3; attempt++ {
			_, state, err := RecordChangeObservation(ChangeObservationOptions{
				Repo: repo, Feature: feature, Message: "repair", SourceStage: "test",
				Classification: class, Evidence: "failure", Mechanism: class + "-" + string(rune('0'+attempt)),
			})
			if err != nil {
				t.Fatalf("%s attempt %d rejected: %v", class, attempt, err)
			}
			if state.RepairCounters[class] != attempt {
				t.Fatalf("%s counter=%d, want %d", class, state.RepairCounters[class], attempt)
			}
		}
	}
	_, before, err := RecordChangeObservation(ChangeObservationOptions{
		Repo: repo, Feature: feature, Message: "duplicate", SourceStage: "test",
		Classification: "review_repair", Evidence: "same", Mechanism: "same mechanism",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = RecordChangeObservation(ChangeObservationOptions{
		Repo: repo, Feature: feature, Message: "duplicate", SourceStage: "test",
		Classification: "review_repair", Evidence: "same", Mechanism: "same mechanism",
	})
	if err == nil || !strings.Contains(err.Error(), "identical") {
		t.Fatalf("duplicate mechanism must be denied as friction: %v", err)
	}
	after, err := LoadDeliveryState(repo, feature)
	if err != nil {
		t.Fatal(err)
	}
	if after.RepairCounters["review_repair"] != before.RepairCounters["review_repair"] {
		t.Fatal("denied duplicate must not consume repair authority")
	}
	_, changed, err := RecordChangeObservation(ChangeObservationOptions{
		Repo: repo, Feature: feature, Message: "changed mechanism", SourceStage: "test",
		Classification: "review_repair", Evidence: "same", Mechanism: "different mechanism",
	})
	if err != nil || changed.RepairCounters["review_repair"] != 2 {
		t.Fatalf("changed mechanism must consume the next class-specific attempt: state=%+v err=%v", changed.RepairCounters, err)
	}
}

// control-law: legacy-exhaustion-cannot-gain-new-retry-authority
func TestLegacyRepairAttemptMigratesToEveryClass(t *testing.T) {
	raw := []byte(`{"schema_version":1,"repair_attempt":3}`)
	upgraded, changed, err := migrateDeliveryStateBytes(raw)
	if err != nil || !changed {
		t.Fatalf("legacy delivery state migration failed: changed=%v err=%v", changed, err)
	}
	var value map[string]any
	if err := json.Unmarshal(upgraded, &value); err != nil {
		t.Fatal(err)
	}
	counters := value["repair_counters"].(map[string]any)
	for _, class := range []string{"implementation_repair", "verification_repair", "review_repair"} {
		if intValue(counters[class]) != 3 {
			t.Fatalf("%s inherited %v, want 3", class, counters[class])
		}
	}
}
