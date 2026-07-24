package boatstack

import (
	"os"
	"path/filepath"
)

// reexecProcess replaces (or, on Windows, spawns-and-exits) the current process
// with another binary. It is a package var so conformance tests can observe the
// hand-off without actually replacing the test process.
var reexecProcess = execReplaceProcess

// reexecUpdate hands the entire update to a replacement helper binary. A running
// helper embeds its own generated bundle and version constants, so it cannot
// correctly install a *different* version in-process; the replacement binary must
// perform its own install so its bundle, constants, and durable operation receipt
// are the authoritative ones. The child runs `update -binary <self>`, where its
// self-report matches its running identity, so it proceeds in-process and the
// hand-off terminates after exactly one hop.
func reexecUpdate(candidate string, options InitOptions) error {
	absolute, err := filepath.Abs(candidate)
	if err != nil {
		return err
	}
	args := []string{absolute, "update"}
	if options.Repo != "" {
		args = append(args, "-repo", options.Repo)
	}
	args = append(args, "-binary", absolute)
	if options.Yes {
		args = append(args, "-yes")
	}
	if options.Repair {
		args = append(args, "-repair")
	}
	if options.AllowDowngrade {
		args = append(args, "-allow-downgrade")
	}
	return reexecProcess(absolute, args, os.Environ())
}
