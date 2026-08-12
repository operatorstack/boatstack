package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func RunFlowFrontend(ctx context.Context, executable, source string) ([]byte, error) {
	if executable == "" || !filepath.IsAbs(executable) || !filepath.IsAbs(source) {
		return nil, fmt.Errorf("Flow frontend and source paths must be exact and absolute")
	}
	command := exec.CommandContext(ctx, executable, source)
	output, err := command.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("Flow frontend failed: %s", string(exit.Stderr))
		}
		return nil, err
	}
	return output, nil
}

func AtomicWrite(path string, content []byte, mode os.FileMode) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("managed output path must be exact and absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".boatstack-flow-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err = temporary.Write(content); err == nil {
		err = temporary.Chmod(mode)
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

// RemoveGeneratedFile keeps Flow projection deletion inside the same owned
// filesystem boundary as projection creation. Callers must validate the exact
// path and prior fingerprint before invoking it.
func RemoveGeneratedFile(path string) error {
	return os.Remove(path)
}
