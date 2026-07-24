package boatstack

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// readBinaryIdentity returns the (version, sourceCommit) a helper binary reports
// for itself. It is a package var so conformance tests can substitute a candidate
// identity without building real multi-version binaries (mirrors recoveryGh).
var readBinaryIdentity = execBinaryIdentity

// execBinaryIdentity executes `<path> version` and parses the helper's own
// self-report. The generated bundle and version constants are embedded in each
// binary, so the only authoritative source of a binary's identity is the binary
// itself — never the running process's compile-time globals.
func execBinaryIdentity(path string) (string, string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, absolute, "version").Output()
	if err != nil {
		return "", "", fmt.Errorf("run %q version: %w", absolute, err)
	}
	return parseVersionOutput(string(output))
}

// parseVersionOutput reads the `version` subcommand line, formatted exactly as
// `Boatstack <version> (<sourceCommit>)`.
func parseVersionOutput(output string) (string, string, error) {
	trimmed := strings.TrimSpace(output)
	const prefix = "Boatstack "
	if !strings.HasPrefix(trimmed, prefix) {
		return "", "", fmt.Errorf("unrecognized helper version output: %q", trimmed)
	}
	rest := strings.TrimPrefix(trimmed, prefix)
	open := strings.LastIndex(rest, " (")
	if open < 0 || !strings.HasSuffix(rest, ")") {
		return "", "", fmt.Errorf("unrecognized helper version output: %q", trimmed)
	}
	version := strings.TrimSpace(rest[:open])
	sourceCommit := strings.TrimSpace(rest[open+2 : len(rest)-1])
	if version == "" || sourceCommit == "" {
		return "", "", fmt.Errorf("incomplete helper identity in version output: %q", trimmed)
	}
	return version, sourceCommit, nil
}
