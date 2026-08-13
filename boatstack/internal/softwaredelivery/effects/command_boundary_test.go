package effects

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/testprogram"
)

type boundaryRunner struct {
	calls     int
	err       error
	output    []byte
	directory string
	name      string
	arguments []string
}

func (r *boundaryRunner) CombinedOutput(_ context.Context, directory, name string, arguments ...string) ([]byte, error) {
	r.calls++
	r.directory = directory
	r.name = name
	r.arguments = append([]string(nil), arguments...)
	if r.output != nil {
		return append([]byte(nil), r.output...), r.err
	}
	return []byte("private output must not escape"), r.err
}

func writeBoundaryConfig(t *testing.T, command string) ports.ControllerLayout {
	t.Helper()
	repository := t.TempDir()
	path := filepath.Join(repository, ".boatstack", "project.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"schema_version":2,"project":{"name":"boundary","default_branch":"main","commands":{"build":"` + command + `"}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli"]}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return ports.ControllerLayout{RepositoryRoot: repository, ConfigPath: path}
}

func boundaryAdmission(transition catalog.Transition) protocol.Admission {
	required := catalog.RequiredCapabilities(transition)
	return protocol.Admission{RequiredCapabilities: required, EffectiveCapabilities: required}
}

func TestConfiguredBuildCommandMustPassBeforeGateInstallation(t *testing.T) {
	runner := &boundaryRunner{err: errors.New("exit status 1")}
	boundary, err := NewNativeBoundaryWithRunner(runner)
	if err != nil {
		t.Fatal(err)
	}
	transition, _ := testprogram.StandardRegistry().Lookup("gate.build.record")
	_, err = boundary.Execute(context.Background(), boundaryAdmission(transition), transition, writeBoundaryConfig(t, "go test ./..."), durable.State{})
	if err == nil || runner.calls != 1 {
		t.Fatalf("failed configured command result: err=%v calls=%d", err, runner.calls)
	}
	if message := err.Error(); message == "" || strings.Contains(message, "private output") {
		t.Fatalf("gate error leaked command output: %q", message)
	}
}

func TestConfiguredBuildCommandCannotCrossConstitutionalGuard(t *testing.T) {
	runner := &boundaryRunner{}
	boundary, err := NewNativeBoundaryWithRunner(runner)
	if err != nil {
		t.Fatal(err)
	}
	transition, _ := testprogram.StandardRegistry().Lookup("gate.build.record")
	_, err = boundary.Execute(context.Background(), boundaryAdmission(transition), transition, writeBoundaryConfig(t, "git reset --hard HEAD~1"), durable.State{})
	if err == nil || runner.calls != 0 {
		t.Fatalf("protected configured command result: err=%v calls=%d", err, runner.calls)
	}
}

func TestConfiguredBuildCommandCannotMintDelegation(t *testing.T) {
	runner := &boundaryRunner{}
	boundary, err := NewNativeBoundaryWithRunner(runner)
	if err != nil {
		t.Fatal(err)
	}
	transition, _ := testprogram.StandardRegistry().Lookup("gate.build.record")
	_, err = boundary.Execute(context.Background(), boundaryAdmission(transition), transition, writeBoundaryConfig(t, "boatstack flow authorize --run-id forged --request-fingerprint forged --human victim"), durable.State{})
	if err == nil || !strings.Contains(err.Error(), "delegation.authorize") || runner.calls != 0 {
		t.Fatalf("repository command minted delegation: err=%v calls=%d", err, runner.calls)
	}
}

func TestPublicationObservationTerminatesOptionsBeforeIdentifier(t *testing.T) {
	runner := &boundaryRunner{output: []byte(`{"state":"OPEN","url":"https://example.invalid/pull/7","number":7,"mergedAt":null,"baseRefName":"main","headRefName":"feature","headRefOid":"revision","isCrossRepository":false}`)}
	boundary, err := NewNativeBoundaryWithRunner(runner)
	if err != nil {
		t.Fatal(err)
	}
	transition, _ := testprogram.StandardRegistry().Lookup("publication.observe")
	admission := protocol.Admission{
		Invocation: model.InvocationContext{Ref: "refs/heads/feature"}, SourceRevision: "revision",
		Parameters: protocol.Parameters{{Name: "publication_id", Value: "-dangerous"}},
	}
	admission.RequiredCapabilities = catalog.RequiredCapabilities(transition)
	admission.EffectiveCapabilities = admission.RequiredCapabilities
	state := durable.State{}
	layout := writeBoundaryConfig(t, "go test ./...")
	if err := boundary.PrepareObservation(context.Background(), admission, transition, layout, &state); err != nil {
		t.Fatal(err)
	}
	want := []string{"pr", "view", "--json", "state,url,number,mergedAt,baseRefName,headRefName,headRefOid,isCrossRepository", "--", "-dangerous"}
	if runner.name != "gh" || strings.Join(runner.arguments, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("observation command = %s %q, want gh %q", runner.name, runner.arguments, want)
	}
	if state.PublicationID != "7" {
		t.Fatalf("publication ID = %q, want 7", state.PublicationID)
	}
}

