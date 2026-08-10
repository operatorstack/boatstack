package boatstack

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func destructiveHookEvent(host string) []byte {
	switch host {
	case "cursor":
		return []byte(`{"hook_event_name":"beforeShellExecution","command":"git reset --hard HEAD~1"}`)
	case "gemini":
		return []byte(`{"hook_event_name":"BeforeTool","tool_name":"run_shell_command","tool_input":{"command":"git reset --hard HEAD~1"}}`)
	default:
		return []byte(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git reset --hard HEAD~1"}}`)
	}
}

func activeEngagementFixture(t *testing.T, repo, feature string) DeliveryState {
	t.Helper()
	branch := strings.TrimSpace(gitOutput(repo, "branch", "--show-current"))
	lockPath := filepath.Join(WorkspaceFor(repo).FeatureDir(feature), "plan.lock.json")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte("active engagement lock\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lockHash, err := SHA256File(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	state := DeliveryState{
		SchemaVersion: deliveryStateSchemaVersion, Feature: feature, PlanLockHash: lockHash,
		ActiveIndex: 0, Mode: "NORMAL", RepairCounters: map[string]int{},
		Slices: []DeliverySlice{{ID: "delivery", Status: StatusBuild, BaseBranch: "main", HeadBranch: branch}},
	}
	if err := saveDeliveryState(repo, state); err != nil {
		t.Fatal(err)
	}
	if err := syncEngagementLease(repo, state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestDormantRepositoryHasZeroBoatstackPolicyEffects(t *testing.T) {
	repo := safetyTestRepo(t)
	draftPlan := writeValidSavedFeaturePlan(t, repo, "saved-draft")
	draftDir := filepath.Dir(draftPlan)
	if err := os.WriteFile(filepath.Join(draftDir, "plan.approval.json"), []byte(`{"approved":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(draftDir, "plan.lock.json"), []byte(`{"schema_version":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeInvalidDelivery(t, repo, "stale-delivery")
	writePublishedDelivery(t, repo, "published-work", "OPEN")

	status := ResolveEngagement(repo, EngagementRequest{})
	if status.Mode != EngagementDormant {
		t.Fatalf("repository evidence activated Boatstack: %+v", status)
	}
	for _, host := range []string{"cursor", "claude", "codex", "gemini"} {
		output, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: destructiveHookEvent(host)})
		if denied {
			t.Fatalf("%s dormant hook denied repository administration: %s", host, output)
		}
		if len(output) != 0 {
			t.Fatalf("%s dormant hook emitted output: %s", host, output)
		}
	}
	write := []byte(`{"hook_event_name":"PreToolUse","tool_name":"Write","tool_input":{"file_path":".git/boatstack/deliveries/demo/state.json","content":"{}"}}`)
	if output, denied := HookDecision(SafetyHookOptions{Host: "codex", Repo: repo, Input: write}); denied {
		t.Fatalf("dormant raw managed-state write was controlled: %s", output)
	}
	for _, command := range []string{
		"git clean -fd", "git branch -D old-work", "git worktree remove /tmp/old-worktree",
		"git reset --hard HEAD", "gcloud sql instances delete fixture",
	} {
		event := []byte(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":` + strconv.Quote(command) + `}}`)
		if output, denied := HookDecision(SafetyHookOptions{Host: "codex", Repo: repo, Input: event}); denied {
			t.Fatalf("dormant operation %q was controlled: %s", command, output)
		}
	}
	for host, event := range map[string][]byte{
		"cursor": []byte(`{"hook_event_name":"beforeMCPExecution","tool_name":"mcp__db__execute_sql","tool_input":{"query":"DROP TABLE fixture"}}`),
		"claude": []byte(`{"hook_event_name":"PreToolUse","tool_name":"mcp__database__query","tool_input":{"query":"DELETE FROM fixture"}}`),
		"codex":  []byte(`{"hook_event_name":"PreToolUse","tool_name":"mcp__database__query","tool_input":{"query":"DELETE FROM fixture"}}`),
		"gemini": []byte(`{"hook_event_name":"BeforeTool","tool_name":"mcp__database__query","tool_input":{"query":"DELETE FROM fixture"}}`),
	} {
		if output, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: event}); denied {
			t.Fatalf("%s dormant MCP mutation was controlled: %s", host, output)
		}
	}
}

func TestExplicitCommandIsEphemeral(t *testing.T) {
	repo := safetyTestRepo(t)
	command := ResolveEngagement(repo, EngagementRequest{ExplicitCommand: true})
	if command.Mode != EngagementCommand {
		t.Fatalf("explicit command did not receive command scope: %+v", command)
	}
	if ambient := ResolveEngagement(repo, EngagementRequest{}); ambient.Mode != EngagementDormant {
		t.Fatalf("command scope leaked into later ambient work: %+v", ambient)
	}
}

func TestActiveLeaseSurvivesResolverRestartAndScopesEveryHost(t *testing.T) {
	repo := safetyTestRepo(t)
	activeEngagementFixture(t, repo, "active-feature")
	for attempt := 0; attempt < 2; attempt++ {
		status := ResolveEngagement(repo, EngagementRequest{})
		if status.Mode != EngagementActive || status.Feature != "active-feature" || status.Slice != "delivery" {
			t.Fatalf("active engagement was not durable: %+v", status)
		}
	}
	for _, host := range []string{"cursor", "claude", "codex", "gemini"} {
		if output, denied := HookDecision(SafetyHookOptions{Host: host, Repo: repo, Input: destructiveHookEvent(host)}); !denied {
			t.Fatalf("%s active hook allowed destructive operation: %s", host, output)
		}
	}
}

func TestWrongBranchAndPublishedDeliveryAreDormant(t *testing.T) {
	repo := safetyTestRepo(t)
	state := activeEngagementFixture(t, repo, "scoped-feature")
	runGit(t, repo, "switch", "-c", "unrelated")
	if status := ResolveEngagement(repo, EngagementRequest{}); status.Mode != EngagementDormant {
		t.Fatalf("cross-branch delivery remained engaged: %+v", status)
	}
	runGit(t, repo, "switch", "main")
	state.Slices[0].Status = StatusPublished
	state.Slices[0].PRState = "OPEN"
	state.ActiveIndex = 1
	if err := saveDeliveryState(repo, state); err != nil {
		t.Fatal(err)
	}
	if err := syncEngagementLease(repo, state); err != nil {
		t.Fatal(err)
	}
	if status := ResolveEngagement(repo, EngagementRequest{}); status.Mode != EngagementDormant {
		t.Fatalf("published delivery retained ambient authority: %+v", status)
	}
	if path, err := engagementLeasePath(repo); err != nil {
		t.Fatal(err)
	} else if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("publication did not release the engagement lease: %v", err)
	}
}

func TestActiveLeaseDoesNotCrossWorktrees(t *testing.T) {
	repo := safetyTestRepo(t)
	activeEngagementFixture(t, repo, "worktree-feature")
	linked := filepath.Join(t.TempDir(), "linked")
	runGit(t, repo, "worktree", "add", "-b", "unrelated-worktree", linked)
	if status := ResolveEngagement(linked, EngagementRequest{}); status.Mode != EngagementDormant {
		t.Fatalf("active engagement crossed into another worktree: %+v", status)
	}
	if output, denied := HookDecision(SafetyHookOptions{Host: "codex", Repo: linked, Input: destructiveHookEvent("codex")}); denied {
		t.Fatalf("unrelated worktree was controlled: %s", output)
	}
}

func TestMalformedOrSymlinkedLeaseIsDormant(t *testing.T) {
	repo := safetyTestRepo(t)
	path, err := engagementLeasePath(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if status := ResolveEngagement(repo, EngagementRequest{}); status.Mode != EngagementDormant {
		t.Fatalf("malformed lease acquired authority: %+v", status)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "lease.json")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if status := ResolveEngagement(repo, EngagementRequest{}); status.Mode != EngagementDormant {
		t.Fatalf("symlinked lease acquired authority: %+v", status)
	}
}

func TestDormantGuardExitsBeforeHydration(t *testing.T) {
	repo := safetyTestRepo(t)
	guard := filepath.Join(repo, "guard.sh")
	if err := os.WriteFile(guard, guardShellScript(), 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(t.TempDir(), "hydrated")
	command := exec.Command("bash", guard, "codex")
	command.Dir = repo
	command.Env = append(os.Environ(), "BOATSTACK_HYDRATE_COMMAND=touch "+sentinel)
	command.Stdin = strings.NewReader(string(destructiveHookEvent("codex")) + "\n")
	output, err := command.CombinedOutput()
	if err != nil || len(output) != 0 {
		t.Fatalf("dormant guard was not silent: err=%v output=%s", err, output)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("dormant guard attempted runtime hydration")
	}
}

func TestEngagementStatusJSONShape(t *testing.T) {
	repo := safetyTestRepo(t)
	value, err := json.Marshal(ResolveEngagement(repo, EngagementRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(value), `"mode":"DORMANT"`) {
		t.Fatalf("engagement status omitted mode: %s", value)
	}
}
