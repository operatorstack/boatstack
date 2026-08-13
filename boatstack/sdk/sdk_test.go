package sdk_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/delivery"
	"github.com/operatorstack/boatstack/boatstack/sdk"
)

func TestPublicProtocolCanBeConstructedWithoutInternalPackages(t *testing.T) {
	request := sdk.Request{
		SchemaVersion: sdk.SchemaVersion,
		Operation:     sdk.OperationResolve,
		Repository:    t.TempDir(),
		Host:          "mcp",
		CorrelationID: "correlation",
		Objective:     sdk.Objective{ID: "objective", TargetID: sdk.ObjectiveVerified, DeliveryID: "delivery"},
	}
	if request.Objective.TargetID != sdk.ObjectiveVerified || request.Operation != sdk.OperationResolve {
		t.Fatalf("public V2 aliases lost protocol identity: %#v", request)
	}
}

func TestSDKPreservesCapabilityAdmissionProtocol(t *testing.T) {
	// control-law: SDK and CLI consume the same versioned prescription fields
	raw := []byte(`{"schema_version":5,"operation":"apply","repository":"/repo","host":"sdk","correlation_id":"correlation","flow_id":"flow","transition_id":"program/write","prescription":{"schema_version":3,"id":"prx-test","transition_id":"program/write","expected_state_revision":7,"expected_program_fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expected_snapshot_fingerprint":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","expected_objective_binding_fingerprint":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","authority_fingerprint":"auth-test","required_capabilities":["repository.write"],"effective_capabilities":["repository.write"]}}`)
	var request sdk.Request
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	if request.Prescription.AuthorityFingerprint != "auth-test" || len(request.Prescription.RequiredCapabilities) != 1 || request.Prescription.RequiredCapabilities[0] != sdk.CapabilityRepositoryWrite {
		t.Fatalf("SDK lost capability admission: %#v", request.Prescription)
	}
}

func TestSDKSerializesTheSameDurableTransitionFactAsTheSurface(t *testing.T) {
	raw := []byte(`{"schema_version":5,"operation":"apply","receipt":{"schema_version":8,"kind":"transition-committed","id":"trc-fact","flow_id":"flow","sequence":1,"program":{"id":"product-delivery","version":"1.0.0","fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"transition_id":"product-delivery/build.begin","transition_version":1,"prescription_id":"prx","admission_id":"adm","prior_state_revision":41,"resulting_state_revision":42,"objective_id":"objective","target_id":"verified","delivery_id":"delivery","objective_scope":"bound-exact","objective_binding_fingerprint":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","source_fingerprint":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","target_fingerprint":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","authority_fingerprint":"auth","authority_sources":[],"required_capabilities":["repository.write"],"granted_capabilities":["repository.write"],"committed_effects":[{"kind":"resource-mutation","effect_id":"build.begin","owner":"product-delivery","resource":"state","target":"/state","operation":"update","prior_fingerprint":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","resulting_fingerprint":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"}],"changed_state_facets":["control","product"],"verification":{"verifier":"build-active","expected_postcondition":"active","result":"satisfied","evidence_fingerprint":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","verified_at":"2026-08-12T00:00:00Z"},"idempotency_key":"idem","terminal":"nonterminal","started_at":"2026-08-12T00:00:00Z","committed_at":"2026-08-12T00:00:01Z","duration_nanoseconds":1000000000}}`)
	var response sdk.Response
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]any
	if err := json.Unmarshal(raw, &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before["receipt"], after["receipt"]) {
		t.Fatalf("SDK changed receipt facts:\nbefore=%#v\nafter=%#v", before["receipt"], after["receipt"])
	}
	if len(response.Receipt.ChangedStateFacets) != 2 || response.Receipt.ChangedStateFacets[0] != sdk.StateFacetControl || response.Receipt.ChangedStateFacets[1] != sdk.StateFacetProduct {
		t.Fatalf("SDK lost durable state facet facts: %v", response.Receipt.ChangedStateFacets)
	}
}

func TestLowLevelSDKRequiresAndAcceptsExactlyOneNonStandardProgramRuntime(t *testing.T) {
	// control-law: low-level-sdk-never-inserts-or-multiplies-standard-flow
	if _, err := sdk.NewProgramClient(""); err == nil {
		t.Fatal("low-level SDK accepted a missing ProgramRuntime")
	}
	flow := syntheticFlow{}
	if _, err := sdk.NewProgramClient("", sdk.WithProgramRuntime(flow)); err != nil {
		t.Fatalf("synthetic ProgramRuntime was rejected: %v", err)
	}
	if _, err := sdk.NewProgramClient("", sdk.WithProgramRuntime(flow), sdk.WithProgramRuntime(flow)); err == nil {
		t.Fatal("low-level SDK accepted two ProgramRuntimes")
	}
	if _, err := sdk.New("", sdk.WithProgramRuntime(flow)); err == nil {
		t.Fatal("standard SDK allowed StandardFlow replacement")
	}
}

