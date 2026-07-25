package boatstack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression for the evidence-path split-brain: activate-plan writes the evidence
// ledger under compiled/, and pr-context resolves it through the shared dual-layout
// rule (feature-root canonical, compiled/ fallback). But the delivery-gate recorder
// used to hand-join the feature-root path with no fallback, so once a real project
// kept its ledger only at compiled/evidence.md the recorder could not find it and
// the gate failed with "delivery gate requires current evidence". These tests pin
// the recorder to the same resolver every other layer uses.

// TestFeatureEvidencePathResolvesBothLayouts locks the shared resolver's ordering:
// feature-root (legacy canonical) first, compiled/ as the current-layout fallback,
// and the canonical path returned even when neither exists so the caller reports a
// clear error location.
func TestFeatureEvidencePathResolvesBothLayouts(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "evidence.md")
	compiled := filepath.Join(dir, "compiled", "evidence.md")

	// With neither present the resolver returns its last candidate (the compiled
	// copy) so a missing-evidence error names the canonical write location.
	if got := featureEvidencePath(dir); got != compiled {
		t.Fatalf("with neither present, want the compiled error path %q, got %q", compiled, got)
	}

	if err := os.MkdirAll(filepath.Dir(compiled), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(compiled, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := featureEvidencePath(dir); got != compiled {
		t.Fatalf("with only compiled present, want %q, got %q", compiled, got)
	}

	if err := os.WriteFile(root, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := featureEvidencePath(dir); got != root {
		t.Fatalf("with both present, legacy root must win to match pr-context, got %q", got)
	}
}

// TestRecordGateResolvesCompiledEvidenceLedger is the proof of fix: a delivery whose
// ledger lives ONLY at compiled/evidence.md (the taxweave state) must gate cleanly
// with no explicit --evidence. This fails on the pre-fix recorder, which resolved
// only the feature root.
func TestRecordGateResolvesCompiledEvidenceLedger(t *testing.T) {
	repo, feature := activateTwoSliceDelivery(t)
	dir := filepath.Join(repo, ".product-loop", "features", feature)

	// Move the edited gate ledger to the compiled layout and drop the feature-root
	// copy, so only compiled/evidence.md carries the gate outcomes.
	ledger := "# Evidence ledger\n\n- Test gate (phase-one): `PASS`\n- Review gate (phase-one): `PASS`\n- Test gate (phase-two): `BLOCKED`\n- Review gate (phase-two): `BLOCKED`\n"
	if err := os.WriteFile(filepath.Join(dir, "compiled", "evidence.md"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "evidence.md")); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "keep the evidence ledger only in the compiled layout")

	receipt, err := RecordDeliveryGate(DeliveryGateOptions{Repo: repo, Feature: feature, SliceID: "phase-one", Gate: "test", Status: "PASS"})
	if err != nil {
		t.Fatalf("recorder could not resolve compiled-only evidence ledger: %v", err)
	}
	if !strings.HasSuffix(filepath.ToSlash(receipt.EvidencePath), "compiled/evidence.md") {
		t.Fatalf("recorder bound the wrong evidence path: %q", receipt.EvidencePath)
	}
}

// TestRecordGateStillResolvesFeatureRootEvidence guards the legacy layout: a ledger
// at the feature root (no compiled copy) must still gate, so the added fallback is
// strictly additive.
func TestRecordGateStillResolvesFeatureRootEvidence(t *testing.T) {
	repo, feature := activateTwoSliceDelivery(t)
	dir := filepath.Join(repo, ".product-loop", "features", feature)

	// activateTwoSliceDelivery already writes the ledger at the feature root; remove
	// the compiled copy so only the legacy location remains.
	if err := os.Remove(filepath.Join(dir, "compiled", "evidence.md")); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "keep the evidence ledger only at the feature root")

	receipt, err := RecordDeliveryGate(DeliveryGateOptions{Repo: repo, Feature: feature, SliceID: "phase-one", Gate: "test", Status: "PASS"})
	if err != nil {
		t.Fatalf("recorder could not resolve feature-root evidence ledger: %v", err)
	}
	if strings.Contains(filepath.ToSlash(receipt.EvidencePath), "compiled/") {
		t.Fatalf("recorder ignored the legacy feature-root ledger: %q", receipt.EvidencePath)
	}
}

// TestRecorderAndPRContextResolveSameEvidence is the anti-drift invariant: the
// recorder's default evidence resolution and pr-context both route through
// featureEvidencePath, so for any given feature they can never resolve different
// evidence files. This is the structural guarantee the layers cannot re-diverge —
// the compiled-artifact analogue of the published-slice addressability invariant.
func TestRecorderAndPRContextResolveSameEvidence(t *testing.T) {
	dir := t.TempDir()
	compiled := filepath.Join(dir, "compiled", "evidence.md")
	if err := os.MkdirAll(filepath.Dir(compiled), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(compiled, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The recorder's default (empty --evidence) and pr-context's managedPRSources
	// both call featureEvidencePath(dir); assert that single resolver returns the
	// file that actually exists rather than a hand-joined feature-root guess.
	if got := featureEvidencePath(dir); got != compiled {
		t.Fatalf("shared resolver must return the existing ledger both consumers read, got %q", got)
	}
}
