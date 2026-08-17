package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/humanidentity"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/plant"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
)

type staticHumanIdentityResponseHandler struct {
	response surfaces.Response
}

func (handler staticHumanIdentityResponseHandler) Handle(context.Context, surfaces.Request) (surfaces.Response, error) {
	return handler.response, nil
}

func TestHumanIdentityPresentationIsBoundToVerifiedConfiguration(t *testing.T) {
	repository := t.TempDir()
	if output, err := exec.Command("git", "init", "-q", repository).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	configRaw := []byte(`{"schema_version":4,"identity":{"human":{"kind":"command","command":"gh","args":["api","user","--jq",".login"]}},"project":{"name":"fixture","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli","codex"],"projections":["codex"]}`)
	configPath := filepath.Join(repository, ".boatstack", "project.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, configRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	resolver, err := plant.NewResolver("")
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := resolver.ResolveInvocation(context.Background(), repository, "cli", "human-identity-test")
	if err != nil {
		t.Fatal(err)
	}
	layout, invocation, err := resolver.ResolveLayout(context.Background(), invocation)
	if err != nil {
		t.Fatal(err)
	}
	config, configFingerprint, err := protocol.ProjectConfigFingerprint(configRaw)
	if err != nil {
		t.Fatal(err)
	}
	state := durable.Default(invocation, time.Now().UTC())
	policy := config.ControlPolicy()
	state.Configuration, state.ConfigFingerprint = model.ConfigurationVerified, configFingerprint
	state.PlanApprovalPolicy, state.VisualEvidencePolicy, state.ExternalEffectPolicy = policy.PlanApproval, policy.VisualEvidence, policy.ExternalEffectAuthority
	state.IndependentReview, state.EnabledHosts = policy.IndependentReviewForHighRisk, policy.Hosts
	stateRaw, err := durable.EncodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(layout.StatePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.StatePath, stateRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := boatstackruntime.NewControlBundleSnapshot(map[string][]byte{".boatstack/project.json": configRaw})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := boatstackruntime.NewControlBundleContract(snapshot, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	request := surfaces.Request{Repository: repository, Host: "cli", CorrelationID: "human-identity-test", ControlBundle: &contract}
	presentation, err := humanIdentityPresentationForRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	wantDescriptor := humanidentity.Descriptor{Kind: humanidentity.KindCommand, Command: "gh", Args: []string{"api", "user", "--jq", ".login"}}
	if !reflect.DeepEqual(presentation.Descriptor, wantDescriptor) || presentation.Validate() != nil {
		t.Fatalf("presentation = %#v", presentation)
	}
	response := surfaces.Response{Question: &surfaces.Question{Authority: []catalog.AuthorityClass{catalog.AuthorityHuman}}}
	if err := attachHumanIdentity(request, &response); err != nil || response.Question.HumanIdentity == nil || !reflect.DeepEqual(*response.Question.HumanIdentity, presentation) {
		t.Fatalf("attached identity = %#v, err=%v", response.Question.HumanIdentity, err)
	}
	programChange, err := handleWithHumanIdentity(context.Background(), staticHumanIdentityResponseHandler{response: surfaces.Response{ProgramChange: &surfaces.ProgramChange{
		PriorProgramFingerprint: strings.Repeat("a", 64), CandidateProgramFingerprint: strings.Repeat("b", 64),
		ProgramDeltaFingerprint: strings.Repeat("c", 64), RequiredTransition: "installation.reconcile-update", AcceptanceFlag: "--accept-program-change",
	}}}, request)
	if err != nil || programChange.ProgramChange.HumanIdentity == nil || !reflect.DeepEqual(*programChange.ProgramChange.HumanIdentity, presentation) {
		t.Fatalf("program-change identity = %#v, err=%v", programChange.ProgramChange.HumanIdentity, err)
	}

	if err := os.WriteFile(configPath, []byte(strings.ReplaceAll(string(configRaw), ".login", ".name")), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := humanIdentityPresentationForRequest(request); err == nil || !strings.Contains(err.Error(), "HUMAN_IDENTITY_DRIFT") {
		t.Fatalf("changed configuration was not rejected: %v", err)
	}
}

func TestHumanIdentityIsAttachedOnlyToHumanAuthorityQuestions(t *testing.T) {
	response := surfaces.Response{Question: &surfaces.Question{Authority: []catalog.AuthorityClass{catalog.AuthorityRepository}}}
	if err := attachHumanIdentity(surfaces.Request{}, &response); err != nil || response.Question.HumanIdentity != nil {
		t.Fatalf("non-human question gained identity: %#v err=%v", response.Question.HumanIdentity, err)
	}
}

func TestPreconfigurationInitializationUsesOnlyExplicitActorBootstrap(t *testing.T) {
	response := surfaces.Response{Question: &surfaces.Question{TransitionID: "installation.initialize", Authority: []catalog.AuthorityClass{catalog.AuthorityHuman}}}
	if err := attachHumanIdentity(surfaces.Request{ControlBundle: &boatstackruntime.ControlBundleContract{}}, &response); err != nil || response.Question.HumanIdentity != nil {
		t.Fatalf("preconfiguration bootstrap identity = %#v err=%v", response.Question.HumanIdentity, err)
	}
	response.Question.TransitionID = "plan.approve"
	if err := attachHumanIdentity(surfaces.Request{}, &response); err == nil || !strings.Contains(err.Error(), "HUMAN_IDENTITY_UNBOUND") {
		t.Fatalf("configured human boundary did not fail closed: %v", err)
	}
}

func TestUnverifiedConfigurationRepairPreservesExplicitActorQuestion(t *testing.T) {
	stale := &model.Snapshot{Observation: model.Observation{Configuration: model.Known(model.ConfigurationStale, model.Evidence{
		Source: "configuration:test", Fingerprint: strings.Repeat("a", 64),
	})}}
	for _, transitionID := range []catalog.TransitionID{"configuration.initialize", "configuration.mutate", "configuration.reconcile"} {
		response := surfaces.Response{Snapshot: stale, Question: &surfaces.Question{
			TransitionID: transitionID, Authority: []catalog.AuthorityClass{catalog.AuthorityHuman},
		}}
		if err := attachHumanIdentity(surfaces.Request{}, &response); err != nil || response.Question.HumanIdentity != nil {
			t.Fatalf("%s repair question = %#v, err=%v", transitionID, response.Question, err)
		}
	}

	response := surfaces.Response{Snapshot: stale, Question: &surfaces.Question{
		TransitionID: "plan.approve", Authority: []catalog.AuthorityClass{catalog.AuthorityHuman},
	}}
	if err := attachHumanIdentity(surfaces.Request{}, &response); err == nil || !strings.Contains(err.Error(), "HUMAN_IDENTITY_UNBOUND") {
		t.Fatalf("ordinary stale-config question did not fail closed: %v", err)
	}

	response.Question.TransitionID = "configuration.mutate"
	response.ProgramChange = &surfaces.ProgramChange{}
	if err := attachHumanIdentity(surfaces.Request{}, &response); err == nil || !strings.Contains(err.Error(), "HUMAN_IDENTITY_UNBOUND") {
		t.Fatalf("program change escaped identity binding through repair question: %v", err)
	}
}

func TestHumanIdentityRenderingPreservesStructuredArgvWithoutExecutingIt(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "executed")
	descriptor := humanidentity.Descriptor{Kind: humanidentity.KindCommand, Command: "touch", Args: []string{marker}}
	presentation, err := humanidentity.NewPresentation(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	response := surfaces.Response{Delegation: &surfaces.DelegationRequired{
		Code: "DELEGATION_REQUIRED", RunID: "run-example", RequestFingerprint: strings.Repeat("a", 64),
		Authorities: []catalog.AuthorityClass{catalog.AuthorityAutonomy}, Description: "authorize exact run", HumanIdentity: presentation,
	}}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var decoded surfaces.Response
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Delegation == nil || decoded.Delegation.HumanIdentity.Descriptor.Command != "touch" || !reflect.DeepEqual(decoded.Delegation.HumanIdentity.Descriptor.Args, []string{marker}) {
		t.Fatalf("structured JSON lost command argv: %#v", decoded.Delegation)
	}
	output, err := captureStdout(t, func() error { return renderResponse(response, "text") })
	expectedCommand := "human_identity_command=" + strconv.Quote("touch") + " " + strconv.Quote(marker)
	if err != nil || !strings.Contains(string(output), expectedCommand) {
		t.Fatalf("text output = %q, err=%v", output, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("rendering executed identity command: %v", err)
	}
	programChange := surfaces.Response{Error: "PROGRAM_DRIFT", ProgramChange: &surfaces.ProgramChange{
		PriorProgramFingerprint: strings.Repeat("b", 64), CandidateProgramFingerprint: strings.Repeat("c", 64),
		ProgramDeltaFingerprint: strings.Repeat("d", 64), RequiredTransition: "installation.reconcile-update", AcceptanceFlag: "--accept-program-change", HumanIdentity: &presentation,
	}}
	output, err = captureStdout(t, func() error { return renderResponse(programChange, "text") })
	if err != nil || !strings.Contains(string(output), expectedCommand) {
		t.Fatalf("program-change text output = %q, err=%v", output, err)
	}
}

func TestAuthorizationReceiptIdentityBindsIdentityProvider(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	requestFingerprint := strings.Repeat("a", 64)
	first := authorizationReceiptID(requestFingerprint, "operator", strings.Repeat("b", 64), 1, now)
	second := authorizationReceiptID(requestFingerprint, "operator", strings.Repeat("c", 64), 1, now)
	if first == second {
		t.Fatal("identity provider change preserved authorization receipt ID")
	}
}
