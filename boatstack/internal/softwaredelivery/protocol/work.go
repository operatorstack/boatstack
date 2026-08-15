package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	general "github.com/operatorstack/boatstack/boatstack/kernel"
)

const WorkEvidenceSchemaVersion = 2

// WorkInputValue binds the value presented to foreground work to the exact
// bytes selected by the trusted entry-input resolver. Value is an ergonomic
// locator or label; Fingerprint is the immutable execution identity.
type WorkInputValue struct {
	Value       string `json:"value"`
	Fingerprint string `json:"fingerprint"`
}

func (v WorkInputValue) Validate() error {
	if v.Value == "" || !validSHA256(v.Fingerprint) {
		return fmt.Errorf("foreground work input requires a value and exact fingerprint")
	}
	return nil
}

// WorkOutputEvidence is a verified, bounded foreground-work output. Content is
// carried into admission so effects never trust a mutable staging path.
type WorkOutputEvidence struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	MediaType string `json:"media_type"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	Content   string `json:"content"`
}

// WorkEvidence is result evidence, not authority and not a domain mutation.
// It is exact to one program, transition, stable context, repository, and
// worktree. Invocation-local driver IDs do not break restart continuity.
type WorkEvidence struct {
	SchemaVersion       int                  `json:"schema_version"`
	RequestID           string               `json:"request_id"`
	RequestFingerprint  string               `json:"request_fingerprint"`
	ResultFingerprint   string               `json:"result_fingerprint"`
	ContractID          string               `json:"contract_id"`
	ContractFingerprint string               `json:"contract_fingerprint"`
	TransitionID        catalog.TransitionID `json:"transition_id"`
	ProgramFingerprint  string               `json:"program_fingerprint"`
	ContextFingerprint  string               `json:"context_fingerprint"`
	StateRevision       uint64               `json:"state_revision"`
	RepositoryID        string               `json:"repository_id"`
	WorktreeID          string               `json:"worktree_id"`
	Outputs             []WorkOutputEvidence `json:"outputs"`
}

func (e WorkEvidence) Validate() error {
	if e.SchemaVersion != WorkEvidenceSchemaVersion || e.RequestID == "" || !validSHA256(e.RequestFingerprint) ||
		!validSHA256(e.ResultFingerprint) || e.ContractID == "" || !validSHA256(e.ContractFingerprint) || e.TransitionID == "" ||
		!validSHA256(e.ProgramFingerprint) || !validSHA256(e.ContextFingerprint) || e.StateRevision == 0 || e.RepositoryID == "" || e.WorktreeID == "" || len(e.Outputs) == 0 {
		return fmt.Errorf("foreground work evidence has incomplete identity")
	}
	seen := map[string]bool{}
	for _, output := range e.Outputs {
		contentDigest := sha256.Sum256([]byte(output.Content))
		if output.ID == "" || output.Path == "" || output.MediaType == "" || !validSHA256(output.SHA256) || hex.EncodeToString(contentDigest[:]) != output.SHA256 || output.Size < 0 || int64(len(output.Content)) != output.Size || seen[output.ID] {
			return fmt.Errorf("foreground work output evidence is incomplete or duplicated")
		}
		seen[output.ID] = true
	}
	canonical := e
	canonical.ResultFingerprint = ""
	want, err := general.Fingerprint(canonical)
	if err != nil || want != e.ResultFingerprint {
		return fmt.Errorf("foreground work result fingerprint is invalid")
	}
	return nil
}

func (e WorkEvidence) ValidateCurrent(snapshot model.Snapshot, transition catalog.Transition) error {
	if err := e.Validate(); err != nil {
		return err
	}
	contextFingerprint, err := model.ForegroundWorkContextFingerprint(snapshot)
	if err != nil {
		return fmt.Errorf("foreground work context fingerprint: %w", err)
	}
	if transition.Work == nil || e.ContractID != transition.Work.ID || e.ContractFingerprint != transition.Work.Fingerprint ||
		e.TransitionID != transition.ID || e.ProgramFingerprint != snapshot.ProgramFingerprint || e.ContextFingerprint != contextFingerprint ||
		e.StateRevision != snapshot.StateRevision || e.RepositoryID != snapshot.Invocation.RepositoryID || e.WorktreeID != snapshot.Invocation.WorktreeID {
		return fmt.Errorf("foreground work evidence is stale or belongs to a different transition context")
	}
	declared := map[string]catalog.WorkOutput{}
	for _, output := range transition.Work.Outputs {
		declared[output.ID] = output
	}
	for _, output := range e.Outputs {
		contract, ok := declared[output.ID]
		if !ok || contract.Path != output.Path || contract.MediaType != output.MediaType || output.Size > contract.MaxBytes {
			return fmt.Errorf("foreground work output %q does not match the trusted contract", output.ID)
		}
		delete(declared, output.ID)
	}
	for _, output := range declared {
		if output.Required {
			return fmt.Errorf("foreground work result is missing required output %q", output.ID)
		}
	}
	return nil
}

func CanonicalWorkOutputs(outputs []WorkOutputEvidence) []WorkOutputEvidence {
	result := append([]WorkOutputEvidence(nil), outputs...)
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func SealWorkEvidence(e WorkEvidence) (WorkEvidence, error) {
	e.Outputs = CanonicalWorkOutputs(e.Outputs)
	e.ResultFingerprint = ""
	fingerprint, err := general.Fingerprint(e)
	if err != nil {
		return WorkEvidence{}, err
	}
	e.ResultFingerprint = fingerprint
	return e, e.Validate()
}
