package effects

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeStoreOwnsAtomicForegroundRecordMutation(t *testing.T) {
	// control-law: runtime records mutate only through the effects-owned atomic store
	store := NewRuntimeStore()
	path := filepath.Join(t.TempDir(), "work", "record.json")
	if err := store.EnsureDirectory(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteAtomic(path, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteAtomic(path, []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "second\n" {
		t.Fatalf("runtime record = %q err=%v", raw, err)
	}
	if err := store.WriteAtomic("relative/record.json", []byte("invalid"), 0o600); err == nil {
		t.Fatal("relative runtime record path was accepted")
	}
}
