package effects

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/control"
)

func TestNamespacedExtensionWriteRejectsSymlinkEscapeWithoutMutation(t *testing.T) {
	// control-law: declarative-extension-write-cannot-escape-its-owned-resource-root
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated Windows privileges")
	}
	repository := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(repository, ".boatstack", "extensions", "example.guard", "link")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	content := []byte("must-not-escape")
	digest := sha256.Sum256(content)
	_, err := NewExtensionLocalPrepared(repository, "example.guard", []control.ResourceWrite{{
		Resource: "example.guard.evidence", Path: filepath.Join(link, "evidence.json"), Content: content, SHA256: hex.EncodeToString(digest[:]),
	}})
	if err == nil {
		t.Fatal("symlink escape was accepted")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "evidence.json")); !os.IsNotExist(statErr) {
		t.Fatalf("rejected write mutated outside path: %v", statErr)
	}
}
