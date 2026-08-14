package surfaces

import (
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/supervisor"
)

func TestSurfaceSchemaIsFlagDayAndApplyRequiresPrescription(t *testing.T) {
	// control-law: every mutating surface reaches the exact prescription boundary
	base := Request{
		SchemaVersion: SchemaVersion,
		Operation:     OperationResolve,
		Repository:    "/repository",
		Host:          "cli",
		CorrelationID: "correlation",
	}
	old := base
	old.SchemaVersion--
	if err := old.Validate(time.Now()); err == nil {
		t.Fatal("older surface schema was accepted")
	}
	apply := base
	apply.Operation = OperationApply
	apply.FlowID = "flow"
	apply.TransitionID = "engagement.begin"
	if err := apply.Validate(time.Now()); err == nil {
		t.Fatal("apply without an exact prescription was accepted")
	}
}

func TestExplainRequestRejectsMutationArtifacts(t *testing.T) {
	now := time.Now().UTC()
	request := Request{SchemaVersion: SchemaVersion, Operation: OperationExplain, Repository: "/repo", Host: "cli", CorrelationID: "explain"}
	if err := request.Validate(now); err != nil {
		t.Fatal(err)
	}
	request.IdempotencyKey = "idem-forbidden"
	if err := request.Validate(now); err == nil {
		t.Fatal("explain accepted an apply idempotency key")
	}
}

func TestQuestionSuspendsAndBindsOneRunSnapshot(t *testing.T) {
	// control-law: human-input-suspension-cannot-cross-run-or-snapshot
	transition := catalog.Transition{
		ID: "plan.approve", Parameters: []catalog.ParameterSpec{{Name: "plan_fingerprint", Required: true}},
		Authority: []catalog.AuthorityClass{catalog.AuthorityHuman}, Prescription: catalog.Prescription{AuthorityPrompt: "Approve exact plan bytes"},
	}
	decision := supervisor.Decision{Kind: supervisor.DecisionCandidate, Transition: &transition}
	one := QuestionFor("run-one", "snapshot-one", decision)
	two := QuestionFor("run-one", "snapshot-one", decision)
	changed := QuestionFor("run-one", "snapshot-two", decision)
	if one == nil || two == nil || changed == nil || one.ID != two.ID || one.ID == changed.ID || one.RunID != "run-one" {
		t.Fatalf("question bindings = %#v %#v %#v", one, two, changed)
	}
}
