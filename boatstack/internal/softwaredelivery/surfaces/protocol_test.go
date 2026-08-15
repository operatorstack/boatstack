package surfaces

import (
	"encoding/json"
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

func TestForegroundWorkSurfaceRejectsAmbiguousMutationPayloads(t *testing.T) {
	// control-law: each foreground-work mutation crosses one typed operation boundary
	base := Request{
		SchemaVersion: SchemaVersion, Repository: "/repository", Host: "cli", CorrelationID: "work",
		ProgramID: "incident-response", ProgramFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		EntryID: "respond", FlowID: "run-1", WorkID: "diagnose",
	}
	valid := []Request{
		func() Request { value := base; value.Operation = OperationWorkShow; return value }(),
		func() Request { value := base; value.Operation = OperationWorkComplete; return value }(),
		func() Request {
			value := base
			value.Operation = OperationWorkInputRequired
			value.WorkQuestionPrompt = "Which service?"
			value.WorkQuestionSchema = []byte(`{"type":"string"}`)
			return value
		}(),
		func() Request {
			value := base
			value.Operation = OperationWorkAnswer
			value.WorkQuestionID = "question-1"
			value.WorkAnswer = json.RawMessage(`"api"`)
			return value
		}(),
		func() Request {
			value := base
			value.Operation = OperationWorkBlock
			value.WorkBlockReason = "input unavailable"
			return value
		}(),
	}
	for _, request := range valid {
		if err := request.Validate(time.Now()); err != nil {
			t.Fatalf("valid %s request: %v", request.Operation, err)
		}
	}
	invalid := []Request{valid[0], valid[1], valid[2], valid[3], valid[4]}
	invalid[0].WorkBlockReason = "hidden mutation"
	invalid[1].WorkAnswer = []byte(`true`)
	invalid[2].WorkQuestionPrompt = ""
	invalid[3].WorkQuestionID = ""
	invalid[4].WorkQuestionID = "question-1"
	for _, request := range invalid {
		if err := request.Validate(time.Now()); err == nil {
			t.Fatalf("ambiguous %s request was accepted", request.Operation)
		}
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
