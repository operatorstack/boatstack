package protocol

import (
	"fmt"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

const PrescriptionSchemaVersion = 1

// Prescription is the immutable compare-and-swap binding emitted by
// resolution and required by apply. It carries no authority.
type Prescription struct {
	SchemaVersion               int                  `json:"schema_version"`
	ID                          string               `json:"id"`
	TransitionID                catalog.TransitionID `json:"transition_id"`
	ExpectedStateRevision       uint64               `json:"expected_state_revision"`
	ExpectedProgramFingerprint  string               `json:"expected_program_fingerprint"`
	ExpectedSnapshotFingerprint string               `json:"expected_snapshot_fingerprint"`
}

func NewPrescription(snapshot model.Snapshot, transition catalog.Transition) (Prescription, error) {
	prescription := Prescription{
		SchemaVersion:               PrescriptionSchemaVersion,
		TransitionID:                transition.ID,
		ExpectedStateRevision:       snapshot.StateRevision,
		ExpectedProgramFingerprint:  snapshot.ProgramFingerprint,
		ExpectedSnapshotFingerprint: snapshot.Fingerprint,
	}
	if err := prescription.validateFields(); err != nil {
		return Prescription{}, err
	}
	identity := prescription
	identity.ID = ""
	var err error
	prescription.ID, err = contentID("prx-", identity)
	if err != nil {
		return Prescription{}, err
	}
	return prescription, nil
}

func (p Prescription) Validate() error {
	if err := p.validateFields(); err != nil {
		return err
	}
	identity := p
	want := identity.ID
	identity.ID = ""
	got, err := contentID("prx-", identity)
	if err != nil {
		return err
	}
	if want == "" || got != want {
		return fmt.Errorf("prescription failed content identity verification")
	}
	return nil
}

func (p Prescription) validateFields() error {
	if p.SchemaVersion != PrescriptionSchemaVersion || p.TransitionID == "" || p.ExpectedStateRevision == 0 ||
		len(p.ExpectedProgramFingerprint) != 64 || len(p.ExpectedSnapshotFingerprint) != 64 {
		return fmt.Errorf("prescription has invalid schema, transition, state revision, program, or snapshot identity")
	}
	return nil
}

func (p Prescription) ValidateCurrent(snapshot model.Snapshot, transition catalog.Transition) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if p.TransitionID != transition.ID {
		return fmt.Errorf("prescription %q is bound to transition %q, not %q", p.ID, p.TransitionID, transition.ID)
	}
	if p.ExpectedStateRevision != snapshot.StateRevision {
		return fmt.Errorf("prescription %q expected state revision %d, observed %d", p.ID, p.ExpectedStateRevision, snapshot.StateRevision)
	}
	if p.ExpectedProgramFingerprint != snapshot.ProgramFingerprint {
		return fmt.Errorf("prescription %q expected control program %s, observed %s", p.ID, p.ExpectedProgramFingerprint, snapshot.ProgramFingerprint)
	}
	if p.ExpectedSnapshotFingerprint != snapshot.Fingerprint {
		return fmt.Errorf("prescription %q expected snapshot %s, observed %s", p.ID, p.ExpectedSnapshotFingerprint, snapshot.Fingerprint)
	}
	return nil
}
