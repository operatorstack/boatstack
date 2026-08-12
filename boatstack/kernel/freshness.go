package kernel

import "fmt"

// Freshness is the domain-independent compare-and-swap identity shared by
// every prescription. Snapshot means the exact canonical domain observation;
// ObjectiveBindingFingerprint identifies either the exact bound revision or
// verified absence.
type Freshness struct {
	ExpectedStateRevision               uint64 `json:"expected_state_revision"`
	ExpectedProgramFingerprint          string `json:"expected_program_fingerprint"`
	ExpectedSnapshotFingerprint         string `json:"expected_snapshot_fingerprint"`
	ExpectedObjectiveBindingFingerprint string `json:"expected_objective_binding_fingerprint"`
	AuthorityFingerprint                string `json:"authority_fingerprint"`
}

func NewFreshness(stateRevision uint64, programFingerprint, snapshotFingerprint, objectiveBindingFingerprint, authorityFingerprint string) (Freshness, error) {
	value := Freshness{
		ExpectedStateRevision: stateRevision, ExpectedProgramFingerprint: programFingerprint,
		ExpectedSnapshotFingerprint:         snapshotFingerprint,
		ExpectedObjectiveBindingFingerprint: objectiveBindingFingerprint,
		AuthorityFingerprint:                authorityFingerprint,
	}
	return value, value.Validate()
}

func (f Freshness) Validate() error {
	if f.ExpectedStateRevision == 0 || len(f.ExpectedProgramFingerprint) != 64 || len(f.ExpectedSnapshotFingerprint) != 64 || len(f.ExpectedObjectiveBindingFingerprint) != 64 || f.AuthorityFingerprint == "" {
		return fmt.Errorf("freshness requires exact state, program, snapshot, objective binding, and authority identities")
	}
	return nil
}

func (f Freshness) Check(current Freshness) error {
	if err := f.Validate(); err != nil {
		return err
	}
	if err := current.Validate(); err != nil {
		return err
	}
	if f != current {
		return fmt.Errorf("state, program, snapshot, objective binding, or authority changed")
	}
	return nil
}

// Fingerprint returns the canonical JSON content identity used at kernel
// boundaries. Domain adapters use it to bind their exact objective projection.
func Fingerprint(value any) (string, error) { return contentHash(value) }
