package effects

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
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

func TestPreparedEffectInstallsAndRecoversPlanningTreeAsOneResource(t *testing.T) {
	// control-law: recovery removes only the immutable tree created by this transaction
	root := filepath.Join(t.TempDir(), "package")
	prepared := &preparedEffect{
		requiredCapabilities:  []catalog.Capability{catalog.CapabilityRepositoryWrite},
		effectiveCapabilities: []catalog.Capability{catalog.CapabilityRepositoryWrite},
		mutations: []ports.ResourceMutation{
			{Path: filepath.Join(root, "manifest.json"), Target: []byte("manifest"), Mode: 0o644, AtomicTreeRoot: root, InstallLast: true},
			{Path: filepath.Join(root, "compiled", "tasks.json"), Target: []byte("tasks"), Mode: 0o644, AtomicTreeRoot: root},
		},
	}
	if _, err := prepared.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(root, "compiled", "tasks.json")); err != nil || string(raw) != "tasks" {
		t.Fatalf("installed tree member = %q, %v", raw, err)
	}
	if err := prepared.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("transaction-created tree survived recovery: %v", err)
	}
}

func TestPreparedEffectPreservesConflictingPlanningTree(t *testing.T) {
	// control-law: an existing fingerprint path is never replaced by staged installation
	parent := t.TempDir()
	root := filepath.Join(parent, "package")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	prior := filepath.Join(root, "manifest.json")
	if err := os.WriteFile(prior, []byte("prior"), 0o644); err != nil {
		t.Fatal(err)
	}
	prepared := &preparedEffect{
		requiredCapabilities:  []catalog.Capability{catalog.CapabilityRepositoryWrite},
		effectiveCapabilities: []catalog.Capability{catalog.CapabilityRepositoryWrite},
		mutations:             []ports.ResourceMutation{{Path: prior, Target: []byte("candidate"), Mode: 0o644, AtomicTreeRoot: root}},
	}
	if _, err := prepared.Execute(context.Background()); err == nil {
		t.Fatal("conflicting immutable tree was replaced")
	}
	if raw, err := os.ReadFile(prior); err != nil || string(raw) != "prior" {
		t.Fatalf("conflicting tree changed: %q, %v", raw, err)
	}
}

func TestPreparedEffectReportsOnlyActuallyAppliedEffects(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "state.json")
	transition := catalog.Transition{ID: "program/write", Effect: "program.write", Owner: "program", OwnedResources: []string{"program.state"}}
	prepared := &preparedEffect{
		transition: transition, requiredCapabilities: []catalog.Capability{catalog.CapabilityRepositoryWrite}, effectiveCapabilities: []catalog.Capability{catalog.CapabilityRepositoryWrite},
		mutations: []ports.ResourceMutation{{Resource: "program.state", Owner: "program", Path: target, Target: []byte("committed"), Mode: 0o600}},
	}
	if facts := prepared.CommittedEffects(); len(facts) != 0 {
		t.Fatalf("planned mutation escaped as committed evidence: %#v", facts)
	}
	if _, err := prepared.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	facts := prepared.CommittedEffects()
	if len(facts) != 1 || facts[0].Kind != protocol.EffectResourceMutation || facts[0].EffectID != transition.Effect || facts[0].Target != target || facts[0].Operation != "create" {
		t.Fatalf("applied mutation fact = %#v", facts)
	}
	if err := prepared.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	if facts := prepared.CommittedEffects(); len(facts) != 0 {
		t.Fatalf("rolled-back mutation remained committed: %#v", facts)
	}
}

func TestSettledEffectBoundaryProducesAStableKernelFact(t *testing.T) {
	transition := catalog.Transition{ID: "publication.execute", Effect: "publication.execute", Owner: "product-delivery"}
	prepared := &preparedEffect{
		transition: transition, requiredCapabilities: []catalog.Capability{catalog.CapabilityPublicationPublish}, effectiveCapabilities: []catalog.Capability{catalog.CapabilityPublicationPublish},
		boundary: func(context.Context) (ports.EffectResult, error) {
			return ports.EffectResult{Settlement: ports.EffectSettled, Detail: "provider-adapter-settled"}, nil
		},
	}
	if _, err := prepared.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	facts := prepared.CommittedEffects()
	if len(facts) != 1 || facts[0].Kind != protocol.EffectBoundarySettled || facts[0].Target != string(transition.ID) || len(facts[0].ResultingFingerprint) != 64 {
		t.Fatalf("settled boundary fact = %#v", facts)
	}
}
