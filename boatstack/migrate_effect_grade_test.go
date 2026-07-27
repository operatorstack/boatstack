package boatstack

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// migrateGradeRepo builds a repo whose project config declares the given migration
// apply/verify commands. A disposable SQLite database is created under the test's
// temp dir (removed automatically when the test ends — the guaranteed-teardown
// invariant), seeded with one row, and returned as the BOATSTACK_MIGRATE_DB env the
// commands read.
func migrateGradeRepo(t *testing.T, apply, verify string) (repo string, env []string) {
	t.Helper()
	repo = t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".product-loop", "features"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := testConfig()
	config.Project.Migration = MigrationConfig{ApplyCommand: apply, VerifyCommand: verify}
	value, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".product-loop", "project.json"), value, 0o644); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(t.TempDir(), "disposable.sqlite")
	if out, seedErr := exec.Command("sqlite3", db, "CREATE TABLE accounts(id INTEGER); INSERT INTO accounts VALUES (1);").CombinedOutput(); seedErr != nil {
		t.Fatalf("seed disposable db: %v: %s", seedErr, out)
	}
	return repo, []string{"BOATSTACK_MIGRATE_DB=" + db}
}

// Sandboxed-Effect law: a migration's safety is graded by EXECUTING it against a
// fresh, disposable database and reading the oracle — never approximated from its
// SQL text. The guard treats the same migration as inert data; this harness is the
// controlled executor. A repo that declares no migration commands is unaffected.
func TestMigrationEffectGradingSandbox(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not available")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	const apply = `sqlite3 "$BOATSTACK_MIGRATE_DB" < migrate.sql`
	// Verify the invariant the migration must preserve: the seeded row still exists.
	const verify = `test "$(sqlite3 "$BOATSTACK_MIGRATE_DB" 'SELECT count(*) FROM accounts')" = "1"`

	t.Run("skips cleanly when no migration commands are declared", func(t *testing.T) {
		repo, env := migrateGradeRepo(t, "", "")
		result, err := GradeMigrationEffect(repo, env)
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != MigrationEffectSkipped {
			t.Fatalf("unconfigured repo did not skip: %+v", result)
		}
	})

	t.Run("safe forward migration grades PASS", func(t *testing.T) {
		repo, env := migrateGradeRepo(t, apply, verify)
		// A declarative migration full of DDL — the guard treats this as data.
		if err := os.WriteFile(filepath.Join(repo, "migrate.sql"),
			[]byte("ALTER TABLE accounts ADD COLUMN active INTEGER DEFAULT 1;\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		result, err := GradeMigrationEffect(repo, env)
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != MigrationEffectPass {
			t.Fatalf("safe migration did not grade PASS: %+v", result)
		}
	})

	t.Run("destructive migration grades FAIL against the disposable db", func(t *testing.T) {
		repo, env := migrateGradeRepo(t, apply, verify)
		// Dropping the populated table is inert as TEXT (the static guard allows it as
		// a data artifact) but its EFFECT is caught by executing it in the sandbox.
		if err := os.WriteFile(filepath.Join(repo, "migrate.sql"),
			[]byte("DROP TABLE accounts;\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		result, err := GradeMigrationEffect(repo, env)
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != MigrationEffectFail {
			t.Fatalf("destructive migration was not caught by effect grading: %+v", result)
		}
	})
}
