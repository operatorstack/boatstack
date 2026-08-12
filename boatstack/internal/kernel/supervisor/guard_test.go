package supervisor

import (
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

func TestManagedCommandRoutingComesOnlyFromCompiledProgram(t *testing.T) {
	registry, err := catalog.New([]catalog.Transition{
		syntheticManagedTransition("synthetic.publish", catalog.EventOwnedLocal, catalog.SelectionExplicitOnly, "synthetic.recover"),
		syntheticManagedTransition("synthetic.recover", catalog.EventRecovery, catalog.SelectionProgramRecovery, "synthetic.recover"),
	})
	if err != nil {
		t.Fatal(err)
	}
	active := model.Snapshot{
		Fingerprint: "snapshot",
		Observation: model.Observation{
			Phase:      model.Fact[model.ProtocolPhase]{Status: model.FactKnown, Value: model.PhaseActive},
			Engagement: model.Fact[model.EngagementState]{Status: model.FactKnown, Value: model.EngagementActive},
		},
	}
	supervisor := New(registry, catalog.GoalContracts{})
	managed := supervisor.Guard(active, CommandIntent{Class: IntentManagedBypass, Operation: "artifact.publish", Fingerprint: "command"})
	if managed.Allowed || managed.RequiredTransition != "synthetic.publish" || managed.Intent.Transition != "synthetic.publish" {
		t.Fatalf("compiled managed operation was not routed through admission: %#v", managed)
	}
	unowned := supervisor.Guard(active, CommandIntent{Class: IntentManagedBypass, Operation: "publication.create", Fingerprint: "command"})
	if !unowned.Allowed || unowned.RequiredTransition != "" || unowned.Intent.Class != IntentOrdinary {
		t.Fatalf("custom program inherited StandardFlow command semantics: %#v", unowned)
	}
}

func syntheticManagedTransition(id catalog.TransitionID, class catalog.EventClass, selection catalog.SelectionClass, recovery catalog.TransitionID) catalog.Transition {
	policy := catalog.PolicyContract{}
	if id == "synthetic.publish" {
		policy.ManagedOperations = []string{"artifact.publish"}
	}
	return catalog.Transition{
		ID: id, Version: 1,
		Origin: catalog.TransitionOrigin{Kind: catalog.OriginControlProgram, ID: "test.synthetic", Version: "1.0.0", ManifestFingerprint: "manifest"},
		Owner:  "test.synthetic", SelectionClass: selection, Class: class,
		SourcePhases: []model.ProtocolPhase{model.PhaseActive}, TargetPhases: []model.ProtocolPhase{model.PhaseActive},
		GoalKinds: []model.GoalKind{model.GoalVerified}, RequiredIdentity: []string{"repository-id"},
		Authority: []catalog.AuthorityClass{catalog.AuthorityRepository}, RequiredCapabilities: []catalog.Capability{catalog.CapabilityRepositoryWrite},
		DeclaredCapabilities: []catalog.Capability{catalog.CapabilityRepositoryWrite}, RequiredEvidence: []string{"snapshot"},
		OwnedResources: []string{"test.synthetic.state"}, Effect: catalog.EffectID(id), LocalEffects: []catalog.EffectID{catalog.EffectID(id)}, Idempotent: true,
		Prescription:    catalog.Prescription{Operation: string(id), ExpectedPostcondition: "synthetic-target"},
		SourcePredicate: "synthetic-source", SourceConditions: []catalog.FacetCondition{{Facet: model.FacetProgram, Statuses: []model.FactStatus{model.FactKnown}}},
		AdmissionPredicate: "exact-admission", TargetPredicate: "synthetic-target",
		TargetConditions: []catalog.FacetCondition{{Facet: model.FacetProgram, Statuses: []model.FactStatus{model.FactKnown}}}, Verifier: "synthetic-verifier",
		Interruption: catalog.InterruptionContract{
			Points: []string{"after-effect"}, PartialState: []string{"state"}, Detection: "fresh-observation", ResumeContract: "resume",
			RollbackContract: "rollback", CompensationContract: "none", Recovery: recovery, RecoveryAuthority: "repository-policy", ResumptionPredicate: "fresh-state",
		},
		Reversibility: catalog.Reversible, TerminalEffect: "none", PrivacyClassification: "metadata-only", TelemetryClassification: "receipt", CostClass: "synthetic",
		Policy: policy, Priority: 1,
	}
}
