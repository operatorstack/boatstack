package sdk_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/control"
	"github.com/operatorstack/boatstack/boatstack/sdk"
)

func TestPublicProtocolCanBeConstructedWithoutInternalPackages(t *testing.T) {
	request := sdk.Request{
		SchemaVersion: sdk.SchemaVersion,
		Operation:     sdk.OperationResolve,
		Repository:    t.TempDir(),
		Host:          "mcp",
		CorrelationID: "correlation",
		Goal:          sdk.Goal{ID: "goal", Kind: sdk.GoalVerified, DeliveryID: "delivery"},
	}
	if request.Goal.Kind != sdk.GoalVerified || request.Operation != sdk.OperationResolve {
		t.Fatalf("public V2 aliases lost protocol identity: %#v", request)
	}
}

func TestSDKPreservesCapabilityAdmissionProtocol(t *testing.T) {
	// control-law: SDK and CLI consume the same versioned prescription fields
	raw := []byte(`{"schema_version":4,"operation":"apply","repository":"/repo","host":"sdk","correlation_id":"correlation","flow_id":"flow","transition_id":"program/write","prescription":{"schema_version":2,"id":"prx-test","transition_id":"program/write","expected_state_revision":7,"expected_program_fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expected_snapshot_fingerprint":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","authority_fingerprint":"auth-test","required_capabilities":["repository.write"],"effective_capabilities":["repository.write"]}}`)
	var request sdk.Request
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	if request.Prescription.AuthorityFingerprint != "auth-test" || len(request.Prescription.RequiredCapabilities) != 1 || request.Prescription.RequiredCapabilities[0] != sdk.CapabilityRepositoryWrite {
		t.Fatalf("SDK lost capability admission: %#v", request.Prescription)
	}
}

func TestLowLevelSDKRequiresAndAcceptsExactlyOneNonStandardProgramRuntime(t *testing.T) {
	// control-law: low-level-sdk-never-inserts-or-multiplies-standard-flow
	if _, err := sdk.NewKernel(""); err == nil {
		t.Fatal("low-level SDK accepted a missing ProgramRuntime")
	}
	flow := syntheticFlow{}
	if _, err := sdk.NewKernel("", sdk.WithProgramRuntime(flow)); err != nil {
		t.Fatalf("synthetic ProgramRuntime was rejected: %v", err)
	}
	if _, err := sdk.NewKernel("", sdk.WithProgramRuntime(flow), sdk.WithProgramRuntime(flow)); err == nil {
		t.Fatal("low-level SDK accepted two ProgramRuntimes")
	}
	if _, err := sdk.New("", sdk.WithProgramRuntime(flow)); err == nil {
		t.Fatal("standard SDK allowed StandardFlow replacement")
	}
}

type syntheticFlow struct{}

func (syntheticFlow) ProgramRuntime() control.ProgramRuntime { return syntheticRuntime{} }

func (syntheticFlow) RuntimeManifest(context.Context) (control.ProgramRuntimeManifest, error) {
	const (
		id       = "synthetic.lifecycle"
		fact     = "synthetic.lifecycle.stage"
		resource = "synthetic.lifecycle.state"
	)
	transition := func(id control.TransitionID, source, target string, priority int) control.Transition {
		effect := control.EffectID(string(id) + "-effect")
		verifier := string(id) + "-verifier"
		return control.Transition{
			ID: id, Version: 1, SelectionClass: control.SelectionProgramProgress, Class: control.EventOwnedLocal,
			SourcePhases: []control.ProtocolPhase{control.PhaseObserved, control.PhaseActive}, TargetPhases: []control.ProtocolPhase{control.PhaseObserved, control.PhaseActive},
			GoalKinds: []control.GoalKind{control.GoalVerified}, RequiredIdentity: []string{"repository-id", "git-common-id", "worktree-id"},
			Authority: []control.AuthorityClass{control.AuthorityRepository}, RequiredCapabilities: []control.Capability{control.CapabilityRepositoryWrite, control.CapabilityCommandExecute}, RequiredEvidence: []string{"snapshot", "goal", "facet:" + fact},
			OwnedResources: []string{resource}, Effect: effect, LocalEffects: []control.EffectID{effect}, Idempotent: true,
			Prescription:    control.Prescription{Operation: string(id), ExpectedPostcondition: target},
			SourcePredicate: "synthetic-source", AdmissionPredicate: "exact-admission", TargetPredicate: "synthetic-target",
			SourceConditions: []control.FacetCondition{control.KnownCondition(control.FacetName(fact), source)},
			TargetConditions: []control.FacetCondition{control.KnownCondition(control.FacetName(fact), target)}, Verifier: verifier,
			Interruption: control.InterruptionContract{
				Points: []string{"after-effect"}, PartialState: []string{"namespaced-flow-state"}, Detection: "fresh-flow-observation",
				ResumeContract: "re-observe", RollbackContract: "restore-prior-bytes", CompensationContract: "not-required",
				Recovery: "recovery.escalate", RecoveryAuthority: "repository-policy", ResumptionPredicate: "fresh-flow-fact",
			},
			Reversibility: control.Reversible, TerminalEffect: "compiled-goal-contract", PrivacyClassification: "metadata-only",
			TelemetryClassification: "transition-receipt", CostClass: "synthetic", Priority: priority,
		}
	}
	verify := transition("synthetic.lifecycle.verify", "start", "verify", 1)
	finish := transition("synthetic.lifecycle.finish", "verify", "terminal", 2)
	return control.ProgramRuntimeManifest{
		ID: id, Version: "1.0.0", ProtocolVersion: control.ProgramRuntimeProtocolVersion, RuntimeMode: control.ProgramRuntimeProtocol,
		SupportedGoals: []control.GoalKind{control.GoalVerified},
		GoalContracts:  []control.GoalContract{{GoalKind: control.GoalVerified, Conditions: []control.FacetCondition{control.KnownCondition(control.FacetName(fact), "terminal")}}},
		Transitions:    []control.Transition{verify, finish}, Facts: []string{fact}, OwnedResources: []string{resource},
		Effects: []string{string(verify.Effect), string(finish.Effect)}, Verifiers: []string{verify.Verifier, finish.Verifier},
		Capabilities:          []control.Capability{control.CapabilityRepositoryWrite, control.CapabilityCommandExecute},
		ConfigurationSchema:   json.RawMessage(`{"type":"object"}`),
		PrivacyClassification: "metadata-only", TelemetryClassification: "transition-receipt",
	}, nil
}

type syntheticRuntime struct{}

func (syntheticRuntime) InvokeProgram(_ context.Context, request control.ProgramRuntimeRequest) (control.ProgramRuntimeResponse, error) {
	return control.ProgramRuntimeResponse{
		ProtocolVersion: control.ProgramRuntimeProtocolVersion, Operation: request.Operation,
		ProgramID: request.ProgramID, ProgramVersion: request.ProgramVersion, CorrelationID: request.CorrelationID,
	}, nil
}
