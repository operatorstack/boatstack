package boatstack

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// control-law: uncertain-workspace-identity-or-lifecycle-preserves-resource
func TestDetachedUnpublishedCutRemainsActiveAndActivatesOnlyFromLinkedAlias(t *testing.T) {
	repo := detachedTestRepo(t, "")
	embeddedFeatureForDetach(t, repo, "feature-one", "")
	config := testConfig()
	config.Project.DefaultBranch = "main"
	config.Workflow.HumanPlanApproval = false
	config.Workspace = defaultWorkspace()
	config.Adapters = nil
	raw, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, sourceConfigName), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, productLoopDirName, "project.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	workspaceGitDo(t, repo, "add", sourceConfigName, "plans/source.md")
	workspaceGitDo(t, repo, "commit", "-m", "record detached workspace inputs")
	addWorkspaceOrigin(t, repo)

	attached, err := AttachDetached(AttachOptions{Repo: repo})
	if err != nil || attached.VerificationStatus != "VERIFIED" {
		t.Fatalf("attach: %+v (%v)", attached, err)
	}
	cut, err := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "feature-one"})
	if err != nil || cut.VerificationStatus != "VERIFIED" || cut.DestinationRepo == repo {
		t.Fatalf("cut: %+v (%v)", cut, err)
	}
	withWorkspaceGh(t, ghUnavailable())

	status, err := FeatureWorkspaceStatus(repo, cut.Branch)
	if err != nil {
		t.Fatal(err)
	}
	if status.Merged || status.CleanupDue || status.MergeSource != "unpublished" {
		t.Fatalf("unpublished cut collapsed into completion: %+v", status)
	}
	cleanup, err := CleanupFeatureWorkspace(WorkspaceCleanupOptions{Repo: repo, Branch: cut.Branch, Confirm: true})
	if err != nil || cleanup.VerificationStatus != "BLOCKED" {
		t.Fatalf("unpublished cleanup must block: %+v (%v)", cleanup, err)
	}
	reap, err := ReapWorkspaces(WorkspaceReapOptions{Repo: repo, Confirm: true})
	if err != nil || reap.ReapedCount != 0 || !dirExists(cut.WorktreePath) {
		t.Fatalf("reap removed unpublished workspace: %+v (%v)", reap, err)
	}

	directory := WorkspaceFor(cut.DestinationRepo).FeatureDir("feature-one")
	planPath := filepath.Join(directory, "plan.md")
	lockPath := filepath.Join(directory, "plan.lock.json")
	compiled := filepath.Join(directory, "compiled")
	if _, err := ResolveControllerRepository(directory); err == nil || !strings.Contains(err.Error(), "multiple verified repository aliases") {
		t.Fatalf("ambiguous controller inverse selected an alias: %v", err)
	}
	mainOptions := ActivationOptions{Repo: repo, PlanPath: planPath, OutDir: compiled, OutputPath: lockPath, SourceCommit: "test"}
	if err := ActivatePlan(mainOptions); err == nil || !strings.Contains(err.Error(), "cut worktree") {
		t.Fatalf("main activation did not refuse with the worktree guard: %v", err)
	}
	if fileExists(lockPath) {
		t.Fatal("refused main activation wrote a plan lock")
	}
	linkedOptions := mainOptions
	linkedOptions.Repo = cut.DestinationRepo
	if err := ActivatePlan(linkedOptions); err != nil {
		t.Fatalf("linked detached alias did not activate: %v", err)
	}
	if !fileExists(lockPath) {
		t.Fatal("linked activation did not write the plan lock")
	}
}

