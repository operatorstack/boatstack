package boatstack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// activationReceiptID returns the id of the APPLIED plan-activation receipt in a
// repository, failing the test if none exists.
func activationReceiptID(t *testing.T, repo string) string {
	t.Helper()
	receipts, err := ListMutationReceipts(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, receipt := range receipts {
		if receipt.Kind == "plan-activation" && receipt.Status == "APPLIED" {
			return receipt.MutationID
		}
	}
	t.Fatalf("no APPLIED plan-activation receipt among %d receipts", len(receipts))
	return ""
}

// TestUndoManagedMutationReversesFreshActivation proves the bounded undo verb
// reverses a plan activation right after it happens (before any delivery gate has
// been recorded): all four managed artifacts are removed by re-applying the
// receipt's inverse through the same boundary.
func TestUndoManagedMutationReversesFreshActivation(t *testing.T) {
	root, _, compiled, lock, _ := activatePolicyPlan(t)
	id := activationReceiptID(t, root)
	if _, err := UndoManagedMutation(root, id); err != nil {
		t.Fatalf("undo of a fresh activation should be allowed: %v", err)
	}
	for _, path := range []string{
		filepath.Join(compiled, "tasks.json"),
		filepath.Join(compiled, "test-matrix.json"),
		filepath.Join(compiled, "evidence.md"),
		lock,
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("undo left managed artifact behind: %s", path)
		}
	}
}

// TestUndoManagedMutationRefusedAfterDeliveryGate proves the domain guard: once a
// delivery gate receipt exists, undoing the activation would strand delivery state
// without its plan lock, so the verb refuses (the primitive stays domain-agnostic;
// the guard lives in the verb layer, mirroring RepairState).
func TestUndoManagedMutationRefusedAfterDeliveryGate(t *testing.T) {
	root, _, compiled, lock, feature := activatePolicyPlan(t)
	id := activationReceiptID(t, root)

	statePath, err := deliveryStatePath(root, feature)
	if err != nil {
		t.Fatal(err)
	}
	gateDir := filepath.Join(filepath.Dir(statePath), "receipts", "slice-one")
	if err := os.MkdirAll(gateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gateDir, "test.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = UndoManagedMutation(root, id)
	if err == nil || !strings.Contains(err.Error(), "delivery has progressed") {
		t.Fatalf("expected undo to be refused after a delivery gate, got %v", err)
	}
	// The refusal must leave every artifact in place — a rejected undo is a no-op.
	for _, path := range []string{filepath.Join(compiled, "tasks.json"), lock} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("refused undo removed managed artifact %s: %v", path, statErr)
		}
	}
}

// TestControlledPhaseTransitionAllowsBoundedRecoveryVerbs proves the guard
// allowlists the two agent-facing verbs at any stage while still rejecting shell
// metacharacters and non-helper executables.
func TestControlledPhaseTransitionAllowsBoundedRecoveryVerbs(t *testing.T) {
	allowed := []struct {
		command string
		stage   string
	}{
		{"boatstack-helper mutation-status --repo .", "DELIVERY"},
		{"boatstack-helper mutation-status --repo . --mutation abc --json", "INVALID_STATE"},
		{"boatstack-helper undo --repo . --mutation abc", "INVALID_STATE"},
		{"boatstack-helper undo --repo . --mutation abc --json", "DELIVERY"},
	}
	for _, test := range allowed {
		if !controlledPhaseTransition(test.command, test.stage) {
			t.Fatalf("expected %q to be allowed at stage %s", test.command, test.stage)
		}
	}

	rejected := []string{
		"boatstack-helper undo --repo . --mutation abc; rm -rf .",
		"boatstack-helper undo --repo . --mutation abc && echo done",
		"git undo --mutation abc",
		"rm -rf .product-loop/plan.lock.json",
	}
	for _, command := range rejected {
		if controlledPhaseTransition(command, "INVALID_STATE") {
			t.Fatalf("expected %q to be rejected", command)
		}
	}
}
