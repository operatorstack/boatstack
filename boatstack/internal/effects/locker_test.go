package effects

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/plant"
)

func TestKernelLockUsesProcessScopedHandleNotFilePresence(t *testing.T) {
	repository := recoveryRepository(t)
	resolver, err := plant.NewResolver(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := resolver.ResolveInvocation(context.Background(), repository, "cli", "lock-owner")
	if err != nil {
		t.Fatal(err)
	}
	locker, err := NewLocker(resolver)
	if err != nil {
		t.Fatal(err)
	}
	first, err := locker.Acquire(context.Background(), invocation, []string{"state"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := locker.Acquire(context.Background(), invocation, []string{"state"}); err == nil {
		t.Fatal("concurrent acquisition unexpectedly succeeded")
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	layout, _, err := resolver.ResolveLayout(context.Background(), invocation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(layout.LockRoot, "kernel.lock")); err != nil {
		t.Fatalf("stable lock inode was removed: %v", err)
	}
	second, err := locker.Acquire(context.Background(), invocation, []string{"state"})
	if err != nil {
		t.Fatalf("released persistent lock could not be reacquired: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestInstallationLockCoordinatesRepositoriesSharingOneLauncher(t *testing.T) {
	// control-law: shared-launcher-installation-has-one-cross-repository-writer
	resolver, err := plant.NewResolver(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	firstRepository := recoveryRepository(t)
	secondRepository := recoveryRepository(t)
	firstInvocation, err := resolver.ResolveInvocation(context.Background(), firstRepository, "cli", "first-installation")
	if err != nil {
		t.Fatal(err)
	}
	secondInvocation, err := resolver.ResolveInvocation(context.Background(), secondRepository, "cli", "second-installation")
	if err != nil {
		t.Fatal(err)
	}
	locker, err := NewLocker(resolver)
	if err != nil {
		t.Fatal(err)
	}
	first, err := locker.Acquire(context.Background(), firstInvocation, []string{"installation"})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	if _, err := locker.Acquire(context.Background(), secondInvocation, []string{"installation"}); err == nil {
		t.Fatal("two repositories concurrently acquired their shared launcher installation")
	}
	independent, err := locker.Acquire(context.Background(), secondInvocation, []string{"state"})
	if err != nil {
		t.Fatalf("unrelated repository state lock was coupled to installation: %v", err)
	}
	if err := independent.Release(); err != nil {
		t.Fatal(err)
	}
}
