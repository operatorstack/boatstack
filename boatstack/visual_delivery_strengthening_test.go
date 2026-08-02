package boatstack

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// control-law: registered-visual-surfaces-share-one-impact-decision
func TestVisualSurfaceChangesAreScreenshotCandidates(t *testing.T) {
	got := visualSurfaceChangedPaths([]string{"apps/web/page.tsx", "api/server.go"}, []VisualSurface{{ID: "web", Paths: []string{"apps/web/**"}}})
	if len(got) != 1 || got[0] != "apps/web/page.tsx" {
		t.Fatalf("unexpected candidates: %#v", got)
	}
}

// control-law: scenario-verification-requires-a-receipt
func TestPNGOnlyAndReceiptHarnessStatusesStaySeparate(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.json")
	if _, err := loadVisualScenarioReceipt(missing, "checkout"); err == nil {
		t.Fatal("missing receipt must not verify a scenario")
	}
	receiptPath := filepath.Join(dir, "receipt.json")
	receipt := []byte(`{"scenario_id":"checkout","reached_state_or_url":"/checkout","checks":[{"name":"total visible","result":"PASS"}],"overall_result":"PASS"}`)
	if err := atomicWrite(receiptPath, receipt); err != nil {
		t.Fatal(err)
	}
	got, err := loadVisualScenarioReceipt(receiptPath, "checkout")
	if err != nil || got.OverallResult != "PASS" {
		t.Fatalf("receipt was not verified: %#v %v", got, err)
	}
}

// control-law: external-upload-requires-human-privacy-review
func TestExternalUploadRefusesWithoutHumanPrivacyReview(t *testing.T) {
	repo := visualTestRepo(t)
	runGit(t, repo, "remote", "add", "origin", "https://github.com/o/n.git")
	manifest := PRVisualEvidenceManifest{Key: "x", Items: []PRVisualEvidenceItem{{ScenarioID: "s", PrivacyStatus: "clean"}}}
	_, err := (ExternalHostVisualEvidencePublisher{}).PublishVisualEvidence(repo, "https://github.com/o/n/pull/1", "", manifest)
	if err == nil || !strings.Contains(err.Error(), "HUMAN_REVIEWED") {
		t.Fatalf("expected privacy refusal, got %v", err)
	}
}

// control-law: only-verified-hosted-links-enter-the-comment
func TestHostedURLVerificationRejectsInvalidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	defer server.Close()
	if err := verifyHostedVisualURL(externalHostSpec{endpoint: server.URL, label: "example.invalid"}, server.URL+"/missing.png"); err == nil {
		t.Fatal("invalid hosted URL was accepted")
	}
}

func TestHostedURLVerificationRejectsUnexpectedDomain(t *testing.T) {
	err := verifyHostedVisualURL(externalHostSpec{endpoint: "https://litterbox.catbox.moe/upload", label: "litter.catbox.moe"}, "https://example.com/image.png")
	if err == nil || !strings.Contains(err.Error(), "unexpected domain") { t.Fatalf("unexpected host URL was accepted: %v", err) }
}

func TestExternalCommentIncludesJourneyContext(t *testing.T) {
	manifest := PRVisualEvidenceManifest{Key: "x", Scenarios: []PRVisualScenario{{ID: "s", Entry: "/start", State: "ready", Viewport: "800x600", Surface: "web", UserContext: "new customer", UserGoal: "finish checkout", JourneyStep: "confirm order", ReviewerContext: "verify price clarity", Expected: []string{"total visible"}}}, Items: []PRVisualEvidenceItem{{ScenarioID: "s"}}}
	body := composeExternalHostComment(externalHostSpec{label: "host"}, "72h", map[string]string{"s": "https://host/s.png"}, manifest)
	for _, want := range []string{"new customer", "finish checkout", "confirm order", "verify price clarity", "/start", "ready", "web"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in comment:\n%s", want, body)
		}
	}
}

func TestExternalCommentCannotInjectUnverifiedImageLinks(t *testing.T) {
	manifest := PRVisualEvidenceManifest{Key: "x", Scenarios: []PRVisualScenario{{ID: "s", Viewport: "800x600", Expected: []string{"![leak](file:///private.png)"}}}, Items: []PRVisualEvidenceItem{{ScenarioID: "s"}}}
	body := composeExternalHostComment(externalHostSpec{label: "host"}, "72h", map[string]string{"s": "https://host/s.png"}, manifest)
	if strings.Count(body, "![") != 1 || !strings.Contains(body, "![s](https://host/s.png)") {
		t.Fatalf("comment admitted an unverified image link:\n%s", body)
	}
}