type syntheticFlow struct{}

func (syntheticFlow) ProgramRuntime() delivery.ProgramRuntime { return syntheticRuntime{} }

func (syntheticFlow) RuntimeManifest(context.Context) (delivery.ProgramRuntimeManifest, error) {
	const (
		id       = "synthetic.lifecycle"
		fact     = "synthetic.lifecycle.stage"
		resource = "synthetic.lifecycle.state"
	)
	transition := func(id delivery.TransitionID, source, target string, priority int) delivery.Transition {
		effect := delivery.EffectID(string(id) + "-effect")
		verifier := string(id) + "-verifier"
		return delivery.Transition{
			ID: id, Version: 1, SelectionClass: delivery.SelectionProgramProgress, Class: delivery.EventOwnedLocal,
			SourcePhases: []delivery.ProtocolPhase{delivery.PhaseObserved, delivery.PhaseActive}, TargetPhases: []delivery.ProtocolPhase{delivery.PhaseObserved, delivery.PhaseActive},
			TargetIDs: []delivery.TargetID{delivery.ObjectiveVerified}, RequiredIdentity: []string{"repository-id", "git-common-id", "worktree-id"},
			Authority: []delivery.AuthorityClass{delivery.AuthorityRepository}, RequiredCapabilities: []delivery.Capability{delivery.CapabilityRepositoryWrite, delivery.CapabilityCommandExecute}, RequiredEvidence: []string{"snapshot", "objective", "facet:" + fact},
			OwnedResources: []string{resource}, OwnedFacets: []delivery.StateFacet{delivery.StateFacetControl}, StateEffect: delivery.StateEffect{Kind: delivery.StateEffectAssignments},
			Effect: effect, LocalEffects: []delivery.EffectID{effect}, Idempotent: true,
			Prescription:    delivery.Prescription{Operation: string(id), ExpectedPostcondition: target},
			SourcePredicate: "synthetic-source", AdmissionPredicate: "exact-admission", TargetPredicate: "synthetic-target",
			SourceConditions: []delivery.FacetCondition{delivery.KnownCondition(delivery.FacetName(fact), source)},
			TargetConditions: []delivery.FacetCondition{delivery.KnownCondition(delivery.FacetName(fact), target)}, Verifier: verifier,
			Interruption: delivery.InterruptionContract{
				Points: []string{"after-effect"}, PartialState: []string{"namespaced-flow-state"}, Detection: "fresh-flow-observation",
				ResumeContract: "re-observe", RollbackContract: "restore-prior-bytes", CompensationContract: "not-required",
				Recovery: "recovery.escalate", RecoveryAuthority: "repository-policy", ResumptionPredicate: "fresh-flow-fact",
			},
			Reversibility: delivery.Reversible, TerminalEffect: "compiled-objective-contract", PrivacyClassification: "metadata-only",
			TelemetryClassification: "transition-receipt", CostClass: "synthetic", Policy: delivery.PolicyContract{ObjectiveScope: delivery.ObjectiveScopeBoundExact}, Priority: priority,
		}
	}
	verify := transition("synthetic.lifecycle.verify", "start", "verify", 1)
	finish := transition("synthetic.lifecycle.finish", "verify", "terminal", 2)
	return delivery.ProgramRuntimeManifest{
		ID: id, Version: "1.0.0", ProtocolVersion: delivery.ProgramRuntimeProtocolVersion, RuntimeMode: delivery.ProgramRuntimeProtocol,
		SupportedTargets:   []delivery.TargetID{delivery.ObjectiveVerified},
		ObjectiveContracts: []delivery.ObjectiveContract{{TargetID: delivery.ObjectiveVerified, Conditions: []delivery.FacetCondition{delivery.KnownCondition(delivery.FacetName(fact), "terminal")}}},
		Transitions:        []delivery.Transition{verify, finish}, Facts: []string{fact}, OwnedResources: []string{resource},
		Effects: []string{string(verify.Effect), string(finish.Effect)}, Verifiers: []string{verify.Verifier, finish.Verifier},
		Capabilities:          []delivery.Capability{delivery.CapabilityRepositoryWrite, delivery.CapabilityCommandExecute},
		ConfigurationSchema:   json.RawMessage(`{"type":"object"}`),
		PrivacyClassification: "metadata-only", TelemetryClassification: "transition-receipt",
	}, nil
}

type syntheticRuntime struct{}

func (syntheticRuntime) InvokeProgram(_ context.Context, request delivery.ProgramRuntimeRequest) (delivery.ProgramRuntimeResponse, error) {
	return delivery.ProgramRuntimeResponse{
		ProtocolVersion: delivery.ProgramRuntimeProtocolVersion, Operation: request.Operation,
		ProgramID: request.ProgramID, ProgramVersion: request.ProgramVersion, CorrelationID: request.CorrelationID,
	}, nil
}
