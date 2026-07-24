//go:build windows

package boatstack

import (
	"errors"
	"os"
	"os/exec"
)

// execReplaceProcess spawns the target binary, mirrors its stdio, waits for it,
// and exits with its status. Windows has no execve, so this is the closest
// equivalent to replacing the current process; it never returns on success.
func execReplaceProcess(path string, args []string, env []string) error {
	command := exec.Command(path, args[1:]...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	command.Env = env
	if err := command.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	os.Exit(0)
	return nil
}
