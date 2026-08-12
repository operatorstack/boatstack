package protocol

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

func committedReceiptFixture(t *testing.T) (TransitionReceipt, Admission, catalog.Transition, model.Snapshot, time.Time) {
	t.Helper()
	now := time.Unix(200, 0).UTC()
	authority := capabilityAuthority(now, catalog.AuthorityRepository, "policy")
	authorityFingerprint, err := authority.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	admission := Admission{
		ID: "adm-fixture", PrescriptionID: "prx-fixture", ExpectedStateRevision: 41,
		ExpectedProgramFingerprint: strings.Repeat("a", 64), ExpectedSnapshotFingerprint: strings.Repeat("b", 64),
		ExpectedObjectiveBindingFingerprint: strings.Repeat("d", 64),
		Objective:                           model.Objective{ID: "objective", Kind: model.ObjectiveApprovedPlan, DeliveryID: "delivery"},
		ObjectiveScope:                      catalog.ObjectiveScopeBoundExact,
		Authority:                           authority, AuthorityFingerprint: authorityFingerprint,
		RequiredCapabilities: []catalog.Capability{catalog.CapabilityRepositoryWrite}, GrantedCapabilities: authority.GrantedCapabilities(now), EffectiveCapabilities: []catalog.Capability{catalog.CapabilityRepositoryWrite},
		IdempotencyKey: "idem-fixture",
	}
	transition := catalog.Transition{
		ID: "product-delivery/build.begin", Version: 3, Owner: "product-delivery", Effect: "build.begin",
		TargetPredicate: "build is active", Verifier: "product-delivery.build-active",
		Policy: catalog.PolicyContract{ObjectiveScope: catalog.ObjectiveScopeBoundExact},
	}
	target := model.Snapshot{Observation: model.Observation{StateRevision: 42}, Fingerprint: strings.Repeat("c", 64)}
	effects := []EffectFact{
		{Kind: EffectResourceMutation, EffectID: transition.Effect, Owner: transition.Owner, Resource: "product-delivery.state", Target: "/repo/.boatstack/state.json", Operation: "update", PriorFingerprint: strings.Repeat("1", 64), ResultingFingerprint: strings.Repeat("2", 64)},
		{Kind: EffectResourceMutation, EffectID: transition.Effect, Owner: transition.Owner, Resource: "product-delivery.evidence", Target: "/repo/.boatstack/build.json", Operation: "create", PriorFingerprint: strings.Repeat("3", 64), ResultingFingerprint: strings.Repeat("4", 64)},
	}
	receipt, err := NewReceipt("flow", 7, ProgramIdentity{ID: "product-delivery", Version: "2.1.0", Fingerprint: admission.ExpectedProgramFingerprint}, admission, transition, target, []model.StateFacet{model.StateFacetControl, model.StateFacetProduct}, effects, nil, now, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	return receipt, admission, transition, target, now
}

func rehashReceipt(t *testing.T, receipt TransitionReceipt) TransitionReceipt {
	t.Helper()
	receipt.ID = ""
	id, err := contentID("trc-", receipt)
	if err != nil {
		t.Fatal(err)
	}
	receipt.ID = id
	return receipt
}

func TestCommittedTransitionFactBindsProgramTransitionStateAuthorityEffectsAndVerification(t *testing.T) {
	receipt, admission, transition, target, _ := committedReceiptFixture(t)
	if err := receipt.Validate(); err != nil {
		t.Fatal(err)
	}
	if receipt.Kind != TransitionCommitted || receipt.Program.ID != "product-delivery" || receipt.Program.Version != "2.1.0" || receipt.Program.Fingerprint != admission.ExpectedProgramFingerprint {
		t.Fatalf("program fact = %#v", receipt.Program)
	}
	if receipt.TransitionID != transition.ID || receipt.PriorStateRevision != 41 || receipt.ResultingStateRevision != 42 {
		t.Fatalf("transition/revision fact = %#v", receipt)
	}
	if receipt.AuthorityFingerprint != admission.AuthorityFingerprint || len(receipt.AuthoritySources) != 1 || len(receipt.RequiredCapabilities) != 1 || len(receipt.GrantedCapabilities) == 0 {
		t.Fatalf("authority fact = %#v", receipt)
	}
	if len(receipt.ExercisedCapabilities) != 0 {
		t.Fatalf("receipt fabricated capability exercise: %#v", receipt.ExercisedCapabilities)
	}
	if len(receipt.CommittedEffects) != 2 || receipt.CommittedEffects[0].Target > receipt.CommittedEffects[1].Target {
		t.Fatalf("committed effects are absent or non-canonical: %#v", receipt.CommittedEffects)
	}
	if !slices.Equal(receipt.ChangedStateFacets, []model.StateFacet{model.StateFacetControl, model.StateFacetProduct}) {
		t.Fatalf("changed state facets = %v", receipt.ChangedStateFacets)
	}
	if receipt.Verification.Result != VerificationSatisfied || receipt.Verification.ExpectedPostcondition != transition.TargetPredicate || receipt.Verification.EvidenceFingerprint != target.Fingerprint {
		t.Fatalf("verification fact = %#v", receipt.Verification)
	}
}

func TestCommittedTransitionFactRejectsNonSuccessSemantics(t *testing.T) {
	receipt, _, _, _, _ := committedReceiptFixture(t)
	receipt.Kind = "transition-refused"
	receipt = rehashReceipt(t, receipt)
	if err := receipt.Validate(); err == nil {
		t.Fatal("refusal was accepted as a successful transition fact")
	}
}

func TestCommittedTransitionFactRejectsProgramMismatchAtConstruction(t *testing.T) {
	_, admission, transition, target, now := committedReceiptFixture(t)
	effect := []EffectFact{{Kind: EffectResourceMutation, EffectID: transition.Effect, Owner: transition.Owner, Resource: "state", Target: "/state", Operation: "update", PriorFingerprint: strings.Repeat("1", 64), ResultingFingerprint: strings.Repeat("2", 64)}}
	_, err := NewReceipt("flow", 1, ProgramIdentity{ID: "other", Version: "1", Fingerprint: strings.Repeat("b", 64)}, admission, transition, target, []model.StateFacet{model.StateFacetControl}, effect, nil, now, now)
	if err == nil || !strings.Contains(err.Error(), "differs from admitted program") {
		t.Fatalf("program mismatch error = %v", err)
	}
}

func TestCommittedTransitionFactRejectsWrongRevision(t *testing.T) {
	_, admission, transition, target, now := committedReceiptFixture(t)
	target.StateRevision = 43
	effect := []EffectFact{{Kind: EffectResourceMutation, EffectID: transition.Effect, Owner: transition.Owner, Resource: "state", Target: "/state", Operation: "update", PriorFingerprint: strings.Repeat("1", 64), ResultingFingerprint: strings.Repeat("2", 64)}}
	_, err := NewReceipt("flow", 1, ProgramIdentity{ID: "product-delivery", Version: "2.1.0", Fingerprint: admission.ExpectedProgramFingerprint}, admission, transition, target, []model.StateFacet{model.StateFacetControl}, effect, nil, now, now)
	if err == nil || !strings.Contains(err.Error(), "advance exactly once") {
		t.Fatalf("revision mismatch error = %v", err)
	}
}

func TestCommittedTransitionFactRejectsExercisedCapabilityOutsideAdmission(t *testing.T) {
	receipt, _, _, _, _ := committedReceiptFixture(t)
	receipt.ExercisedCapabilities = []catalog.Capability{catalog.CapabilityPublicationPublish}
	receipt = rehashReceipt(t, receipt)
	if err := receipt.Validate(); err == nil || !strings.Contains(err.Error(), "outside exact admission") {
		t.Fatalf("unadmitted exercise error = %v", err)
	}
}

func TestCommittedTransitionFactRejectsRequiredButUngrantedCapability(t *testing.T) {
	receipt, _, _, _, _ := committedReceiptFixture(t)
	receipt.RequiredCapabilities = []catalog.Capability{catalog.CapabilityPublicationPublish}
	receipt = rehashReceipt(t, receipt)
	if err := receipt.Validate(); err == nil || !strings.Contains(err.Error(), "was not admitted") {
		t.Fatalf("ungranted declaration error = %v", err)
	}
}

func TestCommittedTransitionFactRejectsFailedOrMismatchedVerification(t *testing.T) {
	receipt, _, _, _, _ := committedReceiptFixture(t)
	for _, mutate := range []func(*TransitionReceipt){
		func(value *TransitionReceipt) { value.Verification.Result = "failed" },
		func(value *TransitionReceipt) { value.Verification.EvidenceFingerprint = strings.Repeat("d", 64) },
	} {
		candidate := receipt
		mutate(&candidate)
		candidate = rehashReceipt(t, candidate)
		if err := candidate.Validate(); err == nil {
			t.Fatalf("invalid verification accepted: %#v", candidate.Verification)
		}
	}
}

func TestCommittedTransitionFactRejectsPlannedOrDuplicateEffectEvidence(t *testing.T) {
	receipt, _, _, _, _ := committedReceiptFixture(t)
	for _, mutate := range []func(*TransitionReceipt){
		func(value *TransitionReceipt) { value.CommittedEffects = nil },
		func(value *TransitionReceipt) {
			value.CommittedEffects = append(value.CommittedEffects, value.CommittedEffects[0])
		},
		func(value *TransitionReceipt) { value.CommittedEffects[0].PriorFingerprint = "planned" },
	} {
		candidate := receipt
		candidate.CommittedEffects = append([]EffectFact(nil), receipt.CommittedEffects...)
		mutate(&candidate)
		candidate = rehashReceipt(t, candidate)
		if err := candidate.Validate(); err == nil {
			t.Fatalf("invalid committed effect evidence accepted: %#v", candidate.CommittedEffects)
		}
	}
}

func TestCommittedTransitionFactRejectsMissingOrNonCanonicalStateFacets(t *testing.T) {
	receipt, _, _, _, _ := committedReceiptFixture(t)
	for _, facets := range [][]model.StateFacet{
		nil,
		{model.StateFacetProduct, model.StateFacetControl},
		{model.StateFacetControl, model.StateFacetControl},
	} {
		candidate := receipt
		candidate.ChangedStateFacets = facets
		candidate = rehashReceipt(t, candidate)
		if err := candidate.Validate(); err == nil {
			t.Fatalf("invalid changed state facets accepted: %v", facets)
		}
	}
}

func TestHistoricalTransitionFactRetainsOriginalProgramIdentity(t *testing.T) {
	receipt, _, _, _, _ := committedReceiptFixture(t)
	upgraded := ProgramIdentity{ID: receipt.Program.ID, Version: "3.0.0", Fingerprint: strings.Repeat("b", 64)}
	if upgraded == receipt.Program || receipt.Program.Version != "2.1.0" || receipt.Program.Fingerprint != strings.Repeat("a", 64) {
		t.Fatalf("historical identity was reinterpreted: receipt=%#v current=%#v", receipt.Program, upgraded)
	}
	if err := receipt.Validate(); err != nil {
		t.Fatalf("program upgrade invalidated historical fact: %v", err)
	}
}

func TestMateriallyDifferentCommitsHaveDifferentReceiptIdentity(t *testing.T) {
	first, _, _, _, _ := committedReceiptFixture(t)
	second := first
	second.Sequence++
	second.ResultingStateRevision++
	second.PriorStateRevision++
	second = rehashReceipt(t, second)
	if first.ID == second.ID {
		t.Fatal("distinct commits shared one receipt identity")
	}
}
