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
	SchemaRevision = 4
)

var identity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var fingerprint = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Request struct {
	RunID                            string   `json:"run_id"`
	ProgramID                        string   `json:"program_id"`
	ProgramFingerprint               string   `json:"program_fingerprint"`
	ControlBundleFingerprint         string   `json:"control_bundle_fingerprint"`
	EntryID                          string   `json:"entry_id"`
	TargetID                         string   `json:"target_id"`
	ObjectiveID                      string   `json:"objective_id"`
	DeliveryID                       string   `json:"delivery_id"`
	InputFingerprints                []string `json:"input_fingerprints"`
	RepositoryID                     string   `json:"repository_id"`
	GitCommonID                      string   `json:"git_common_id"`
	InitialWorktreeID                string   `json:"initial_worktree_id"`
	InitialRef                       string   `json:"initial_ref"`
	BindingFingerprint               string   `json:"binding_fingerprint"`
	HumanIdentityRole                string   `json:"human_identity_role"`
	HumanIdentityProviderFingerprint string   `json:"human_identity_provider_fingerprint"`
	RequestedAuthorities             []string `json:"requested_authorities"`
	Description                      string   `json:"description"`
}

func (r Request) Fingerprint() (string, error) {
	if !identity.MatchString(r.RunID) || !identity.MatchString(r.ProgramID) || len(r.ProgramFingerprint) != 64 || len(r.ControlBundleFingerprint) != 64 || !identity.MatchString(r.EntryID) || !identity.MatchString(r.TargetID) || r.ObjectiveID == "" || r.DeliveryID == "" || r.RepositoryID == "" || r.GitCommonID == "" || r.InitialWorktreeID == "" || r.InitialRef == "" || len(r.BindingFingerprint) != 64 || humanidentity.ValidateRole(r.HumanIdentityRole) != nil || !fingerprint.MatchString(r.HumanIdentityProviderFingerprint) || len(r.RequestedAuthorities) == 0 || r.Description == "" {
		return "", fmt.Errorf("DELEGATION_REQUEST_INVALID: request is incomplete")
	}
	r.InputFingerprints = append([]string(nil), r.InputFingerprints...)
	r.RequestedAuthorities = append([]string(nil), r.RequestedAuthorities...)
	sort.Strings(r.InputFingerprints)
	sort.Strings(r.RequestedAuthorities)
	for index, value := range r.RequestedAuthorities {
		if value == "" || (index > 0 && r.RequestedAuthorities[index-1] == value) {
			return "", fmt.Errorf("DELEGATION_REQUEST_INVALID: authorities are empty or duplicated")
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
