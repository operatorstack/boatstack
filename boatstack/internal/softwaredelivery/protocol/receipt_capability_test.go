package protocol

import (
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
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
		ExpectedProgramFingerprint: strings.Repeat("a", 64), ExpectedSnapshotFingerprint: strings.Repeat("b", 64),
		ExpectedObjectiveBindingFingerprint: strings.Repeat("d", 64),
		Objective:                           model.Objective{ID: "objective", TargetID: model.ObjectiveApprovedPlan, DeliveryID: "delivery"},
		ObjectiveScope:                      catalog.ObjectiveScopeBoundExact,
		Authority:                           authority, AuthorityFingerprint: authorityFingerprint,
		RequiredCapabilities:  []catalog.Capability{catalog.CapabilityRepositoryWrite},
		GrantedCapabilities:   authority.GrantedCapabilities(now),
		EffectiveCapabilities: []catalog.Capability{catalog.CapabilityRepositoryWrite},
		IdempotencyKey:        "idempotency",
	}
	transition := catalog.Transition{ID: "program/write", Version: 1, Owner: "program", Effect: "program.write", TargetPredicate: "program.written", Verifier: "program.written", Policy: catalog.PolicyContract{ObjectiveScope: catalog.ObjectiveScopeBoundExact}}
	effects := []EffectFact{{Kind: EffectResourceMutation, EffectID: transition.Effect, Owner: transition.Owner, Resource: "program.state", Target: "/state", Operation: "update", PriorFingerprint: strings.Repeat("1", 64), ResultingFingerprint: strings.Repeat("2", 64)}}
	receipt, err := NewReceipt("flow", 1, ProgramIdentity{ID: "program", Version: "1.0.0", Fingerprint: admission.ExpectedProgramFingerprint}, admission, transition, model.Snapshot{Observation: model.Observation{StateRevision: 2}, Fingerprint: strings.Repeat("c", 64)}, []model.StateFacet{model.StateFacetControl}, effects, nil, now, now.Add(time.Second))
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
