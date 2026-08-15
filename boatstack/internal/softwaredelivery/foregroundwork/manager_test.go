package foregroundwork_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/effects"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/foregroundwork"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	general "github.com/operatorstack/boatstack/boatstack/kernel"
)

type resolver struct{ layout ports.ControllerLayout }

func (r resolver) ResolveInvocation(context.Context, string, string, string) (model.InvocationContext, error) {
	return invocation(), nil
}
func (r resolver) ResolveLayout(context.Context, model.InvocationContext) (ports.ControllerLayout, model.InvocationContext, error) {
	return r.layout, invocation(), nil
}

type clock struct{ at time.Time }

func (c clock) Now() time.Time { return c.at }

func invocation() model.InvocationContext {
	return model.InvocationContext{RepositoryID: "repository", GitCommonID: "common", WorktreeID: "worktree", Ref: "refs/heads/main"}
}

func workInputs(value, fingerprint string) map[string]protocol.WorkInputValue {
	return map[string]protocol.WorkInputValue{"incident": {Value: value, Fingerprint: fingerprint}}
}

func fixture(t *testing.T) (foregroundwork.Manager, model.Snapshot, catalog.Transition, string) {
	t.Helper()
	root := t.TempDir()
	resolved := resolver{layout: ports.ControllerLayout{FlowRoot: filepath.Join(root, "flow"), LockRoot: filepath.Join(root, "locks")}}
	locker, err := effects.NewLocker(resolved)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := foregroundwork.NewManager(resolved, locker, clock{at: time.Unix(100, 0).UTC()}, effects.NewRuntimeStore())
	if err != nil {
		t.Fatal(err)
	}
	outputs := []catalog.WorkOutput{{
		ID: "diagnosis", Path: "diagnosis.json", MediaType: "application/json", Required: true, MaxBytes: 1024,
		SchemaPath: "schema.json", SchemaSHA256: strings.Repeat("b", 64),
		SchemaContent: `{"type":"object","properties":{"cause":{"type":"string"}},"required":["cause"],"additionalProperties":false}`,
	}}
	work := &catalog.WorkContract{ID: "diagnose", InstructionPath: "instructions.md", InstructionSHA256: strings.Repeat("a", 64), InstructionContent: "Diagnose.", Inputs: []catalog.WorkInput{{ID: "incident", EntryInput: "incident"}}, Outputs: outputs}
	fingerprint, err := general.Fingerprint(struct {
		ID                 string               `json:"id"`
		InstructionPath    string               `json:"instruction_path"`
		InstructionSHA256  string               `json:"instruction_sha256"`
		InstructionContent string               `json:"instruction_content"`
		Inputs             []catalog.WorkInput  `json:"inputs,omitempty"`
		Outputs            []catalog.WorkOutput `json:"outputs"`
	}{work.ID, work.InstructionPath, work.InstructionSHA256, work.InstructionContent, work.Inputs, work.Outputs})
	if err != nil {
		t.Fatal(err)
	}
	work.Fingerprint = fingerprint
	transition := catalog.Transition{ID: "incident.diagnose", Work: work}
	snapshot := model.Snapshot{Observation: model.Observation{ProgramFingerprint: strings.Repeat("c", 64), StateRevision: 4, Invocation: invocation()}, Fingerprint: strings.Repeat("d", 64)}
	return manager, snapshot, transition, root
}