func TestScenarioAndCommandChangesInvalidateEvidenceIdentity(t *testing.T) {
	config := ProjectConfig{Project: Project{Commands: map[string]string{"visual": "capture-v1"}}}
	scenarios := []PRVisualScenario{{ID: "s", Entry: "/", State: "ready", Viewport: "800x600", Expected: []string{"visible"}}}
	s1, c1, err := currentVisualEvidenceIdentity(scenarios, config)
	if err != nil {
		t.Fatal(err)
	}
	scenarios[0].UserGoal = "complete purchase"
	s2, _, err := currentVisualEvidenceIdentity(scenarios, config)
	if err != nil || s1 == s2 {
		t.Fatal("scenario change did not stale identity")
	}
	config.Project.Commands["visual"] = "capture-v2"
	_, c2, err := currentVisualEvidenceIdentity(scenarios, config)
	if err != nil || c1 == c2 {
		t.Fatal("command change did not stale identity")
	}
}

func TestPublicationStateDoesNotChangeEvidenceFingerprint(t *testing.T) {
	manifest := PRVisualEvidenceManifest{SchemaVersion: visualEvidenceSchemaVersion, Key: "x", Policy: "require", Relevance: "not_relevant", RelevanceSource: "managed-plan", Reason: "nonvisual", Status: "NOT_APPLICABLE"}
	first, err := visualManifestFingerprint(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Publication = PRVisualPublication{State: "visual_pending", PRURL: "https://github.com/o/n/pull/1", CommentURL: "https://github.com/o/n/pull/1#issuecomment-2"}
	second, err := visualManifestFingerprint(manifest)
	if err != nil || first != second {
		t.Fatal("publication retry changed evidence identity")
	}
}

// control-law: partial-host-failure-preserves-pr-and-evidence-identity
func TestPartialExternalUploadLeavesVisualPendingOnSameFingerprint(t *testing.T) {
	repo := visualTestRepo(t)
	runGit(t, repo, "remote", "add", "origin", "https://github.com/o/n.git")
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "one.png"), filepath.Join(dir, "two.png")}
	for _, path := range paths {
		writeTestPNG(t, path)
	}
	now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	manifest, err := SavePRVisualEvidence(repo, PRVisualEvidenceManifest{
		Key: "partial", Policy: "require", Relevance: "relevant", RelevanceSource: "managed-plan", Status: "PASS",
		SourceCommit: "head", ProductDiffSHA256: strings.Repeat("a", 64), ScenarioDefinitionSHA256: strings.Repeat("b", 64), CaptureCommandSHA256: strings.Repeat("c", 64),
		Scenarios: []PRVisualScenario{{ID: "one", Entry: "/", State: "one", Viewport: "800x600", Expected: []string{"one"}}, {ID: "two", Entry: "/", State: "two", Viewport: "800x600", Expected: []string{"two"}}},
		Items:     []PRVisualEvidenceItem{{ScenarioID: "one", Path: paths[0], Viewport: "800x600", CapturedAt: now, Status: "CAPTURED", PrivacyStatus: "human-reviewed", VerificationStatus: "CAPTURED"}, {ScenarioID: "two", Path: paths[1], Viewport: "800x600", CapturedAt: now, Status: "CAPTURED", PrivacyStatus: "human-reviewed", VerificationStatus: "CAPTURED"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	uploads := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("png"))
			return
		}
		uploads++
		if uploads == 2 {
			http.Error(w, "partial failure", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(server.URL + "/one.png"))
	}))
	defer server.Close()
	original := visualExternalHosts["litterbox"]
	visualExternalHosts["litterbox"] = externalHostSpec{endpoint: server.URL, label: "local", withExpiry: true}
	defer func() { visualExternalHosts["litterbox"] = original }()
	prURL := "https://github.com/o/n/pull/7"
	err = attachVisualEvidence(repo, prURL, manifest, ExternalHostVisualEvidencePublisher{}, "require")
	if err == nil {
		t.Fatal("partial upload unexpectedly published")
	}
	pending, loadErr := LoadPRVisualEvidence(repo, manifest.Key)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if pending.Publication.State != "visual_pending" || pending.Publication.PRURL != prURL || pending.Fingerprint != manifest.Fingerprint {
		t.Fatalf("partial failure lost PR or evidence identity: %#v", pending)
	}
}
