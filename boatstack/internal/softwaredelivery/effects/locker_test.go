package effects

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/hostprojection"
	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/plant"
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
	blocked, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, err := locker.Acquire(blocked, invocation, []string{"state"}); err == nil {
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

func TestInstallationLocksAreRepositoryScoped(t *testing.T) {
	// control-law: repository-runtime-admission-cannot-lock-or-mutate-another-repository
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
	independent, err := locker.Acquire(context.Background(), secondInvocation, []string{"installation"})
	if err != nil {
		t.Fatalf("independent repository installation was coupled to another repository: %v", err)
	}
	if err := independent.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestConfigurationLockSerializesSharedProjectionPublicationAcrossPreparation(t *testing.T) {
	// control-law: maintenance-reference-observation-and-retirement-share-the-flow-projection-lease
	repository, err := filepath.EvalSymlinks(recoveryRepository(t))
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := plant.NewResolver(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := resolver.ResolveInvocation(context.Background(), repository, "cli", "configuration-prepared")
	if err != nil {
		t.Fatal(err)
	}
	locker, err := NewLocker(resolver)
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := locker.Acquire(context.Background(), invocation, []string{"configuration"})
	if err != nil {
		t.Fatal(err)
	}

	sharedRelative, sharedContent, ok := hostprojection.SharedCheckoutPath(hostprojection.Gemini)
	if !ok {
		t.Fatal("Gemini shared checkout path is unavailable")
	}
	source := ".boatstack/flows/concurrent.flow.ts"
	artifact := ".boatstack/flows/concurrent.flow.ir.json"
	artifactRaw := []byte("concurrent artifact")
	prior, err := boatstackruntime.LoadFlowProjectionOwnership(repository, source)
	if err != nil {
		t.Fatal(err)
	}
	next := boatstackruntime.NewFlowProjectionOwnership(source, artifact, artifactRaw, strings.Repeat("a", 64), map[string][]byte{sharedRelative: sharedContent})
	writes := []boatstackruntime.ProjectionWrite{
		{Path: filepath.Join(repository, filepath.FromSlash(sharedRelative)), Content: sharedContent, Mode: 0o644},
		{Path: filepath.Join(repository, filepath.FromSlash(artifact)), Content: artifactRaw, Mode: 0o644, PublishLast: true},
	}
	if err := boatstackruntime.ApplyOwnedFlowProjection(repository, writes, nil, nil, prior, next); err == nil || !strings.Contains(err.Error(), "FLOW_PROJECTION_BUSY") {
		t.Fatalf("Flow publication crossed prepared configuration effect: %v", err)
	}
	if current, err := boatstackruntime.LoadFlowProjectionOwnership(repository, source); err != nil || current.Exists() {
		t.Fatalf("blocked Flow publication installed ownership: exists=%t err=%v", current.Exists(), err)
	}
	if _, err := os.Stat(filepath.Join(repository, filepath.FromSlash(sharedRelative))); !os.IsNotExist(err) {
		t.Fatalf("blocked Flow publication installed shared metadata: %v", err)
	}

	if err := configuration.Release(); err != nil {
		t.Fatal(err)
	}
	if err := boatstackruntime.ApplyOwnedFlowProjection(repository, writes, nil, nil, prior, next); err != nil {
		t.Fatalf("Flow publication remained blocked after configuration settlement: %v", err)
	}
	if actual, err := os.ReadFile(filepath.Join(repository, filepath.FromSlash(sharedRelative))); err != nil || string(actual) != string(sharedContent) {
		t.Fatalf("published shared metadata = %q, %v", actual, err)
	}
}
