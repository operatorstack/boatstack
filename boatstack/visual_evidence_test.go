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

func TestAutomatedVisualPrivacyReviewBindsExactPixelsAndReplaysIdempotently(t *testing.T) {
	repo := visualTestRepo(t)
	pngPath := filepath.Join(t.TempDir(), "warning.png")
	writeTestPNG(t, pngPath)
	manifest, err := SavePRVisualEvidence(repo, PRVisualEvidenceManifest{
		Key: "automated-warning", Policy: "require", Relevance: "relevant", RelevanceSource: "repository-evidenced",
		Status: "PASS", SourceCommit: runGit(t, repo, "rev-parse", "HEAD"), ProductDiffSHA256: strings.Repeat("a", 64),
		Scenarios:   []PRVisualScenario{{ID: "warning", Entry: "/onboarding", State: "picker open", Viewport: "1440x900", Expected: []string{"warning visible"}}},
		Items:       []PRVisualEvidenceItem{{ScenarioID: "warning", Path: pngPath, Viewport: "1440x900", CapturedAt: time.Now().UTC().Truncate(time.Second).Format(time.RFC3339), Status: "captured", PrivacyStatus: "clean"}},
		Publication: PRVisualPublication{State: "pending"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if status, fingerprint, err := ResolvePRVisualPrivacyStatus(repo, &manifest); err != nil || status != "REVIEW_REQUIRED" || fingerprint != "" {
		t.Fatalf("automated capture bypassed human review: %s %q %v", status, fingerprint, err)
	}
	if _, err := RecordPRVisualPrivacyReview(repo, manifest.Key, strings.Repeat("0", 64), "reviewer@example.invalid"); err == nil {
		t.Fatal("mismatched evidence fingerprint was accepted")
	}
	review, err := RecordPRVisualPrivacyReview(repo, manifest.Key, manifest.Fingerprint, "reviewer@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := RecordPRVisualPrivacyReview(repo, manifest.Key, manifest.Fingerprint, "reviewer@example.invalid")
	if err != nil || replayed.Fingerprint != review.Fingerprint || replayed.ReviewedAt != review.ReviewedAt {
		t.Fatalf("exact review replay was not idempotent: %#v %#v %v", review, replayed, err)
	}
	if status, fingerprint, err := ResolvePRVisualPrivacyStatus(repo, &manifest); err != nil || status != "PASS" || fingerprint != review.Fingerprint {
		t.Fatalf("current privacy review was not accepted: %s %q %v", status, fingerprint, err)
	}
	changedPath := filepath.Join(t.TempDir(), "changed.png")
	file, err := os.Create(changedPath)
	if err != nil {
		t.Fatal(err)
	}
	canvas := image.NewRGBA(image.Rect(0, 0, 4, 3))
	canvas.Set(1, 1, color.RGBA{R: 10, G: 20, B: 240, A: 255})
	if err := png.Encode(file, canvas); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	changed, err := SavePRVisualEvidence(repo, PRVisualEvidenceManifest{
		Key: manifest.Key, Policy: manifest.Policy, Relevance: manifest.Relevance, RelevanceSource: manifest.RelevanceSource,
		Status: manifest.Status, SourceCommit: manifest.SourceCommit, ProductDiffSHA256: manifest.ProductDiffSHA256,
		Scenarios:   manifest.Scenarios,
		Items:       []PRVisualEvidenceItem{{ScenarioID: "warning", Path: changedPath, Viewport: "1440x900", CapturedAt: time.Now().UTC().Truncate(time.Second).Format(time.RFC3339), Status: "captured", PrivacyStatus: "clean"}},
		Publication: PRVisualPublication{State: "pending"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if status, fingerprint, err := ResolvePRVisualPrivacyStatus(repo, &changed); err != nil || status != "REVIEW_REQUIRED" || fingerprint != "" {
		t.Fatalf("changed current pixels did not require a fresh review: %s %q %v", status, fingerprint, err)
	}
	if _, err := LoadPRVisualPrivacyReview(repo, manifest.Key); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("changed pixels did not invalidate privacy review: %v", err)
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

// Invariant: the attach retry completes exactly the owed publication — the
// confirmed fingerprinted package against its recorded PR — and an already
// published attachment is a no-op that never re-consults the publisher.
func TestRetryVisualAttachmentCompletesOwedPublication(t *testing.T) {
	repo := visualTestRepo(t)
	manifest := savedVisualManifest(t, repo, "feature-warning")
	context := PRContext{PRVisualEvidencePolicy: "suggest", PRVisualEvidenceStatus: "PASS", PRVisualEvidence: &manifest}
	prURL := "https://github.com/example/repo/pull/3"
	if err := publishPRVisualEvidence(repo, prURL, context, &fakeVisualPublisher{err: os.ErrPermission}); err == nil {
		t.Fatal("fixture publication was expected to fail into visual_pending")
	}
	retried, err := RetryVisualAttachment(repo, "feature-warning", &fakeVisualPublisher{commentURL: "https://github.com/example/repo/pull/3#issuecomment-9"})
	if err != nil {
		t.Fatal(err)
	}
	if retried.Publication.State != "published" || retried.Publication.PRURL != prURL || retried.Publication.CommentURL == "" {
		t.Fatalf("retry did not complete the owed publication: %#v", retried.Publication)
	}
	if retried.Fingerprint != manifest.Fingerprint {
		t.Fatalf("retry changed the evidence fingerprint: before=%s after=%s", manifest.Fingerprint, retried.Fingerprint)
	}
	again, err := RetryVisualAttachment(repo, "feature-warning", &fakeVisualPublisher{err: os.ErrPermission})
	if err != nil || again.Publication.State != "published" {
		t.Fatalf("published attachment must be an idempotent no-op: %#v %v", again.Publication, err)
	}
}

// Refusals: the retry never usurps first publication (publish-pr owns it) and
// a missing publisher routes to the manual recording verb by name.
func TestRetryVisualAttachmentRefusesWhatItDoesNotOwn(t *testing.T) {
	repo := visualTestRepo(t)
	if _, err := RetryVisualAttachment(repo, "missing-feature", &fakeVisualPublisher{commentURL: "x"}); err == nil || !strings.Contains(err.Error(), "no recorded visual evidence") {
		t.Fatalf("missing manifest was not refused: %v", err)
	}
	manifest := savedVisualManifest(t, repo, "feature-warning")
	if _, err := RetryVisualAttachment(repo, "feature-warning", &fakeVisualPublisher{commentURL: "x"}); err == nil || !strings.Contains(err.Error(), "publish-pr") {
		t.Fatalf("pre-publication manifest was not routed to publish-pr: %v", err)
	}
	context := PRContext{PRVisualEvidencePolicy: "suggest", PRVisualEvidenceStatus: "PASS", PRVisualEvidence: &manifest}
	if err := publishPRVisualEvidence(repo, "https://github.com/example/repo/pull/4", context, nil); err == nil || !strings.Contains(err.Error(), "external visual evidence is pending") {
		t.Fatalf("missing external publisher did not preserve a pending PR: %v", err)
	}
	if _, err := RetryVisualAttachment(repo, "feature-warning", nil); err == nil || !strings.Contains(err.Error(), "external visual publisher") {
		t.Fatalf("missing publisher must preserve the hosted retry: %v", err)
	}
	recovered, err := RetryVisualAttachment(repo, "feature-warning", &fakeVisualPublisher{commentURL: "https://github.com/example/repo/pull/4#issuecomment-1"})
	if err != nil || recovered.Publication.State != "published" {
		t.Fatalf("legacy owed state with a live publisher should still recover: %#v %v", recovered.Publication, err)
	}
}
