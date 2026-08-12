package protocol

import (
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

func TestPrescriptionContentIdentityBindsTransitionStateProgramAndSnapshot(t *testing.T) {
	// control-law: resolution emits one immutable state-program CAS identity
	base := model.Snapshot{
		Observation: model.Observation{StateRevision: 41, ProgramFingerprint: strings.Repeat("a", 64)},
		Fingerprint: strings.Repeat("b", 64),
	}
	transition := catalog.Transition{ID: "program/advance"}
	one, err := NewPrescription(base, transition)
	if err != nil {
		t.Fatal(err)
	}
	if err := one.ValidateCurrent(base, transition); err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name       string
		snapshot   model.Snapshot
		transition catalog.Transition
	}{
		{name: "state", snapshot: model.Snapshot{Observation: model.Observation{StateRevision: 42, ProgramFingerprint: strings.Repeat("a", 64)}, Fingerprint: strings.Repeat("b", 64)}, transition: transition},
		{name: "program", snapshot: model.Snapshot{Observation: model.Observation{StateRevision: 41, ProgramFingerprint: strings.Repeat("c", 64)}, Fingerprint: strings.Repeat("b", 64)}, transition: transition},
		{name: "snapshot", snapshot: model.Snapshot{Observation: model.Observation{StateRevision: 41, ProgramFingerprint: strings.Repeat("a", 64)}, Fingerprint: strings.Repeat("d", 64)}, transition: transition},
		{name: "transition", snapshot: base, transition: catalog.Transition{ID: "program/other"}},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			other, err := NewPrescription(mutation.snapshot, mutation.transition)
			if err != nil {
				t.Fatal(err)
			}
			if other.ID == one.ID {
				t.Fatal("load-bearing prescription change preserved content identity")
			}
		})
	}
	tampered := one
	tampered.ExpectedStateRevision++
	if err := tampered.Validate(); err == nil {
		t.Fatal("tampered prescription retained validity")
	}
}
