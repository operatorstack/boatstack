package durable

import "testing"

func TestNextRevisionAdvancesExactlyOnceAndRejectsOverflow(t *testing.T) {
	// control-law: every successful logical transition advances one durable revision
	next, err := NextRevision(41)
	if err != nil || next != 42 {
		t.Fatalf("next revision = %d, %v", next, err)
	}
	if _, err := NextRevision(0); err == nil {
		t.Fatal("absent revision advanced")
	}
	if _, err := NextRevision(^uint64(0)); err == nil {
		t.Fatal("uint64 revision overflow wrapped")
	}
}
