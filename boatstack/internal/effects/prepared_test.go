package effects

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
	prepared := &preparedEffect{mutations: []ports.ResourceMutation{
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
