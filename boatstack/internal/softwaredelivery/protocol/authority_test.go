package protocol

import (
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

func TestAuthorityFingerprintBindsIdentityProvenance(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	bundle := AuthorityBundle{Receipts: []AuthorityReceipt{{
		ID: "human", Class: catalog.AuthorityHuman, Subject: "operator", Fingerprint: "presentation",
		IdentityRole: "developer", IdentityProviderFingerprint: strings.Repeat("a", 64), IssuedAt: now,
	}}}
	base, err := bundle.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	changedRole := bundle
	changedRole.Receipts = append([]AuthorityReceipt(nil), bundle.Receipts...)
	changedRole.Receipts[0].IdentityRole = "reviewer"
	roleFingerprint, err := changedRole.Fingerprint()
	if err != nil || roleFingerprint == base {
		t.Fatalf("role fingerprint = %q, err=%v", roleFingerprint, err)
	}
	changedProvider := bundle
	changedProvider.Receipts = append([]AuthorityReceipt(nil), bundle.Receipts...)
	changedProvider.Receipts[0].IdentityProviderFingerprint = strings.Repeat("b", 64)
	providerFingerprint, err := changedProvider.Fingerprint()
	if err != nil || providerFingerprint == base {
		t.Fatalf("provider fingerprint = %q, err=%v", providerFingerprint, err)
	}
}

func TestRepositoryAuthorityWaitsForVerifiedConfiguration(t *testing.T) {
	// control-law: an authority request never fabricates repository evidence
	now := time.Unix(100, 0).UTC()
	autonomy := AuthorityBundle{Receipts: []AuthorityReceipt{{
		ID: "delegated-autonomy", Class: catalog.AuthorityAutonomy, Subject: "operator", Fingerprint: "delegation",
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}}}
	snapshot := model.Snapshot{Observation: model.Observation{
		Invocation:    model.InvocationContext{RepositoryID: "repository", GitCommonID: "git-common"},
		Configuration: model.Known(model.ConfigurationStale, model.Evidence{Source: "configuration:project.json", Fingerprint: "stale"}),
	}}

	result, err := DeriveRepositoryAuthorityWhenAvailable(snapshot, autonomy, now)
	if err != nil {
		t.Fatal(err)
	}
	set := result.Set(now)
	if !set[catalog.AuthorityAutonomy] || set[catalog.AuthorityRepository] {
		t.Fatalf("fresh authority projection = %v", set)
	}

	snapshot.Configuration = model.Known(model.ConfigurationVerified, model.Evidence{Source: "configuration:project.json", Fingerprint: "verified"})
	result, err = DeriveRepositoryAuthorityWhenAvailable(snapshot, autonomy, now)
	if err != nil {
		t.Fatal(err)
	}
	set = result.Set(now)
	if !set[catalog.AuthorityAutonomy] || !set[catalog.AuthorityRepository] {
		t.Fatalf("verified authority projection = %v", set)
	}
}
