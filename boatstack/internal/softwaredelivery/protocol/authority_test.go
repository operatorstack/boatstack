package protocol

import (
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

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
