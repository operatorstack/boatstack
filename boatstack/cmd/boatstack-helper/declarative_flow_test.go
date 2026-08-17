package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/humanidentity"
)

func declarativeInvocationDocument() controlprogram.Document {
	truth := true
	mitigated := "mitigated"
	return controlprogram.Document{
		Schema: controlprogram.SchemaName, SchemaRevision: controlprogram.SchemaRevision,
		Program:      controlprogram.Program{ID: "incident-response-invocation", Version: "1", HumanIdentity: "developer"},
		Declarations: controlprogram.Declarations{Authorities: []string{"human"}, Verifiers: []string{"state-effect"}},
		Facets:       []controlprogram.Facet{{ID: "incident", Kind: "enum", Values: []string{"open", "mitigated"}}},
		Evidence:     []controlprogram.Evidence{{ID: "state-effect", Subject: "incident", Kind: "state-observation"}},
		Operators: []controlprogram.Operator{{
			ID: "restart", Authority: controlprogram.AuthorityRequirement{AnyOf: []string{"human"}}, Verifier: "state-effect", ExecutionContext: "preserve",
			Parameters: []controlprogram.OperatorParameter{
				{ID: "incident", Type: controlprogram.ValueTypeDefinition{Kind: "string"}, Required: true, AllowedSources: []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceEntryInput}},
				{ID: "channel", Type: controlprogram.ValueTypeDefinition{Kind: "string"}, Required: true, AllowedSources: []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceHostInput}, Authority: controlprogram.AuthorityRequirement{AnyOf: []string{"human"}}},
			},
			StateEffect: &controlprogram.StateEffect{Kind: "assignments", Assignments: []controlprogram.StateAssignment{{Facet: "incident", Value: &mitigated}}},
		}},
		Transitions: []controlprogram.Transition{{
			ID: "restart", Operator: "restart", Guard: controlprogram.Predicate{True: &truth}, Target: flowFact("incident", "mitigated"), Priority: 10,
			Parameters: []controlprogram.TransitionParameterBinding{
				{Parameter: "incident", Producer: controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceEntryInput, Input: "incident"}},
				{Parameter: "channel", Producer: controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceHostInput, Request: &controlprogram.HostInputRequest{ID: "channel", Description: "Select the response channel.", Authorities: []string{"human"}, Scope: "transition"}}},
			},
		}},
		Targets: []controlprogram.Target{{ID: "mitigated", Predicate: flowFact("incident", "mitigated")}},
		Entries: []controlprogram.Entry{{ID: "respond", Target: "mitigated", Inputs: []controlprogram.EntryInput{{ID: "incident", Type: "string", Required: true}}}},
	}
}

func declarativeFlowRepository(t *testing.T) string {
	return declarativeFlowRepositoryWithDocument(t, declarativeInvocationDocument())
}

func declarativeFlowRepositoryWithDocument(t *testing.T, document controlprogram.Document) string {
	t.Helper()
	repository := t.TempDir()
	runFlowGit(t, repository, "init", "-b", "main")
	runFlowGit(t, repository, "config", "user.email", "fixture@example.invalid")
	runFlowGit(t, repository, "config", "user.name", "Fixture")
	runFlowGit(t, repository, "config", "core.autocrlf", "false")
	sourcePath, lockPath := ".boatstack/flows/incident-response-invocation.flow.ts", "package-lock.json"
	source, lock := []byte("declarative flow source\n"), []byte("lock\n")
	writeFixture(t, repository, sourcePath, source)
	writeFixture(t, repository, lockPath, lock)
	writeFlowArtifact(t, repository, document, sourcePath, source, lockPath, lock)
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "commit", "-m", "fixture")
	writeVerifiedFlowConfigurationState(t, repository)
	return repository
}

