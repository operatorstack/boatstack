package boatstack

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func visualTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Boatstack Test")
	runGit(t, repo, "config", "user.email", "boatstack@example.invalid")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "base")
	return repo
}

func TestPRVisualCapabilityCutCoversRepositoryAndHostBrowserConsumers(t *testing.T) {
	repo := visualTestRepo(t)
	config := testConfig()
	config.Project.Commands["e2e"] = "npm run e2e"
	capability, err := ResolvePRVisualCaptureCapability(repo, config, true, "npm run dev", PRVisualCapabilityReceipt{})
	if err != nil || capability.Kind != "repository-command" {
		t.Fatalf("repository capability did not win: %#v %v", capability, err)
	}
	delete(config.Project.Commands, "e2e")
	capability, err = ResolvePRVisualCaptureCapability(repo, config, true, "npm run dev", PRVisualCapabilityReceipt{})
	if err != nil || capability.Kind != "host-browser" {
		t.Fatalf("host browser consumer was not selected: %#v %v", capability, err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	if err := ProbePRVisualReadiness(context.Background(), server.URL, time.Second); err != nil {
		t.Fatalf("representative dev-server readiness failed: %v", err)
	}
}

func writeTestPNG(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	canvas := image.NewRGBA(image.Rect(0, 0, 4, 3))
	canvas.Set(1, 1, color.RGBA{R: 240, G: 160, B: 20, A: 255})
	if err := png.Encode(file, canvas); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPRVisualEvidenceIsMachineLocalFreshAndExact(t *testing.T) {
	repo := visualTestRepo(t)
	pngPath := filepath.Join(t.TempDir(), "warning.png")
	writeTestPNG(t, pngPath)
	manifest, err := SavePRVisualEvidence(repo, PRVisualEvidenceManifest{
		Key: "feature-warning", Policy: "suggest", Relevance: "relevant", RelevanceSource: "human-provided",
		Status: "PASS", SourceCommit: runGit(t, repo, "rev-parse", "HEAD"), ProductDiffSHA256: strings.Repeat("a", 64),
		Scenarios:   []PRVisualScenario{{ID: "warning", Entry: "/onboarding", State: "picker open", Viewport: "1440x900", Expected: []string{"warning visible"}}},
		Items:       []PRVisualEvidenceItem{{ScenarioID: "warning", Path: pngPath, Viewport: "1440x900", CapturedAt: time.Now().UTC().Truncate(time.Second).Format(time.RFC3339), Status: "captured", PrivacyStatus: "human-reviewed"}},
		Publication: PRVisualPublication{State: "pending"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Items[0].Width != 4 || manifest.Items[0].Height != 3 || !strings.Contains(manifest.Items[0].Path, filepath.Join("boatstack", "visual-evidence")) {
		t.Fatalf("unexpected normalized visual evidence: %#v", manifest.Items[0])
	}
	if status := runGit(t, repo, "status", "--short"); status != "" {
		t.Fatalf("visual evidence changed the product tree: %s", status)
	}
	loaded, err := LoadPRVisualEvidence(repo, "feature-warning")
	if err != nil || loaded.Fingerprint != manifest.Fingerprint {
		t.Fatalf("fresh visual evidence did not reload: %#v %v", loaded, err)
	}
	if err := os.WriteFile(loaded.Items[0].Path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPRVisualEvidence(repo, "feature-warning"); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("changed screenshot was not rejected: %v", err)
	}
}

func TestPRVisualEvidenceRequiresPrivacyReview(t *testing.T) {
	repo := visualTestRepo(t)
	pngPath := filepath.Join(t.TempDir(), "warning.png")
	writeTestPNG(t, pngPath)
	_, err := SavePRVisualEvidence(repo, PRVisualEvidenceManifest{
		Key: "feature-warning", Policy: "suggest", Relevance: "relevant", RelevanceSource: "human-provided",
		Status: "PASS", SourceCommit: runGit(t, repo, "rev-parse", "HEAD"), ProductDiffSHA256: strings.Repeat("a", 64),
		Scenarios:   []PRVisualScenario{{ID: "warning", Entry: "/onboarding", State: "picker open", Viewport: "1440x900", Expected: []string{"warning visible"}}},
		Items:       []PRVisualEvidenceItem{{ScenarioID: "warning", Path: pngPath, Viewport: "1440x900", CapturedAt: time.Now().UTC().Truncate(time.Second).Format(time.RFC3339), Status: "captured"}},
		Publication: PRVisualPublication{State: "pending"},
	})
	if err == nil || !strings.Contains(err.Error(), "privacy_status") {
		t.Fatalf("missing privacy review was not rejected: %v", err)
	}
}

func TestPRVisualCapabilityReceiptInvalidatesChangedInputs(t *testing.T) {
	repo := visualTestRepo(t)
	receipt := PRVisualCapabilityReceipt{
		LockfileSHA256: "lock", LaunchCommandHash: "launch", BrowserVersion: "browser-1",
		FrameworkConfigSHA: "config", HealthStatus: "ready",
	}
	if err := SavePRVisualCapability(repo, receipt); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPRVisualCapability(repo, receipt); err != nil {
		t.Fatal(err)
	}
	receipt.BrowserVersion = "browser-2"
	if _, err := LoadPRVisualCapability(repo, receipt); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("changed capability input was not rejected: %v", err)
	}
}

type fakeVisualPublisher struct {
	commentURL string
	err        error
	existing   string
}

func (publisher *fakeVisualPublisher) PublishVisualEvidence(repo, prURL, existingCommentURL string, manifest PRVisualEvidenceManifest) (string, error) {
	publisher.existing = existingCommentURL
	return publisher.commentURL, publisher.err
}

func savedVisualManifest(t *testing.T, repo, key string) PRVisualEvidenceManifest {
	t.Helper()
	pngPath := filepath.Join(t.TempDir(), "warning.png")
	writeTestPNG(t, pngPath)
	manifest, err := SavePRVisualEvidence(repo, PRVisualEvidenceManifest{
		Key: key, Policy: "suggest", Relevance: "relevant", RelevanceSource: "repository-evidenced",
		Status: "PASS", SourceCommit: runGit(t, repo, "rev-parse", "HEAD"), ProductDiffSHA256: strings.Repeat("b", 64),
		Scenarios:   []PRVisualScenario{{ID: "warning", Entry: "/onboarding", State: "picker open", Viewport: "1440x900", Expected: []string{"warning visible"}}},
		Items:       []PRVisualEvidenceItem{{ScenarioID: "warning", Path: pngPath, Viewport: "1440x900", CapturedAt: time.Now().UTC().Truncate(time.Second).Format(time.RFC3339), Status: "captured", PrivacyStatus: "human-reviewed"}},
		Publication: PRVisualPublication{State: "pending"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestPRVisualPublisherReusesOneCommentAndRecordsPendingFailure(t *testing.T) {
	repo := visualTestRepo(t)
	manifest := savedVisualManifest(t, repo, "feature-warning")
	context := PRContext{PRVisualEvidencePolicy: "suggest", PRVisualEvidenceStatus: "PASS", PRVisualEvidence: &manifest}
	publisher := &fakeVisualPublisher{commentURL: "https://github.com/example/repo/pull/1#issuecomment-2"}
	if err := publishPRVisualEvidence(repo, "https://github.com/example/repo/pull/1", context, publisher); err != nil {
		t.Fatal(err)
	}
	published, err := LoadPRVisualEvidence(repo, manifest.Key)
	if err != nil || published.Publication.State != "published" {
		t.Fatalf("publication was not recorded: %#v %v", published.Publication, err)
	}
	updated := savedVisualManifest(t, repo, "feature-warning")
	context.PRVisualEvidence = &updated
	if err := publishPRVisualEvidence(repo, "https://github.com/example/repo/pull/1", context, publisher); err != nil {
		t.Fatal(err)
	}
	if publisher.existing != published.Publication.CommentURL {
		t.Fatalf("existing evidence comment was not reused: %q", publisher.existing)
	}

	pending := savedVisualManifest(t, repo, "feature-failure")
	context.PRVisualEvidence = &pending
	failing := &fakeVisualPublisher{err: os.ErrPermission}
	if err := publishPRVisualEvidence(repo, "https://github.com/example/repo/pull/2", context, failing); err == nil || !strings.Contains(err.Error(), "fix forward") {
		t.Fatalf("publication failure was not routed to fix-forward: %v", err)
	}
	failed, err := LoadPRVisualEvidence(repo, pending.Key)
	if err != nil || failed.Publication.State != "visual_pending" {
		t.Fatalf("visual-pending state was not retained: %#v %v", failed.Publication, err)
	}
}