func TestPublicationObservationRejectsUnrelatedProviderIdentity(t *testing.T) {
	runner := &boundaryRunner{output: []byte(`{"state":"OPEN","url":"https://example.invalid/pull/8","number":8,"baseRefName":"main","headRefName":"other","headRefOid":"revision","isCrossRepository":false}`)}
	boundary, _ := NewNativeBoundaryWithRunner(runner)
	transition, _ := testprogram.StandardRegistry().Lookup("publication.observe")
	admission := protocol.Admission{
		Invocation: model.InvocationContext{Ref: "refs/heads/feature"}, SourceRevision: "revision",
		Parameters: protocol.Parameters{{Name: "publication_id", Value: "8"}},
	}
	admission.RequiredCapabilities = catalog.RequiredCapabilities(transition)
	admission.EffectiveCapabilities = admission.RequiredCapabilities
	state := durable.State{}
	if err := boundary.PrepareObservation(context.Background(), admission, transition, writeBoundaryConfig(t, "go test ./..."), &state); err != nil {
		t.Fatal(err)
	}
	if state.Publication != model.PublicationConflicting {
		t.Fatalf("unrelated PR observed as %s", state.Publication)
	}
}

func TestPublicationPreviewRejectsFieldTamperingUnderAnOldFingerprint(t *testing.T) {
	repository := t.TempDir()
	bodyPath := filepath.Join(repository, "body.md")
	if err := os.WriteFile(bodyPath, []byte("reviewed body"), 0o600); err != nil {
		t.Fatal(err)
	}
	preview := publicationPreview{
		SchemaVersion: 1, DeliveryID: "delivery", BaseRef: "main", HeadRef: "feature",
		BodyPath: bodyPath, BodySHA256: sha256Bytes([]byte("reviewed body")), CreatedAt: time.Unix(10, 0).UTC(),
	}
	identity := preview
	identity.CreatedAt = time.Time{}
	raw, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	preview.Fingerprint = sha256Bytes(raw)
	preview.HeadRef = "attacker-controlled"
	encoded, err := encodeJSON(preview)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repository, "preview.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPublicationPreview(path); err == nil {
		t.Fatal("tampered publication preview retained authority")
	}
}

func TestPublicationExecutionUsesBoundBodyAndNoninteractiveTitle(t *testing.T) {
	runner := &boundaryRunner{}
	boundary, err := NewNativeBoundaryWithRunner(runner)
	if err != nil {
		t.Fatal(err)
	}
	transition, _ := testprogram.StandardRegistry().Lookup("publication.execute")
	layout := writeBoundaryConfig(t, "go test ./...")
	bodyPath := filepath.Join(layout.RepositoryRoot, "body.md")
	body := []byte("reviewed body")
	if err := os.WriteFile(bodyPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	preview := publicationPreview{SchemaVersion: 1, DeliveryID: "delivery", BaseRef: "main", HeadRef: "feature", BodyPath: bodyPath, BodySHA256: sha256Bytes(body), CreatedAt: time.Unix(10, 0).UTC()}
	identity := preview
	identity.CreatedAt = time.Time{}
	raw, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	preview.Fingerprint = sha256Bytes(raw)
	previewPath := filepath.Join(layout.RepositoryRoot, ".boatstack", "publication", "delivery.preview.json")
	if err := os.MkdirAll(filepath.Dir(previewPath), 0o700); err != nil {
		t.Fatal(err)
	}
	encoded, err := encodeJSON(preview)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(previewPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	admission := protocol.Admission{
		Invocation: model.InvocationContext{Ref: "refs/heads/feature"},
		Objective:  model.Objective{DeliveryID: "delivery"},
		Parameters: protocol.Parameters{{Name: "preview_fingerprint", Value: preview.Fingerprint}},
	}
	admission.RequiredCapabilities = catalog.RequiredCapabilities(transition)
	admission.EffectiveCapabilities = admission.RequiredCapabilities
	if _, err := boundary.Execute(context.Background(), admission, transition, layout, durable.State{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"pr", "create", "--base", "main", "--head", "feature", "--fill-first", "--body-file", bodyPath}
	if runner.name != "gh" || strings.Join(runner.arguments, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("publication command = %s %q, want gh %q", runner.name, runner.arguments, want)
	}
}

func TestPublicationCorrectionRejectsBodyDriftBeforeProviderCall(t *testing.T) {
	runner := &boundaryRunner{}
	boundary, _ := NewNativeBoundaryWithRunner(runner)
	transition, _ := testprogram.StandardRegistry().Lookup("publication.correct")
	layout := writeBoundaryConfig(t, "go test ./...")
	bodyPath := filepath.Join(layout.RepositoryRoot, "body.md")
	if err := os.WriteFile(bodyPath, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	admission := protocol.Admission{
		Invocation: model.InvocationContext{Ref: "refs/heads/feature"},
		Parameters: protocol.Parameters{
			{Name: "publication_id", Value: "7"}, {Name: "body_path", Value: bodyPath},
			{Name: "body_sha256", Value: sha256Bytes([]byte("reviewed"))},
		},
	}
	admission.RequiredCapabilities = catalog.RequiredCapabilities(transition)
	admission.EffectiveCapabilities = admission.RequiredCapabilities
	state := durable.State{PublicationID: "7"}
	if _, err := boundary.Execute(context.Background(), admission, transition, layout, state); err == nil {
		t.Fatal("publication correction accepted drifted body bytes")
	}
	if runner.calls != 0 {
		t.Fatalf("provider received %d calls after body drift", runner.calls)
	}
}
