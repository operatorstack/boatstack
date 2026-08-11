package effects

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/ports"
)

type Locker struct{ resolver ports.InvocationResolver }

func NewLocker(resolver ports.InvocationResolver) (Locker, error) {
	if resolver == nil {
		return Locker{}, fmt.Errorf("effect locker requires an invocation resolver")
	}
	return Locker{resolver: resolver}, nil
}

type heldLock struct {
	path string
	file *os.File
}

type heldLocks struct{ values []heldLock }

func (l *heldLocks) Release() error {
	var first error
	for index := len(l.values) - 1; index >= 0; index-- {
		value := l.values[index]
		if err := unlockFile(value.file); err != nil && first == nil {
			first = fmt.Errorf("unlock %s: %w", value.path, err)
		}
		if err := value.file.Close(); err != nil && first == nil {
			first = fmt.Errorf("close lock %s: %w", value.path, err)
		}
	}
	l.values = nil
	return first
}

func (l Locker) Acquire(ctx context.Context, invocation model.InvocationContext, resources []string) (ports.Lock, error) {
	layout, _, err := l.resolver.ResolveLayout(ctx, invocation)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(layout.LockRoot, 0o700); err != nil {
		return nil, err
	}
	names := append([]string{"kernel"}, resources...)
	sort.Strings(names)
	unique := names[:0]
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || strings.ContainsAny(name, `/\\`) {
			return nil, fmt.Errorf("invalid lock resource %q", name)
		}
		if len(unique) == 0 || unique[len(unique)-1] != name {
			unique = append(unique, name)
		}
	}
	paths := make([]string, 0, len(unique)+1)
	for _, name := range unique {
		paths = append(paths, filepath.Join(layout.LockRoot, name+".lock"))
	}
	if containsString(unique, "installation") {
		paths = append(paths, filepath.Join(filepath.Dir(invocation.RuntimePath), ".boatstack-installation.lock"))
	}
	sort.Strings(paths)
	held := &heldLocks{}
	for _, path := range paths {
		file, openErr := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
		if openErr != nil {
			_ = held.Release()
			return nil, fmt.Errorf("open lock %s: %w", path, openErr)
		}
		if lockErr := lockFile(file); lockErr != nil {
			_ = file.Close()
			_ = held.Release()
			return nil, fmt.Errorf("acquire lock %s: %w", path, lockErr)
		}
		if truncateErr := file.Truncate(0); truncateErr != nil {
			_ = unlockFile(file)
			_ = file.Close()
			_ = held.Release()
			return nil, fmt.Errorf("truncate lock %s: %w", path, truncateErr)
		}
		if _, seekErr := file.Seek(0, 0); seekErr != nil {
			_ = unlockFile(file)
			_ = file.Close()
			_ = held.Release()
			return nil, fmt.Errorf("seek lock %s: %w", path, seekErr)
		}
		_, writeErr := fmt.Fprintf(file, "%s\n%s\n", invocation.Correlation, invocation.Ref)
		syncErr := file.Sync()
		if writeErr != nil || syncErr != nil {
			_ = unlockFile(file)
			_ = file.Close()
			_ = held.Release()
			if writeErr != nil {
				return nil, writeErr
			}
			return nil, syncErr
		}
		held.values = append(held.values, heldLock{path: path, file: file})
	}
	return held, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
