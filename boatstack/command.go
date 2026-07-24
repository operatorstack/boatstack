package boatstack

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// commandChannels preserves the subprocess transport boundary. Stdout is the
// only authority-bearing channel; stderr is diagnostic even when the command
// exits successfully.
type commandChannels struct {
	Stdout []byte
	Stderr []byte
}

func runCommandChannels(command *exec.Cmd) (commandChannels, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return commandChannels{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, err
}

func commandFailure(channels commandChannels, runErr error) error {
	message := strings.TrimSpace(string(channels.Stderr))
	if message == "" {
		message = strings.TrimSpace(string(channels.Stdout))
	}
	if message == "" && runErr != nil {
		message = runErr.Error()
	}
	return fmt.Errorf("%s", boundedObservation(message))
}

// commandOutput returns only successful stdout for machine parsing. Successful
// stderr can contain warnings, progress, locale text, or host diagnostics and
// must never become a path, ref, URL, fingerprint, or workflow status.
func commandOutput(repo string, name string, arguments ...string) (string, error) {
	command := exec.Command(name, arguments...)
	command.Dir = repo
	channels, err := runCommandChannels(command)
	if err != nil {
		return "", commandFailure(channels, err)
	}
	return strings.TrimSpace(string(channels.Stdout)), nil
}

// commandOutputEnv is commandOutput with additional environment variables appended
// to the inherited environment. It keeps the same stdout-is-the-only-authority
// contract; extra entries are ordinary NAME=VALUE strings.
func commandOutputEnv(repo string, extraEnv []string, name string, arguments ...string) (string, error) {
	command := exec.Command(name, arguments...)
	command.Dir = repo
	command.Env = append(os.Environ(), extraEnv...)
	channels, err := runCommandChannels(command)
	if err != nil {
		return "", commandFailure(channels, err)
	}
	return strings.TrimSpace(string(channels.Stdout)), nil
}
