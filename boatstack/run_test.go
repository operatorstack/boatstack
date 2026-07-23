package boatstack

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func runTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.name", "Boatstack Test"},
		{"config", "user.email", "boatstack@example.test"},
	} {
		if output, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "README.md"}, {"commit", "-m", "initial"}} {
		if output, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
		}
	}
	configDirectory := filepath.Join(repo, ".product-loop")
	if err := os.MkdirAll(configDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	config := `{"schema_version":1,"project":{"name":"test","default_branch":"main","commands":{"test":"go test ./..."}},"workflow":{"human_plan_approval":true,"independent_review_for_high_risk":true,"allow_pass_with_gaps":false},"adapters":["cursor"]}`
	if err := os.WriteFile(filepath.Join(configDirectory, "project.json"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

func withRunGit(t *testing.T, responses map[string]struct {
	value string
	err   error
}) {
	t.Helper()
	old := runGitCommand
	runGitCommand = func(_ string, arguments ...string) (string, error) {
		key := strings.Join(arguments, " ")
		if response, ok := responses[key]; ok {
			return response.value, response.err
		}
		return "", fmt.Errorf("unexpected git command: %s", key)
	}
	t.Cleanup(func() { runGitCommand = old })
}

func writeRunConfig(t *testing.T, repo string, ignored ...string) {
	t.Helper()
	config := testConfig()
	config.Project.DefaultBranch = "main"
	config.Adapters = []string{"cursor"}
	config.Workflow.IgnoredDeliveries = ignored
	value, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".product-loop", "project.json"), value, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunBranchesIgnoredActiveDeliveryClearsAmbiguity(t *testing.T) {
	repo := runTestRepo(t)
	writeNextDelivery(t, repo, "first", "BUILD", 0)
	writeNextDelivery(t, repo, "second", "BUILD", 0)
	writeRunConfig(t, repo, "first")
	withRunGit(t, map[string]struct {
		value string
		err   error
	}{"branch --show-current": {value: "feature"}})

	if _, _, err := runBranches(repo, ""); err != nil {
		t.Fatalf("ignored active delivery should clear run ambiguity: %v", err)
	}
}

func TestRunBranchesNewUnignoredActiveDeliveryStillBlocks(t *testing.T) {
	repo := runTestRepo(t)
	writeNextDelivery(t, repo, "first", "BUILD", 0)
	writeNextDelivery(t, repo, "second", "BUILD", 0)
	writeRunConfig(t, repo, "unrelated")
	withRunGit(t, map[string]struct {
		value string
		err   error
	}{"branch --show-current": {value: "feature"}})

	_, _, err := runBranches(repo, "")
	if err == nil || !strings.Contains(err.Error(), "more than one managed delivery is active") {
		t.Fatalf("un-ignored ambiguous deliveries should still block run: %v", err)
	}
}

func TestCheckRunPreflightRequiresOriginBeforeMutation(t *testing.T) {
	repo := runTestRepo(t)
	before, err := os.ReadFile(filepath.Join(repo, ".product-loop", "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	status := CheckRunPreflight(repo, "")
	after, err := os.ReadFile(filepath.Join(repo, ".product-loop", "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	if status.VerificationStatus != "BLOCKED" || status.Relation != "MISSING_ORIGIN" {
		t.Fatalf("unexpected preflight: %+v", status)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("blocked preflight changed Boatstack state")
	}
}

func TestCheckRunPreflightFetchesAndAcceptsFreshUnpublishedBranch(t *testing.T) {
	repo := runTestRepo(t)
	remote := filepath.Join(t.TempDir(), "origin.git")
	if output, err := exec.Command("git", "init", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v: %s", err, output)
	}
	for _, args := range [][]string{
		{"remote", "add", "origin", remote},
		{"push", "-u", "origin", "main"},
		{"switch", "-c", "feature"},
	} {
		if output, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
		}
	}
	status := CheckRunPreflight(repo, "")
	if status.VerificationStatus != "VERIFIED" || status.Relation != "UNPUBLISHED" || status.BaseBranch != "main" || status.HeadBranch != "feature" {
		t.Fatalf("unexpected preflight: %+v", status)
	}
}

func TestCheckRunPreflightBlocksFetchAndFreshnessFailures(t *testing.T) {
	tests := []struct {
		name      string
		responses map[string]struct {
			value string
			err   error
		}
		relation string
	}{
		{
			name: "fetch failure",
			responses: map[string]struct {
				value string
				err   error
			}{
				"remote get-url origin": {value: "git@example.test/repo.git"},
				"fetch origin":          {err: fmt.Errorf("authentication failed")},
			},
			relation: "FETCH_FAILED",
		},
		{
			name: "stale base",
			responses: map[string]struct {
				value string
				err   error
			}{
				"remote get-url origin": {value: "git@example.test/repo.git"},
				"fetch origin":          {},
				"branch --show-current": {value: "feature"},
				"rev-parse --verify refs/remotes/origin/main^{commit}":   {value: "abc"},
				"merge-base --is-ancestor refs/remotes/origin/main HEAD": {err: fmt.Errorf("not ancestor")},
			},
			relation: "STALE_BASE",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := runTestRepo(t)
			withRunGit(t, test.responses)
			status := CheckRunPreflight(repo, "")
			if status.VerificationStatus != "BLOCKED" || status.Relation != test.relation {
				t.Fatalf("unexpected preflight: %+v", status)
			}
		})
	}
}

func TestCheckRunPreflightClassifiesUpstreamRelations(t *testing.T) {
	for _, test := range []struct {
		counts, relation, verification string
	}{
		{counts: "0 0", relation: "CURRENT", verification: "VERIFIED"},
		{counts: "2 0", relation: "AHEAD", verification: "VERIFIED"},
		{counts: "0 1", relation: "BEHIND", verification: "BLOCKED"},
		{counts: "2 1", relation: "DIVERGED", verification: "BLOCKED"},
	} {
		t.Run(test.relation, func(t *testing.T) {
			repo := runTestRepo(t)
			withRunGit(t, map[string]struct {
				value string
				err   error
			}{
				"remote get-url origin": {value: "git@example.test/repo.git"},
				"fetch origin":          {},
				"branch --show-current": {value: "feature"},
				"rev-parse --verify refs/remotes/origin/main^{commit}":    {value: "abc"},
				"merge-base --is-ancestor refs/remotes/origin/main HEAD":  {},
				"rev-parse --abbrev-ref --symbolic-full-name @{upstream}": {value: "origin/feature"},
				"rev-list --left-right --count HEAD...@{upstream}":        {value: test.counts},
			})
			status := CheckRunPreflight(repo, "")
			if status.VerificationStatus != test.verification || status.Relation != test.relation {
				t.Fatalf("unexpected preflight: %+v", status)
			}
		})
	}
}

func TestCheckRunPreflightBlocksConstrainedDeliveryBranchMismatch(t *testing.T) {
	repo := runTestRepo(t)
	directory := filepath.Join(repo, ".product-loop", "features", "bounded-run")
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
		SchemaVersion: deliveryStateSchemaVersion,
		Feature:       "bounded-run",
		PlanLockHash:  lockHash,
		ActiveIndex:   0,
		Slices: []DeliverySlice{{
			ID: "delivery", Title: "Delivery", Status: "BUILD",
			BaseBranch: "main", HeadBranch: "expected-feature",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	withRunGit(t, map[string]struct {
		value string
		err   error
	}{
		"remote get-url origin": {value: "git@example.test/repo.git"},
		"fetch origin":          {},
		"branch --show-current": {value: "wrong-feature"},
	})
	status := CheckRunPreflight(repo, "")
	if status.VerificationStatus != "BLOCKED" || status.Relation != "BRANCH_MISMATCH" || !strings.Contains(status.Reason, "expected-feature") {
		t.Fatalf("unexpected preflight: %+v", status)
	}
}
