package effects

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
)

type runtimeStore struct{}

func NewRuntimeStore() ports.RuntimeStore { return runtimeStore{} }

func (runtimeStore) EnsureDirectory(path string, mode uint32) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("runtime store directory must be canonical and absolute")
	}
	return os.MkdirAll(path, os.FileMode(mode))
}

func (runtimeStore) WriteAtomic(path string, raw []byte, mode uint32) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("runtime store path must be canonical and absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".runtime-record-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(os.FileMode(mode)); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
