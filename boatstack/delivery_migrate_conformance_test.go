package boatstack

import (
	"encoding/json"
	"strings"
	"testing"
)

// control-law: delivery-state-migrates-forward-or-fails-closed
//
// The managed delivery state carries a schema version. The migration hook upgrades
// an older document forward through a registered one-step chain and stamps it to the
// target (Positive/Relation); a document already at the target passes through
// byte-for-byte unchanged so today's behavior is preserved exactly (Negative); a
// document written by a NEWER Boatstack, or one with a gap in the chain, fails
// closed with an actionable error rather than being loaded or silently upgraded
// (Bypass/Failure-state). Blank input is a no-op.

// bumpSchema is a synthetic one-step migration (v1 -> v2) used only by these tests,
// so the forward-walk is exercised without a real schema bump.
func bumpSchema() []deliveryStateMigration {
	return []deliveryStateMigration{{
		from: 1, to: 2,
		apply: func(m map[string]any) (map[string]any, error) {
			m["migrated_marker"] = true
			return m, nil
		},
	}}
}

// Negative: a document already at the target version is returned byte-for-byte
// unchanged — the production path at schema version 1 must not rewrite state.
func TestMigrateDeliveryStateCurrentIsPassThrough(t *testing.T) {
	raw := []byte(`{"schema_version":1,"feature":"demo"}`)
	out, changed, err := migrateDeliveryStateBytesWith(raw, bumpSchema(), 1)
	if err != nil {
		t.Fatalf("current-version document must not error: %v", err)
	}
	if changed {
		t.Error("a current-version document must report no change")
	}
	if string(out) != string(raw) {
		t.Errorf("current-version document must pass through unchanged; got %s", out)
	}
}

// Positive + Relation: an older document is walked forward through the registered
// step, the migration's transform is applied, and schema_version is stamped to the
// target.
func TestMigrateDeliveryStateWalksForward(t *testing.T) {
	raw := []byte(`{"schema_version":1,"feature":"demo"}`)
	out, changed, err := migrateDeliveryStateBytesWith(raw, bumpSchema(), 2)
	if err != nil {
		t.Fatalf("forward migration must succeed: %v", err)
	}
	if !changed {
		t.Error("a forward migration must report a change")
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("migrated output must be valid JSON: %v", err)
	}
	if v, _ := got["schema_version"].(float64); int(v) != 2 {
		t.Errorf("schema_version must be stamped to the target 2, got %v", got["schema_version"])
	}
	if marker, _ := got["migrated_marker"].(bool); !marker {
		t.Error("the migration's transform must have been applied")
	}
}

// Bypass: a document written by a newer Boatstack is never loaded or downgraded —
// it fails closed with a message that tells the operator to update.
func TestMigrateDeliveryStateNewerFailsClosed(t *testing.T) {
	raw := []byte(`{"schema_version":99,"feature":"demo"}`)
	_, _, err := migrateDeliveryStateBytesWith(raw, bumpSchema(), 1)
	if err == nil {
		t.Fatal("a newer-schema document must fail closed, not load")
	}
	if !strings.Contains(err.Error(), "newer Boatstack") {
		t.Errorf("error must point at updating Boatstack, got %q", err)
	}
}

// Failure-state: a gap in the migration chain fails closed rather than guessing a
// path forward.
func TestMigrateDeliveryStateMissingStepFailsClosed(t *testing.T) {
	raw := []byte(`{"schema_version":1,"feature":"demo"}`)
	_, _, err := migrateDeliveryStateBytesWith(raw, nil, 3) // no migrations registered
	if err == nil {
		t.Fatal("a missing migration step must fail closed")
	}
	if !strings.Contains(err.Error(), "no delivery-state migration") {
		t.Errorf("error must name the missing step, got %q", err)
	}
}

// Blank input is a no-op pass-through — there is nothing to migrate.
func TestMigrateDeliveryStateBlankIsNoOp(t *testing.T) {
	out, changed, err := migrateDeliveryStateBytesWith([]byte("  "), bumpSchema(), 2)
	if err != nil || changed {
		t.Fatalf("blank input must be a silent no-op; changed=%v err=%v", changed, err)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("blank input must pass through, got %q", out)
	}
}
