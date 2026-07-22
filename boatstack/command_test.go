package boatstack

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandOutputSeparatesAuthorityFromDiagnostics(t *testing.T) {
	t.Setenv("BOATSTACK_COMMAND_HELPER", "success")
	output, err := commandOutput(t.TempDir(), os.Args[0], "-test.run=^TestCommandOutputHelperProcess$")
	if err != nil {
		t.Fatal(err)
	}
	if output != ".cursor/commands/boatstack-update.md" {
		t.Fatalf("stderr contaminated machine output: %q", output)
	}

	t.Setenv("BOATSTACK_COMMAND_HELPER", "failure")
	output, err = commandOutput(t.TempDir(), os.Args[0], "-test.run=^TestCommandOutputHelperProcess$")
	if err == nil || output != "" || !strings.Contains(err.Error(), "CRLF diagnostic") || strings.Contains(err.Error(), "not-authoritative") {
		t.Fatalf("failed command did not prefer bounded stderr: output=%q err=%v", output, err)
	}
}

func TestCommandOutputHelperProcess(t *testing.T) {
	mode := os.Getenv("BOATSTACK_COMMAND_HELPER")
	if mode == "" {
		return
	}
	fmt.Fprintln(os.Stdout, ".cursor/commands/boatstack-update.md")
	if mode == "failure" {
		fmt.Fprintln(os.Stdout, "not-authoritative")
		fmt.Fprintln(os.Stderr, "CRLF diagnostic")
		os.Exit(7)
	}
	fmt.Fprintln(os.Stderr, "warning: fake/path will be replaced by CRLF")
	os.Exit(0)
}

func TestProductionControllersDoNotCollapseSubprocessChannels(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		value, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return err
		}
		if strings.Contains(string(value), ".CombinedOutput()") {
			return fmt.Errorf("%s collapses authority-bearing stdout and diagnostic stderr", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