func twoStepDeclarativeDocument() controlprogram.Document {
	truth := true
	contained, mitigated := "contained", "mitigated"
	return controlprogram.Document{
		Schema: controlprogram.SchemaName, SchemaRevision: controlprogram.SchemaRevision,
		Program:      controlprogram.Program{ID: "incident-response-invocation", Version: "1", HumanIdentity: "developer"},
		Declarations: controlprogram.Declarations{Authorities: []string{"human"}, Verifiers: []string{"state-effect"}},
		Facets:       []controlprogram.Facet{{ID: "incident", Kind: "enum", Values: []string{"open", "contained", "mitigated"}}},
		Evidence:     []controlprogram.Evidence{{ID: "state-effect", Subject: "incident", Kind: "state-observation"}},
		Operators: []controlprogram.Operator{
			{ID: "contain", Authority: controlprogram.AuthorityRequirement{AnyOf: []string{"human"}}, Verifier: "state-effect", ExecutionContext: "preserve", StateEffect: &controlprogram.StateEffect{Kind: "assignments", Assignments: []controlprogram.StateAssignment{{Facet: "incident", Value: &contained}}}},
			{ID: "mitigate", Authority: controlprogram.AuthorityRequirement{AnyOf: []string{"human"}}, Verifier: "state-effect", ExecutionContext: "preserve", StateEffect: &controlprogram.StateEffect{Kind: "assignments", Assignments: []controlprogram.StateAssignment{{Facet: "incident", Value: &mitigated}}}},
		},
		Transitions: []controlprogram.Transition{
			{ID: "contain", Operator: "contain", Guard: controlprogram.Predicate{True: &truth}, Target: flowFact("incident", "contained"), Priority: 10},
			{ID: "mitigate", Operator: "mitigate", Guard: flowFact("incident", "contained"), Target: flowFact("incident", "mitigated"), Priority: 20},
		},
		Targets: []controlprogram.Target{{ID: "mitigated", Predicate: flowFact("incident", "mitigated")}},
		Entries: []controlprogram.Entry{{ID: "respond", Target: "mitigated", Inputs: []controlprogram.EntryInput{{ID: "incident", Type: "string", Required: true}}}},
	}
}

func decodeObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode output: %v\n%s", err, raw)
	}
	return value
}

func runApprovedDeclarativeTransition(t *testing.T, repository, runID string, entryInputs ...string) []byte {
	t.Helper()
	arguments := []string{"--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond"}
	if runID != "" {
		arguments = append(arguments, "--run-id", runID)
	}
	for _, input := range entryInputs {
		arguments = append(arguments, "--input", input)
	}
	arguments = append(arguments, "--host", "codex", "--format", "json")
	blockedRaw, err := captureStdout(t, func() error { return runFlowContinuation(arguments) })
	if err != nil {
		t.Fatal(err)
	}
	blocked := decodeObject(t, blockedRaw)
	if blocked["kind"] != "blocked" || blocked["code"] != "AUTHORITY_REQUIRED" {
		t.Fatalf("declarative authority suspension = %s", blockedRaw)
	}
	runID, _ = blocked["run_id"].(string)
	authorityFingerprint, _ := blocked["authority_fingerprint"].(string)
	if len(authorityFingerprint) != 64 || blocked["human_identity"] == nil {
		t.Fatalf("declarative authority binding = %s", blockedRaw)
	}
	approved := []string{
		"--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--run-id", runID,
		"--authority-fingerprint", authorityFingerprint, "--human", "boateng", "--host", "codex", "--format", "json",
	}
	completedRaw, err := captureStdout(t, func() error { return runFlowContinuation(approved) })
	if err != nil {
		t.Fatal(err)
	}
	return completedRaw
}

