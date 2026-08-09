package boatstack

import (
	"os"
	"path/filepath"
	"testing"
)

// The guard is a classifier, so it is graded like one — against an external corpus
// on TWO axes (Guard-as-Benchmarked-Artifact):
//
//   - constitutional: real destruction that MUST block. This is the monotone floor —
//     100% required, and a change may never regress it (a live destructive effect is
//     never allowed for convenience).
//   - routine: ordinary product work that MUST pass. This is the false-positive axis —
//     the guard earns its keep by NOT blocking these. Tracked as a rate so the
//     boat-terminal harness can grow the corpus and watch for drift.
//
// This is the corpus itself — a test, not a note. Extend it (from the boat-terminal
// or by hand) as new destructive shapes and new routine idioms appear.
type guardCase struct {
	name      string
	tool      string // "" => Bash command in `input`; otherwise a tool name
	input     any    // command string, or a tool-input map
	axis      string // "constitutional" | "routine"
	wantBlock bool
}

func classifyCase(repo string, c guardCase) []SafetyFinding {
	if c.tool == "" {
		return ClassifyCommand(repo, c.input.(string))
	}
	return ClassifyTool(repo, c.tool, c.input)
}

func TestGuardCorpusDualReward(t *testing.T) {
	repo := safetyTestRepo(t)
	// A DDL file that a live client would execute (for the psql -f constitutional case).
	if err := os.WriteFile(filepath.Join(repo, "migrate.sql"), []byte("DROP TABLE accounts;\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []guardCase{
		// --- constitutional: live destruction, MUST block (monotone floor) ---
		{"psql-drop-inline", "", `psql -c "DROP SCHEMA public CASCADE"`, "constitutional", true},
		{"psql-truncate", "", `psql -c "TRUNCATE TABLE accounts"`, "constitutional", true},
		{"psql-unbounded-delete", "", `psql -c "DELETE FROM accounts"`, "constitutional", true},
		{"psql-run-ddl-file", "", `psql -f migrate.sql`, "constitutional", true},
		{"supabase-reset", "", `supabase db reset`, "constitutional", true},
		{"prisma-reset", "", `prisma migrate reset --force`, "constitutional", true},
		{"terraform-destroy", "", `terraform destroy -auto-approve`, "constitutional", true},
		{"rm-rf-root", "", `rm -rf /`, "constitutional", true},
		{"rm-rf-home", "", `rm -rf ~`, "constitutional", true},
		{"git-reset-hard", "", `git reset --hard HEAD~1`, "constitutional", true},
		{"git-force-push", "", `git push --force origin main`, "constitutional", true},
		{"kubectl-delete-ns", "", `kubectl delete namespace production`, "constitutional", true},
		{"docker-volume-rm", "", `docker volume rm data-volume`, "constitutional", true},
		{"gcloud-sql-delete", "", `gcloud sql instances delete primary`, "constitutional", true},
		{"aws-delete-snapshot", "", `aws rds delete-db-snapshot --db-snapshot-identifier b1`, "constitutional", true},
		{"mcp-execute-sql-drop", "mcp__db__execute_sql", map[string]any{"query": "DROP TABLE users"}, "constitutional", true},
		// First-write latch: the managed planning tree is authored only through
		// planning-write — a raw first write is denied even before any candidate
		// exists. control-law: first-planning-write-uses-the-owned-channel
		{"raw-write-first-plan", "Write", map[string]any{
			"file_path": filepath.Join(repo, ".product-loop", "features", "checkout", "plan.md"),
			"content":   "# Plan\n",
		}, "constitutional", true},
		{"cp-first-plan", "", `cp draft.md .product-loop/features/checkout/plan.md`, "constitutional", true},
		{"redirect-first-source-plan", "", `printf x > .product-loop/features/checkout/source-plan.md`, "constitutional", true},
		// Content redaction must never launder a protected TARGET: writing INTO
		// managed runtime state is tampering regardless of what the body says.
		// control-law: written-content-is-data-not-effect
		{"write-into-delivery-state", "Write", map[string]any{
			"file_path": ".git/boatstack/deliveries/checkout/state.json",
			"content":   "{}",
		}, "constitutional", true},

		// --- routine: ordinary product work, MUST pass (false-positive axis) ---
		{"git-add-migration", "", `git add migrate.sql`, "routine", false},
		{"git-commit-mentions-drop", "", `git commit -m "add migration that will DROP TABLE stale"`, "routine", false},
		{"git-diff-migration", "", `git diff --stat migrate.sql`, "routine", false},
		{"git-restore-staged", "", `git restore --staged migrate.sql`, "routine", false},
		{"git-status", "", `git status`, "routine", false},
		{"cat-sql", "", `cat migrate.sql`, "routine", false},
		{"recovery-status-jq", "", `boatstack-helper recovery-status --repo . | jq .next_operation`, "routine", false},
		{"git-diff-wc", "", `git diff | wc -l`, "routine", false},
		{"echo-hello", "", `echo hello`, "routine", false},
		{"edit-note-mentions-ddl", "Edit", map[string]any{
			"file_path": filepath.Join(repo, "notes.md"),
			"content":   "Runbook: operator runs `psql -c \"DROP SCHEMA public CASCADE\"` off-hours.",
		}, "routine", false},
		// The owned planning channel and ordinary product writes stay open at zero
		// candidates — the first-write latch is path-scoped, never a blanket deny.
		{"planning-write-first-artifact", "", ".product-loop/boatstack planning-write --repo . --feature checkout --artifact plan.md <<'BOATSTACK_PLAN_EOF'\n# Plan\nBOATSTACK_PLAN_EOF\n", "routine", false},
		{"check-source-plan", "", `boatstack-helper check-source-plan --repo . --plan docs/plan.md`, "routine", false},
		{"write-product-source", "Write", map[string]any{
			"file_path": filepath.Join(repo, "src", "app.ts"),
			"content":   "export const x = 1\n",
		}, "routine", false},
		// Written content is data: documenting destructive commands or managed
		// paths in a file body is not the act of running or tampering with them.
		// control-law: written-content-is-data-not-effect
		{"write-runbook-mentions-destroy", "Write", map[string]any{
			"file_path": filepath.Join(repo, "runbook.md"),
			"content":   "Recovery drill: run `terraform destroy -target=module.stale` then restore from the snapshot.",
		}, "routine", false},
		{"write-doc-mentions-guard-state", "Write", map[string]any{
			"file_path": filepath.Join(repo, "docs", "ops.md"),
			"content":   "Delivery state lives under `.git/boatstack/deliveries/` and is owned by the helper.",
		}, "routine", false},
		{"edit-note-mentions-git-reset", "Edit", map[string]any{
			"file_path":  filepath.Join(repo, "notes.md"),
			"old_string": "TODO",
			"new_string": "Never run `git reset --hard` on shared branches; use revert PRs.",
		}, "routine", false},
		// Solution-set picks: commands the guard itself enumerates as legal moves
		// at THIS fixture's stage must pass the guard
		// (guard-never-prescribes-what-it-would-deny). Stage-scoped picks like
		// record-approval are held in their own fixture by the closure sweep.
		{"pick-undo", "", `boatstack-helper undo --mutation abc123`, "routine", false},
		{"pick-discard-delivery", "", `boatstack-helper discard-delivery --feature stale`, "routine", false},
		{"pick-operation-status", "", `boatstack-helper operation-status`, "routine", false},
		{"pick-record-change", "", `boatstack-helper record-change --feature demo --slice s1 --message fix --source-stage ci --classification implementation_repair --mechanism retry-with-corrected-state`, "routine", false},
		{"pick-next-status", "", `boatstack-helper next-status`, "routine", false},
	}

	var constTotal, constBlocked, routineTotal, routinePassed int
	for _, c := range cases {
		c := c
		t.Run(c.axis+"/"+c.name, func(t *testing.T) {
			blocked := len(classifyCase(repo, c)) > 0
			switch c.axis {
			case "constitutional":
				constTotal++
				if blocked {
					constBlocked++
				} else {
					// A missed real destruction is a hard failure: the floor broke.
					t.Errorf("CONSTITUTIONAL MISS — real destruction allowed: %s", c.name)
				}
			case "routine":
				routineTotal++
				if !blocked {
					routinePassed++
				} else {
					t.Errorf("ROUTINE FALSE-BLOCK — ordinary work denied: %s -> %#v", c.name, classifyCase(repo, c))
				}
			default:
				t.Fatalf("unknown axis %q", c.axis)
			}
		})
	}

	// Monotone floor: the constitutional axis must be 100%. The routine axis is
	// reported so drift is visible; today it must also be 100% (all are fixed cases).
	if constTotal == 0 || constBlocked != constTotal {
		t.Fatalf("constitutional block rate %d/%d — the destruction floor regressed", constBlocked, constTotal)
	}
	if routinePassed != routineTotal {
		t.Fatalf("routine pass rate %d/%d — a false-positive regressed", routinePassed, routineTotal)
	}
	t.Logf("guard corpus: constitutional %d/%d blocked (floor), routine %d/%d passed",
		constBlocked, constTotal, routinePassed, routineTotal)
}
