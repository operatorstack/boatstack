package boatstack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func embeddedFeatureForDetach(t *testing.T, repo, feature string, approvalFingerprint string) string {
	t.Helper()
	config := testConfig()
	config.Workflow.HumanPlanApproval = false
	// Host activation files remain repository-owned. They are orthogonal to the
	// detached generated-state invariant exercised by these fixtures.
	config.Adapters = nil
	raw, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, sourceConfigName), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, productLoopDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, productLoopDirName, "project.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "plans"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "plans", "source.md"), []byte("# Durable source plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(repo, productLoopDirName, "features", feature)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	plan := validPlan()
	plan["feature_id"] = feature
	plan["source_plan_path"] = "../../../plans/source.md"
	writeMarkdownPlan(t, filepath.Join(directory, "plan.md"), plan, true)
	if err := os.WriteFile(filepath.Join(directory, "spec.md"), []byte("# Accepted specification\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if approvalFingerprint != "" {
		writeApprovalReceipt(t, filepath.Join(directory, "approval.md"), approvalFingerprint)
	}
	return directory
}

func TestDetachedOpenFeatureCandidatesIgnoreHistoricalPackagesOnMain(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/history.git")
	selected := detachedOpenFeatureCandidates(repo, []string{"old-one", "old-two"})
	if len(selected) != 0 {
		t.Fatalf("historical packages were selected on main: %v", selected)
	}
}

// control-law: detached-generated-state-has-one-resolved-owner
func TestDetachedAttachImportsFeatureAndIgnoresEmbeddedDrift(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/import.git")
	source := embeddedFeatureForDetach(t, repo, "feature-one", "")
	result, err := AttachDetached(AttachOptions{Repo: repo})
	if err != nil || result.VerificationStatus != "VERIFIED" {
		t.Fatalf("attach: %+v %v", result, err)
	}
	if len(result.FeatureMigrations) != 1 || result.FeatureMigrations[0].Status != "IMPORTED" {
		t.Fatalf("migration result: %+v", result.FeatureMigrations)
	}
	target := WorkspaceFor(repo).FeatureDir("feature-one")
	if strings.HasPrefix(target, repo+string(filepath.Separator)) || !fileExists(filepath.Join(target, "plan.md")) {
		t.Fatalf("feature was not imported outside the repository: %s", target)
	}
	if err := os.WriteFile(filepath.Join(source, "embedded-only.md"), []byte("ignored\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, productLoopDirName, "project.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Doctor(repo); err != nil {
		t.Fatalf("doctor did not validate the complete detached controller without consulting embedded drift: %v", err)
	}
	status, err := ResolveNext(repo, "")
	if err != nil || status.Feature != "feature-one" || status.ObservedStage != "POLICY_READY" {
		t.Fatalf("next did not use detached feature package: %+v %v", status, err)
	}
}

// control-law: detached-import-never-chooses-by-recency
func TestDetachedReattachBlocksConflictingFeaturePackages(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/conflict.git")
	source := embeddedFeatureForDetach(t, repo, "feature-one", "")
	first, _ := AttachDetached(AttachOptions{Repo: repo})
	if first.VerificationStatus != "VERIFIED" {
		t.Fatalf("first attach: %+v", first)
	}
	identical, identicalErr := AttachDetached(AttachOptions{Repo: repo, Force: true})
	if identicalErr != nil || identical.VerificationStatus != "VERIFIED" || len(identical.FeatureMigrations) != 1 || identical.FeatureMigrations[0].Status != "UNCHANGED" {
		t.Fatalf("identical packages were not preserved: %+v %v", identical, identicalErr)
	}
	if err := os.WriteFile(filepath.Join(source, "questions.md"), []byte("# changed later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := AttachDetached(AttachOptions{Repo: repo, Force: true})
	if err != nil || second.VerificationStatus != "BLOCKED" || len(second.FeatureMigrations) != 1 || second.FeatureMigrations[0].Status != "CONFLICTING" {
		t.Fatalf("conflict was not fail-closed: %+v %v", second, err)
	}
}

// control-law: detached-import-requires-current-receipt-fingerprints
func TestDetachedAttachRejectsStaleApprovalReceipt(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/stale.git")
	embeddedFeatureForDetach(t, repo, "feature-one", "wrong")
	result, err := AttachDetached(AttachOptions{Repo: repo})
	if err != nil || result.VerificationStatus != "BLOCKED" || len(result.FeatureMigrations) != 1 || result.FeatureMigrations[0].Status != "REJECTED" {
		t.Fatalf("stale receipt was imported: %+v %v", result, err)
	}
}

// control-law: detached-import-is-atomic-before-directory-promotion
func TestDetachedImportInterruptionLeavesNoPartialTarget(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/interrupted.git")
	embeddedFeatureForDetach(t, repo, "feature-one", "")
	old := detachedImportBeforeRename
	detachedImportBeforeRename = func(_, _, _ string) error { return fmt.Errorf("injected interruption") }
	t.Cleanup(func() { detachedImportBeforeRename = old })
	result, err := AttachDetached(AttachOptions{Repo: repo})
	if err != nil || result.VerificationStatus != "BLOCKED" {
		t.Fatalf("interrupted attach: %+v %v", result, err)
	}
	identity, _ := repoIdentity(repo)
	ctx := detachedContextFromIdentity(filepath.Join(os.Getenv(stateRootEnv), "boatstack"), identity)
	if fileExists(ctx.FeatureDir("feature-one")) {
		t.Fatal("interrupted import exposed a partial feature directory")
	}
}

// control-law: detached-activation-writes-and-verifies-one-feature-root
func TestDetachedActivationUsesCanonicalFeatureDirectory(t *testing.T) {
	repo := detachedTestRepo(t, "https://github.com/acme/activate.git")
	embeddedFeatureForDetach(t, repo, "feature-one", "")
	result, _ := AttachDetached(AttachOptions{Repo: repo})
	if result.VerificationStatus != "VERIFIED" {
		t.Fatalf("attach: %+v", result)
	}
	directory := WorkspaceFor(repo).FeatureDir("feature-one")
	if resolved, resolveErr := ResolveControllerRepository(directory); resolveErr != nil || canonicalizeExistingAncestor(resolved) != canonicalizeExistingAncestor(repo) {
		t.Fatalf("detached feature owner mismatch: directory=%s resolved=%s repo=%s err=%v", directory, resolved, repo, resolveErr)
	}
	resolved, _ := ResolveControllerRepository(directory)
	if ctx, ctxErr := ResolveWorkspaceContext(resolved); ctxErr != nil || ctx.Mode != SupervisionDetached {
		t.Fatalf("resolved owner lost detached context: resolved=%s ctx=%+v err=%v", resolved, ctx, ctxErr)
	}
	err := ActivatePlan(ActivationOptions{
		PlanPath: filepath.Join(directory, "plan.md"), OutDir: filepath.Join(directory, "compiled"),
		OutputPath: filepath.Join(directory, "plan.lock.json"), SourceCommit: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"compiled/tasks.json", "compiled/journey-oracles.json", "plan.lock.json"} {
		if !fileExists(filepath.Join(directory, filepath.FromSlash(path))) {
			t.Errorf("missing detached activation artifact %s", path)
		}
		if fileExists(filepath.Join(repo, productLoopDirName, "features", "feature-one", filepath.FromSlash(path))) {
			t.Errorf("activation artifact leaked into embedded package: %s", path)
		}
	}
}
