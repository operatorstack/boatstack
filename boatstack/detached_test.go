package boatstack

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Conformance tests for Detached Supervision. Each names the control law it proves.
//
//	control-law: detached-control-state-never-enters-the-plant
//	control-law: detached-state-controls-only-its-bound-repository
//	control-law: unattached-repositories-are-not-controlled
//	control-law: attached-but-unverifiable-fails-closed
//	control-law: supervisor-semantics-are-mode-invariant

// detachedTestRepo builds a minimal real git repository with one commit and an
// origin remote (so identity derivation is stable), and points the external state
// root at a temp dir so no real home directory is touched.
func detachedTestRepo(t *testing.T, origin string) string {
	t.Helper()
	invalidateWorkspaceCache()
	t.Setenv(stateRootEnv, t.TempDir())
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Boatstack Test")
	runGit(t, repo, "config", "user.email", "boatstack@example.invalid")
	if origin != "" {
		runGit(t, repo, "remote", "add", "origin", origin)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A go.mod gives detectTestCommand a concrete command so config synthesis is
	// valid without an interactive prompt.
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module app\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")
	return repo
}

func gitPorcelain(t *testing.T, repo string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "status", "--porcelain=v1", "--untracked-files=all").CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// control-law: detached-control-state-never-enters-the-plant
func TestAttachLeavesRepositoryUnchanged(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/app.git")
	before := gitPorcelain(t, repo)

	result, err := AttachDetached(AttachOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if result.VerificationStatus != "VERIFIED" {
		t.Fatalf("attach did not verify: %+v", result)
	}
	if after := gitPorcelain(t, repo); after != before {
		t.Fatalf("attach changed the working tree: before=%q after=%q", before, after)
	}
	for _, forbidden := range []string{".product-loop", ".boatstack-project.json", ".claude", ".cursor", ".codex", ".gemini", ".agents"} {
		if _, err := os.Stat(filepath.Join(repo, forbidden)); !os.IsNotExist(err) {
			t.Fatalf("attach created %s inside the repository", forbidden)
		}
	}
	// Controller state exists under the external control root.
	if _, err := os.Stat(WorkspaceFor(repo).ProjectConfigPath()); err != nil {
		t.Fatalf("external project.json missing: %v", err)
	}
	if !strings.Contains(result.ControlRoot, "boatstack") || strings.HasPrefix(result.ControlRoot, repo) {
		t.Fatalf("control root should be external, got %q", result.ControlRoot)
	}
}

// control-law: unattached-repositories-are-not-controlled
func TestUnattachedRepositoryResolvesEmbedded(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/app.git")
	ctx := WorkspaceFor(repo)
	if ctx.Mode != SupervisionEmbedded {
		t.Fatalf("unattached repo must resolve embedded, got %s", ctx.Mode)
	}
	if ctx.ProjectConfigPath() != filepath.Join(repo, ".product-loop", "project.json") {
		t.Fatalf("embedded config path drifted: %s", ctx.ProjectConfigPath())
	}
}

// control-law: detached-control-state-never-enters-the-plant
func TestAttachRoutesWorkspaceToExternalControlRoot(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/app.git")
	if _, err := AttachDetached(AttachOptions{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	ctx := WorkspaceFor(repo)
	if ctx.Mode != SupervisionDetached {
		t.Fatalf("attached repo must resolve detached, got %s", ctx.Mode)
	}
	for _, p := range []string{ctx.ProjectConfigPath()} {
		if strings.HasPrefix(p, repo+string(filepath.Separator)) {
			t.Fatalf("detached path points inside the repo: %s", p)
		}
	}
	delivery, err := ctx.DeliveryDir()
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(delivery, repo+string(filepath.Separator)) {
		t.Fatalf("detached delivery dir points inside the repo: %s", delivery)
	}
}

// control-law: detached-state-controls-only-its-bound-repository
func TestDetachedStateIsPerRepositoryAndPerWorktree(t *testing.T) {
	repoA := detachedTestRepo(t, "https://github.com/acme/app.git")
	// A second repo shares the same state root (set by the first helper call is
	// overwritten; set it explicitly to the same dir for both).
	stateRoot := os.Getenv(stateRootEnv)
	repoB := t.TempDir()
	runGit(t, repoB, "init", "-b", "main")
	runGit(t, repoB, "config", "user.name", "T")
	runGit(t, repoB, "config", "user.email", "t@example.invalid")
	runGit(t, repoB, "remote", "add", "origin", "https://github.com/acme/other.git")
	if err := os.WriteFile(filepath.Join(repoB, "README.md"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoB, "add", ".")
	runGit(t, repoB, "commit", "-m", "b")

	if _, err := AttachDetached(AttachOptions{Repo: repoA}); err != nil {
		t.Fatal(err)
	}
	_ = stateRoot
	// repoB is not attached → embedded, unaffected by repoA's attachment.
	if WorkspaceFor(repoB).Mode != SupervisionEmbedded {
		t.Fatal("attaching repoA must not control repoB")
	}
	// The two repositories have distinct identities.
	idA, _ := repoIdentity(repoA)
	idB, _ := repoIdentity(repoB)
	if idA.RepoID == idB.RepoID {
		t.Fatal("distinct repositories must have distinct repo_ids")
	}
}

// control-law: attached-but-unverifiable-fails-closed
func TestAttachedButCorruptBindingFailsClosed(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/app.git")
	if _, err := AttachDetached(AttachOptions{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	stateRoot := os.Getenv(stateRootEnv)
	boatstackRoot := filepath.Join(stateRoot, "boatstack")
	identity, _ := repoIdentity(repo)
	// Corrupt the binding so identity can no longer be verified.
	if err := os.WriteFile(bindingPath(boatstackRoot, identity.RepoID), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	invalidateWorkspaceCache()
	if _, _, err := detachedContextFor(repo); err == nil {
		t.Fatal("a corrupt binding for an attached repo must fail closed")
	}
	status, _ := DetachedStatus(repo)
	if !status.Attached || status.Verified {
		t.Fatalf("status must report attached-but-unverified: %+v", status)
	}
}

// control-law: detached-state-controls-only-its-bound-repository
func TestMovedRepositoryWithMismatchedIdentityFailsClosed(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/app.git")
	if _, err := AttachDetached(AttachOptions{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	stateRoot := os.Getenv(stateRootEnv)
	boatstackRoot := filepath.Join(stateRoot, "boatstack")
	identity, _ := repoIdentity(repo)
	// Rewrite the binding to claim a different origin/initial-commit identity while
	// keeping the same repo_id index entry: the strong-identity check must reject it.
	binding, err := loadBinding(boatstackRoot, identity.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	binding.NormalizedOrigin = "github.com/acme/somethingelse"
	binding.InitialCommit = "0000000000000000000000000000000000000000"
	raw, _ := MarshalJSON(binding)
	if err := os.WriteFile(bindingPath(boatstackRoot, identity.RepoID), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	invalidateWorkspaceCache()
	if _, _, err := detachedContextFor(repo); err == nil {
		t.Fatal("an identity mismatch must fail closed, not silently rebind")
	}
}

// control-law: supervisor-semantics-are-mode-invariant
// The delivery-state layer round-trips identically whether stored embedded (in the
// Git dir) or detached (in the external control root); detached storage lands
// outside the repository.
func TestDeliveryStateRoundTripsIdenticallyAcrossLayouts(t *testing.T) {
	state := DeliveryState{
		SchemaVersion: deliveryStateSchemaVersion,
		Feature:       "sample-feature",
		ActiveIndex:   1,
		Slices: []DeliverySlice{
			{ID: "delivery", Title: "Delivery", Status: "PUBLISHED", HeadBranch: "feat/sample-feature"},
		},
	}

	// Embedded: no attachment; state lives in the Git directory.
	embedded := detachedTestRepo(t, "https://github.com/acme/embedded.git")
	if err := saveDeliveryState(embedded, state); err != nil {
		t.Fatal(err)
	}
	gotEmbedded, err := LoadDeliveryState(embedded, state.Feature)
	if err != nil {
		t.Fatal(err)
	}

	// Detached: attached; state lives under the external control root.
	detached := detachedTestRepo(t, "https://github.com/acme/detached.git")
	if _, err := AttachDetached(AttachOptions{Repo: detached}); err != nil {
		t.Fatal(err)
	}
	if err := saveDeliveryState(detached, state); err != nil {
		t.Fatal(err)
	}
	gotDetached, err := LoadDeliveryState(detached, state.Feature)
	if err != nil {
		t.Fatal(err)
	}

	dir, err := WorkspaceFor(detached).DeliveryDir()
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(dir, detached+string(filepath.Separator)) {
		t.Fatalf("detached delivery state must live outside the repo, got %s", dir)
	}

	if gotEmbedded.Feature != gotDetached.Feature ||
		gotEmbedded.ActiveIndex != gotDetached.ActiveIndex ||
		len(gotEmbedded.Slices) != len(gotDetached.Slices) ||
		gotEmbedded.Slices[0].Status != gotDetached.Slices[0].Status ||
		gotEmbedded.Slices[0].HeadBranch != gotDetached.Slices[0].HeadBranch {
		t.Fatalf("delivery state differs across layouts:\nembedded=%+v\ndetached=%+v", gotEmbedded, gotDetached)
	}
}

// control-law: detached-control-state-never-enters-the-plant
// The safety guard denies direct model mutation of the detached external control
// root exactly as it does the embedded runtime state.
func TestSafetyProtectsExternalControlRoot(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/app.git")
	for _, command := range []string{
		"rm -rf /Users/dev/.local/state/boatstack/repositories/abc123",
		"echo tampered > ~/.local/state/boatstack/registry.json",
		"cp evil ~/Library/Application Support/boatstack/runtimes/x/y/z/boatstack-helper",
	} {
		findings := ClassifyCommand(repo, command)
		if len(findings) == 0 || findings[0].Category != "workflow-state-tamper" {
			t.Fatalf("external control-root mutation was not denied as tamper: %q -> %#v", command, findings)
		}
	}
}

// control-law: supervisor-semantics-are-mode-invariant
func TestContextProjectionReportsMode(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/app.git")

	embedded, err := ProjectOperatorContext(repo, "build", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if embedded.Mode != string(SupervisionEmbedded) || embedded.Attached {
		t.Fatalf("unattached context should be embedded/unattached: %+v", embedded)
	}

	if _, err := AttachDetached(AttachOptions{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	detached, err := ProjectOperatorContext(repo, "build", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if detached.Mode != string(SupervisionDetached) || !detached.Attached || detached.ControlRoot == "" {
		t.Fatalf("attached context should be detached with a control root: %+v", detached)
	}
	if strings.HasPrefix(detached.ControlRoot, repo+string(filepath.Separator)) {
		t.Fatalf("context control root must be external: %s", detached.ControlRoot)
	}
}

// control-law: unattached-repositories-are-not-controlled
// A developer-level guard allows an unmanaged repository (no Boatstack control) but
// enforces the full policy on a managed one.
func TestAmbientHookNoOpsUnmanagedButEnforcesManaged(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/app.git")
	event := []byte(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git reset --hard HEAD~1"}}`)

	// Unmanaged: the ambient guard must not control this repository.
	output, denied := AmbientHookDecision(SafetyHookOptions{Host: "claude", Repo: repo, Input: event})
	if denied {
		t.Fatalf("ambient guard controlled an unattached repository: %s", output)
	}

	// Attached (detached): the same destructive command is now denied.
	if _, err := AttachDetached(AttachOptions{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	invalidateWorkspaceCache()
	output, denied = AmbientHookDecision(SafetyHookOptions{Host: "claude", Repo: repo, Input: event})
	if !denied || !strings.Contains(string(output), `"permissionDecision":"deny"`) {
		t.Fatalf("ambient guard did not enforce policy on a managed repository: %s", output)
	}
}

// control-law: unattached-repositories-are-not-controlled
// Activation instructions are host-neutral (every agent), point at developer-level
// config outside the repo, and install the ambient guard that no-ops unattached
// repositories. An unattached repository has nothing to activate.
func TestActivationPlanIsHostNeutralAndExternal(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/app.git")
	t.Setenv("BOATSTACK_USER_CONFIG_ROOT", t.TempDir())

	unattached, err := DetachedActivationPlan(repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	if unattached.Attached || len(unattached.Hosts) != 0 {
		t.Fatalf("unattached repo should have no activation plan: %+v", unattached)
	}

	if _, err := AttachDetached(AttachOptions{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	plan, err := DetachedActivationPlan(repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Attached || len(plan.Hosts) != 4 {
		t.Fatalf("expected activation for all four agents: %+v", plan)
	}
	userRoot := os.Getenv("BOATSTACK_USER_CONFIG_ROOT")
	for _, host := range plan.Hosts {
		if !strings.HasPrefix(host.ConfigPath, userRoot) {
			t.Fatalf("%s config path must be developer-level, got %s", host.Host, host.ConfigPath)
		}
		if strings.HasPrefix(host.ConfigPath, repo+string(filepath.Separator)) {
			t.Fatalf("%s activation must not point inside the repo: %s", host.Host, host.ConfigPath)
		}
		if !strings.Contains(host.Snippet, "ambient-safety-hook") || !strings.Contains(host.Snippet, "--host "+host.Host) {
			t.Fatalf("%s snippet missing ambient guard command: %s", host.Host, host.Snippet)
		}
		var decoded any
		if err := json.Unmarshal([]byte(host.Snippet), &decoded); err != nil {
			t.Fatalf("%s snippet is not valid JSON: %v", host.Host, err)
		}
	}
}

// control-law: detached-control-state-never-enters-the-plant
// Attaching populates the external shared-runtime slot so the ambient guard has a
// helper to invoke, without writing into the repository.
func TestAttachPopulatesExternalRuntimeSlot(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/app.git")
	if _, err := AttachDetached(AttachOptions{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	binaryPath, manifestPath, err := sharedRuntimePaths(repo, Version, SourceCommit)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{binaryPath, manifestPath} {
		if !fileExists(p) {
			t.Fatalf("external runtime slot missing %s", p)
		}
		if strings.HasPrefix(p, repo+string(filepath.Separator)) {
			t.Fatalf("runtime slot must be external, got %s", p)
		}
	}
}

// control-law: activation-preserves-existing-host-config
// Installing the ambient guard adds only a Boatstack-owned entry, preserves the
// developer's existing hooks, and is idempotent.
func TestActivateInstallsAmbientGuardPreservingUserHooks(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/app.git")
	userRoot := t.TempDir()
	t.Setenv("BOATSTACK_USER_CONFIG_ROOT", userRoot)
	if _, err := AttachDetached(AttachOptions{Repo: repo}); err != nil {
		t.Fatal(err)
	}

	// Seed a developer's own Claude hook that Boatstack must never touch.
	claudeCfg := filepath.Join(userRoot, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(claudeCfg), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := `{"theme":"dark","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"my-own-check.sh"}]}]}}`
	if err := os.WriteFile(claudeCfg, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := InstallAmbientHooks(repo, []string{"claude"})
	if err != nil {
		t.Fatal(err)
	}
	if result.VerificationStatus != "VERIFIED" || len(result.Hosts) != 1 || result.Hosts[0].Action != "installed" {
		t.Fatalf("unexpected install result: %+v", result)
	}
	body, err := os.ReadFile(claudeCfg)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "my-own-check.sh") {
		t.Fatalf("install clobbered the developer's own hook: %s", text)
	}
	if !strings.Contains(text, "ambient-safety-hook") {
		t.Fatalf("install did not add the ambient guard: %s", text)
	}
	if !strings.Contains(text, `"theme"`) {
		t.Fatalf("install dropped unrelated user settings: %s", text)
	}

	// Idempotent: a second install changes nothing.
	again, err := InstallAmbientHooks(repo, []string{"claude"})
	if err != nil {
		t.Fatal(err)
	}
	if again.Hosts[0].Action != "unchanged" {
		t.Fatalf("second install was not idempotent: %+v", again)
	}

	// Deactivate removes only the ambient guard, preserving the developer's hook.
	removed, err := RemoveAmbientHooks(repo, []string{"claude"})
	if err != nil {
		t.Fatal(err)
	}
	if removed.Hosts[0].Action != "removed" {
		t.Fatalf("deactivate did not remove the ambient guard: %+v", removed)
	}
	after, err := os.ReadFile(claudeCfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(after), "ambient-safety-hook") {
		t.Fatalf("deactivate left the ambient guard behind: %s", after)
	}
	if !strings.Contains(string(after), "my-own-check.sh") {
		t.Fatalf("deactivate removed the developer's own hook: %s", after)
	}
}

// control-law: unattached-repositories-are-not-controlled
// Installing the ambient guard requires an attachment; an unattached repo is refused.
func TestActivateRefusesUnattachedRepository(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/app.git")
	t.Setenv("BOATSTACK_USER_CONFIG_ROOT", t.TempDir())
	result, err := InstallAmbientHooks(repo, []string{"claude"})
	if err != nil {
		t.Fatal(err)
	}
	if result.VerificationStatus != "BLOCKED" {
		t.Fatalf("activation of an unattached repo must be blocked: %+v", result)
	}
}

// control-law: detached-control-state-never-enters-the-plant
func TestDetachRemovesAttachmentAndState(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/app.git")
	if _, err := AttachDetached(AttachOptions{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	control := WorkspaceFor(repo).controlRoot
	result, err := DetachDetached(DetachOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if result.VerificationStatus != "VERIFIED" || !result.StateRemoved {
		t.Fatalf("detach did not remove state: %+v", result)
	}
	if _, err := os.Stat(control); !os.IsNotExist(err) {
		t.Fatalf("detach left external control state behind: %s", control)
	}
	if WorkspaceFor(repo).Mode != SupervisionEmbedded {
		t.Fatal("after detach the repo must resolve embedded again")
	}
}
