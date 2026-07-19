package boatstack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func changelogConfig() ProjectConfig {
	config := testConfig()
	config.Workflow.MaintainChangelog = true
	return config
}

func writeChangelog(t *testing.T, repo, value string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, changelogPath), []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestChangelogPolicyAcceptsExistingAndFirstEntries(t *testing.T) {
	for _, test := range []struct {
		name string
		base string
		head string
	}{
		{
			name: "existing changelog",
			base: "# Changelog\n\n## Unreleased\n\n### Added\n\n- Existing capability.\n\n## 1.0.0\n\n- First release.\n",
			head: "# Changelog\n\n## Unreleased\n\n### Added\n\n- Existing capability.\n- New reader-visible capability.\n\n## 1.0.0\n\n- First release.\n",
		},
		{
			name: "first changelog",
			head: "# Changelog\n\n## Unreleased\n\n### Maintenance\n\n- Document the supported delivery workflow.\n",
		},
		{
			name: "existing legacy changelog adopts policy",
			base: "# Changes\n\n## 1.0.0\n\n- First release.\n",
			head: "# Changes\n\n## Unreleased\n\n### Changed\n\n- Adopt readable unreleased entries.\n\n## 1.0.0\n\n- First release.\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := t.TempDir()
			runGit(t, repo, "init", "-b", "main")
			runGit(t, repo, "config", "user.name", "Boatstack Test")
			runGit(t, repo, "config", "user.email", "boatstack@example.invalid")
			if test.base != "" {
				writeChangelog(t, repo, test.base)
			} else if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# Fixture\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			runGit(t, repo, "add", ".")
			runGit(t, repo, "commit", "-m", "base")
			base := runGit(t, repo, "rev-parse", "HEAD")
			writeChangelog(t, repo, test.head)
			if err := validateChangelogChange(repo, base, changelogConfig()); err != nil {
				t.Fatalf("valid changelog rejected: %v", err)
			}
		})
	}
}

func TestChangelogPolicyRejectsMissingMalformedAndHistoricalOnlyChanges(t *testing.T) {
	base := "# Changelog\n\n## Unreleased\n\n### Added\n\n- Existing capability.\n\n## 1.0.0\n\n- First release.\n"
	tests := []struct {
		name string
		head string
		want string
	}{
		{name: "no new entry", head: base, want: "new categorized entry"},
		{name: "historical only", head: strings.Replace(base, "- First release.", "- First release.\n- Rewritten history.", 1), want: "new categorized entry"},
		{name: "entry outside category", head: "# Changelog\n\n## Unreleased\n\n- Missing category.\n", want: "allowed category"},
		{name: "empty entry", head: "# Changelog\n\n## Unreleased\n\n### Fixed\n\n- \n", want: "empty"},
		{name: "unsupported category", head: "# Changelog\n\n## Unreleased\n\n### Internal\n\n- Hidden work.\n", want: "unsupported"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := t.TempDir()
			runGit(t, repo, "init", "-b", "main")
			runGit(t, repo, "config", "user.name", "Boatstack Test")
			runGit(t, repo, "config", "user.email", "boatstack@example.invalid")
			writeChangelog(t, repo, base)
			runGit(t, repo, "add", ".")
			runGit(t, repo, "commit", "-m", "base")
			baseCommit := runGit(t, repo, "rev-parse", "HEAD")
			writeChangelog(t, repo, test.head)
			if err := validateChangelogChange(repo, baseCommit, changelogConfig()); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q failure, got %v", test.want, err)
			}
		})
	}

	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Boatstack Test")
	runGit(t, repo, "config", "user.email", "boatstack@example.invalid")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# Fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "base")
	if err := validateChangelogChange(repo, runGit(t, repo, "rev-parse", "HEAD"), changelogConfig()); err == nil || !strings.Contains(err.Error(), "requires CHANGELOG.md") {
		t.Fatalf("missing changelog did not fail: %v", err)
	}
}

func TestDisabledChangelogPolicyLeavesRepositoriesUnchanged(t *testing.T) {
	if err := validateChangelogChange(t.TempDir(), "unused", testConfig()); err != nil {
		t.Fatalf("disabled changelog policy affected repository: %v", err)
	}
}

func TestAdHocPRContextEnforcesConfiguredChangelog(t *testing.T) {
	repo := prTestRepo(t)
	configPath := filepath.Join(repo, ".product-loop", "project.json")
	config := changelogConfig()
	config.Project.DefaultBranch = "main"
	value, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, value, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".product-loop/project.json")
	runGit(t, repo, "commit", "-m", "enable changelog policy")
	if _, err := PreparePRContext(PRContextOptions{Repo: repo}); err == nil || !strings.Contains(err.Error(), "requires CHANGELOG.md") {
		t.Fatalf("ad-hoc PR ignored missing changelog: %v", err)
	}
	writeChangelog(t, repo, "# Changelog\n\n## Unreleased\n\n### Added\n\n- Make reviewer-visible behavior predictable.\n")
	runGit(t, repo, "add", changelogPath)
	runGit(t, repo, "commit", "-m", "add changelog entry")
	if _, err := PreparePRContext(PRContextOptions{Repo: repo}); err != nil {
		t.Fatalf("ad-hoc PR rejected valid changelog: %v", err)
	}
}
