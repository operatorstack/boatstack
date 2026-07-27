package boatstack

import (
	"path/filepath"
	"strings"
	"testing"
)

// control-law: written-content-is-data-not-effect
//
// A file-writer tool's document body is inert until something executes it, so
// the guard grades a Write/Edit by its NAME and STRUCTURAL fields (paths,
// destinations) — never by prose inside the body. Previously the text
// classifiers ran over the full marshaled input, so a runbook that mentioned
// `terraform destroy` or docs that named `.git/boatstack/` were denied as if
// the words were the act. The floor is unchanged: the same strings as a Bash
// command still block (the text IS the command), a live SQL executor's
// arguments still block (they are executed), and a protected path in any
// structural field still blocks (redaction never launders the target).

// contentDocCases name every non-sqlEffect destruction rule once, as prose.
var contentDocCases = []struct {
	name    string
	content string
}{
	{"database-reset", "Ask the operator before `supabase db reset` on staging."},
	{"filesystem-destruction", "Cleanup appendix: `rm -rf ~` is the classic footgun; never script it."},
	{"git-history-destruction", "Policy: `git push --force origin main` is forbidden; use revert PRs."},
	{"infrastructure-destruction", "Teardown runbook: `terraform destroy -auto-approve` after sign-off only."},
	{"recovery-destruction", "Quarterly drill: delete backup snapshots older than the retention window."},
	{"guard-state-doc", "Delivery state lives under `.git/boatstack/deliveries/` — helper-owned."},
}

// Positive: documents whose bodies name every destruction rule (and the
// managed state tree) pass as file-writer tool calls.
func TestWrittenContentIsNotClassifiedAsEffect(t *testing.T) {
	repo := safetyTestRepo(t)
	for _, c := range contentDocCases {
		c := c
		t.Run("write/"+c.name, func(t *testing.T) {
			findings := ClassifyTool(repo, "Write", map[string]any{
				"file_path": filepath.Join(repo, "docs", c.name+".md"),
				"content":   c.content,
			})
			if len(findings) > 0 {
				t.Fatalf("document content classified as effect: %#v", findings)
			}
		})
		t.Run("edit/"+c.name, func(t *testing.T) {
			findings := ClassifyTool(repo, "Edit", map[string]any{
				"file_path":  filepath.Join(repo, "docs", c.name+".md"),
				"old_string": "TODO",
				"new_string": c.content,
			})
			if len(findings) > 0 {
				t.Fatalf("edit content classified as effect: %#v", findings)
			}
		})
	}
}

// Negative: the executor contexts keep the boundary — the same text blocks
// when it IS the command, and executed SQL arguments stay live.
func TestExecutorContextsStillBlockAfterRedaction(t *testing.T) {
	repo := safetyTestRepo(t)
	for _, command := range []string{
		`terraform destroy -auto-approve`,
		`git push --force origin main`,
		`supabase db reset`,
	} {
		if findings := ClassifyCommand(repo, command); len(findings) == 0 {
			t.Fatalf("live command must still block: %q", command)
		}
	}
	if findings := ClassifyTool(repo, "mcp__db__execute_sql", map[string]any{"query": "DROP TABLE users"}); len(findings) == 0 {
		t.Fatal("SQL executor arguments are executed, not stored — must still block")
	}
}

// Relation: one table drives both outcomes for the identical hook-shaped tool
// call — content alone allows, a protected structural path denies.
func TestContentAllowsWhilePathDenies(t *testing.T) {
	repo := safetyTestRepo(t)
	content := "Ops note: state is under .git/boatstack/deliveries/ and terraform destroy is operator-only."

	if findings := ClassifyTool(repo, "Write", map[string]any{
		"file_path": filepath.Join(repo, "docs", "ops.md"),
		"content":   content,
	}); len(findings) > 0 {
		t.Fatalf("content-only mention must pass: %#v", findings)
	}

	findings := ClassifyTool(repo, "Write", map[string]any{
		"file_path": ".git/boatstack/deliveries/checkout/state.json",
		"content":   content,
	})
	if len(findings) == 0 {
		t.Fatal("write INTO managed state must deny regardless of content")
	}
	if findings[0].Category != "workflow-state-tamper" {
		t.Fatalf("wrong category for state tamper: %#v", findings)
	}
}

// Bypass: redaction drops only content fields — a protected path smuggled in
// any structural field (file_path, destination, nested) still blocks, and a
// non-writer tool keeps full-input grading.
func TestRedactionCannotLaunderProtectedTargets(t *testing.T) {
	repo := safetyTestRepo(t)

	for name, input := range map[string]map[string]any{
		"file_path":   {"file_path": ".git/boatstack/flow/trajectory.jsonl", "content": "x"},
		"destination": {"file_path": "notes.md", "destination": ".git/boatstack/deliveries/x", "content": "x"},
		"nested":      {"file_path": "notes.md", "meta": map[string]any{"target_path": ".git/boatstack/runtimes/v1"}, "content": "x"},
	} {
		if findings := ClassifyTool(repo, "Write", input); len(findings) == 0 {
			t.Fatalf("structural field %s must survive redaction and deny: %#v", name, input)
		}
	}

	// A tool with no extracted path is not a file-writer: full-input grading holds.
	if findings := ClassifyTool(repo, "mcp__infra__delete_resource", map[string]any{
		"kind": "database", "note": "drop the staging cluster",
	}); len(findings) == 0 {
		t.Fatal("non-writer destructive tool must keep full-input grading")
	}
}

// Failure-state: redaction is pure — the caller's input map is never mutated,
// and classification performs no I/O on the named document.
func TestRedactionIsPureAndReadOnly(t *testing.T) {
	repo := safetyTestRepo(t)
	input := map[string]any{
		"file_path": filepath.Join(repo, "docs", "ops.md"),
		"content":   "terraform destroy notes",
		"meta":      map[string]any{"body": "git reset --hard"},
	}
	_ = ClassifyTool(repo, "Write", input)

	if input["content"] != "terraform destroy notes" {
		t.Fatalf("caller input mutated: %#v", input)
	}
	if meta := input["meta"].(map[string]any); meta["body"] != "git reset --hard" {
		t.Fatalf("nested caller input mutated: %#v", meta)
	}
	if strings.Contains(strings.ToLower("docs/ops.md"), "boatstack") {
		t.Fatal("fixture invariant")
	}
}
