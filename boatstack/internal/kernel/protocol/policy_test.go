package protocol

import (
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/testprogram"
)

func policySnapshot(policy model.ConfigurationPolicy) model.Snapshot {
	return model.Snapshot{Observation: model.Observation{
		Invocation:          model.InvocationContext{Host: "cli"},
		ConfigurationPolicy: model.Fact[model.ConfigurationPolicy]{Status: model.FactKnown, Value: policy},
	}}
}

func TestProviderAuthorityBindsExactExternalRequest(t *testing.T) {
	now := time.Now().UTC()
	authority := AuthorityBundle{Receipts: []AuthorityReceipt{{
		ID: "provider", Class: catalog.AuthorityProvider, Subject: "github",
		Fingerprint: "different-preview", IssuedAt: now, ExpiresAt: now.Add(time.Minute),
	}}}
	transition, _ := testprogram.StandardRegistry().Lookup("publication.execute")
	parameters := Parameters{{Name: "preview_fingerprint", Value: "reviewed-preview"}}
	if err := validateProviderAuthorityBinding(authority, transition, parameters); err == nil {
		t.Fatal("provider authority for different preview was accepted")
	}
	authority.Receipts[0].Fingerprint = "reviewed-preview"
	if err := validateProviderAuthorityBinding(authority, transition, parameters); err != nil {
		t.Fatalf("exact provider authority rejected: %v", err)
	}
}

func TestAdmissionPolicyRejectsRepositoryOnlyHighRiskReview(t *testing.T) {
	snapshot := policySnapshot(model.ConfigurationPolicy{
		PlanApproval: "human", IndependentReviewForHighRisk: true, HighRiskChange: true,
		VisualEvidence: "optional", ExternalEffectAuthority: "human-or-autonomy-plus-provider", Hosts: []string{"cli"},
	})
	transition, _ := testprogram.StandardRegistry().Lookup("gate.review.record")
	if err := validatePolicyAuthority(snapshot, transition, catalog.AuthoritySet{catalog.AuthorityRepository: true}); err == nil {
		t.Fatal("repository-only authority admitted a high-risk review")
	}
	if err := validatePolicyAuthority(snapshot, transition, catalog.AuthoritySet{catalog.AuthorityHuman: true}); err != nil {
		t.Fatalf("human high-risk review authority rejected: %v", err)
	}
}

func TestAdmissionPolicyRejectsDisabledVisualEvidence(t *testing.T) {
	snapshot := policySnapshot(model.ConfigurationPolicy{
		PlanApproval: "human", VisualEvidence: "off",
		ExternalEffectAuthority: "human-or-autonomy-plus-provider", Hosts: []string{"cli"},
	})
	transition, _ := testprogram.StandardRegistry().Lookup("evidence.visual.attach")
	if err := validatePolicyAuthority(snapshot, transition, catalog.AuthoritySet{catalog.AuthorityHuman: true}); err == nil {
		t.Fatal("visual evidence attachment admitted while disabled")
	}
}

func TestAdmissionRejectsRecoveryOutsideExactJournalContract(t *testing.T) {
	snapshot := policySnapshot(model.ConfigurationPolicy{
		PlanApproval: "human", VisualEvidence: "optional",
		ExternalEffectAuthority: "human-or-autonomy-plus-provider", Hosts: []string{"cli"},
	})
	snapshot.RecoveryInfo = model.Fact[model.RecoveryContext]{Status: model.FactKnown, Value: model.RecoveryContext{
		TransactionID: "adm-interrupted", Cause: "interrupted", SourcePhase: model.PhaseActive,
		Permitted: []string{"recovery.resume"}, BudgetRemaining: 2, Resumption: model.PhaseActive,
	}}
	if err := validateRecoveryPermission(snapshot, catalog.Transition{ID: "recovery.rollback", Class: catalog.EventRecovery}); err == nil {
		t.Fatal("recovery transition outside journal contract was admitted")
	}
	if err := validateRecoveryPermission(snapshot, catalog.Transition{ID: "recovery.resume", Class: catalog.EventRecovery}); err != nil {
		t.Fatalf("permitted recovery transition rejected: %v", err)
	}
}
