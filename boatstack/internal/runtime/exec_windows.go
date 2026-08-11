//go:build windows

package runtime

import (
	"os"
	"os/exec"
)

func execute(path string, arguments, environment []string) (int, error) {
	command := exec.Command(path, arguments[1:]...)
	command.Env, command.Stdin, command.Stdout, command.Stderr = environment, os.Stdin, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}
