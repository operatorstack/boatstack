package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/buildinfo"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
	"github.com/operatorstack/boatstack/boatstack/internal/testprogram"
)

func TestEveryFriendlyMutationAliasMapsToOneRegistryTransition(t *testing.T) {
	// control-law: cli-verbs-are-adapters-not-transition-authority
	registry := testprogram.StandardRegistry()
	commands := []string{"init", "update", "attach", "detach", "hydrate-runtime", "configure", "objective-bind", "plan-create", "plan-validate", "plan-approve", "plan-activate", "plan-amend", "workspace-cut", "workspace-sync", "workspace-cleanup", "workspace-reap", "record-build", "record-test", "record-review", "record-change", "record-journey", "publication-preview", "publish-pr", "observe-pr", "correct-pr", "abandon"}
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

func TestApplyWithoutRestatedObjectiveGetsCommandScopedFlowIdentity(t *testing.T) {
	request, err := buildRequest(surfaces.OperationApply, commandOptions{transitionID: "installation.update", host: "cli", repository: "."})
	if err != nil {
		t.Fatal(err)
	}
	if request.FlowID == "" || request.Objective.ID != "" {
		t.Fatalf("request did not preserve configured-objective lookup with a generated flow: %#v", request)
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
	prior := buildinfo.SourceCommit
	buildinfo.SourceCommit = "exact-release-source"
	t.Cleanup(func() { buildinfo.SourceCommit = prior })
	if got := buildRevision(); got != "exact-release-source" {
		t.Fatalf("build revision = %q", got)
	}
}

func TestTransitionReceiptCannotBeLoadedAsAuthority(t *testing.T) {
	// control-law: historical execution evidence is not a reusable authority token
	path := filepath.Join(t.TempDir(), "transition-receipt.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":5,"id":"trc-old"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadAuthority(commandOptions{authorityReceipts: stringList{path}}, "correlation", model.Objective{}, nil, time.Now().UTC())
	if err == nil {
		t.Fatal("transition receipt was accepted as authority")
	}
}

func TestHumanPublicationConfirmationBindsExactPreviewFingerprint(t *testing.T) {
	// control-law: publication-authority-confirms-exact-preview-bytes
	now := time.Now().UTC()
	options := commandOptions{humanActor: "reviewer", transitionID: "publication.execute"}
	objective := model.Objective{ID: "publish", TargetID: model.ObjectiveOpenPR, DeliveryID: "delivery"}
	one, err := loadAuthority(options, "correlation", objective, protocol.Parameters{{Name: "preview_fingerprint", Value: strings.Repeat("a", 64)}}, now)
	if err != nil {
		t.Fatal(err)
	}
	two, err := loadAuthority(options, "correlation", objective, protocol.Parameters{{Name: "preview_fingerprint", Value: strings.Repeat("b", 64)}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(one.Receipts) != 1 || len(two.Receipts) != 1 || one.Receipts[0].Fingerprint == two.Receipts[0].Fingerprint {
		t.Fatal("different preview fingerprints reused one human confirmation")
	}
}

func TestRawCLIReconstructsExactCapabilityPrescription(t *testing.T) {
	options := commandOptions{
		transitionID: "installation.update", host: "cli", repository: ".", prescriptionID: "prx-test",
		expectedStateRevision: 7, expectedProgramFingerprint: hash([]byte("program")), expectedSnapshotFingerprint: hash([]byte("snapshot")),
		authorityFingerprint:  "auth-test",
		requiredCapabilities:  stringList{string(catalog.CapabilityRepositoryWrite), string(catalog.CapabilityCommandExecute)},
		effectiveCapabilities: stringList{string(catalog.CapabilityRepositoryWrite), string(catalog.CapabilityCommandExecute)},
	}
	request, err := buildRequest(surfaces.OperationApply, options)
	if err != nil {
		t.Fatal(err)
	}
	if request.Prescription.AuthorityFingerprint != options.authorityFingerprint || len(request.Prescription.RequiredCapabilities) != 2 || len(request.Prescription.EffectiveCapabilities) != 2 {
		t.Fatalf("CLI lost capability prescription: %#v", request.Prescription)
	}
}
