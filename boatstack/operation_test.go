package boatstack

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func operationTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Boatstack Test")
	runGit(t, repo, "config", "user.email", "boatstack@example.invalid")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "base")
	return repo
}

func activeOperationTestRepo(t *testing.T) string {
	t.Helper()
	repo := safetyTestRepo(t)
	feature := "durable-tool"
	directory := filepath.Join(repo, ".product-loop", "features", feature)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(directory, "plan.lock.json")
	if err := os.WriteFile(lockPath, []byte("lock\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lockHash, err := SHA256File(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveDeliveryState(repo, DeliveryState{
		SchemaVersion: deliveryStateSchemaVersion, Feature: feature, PlanLockHash: lockHash,
		ActiveIndex: 0, Slices: []DeliverySlice{{ID: "delivery", Title: "Delivery", Status: "BUILD", BaseBranch: "main", HeadBranch: "main"}},
	}); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestHostHooksCreateAndCompleteOneDurableAttempt(t *testing.T) {
	fixtures := map[string]struct{ pre, post string }{
		"cursor": {
			pre:  `{"hook_event_name":"preToolUse","tool_name":"Write","tool_input":{"file_path":"feature.go","content":"package feature"}}`,
			post: `{"hook_event_name":"postToolUse","tool_name":"Write","tool_input":{"file_path":"feature.go","content":"package feature"},"tool_result":"ok"}`,
		},
		"claude": {
			pre:  `{"hook_event_name":"PreToolUse","tool_name":"Write","tool_input":{"file_path":"feature.go","content":"package feature"}}`,
			post: `{"hook_event_name":"PostToolUse","tool_name":"Write","tool_input":{"file_path":"feature.go","content":"package feature"},"tool_response":"ok"}`,
		},
		"codex": {
			pre:  `{"hook_event_name":"PreToolUse","tool_name":"Write","tool_input":{"file_path":"feature.go","content":"package feature"}}`,
			post: `{"hook_event_name":"PostToolUse","tool_name":"Write","tool_input":{"file_path":"feature.go","content":"package feature"},"tool_response":"ok"}`,
		},
		"gemini": {
			pre:  `{"hook_event_name":"BeforeTool","tool_name":"Write","tool_input":{"file_path":"feature.go","content":"package feature"}}`,
			post: `{"hook_event_name":"AfterTool","tool_name":"Write","tool_input":{"file_path":"feature.go","content":"package feature"},"tool_response":"ok"}`,
		},
	}
	for host, fixture := range fixtures {
		t.Run(host, func(t *testing.T) {
			repo := activeOperationTestRepo(t)
			if output, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: []byte(fixture.pre)}); denied {
				t.Fatalf("first supervised tool call was denied: %s", output)
			}
			if output, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: []byte(fixture.post)}); denied {
				t.Fatalf("completion event was denied: %s", output)
			}
			status, err := ResolveOperationStatus(repo, "")
			if err != nil || status.Operation != nil {
				t.Fatalf("completed operation remained active: %+v %v", status, err)
			}
			output, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: []byte(fixture.pre)})
			if !denied || !strings.Contains(string(output), "already") {
				t.Fatalf("late duplicate was not suppressed: %s", output)
			}
		})
	}
}

func TestAsyncCompletionCannotInitiateAnOperation(t *testing.T) {
	repo := activeOperationTestRepo(t)
	post := []byte(`{"hook_event_name":"PostToolUse","tool_name":"Write","tool_input":{"file_path":"never-started.go","content":"x"},"tool_response":"ok"}`)
	if output, denied := HookDecision(SafetyHookOptions{Host: "codex", Repo: repo, Input: post}); denied {
		t.Fatalf("unmatched completion should be consumed without authority: %s", output)
	}
	status, err := ResolveOperationStatus(repo, "")
	if err != nil || status.Operation != nil {
		t.Fatalf("async completion created workflow authority: %+v %v", status, err)
	}
}

