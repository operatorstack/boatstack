package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
)

func declarativeInvocationDocument() controlprogram.Document {
	truth := true
	mitigated := "mitigated"
	return controlprogram.Document{
		Schema: controlprogram.SchemaName, SchemaRevision: controlprogram.SchemaRevision,
		Program:      controlprogram.Program{ID: "incident-response-invocation", Version: "1"},
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
	sourcePath, lockPath := ".boatstack/flows/incident-response-invocation.flow.ts", "package-lock.json"
	source, lock := []byte("declarative flow source\n"), []byte("lock\n")
	writeFixture(t, repository, sourcePath, source)
	writeFixture(t, repository, lockPath, lock)
	writeFlowArtifact(t, repository, document, sourcePath, source, lockPath, lock)
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "commit", "-m", "fixture")
	return repository
}

func twoStepDeclarativeDocument() controlprogram.Document {
	truth := true
	contained, mitigated := "contained", "mitigated"
	return controlprogram.Document{
		Schema: controlprogram.SchemaName, SchemaRevision: controlprogram.SchemaRevision,
		Program:      controlprogram.Program{ID: "incident-response-invocation", Version: "1"},
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
	if !strings.Contains(string(skill), "boatstack flow run --repo . --flow incident-response-invocation --entry respond --host codex --format json") || !strings.Contains(string(skill), "--input name=value") {
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

	completedRaw, err := captureStdout(t, func() error {
		return runFlowContinuation([]string{"--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--run-id", runID, "--human", "boateng", "--host", "codex", "--format", "json"})
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

func TestDeclarativeReceiptHistoryIsImmutableAndContiguous(t *testing.T) {
	// control-law: every accepted declarative transition remains recoverable as
	// one contiguous durable receipt chain after later commits and restart.
	t.Setenv("BOATSTACK_STATE_ROOT", t.TempDir())
	repository := declarativeFlowRepositoryWithDocument(t, twoStepDeclarativeDocument())
	firstRaw, err := captureStdout(t, func() error {
		return runFlowContinuation([]string{"--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--input", "incident=INC-7", "--human", "boateng", "--host", "codex", "--format", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	first := decodeObject(t, firstRaw)
	if first["kind"] != "continued" {
		t.Fatalf("first transition = %s", firstRaw)
	}
	runID, _ := first["run_id"].(string)
	firstReceipt := first["receipt"]
	secondRaw, err := captureStdout(t, func() error {
		return runFlowContinuation([]string{"--repo", repository, "--flow", "incident-response-invocation", "--entry", "respond", "--run-id", runID, "--human", "boateng", "--host", "codex", "--format", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}
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
		"unproved-precondition": {func(document *controlprogram.Document) {
			document.Operators[0].StateEffect.Preconditions = []controlprogram.StatePrecondition{{Facet: "incident", Values: []string{"open"}}}
		}, "does not establish precondition"},
		"unsupported-assignment-source": {func(document *controlprogram.Document) {
			document.Operators[0].StateEffect.Assignments[0] = controlprogram.StateAssignment{Facet: "incident", ValueFrom: &controlprogram.ValueReference{Admission: "id"}}
		}, "unsupported value source"},
		"unproved-enum-parameter": {func(document *controlprogram.Document) {
			document.Operators[0].StateEffect.Assignments[0] = controlprogram.StateAssignment{Facet: "incident", ValueFrom: &controlprogram.ValueReference{Parameter: "channel"}}
		}, "cannot prove parameter"},
		"target-not-established": {func(document *controlprogram.Document) {
			document.Operators[0].StateEffect.Assignments[0] = controlprogram.StateAssignment{Facet: "incident", Value: &open}
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