func TestForegroundWorkQuestionCompletionAndDrift(t *testing.T) {
	// control-law: questions suspend one work request and drift invalidates its evidence
	manager, snapshot, transition, _ := fixture(t)
	ctx := context.Background()
	objective := model.Objective{ID: "incident-1", TargetID: "mitigated", DeliveryID: "incident-1"}
	record, err := manager.Ensure(ctx, invocation(), "run-1", "incident-response", "respond", objective, snapshot, transition, workInputs("incident.json", strings.Repeat("e", 64)))
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != foregroundwork.StatusRequested || record.Request.InstructionContent != "Diagnose." {
		t.Fatalf("request = %#v", record)
	}
	record, err = manager.InputRequired(ctx, invocation(), "run-1", "diagnose", "Which service?", []byte(`{"type":"string","minLength":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Answer(ctx, invocation(), "run-1", "diagnose", record.Question.ID, []byte(`""`)); err == nil {
		t.Fatal("schema-invalid answer was accepted")
	}
	record, err = manager.Answer(ctx, invocation(), "run-1", "diagnose", record.Question.ID, []byte("  \"api\"\n"))
	if err != nil || record.Status != foregroundwork.StatusRequested {
		t.Fatalf("answer = %#v err=%v", record, err)
	}
	record, err = manager.Show(ctx, invocation(), "run-1", "diagnose")
	if err != nil || string(record.Answers[0].Value) != `"api"` {
		t.Fatalf("persisted canonical answer = %#v err=%v", record.Answers, err)
	}
	if err := os.WriteFile(filepath.Join(record.Request.StagingRoot, "diagnosis.json"), []byte(`{"cause":"overload"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	record, err = manager.Complete(ctx, invocation(), "run-1", "diagnose")
	if err != nil || record.Result == nil || record.Result.Outputs[0].Content != `{"cause":"overload"}` {
		t.Fatalf("completion = %#v err=%v", record, err)
	}
	snapshot.StateRevision++
	record, err = manager.Ensure(ctx, invocation(), "run-1", "incident-response", "respond", objective, snapshot, transition, workInputs("incident.json", strings.Repeat("e", 64)))
	if err != nil || record.Status != foregroundwork.StatusRequested || len(record.Events) < 4 || record.Events[len(record.Events)-2].Kind != "work.invalidated" {
		t.Fatalf("drift reset = %#v err=%v", record, err)
	}
}

func TestForegroundWorkSurvivesInvocationLocalRestartIdentity(t *testing.T) {
	// control-law: restart-local driver identity cannot invalidate otherwise-current work
	manager, snapshot, transition, _ := fixture(t)
	ctx := context.Background()
	objective := model.Objective{ID: "incident-1", TargetID: "mitigated", DeliveryID: "incident-1"}
	first, err := manager.Ensure(ctx, invocation(), "run-1", "incident-response", "respond", objective, snapshot, transition, workInputs("incident.json", strings.Repeat("e", 64)))
	if err != nil {
		t.Fatal(err)
	}
	restarted := snapshot
	restarted.Invocation.ControllerID = "controller-after-restart"
	restarted.Invocation.Host = "claude"
	restarted.Invocation.Correlation = "correlation-after-restart"
	restarted.Invocation.RuntimePath = "/different/immutable/runtime/location"
	restarted.Fingerprint = strings.Repeat("e", 64)
	second, err := manager.Ensure(ctx, invocation(), "run-1", "incident-response", "respond", objective, restarted, transition, workInputs("incident.json", strings.Repeat("e", 64)))
	if err != nil {
		t.Fatal(err)
	}
	if second.Request.Fingerprint != first.Request.Fingerprint || second.Revision != first.Revision || len(second.Events) != len(first.Events) {
		t.Fatalf("restart invalidated stable work: before=%#v after=%#v", first.Request, second.Request)
	}
	restarted.Invocation.Ref = "refs/heads/other"
	third, err := manager.Ensure(ctx, invocation(), "run-1", "incident-response", "respond", objective, restarted, transition, workInputs("incident.json", strings.Repeat("e", 64)))
	if err != nil {
		t.Fatal(err)
	}
	if third.Request.Fingerprint == first.Request.Fingerprint || third.Events[len(third.Events)-2].Kind != "work.invalidated" {
		t.Fatalf("repository context drift did not invalidate work: %#v", third)
	}
}

func TestForegroundWorkInputFingerprintInvalidatesOutputsAndStaging(t *testing.T) {
	// control-law: same-locator input changes cannot reuse foreground-work outputs
	manager, snapshot, transition, _ := fixture(t)
	ctx := context.Background()
	objective := model.Objective{ID: "incident-1", TargetID: "mitigated", DeliveryID: "incident-1"}
	first, err := manager.Ensure(ctx, invocation(), "run-1", "incident-response", "respond", objective, snapshot, transition, workInputs("incident.json", strings.Repeat("1", 64)))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first.Request.StagingRoot, "diagnosis.json"), []byte(`{"cause":"first input"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Complete(ctx, invocation(), "run-1", "diagnose"); err != nil {
		t.Fatal(err)
	}
	second, err := manager.Ensure(ctx, invocation(), "run-1", "incident-response", "respond", objective, snapshot, transition, workInputs("incident.json", strings.Repeat("2", 64)))
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != foregroundwork.StatusRequested || second.Request.Fingerprint == first.Request.Fingerprint || second.Request.StagingRoot == first.Request.StagingRoot {
		t.Fatalf("input change reused request identity or staging: first=%#v second=%#v", first.Request, second.Request)
	}
	if _, err := manager.Complete(ctx, invocation(), "run-1", "diagnose"); err == nil {
		t.Fatalf("invalidated output completed new request: %v", err)
	}
}

func TestForegroundWorkRejectsMissingInvalidAndEscapingOutputs(t *testing.T) {
	// control-law: only declared bounded regular staged outputs become work evidence
	manager, snapshot, transition, root := fixture(t)
	ctx := context.Background()
	objective := model.Objective{ID: "incident-1", TargetID: "mitigated", DeliveryID: "incident-1"}
	record, err := manager.Ensure(ctx, invocation(), "run-1", "incident-response", "respond", objective, snapshot, transition, workInputs("incident.json", strings.Repeat("e", 64)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Complete(ctx, invocation(), "run-1", "diagnose"); err == nil {
		t.Fatal("missing required output was accepted")
	}
	if err := os.WriteFile(filepath.Join(record.Request.StagingRoot, "diagnosis.json"), []byte(`{"cause":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Complete(ctx, invocation(), "run-1", "diagnose"); err == nil {
		t.Fatal("schema-invalid output was accepted")
	}
	outside := filepath.Join(root, "outside.json")
	if err := os.WriteFile(outside, []byte(`{"cause":"outside"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(record.Request.StagingRoot, "diagnosis.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(record.Request.StagingRoot, "diagnosis.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Complete(ctx, invocation(), "run-1", "diagnose"); err == nil {
		t.Fatal("symlink output was accepted")
	}
}

func TestForegroundWorkConcurrentCompletionIsSerialized(t *testing.T) {
	// control-law: one work request has at most one successful completion mutation
	manager, snapshot, transition, _ := fixture(t)
	ctx := context.Background()
	objective := model.Objective{ID: "incident-1", TargetID: "mitigated", DeliveryID: "incident-1"}
	record, err := manager.Ensure(ctx, invocation(), "run-1", "incident-response", "respond", objective, snapshot, transition, workInputs("incident.json", strings.Repeat("e", 64)))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(record.Request.StagingRoot, "diagnosis.json"), []byte(`{"cause":"overload"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, completeErr := manager.Complete(ctx, invocation(), "run-1", "diagnose")
			errors <- completeErr
		}()
	}
	wait.Wait()
	close(errors)
	successes := 0
	for err := range errors {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent completions succeeded %d times", successes)
	}
}

func TestForegroundWorkRejectsTamperedRuntimeRecord(t *testing.T) {
	// control-law: runtime work state must preserve typed status and event evidence
	manager, snapshot, transition, root := fixture(t)
	ctx := context.Background()
	objective := model.Objective{ID: "incident-1", TargetID: "mitigated", DeliveryID: "incident-1"}
	if _, err := manager.Ensure(ctx, invocation(), "run-1", "incident-response", "respond", objective, snapshot, transition, workInputs("incident.json", strings.Repeat("e", 64))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "flow", "work", "run-1", "diagnose", "record.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(raw), `"status": "requested"`, `"status": "completed"`, 1)
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Show(ctx, invocation(), "run-1", "diagnose"); err == nil {
		t.Fatal("tampered status was accepted")
	}
}
