package boatstack

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var stableReleaseVersion = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

// ReleaseClassification separates a projected documentation sync from a
// change that alters the installed Boatstack delivery harness.
type ReleaseClassification struct {
	Required bool
	Paths    []string
}

func normalizedReleasePath(value string) string {
	return filepath.ToSlash(filepath.Clean(strings.TrimSpace(value)))
}

func isReleaseBearingPath(value string) bool {
	path := normalizedReleasePath(value)
	if path == "." || path == "" {
		return false
	}
	for _, exact := range []string{
		".gitignore", "CONTRIBUTING.md", "README.md", "UPSTREAM.json",
		"project.example.json",
	} {
		if path == exact {
			return false
		}
	}
	for _, prefix := range []string{
		".github/", "assets/", "automation/", "docs/", "examples/", "release-notes/",
	} {
		if strings.HasPrefix(path, prefix) {
			return false
		}
	}
	if strings.HasPrefix(path, "boatstack/testdata/") || strings.HasSuffix(path, "_test.go") {
		return false
	}
	return true
}

// ClassifyReleasePaths is conservative: unknown projected product paths are
// release-bearing, while known presentation, provenance, test, and control
// plane paths are not.
func ClassifyReleasePaths(paths []string) ReleaseClassification {
	releasePaths := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, value := range paths {
		path := normalizedReleasePath(value)
		if !seen[path] && isReleaseBearingPath(path) {
			seen[path] = true
			releasePaths = append(releasePaths, path)
		}
	}
	sort.Strings(releasePaths)
	return ReleaseClassification{Required: len(releasePaths) > 0, Paths: releasePaths}
}

// ClassifyReleaseDiff reads the exact projected Git diff used by the release
// workflow and applies the same deterministic path policy as unit tests.
func ClassifyReleaseDiff(repo, base, head string) (ReleaseClassification, error) {
	if strings.TrimSpace(base) == "" || strings.TrimSpace(head) == "" {
		return ReleaseClassification{}, fmt.Errorf("release classification requires base and head revisions")
	}
	command := exec.Command("git", "-C", repo, "diff", "--name-only", "--no-renames", base, head)
	output, err := command.CombinedOutput()
	if err != nil {
		return ReleaseClassification{}, fmt.Errorf("release diff failed: %s", strings.TrimSpace(string(output)))
	}
	return ClassifyReleasePaths(strings.Split(strings.TrimSpace(string(output)), "\n")), nil
}

// NextPatchVersion returns the next stable patch version. Minor and major
// releases remain deliberate changes rather than being inferred from commits.
func NextPatchVersion(current string) (string, error) {
	matches := stableReleaseVersion.FindStringSubmatch(strings.TrimSpace(current))
	if matches == nil {
		return "", fmt.Errorf("release version must match vMAJOR.MINOR.PATCH: %s", current)
	}
	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	patch, _ := strconv.Atoi(matches[3])
	return fmt.Sprintf("v%d.%d.%d", major, minor, patch+1), nil
}
