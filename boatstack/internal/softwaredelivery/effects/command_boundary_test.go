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
	outputs   [][]byte
	directory string
	name      string
	arguments []string
	history   [][]string
}

func (r *boundaryRunner) CombinedOutput(_ context.Context, directory, name string, arguments ...string) ([]byte, error) {
	r.calls++
	r.directory = directory
	r.name = name
	r.arguments = append([]string(nil), arguments...)
	r.history = append(r.history, append([]string{name}, arguments...))
	if len(r.outputs) >= r.calls {
		return append([]byte(nil), r.outputs[r.calls-1]...), r.err
	}
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

func TestGitHubProviderAuthorityIsDerivedFromWriteCapability(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	observation := []byte(`{"nameWithOwner":"owner/repository","url":"https://github.com/owner/repository","viewerPermission":"WRITE"}`)
	runner := &boundaryRunner{outputs: [][]byte{[]byte("git@github.com:owner/repository.git\n"), observation, []byte("git@github.com:owner/repository.git\n"), observation}}
	boundary, err := NewNativeBoundaryWithRunner(runner)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := strings.Repeat("a", 64)
	receipt, err := boundary.ResolveGitHubProviderAuthority(context.Background(), t.TempDir(), fingerprint, now)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Class != catalog.AuthorityProvider || receipt.Subject != "github:owner/repository" || receipt.Fingerprint != fingerprint || !receipt.ExpiresAt.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("provider receipt = %#v", receipt)
	}
	if runner.name != "gh" || strings.Join(runner.arguments, " ") != "repo view owner/repository --json nameWithOwner,viewerPermission,url" || strings.Join(runner.history[0], " ") != "git remote get-url --push origin" {
		t.Fatalf("provider observation command = %s %v", runner.name, runner.arguments)
	}
	renewed, err := boundary.ResolveGitHubProviderAuthority(context.Background(), t.TempDir(), fingerprint, now.Add(time.Minute))
	if err != nil || renewed.ID != receipt.ID {
		t.Fatalf("provider identity changed across renewal: first=%q renewed=%q err=%v", receipt.ID, renewed.ID, err)
	}
}

