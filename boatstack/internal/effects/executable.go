package effects

import (
	"fmt"
	"os"
	"path/filepath"
)

// StageVerifiedExecutable materializes already verified bytes in a private,
// single-use location. Callers execute the returned path instead of reopening
// a mutable repository path after verification.
func StageVerifiedExecutable(source string, raw []byte) (string, func(), error) {
	directory, err := os.MkdirTemp("", "boatstack-extension-")
	if err != nil {
		return "", func() {}, fmt.Errorf("create private subprocess extension staging directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	path := filepath.Join(directory, "extension"+filepath.Ext(source))
	if err := os.WriteFile(path, raw, 0o700); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("stage verified subprocess extension bytes: %w", err)
	}
	return path, cleanup, nil
}
