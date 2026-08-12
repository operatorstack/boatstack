package effects

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/ports"
)

func TestPreparedEffectNeverAcceptsMixedEpochAtWriteBoundary(t *testing.T) {
	// control-law: partial-local-install-restores-exact-prior-bytes
	root := t.TempDir()
	first := filepath.Join(root, "first.json")
	authoritative := filepath.Join(root, "state.json")
	blocker := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(first, []byte("prior-first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authoritative, []byte("prior-state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocker, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared := &preparedEffect{requiredCapabilities: []catalog.Capability{catalog.CapabilityRepositoryWrite}, effectiveCapabilities: []catalog.Capability{catalog.CapabilityRepositoryWrite}, mutations: []ports.ResourceMutation{
		{Path: first, Prior: []byte("prior-first"), PriorExists: true, Target: []byte("target-first"), Mode: 0o600},
		{Path: filepath.Join(blocker, "second.json"), Target: []byte("target-second"), Mode: 0o600},
		{Path: authoritative, Prior: []byte("prior-state"), PriorExists: true, Target: []byte("target-state"), Mode: 0o600, InstallLast: true},
	}}
	if _, err := prepared.Execute(context.Background()); err == nil {
		t.Fatal("injected write-boundary failure was accepted")
	}
	if err := prepared.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	firstValue, _ := os.ReadFile(first)
	stateValue, _ := os.ReadFile(authoritative)
	if string(firstValue) != "prior-first" || string(stateValue) != "prior-state" {
		t.Fatalf("mixed epoch survived rollback: first=%q state=%q", firstValue, stateValue)
	}
}

func TestPreparedEffectRollsBackManagedLauncherSymlink(t *testing.T) {
	// control-law: interrupted-program-update-restores-the-exact-prior-launcher
	root := t.TempDir()
	launcher := filepath.Join(root, "boatstack")
	if err := os.Symlink("boatstack-old", launcher); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared := &preparedEffect{requiredCapabilities: []catalog.Capability{catalog.CapabilityRepositoryWrite}, effectiveCapabilities: []catalog.Capability{catalog.CapabilityRepositoryWrite}, mutations: []ports.ResourceMutation{
		{Path: launcher, PriorExists: true, PriorLink: "boatstack-old", TargetLink: "boatstack-new", Mode: 0o700},
		{Path: filepath.Join(blocker, "state.json"), Target: []byte("new-state"), Mode: 0o600, InstallLast: true},
	}}
	if _, err := prepared.Execute(context.Background()); err == nil {
		t.Fatal("injected post-launcher failure was accepted")
	}
	if err := prepared.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(launcher)
	if err != nil {
		t.Fatal(err)
	}
	if target != "boatstack-old" {
		t.Fatalf("launcher rollback target = %q, want boatstack-old", target)
	}
}

func TestPreparedEffectRefusesMissingKernelCapabilityBeforeAnyEffect(t *testing.T) {
	// control-law: a trusted helper cannot use ambient authority for its caller
	root := t.TempDir()
	target := filepath.Join(root, "must-not-exist")
	boundaryCalls := 0
	prepared := &preparedEffect{
		requiredCapabilities:  []catalog.Capability{catalog.CapabilityPublicationPublish},
		effectiveCapabilities: []catalog.Capability{catalog.CapabilityPublicationPrepare},
		boundary: func(context.Context) (ports.EffectResult, error) {
			boundaryCalls++
			return ports.EffectResult{Settlement: ports.EffectSettled}, nil
		},
		mutations: []ports.ResourceMutation{{Path: target, Target: []byte("changed"), Mode: 0o600}},
	}
	if _, err := prepared.Execute(context.Background()); err == nil {
		t.Fatal("publication.prepare crossed a publication.publish effect boundary")
	}
	if boundaryCalls != 0 {
		t.Fatalf("trusted helper ran %d privileged calls", boundaryCalls)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("denied effect mutated repository: %v", err)
	}
}
