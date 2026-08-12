package protocol

import (
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

func TestReceiptRejectsRehashedAuthorityProvenanceTampering(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	authority := capabilityAuthority(now, catalog.AuthorityRepository, "policy")
	authorityFingerprint, err := authority.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	admission := Admission{
		ID: "admission", PrescriptionID: "prescription", ExpectedStateRevision: 1,
		ExpectedProgramFingerprint: strings.Repeat("a", 64), ExpectedSnapshotFingerprint: "source",
		Goal:      model.Goal{ID: "goal", Kind: model.GoalApprovedPlan, DeliveryID: "delivery"},
		Authority: authority, AuthorityFingerprint: authorityFingerprint,
		RequiredCapabilities:  []catalog.Capability{catalog.CapabilityRepositoryWrite},
		GrantedCapabilities:   authority.GrantedCapabilities(now),
		EffectiveCapabilities: []catalog.Capability{catalog.CapabilityRepositoryWrite},
		IdempotencyKey:        "idempotency",
	}
	transition := catalog.Transition{ID: "program/write", Version: 1, Verifier: "program.written"}
	receipt, err := NewReceipt("flow", 1, admission, transition, model.Snapshot{Observation: model.Observation{StateRevision: 2}, Fingerprint: "target"}, now, now.Add(time.Second), OutcomeSucceeded, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := receipt.Validate(); err != nil {
		t.Fatalf("valid receipt rejected: %v", err)
	}

	receipt.AuthoritySources[0].Fingerprint = "substituted-source"
	receipt.ID = ""
	receipt.ID, err = contentID("trc-", receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := receipt.Validate(); err == nil || !strings.Contains(err.Error(), "authority fingerprint does not match") {
		t.Fatalf("rehashed provenance substitution was accepted: %v", err)
	}
}
