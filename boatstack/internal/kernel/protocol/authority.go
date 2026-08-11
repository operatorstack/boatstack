package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

type AuthorityReceipt struct {
	ID          string                 `json:"id"`
	Class       catalog.AuthorityClass `json:"class"`
	Subject     string                 `json:"subject"`
	Fingerprint string                 `json:"fingerprint"`
	IssuedAt    time.Time              `json:"issued_at"`
	ExpiresAt   time.Time              `json:"expires_at,omitempty"`
}

func (r AuthorityReceipt) Validate(now time.Time) error {
	if r.ID == "" || r.Subject == "" || r.Fingerprint == "" || r.IssuedAt.IsZero() {
		return fmt.Errorf("authority receipt requires id, subject, fingerprint, and issue time")
	}
	if !r.Class.Valid() || r.Class == catalog.AuthorityNone {
		return fmt.Errorf("authority receipt has invalid class %q", r.Class)
	}
	if !r.ExpiresAt.IsZero() && !now.Before(r.ExpiresAt) {
		return fmt.Errorf("authority receipt %q expired", r.ID)
	}
	return nil
}

type AuthorityBundle struct {
	Receipts []AuthorityReceipt `json:"receipts"`
}

func (b AuthorityBundle) Validate(now time.Time) error {
	seen := map[string]bool{}
	for _, receipt := range b.Receipts {
		if err := receipt.Validate(now); err != nil {
			return err
		}
		if seen[receipt.ID] {
			return fmt.Errorf("authority receipt %q is duplicated", receipt.ID)
		}
		seen[receipt.ID] = true
	}
	return nil
}

func (b AuthorityBundle) Set(now time.Time) catalog.AuthoritySet {
	set := catalog.AuthoritySet{}
	for _, receipt := range b.Receipts {
		if receipt.Validate(now) == nil {
			set[receipt.Class] = true
		}
	}
	return set
}

func (b AuthorityBundle) canonical() AuthorityBundle {
	result := AuthorityBundle{Receipts: append([]AuthorityReceipt(nil), b.Receipts...)}
	sort.Slice(result.Receipts, func(i, j int) bool { return result.Receipts[i].ID < result.Receipts[j].ID })
	return result
}

func DeriveRepositoryAuthority(snapshot model.Snapshot, bundle AuthorityBundle, now time.Time) (AuthorityBundle, error) {
	for _, receipt := range bundle.Receipts {
		if receipt.Class == catalog.AuthorityRepository {
			return AuthorityBundle{}, fmt.Errorf("repository authority must be derived once by the V2 kernel")
		}
	}
	if snapshot.Configuration.Status != model.FactKnown || snapshot.Configuration.Value != model.ConfigurationVerified {
		return AuthorityBundle{}, fmt.Errorf("repository authority requires current verified configuration evidence")
	}
	var source, fingerprint string
	for _, evidence := range snapshot.Configuration.Evidence {
		if strings.HasPrefix(evidence.Source, "configuration:") {
			source, fingerprint = evidence.Source, evidence.Fingerprint
			break
		}
	}
	if source == "" || fingerprint == "" {
		return AuthorityBundle{}, fmt.Errorf("repository authority has no exact configuration evidence")
	}
	digest := sha256.Sum256([]byte(snapshot.Invocation.RepositoryID + "\x00" + snapshot.Invocation.GitCommonID + "\x00" + source + "\x00" + fingerprint))
	bundle.Receipts = append(bundle.Receipts, AuthorityReceipt{
		ID: "policy-" + hex.EncodeToString(digest[:])[:16], Class: catalog.AuthorityRepository,
		Subject: source, Fingerprint: fingerprint, IssuedAt: now.UTC(), ExpiresAt: now.Add(2 * time.Minute).UTC(),
	})
	return bundle, nil
}