// control-law: cleanup-policy-never-substitutes-for-lifecycle-evidence
func TestCleanupAfterShipStillRequiresCompletedPublication(t *testing.T) {
	policy := defaultWorkspace()
	policy.CleanupAfter = "ship"
	repo := workspaceRepo(t, policy)
	cut, err := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "ship-later"})
	if err != nil || cut.VerificationStatus != "VERIFIED" {
		t.Fatalf("cut: %+v (%v)", cut, err)
	}
	withWorkspaceGh(t, ghUnavailable())
	status, err := FeatureWorkspaceStatus(repo, cut.Branch)
	if err != nil || status.CleanupDue || status.Merged {
		t.Fatalf("ship policy treated an unpublished cut as terminal: %+v (%v)", status, err)
	}
	cleanup, err := CleanupFeatureWorkspace(WorkspaceCleanupOptions{Repo: repo, Branch: cut.Branch, Confirm: true})
	if err != nil || cleanup.VerificationStatus != "BLOCKED" || !dirExists(cut.WorktreePath) {
		t.Fatalf("ship cleanup removed unpublished work: %+v (%v)", cleanup, err)
	}
	if count := CountReclaimableWorkspaces(repo); count != 0 {
		t.Fatalf("ship reap exposed %d unpublished workspace(s)", count)
	}
}

// control-law: active-managed-delivery-outranks-branch-pr-projections
func TestPartialManagedDeliveryRemainsActiveEvenWhenGitHubReportsMerged(t *testing.T) {
	repo := workspaceRepo(t, defaultWorkspace())
	cut, err := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "partial"})
	if err != nil || cut.VerificationStatus != "VERIFIED" {
		t.Fatalf("cut: %+v (%v)", cut, err)
	}
	directory := WorkspaceFor(cut.WorktreePath).FeatureDir("partial")
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
	if err := saveDeliveryState(cut.WorktreePath, DeliveryState{
		SchemaVersion: deliveryStateSchemaVersion, Feature: "partial", PlanLockHash: lockHash, ActiveIndex: 1,
		Slices: []DeliverySlice{
			{ID: "first", Title: "First", Status: StatusPublished, HeadBranch: cut.Branch, PRURL: "https://example.invalid/pr/1", PRState: "MERGED"},
			{ID: "second", Title: "Second", Status: StatusBuild, HeadBranch: cut.Branch},
		},
	}); err != nil {
		t.Fatal(err)
	}
	withWorkspaceGh(t, ghState("MERGED"))
	status, err := FeatureWorkspaceStatus(repo, cut.Branch)
	if err != nil || status.Merged || status.CleanupDue || status.MergeSource != "delivery" {
		t.Fatalf("partial delivery collapsed into completion: %+v (%v)", status, err)
	}
}

// control-law: closed-unmerged-publication-is-preservation-only
func TestClosedUnmergedWorkspaceRequiresAttention(t *testing.T) {
	policy := defaultWorkspace()
	policy.CleanupAfter = "ship"
	repo := workspaceRepo(t, policy)
	cut, err := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "closed"})
	if err != nil || cut.VerificationStatus != "VERIFIED" {
		t.Fatalf("cut: %+v (%v)", cut, err)
	}
	withWorkspaceGh(t, ghState("CLOSED"))
	status, err := FeatureWorkspaceStatus(repo, cut.Branch)
	if err != nil || status.Merged || status.CleanupDue || status.MergeSource != "gh" {
		t.Fatalf("closed-unmerged workspace became cleanup-ready: %+v (%v)", status, err)
	}
}