func TestGitHubProviderAuthorityAcceptsOpaqueTrustedTransitionBinding(t *testing.T) {
	runner := &boundaryRunner{outputs: [][]byte{[]byte("https://github.com/owner/repository.git\n"), []byte(`{"nameWithOwner":"owner/repository","url":"https://github.com/owner/repository","viewerPermission":"WRITE"}`)}}
	boundary, _ := NewNativeBoundaryWithRunner(runner)
	receipt, err := boundary.ResolveGitHubProviderAuthority(context.Background(), t.TempDir(), "123", time.Unix(100, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Fingerprint != "123" {
		t.Fatalf("provider binding = %q", receipt.Fingerprint)
	}
}

func TestGitHubProviderAuthorityRejectsUnsafeBinding(t *testing.T) {
	boundary, _ := NewNativeBoundaryWithRunner(&boundaryRunner{})
	for _, binding := range []string{"", " publication", "publication\n"} {
		if _, err := boundary.ResolveGitHubProviderAuthority(context.Background(), t.TempDir(), binding, time.Unix(100, 0).UTC()); err == nil || !strings.Contains(err.Error(), "PROVIDER_AUTHORITY_INVALID") {
			t.Fatalf("unsafe binding %q error = %v", binding, err)
		}
	}
}

func TestGitHubProviderAuthorityRejectsRepositoryDifferentFromOrigin(t *testing.T) {
	runner := &boundaryRunner{outputs: [][]byte{
		[]byte("git@github.com:owner/destination.git\n"),
		[]byte(`{"nameWithOwner":"owner/ambient","url":"https://github.com/owner/ambient","viewerPermission":"WRITE"}`),
	}}
	boundary, _ := NewNativeBoundaryWithRunner(runner)
	_, err := boundary.ResolveGitHubProviderAuthority(context.Background(), t.TempDir(), "binding", time.Unix(100, 0).UTC())
	if err == nil || !strings.Contains(err.Error(), "does not match the origin") || runner.calls != 2 {
		t.Fatalf("mismatched provider authority error=%v calls=%d", err, runner.calls)
	}
}

func TestGitHubProviderAuthorityRejectsReadOnlyIdentity(t *testing.T) {
	runner := &boundaryRunner{outputs: [][]byte{[]byte("git@github.com:owner/repository.git\n"), []byte(`{"nameWithOwner":"owner/repository","url":"https://github.com/owner/repository","viewerPermission":"READ"}`)}}
	boundary, _ := NewNativeBoundaryWithRunner(runner)
	_, err := boundary.ResolveGitHubProviderAuthority(context.Background(), t.TempDir(), strings.Repeat("a", 64), time.Unix(100, 0).UTC())
	if err == nil || !strings.Contains(err.Error(), "PROVIDER_AUTHORITY_DENIED") {
		t.Fatalf("read-only provider authority error = %v", err)
	}
}

func TestPublicationPreviewRequiresCommittedProductWorktree(t *testing.T) {
	transition, _ := testprogram.StandardRegistry().Lookup("publication.preview")
	admission := boundaryAdmission(transition)
	admission.SourceRevision = "revision"
	runner := &boundaryRunner{output: []byte(" M product.go\x00")}
	boundary, _ := NewNativeBoundaryWithRunner(runner)
	err := boundary.PrepareObservation(context.Background(), admission, transition, writeBoundaryConfig(t, "go test ./..."), &durable.State{})
	if err == nil || !strings.Contains(err.Error(), "WORKSPACE_COMMIT_REQUIRED") || runner.calls != 1 {
		t.Fatalf("dirty publication preview error=%v calls=%d", err, runner.calls)
	}
}

func TestPublicationPreviewAcceptsExactCleanCommittedHead(t *testing.T) {
	transition, _ := testprogram.StandardRegistry().Lookup("publication.preview")
	admission := boundaryAdmission(transition)
	admission.SourceRevision = "revision"
	runner := &boundaryRunner{outputs: [][]byte{[]byte("?? .boatstack/publication/delivery.md\x00"), []byte("revision\n")}}
	boundary, _ := NewNativeBoundaryWithRunner(runner)
	if err := boundary.PrepareObservation(context.Background(), admission, transition, writeBoundaryConfig(t, "go test ./..."), &durable.State{}); err != nil {
		t.Fatal(err)
	}
	if runner.calls != 2 {
		t.Fatalf("publication preflight calls = %d", runner.calls)
	}
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
		SchemaVersion: 2, DeliveryID: "delivery", BaseRef: "main", HeadRef: "feature",
		SourceRevision: "revision", WorktreeFingerprint: "worktree",
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
	runner := &boundaryRunner{outputs: [][]byte{[]byte("push complete"), []byte("https://github.com/operatorstack/boatstack/pull/222\n")}}
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
	preview := publicationPreview{
		SchemaVersion: 2, DeliveryID: "delivery", BaseRef: "main", HeadRef: "feature",
		SourceRevision: "revision", WorktreeFingerprint: "worktree",
		BodyPath: bodyPath, BodySHA256: sha256Bytes(body), CreatedAt: time.Unix(10, 0).UTC(),
	}
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
		Invocation: model.InvocationContext{Ref: "refs/heads/feature"}, SourceRevision: "revision", WorktreeFingerprint: "worktree",
		Objective: model.Objective{DeliveryID: "delivery"}, Parameters: protocol.Parameters{{Name: "preview_fingerprint", Value: preview.Fingerprint}},
	}
	admission.RequiredCapabilities = catalog.RequiredCapabilities(transition)
	admission.EffectiveCapabilities = admission.RequiredCapabilities
	result, err := boundary.Execute(context.Background(), admission, transition, layout, durable.State{})
	if err != nil {
		t.Fatal(err)
	}
	if publicationID, ok := result.Outputs.Get("publication_id"); !ok || publicationID != "222" {
		t.Fatalf("publication effect outputs = %#v", result.Outputs)
	}
	want := []string{"pr", "create", "--base", "main", "--head", "feature", "--fill-first", "--body-file", bodyPath}
	if runner.name != "gh" || strings.Join(runner.arguments, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("publication command = %s %q, want gh %q", runner.name, runner.arguments, want)
	}
	if len(runner.history) != 2 || strings.Join(runner.history[0], " ") != "git push origin revision:refs/heads/feature" {
		t.Fatalf("publication push history = %v", runner.history)
	}
}

func TestPublicationExecutionRejectsCommittedHeadDrift(t *testing.T) {
	layout := writeBoundaryConfig(t, "go test ./...")
	bodyPath := filepath.Join(layout.RepositoryRoot, "body.md")
	body := []byte("reviewed body")
	if err := os.WriteFile(bodyPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	preview := publicationPreview{
		SchemaVersion: 2, DeliveryID: "delivery", BaseRef: "main", HeadRef: "feature",
		SourceRevision: "old-revision", WorktreeFingerprint: "worktree",
		BodyPath: bodyPath, BodySHA256: sha256Bytes(body), CreatedAt: time.Unix(10, 0).UTC(),
	}
	identity := preview
	identity.CreatedAt = time.Time{}
	raw, _ := json.Marshal(identity)
	preview.Fingerprint = sha256Bytes(raw)
	admission := protocol.Admission{
		Invocation: model.InvocationContext{Ref: "refs/heads/feature"}, SourceRevision: "new-revision", WorktreeFingerprint: "worktree",
		Objective: model.Objective{DeliveryID: "delivery"}, Parameters: protocol.Parameters{{Name: "preview_fingerprint", Value: preview.Fingerprint}},
	}
	if err := validatePublicationPreviewForAdmission(layout, admission, preview); err == nil || !strings.Contains(err.Error(), "exact committed HEAD") {
		t.Fatalf("committed-head drift error = %v", err)
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
