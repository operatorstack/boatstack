package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
)

func TestFlowInputAnswerResumesSameRunAndConflictsFailClosed(t *testing.T) {
	// control-law: a missing declared host value suspends and only an exact
	// runtime-owned receipt resumes the same invocation.
	repository := flowRepository(t)
	runFlowGit(t, repository, "init", "-q")
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery-one.md", []byte("plan"))
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "fixture")
	writeAdmittedFlowProgramState(t, repository, strings.Repeat("f", 64))

	base := commandOptions{repository: repository, programID: "product-delivery", entryID: "run", host: "codex", transitionID: "publication.observe"}
	suspended, err := bindFlowEntry(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if suspended.inputRequest == nil || suspended.inputRequest.Code != "TRANSITION_INPUT_REQUIRED" || suspended.invocationEvidence != nil {
		t.Fatalf("suspension = %#v", suspended.inputRequest)
	}
	answerPath := filepath.Join(t.TempDir(), "answer.json")
	if err := os.WriteFile(answerPath, []byte(`{"publication_id":"123"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	arguments := []string{
		"answer", "--repo", repository, "--flow", "product-delivery", "--entry", "run", "--run-id", suspended.runID,
		"--request-fingerprint", suspended.inputRequest.Fingerprint, "--answer", answerPath, "--human", "operator", "--host", "codex", "--format", "json",
	}
	if _, err := captureStdout(t, func() error { return runFlowInput(arguments) }); err != nil {
		t.Fatal(err)
	}

	resumed, err := bindFlowEntry(context.Background(), commandOptions{
		repository: repository, programID: "product-delivery", entryID: "run", host: "codex", transitionID: "publication.observe",
		runID: suspended.runID, deliveryID: suspended.deliveryID, targetID: suspended.targetID, objectiveID: suspended.objectiveID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.invocationEvidence == nil || resumed.inputRequest != nil || strings.Join(resumed.parameters, "") != "publication_id=123" {
		t.Fatalf("resumed invocation = evidence %#v request %#v parameters %#v", resumed.invocationEvidence, resumed.inputRequest, resumed.parameters)
	}

	if err := os.WriteFile(answerPath, []byte(`{"publication_id":"456"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := captureStdout(t, func() error { return runFlowInput(arguments) }); err == nil || !strings.Contains(err.Error(), "FLOW_INPUT_ANSWER_CONFLICT") {
		t.Fatalf("conflicting answer result = %v", err)
	}
}

func TestCLIAndRPCBindingsCreateTheSameInputSuspension(t *testing.T) {
	// control-law: transport selection cannot change the typed invocation
	// request for one exact Flow context.
	repository := flowRepository(t)
	runFlowGit(t, repository, "init", "-q")
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery-one.md", []byte("plan"))
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "fixture")
	writeAdmittedFlowProgramState(t, repository, strings.Repeat("f", 64))

	cli, err := bindFlowEntry(context.Background(), commandOptions{
		repository: repository, programID: "product-delivery", entryID: "run", host: "codex",
		correlationID: "transport-parity", transitionID: "publication.observe",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cli.inputRequest == nil {
		t.Fatal("CLI binding did not suspend for host input")
	}
	rpc, err := bindRPCFlowEntry(context.Background(), surfaces.Request{
		SchemaVersion: surfaces.SchemaVersion, Operation: surfaces.OperationResolve,
		Repository: repository, Host: "codex", CorrelationID: "transport-parity",
		ProgramID: "product-delivery", EntryID: "run", FlowID: cli.runID,
		Objective: model.Objective{
			ID: cli.objectiveID, TargetID: model.TargetID(cli.targetID),
			TrustedClass: model.TargetID(cli.trustedObjectiveClass), DeliveryID: cli.deliveryID,
		},
		TransitionID: "publication.observe",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rpc.InputRequest == nil || rpc.InvocationEvidence != nil {
		t.Fatalf("RPC binding did not create a typed suspension: %#v", rpc)
	}
	if rpc.InputRequest.Fingerprint != cli.inputRequest.Fingerprint || rpc.InputRequest.RunID != cli.inputRequest.RunID || rpc.InputRequest.TransitionID != cli.inputRequest.TransitionID {
		t.Fatalf("CLI/RPC suspension drift:\nCLI %#v\nRPC %#v", cli.inputRequest, rpc.InputRequest)
	}
}

func TestApplyAndRecoveryDiscardRevokedInvocationEvidence(t *testing.T) {
	// control-law: apply and recovery rematerialize from runtime-owned inputs;
	// neither can reuse evidence after its receipt is revoked.
	stateRoot := t.TempDir()
	t.Setenv("BOATSTACK_STATE_ROOT", stateRoot)
	repository := flowRepository(t)
	runFlowGit(t, repository, "init", "-q")
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery-one.md", []byte("plan"))
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "fixture")
	writeAdmittedFlowProgramState(t, repository, strings.Repeat("f", 64))

	suspended, err := bindFlowEntry(context.Background(), commandOptions{
		repository: repository, programID: "product-delivery", entryID: "run", host: "codex",
		correlationID: "revocation", transitionID: "publication.observe",
	})
	if err != nil {
		t.Fatal(err)
	}
	answerPath := filepath.Join(t.TempDir(), "answer.json")
	if err := os.WriteFile(answerPath, []byte(`{"publication_id":"123"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := captureStdout(t, func() error {
		return runFlowInput([]string{
			"answer", "--repo", repository, "--flow", "product-delivery", "--entry", "run", "--run-id", suspended.runID,
			"--request-fingerprint", suspended.inputRequest.Fingerprint, "--answer", answerPath, "--human", "operator", "--host", "codex", "--format", "json",
		})
	}); err != nil {
		t.Fatal(err)
	}
	resumed, err := bindFlowEntry(context.Background(), commandOptions{
		repository: repository, programID: "product-delivery", entryID: "run", host: "codex", correlationID: "revocation",
		transitionID: "publication.observe", runID: suspended.runID, deliveryID: suspended.deliveryID,
		targetID: suspended.targetID, objectiveID: suspended.objectiveID,
	})
	if err != nil || resumed.invocationEvidence == nil {
		t.Fatalf("resolved invocation = %#v, %v", resumed.invocationEvidence, err)
	}
	boundFingerprint := resumed.invocationEvidence.InvocationFingerprint
	prior, err := buildRequest(surfaces.OperationApply, resumed)
	if err != nil {
		t.Fatal(err)
	}
	prior.Prescription = protocol.Prescription{InvocationFingerprint: boundFingerprint}

	removed := 0
	if err := filepath.WalkDir(stateRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".receipt.json") {
			removed++
			return os.Remove(path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed %d input receipts, want 1", removed)
	}

	for _, operation := range []surfaces.Operation{surfaces.OperationApply, surfaces.OperationRecover} {
		fresh, _, err := refreshFlowInvocation(context.Background(), operation, prior, resumed)
		if err != nil {
			t.Fatalf("%s refresh: %v", operation, err)
		}
		if fresh.InvocationEvidence != nil || fresh.InputRequest == nil || fresh.InputRequest.Fingerprint != suspended.inputRequest.Fingerprint {
			t.Fatalf("%s reused revoked evidence: evidence=%#v request=%#v", operation, fresh.InvocationEvidence, fresh.InputRequest)
		}
		if err := fresh.Prescription.ValidateInvocation(""); err == nil || !strings.Contains(err.Error(), "INVOCATION_DRIFT") {
			t.Fatalf("%s old prescription result = %v", operation, err)
		}
	}
}
