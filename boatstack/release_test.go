package boatstack

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSyncWorkflowUsesCurrentUpstreamLabPath(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Join("..", ".github", "workflows", "sync-upstream.yml"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(workflow)
	current := "labs/12-product-engineering-loop"
	if count := strings.Count(content, current); count != 2 {
		t.Fatalf("sync workflow contains %q %d times, want 2", current, count)
	}
	retired := "examples/12-product-engineering-loop"
	if strings.Contains(content, retired) {
		t.Fatalf("sync workflow still references retired upstream path %q", retired)
	}
}

func TestClassifyReleasePaths(t *testing.T) {
	documentation := []string{
		"README.md", "docs/getting-started.md", "assets/boatstack-mark.svg",
		"release-notes/2026-07-18-copy.md", "UPSTREAM.json",
		"boatstack/export_test.go", "boatstack/testdata/example.txt",
		".github/workflows/sync-upstream.yml", "automation/release-policy.md",
	}
	if got := ClassifyReleasePaths(documentation); got.Required || len(got.Paths) != 0 {
		t.Fatalf("documentation-only projection requested a release: %#v", got)
	}

	runtime := append(documentation,
		"boatstack/safety.go", "boatstack/SKILL.md", "install.sh", "new-runtime-path",
	)
	want := []string{"boatstack/SKILL.md", "boatstack/safety.go", "install.sh", "new-runtime-path"}
	got := ClassifyReleasePaths(runtime)
	if !got.Required || !reflect.DeepEqual(got.Paths, want) {
		t.Fatalf("runtime classification = %#v, want %#v", got, want)
	}
}

func TestClassifyReleaseDiff(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Boatstack Test")
	runGit(t, repo, "config", "user.email", "boatstack@example.invalid")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "base")
	base := runGit(t, repo, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "docs")
	docsHead := runGit(t, repo, "rev-parse", "HEAD")
	if got, err := ClassifyReleaseDiff(repo, base, docsHead); err != nil || got.Required {
		t.Fatalf("documentation diff = %#v, %v", got, err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "boatstack"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "boatstack", "runtime.go"), []byte("package boatstack\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "runtime")
	runtimeHead := runGit(t, repo, "rev-parse", "HEAD")
	if got, err := ClassifyReleaseDiff(repo, docsHead, runtimeHead); err != nil || !got.Required {
		t.Fatalf("runtime diff = %#v, %v", got, err)
	}
}

func TestNextPatchVersion(t *testing.T) {
	if got, err := NextPatchVersion("v0.7.0"); err != nil || got != "v0.7.1" {
		t.Fatalf("next patch = %q, %v", got, err)
	}
	if _, err := NextPatchVersion("latest"); err == nil {
		t.Fatal("invalid release version was accepted")
	}
}
