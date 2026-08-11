package main

import (
	"os"
	"path/filepath"
	"testing"

	boatstack "github.com/operatorstack/boatstack/boatstack"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/surfaces"
)

func TestEveryFriendlyMutationAliasMapsToOneRegistryTransition(t *testing.T) {
	// control-law: cli-verbs-are-adapters-not-transition-authority
	registry := catalog.Default()
	commands := []string{"init", "update", "attach", "detach", "hydrate-runtime", "configure", "goal-configure", "plan-create", "plan-validate", "plan-approve", "plan-activate", "plan-amend", "workspace-cut", "workspace-sync", "workspace-cleanup", "workspace-reap", "record-build", "record-test", "record-review", "record-change", "record-journey", "publication-preview", "publish-pr", "observe-pr", "correct-pr", "abandon"}
	for _, command := range commands {
		operation, transitionID, _, err := classifyCommand(command)
		if err != nil {
			t.Errorf("%s: %v", command, err)
			continue
		}
		if operation != surfaces.OperationApply {
			t.Errorf("%s operation = %s, want apply", command, operation)
		}
		if _, ok := registry.Lookup(transitionID); !ok {
			t.Errorf("%s maps to unregistered transition %s", command, transitionID)
		}
	}
}

func TestParametersRejectMissingEquals(t *testing.T) {
	if _, err := parseParameters([]string{"unsafe"}); err == nil {
		t.Fatal("malformed transition parameter was accepted")
	}
}

func TestApplyWithoutRestatedGoalGetsCommandScopedFlowIdentity(t *testing.T) {
	request, err := buildRequest(surfaces.OperationApply, commandOptions{transitionID: "installation.update", host: "cli", repository: "."})
	if err != nil {
		t.Fatal(err)
	}
	if request.FlowID == "" || request.Goal.ID != "" {
		t.Fatalf("request did not preserve configured-goal lookup with a generated flow: %#v", request)
	}
}

func TestCorrectPRBindsExactBodyBytes(t *testing.T) {
	bodyPath := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(bodyPath, []byte("reviewed body"), 0o600); err != nil {
		t.Fatal(err)
	}
	options, err := parseOptions("correct-pr", []string{
		"--param", "publication_id=7", "--param", "body_path=" + bodyPath,
	}, "publication.correct", nil)
	if err != nil {
		t.Fatal(err)
	}
	parameters, err := parseParameters(options.parameters)
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint, ok := parameters.Get("body_sha256"); !ok || fingerprint != hash([]byte("reviewed body")) {
		t.Fatalf("correction body fingerprint = %q, present=%t", fingerprint, ok)
	}
}

func TestBuildRevisionPrefersReleaseEmbeddedSourceCommit(t *testing.T) {
	prior := boatstack.SourceCommit
	boatstack.SourceCommit = "exact-release-source"
	t.Cleanup(func() { boatstack.SourceCommit = prior })
	if got := buildRevision(); got != "exact-release-source" {
		t.Fatalf("build revision = %q", got)
	}
}
