package boatstack

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const changelogPath = "CHANGELOG.md"

var changelogCategories = map[string]bool{
	"Added":         true,
	"Changed":       true,
	"Fixed":         true,
	"Removed":       true,
	"Security":      true,
	"Documentation": true,
	"Maintenance":   true,
}

func isUnreleasedHeading(line string) bool {
	if line == "## Unreleased" || line == "## [Unreleased]" {
		return true
	}
	const prefix = "## [Unreleased] - "
	if !strings.HasPrefix(line, prefix) {
		return false
	}
	_, err := time.Parse("2006-01-02", strings.TrimPrefix(line, prefix))
	return err == nil
}

// changelogEntries returns the categorized bullets in the Unreleased section.
// Historical release sections are intentionally ignored: the policy requires a
// new reader-facing entry for the change being prepared, not rewritten history.
func changelogEntries(value []byte) (map[string]int, error) {
	entries := map[string]int{}
	inUnreleased := false
	category := ""
	foundUnreleased := false
	for _, rawLine := range strings.Split(strings.ReplaceAll(string(value), "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "## ") {
			if isUnreleasedHeading(line) {
				if foundUnreleased {
					return nil, fmt.Errorf("%s must contain exactly one ## Unreleased section", changelogPath)
				}
				foundUnreleased = true
				inUnreleased = true
				category = ""
				continue
			}
			if inUnreleased {
				inUnreleased = false
			}
			continue
		}
		if !inUnreleased || line == "" {
			continue
		}
		if strings.HasPrefix(line, "### ") {
			category = strings.TrimSpace(strings.TrimPrefix(line, "### "))
			if !changelogCategories[category] {
				return nil, fmt.Errorf("%s uses unsupported Unreleased category %q", changelogPath, category)
			}
			continue
		}
		if strings.HasPrefix(line, "-") {
			entry := strings.TrimSpace(strings.TrimPrefix(line, "-"))
			if category == "" {
				return nil, fmt.Errorf("%s Unreleased entries must appear under an allowed category", changelogPath)
			}
			if entry == "" {
				return nil, fmt.Errorf("%s contains an empty Unreleased entry", changelogPath)
			}
			entries[category+"\x00"+entry]++
		}
	}
	if !foundUnreleased {
		return nil, fmt.Errorf("%s must contain a ## Unreleased section", changelogPath)
	}
	return entries, nil
}

func readFileAtCommit(repo, commit, path string) ([]byte, bool, error) {
	command := exec.Command("git", "-C", repo, "show", commit+":"+path)
	value, err := command.Output()
	if err == nil {
		return value, true, nil
	}
	if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() != 0 {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("cannot read %s at base commit: %w", path, err)
}

func validateChangelogChange(repo, baseCommit string, config ProjectConfig) error {
	if !config.Workflow.MaintainChangelog {
		return nil
	}
	current, err := os.ReadFile(filepath.Join(repo, changelogPath))
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("changelog policy requires %s with a new entry under ## Unreleased", changelogPath)
		}
		return err
	}
	currentEntries, err := changelogEntries(current)
	if err != nil {
		return err
	}
	baseEntries := map[string]int{}
	base, exists, err := readFileAtCommit(repo, baseCommit, changelogPath)
	if err != nil {
		return err
	}
	if exists {
		parsed, parseErr := changelogEntries(base)
		if parseErr == nil {
			baseEntries = parsed
		}
	}
	for entry, count := range currentEntries {
		if count > baseEntries[entry] {
			return nil
		}
	}
	return fmt.Errorf("changelog policy requires a new categorized entry under ## Unreleased in %s", changelogPath)
}

// changelogComparisonBase makes each managed slice prove its own entry. Later
// slices compare with the previous slice's reviewed head, even when both slices
// use the same Git base and earlier Unreleased entries are still present. The
// comparison is anchored to the slice actually being gated or shipped (the
// addressable slice), not the BUILD pointer — a published-open earlier slice
// corrected in place compares against ITS predecessor, not the active slice's.
func changelogComparisonBase(repo, feature, sliceID, mergeBase string) (string, error) {
	if strings.TrimSpace(feature) == "" {
		return mergeBase, nil
	}
	state, err := LoadDeliveryState(repo, feature)
	if err != nil {
		return "", err
	}
	index, _, err := resolveAddressableSlice(state, sliceID)
	if err != nil {
		return "", err
	}
	if index <= 0 {
		return mergeBase, nil
	}
	previous := state.Slices[index-1]
	receipt, err := readDeliveryReceipt(repo, feature, previous.ID, "review")
	if err != nil {
		return "", fmt.Errorf("cannot establish changelog baseline for delivery slice %s: %w", state.Slices[index].ID, err)
	}
	if strings.TrimSpace(receipt.HeadCommit) == "" {
		return "", fmt.Errorf("previous delivery slice %s has no reviewed head commit", previous.ID)
	}
	return receipt.HeadCommit, nil
}
