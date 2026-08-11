package distribution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
)

func TestRepositoryScopedProgramsAreIndependentUnderConcurrency(t *testing.T) {
	// control-law: repository-program-selection-has-no-mutable-global-state
	if runtime.GOOS == "windows" {
		t.Skip("the language-neutral Python fixture is exercised on POSIX")
	}
	if _, err := os.Stat("/usr/bin/python3"); err != nil {
		t.Skip("/usr/bin/python3 is unavailable")
	}
	fixturePath, err := filepath.Abs(filepath.Join("..", "extension", "subprocess", "testdata", "reference_extension.py"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	extensions := func(id string) protocol.SubprocessExtensionSettings {
		path := filepath.Join(t.TempDir(), strings.ReplaceAll(id, ".", "-")+".py")
		content := []byte(strings.ReplaceAll(string(source), "fixture.echo", id))
		if err := os.WriteFile(path, content, 0o700); err != nil {
			t.Fatal(err)
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(content)
		return protocol.SubprocessExtensionSettings{
			ID: id, Version: "1.0.0", Executable: resolved, SHA256: hex.EncodeToString(digest[:]), DeadlineMillis: 30_000,
		}
	}
	x, y := extensions("fixture.echo"), extensions("fixture.second")
	candidateConfig := protocol.ProjectConfig{
		SchemaVersion: protocol.ConfigSchemaVersion,
		Project:       protocol.ProjectSettings{Name: "fixture", DefaultBranch: "main", Commands: map[string]string{}},
		Policy:        protocol.PolicySettings{PlanApproval: "human", VisualEvidence: "optional"},
		Hosts:         []string{"cli"}, Extensions: []protocol.SubprocessExtensionSettings{x},
	}
	candidateBytes, err := json.Marshal(candidateConfig)
	if err != nil {
		t.Fatal(err)
	}
	_, candidateFingerprint, err := protocol.ProjectConfigFingerprint(candidateBytes)
	if err != nil {
		t.Fatal(err)
	}
	candidatePath := filepath.Join(t.TempDir(), "candidate.json")
	if err := os.WriteFile(candidatePath, candidateBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	candidateProgram, err := StandardProgramForRepository(context.Background(), RepositoryProgramRequest{
		Repository: repositoryFixture(t, nil), ExternalStateRoot: t.TempDir(), Host: "cli", CorrelationID: "candidate-program",
		ConfigurationPath: candidatePath, ConfigurationFingerprint: candidateFingerprint,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidateProgram.Summary().Extensions) != 1 {
		t.Fatalf("candidate program extensions = %d, want 1", len(candidateProgram.Summary().Extensions))
	}
	if _, err := StandardProgramForRepository(context.Background(), RepositoryProgramRequest{
		Repository: repositoryFixture(t, nil), ExternalStateRoot: t.TempDir(), Host: "cli", CorrelationID: "candidate-mismatch",
		ConfigurationPath: candidatePath, ConfigurationFingerprint: strings.Repeat("0", 64),
	}); err == nil {
		t.Fatal("candidate program accepted a mismatched configuration fingerprint")
	}
	repositories := []string{repositoryFixture(t, nil), repositoryFixture(t, []protocol.SubprocessExtensionSettings{x}), repositoryFixture(t, []protocol.SubprocessExtensionSettings{x, y})}
	expected := []int{0, 1, 2}
	fingerprints := make([]string, len(repositories))
	externalRoots := []string{t.TempDir(), t.TempDir(), t.TempDir()}
	var wait sync.WaitGroup
	errors := make(chan error, len(repositories))
	for index, repository := range repositories {
		wait.Add(1)
		go func(index int, repository string) {
			defer wait.Done()
			program, err := StandardProgramForRepository(context.Background(), RepositoryProgramRequest{
				Repository: repository, ExternalStateRoot: externalRoots[index], Host: "cli", CorrelationID: "repository-program",
			})
			if err != nil {
				errors <- err
				return
			}
			if len(program.Summary().Extensions) != expected[index] {
				errors <- &countError{got: len(program.Summary().Extensions), want: expected[index]}
				return
			}
			fingerprints[index] = program.Fingerprint()
		}(index, repository)
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	if fingerprints[0] == fingerprints[1] || fingerprints[1] == fingerprints[2] || fingerprints[0] == fingerprints[2] {
		t.Fatalf("repository program identities collided: %#v", fingerprints)
	}
}

func TestSnapshotPolicyDoesNotCreateCatalogDriftWithoutCompositionChange(t *testing.T) {
	// control-law: only-repository-policy-that-changes-composition-enters-program-identity
	left := repositoryFixture(t, []protocol.SubprocessExtensionSettings{})
	right := repositoryFixture(t, []protocol.SubprocessExtensionSettings{})
	path := filepath.Join(right, ".boatstack", "project.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var configuration protocol.ProjectConfig
	if err := json.Unmarshal(raw, &configuration); err != nil {
		t.Fatal(err)
	}
	configuration.Policy.PlanApproval = "human-or-autonomy"
	configuration.Policy.VisualEvidence = "required"
	configuration.Hosts = append(configuration.Hosts, "sdk")
	raw, err = json.Marshal(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	compile := func(repository string) string {
		program, err := StandardProgramForRepository(context.Background(), RepositoryProgramRequest{
			Repository: repository, ExternalStateRoot: t.TempDir(), Host: "cli", CorrelationID: "policy-projection",
		})
		if err != nil {
			t.Fatal(err)
		}
		return program.Fingerprint()
	}
	if one, two := compile(left), compile(right); one != two {
		t.Fatalf("snapshot-only policy changed compiled program identity: %s != %s", one, two)
	}
}

type countError struct{ got, want int }

func (e *countError) Error() string { return "extension count mismatch" }

func repositoryFixture(t *testing.T, extensions []protocol.SubprocessExtensionSettings) string {
	t.Helper()
	repository := t.TempDir()
	command := func(arguments ...string) {
		cmd := exec.Command("git", arguments...)
		cmd.Dir = repository
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", arguments, err, output)
		}
	}
	command("init", "-q")
	command("config", "user.email", "boatstack@example.invalid")
	command("config", "user.name", "Boatstack Test")
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	command("add", "README.md")
	command("commit", "-q", "-m", "fixture")
	if extensions != nil {
		configuration := protocol.ProjectConfig{
			SchemaVersion: protocol.ConfigSchemaVersion,
			Project:       protocol.ProjectSettings{Name: "fixture", DefaultBranch: "main", Commands: map[string]string{}},
			Policy:        protocol.PolicySettings{PlanApproval: "human", VisualEvidence: "optional"},
			Hosts:         []string{"cli"}, Extensions: extensions,
		}
		raw, err := json.Marshal(configuration)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(repository, ".boatstack"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repository, ".boatstack", "project.json"), append(raw, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return repository
}
