package protocol

import (
	"fmt"
	"strings"
)

// ValidateGitReference accepts only non-option, non-revision-expression names.
// Callers that need ancestry operators must resolve them to an exact object ID
// before crossing an effect boundary.
func ValidateGitReference(value string) error {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 255 || strings.HasPrefix(value, "-") {
		return fmt.Errorf("Git reference must be a bounded non-option value")
	}
	if value == "@" || strings.Contains(value, "..") || strings.Contains(value, "@{") || strings.Contains(value, "//") ||
		strings.HasSuffix(value, ".") || strings.HasSuffix(value, "/") {
		return fmt.Errorf("Git reference %q contains forbidden reference syntax", value)
	}
	for _, character := range value {
		if character <= 0x20 || character == 0x7f || strings.ContainsRune(`~^:?*[\`, character) {
			return fmt.Errorf("Git reference %q contains a forbidden character", value)
		}
	}
	for _, component := range strings.Split(value, "/") {
		if component == "" || strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return fmt.Errorf("Git reference %q contains a forbidden component", value)
		}
	}
	return nil
}

func ValidateGitBranch(value string) error {
	if err := ValidateGitReference(value); err != nil {
		return err
	}
	if value == "HEAD" || strings.HasPrefix(value, "refs/") {
		return fmt.Errorf("Git branch %q must be an unqualified branch name", value)
	}
	return nil
}