// control-law: managed-terminal-goal-controls-lifecycle-completion
func TestMergedTerminalKeepsPublishedManagedDeliveryActive(t *testing.T) {
	policy := defaultWorkspace()
	policy.CleanupAfter = "ship"
	repo := workspaceRepo(t, policy)
	config, _, err := LoadConfig(WorkspaceFor(repo).ProjectConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	config.Delivery = &DeliveryPolicy{Terminal: string(TerminalMerged)}
	raw, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(WorkspaceFor(repo).ProjectConfigPath(), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	cut, err := CutFeatureWorkspace(WorkspaceCutOptions{Repo: repo, Feature: "merged-goal"})
	if err != nil || cut.VerificationStatus != "VERIFIED" {
		t.Fatalf("cut: %+v (%v)", cut, err)
	}
	if err := os.WriteFile(filepath.Join(cut.WorktreePath, "managed-change.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspaceGitDo(t, cut.WorktreePath, "add", "managed-change.txt")
	workspaceGitDo(t, cut.WorktreePath, "commit", "-m", "managed change")
	writeCompletedDelivery(t, cut.WorktreePath, "merged-goal", cut.Branch)
	state, err := CurrentDeliveryState(cut.WorktreePath, "merged-goal")
	if err != nil {
		t.Fatal(err)
	}
	state.Goal = string(TerminalMerged)
	state.Slices[0].PRState = "OPEN"
	if err := saveDeliveryState(cut.WorktreePath, state); err != nil {
		t.Fatal(err)
	}
	withWorkspaceGh(t, ghState("MERGED"))
	status, err := FeatureWorkspaceStatus(repo, cut.Branch)
	if err != nil || status.Merged || status.CleanupDue || status.MergeSource != "delivery" {
		t.Fatalf("published slice escaped the configured merged terminal: %+v (%v)", status, err)
	}
}

func directCallers(t *testing.T, filename, callee string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	callers := []string{}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		found := false
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			identifier, ok := call.Fun.(*ast.Ident)
			if ok && identifier.Name == callee {
				found = true
			}
			return true
		})
		if found {
			callers = append(callers, function.Name.Name)
		}
	}
	sort.Strings(callers)
	return callers
}

func requireCallerInventory(t *testing.T, filename, callee string, expected []string) {
	t.Helper()
	actual := directCallers(t, filename, callee)
	sort.Strings(expected)
	if strings.Join(actual, "\x00") != strings.Join(expected, "\x00") {
		t.Fatalf("%s callers changed without authority review: got %v want %v", callee, actual, expected)
	}
}

func repositoryCallers(t *testing.T, callee string) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	callers := []string{}
	for _, filename := range files {
		if strings.HasSuffix(filename, "_test.go") {
			continue
		}
		for _, caller := range directCallers(t, filename, callee) {
			callers = append(callers, filename+":"+caller)
		}
	}
	sort.Strings(callers)
	return callers
}

func requireRepositoryCallerInventory(t *testing.T, callee string, expected []string) {
	t.Helper()
	actual := repositoryCallers(t, callee)
	sort.Strings(expected)
	if strings.Join(actual, "\x00") != strings.Join(expected, "\x00") {
		t.Fatalf("%s repository callers changed without authority review: got %v want %v", callee, actual, expected)
	}
}

// control-law: workspace-authority-boundaries-cover-every-static-consumer
func TestWorkspaceAuthoritySurfaceInventory(t *testing.T) {
	requireCallerInventory(t, "workspace.go", "assessWorkspaceLifecycle", []string{
		"CleanupFeatureWorkspace", "FeatureWorkspaceStatus", "reclaimableScan", "workspaceMergeStatus",
	})
	requireCallerInventory(t, "workspace.go", "workspaceBranchLanded", []string{"managedWorkspaceLifecycle"})
	requireCallerInventory(t, "plan.go", "ResolveControllerRepositoryFor", []string{
		"ActivatePlan", "CheckPlanForRepository", "CheckApprovalLock", "checkApprovalSourcePlan",
	})
	requireCallerInventory(t, "planning.go", "ResolveControllerRepositoryFor", []string{
		"PlanningBaselineForRepository", "RecordApproval",
	})
	requireCallerInventory(t, "readiness.go", "ResolveControllerRepositoryFor", []string{"CheckPlanReadinessForRepository"})
	requireRepositoryCallerInventory(t, "ResolveControllerRepositoryFor", []string{
		"plan.go:ActivatePlan", "plan.go:CheckApprovalLock", "plan.go:CheckPlanForRepository", "plan.go:checkApprovalSourcePlan",
		"planning.go:PlanningBaselineForRepository", "planning.go:RecordApproval",
		"readiness.go:CheckPlanReadinessForRepository",
	})
	requireRepositoryCallerInventory(t, "ResolveControllerRepository", []string{
		"plan.go:CheckApprovalLock", "plan.go:CheckPlan", "plan.go:checkApprovalReceipt", "plan.go:compilePlanFiles", "plan.go:sourcePlanForStructuredPlan",
		"planning.go:PlanningBaselineForPlan", "readiness.go:CheckPlanReadiness",
	})
}
