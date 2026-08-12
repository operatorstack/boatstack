// Package kernel implements Boatstack's domain-neutral supervisory-control
// mechanism. Domain packages supply observations, transition predicates, and
// operators; the kernel owns freshness, authority, selection, verification,
// state revision, and receipts.
package kernel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"time"
)

const (
	ProgramSchemaVersion      = 1
	PrescriptionSchemaVersion = 2
	ReceiptSchemaVersion      = 3
)

var semanticID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Objective is an external reference. It is not supervisory state.
type Objective struct {
	ID          string          `json:"id"`
	Revision    uint64          `json:"revision"`
	Fingerprint string          `json:"fingerprint"`
	Reference   json.RawMessage `json:"reference"`
}

func NewObjective(id string, revision uint64, reference any) (Objective, error) {
	encoded, err := json.Marshal(reference)
	if err != nil {
		return Objective{}, err
	}
	objective := Objective{ID: id, Revision: revision, Reference: encoded}
	fingerprint, err := contentHash(struct {
		ID        string          `json:"id"`
		Revision  uint64          `json:"revision"`
		Reference json.RawMessage `json:"reference"`
	}{objective.ID, objective.Revision, objective.Reference})
	if err != nil {
		return Objective{}, err
	}
	objective.Fingerprint = fingerprint
	return objective, objective.Validate()
}

func (o Objective) Validate() error {
	if !semanticID.MatchString(o.ID) || o.Revision == 0 || len(o.Fingerprint) != 64 || len(o.Reference) == 0 || !json.Valid(o.Reference) {
		return fmt.Errorf("objective requires semantic identity, positive revision, canonical reference, and fingerprint")
	}
	fingerprint, err := contentHash(struct {
		ID        string          `json:"id"`
		Revision  uint64          `json:"revision"`
		Reference json.RawMessage `json:"reference"`
	}{o.ID, o.Revision, o.Reference})
	if err != nil || fingerprint != o.Fingerprint {
		return fmt.Errorf("objective fingerprint does not identify its exact revision")
	}
	return nil
}

// ObjectiveBinding is the only objective material retained in supervisory
// state. It binds an exact immutable objective revision.
type ObjectiveBinding struct {
	ObjectiveID          string `json:"objective_id"`
	ObjectiveRevision    uint64 `json:"objective_revision"`
	ObjectiveFingerprint string `json:"objective_fingerprint"`
}

func BindObjective(objective Objective) (ObjectiveBinding, error) {
	if err := objective.Validate(); err != nil {
		return ObjectiveBinding{}, err
	}
	return ObjectiveBinding{objective.ID, objective.Revision, objective.Fingerprint}, nil
}

func (b ObjectiveBinding) Validate() error {
	if !semanticID.MatchString(b.ObjectiveID) || b.ObjectiveRevision == 0 || len(b.ObjectiveFingerprint) != 64 {
		return fmt.Errorf("objective binding requires exact identity, revision, and fingerprint")
	}
	return nil
}

func (b ObjectiveBinding) Matches(objective Objective) bool {
	return objective.Validate() == nil && b.ObjectiveID == objective.ID && b.ObjectiveRevision == objective.Revision && b.ObjectiveFingerprint == objective.Fingerprint
}

type ProgramIdentity struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Fingerprint string `json:"fingerprint"`
}

func (p ProgramIdentity) Validate() error {
	if !semanticID.MatchString(p.ID) || p.Version == "" || len(p.Fingerprint) != 64 {
		return fmt.Errorf("program identity requires id, version, and fingerprint")
	}
	return nil
}

// ControlState is durable supervisory state. Domain state is deliberately not
// embedded here; it is supplied as a canonical observation by a Domain.
type ControlState struct {
	InstanceID       string            `json:"instance_id"`
	Program          ProgramIdentity   `json:"program"`
	ObjectiveBinding *ObjectiveBinding `json:"objective_binding,omitempty"`
	Mode             string            `json:"mode"`
	Revision         uint64            `json:"revision"`
	Recovery         *RecoveryState    `json:"recovery,omitempty"`
}

