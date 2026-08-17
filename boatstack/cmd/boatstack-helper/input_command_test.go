package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
	"github.com/operatorstack/boatstack/boatstack/invocation"
)

func TestFlowInputAnswerResumesSameRunAndConflictsFailClosed(t *testing.T) {
	// control-law: a missing declared host value suspends and only an exact
	// runtime-owned receipt resumes the same invocation.
	repository := flowRepositoryWithHumanSlice(t)
	runFlowGit(t, repository, "init", "-q")
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery-one.md", []byte("plan"))
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "fixture")
	writeAdmittedFlowProgramState(t, repository, strings.Repeat("f", 64))

	base := commandOptions{repository: repository, programID: "product-delivery", entryID: "run", host: "codex", transitionID: "delivery.slice.advance"}
	suspended, err := bindFlowEntry(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if suspended.inputRequest == nil || suspended.inputRequest.Code != "TRANSITION_INPUT_REQUIRED" || suspended.invocationEvidence != nil {
		t.Fatalf("suspension = %#v", suspended.inputRequest)
	}
	answerPath := filepath.Join(t.TempDir(), "answer.json")
	if err := os.WriteFile(answerPath, []byte(`{"slice_id":"slice-one"}`), 0o600); err != nil {
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
		repository: repository, programID: "product-delivery", entryID: "run", host: "codex", transitionID: "delivery.slice.advance",
		runID: suspended.runID, deliveryID: suspended.deliveryID, targetID: suspended.targetID, objectiveID: suspended.objectiveID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.invocationEvidence == nil || resumed.inputRequest != nil || !strings.Contains(strings.Join(resumed.parameters, ","), "slice_id=slice-one") {
		t.Fatalf("resumed invocation = evidence %#v request %#v parameters %#v", resumed.invocationEvidence, resumed.inputRequest, resumed.parameters)
	}

	if err := os.WriteFile(answerPath, []byte(`{"slice_id":"slice-two"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := captureStdout(t, func() error { return runFlowInput(arguments) }); err == nil || !strings.Contains(err.Error(), "FLOW_INPUT_ANSWER_CONFLICT") {
		t.Fatalf("conflicting answer result = %v", err)
	}
}

func TestFlowInputAnswerRejectsIdentityControlBundleDriftBeforeReceipt(t *testing.T) {
	repository := flowRepositoryWithHumanSlice(t)
	runFlowGit(t, repository, "init", "-q")
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery-one.md", []byte("plan"))
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "fixture")
	writeAdmittedFlowProgramState(t, repository, strings.Repeat("f", 64))

	suspended, err := bindFlowEntry(context.Background(), commandOptions{
		repository: repository, programID: "product-delivery", entryID: "run", host: "codex", transitionID: "delivery.slice.advance",
	})
	if err != nil || suspended.inputRequest == nil {
		t.Fatalf("suspension = %#v, err=%v", suspended.inputRequest, err)
	}
	answerPath := filepath.Join(t.TempDir(), "answer.json")
	if err := os.WriteFile(answerPath, []byte(`{"slice_id":"slice-one"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	arguments := []string{
		"answer", "--repo", repository, "--flow", "product-delivery", "--entry", "run", "--run-id", suspended.runID,
		"--request-fingerprint", suspended.inputRequest.Fingerprint, "--answer", answerPath, "--human", "operator", "--host", "codex", "--format", "json",
	}
	configPath := filepath.Join(repository, ".boatstack", "project.json")
	original, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	drifted := bytes.Replace(original, []byte(`"value":"operator"`), []byte(`"value":"other-operator"`), 1)
	if bytes.Equal(drifted, original) {
		t.Fatal("fixture project configuration has no literal identity")
	}
	if err := os.WriteFile(configPath, drifted, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := captureStdout(t, func() error { return runFlowInput(arguments) }); err == nil || !strings.Contains(err.Error(), "HUMAN_IDENTITY_DRIFT") {
		t.Fatalf("drifted answer result = %v", err)
	}
	if err := os.WriteFile(configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := captureStdout(t, func() error { return runFlowInput(arguments) }); err != nil {
		t.Fatalf("failed drift attempt contaminated immutable input receipts: %v", err)
	}
}

func TestRejectedHostAnswerCanBeSupersededWithoutMutation(t *testing.T) {
	// control-law: semantic rejection preserves the original request and receipt
	// while a fresh request generation can collect a corrected free-form value.
	stateRoot := t.TempDir()
	t.Setenv("BOATSTACK_STATE_ROOT", stateRoot)
	repository := flowRepositoryWithHumanSlice(t)
	runFlowGit(t, repository, "init", "-q")
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery-one.md", []byte("plan"))
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "fixture")
	writeAdmittedFlowProgramState(t, repository, strings.Repeat("f", 64))

	base := commandOptions{repository: repository, programID: "product-delivery", entryID: "run", host: "codex", transitionID: "delivery.slice.advance"}
	first, err := bindFlowEntry(context.Background(), base)
	if err != nil || first.inputRequest == nil {
		t.Fatalf("first request = %#v, %v", first.inputRequest, err)
	}
	answerPath := filepath.Join(t.TempDir(), "answer.json")
	if err := os.WriteFile(answerPath, []byte(`{"slice_id":"rejected-slice"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	answer := func(fingerprint string) error {
		_, answerErr := captureStdout(t, func() error {
			return runFlowInput([]string{
				"answer", "--repo", repository, "--flow", "product-delivery", "--entry", "run", "--run-id", first.runID,
				"--request-fingerprint", fingerprint, "--answer", answerPath, "--human", "operator", "--host", "codex", "--format", "json",
			})
		})
		return answerErr
	}
	if err := answer(first.inputRequest.Fingerprint); err != nil {
		t.Fatal(err)
	}

	output, err := captureStdout(t, func() error {
		return runFlowInput([]string{
			"supersede", "--repo", repository, "--flow", "product-delivery", "--entry", "run", "--run-id", first.runID,
			"--request-fingerprint", first.inputRequest.Fingerprint, "--reason", "slice is outside the accepted plan", "--human", "operator", "--host", "codex", "--format", "json",
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	var superseded struct {
		Request invocation.InputRequest `json:"request"`
	}
	if err := json.Unmarshal(output, &superseded); err != nil {
		t.Fatal(err)
	}
	second := superseded.Request
	if second.EffectiveGeneration() != 2 || second.Fingerprint == first.inputRequest.Fingerprint || second.Supersession == nil || second.Supersession.PreviousRequestFingerprint != first.inputRequest.Fingerprint {
		t.Fatalf("superseded request = %#v", second)
	}
	resuspended, err := bindFlowEntry(context.Background(), commandOptions{
		repository: repository, programID: "product-delivery", entryID: "run", host: "codex", transitionID: "delivery.slice.advance",
		runID: first.runID, deliveryID: first.deliveryID, targetID: first.targetID, objectiveID: first.objectiveID,
	})
	if err != nil || resuspended.inputRequest == nil || resuspended.inputRequest.Fingerprint != second.Fingerprint || resuspended.invocationEvidence != nil {
		t.Fatalf("new generation did not suspend: %#v evidence=%#v err=%v", resuspended.inputRequest, resuspended.invocationEvidence, err)
	}
	if err := os.WriteFile(answerPath, []byte(`{"slice_id":"accepted-slice"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := answer(second.Fingerprint); err != nil {
		t.Fatal(err)
	}
	resumed, err := bindFlowEntry(context.Background(), commandOptions{
		repository: repository, programID: "product-delivery", entryID: "run", host: "codex", transitionID: "delivery.slice.advance",
		runID: first.runID, deliveryID: first.deliveryID, targetID: first.targetID, objectiveID: first.objectiveID,
	})
	if err != nil || resumed.invocationEvidence == nil || resumed.inputRequest != nil || !strings.Contains(strings.Join(resumed.parameters, ","), "slice_id=accepted-slice") {
		t.Fatalf("corrected generation did not resume: parameters=%#v evidence=%#v request=%#v err=%v", resumed.parameters, resumed.invocationEvidence, resumed.inputRequest, err)
	}
	receiptCount := 0
	if err := filepath.WalkDir(stateRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".receipt.json") {
			receiptCount++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if receiptCount != 2 {
		t.Fatalf("immutable receipt count = %d, want rejected and corrected generations", receiptCount)
	}
}

func TestCLIAndRPCBindingsCreateTheSameInputSuspension(t *testing.T) {
	// control-law: transport selection cannot change the typed invocation
	// request for one exact Flow context.
	repository := flowRepositoryWithHumanSlice(t)
	runFlowGit(t, repository, "init", "-q")
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery-one.md", []byte("plan"))
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "fixture")
	writeAdmittedFlowProgramState(t, repository, strings.Repeat("f", 64))

	cli, err := bindFlowEntry(context.Background(), commandOptions{
		repository: repository, programID: "product-delivery", entryID: "run", host: "codex",
		correlationID: "transport-parity", transitionID: "delivery.slice.advance",
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
		TransitionID: "delivery.slice.advance",
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
	repository := flowRepositoryWithHumanSlice(t)
	runFlowGit(t, repository, "init", "-q")
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery-one.md", []byte("plan"))
	pinRaw, err := boatstackruntime.EncodePin(boatstackruntime.NewPin(
		boatstackruntime.Identity{Version: "v-test", SHA256: strings.Repeat("a", 64), SourceRevision: "test-revision"},
		strings.Repeat("f", 64), durable.StateSchemaVersion,
	))
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, repository, ".boatstack/runtime.json", pinRaw)
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "fixture")
	writeAdmittedFlowProgramState(t, repository, strings.Repeat("f", 64))

	suspended, err := bindFlowEntry(context.Background(), commandOptions{
		repository: repository, programID: "product-delivery", entryID: "run", host: "codex",
		correlationID: "revocation", transitionID: "delivery.slice.advance",
	})
	if err != nil {
		t.Fatal(err)
	}
	answerPath := filepath.Join(t.TempDir(), "answer.json")
	if err := os.WriteFile(answerPath, []byte(`{"slice_id":"slice-one"}`), 0o600); err != nil {
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
		transitionID: "delivery.slice.advance", runID: suspended.runID, deliveryID: suspended.deliveryID,
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
