package boatstack

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
)

// MigrationEffectStatus is the verdict of grading a migration by its observed effect.
type MigrationEffectStatus string

const (
	// MigrationEffectSkipped means the project declared no migration commands, so no
	// effect was executed — a repository without a database is unaffected.
	MigrationEffectSkipped MigrationEffectStatus = "SKIPPED"
	// MigrationEffectPass means the migration applied and verified against the
	// disposable database.
	MigrationEffectPass MigrationEffectStatus = "PASS"
	// MigrationEffectFail means applying or verifying the migration failed — real
	// breakage the static guard cannot see, because a migration is inert as data.
	MigrationEffectFail MigrationEffectStatus = "FAIL"
)

// MigrationEffectResult is the outcome of GradeMigrationEffect.
type MigrationEffectResult struct {
	Status MigrationEffectStatus
	Reason string
}

// GradeMigrationEffect grades a project's migrations by their OBSERVED EFFECT rather
// than by their SQL text. Sandboxed-Effect law: when an effect's safety cannot be
// certified statically, execute it in a disposable environment and read the oracle;
// never approximate it by reading the source. The guard keeps treating a committed
// migration as a data artifact (it is applied later by the controlled pipeline); this
// harness IS that controlled executor for grading purposes.
//
// It runs the project's configured apply_command, then verify_command, via `sh -c`
// with the caller-provided environment (which carries the disposable database
// coordinate, BOATSTACK_MIGRATE_DB). PASS iff both succeed; FAIL if either fails;
// SKIPPED when no apply_command is configured. The caller owns the disposable
// database and its guaranteed teardown (a fresh-per-run service container in CI, or a
// temp file removed by the test) — this function only executes and grades.
func GradeMigrationEffect(repo string, extraEnv []string) (MigrationEffectResult, error) {
	config, _, err := LoadConfig(WorkspaceFor(repo).ProjectConfigPath())
	if err != nil {
		return MigrationEffectResult{}, err
	}
	apply := strings.TrimSpace(config.Project.Migration.ApplyCommand)
	verify := strings.TrimSpace(config.Project.Migration.VerifyCommand)
	if apply == "" {
		return MigrationEffectResult{Status: MigrationEffectSkipped, Reason: "no migration apply_command is configured; effect grading skipped"}, nil
	}
	if out, runErr := runMigrationShell(repo, apply, extraEnv); runErr != nil {
		return MigrationEffectResult{Status: MigrationEffectFail, Reason: "apply failed: " + firstOutputLine(out)}, nil
	}
	if verify != "" {
		if out, runErr := runMigrationShell(repo, verify, extraEnv); runErr != nil {
			return MigrationEffectResult{Status: MigrationEffectFail, Reason: "verify failed: " + firstOutputLine(out)}, nil
		}
	}
	return MigrationEffectResult{Status: MigrationEffectPass, Reason: "migration applied and verified against the disposable database"}, nil
}

// runMigrationShell keeps stdout (authority-bearing) and stderr (diagnostic)
// separate. Grading needs only the exit status and a diagnostic line, so it reports
// stderr, falling back to stdout when a tool writes its error there.
func runMigrationShell(dir, command string, extraEnv []string) (string, error) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	diagnostic := strings.TrimSpace(stderr.String())
	if diagnostic == "" {
		diagnostic = strings.TrimSpace(stdout.String())
	}
	return diagnostic, err
}

func firstOutputLine(s string) string {
	s = strings.TrimSpace(s)
	if index := strings.IndexByte(s, '\n'); index >= 0 {
		s = s[:index]
	}
	if s == "" {
		return "(no output)"
	}
	return s
}