type RecoveryState struct {
	PrescriptionID string `json:"prescription_id"`
	TransitionID   string `json:"transition_id"`
	Reason         string `json:"reason"`
}

func (s ControlState) Validate() error {
	if !semanticID.MatchString(s.InstanceID) || s.Mode == "" || s.Revision == 0 {
		return fmt.Errorf("control state requires instance, mode, and positive revision")
	}
	if err := s.Program.Validate(); err != nil {
		return err
	}
	if s.ObjectiveBinding != nil {
		if err := s.ObjectiveBinding.Validate(); err != nil {
			return err
		}
	}
	if s.Recovery != nil && (s.Recovery.PrescriptionID == "" || !semanticID.MatchString(s.Recovery.TransitionID) || s.Recovery.Reason == "") {
		return fmt.Errorf("recovery state is incomplete")
	}
	return nil
}

type ObjectiveScope string

const (
	ObjectiveNone             ObjectiveScope = "none"
	ObjectiveOptionalPreserve ObjectiveScope = "optional-preserve"
	ObjectiveBoundExact       ObjectiveScope = "bound-exact"
)

func (s ObjectiveScope) Valid() bool {
	return s == ObjectiveNone || s == ObjectiveOptionalPreserve || s == ObjectiveBoundExact
}

type Capability string

func (c Capability) Validate() error {
	if !semanticID.MatchString(string(c)) {
		return fmt.Errorf("capability %q is not a semantic identifier", c)
	}
	return nil
}

type AuthorityReceipt struct {
	ID           string       `json:"id"`
	Subject      string       `json:"subject"`
	Fingerprint  string       `json:"fingerprint"`
	Capabilities []Capability `json:"capabilities"`
	IssuedAt     time.Time    `json:"issued_at"`
	ExpiresAt    time.Time    `json:"expires_at,omitempty"`
}

func (r AuthorityReceipt) Validate(now time.Time) error {
	if !semanticID.MatchString(r.ID) || r.Subject == "" || r.Fingerprint == "" || r.IssuedAt.IsZero() || r.IssuedAt.After(now) || (!r.ExpiresAt.IsZero() && !now.Before(r.ExpiresAt)) {
		return fmt.Errorf("authority receipt %q is invalid or expired", r.ID)
	}
	if len(r.Capabilities) == 0 {
		return fmt.Errorf("authority receipt %q grants no capabilities", r.ID)
	}
	_, err := normalizeCapabilities(r.Capabilities)
	return err
}

type Authority struct {
	Receipts []AuthorityReceipt `json:"receipts"`
}

func (a Authority) projection(now time.Time) (authorityProjection, error) {
	receipts := append([]AuthorityReceipt(nil), a.Receipts...)
	sort.Slice(receipts, func(i, j int) bool { return receipts[i].ID < receipts[j].ID })
	seen := map[string]bool{}
	var capabilities []Capability
	for _, receipt := range receipts {
		if err := receipt.Validate(now); err != nil {
			return authorityProjection{}, err
		}
		if seen[receipt.ID] {
			return authorityProjection{}, fmt.Errorf("authority receipt %q is duplicated", receipt.ID)
		}
		seen[receipt.ID] = true
		capabilities = append(capabilities, receipt.Capabilities...)
	}
	capabilities, err := normalizeCapabilities(capabilities)
	if err != nil {
		return authorityProjection{}, err
	}
	fingerprint, err := contentHash(receipts)
	return authorityProjection{Fingerprint: fingerprint, Capabilities: capabilities}, err
}

type authorityProjection struct {
	Fingerprint  string
	Capabilities []Capability
}

type EffectFact struct {
	Facet       string `json:"facet"`
	Operation   string `json:"operation"`
	Fingerprint string `json:"fingerprint"`
}

type Effect struct {
	Facts []EffectFact `json:"facts"`
}

func normalizeCapabilities(values []Capability) ([]Capability, error) {
	seen := map[Capability]bool{}
	for _, value := range values {
		if err := value.Validate(); err != nil {
			return nil, err
		}
		seen[value] = true
	}
	result := make([]Capability, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}

func contentHash(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}
