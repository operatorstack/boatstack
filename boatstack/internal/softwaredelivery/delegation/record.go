// Package delegation owns run-bound authority records outside repositories.
package delegation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/humanidentity"
)

const (
	Schema         = "run-delegation"
	SchemaRevision = 5
)

var identity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var fingerprint = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Request struct {
	RunID                            string             `json:"run_id"`
	ProgramID                        string             `json:"program_id"`
	ProgramFingerprint               string             `json:"program_fingerprint"`
	ControlBundleFingerprint         string             `json:"control_bundle_fingerprint"`
	EntryID                          string             `json:"entry_id"`
	TargetID                         string             `json:"target_id"`
	ObjectiveID                      string             `json:"objective_id"`
	DeliveryID                       string             `json:"delivery_id"`
	InputFingerprints                []InputFingerprint `json:"input_fingerprints"`
	RepositoryID                     string             `json:"repository_id"`
	GitCommonID                      string             `json:"git_common_id"`
	InitialWorktreeID                string             `json:"initial_worktree_id"`
	InitialRef                       string             `json:"initial_ref"`
	EntryActivationAuthorities       []string           `json:"entry_activation_authorities,omitempty"`
	BindingFingerprint               string             `json:"binding_fingerprint,omitempty"`
	HumanIdentityRole                string             `json:"human_identity_role"`
	HumanIdentityProviderFingerprint string             `json:"human_identity_provider_fingerprint"`
	RequestedAuthorities             []string           `json:"requested_authorities,omitempty"`
	Description                      string             `json:"description"`
}

// InputFingerprint binds authorization to one exact, named entry input.
type InputFingerprint struct {
	ID          string `json:"id"`
	Fingerprint string `json:"fingerprint"`
}

func (r Request) Fingerprint() (string, error) {
	delegationPresent := len(r.RequestedAuthorities) != 0
	activationPresent := len(r.EntryActivationAuthorities) != 0
	if !identity.MatchString(r.RunID) || !identity.MatchString(r.ProgramID) || !fingerprint.MatchString(r.ProgramFingerprint) || !fingerprint.MatchString(r.ControlBundleFingerprint) || !identity.MatchString(r.EntryID) || !identity.MatchString(r.TargetID) || r.ObjectiveID == "" || r.DeliveryID == "" || r.RepositoryID == "" || r.GitCommonID == "" || r.InitialWorktreeID == "" || r.InitialRef == "" || (!activationPresent && !delegationPresent) || (delegationPresent != fingerprint.MatchString(r.BindingFingerprint)) || humanidentity.ValidateRole(r.HumanIdentityRole) != nil || !fingerprint.MatchString(r.HumanIdentityProviderFingerprint) || r.Description == "" {
		return "", fmt.Errorf("DELEGATION_REQUEST_INVALID: request is incomplete")
	}
	r.InputFingerprints = append([]InputFingerprint(nil), r.InputFingerprints...)
	r.EntryActivationAuthorities = append([]string(nil), r.EntryActivationAuthorities...)
	r.RequestedAuthorities = append([]string(nil), r.RequestedAuthorities...)
	sort.Slice(r.InputFingerprints, func(i, j int) bool { return r.InputFingerprints[i].ID < r.InputFingerprints[j].ID })
	sort.Strings(r.EntryActivationAuthorities)
	sort.Strings(r.RequestedAuthorities)
	for index, value := range r.InputFingerprints {
		if !identity.MatchString(value.ID) || !fingerprint.MatchString(value.Fingerprint) || (index > 0 && r.InputFingerprints[index-1].ID == value.ID) {
			return "", fmt.Errorf("DELEGATION_REQUEST_INVALID: input fingerprints are invalid or duplicated")
		}
	}
	for _, authorities := range [][]string{r.EntryActivationAuthorities, r.RequestedAuthorities} {
		for index, value := range authorities {
			if value == "" || (index > 0 && authorities[index-1] == value) {
				return "", fmt.Errorf("DELEGATION_REQUEST_INVALID: authorities are empty or duplicated")
			}
		}
	}
	for _, value := range r.EntryActivationAuthorities {
		if value != "human" {
			return "", fmt.Errorf("ENTRY_ACTIVATION_AUTHORITY_UNSUPPORTED: no trusted producer exists for %q", value)
		}
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

type Record struct {
	Schema                           string    `json:"schema"`
	SchemaRevision                   int       `json:"schema_revision"`
	Request                          Request   `json:"request"`
	RequestFingerprint               string    `json:"request_fingerprint"`
	ReceiptID                        string    `json:"receipt_id"`
	Actor                            string    `json:"actor"`
	ActorIdentityRole                string    `json:"actor_identity_role"`
	ActorIdentityProviderFingerprint string    `json:"actor_identity_provider_fingerprint"`
	AuthorizedAt                     time.Time `json:"authorized_at"`
	ExpiresAt                        time.Time `json:"expires_at,omitempty"`
	Revision                         uint64    `json:"revision"`
	Status                           string    `json:"status"`
	RevokedAt                        time.Time `json:"revoked_at,omitempty"`
	EndedAt                          time.Time `json:"ended_at,omitempty"`
	EndReason                        string    `json:"end_reason,omitempty"`
}

func Path(flowRoot, runID string) (string, error) {
	if !identity.MatchString(runID) {
		return "", fmt.Errorf("DELEGATION_RUN_INVALID: invalid run identity")
	}
	return filepath.Join(flowRoot, "delegations", runID+".json"), nil
}

// SupersededPath preserves an earlier exact authorization record before a
// verified installation boundary accepts a fresh delegation request.
func SupersededPath(flowRoot, runID, requestFingerprint string) (string, error) {
	if !identity.MatchString(runID) || !fingerprint.MatchString(requestFingerprint) {
		return "", fmt.Errorf("DELEGATION_RUN_INVALID: invalid superseded authorization identity")
	}
	return filepath.Join(flowRoot, "delegations", runID+".superseded-"+requestFingerprint[:12]+".json"), nil
}

func LockPath(lockRoot, runID string) (string, error) {
	if !identity.MatchString(runID) {
		return "", fmt.Errorf("DELEGATION_RUN_INVALID: invalid run identity")
	}
	return filepath.Join(lockRoot, "delegation-"+runID+".lock"), nil
}

func Load(path string) (Record, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Record{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var record Record
	if err := decoder.Decode(&record); err != nil {
		return Record{}, fmt.Errorf("DELEGATION_RECORD_INVALID: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Record{}, fmt.Errorf("DELEGATION_RECORD_INVALID: trailing JSON")
	}
	if record.Schema != Schema || record.SchemaRevision != SchemaRevision || record.Revision == 0 || record.Status == "" || record.Actor == "" || record.ReceiptID == "" || !fingerprint.MatchString(record.ActorIdentityProviderFingerprint) {
		return Record{}, fmt.Errorf("DELEGATION_RECORD_INVALID: record is incomplete")
	}
	if err := humanidentity.ValidateActor(record.Actor); err != nil || record.ActorIdentityRole != record.Request.HumanIdentityRole || record.ActorIdentityProviderFingerprint != record.Request.HumanIdentityProviderFingerprint {
		return Record{}, fmt.Errorf("DELEGATION_RECORD_INVALID: actor identity provenance is invalid")
	}
	fingerprint, err := record.Request.Fingerprint()
	if err != nil || fingerprint != record.RequestFingerprint {
		return Record{}, fmt.Errorf("DELEGATION_RECORD_INVALID: request fingerprint mismatch")
	}
	return record, nil
}
