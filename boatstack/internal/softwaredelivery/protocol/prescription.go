package protocol

import (
	"fmt"

	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	general "github.com/operatorstack/boatstack/boatstack/kernel"
)

const PrescriptionSchemaVersion = 6

// Prescription is the immutable compare-and-swap binding emitted by
// resolution and required by apply. It carries no reusable authority or
// credential material, only the content identity of the admitted projection.
type Prescription struct {
	SchemaVersion int                  `json:"schema_version"`
	ID            string               `json:"id"`
	TransitionID  catalog.TransitionID `json:"transition_id"`
	general.Freshness
	RequiredCapabilities     []catalog.Capability `json:"required_capabilities"`
	EffectiveCapabilities    []catalog.Capability `json:"effective_capabilities"`
	WorkResultFingerprint    string               `json:"work_result_fingerprint,omitempty"`
	ControlBundleFingerprint string               `json:"control_bundle_fingerprint,omitempty"`
}

func NewPrescription(snapshot model.Snapshot, transition catalog.Transition, capabilities CapabilityProjection) (Prescription, error) {
	return NewPrescriptionWithWork(snapshot, transition, capabilities, nil)
}

func NewPrescriptionWithWork(snapshot model.Snapshot, transition catalog.Transition, capabilities CapabilityProjection, work *WorkEvidence) (Prescription, error) {
	return NewPrescriptionWithWorkAndBundle(snapshot, transition, capabilities, work, nil)
}

func NewPrescriptionWithWorkAndBundle(snapshot model.Snapshot, transition catalog.Transition, capabilities CapabilityProjection, work *WorkEvidence, bundle *boatstackruntime.ControlBundleContract) (Prescription, error) {
	objectiveBindingFingerprint, err := ObjectiveBindingFingerprint(snapshot)
	if err != nil {
		return Prescription{}, err
	}
	freshness, err := general.NewFreshness(snapshot.Invocation.RepositoryID, snapshot.StateRevision, snapshot.ProgramFingerprint, snapshot.Fingerprint, objectiveBindingFingerprint, capabilities.AuthorityFingerprint)
	if err != nil {
		return Prescription{}, err
	}
	prescription := Prescription{
		SchemaVersion:         PrescriptionSchemaVersion,
		TransitionID:          transition.ID,
		Freshness:             freshness,
		RequiredCapabilities:  append([]catalog.Capability(nil), capabilities.Required...),
		EffectiveCapabilities: append([]catalog.Capability(nil), capabilities.Effective...),
	}
	if err := ValidateControlBundleForTransition(bundle, transition); err != nil {
		return Prescription{}, err
	}
	if bundle != nil {
		prescription.ControlBundleFingerprint = bundle.Fingerprint
	}
	if work != nil {
		if err := work.ValidateCurrent(snapshot, transition); err != nil {
			return Prescription{}, err
		}
		prescription.WorkResultFingerprint = work.ResultFingerprint
	}
	if err := prescription.validateFields(); err != nil {
		return Prescription{}, err
	}
	identity := prescription
	identity.ID = ""
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
	if p.SchemaVersion != PrescriptionSchemaVersion || p.TransitionID == "" || p.Freshness.Validate() != nil ||
		len(p.RequiredCapabilities) == 0 || len(p.EffectiveCapabilities) == 0 || (p.ControlBundleFingerprint != "" && len(p.ControlBundleFingerprint) != 64) {
		return fmt.Errorf("prescription has invalid schema, transition, state revision, program, or snapshot identity")
	}
	return nil
}

func (p Prescription) ValidateCurrent(snapshot model.Snapshot, transition catalog.Transition, capabilities CapabilityProjection) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if p.TransitionID != transition.ID {
		return fmt.Errorf("prescription %q is bound to transition %q, not %q", p.ID, p.TransitionID, transition.ID)
	}
	objectiveBindingFingerprint, err := ObjectiveBindingFingerprint(snapshot)
	if err != nil {
		return err
	}
	current, err := general.NewFreshness(snapshot.Invocation.RepositoryID, snapshot.StateRevision, snapshot.ProgramFingerprint, snapshot.Fingerprint, objectiveBindingFingerprint, capabilities.AuthorityFingerprint)
	if err != nil {
		return err
	}
	if err := p.Freshness.Check(current); err != nil {
		return fmt.Errorf("prescription %q is stale: %w", p.ID, err)
	}
	if p.AuthorityFingerprint != capabilities.AuthorityFingerprint ||
		!sameCapabilities(p.RequiredCapabilities, capabilities.Required) ||
		!sameCapabilities(p.EffectiveCapabilities, capabilities.Effective) {
		return fmt.Errorf("prescription %q is bound to a different authority or capability context", p.ID)
	}
	return nil
}

func (p Prescription) ValidateWork(work *WorkEvidence) error {
	if p.WorkResultFingerprint == "" {
		if work != nil {
			return fmt.Errorf("prescription is not bound to foreground work")
		}
		return nil
	}
	if work == nil || work.ResultFingerprint != p.WorkResultFingerprint {
		return fmt.Errorf("prescription is bound to a different foreground work result")
	}
	return nil
}

func (p Prescription) ValidateControlBundle(bundle *boatstackruntime.ControlBundleContract, transition catalog.Transition) error {
	if err := ValidateControlBundleForTransition(bundle, transition); err != nil {
		return err
	}
	fingerprint := ""
	if bundle != nil {
		fingerprint = bundle.Fingerprint
	}
	if p.ControlBundleFingerprint != fingerprint {
		return fmt.Errorf("prescription is bound to a different repository control bundle")
	}
	return nil
}

// ObjectiveBindingFingerprint projects only the durable binding status and
// value. Observation evidence may be refreshed without changing the binding.
func ObjectiveBindingFingerprint(snapshot model.Snapshot) (string, error) {
	return general.Fingerprint(struct {
		Status    model.FactStatus `json:"status"`
		Objective model.Objective  `json:"objective"`
	}{snapshot.Objective.Status, snapshot.Objective.Value})
}