func TestHostFailureEventsRequireReconciliation(t *testing.T) {
	fixtures := map[string]struct{ pre, post string }{
		"cursor": {
			pre:  `{"hook_event_name":"preToolUse","tool_call_id":"c-1","tool_name":"Write","tool_input":{"file_path":"failed.go","content":"x"}}`,
			post: `{"hook_event_name":"postToolUseFailure","tool_call_id":"c-1","tool_name":"Write","tool_input":{"file_path":"failed.go","content":"x"},"error":"write interrupted"}`,
		},
		"claude": {
			pre:  `{"hook_event_name":"PreToolUse","tool_use_id":"c-2","tool_name":"Write","tool_input":{"file_path":"failed.go","content":"x"}}`,
			post: `{"hook_event_name":"PostToolUseFailure","tool_use_id":"c-2","tool_name":"Write","tool_input":{"file_path":"failed.go","content":"x"},"error":"write interrupted"}`,
		},
		"codex": {
			pre:  `{"hook_event_name":"PreToolUse","tool_call_id":"c-3","tool_name":"Write","tool_input":{"file_path":"failed.go","content":"x"}}`,
			post: `{"hook_event_name":"PostToolUse","tool_call_id":"c-3","tool_name":"Write","tool_input":{"file_path":"failed.go","content":"x"},"tool_response":{"error":"write interrupted"}}`,
		},
		"gemini": {
			pre:  `{"hook_event_name":"BeforeTool","call_id":"c-4","tool_name":"Write","tool_input":{"file_path":"failed.go","content":"x"}}`,
			post: `{"hook_event_name":"AfterTool","call_id":"c-4","tool_name":"Write","tool_input":{"file_path":"failed.go","content":"x"},"tool_response":{"error":"write interrupted"}}`,
		},
	}
	for host, fixture := range fixtures {
		t.Run(host, func(t *testing.T) {
			repo := activeOperationTestRepo(t)
			if output, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: []byte(fixture.pre)}); denied {
				t.Fatalf("pre event denied: %s", output)
			}
			if output, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: []byte(fixture.post)}); denied {
				t.Fatalf("post-failure observation denied: %s", output)
			}
			status, err := ResolveOperationStatus(repo, "")
			if err != nil || status.Operation == nil || status.Operation.State != OperationReconcileRequired || !status.ReconciliationRequired {
				t.Fatalf("failure was not preserved as unknown: %+v %v", status, err)
			}
		})
	}
}

