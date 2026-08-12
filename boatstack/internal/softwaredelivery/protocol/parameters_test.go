package protocol

import (
	"path/filepath"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/testprogram"
)

func TestEffectPathsAndGitNamesAreValidatedAtAdmissionBoundary(t *testing.T) {
	plan, _ := testprogram.StandardRegistry().Lookup("plan.create")
	if err := (Parameters{{Name: "source_path", Value: "relative.md"}, {Name: "delivery_id", Value: "delivery"}}).Validate(plan); err == nil {
		t.Fatal("relative source path was accepted")
	}
	workspace, _ := testprogram.StandardRegistry().Lookup("workspace.cut")
	if err := (Parameters{{Name: "branch", Value: "--force"}, {Name: "base_ref", Value: "HEAD~1"}, {Name: "destination", Value: filepath.Join(t.TempDir(), "worktree")}}).Validate(workspace); err == nil {
		t.Fatal("unsafe Git parameters were accepted")
	}
}
