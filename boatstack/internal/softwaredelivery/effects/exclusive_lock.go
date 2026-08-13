package effects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
)

// AcquireExclusivePath acquires one runtime-owned lock without implicitly
// acquiring the controller lock. Callers use it for authority records that
// must be revalidated before the controller and effect locks are acquired.
func AcquireExclusivePath(ctx context.Context, path string) (ports.Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = lockFile(file)
		if err == nil {
			return &heldLocks{values: []heldLock{{path: path, file: file}}}, nil
		}
		if !errors.Is(err, errLockHeld) {
			_ = file.Close()
			return nil, fmt.Errorf("acquire lock %s: %w", path, err)
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
