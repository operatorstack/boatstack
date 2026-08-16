package humanidentitybinding

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/plant"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

func TestPresentationUsesRepositoryConfigurationBoundByControlBundle(t *testing.T) {
	repository := identityRepository(t)
	raw := identityConfig("repository-actor")
	writeIdentityFile(t, filepath.Join(repository, ".boatstack", "project.json"), raw)
	bundle, err := boatstackruntime.NewControlBundleSnapshot(map[string][]byte{".boatstack/project.json": raw})
	if err != nil {
		t.Fatal(err)
	}
	presentation, err := PresentationForRepository(context.Background(), t.TempDir(), repository, "sdk", "repository-config", &bundle, nil)
	if err != nil || presentation.Descriptor.Value != "repository-actor" {
		t.Fatalf("presentation = %#v, err=%v", presentation, err)
	}
	writeIdentityFile(t, filepath.Join(repository, ".boatstack", "project.json"), identityConfig("changed-actor"))
	if _, err := PresentationForRepository(context.Background(), t.TempDir(), repository, "sdk", "repository-drift", &bundle, nil); err == nil || !strings.Contains(err.Error(), "HUMAN_IDENTITY_DRIFT") {
		t.Fatalf("unbound repository config change was accepted: %v", err)
	}
}

func TestPresentationUsesExternalConfigurationAuthorityAndVerifiedState(t *testing.T) {
	ctx := context.Background()
	repository := identityRepository(t)
	repositoryRaw := identityConfig("repository-actor")
	writeIdentityFile(t, filepath.Join(repository, ".boatstack", "project.json"), repositoryRaw)
	bundle, err := boatstackruntime.NewControlBundleSnapshot(map[string][]byte{".boatstack/project.json": repositoryRaw})
	if err != nil {
		t.Fatal(err)
	}
	externalBase := t.TempDir()
	resolver, err := plant.NewResolver(externalBase)
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := resolver.ResolveInvocation(ctx, repository, "sdk", "external-setup")
	if err != nil {
		t.Fatal(err)
	}
	sharedRoot := filepath.Join(externalBase, "boatstack", "repositories", embedded.RepositoryID, embedded.GitCommonID)
	bindingRaw, err := durable.EncodeBinding(durable.Binding{
		SchemaVersion: durable.BindingSchemaVersion, RepositoryID: embedded.RepositoryID, GitCommonID: embedded.GitCommonID,
		Topology: model.TopologyDetached, ControllerID: "external-controller", ConfigAuthority: "external", CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	writeIdentityFile(t, filepath.Join(sharedRoot, "binding.json"), bindingRaw)
	externalRaw := identityConfig("external-actor")
	writeIdentityFile(t, filepath.Join(sharedRoot, "project.json"), externalRaw)
	detached, err := resolver.ResolveInvocation(ctx, repository, "sdk", "external-state")
	if err != nil {
		t.Fatal(err)
	}
	layout, detached, err := resolver.ResolveLayout(ctx, detached)
	if err != nil {
		t.Fatal(err)
	}
	config, fingerprint, err := protocol.ProjectConfigFingerprint(externalRaw)
	if err != nil {
		t.Fatal(err)
	}
	observed := &model.Snapshot{Observation: model.Observation{Invocation: detached, Configuration: model.Known(model.ConfigurationVerified, model.Evidence{Source: "configuration:external", Fingerprint: fingerprint})}}
	presentation, err := PresentationForRepository(ctx, externalBase, repository, "sdk", "external-observed", &bundle, observed)
	if err != nil || presentation.Descriptor.Value != "external-actor" {
		t.Fatalf("external presentation = %#v, err=%v", presentation, err)
	}
	wrongInvocation := *observed
	wrongInvocation.Invocation.WorktreeID = "wt-different"
	if _, err := PresentationForRepository(ctx, externalBase, repository, "sdk", "external-wrong-invocation", &bundle, &wrongInvocation); err == nil || !strings.Contains(err.Error(), "different invocation") {
		t.Fatalf("cross-invocation evidence was accepted: %v", err)
	}

	state := durable.Default(detached, time.Now().UTC())
	policy := config.ControlPolicy()
	state.Configuration = model.ConfigurationVerified
	state.ConfigFingerprint = fingerprint
	state.PlanApprovalPolicy = policy.PlanApproval
	state.VisualEvidencePolicy = policy.VisualEvidence
	state.ExternalEffectPolicy = policy.ExternalEffectAuthority
	state.IndependentReview = policy.IndependentReviewForHighRisk
	state.EnabledHosts = policy.Hosts
	stateRaw, err := durable.EncodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	writeIdentityFile(t, layout.StatePath, stateRaw)
	presentation, err = PresentationForRepository(ctx, externalBase, repository, "sdk", "external-durable", &bundle, nil)
	if err != nil || presentation.Descriptor.Value != "external-actor" {
		t.Fatalf("durable external presentation = %#v, err=%v", presentation, err)
	}

	writeIdentityFile(t, filepath.Join(sharedRoot, "project.json"), identityConfig("changed-external-actor"))
	if _, err := PresentationForRepository(ctx, externalBase, repository, "sdk", "external-drift", &bundle, observed); err == nil || !strings.Contains(err.Error(), "HUMAN_IDENTITY_DRIFT") {
		t.Fatalf("external authority drift was accepted: %v", err)
	}
}

func identityRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	command := exec.Command("git", "init", "-q", repository)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	return repository
}

func identityConfig(actor string) []byte {
	return []byte(`{"schema_version":3,"identity":{"human":{"kind":"literal","value":"` + actor + `"}},"project":{"name":"fixture","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli","sdk"]}`)
}

func writeIdentityFile(t *testing.T, path string, raw []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
