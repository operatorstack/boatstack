package boatstack

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// control-law: first-planning-write-uses-the-owned-channel
//
// No host-raw byte ever lands in .product-loop/features/ — before the first
// plan candidate exists or after. Previously the guard latched only once a
// (possibly malformed) draft registered, so the very first raw Write/cp of a
// planning artifact was allowed and the agent discovered planning-write only
// by failing into INVALID_STATE and a quarantine loop. The invariant these
// tests hold: the managed planning tree is authored exclusively through the
// owned channel at every stage; the deny is path-scoped (ordinary product
// writes stay unlatched at zero candidates); and the denial names the verb
// the guard itself admits at that stage (Coreachability).

// Positive: the owned channel clears the cause the denial reports — the
// helper verbs pass the guard at zero candidates, and planning-write actually
// creates the artifact.
func TestFirstPlanningWriteOwnedChannelStaysOpen(t *testing.T) {
	repo := safetyTestRepo(t)
	previousHealth := planningInstallationHealth
	planningInstallationHealth = func(string) error { return nil }
	t.Cleanup(func() { planningInstallationHealth = previousHealth })

	for _, command := range []string{
		".product-loop/boatstack planning-write --repo . --feature checkout --artifact plan.md <<'BOATSTACK_PLAN_EOF'\n# Plan\nBOATSTACK_PLAN_EOF\n",
		"boatstack-helper check-source-plan --repo . --plan docs/plan.md",
	} {
		if findings := ClassifyCommand(repo, command); len(findings) > 0 {
			t.Fatalf("owned channel denied at zero candidates: %q -> %#v", command, findings)
		}
	}

	written, err := WritePlanningArtifact(PlanningWriteOptions{
		Repo: repo, Feature: "checkout", Artifact: "source-plan.md",
		Content: []byte("# Source plan\n"),
	})
	if err != nil {
		t.Fatalf("planning-write must author the first artifact: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(written))); err != nil {
		t.Fatalf("owned write did not land: %v", err)
	}
}

// Negative: the first raw write is denied with the exact finding — category,
// source, stage, prescribed verb, and the slug parsed from the path — at any
// depth and for any name under the planning tree.
func TestFirstRawPlanningWriteIsDenied(t *testing.T) {
	repo := safetyTestRepo(t)

	finding := func(path string) SafetyFinding {
		findings := ClassifyTool(repo, "Write", map[string]any{
			"file_path": filepath.Join(repo, filepath.FromSlash(path)),
			"content":   "# Draft\n",
		})
		if len(findings) == 0 {
			t.Fatalf("raw write of %q must be denied at zero candidates", path)
		}
		return findings[0]
	}

	got := finding(".product-loop/features/checkout/plan.md")
	if got.Category != "workflow-phase-bypass" || got.Source != "planning-state" {
		t.Fatalf("wrong finding identity: %#v", got)
	}
	if got.WorkflowStage != "NOT_STARTED" || got.NextOperation != "planning-write" {
		t.Fatalf("finding must carry the real stage and the owned verb: %#v", got)
	}
	if got.BlockingFeature != "checkout" {
		t.Fatalf("slug must be parsed from the path for a copy-pasteable denial: %#v", got)
	}

	// Non-allowlisted names and nested depths are still inside the latch.
	for _, path := range []string{
		".product-loop/features/checkout/notes/scratch.md",
		".product-loop/features/checkout/random.txt",
	} {
		if got := finding(path); got.NextOperation != "planning-write" {
			t.Fatalf("latch must cover %q: %#v", path, got)
		}
	}
}

// Relation: the same law reaches both guard entry paths — a Write tool and a
// raw shell write yield the same finding, and the rendered denial names the
// owned channel in full.
func TestFirstWriteLatchCoversToolAndCommandPaths(t *testing.T) {
	repo := safetyTestRepo(t)

	toolFindings := ClassifyTool(repo, "Write", map[string]any{
		"file_path": filepath.Join(repo, ".product-loop", "features", "checkout", "plan.md"),
		"content":   "# Draft\n",
	})
	commandFindings := ClassifyCommand(repo, "cp draft.md .product-loop/features/checkout/plan.md")
	if len(toolFindings) == 0 || len(commandFindings) == 0 {
		t.Fatalf("both entry paths must deny: tool=%#v command=%#v", toolFindings, commandFindings)
	}
	for _, pair := range []struct {
		label   string
		finding SafetyFinding
	}{{"tool", toolFindings[0]}, {"command", commandFindings[0]}} {
		if pair.finding.Category != "workflow-phase-bypass" || pair.finding.NextOperation != "planning-write" || pair.finding.WorkflowStage != "NOT_STARTED" {
			t.Fatalf("%s path finding drifted: %#v", pair.label, pair.finding)
		}
	}

	rendered := denialFor("claude", toolFindings[0]).Render(RenderPlain)
	if !strings.Contains(rendered, "planning-write --repo . --feature checkout --artifact <name>") {
		t.Fatalf("denial must name the owned channel: %q", rendered)
	}
	if !strings.Contains(rendered, "NOT_STARTED") {
		t.Fatalf("denial must name the real stage: %q", rendered)
	}
}

// Bypass: shell write idioms and mutation-capable MCP tools cannot slip the
// latch, while product writes outside the planning tree stay unlatched — the
// deny is path-scoped, never a blanket zero-candidate interlock.
func TestFirstWriteLatchBypassAndScope(t *testing.T) {
	repo := safetyTestRepo(t)

	for _, command := range []string{
		`tee .product-loop/features/checkout/plan.md < draft.md`,
		`printf '# Plan' > .product-loop/features/checkout/plan.md`,
		`mv draft.md .product-loop/features/checkout/source-plan.md`,
		`mkdir -p .product-loop/features/checkout && cp draft.md .product-loop/features/checkout/plan.md`,
	} {
		if findings := ClassifyCommand(repo, command); len(findings) == 0 {
			t.Fatalf("shell write bypass allowed: %q", command)
		}
	}

	if findings := ClassifyTool(repo, "mcp__files__update", map[string]any{
		"path": ".product-loop/features/checkout/plan.md", "content": "# Draft\n",
	}); len(findings) == 0 {
		t.Fatal("mutation-capable MCP tool must not bypass the latch")
	}

	// Scope: ordinary product writes remain free at zero candidates.
	for _, path := range []string{"src/app.ts", "product.go"} {
		if findings := ClassifyTool(repo, "Write", map[string]any{
			"file_path": filepath.Join(repo, path), "content": "package main\n",
		}); len(findings) > 0 {
			t.Fatalf("path-scoped latch leaked onto product write %q: %#v", path, findings)
		}
	}
}

// Failure-state: a denial is decided before any effect — the working tree is
// byte-identical afterward and the planning tree still does not exist.
func TestFirstWriteDenialLeavesTreeUntouched(t *testing.T) {
	repo := safetyTestRepo(t)
	before, err := exec.Command("git", "-C", repo, "status", "--short").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}

	findings := ClassifyTool(repo, "Write", map[string]any{
		"file_path": filepath.Join(repo, ".product-loop", "features", "checkout", "plan.md"),
		"content":   "# Draft\n",
	})
	if len(findings) == 0 {
		t.Fatal("expected denial")
	}

	after, err := exec.Command("git", "-C", repo, "status", "--short").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("classification mutated the tree: before=%q after=%q", before, after)
	}
	if _, err := os.Stat(filepath.Join(repo, ".product-loop", "features")); !os.IsNotExist(err) {
		t.Fatalf("planning tree must not exist after a denial: %v", err)
	}
}