func rotateDeclarativeIdentity(t *testing.T, repository, prior, next string) {
	t.Helper()
	path := filepath.Join(repository, ".boatstack", "project.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := bytes.Replace(raw, []byte(`"value":"`+prior+`"`), []byte(`"value":"`+next+`"`), 1)
	if bytes.Equal(updated, raw) {
		t.Fatalf("identity fixture does not contain %q", prior)
	}
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatal(err)
	}
	runFlowGit(t, repository, "add", ".boatstack/project.json")
	runFlowGit(t, repository, "commit", "-m", "rotate identity to "+next)
	writeVerifiedFlowConfigurationState(t, repository)
}

func answerDeclarativeRequest(t *testing.T, repository, runID, requestFingerprint, actor string) {
	t.Helper()
	answer := filepath.Join(t.TempDir(), "answer.json")
	if err := os.WriteFile(answer, []byte(`{"channel":"incident-room"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := captureStdout(t, func() error {
		return runFlowInput([]string{"answer", "--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--run-id", runID, "--request-fingerprint", requestFingerprint, "--answer", answer, "--human", actor, "--host", "codex", "--format", "json"})
	}); err != nil {
		t.Fatal(err)
	}
}

func TestGeneratedDeclarativeDriverSuspendsAnswersRestartsAndExecutes(t *testing.T) {
	// control-law: a non-domain adapter generated driver crosses the same
	// typed invocation boundary and resumes one exact run after restart.
	t.Setenv("BOATSTACK_STATE_ROOT", t.TempDir())
	repository := declarativeFlowRepository(t)
	skillPath := filepath.Join(repository, ".agents", "skills", "incident-response-invocation-respond", "SKILL.md")
	skill, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(skill), "boatstack flow run --repo . --flow incident-response-invocation --entry respond --host codex --format json") || !strings.Contains(string(skill), "--input name=value") || !strings.Contains(string(skill), "--authority-fingerprint <authority-fingerprint> --human <actor>") {
		t.Fatalf("generated driver lacks declarative invocation protocol:\n%s", skill)
	}

	suspendedRaw, err := captureStdout(t, func() error {
		return runFlowContinuation([]string{"--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--input", "incident=INC-7", "--host", "codex", "--format", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	suspended := decodeObject(t, suspendedRaw)
	if suspended["kind"] != "suspended" || suspended["code"] != "TRANSITION_INPUT_REQUIRED" {
		t.Fatalf("first driver result = %s", suspendedRaw)
	}
	runID, _ := suspended["run_id"].(string)
	request, _ := suspended["request"].(map[string]any)
	requestFingerprint, _ := request["fingerprint"].(string)
	if !strings.HasPrefix(runID, "run-") || len(requestFingerprint) != 64 {
		t.Fatalf("suspension identity = %#v", suspended)
	}

	answer := filepath.Join(t.TempDir(), "answer.json")
	if err := os.WriteFile(answer, []byte(`{"channel":"incident-room"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := captureStdout(t, func() error {
		return runFlowInput([]string{"answer", "--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--run-id", runID, "--request-fingerprint", requestFingerprint, "--answer", answer, "--human", "boateng", "--host", "codex", "--format", "json"})
	}); err != nil {
		t.Fatal(err)
	}

	blockedRaw, err := captureStdout(t, func() error {
		return runFlowContinuation([]string{"--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--run-id", runID, "--host", "codex", "--format", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	blocked := decodeObject(t, blockedRaw)
	if blocked["kind"] != "blocked" || blocked["code"] != "AUTHORITY_REQUIRED" {
		t.Fatalf("unauthorized driver result = %s", blockedRaw)
	}
	authorityFingerprint, _ := blocked["authority_fingerprint"].(string)
	if len(authorityFingerprint) != 64 || blocked["human_identity"] == nil {
		t.Fatalf("authority suspension is not identity-bound: %s", blockedRaw)
	}

	completedRaw, err := captureStdout(t, func() error {
		return runFlowContinuation([]string{"--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--run-id", runID, "--authority-fingerprint", authorityFingerprint, "--human", "boateng", "--host", "codex", "--format", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	completed := decodeObject(t, completedRaw)
	if completed["kind"] != "terminal" || completed["run_id"] != runID || completed["transition_id"] != "restart" || completed["invocation"] == nil || completed["receipt"] == nil {
		t.Fatalf("resumed driver result = %s", completedRaw)
	}
	replayedRaw, err := captureStdout(t, func() error {
		return runFlowContinuation([]string{"--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--run-id", runID, "--host", "codex", "--format", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	replayed := decodeObject(t, replayedRaw)
	if replayed["kind"] != "terminal" || replayed["receipt"] == nil || !reflect.DeepEqual(completed["receipt"], replayed["receipt"]) {
		t.Fatalf("terminal replay lost its durable receipt:\ncompleted=%s\nreplayed=%s", completedRaw, replayedRaw)
	}
}

func TestDeclarativeIdentityRotationSupersedesInputAndAuthoritySuspensions(t *testing.T) {
	// control-law: provider rotation never consumes an input receipt or explicit
	// approval presented under the prior verified identity context.
	t.Setenv("BOATSTACK_STATE_ROOT", t.TempDir())
	repository := declarativeFlowRepository(t)
	startRaw, err := captureStdout(t, func() error {
		return runFlowContinuation([]string{"--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--input", "incident=INC-7", "--host", "codex", "--format", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	start := decodeObject(t, startRaw)
	runID, _ := start["run_id"].(string)
	requestA := start["request"].(map[string]any)
	requestFingerprintA, _ := requestA["fingerprint"].(string)
	authorityContextA, _ := requestA["authority_context_fingerprint"].(string)
	identityA := start["human_identity"].(map[string]any)
	presentationA, err := humanidentity.NewPresentation("developer", humanidentity.Descriptor{Kind: humanidentity.KindLiteral, Value: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	bindingA, err := presentationA.BindingFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if len(authorityContextA) != 64 || authorityContextA != bindingA || identityA["role"] != "developer" || identityA["provider_fingerprint"] != presentationA.ProviderFingerprint {
		t.Fatalf("provider A suspension is not bound: %s", startRaw)
	}

	rotateDeclarativeIdentity(t, repository, "operator", "second-operator")
	answer := filepath.Join(t.TempDir(), "answer.json")
	if err := os.WriteFile(answer, []byte(`{"channel":"incident-room"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := captureStdout(t, func() error {
		return runFlowInput([]string{"answer", "--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--run-id", runID, "--request-fingerprint", requestFingerprintA, "--answer", answer, "--human", "operator", "--host", "codex", "--format", "json"})
	}); err == nil || !strings.Contains(err.Error(), "HUMAN_IDENTITY_DRIFT") {
		t.Fatalf("stale provider A input answer = %v", err)
	}

	secondRaw, err := captureStdout(t, func() error {
		return runFlowContinuation([]string{"--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--run-id", runID, "--host", "codex", "--format", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	second := decodeObject(t, secondRaw)
	requestB := second["request"].(map[string]any)
	requestFingerprintB, _ := requestB["fingerprint"].(string)
	authorityContextB, _ := requestB["authority_context_fingerprint"].(string)
	if second["kind"] != "suspended" || requestFingerprintB == requestFingerprintA || authorityContextB == authorityContextA {
		t.Fatalf("provider B did not produce a fresh input suspension: %s", secondRaw)
	}
	answerDeclarativeRequest(t, repository, runID, requestFingerprintB, "second-operator")

	authorityRaw, err := captureStdout(t, func() error {
		return runFlowContinuation([]string{"--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--run-id", runID, "--host", "codex", "--format", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	authorityB := decodeObject(t, authorityRaw)
	authorityFingerprintB, _ := authorityB["authority_fingerprint"].(string)
	if authorityB["code"] != "AUTHORITY_REQUIRED" || len(authorityFingerprintB) != 64 {
		t.Fatalf("provider B authority suspension = %s", authorityRaw)
	}

	rotateDeclarativeIdentity(t, repository, "second-operator", "third-operator")
	resuspendedRaw, err := captureStdout(t, func() error {
		return runFlowContinuation([]string{"--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--run-id", runID, "--authority-fingerprint", authorityFingerprintB, "--human", "second-operator", "--host", "codex", "--format", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	resuspended := decodeObject(t, resuspendedRaw)
	requestC := resuspended["request"].(map[string]any)
	requestFingerprintC, _ := requestC["fingerprint"].(string)
	authorityContextC, _ := requestC["authority_context_fingerprint"].(string)
	if resuspended["kind"] != "suspended" || requestFingerprintC == requestFingerprintB || authorityContextC == authorityContextB {
		t.Fatalf("provider C reused provider B input: %s", resuspendedRaw)
	}
	answerDeclarativeRequest(t, repository, runID, requestFingerprintC, "third-operator")

	freshAuthorityRaw, err := captureStdout(t, func() error {
		return runFlowContinuation([]string{"--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--run-id", runID, "--authority-fingerprint", authorityFingerprintB, "--human", "second-operator", "--host", "codex", "--format", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	freshAuthority := decodeObject(t, freshAuthorityRaw)
	authorityFingerprintC, _ := freshAuthority["authority_fingerprint"].(string)
	if freshAuthority["code"] != "AUTHORITY_REQUIRED" || authorityFingerprintC == authorityFingerprintB {
		t.Fatalf("provider C reused provider B approval: %s", freshAuthorityRaw)
	}

	terminalRaw, err := captureStdout(t, func() error {
		return runFlowContinuation([]string{"--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--run-id", runID, "--authority-fingerprint", authorityFingerprintC, "--human", "third-operator", "--host", "codex", "--format", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	terminal := decodeObject(t, terminalRaw)
	receipt := terminal["receipt"].(map[string]any)
	if terminal["kind"] != "terminal" || receipt["authority_context_fingerprint"] != authorityContextC || receipt["authority_fingerprint"] != authorityFingerprintC || receipt["human_actor"] != "third-operator" {
		t.Fatalf("terminal receipt lost provider C authority: %s", terminalRaw)
	}
}

func TestDeclarativeRunRejectsCrossWorktreeResume(t *testing.T) {
	// control-law: an explicit run ID cannot bypass the opaque execution-scope
	// identity that was bound when the durable declarative run was created.
	t.Setenv("BOATSTACK_STATE_ROOT", t.TempDir())
	repository := declarativeFlowRepository(t)
	suspendedRaw, err := captureStdout(t, func() error {
		return runFlowContinuation([]string{"--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--input", "incident=INC-7", "--host", "codex", "--format", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	suspended := decodeObject(t, suspendedRaw)
	runID, _ := suspended["run_id"].(string)
	request := suspended["request"]
	otherWorktree := filepath.Join(t.TempDir(), "other-worktree")
	runFlowGit(t, repository, "worktree", "add", "-q", "-b", "other-worktree", otherWorktree)
	if _, err := captureStdout(t, func() error {
		return runFlowContinuation([]string{"--repo", otherWorktree, "--flow", "incident-response-invocation", "--entry", "respond", "--run-id", runID, "--host", "codex", "--format", "json"})
	}); err == nil || !strings.Contains(err.Error(), "FLOW_CONTEXT_MISMATCH") {
		t.Fatalf("cross-worktree resume = %v", err)
	}
	replayedRaw, err := captureStdout(t, func() error {
		return runFlowContinuation([]string{"--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--run-id", runID, "--host", "codex", "--format", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	replayed := decodeObject(t, replayedRaw)
	if replayed["kind"] != "suspended" || !reflect.DeepEqual(request, replayed["request"]) {
		t.Fatalf("cross-scope refusal changed the originating run:\nbefore=%s\nafter=%s", suspendedRaw, replayedRaw)
	}
}

func TestDeclarativeSelectionPreservesEqualPriorityFrontier(t *testing.T) {
	// control-law: equally preferred transitions are an explicit frontier, not
	// an ID-based mutation choice.
	t.Setenv("BOATSTACK_STATE_ROOT", t.TempDir())
	document := declarativeInvocationDocument()
	alternate := document.Transitions[0]
	alternate.ID = "alternate-restart"
	document.Transitions = append(document.Transitions, alternate)
	repository := declarativeFlowRepositoryWithDocument(t, document)
	blockedRaw, err := captureStdout(t, func() error {
		return runFlowContinuation([]string{"--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--input", "incident=INC-7", "--human", "boateng", "--host", "codex", "--format", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	blocked := decodeObject(t, blockedRaw)
	if blocked["kind"] != "blocked" || blocked["code"] != "FLOW_SELECTION_AMBIGUOUS" || blocked["state_revision"] != float64(1) {
		t.Fatalf("ambiguous selection = %s", blockedRaw)
	}
	want := []any{"alternate-restart", "restart"}
	if !reflect.DeepEqual(blocked["transitions"], want) {
		t.Fatalf("frontier = %#v, want %#v", blocked["transitions"], want)
	}
	runID, _ := blocked["run_id"].(string)
	replayedRaw, err := captureStdout(t, func() error {
		return runFlowContinuation([]string{"--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--run-id", runID, "--human", "boateng", "--host", "codex", "--format", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if replayed := decodeObject(t, replayedRaw); replayed["code"] != "FLOW_SELECTION_AMBIGUOUS" || replayed["state_revision"] != float64(1) {
		t.Fatalf("ambiguous replay mutated state = %s", replayedRaw)
	}
}

func TestDeclarativeRunRefusesTargetOutsideBestPriorityFrontier(t *testing.T) {
	// control-law: an explicit transition request narrows the current trusted
	// frontier; it cannot select a lower-priority mutation.
	t.Setenv("BOATSTACK_STATE_ROOT", t.TempDir())
	document := declarativeInvocationDocument()
	alternate := document.Transitions[0]
	alternate.ID = "alternate-restart"
	alternate.Priority = 20
	document.Transitions = append(document.Transitions, alternate)
	repository := declarativeFlowRepositoryWithDocument(t, document)
	blockedRaw, err := captureStdout(t, func() error {
		return runFlowContinuation([]string{"--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--transition", "alternate-restart", "--input", "incident=INC-7", "--human", "boateng", "--host", "codex", "--format", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	blocked := decodeObject(t, blockedRaw)
	if blocked["kind"] != "blocked" || blocked["code"] != "FLOW_TRANSITION_NOT_ADMISSIBLE" || blocked["transition_id"] != "alternate-restart" || blocked["state_revision"] != float64(1) {
		t.Fatalf("targeted selection = %s", blockedRaw)
	}
	if !reflect.DeepEqual(blocked["transitions"], []any{"restart"}) || blocked["receipt"] != nil {
		t.Fatalf("targeted refusal did not preserve the frontier: %s", blockedRaw)
	}
	runID, _ := blocked["run_id"].(string)
	suspendedRaw, err := captureStdout(t, func() error {
		return runFlowContinuation([]string{"--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--run-id", runID, "--host", "codex", "--format", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	suspended := decodeObject(t, suspendedRaw)
	if suspended["kind"] != "suspended" || suspended["transition_id"] != "restart" {
		t.Fatalf("untargeted continuation after refusal = %s", suspendedRaw)
	}
}

func TestDeclarativeReceiptHistoryIsImmutableAndContiguous(t *testing.T) {
	// control-law: every accepted declarative transition remains recoverable as
	// one contiguous durable receipt chain after later commits and restart.
	t.Setenv("BOATSTACK_STATE_ROOT", t.TempDir())
	repository := declarativeFlowRepositoryWithDocument(t, twoStepDeclarativeDocument())
	firstRaw := runApprovedDeclarativeTransition(t, repository, "", "incident=INC-7")
	first := decodeObject(t, firstRaw)
	if first["kind"] != "continued" {
		t.Fatalf("first transition = %s", firstRaw)
	}
	runID, _ := first["run_id"].(string)
	firstReceipt := first["receipt"]
	secondRaw := runApprovedDeclarativeTransition(t, repository, runID)
	second := decodeObject(t, secondRaw)
	receipts, _ := second["receipts"].([]any)
	if second["kind"] != "terminal" || len(receipts) != 2 || !reflect.DeepEqual(receipts[0], firstReceipt) {
		t.Fatalf("second transition lost receipt history:\nfirst=%s\nsecond=%s", firstRaw, secondRaw)
	}
	prior := receipts[0].(map[string]any)
	latest := receipts[1].(map[string]any)
	if latest["prior_receipt_fingerprint"] != prior["fingerprint"] || latest["prior_state_revision"] != prior["result_state_revision"] {
		t.Fatalf("receipt chain is not contiguous: %#v", receipts)
	}
	replayedRaw, err := captureStdout(t, func() error {
		return runFlowContinuation([]string{"--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--run-id", runID, "--host", "codex", "--format", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	replayed := decodeObject(t, replayedRaw)
	if replayed["kind"] != "terminal" || !reflect.DeepEqual(replayed["receipts"], second["receipts"]) || !reflect.DeepEqual(replayed["receipt"], second["receipt"]) {
		t.Fatalf("restart changed durable receipt history:\nsecond=%s\nreplayed=%s", secondRaw, replayedRaw)
	}
}

func TestDeclarativeRuntimeRejectsSemanticsItCannotProve(t *testing.T) {
	open := "open"
	for name, test := range map[string]struct {
		mutate  func(*controlprogram.Document)
		witness string
	}{
		"unsupported-verifier": {func(document *controlprogram.Document) {
			document.Declarations.Verifiers = []string{"healthcheck"}
			document.Evidence = []controlprogram.Evidence{{ID: "healthcheck", Subject: "incident", Kind: "observation"}}
			document.Operators[0].Verifier = "healthcheck"
		}, "effect-free"},
		"unsupported-authority": {func(document *controlprogram.Document) {
			document.Declarations.Authorities = append(document.Declarations.Authorities, "autonomy")
			document.Operators[0].Authority = controlprogram.AuthorityRequirement{AnyOf: []string{"autonomy"}}
		}, "unsupported authority"},
		"unsupported-parameter-authority": {func(document *controlprogram.Document) {
			document.Declarations.Authorities = append(document.Declarations.Authorities, "autonomy")
			document.Operators[0].Parameters[1].Authority = controlprogram.AuthorityRequirement{AnyOf: []string{"autonomy"}}
			document.Transitions[0].Parameters[1].Producer.Request.Authorities = []string{"autonomy"}
		}, "parameter \"channel\" uses unsupported authority"},
		"unsupported-host-input-authority": {func(document *controlprogram.Document) {
			document.Declarations.Authorities = append(document.Declarations.Authorities, "autonomy")
			document.Operators[0].Parameters[1].Authority = controlprogram.AuthorityRequirement{}
			document.Transitions[0].Parameters[1].Producer.Request.Authorities = []string{"autonomy"}
		}, "host-input parameter \"channel\" uses unsupported authority"},
		"unproved-precondition": {func(document *controlprogram.Document) {
			document.Operators[0].StateEffect.Preconditions = []controlprogram.StatePrecondition{{Facet: "incident", Values: []string{"open"}}}
		}, "does not establish precondition"},
		"unsupported-assignment-source": {func(document *controlprogram.Document) {
			document.Operators[0].StateEffect.Assignments[0] = controlprogram.StateAssignment{Facet: "incident", ValueFrom: &controlprogram.ValueReference{Admission: "id"}}
		}, "unsupported value source"},
		"unproved-enum-parameter": {func(document *controlprogram.Document) {
			document.Operators[0].StateEffect.Assignments[0] = controlprogram.StateAssignment{Facet: "incident", ValueFrom: &controlprogram.ValueReference{Parameter: "channel"}}
		}, "cannot prove parameter"},
		"string-parameter-to-boolean-facet": {func(document *controlprogram.Document) {
			document.Facets = append(document.Facets, controlprogram.Facet{ID: "approved", Kind: "boolean"})
			document.Operators[0].StateEffect.Assignments = append(document.Operators[0].StateEffect.Assignments,
				controlprogram.StateAssignment{Facet: "approved", ValueFrom: &controlprogram.ValueReference{Parameter: "channel"}})
		}, "cannot prove parameter"},
		"invalid-boolean-literal": {func(document *controlprogram.Document) {
			invalid := "not-a-boolean"
			document.Facets = append(document.Facets, controlprogram.Facet{ID: "approved", Kind: "boolean"})
			document.Operators[0].StateEffect.Assignments = append(document.Operators[0].StateEffect.Assignments,
				controlprogram.StateAssignment{Facet: "approved", Value: &invalid})
		}, "outside the facet type"},
		"target-not-established": {func(document *controlprogram.Document) {
			document.Operators[0].StateEffect.Assignments[0] = controlprogram.StateAssignment{Facet: "incident", Value: &open}
		}, "do not establish its target"},
		"parameter-mutation-cannot-inherit-guard": {func(document *controlprogram.Document) {
			done := "yes"
			document.Facets[0].Kind, document.Facets[0].Values = "string", nil
			document.Facets = append(document.Facets, controlprogram.Facet{ID: "done", Kind: "enum", Values: []string{"no", "yes"}})
			document.Operators[0].StateEffect.Assignments = []controlprogram.StateAssignment{
				{Facet: "incident", ValueFrom: &controlprogram.ValueReference{Parameter: "channel"}},
				{Facet: "done", Value: &done},
			}
			document.Transitions[0].Guard = flowFact("incident", "open")
			document.Transitions[0].Target = controlprogram.Predicate{All: []controlprogram.Predicate{flowFact("incident", "open"), flowFact("done", "yes")}}
			document.Targets[0].Predicate = document.Transitions[0].Target
		}, "do not establish its target"},
		"non-positive-priority": {func(document *controlprogram.Document) {
			document.Transitions[0].Priority = 0
		}, "priority must be positive"},
	} {
		t.Run(name, func(t *testing.T) {
			document := declarativeInvocationDocument()
			test.mutate(&document)
			compiled, err := controlprogram.Compile(document, nil)
			if err == nil {
				err = validateDeclarativeFlow(compiled)
			}
			if err == nil || !strings.Contains(err.Error(), test.witness) {
				t.Fatalf("validation = %v, want %q", err, test.witness)
			}
		})
	}
}

func TestDeclarativeRuntimeRejectsTypedAssignmentBeforeStateOrReceipt(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("BOATSTACK_STATE_ROOT", stateRoot)
	document := declarativeInvocationDocument()
	document.Facets = append(document.Facets, controlprogram.Facet{ID: "approved", Kind: "boolean"})
	document.Operators[0].StateEffect.Assignments = append(document.Operators[0].StateEffect.Assignments,
		controlprogram.StateAssignment{Facet: "approved", ValueFrom: &controlprogram.ValueReference{Parameter: "channel"}})
	repository := declarativeFlowRepositoryWithDocument(t, document)

	_, err := captureStdout(t, func() error {
		return runFlowContinuation([]string{"--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--input", "incident=INC-7", "--host", "codex", "--format", "json"})
	})
	if err == nil || !strings.Contains(err.Error(), "cannot prove parameter \"channel\" belongs to the facet") {
		t.Fatalf("run validation = %v", err)
	}
	entries, readErr := os.ReadDir(stateRoot)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid Flow created controller state or receipts: %#v", entries)
	}
}
