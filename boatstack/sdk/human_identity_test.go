package sdk

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/plant"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
)

type identityResponseHandler struct {
	response surfaces.Response
}

func (handler identityResponseHandler) Handle(context.Context, surfaces.Request) (surfaces.Response, error) {
	return handler.response, nil
}

func TestSDKResponseBoundaryAttachesVerifiedHumanIdentity(t *testing.T) {
	repository := t.TempDir()
	if output, err := exec.Command("git", "init", "-q", repository).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	raw := []byte(`{"schema_version":5,"identity":{"default":"developer","roles":{"developer":{"kind":"literal","value":"sdk-operator"}}},"project":{"name":"sdk-fixture","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli","sdk"],"projections":[]}`)
	configPath := filepath.Join(repository, ".boatstack", "project.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	controlSnapshot, err := boatstackruntime.NewControlBundleSnapshot(map[string][]byte{".boatstack/project.json": raw})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := boatstackruntime.NewControlBundleContract(controlSnapshot, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	externalStateRoot := t.TempDir()
	resolver, err := plant.NewResolver(externalStateRoot)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := resolver.ResolveInvocation(context.Background(), repository, HostIdentity, "sdk-human-identity")
	if err != nil {
		t.Fatal(err)
	}
	_, configFingerprint, err := protocol.ProjectConfigFingerprint(raw)
	if err != nil {
		t.Fatal(err)
	}
	observed := &model.Snapshot{Observation: model.Observation{Invocation: invocation, Configuration: model.Known(model.ConfigurationVerified, model.Evidence{Source: "configuration:sdk", Fingerprint: configFingerprint})}}
	request := Request{Repository: repository, Host: HostIdentity, CorrelationID: "sdk-human-identity", ControlBundle: &contract}
	response, err := (Client{externalStateRoot: externalStateRoot}).handle(context.Background(), identityResponseHandler{response: surfaces.Response{
		Question: &surfaces.Question{Authority: []catalog.AuthorityClass{catalog.AuthorityHuman}}, Snapshot: observed,
	}}, request)
	if err != nil || response.Question == nil || response.Question.HumanIdentity == nil {
		t.Fatalf("SDK response identity = %#v, err=%v", response.Question, err)
	}
	if response.Question.HumanIdentity.Descriptor.Value != "sdk-operator" {
		t.Fatalf("SDK response selected %q", response.Question.HumanIdentity.Descriptor.Value)
	}
}