func TestSafetyFindingOperationFieldsRemainSecretFree(t *testing.T) {
	finding := SafetyFinding{Category: "operation-in-flight", OperationID: "abc", OperationState: "EXECUTING", AttemptNumber: 2, ReconciliationRequired: false}
	value, err := MarshalJSON(finding)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"operation_id": "abc"`, `"operation_state": "EXECUTING"`, `"attempt_number": 2`} {
		if !strings.Contains(string(value), expected) {
			t.Fatalf("missing safety field %s: %s", expected, value)
		}
	}
	if strings.Contains(string(value), "token") || strings.Contains(string(value), "command") {
		t.Fatalf("safety finding leaked execution detail: %s", value)
	}
}

func TestOperationObservationsRedactObviousSecrets(t *testing.T) {
	value := boundedObservation("request failed authorization=BearerValue token:abc123 password=hunter2 Bearer xyz")
	for _, secret := range []string{"BearerValue", "abc123", "hunter2", " xyz"} {
		if strings.Contains(value, secret) {
			t.Fatalf("operation observation retained %q: %s", secret, value)
		}
	}
}

func preparedOperation(t *testing.T, repo, fingerprint, retryClass string, attempts int) OperationReceipt {
	t.Helper()
	receipt, err := PrepareOperation(OperationPrepareOptions{
		Repo: repo, Kind: "test-write", Target: "artifact.json", PackageFingerprint: fingerprint,
		AuthorizationFingerprint: "approved-" + fingerprint, RetryClass: retryClass, MaxAttempts: attempts,
		ExpectedPostcondition: "artifact hash equals " + fingerprint,
	})
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

func TestOperationLifecycleAndReplayProtection(t *testing.T) {
	repo := operationTestRepo(t)
	receipt := preparedOperation(t, repo, "package-a", "ATOMIC_LOCAL", 2)
	if receipt.State != OperationAuthorized || receipt.Attempt != 0 {
		t.Fatalf("unexpected prepared receipt: %+v", receipt)
	}
	begin, err := BeginOperation(repo, receipt.OperationID, "host-call-1", "Write")
	if err != nil || begin.Receipt.State != OperationExecuting || begin.Receipt.Attempt != 1 || begin.LeaseToken == "" {
		t.Fatalf("unexpected begin: %+v %v", begin, err)
	}
	if _, err := BeginOperation(repo, receipt.OperationID, "host-call-1", "Write"); !errors.Is(err, ErrOperationInFlight) {
		t.Fatalf("identical active operation relaunched: %v", err)
	}
	if _, err := CompleteOperation(repo, receipt.OperationID, "wrong", "SUCCEEDED", "", ""); err == nil {
		t.Fatal("invalid lease completed the operation")
	}
	completed, err := CompleteOperation(repo, receipt.OperationID, begin.LeaseToken, "SUCCEEDED", "postcondition observed", "sha256:package-a")
	if err != nil || completed.State != OperationSucceeded || completed.Lease != nil {
		t.Fatalf("unexpected completion: %+v %v", completed, err)
	}
	resumed, err := BeginOperation(repo, receipt.OperationID, "late-notification", "Write")
	if err != nil || resumed.Receipt.State != OperationSucceeded || resumed.LeaseToken != "" {
		t.Fatalf("terminal identity did not suppress a late duplicate: %+v %v", resumed, err)
	}
}

func TestOperationRejectsChangedPackageAndAuthorization(t *testing.T) {
	repo := operationTestRepo(t)
	receipt := preparedOperation(t, repo, "package-auth", "ATOMIC_LOCAL", 2)
	if _, err := PrepareOperation(OperationPrepareOptions{
		Repo: repo, Kind: "test-write", Target: "artifact.json", PackageFingerprint: "package-auth",
		AuthorizationFingerprint: "different-approval", RetryClass: "ATOMIC_LOCAL", MaxAttempts: 2,
		ExpectedPostcondition: "artifact hash equals package-auth",
	}); err == nil || !strings.Contains(err.Error(), "authorization fingerprint changed") {
		t.Fatalf("changed authorization was not rejected: %v", err)
	}
	if _, err := AuthorizeOperation(repo, receipt.OperationID, "different-package", "approved-package-auth"); err == nil {
		t.Fatal("changed package fingerprint was not rejected")
	}
}

func TestUnknownCompletionRequiresReconciliationBeforeRetry(t *testing.T) {
	repo := operationTestRepo(t)
	receipt := preparedOperation(t, repo, "package-b", "RECONCILE_FIRST", 3)
	begin, err := BeginOperation(repo, receipt.OperationID, "call-b", "mcp__github__create_pull_request")
	if err != nil {
		t.Fatal(err)
	}
	unknown, err := CompleteOperation(repo, receipt.OperationID, begin.LeaseToken, "UNKNOWN", "transport ended before a response", "")
	if err != nil || unknown.State != OperationReconcileRequired {
		t.Fatalf("unknown completion did not require reconciliation: %+v %v", unknown, err)
	}
	if _, err := BeginOperation(repo, receipt.OperationID, "call-b-retry", "mcp__github__create_pull_request"); err == nil {
		t.Fatal("reconcile-first operation retried blindly")
	}
	retryable, err := RecordOperationReconciliation(repo, receipt.OperationID, "OBSERVED_ABSENT", "exact PR was not found", "head:abc")
	if err != nil || retryable.State != OperationRetryable {
		t.Fatalf("absence did not permit bounded retry: %+v %v", retryable, err)
	}
	second, err := BeginOperation(repo, receipt.OperationID, "call-b-retry", "mcp__github__create_pull_request")
	if err != nil || second.Receipt.Attempt != 2 {
		t.Fatalf("reconciled operation did not retry: %+v %v", second, err)
	}
}

func TestExpiredLeaseBecomesUnknownAndBudgetPersists(t *testing.T) {
	repo := operationTestRepo(t)
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	previous := operationNow
	operationNow = func() time.Time { return now }
	t.Cleanup(func() { operationNow = previous })
	receipt := preparedOperation(t, repo, "package-c", "ATOMIC_LOCAL", 1)
	if _, err := BeginOperation(repo, receipt.OperationID, "call-c", "Write"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(operationLeaseDuration + time.Second)
	if _, err := BeginOperation(repo, receipt.OperationID, "call-c", "Write"); err == nil {
		t.Fatal("expired attempt did not block for reconciliation")
	}
	status, err := ResolveOperationStatus(repo, receipt.OperationID)
	if err != nil || status.Operation == nil || status.Operation.State != OperationReconcileRequired || !status.ReconciliationRequired {
		t.Fatalf("unexpected expired status: %+v %v", status, err)
	}
	terminal, err := RecordOperationReconciliation(repo, receipt.OperationID, "OBSERVED_ABSENT", "destination hash unchanged", "")
	if err != nil || terminal.State != OperationFailedFinal {
		t.Fatalf("persisted budget was not exhausted: %+v %v", terminal, err)
	}
}

func TestOperationReceiptsAreSharedAcrossLinkedWorktreesAndSerialized(t *testing.T) {
	repo := operationTestRepo(t)
	linked := filepath.Join(t.TempDir(), "linked")
	runGit(t, repo, "worktree", "add", "-b", "linked-test", linked)
	receipt := preparedOperation(t, repo, "package-d", "ATOMIC_LOCAL", 2)
	status, err := ResolveOperationStatus(linked, receipt.OperationID)
	if err != nil || status.Operation == nil || status.Operation.OperationID != receipt.OperationID {
		t.Fatalf("linked worktree did not observe shared operation: %+v %v", status, err)
	}

	var wait sync.WaitGroup
	errorsSeen := make(chan error, 2)
	for _, root := range []string{repo, linked} {
		wait.Add(1)
		go func(path string) {
			defer wait.Done()
			_, beginErr := BeginOperation(path, receipt.OperationID, "same-attempt", "Write")
			errorsSeen <- beginErr
		}(root)
	}
	wait.Wait()
	close(errorsSeen)
	successes, inFlight := 0, 0
	for beginErr := range errorsSeen {
		switch {
		case beginErr == nil:
			successes++
		case errors.Is(beginErr, ErrOperationInFlight):
			inFlight++
		default:
			t.Fatalf("unexpected concurrent begin error: %v", beginErr)
		}
	}
	if successes != 1 || inFlight != 1 {
		t.Fatalf("operation lock admitted %d executions and %d in-flight reports", successes, inFlight)
	}
}

func TestOperationStatusDoesNotChooseAmbiguousWorkByRecency(t *testing.T) {
	repo := operationTestRepo(t)
	preparedOperation(t, repo, "one", "ATOMIC_LOCAL", 1)
	preparedOperation(t, repo, "two", "ATOMIC_LOCAL", 1)
	status, err := ResolveOperationStatus(repo, "")
	if err != nil || status.VerificationStatus != "AMBIGUOUS" || status.NextOperation != "specify_operation" {
		t.Fatalf("unexpected ambiguity result: %+v %v", status, err)
	}
}
