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

func TestLowLevelSDKRequiresAndAcceptsExactlyOneNonStandardPrimaryFlow(t *testing.T) {
	// control-law: low-level-sdk-never-inserts-or-multiplies-standard-flow
	if _, err := sdk.NewKernel(""); err == nil {
		t.Fatal("low-level SDK accepted a missing PrimaryFlow")
	}
	flow := syntheticFlow{}
	if _, err := sdk.NewKernel("", sdk.WithFlow(flow)); err != nil {
		t.Fatalf("synthetic PrimaryFlow was rejected: %v", err)
	}
	if _, err := sdk.NewKernel("", sdk.WithFlow(flow), sdk.WithFlow(flow)); err == nil {
		t.Fatal("low-level SDK accepted two PrimaryFlows")
	}
	if _, err := sdk.New("", sdk.WithFlow(flow)); err == nil {
		t.Fatal("standard SDK allowed StandardFlow replacement")
	}
}

type syntheticFlow struct{}

func (syntheticFlow) FlowRuntime() control.FlowRuntime { return syntheticRuntime{} }

func (syntheticFlow) FlowManifest(context.Context) (control.PrimaryFlowManifest, error) {
	const (
		id       = "synthetic.lifecycle"
		fact     = "synthetic.lifecycle.stage"
		resource = "synthetic.lifecycle.state"
	)
	transition := func(id control.TransitionID, source, target string, priority int) control.Transition {
		effect := control.EffectID(string(id) + "-effect")
		verifier := string(id) + "-verifier"
		return control.Transition{
			ID: id, Version: 1, SelectionClass: control.SelectionFlowProgress, Class: control.EventOwnedLocal,
			SourcePhases: []control.ProtocolPhase{control.PhaseObserved, control.PhaseActive}, TargetPhases: []control.ProtocolPhase{control.PhaseObserved, control.PhaseActive},
			GoalKinds: []control.GoalKind{control.GoalVerified}, RequiredIdentity: []string{"repository-id", "git-common-id", "worktree-id"},
			Authority: []control.AuthorityClass{control.AuthorityRepository}, RequiredEvidence: []string{"snapshot", "goal", "facet:" + fact},
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
	return control.PrimaryFlowManifest{
		ID: id, Version: "1.0.0", ProtocolVersion: control.FlowProtocolVersion, RuntimeMode: control.FlowRuntimeProtocol,
		SupportedGoals: []control.GoalKind{control.GoalVerified},
		GoalContracts:  []control.GoalContract{{GoalKind: control.GoalVerified, Conditions: []control.FacetCondition{control.KnownCondition(control.FacetName(fact), "terminal")}}},
		Transitions:    []control.Transition{verify, finish}, Facts: []string{fact}, OwnedResources: []string{resource},
		Effects: []string{string(verify.Effect), string(finish.Effect)}, Verifiers: []string{verify.Verifier, finish.Verifier},
		ConfigurationSchema:   json.RawMessage(`{"type":"object"}`),
		PrivacyClassification: "metadata-only", TelemetryClassification: "transition-receipt",
	}, nil
}

type syntheticRuntime struct{}

func (syntheticRuntime) InvokeFlow(_ context.Context, request control.FlowRequest) (control.FlowResponse, error) {
	return control.FlowResponse{
		ProtocolVersion: control.FlowProtocolVersion, Operation: request.Operation,
		FlowID: request.FlowID, FlowVersion: request.FlowVersion, CorrelationID: request.CorrelationID,
	}, nil
}
