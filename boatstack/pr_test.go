package boatstack

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func runGit(t *testing.T, repo string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, arguments...)...)
	value, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(arguments, " "), err, value)
	}
	return strings.TrimSpace(string(value))
}

func prTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Boatstack Test")
	runGit(t, repo, "config", "user.email", "boatstack@example.invalid")
	config := testConfig()
	config.Project.DefaultBranch = "main"
	config.Project.Context = []string{"README.md"}
	config.Project.HighRiskPaths = []string{"feature.go"}
	value, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".product-loop"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".product-loop", "project.json"), value, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# Fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "base")
	remote := filepath.Join(t.TempDir(), "origin.git")
	if output, err := exec.Command("git", "init", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v: %s", err, output)
	}
	runGit(t, repo, "remote", "add", "origin", remote)
	runGit(t, repo, "push", "--set-upstream", "origin", "main")
	runGit(t, repo, "switch", "-c", "feat/reviewer-ready")
	if err := os.WriteFile(filepath.Join(repo, "feature.go"), []byte("package fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "feature.go")
	runGit(t, repo, "commit", "-m", "add reviewer-visible behavior")
	return repo
}

func quoted(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func previewDocument(context PRContext, title, body string) string {
	return strings.Join([]string{
		"---",
		"boatstack_pr_version: 2",
		"title: " + quoted(title),
		"mode: " + quoted(context.Mode),
		"feature: " + quoted(context.Feature),
		"slice: " + quoted(context.SliceID),
		"base: " + quoted(context.BaseBranch),
		"head: " + quoted(context.HeadBranch),
		"context_fingerprint: " + quoted(context.ContextFingerprint),
		"---",
		strings.TrimSpace(body),
		"",
	}, "\n")
}

func fixturePRBody(t *testing.T) string {
	t.Helper()
	value, err := os.ReadFile(filepath.Join("testdata", "reviewer-pr-body.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(value)
}

func writePreview(t *testing.T, repo string, context PRContext, title, body string) string {
	t.Helper()
	path := filepath.Join(repo, filepath.FromSlash(context.PreviewPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(previewDocument(context, title, body)), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAdHocPRContextAndPreviewAreEvidenceLimited(t *testing.T) {
	repo := prTestRepo(t)
	context, err := PreparePRContext(PRContextOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if context.Mode != "ad-hoc" || context.Feature != "" {
		t.Fatalf("unexpected ad-hoc context: %#v", context)
	}
	if context.PreviewPath != ".product-loop/pr-briefs/feat-reviewer-ready/pr.md" {
		t.Fatalf("unexpected preview path: %s", context.PreviewPath)
	}
	if len(context.Sources) != 1 || context.Sources[0].Kind != "project_config" || len(context.GateStatus) != 0 {
		t.Fatal("ad-hoc context must not manufacture managed provenance")
	}
	if context.DiffStat == "" || len(context.HighRiskFiles) != 1 || context.HighRiskFiles[0] != "feature.go" || len(context.ContextPaths) != 1 {
		t.Fatalf("ad-hoc context did not project review boundaries: %#v", context)
	}
	previewPath := writePreview(t, repo, context, "Make hooks and privacy fallback predictable", fixturePRBody(t))
	preview, checked, err := CheckPRPreview(repo, previewPath)
	if err != nil {
		t.Fatal(err)
	}
	if checked.ContextFingerprint != context.ContextFingerprint {
		t.Fatal("checked context fingerprint changed")
	}
	if strings.Contains(string(PRBody(preview)), "boatstack_pr_version") || !strings.Contains(string(PRBody(preview)), "## Security and privacy") {
		t.Fatal("rendered PR body must exclude frontmatter and preserve adaptive sections")
	}
	if !strings.Contains(preview.Body, "NOT_VERIFIED") {
		t.Fatal("ad-hoc fixture must expose unavailable evidence")
	}
	runGit(t, repo, "add", context.PreviewPath)
	runGit(t, repo, "commit", "-m", "record reviewer-ready PR preview")
	if _, _, err := CheckPRPreview(repo, previewPath); err != nil {
		t.Fatalf("committing only pr.md must not invalidate its own product-diff fingerprint: %v", err)
	}
}

func activateManagedFeature(t *testing.T, repo, feature string) string {
	t.Helper()
	directory := filepath.Join(repo, ".product-loop", "features", feature)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	plan := validPlan()
	plan["feature_id"] = feature
	plan["spec_path"] = "feature-spec.md"
	if err := os.WriteFile(filepath.Join(directory, "source-plan.md"), []byte("# Host plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "feature-spec.md"), []byte("# Feature spec\n\nDeliver reviewer-ready output.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeMarkdownPlan(t, filepath.Join(directory, "plan.md"), plan, true)
	check, err := CheckPlan(filepath.Join(directory, "plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	writeApprovalReceipt(t, filepath.Join(directory, "approval.md"), check.Fingerprint)
	if err := ActivatePlan(ActivationOptions{
		PlanPath: filepath.Join(directory, "plan.md"), ApprovalPath: filepath.Join(directory, "approval.md"),
		OutDir: filepath.Join(directory, "compiled"), OutputPath: filepath.Join(directory, "plan.lock.json"),
		SourceCommit: runGit(t, repo, "rev-parse", "HEAD"),
	}); err != nil {
		t.Fatal(err)
	}
	for _, artifact := range []struct{ name, body string }{
		{"questions.md", "# Questions\n\nAll material decisions answered.\n"},
		{"gaps.md", "# Gaps\n\nNo material ship-blocking gaps.\n"},
		{"test-plan.md", "# Test plan\n\nRun the approved contract check.\n"},
	} {
		if err := os.WriteFile(filepath.Join(directory, artifact.name), []byte(artifact.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	evidence := `# Evidence ledger

- Test gate: ` + "`PASS`" + `
- Review gate: ` + "`PASS_WITH_GAPS`" + `
- Ship gate: ` + "`BLOCKED`" + `

## Acceptance evidence

| Criterion | Tasks | Result | Evidence |
|---|---|---|---|
| AC-1 | T-1 | ` + "`PASS`" + ` | Contract assertions passed |

## Commands and checks

The project checks passed.

## Review findings

No blocking findings.

## Known gaps

One non-critical portability gap is owned.

## Rollout and rollback

No migration; revert the feature commit.
`
	if err := os.WriteFile(filepath.Join(directory, "evidence.md"), []byte(evidence), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".product-loop/features/"+feature)
	runGit(t, repo, "commit", "-m", "record approved feature evidence")
	for _, gate := range []string{"test", "review"} {
		status := "PASS"
		if gate == "review" {
			status = "PASS_WITH_GAPS"
		}
		if _, err := RecordDeliveryGate(DeliveryGateOptions{
			Repo: repo, Feature: feature, SliceID: "delivery", Gate: gate,
			Status: status, EvidencePath: filepath.Join(directory, "evidence.md"),
		}); err != nil {
			t.Fatalf("record %s delivery gate: %v", gate, err)
		}
	}
	return directory
}

func managedPRBody() string {
	return `## Why this change

Reviewers need a concise, evidence-backed view of the approved outcome.

## What changed

| Area | Before | After | Reviewer focus |
|---|---|---|---|
| PR preparation | Generic placeholder | Reviewer-ready evidence projection | Evidence remains traceable |

## Review order

1. Review the evidence boundary, then the rendered output.

## Evidence

| Claim | Evidence | Result | Source |
|---|---|---|---|
| Approved behavior is implemented | Contract assertions passed | ` + "`PASS`" + ` | [Evidence ledger](.product-loop/features/reviewer-ready/evidence.md) |
| Review found no blocking issue | Independent diff review | ` + "`PASS_WITH_GAPS`" + ` | [Evidence ledger](.product-loop/features/reviewer-ready/evidence.md) |

## Operational safety

Repository safety scan passed. Destructive recovery remains operator-only.

## Known gaps and risks

One non-critical portability gap remains recorded with an owner.

## Rollout and rollback

No migration is required; revert the feature commit to roll back.

<details>
<summary>Boatstack provenance</summary>

- Mode: managed
- Approval and gates: current
- Coding-host attribution: recorded when known

</details>
`
}

func TestManagedPRBlocksCommittedIrreversibleCapability(t *testing.T) {
	repo := prTestRepo(t)
	activateManagedFeature(t, repo, "reviewer-ready")
	path := filepath.Join(repo, "scripts", "recover.sql")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("DROP SCHEMA public CASCADE;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "scripts/recover.sql")
	runGit(t, repo, "commit", "-m", "add unsafe recovery")
	if _, err := PreparePRContext(PRContextOptions{Repo: repo, Feature: "reviewer-ready"}); err == nil || !strings.Contains(err.Error(), "irreversible capability") {
		t.Fatalf("managed PR did not block committed destructive code: %v", err)
	}
}

func TestManagedPRRequiresCurrentApprovalLockAndGateEvidence(t *testing.T) {
	repo := prTestRepo(t)
	directory := activateManagedFeature(t, repo, "reviewer-ready")
	context, err := PreparePRContext(PRContextOptions{Repo: repo, Feature: "reviewer-ready"})
	if err != nil {
		t.Fatal(err)
	}
	if context.Mode != "managed" || context.GateStatus["test"] != "PASS" || context.GateStatus["review"] != "PASS_WITH_GAPS" {
		t.Fatalf("unexpected managed context: %#v", context)
	}
	previewPath := writePreview(t, repo, context, "Generate evidence-backed PR reviews", managedPRBody())
	preview, _, err := CheckPRPreview(repo, previewPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PublishPR(PRPublishOptions{Repo: repo, PreviewPath: previewPath, ExpectedFingerprint: preview.Fingerprint, Action: "open"}); err == nil || !strings.Contains(err.Error(), "commit the exact reviewed pr.md") {
		t.Fatalf("expected uncommitted preview to block publication, got %v", err)
	}
	runGit(t, repo, "add", context.PreviewPath)
	runGit(t, repo, "commit", "-m", "record PR preview")
	preview, _, err = CheckPRPreview(repo, previewPath)
	if err != nil {
		t.Fatal(err)
	}
	unsupportedSource := strings.Replace(managedPRBody(), "[Evidence ledger](.product-loop/features/reviewer-ready/evidence.md)", "Unlinked summary", 1)
	if err := os.WriteFile(previewPath, []byte(previewDocument(context, preview.Title, unsupportedSource)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CheckPRPreview(repo, previewPath); err == nil || !strings.Contains(err.Error(), "link the current evidence ledger") {
		t.Fatalf("expected untraceable managed evidence to block, got %v", err)
	}

	unsafeBody := strings.Replace(managedPRBody(), "`PASS_WITH_GAPS`", "`NOT_VERIFIED`", 1)
	if err := os.WriteFile(previewPath, []byte(previewDocument(context, preview.Title, unsafeBody)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParsePRPreview(previewPath); err == nil || !strings.Contains(err.Error(), "managed PR evidence") {
		t.Fatalf("expected unsupported managed claim to block, got %v", err)
	}
	if err := os.WriteFile(previewPath, []byte(previewDocument(context, preview.Title, managedPRBody())), 0o644); err != nil {
		t.Fatal(err)
	}

	evidencePath := filepath.Join(directory, "evidence.md")
	evidence, _ := os.ReadFile(evidencePath)
	if err := os.WriteFile(evidencePath, append(evidence, []byte("\nAdditional runtime evidence.\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", filepath.ToSlash(filepath.Join(".product-loop", "features", "reviewer-ready", "evidence.md")))
	runGit(t, repo, "commit", "-m", "add runtime evidence")
	if _, _, err := CheckPRPreview(repo, previewPath); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected evidence drift to invalidate preview, got %v", err)
	}
}

func TestPRPreviewRejectsMissingSectionsMalformedRowsAndStaleDiff(t *testing.T) {
	repo := prTestRepo(t)
	context, err := PreparePRContext(PRContextOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	body := fixturePRBody(t)
	previewPath := writePreview(t, repo, context, "Reviewer-ready change", body)

	missing := strings.Replace(body, "## Review order", "## Reading notes", 1)
	if err := os.WriteFile(previewPath, []byte(previewDocument(context, "Reviewer-ready change", missing)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParsePRPreview(previewPath); err == nil || !strings.Contains(err.Error(), "Review order") {
		t.Fatalf("expected missing section failure, got %v", err)
	}

	malformed := strings.Replace(body, "| Hook runs without an activated environment | Repository hook smoke procedure | `NOT_VERIFIED` | Current branch test notes |", "| incomplete | row |", 1)
	if err := os.WriteFile(previewPath, []byte(previewDocument(context, "Reviewer-ready change", malformed)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParsePRPreview(previewPath); err == nil || !strings.Contains(err.Error(), "Evidence rows") {
		t.Fatalf("expected malformed evidence row failure, got %v", err)
	}

	if err := os.WriteFile(previewPath, []byte(previewDocument(context, "Reviewer-ready change", body)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "feature.go"), []byte("package fixture\n\nconst Changed = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CheckPRPreview(repo, previewPath); err == nil || !strings.Contains(err.Error(), "working-tree changes") {
		t.Fatalf("expected uncommitted product drift to block, got %v", err)
	}
}

func TestPublishPRRequiresExactConfirmationAndUsesBodyWithoutFrontmatter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake gh fixture uses a POSIX shell; publication behavior is covered by cross-platform pure-Go checks")
	}
	repo := prTestRepo(t)
	context, err := PreparePRContext(PRContextOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	previewPath := writePreview(t, repo, context, "Make hooks and privacy fallback predictable", fixturePRBody(t))
	preview, _, err := CheckPRPreview(repo, previewPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PublishPR(PRPublishOptions{Repo: repo, PreviewPath: previewPath, ExpectedFingerprint: preview.Fingerprint, Action: "open"}); err == nil || !strings.Contains(err.Error(), "commit the exact reviewed pr.md") {
		t.Fatalf("expected uncommitted preview to block publication, got %v", err)
	}
	runGit(t, repo, "add", context.PreviewPath)
	runGit(t, repo, "commit", "-m", "record exact PR preview")
	preview, _, err = CheckPRPreview(repo, previewPath)
	if err != nil {
		t.Fatal(err)
	}
	fakeDir := t.TempDir()
	capture := filepath.Join(fakeDir, "body.md")
	script := filepath.Join(fakeDir, "gh")
	scriptBody := `#!/bin/sh
if [ "$1" = "auth" ]; then exit 0; fi
if [ "$1" = "pr" ] && [ "$2" = "view" ]; then
  if [ "$BOATSTACK_GH_ERROR" = "1" ]; then echo "network timeout" >&2; exit 1; fi
  if [ "$BOATSTACK_EXISTING_PR" = "1" ]; then echo "https://github.com/example/repo/pull/7"; exit 0; fi
  echo "no pull requests found for branch" >&2
  exit 1
fi
if [ "$1" = "pr" ] && [ "$2" = "create" ]; then
  while [ "$#" -gt 0 ]; do
    if [ "$1" = "--body-file" ]; then shift; cp "$1" "$BOATSTACK_BODY_CAPTURE"; fi
    shift
  done
  echo "https://github.com/example/repo/pull/8"
  exit 0
fi
if [ "$1" = "pr" ] && [ "$2" = "edit" ]; then
  while [ "$#" -gt 0 ]; do
    if [ "$1" = "--body-file" ]; then shift; cp "$1" "$BOATSTACK_BODY_CAPTURE"; fi
    shift
  done
  exit 0
fi
exit 1
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BOATSTACK_BODY_CAPTURE", capture)
	t.Setenv("BOATSTACK_GH_ERROR", "1")
	if action, _, err := RecommendedPRAction(repo); action != "manual" || err == nil || !strings.Contains(err.Error(), "cannot determine") {
		t.Fatalf("expected GitHub lookup failure to remain distinct from no PR, got action=%s err=%v", action, err)
	}
	t.Setenv("BOATSTACK_GH_ERROR", "")
	if _, err := PublishPR(PRPublishOptions{Repo: repo, PreviewPath: previewPath, ExpectedFingerprint: "wrong", Action: "open"}); err == nil || !strings.Contains(err.Error(), "confirmed") {
		t.Fatalf("expected exact confirmation fingerprint, got %v", err)
	}
	url, err := PublishPR(PRPublishOptions{Repo: repo, PreviewPath: previewPath, ExpectedFingerprint: preview.Fingerprint, Action: "open"})
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://github.com/example/repo/pull/8" {
		t.Fatalf("unexpected created PR URL: %s", url)
	}
	publishedBody, _ := os.ReadFile(capture)
	if strings.Contains(string(publishedBody), "boatstack_pr_version") || !strings.Contains(string(publishedBody), "## Why this change") {
		t.Fatal("publisher did not strip preview frontmatter")
	}

	t.Setenv("BOATSTACK_EXISTING_PR", "1")
	if _, err := PublishPR(PRPublishOptions{Repo: repo, PreviewPath: previewPath, ExpectedFingerprint: preview.Fingerprint, Action: "open"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected open/update mismatch to block, got %v", err)
	}
	url, err = PublishPR(PRPublishOptions{Repo: repo, PreviewPath: previewPath, ExpectedFingerprint: preview.Fingerprint, Action: "update"})
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://github.com/example/repo/pull/7" {
		t.Fatalf("unexpected updated PR URL: %s", url)
	}
}
