package boatstack

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOriginRepoSlugParsesSSHAndHTTPS(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	cases := map[string]struct{ owner, name string }{
		"git@github.com:millennialcpa/taxweave.git":      {"millennialcpa", "taxweave"},
		"https://github.com/operatorstack/boatstack.git": {"operatorstack", "boatstack"},
		"https://github.com/operatorstack/boatstack":     {"operatorstack", "boatstack"},
	}
	runGit(t, repo, "remote", "add", "origin", "https://github.com/placeholder/placeholder.git")
	for remote, want := range cases {
		runGit(t, repo, "remote", "set-url", "origin", remote)
		owner, name, err := originRepoSlug(repo)
		if err != nil {
			t.Fatalf("%s: %v", remote, err)
		}
		if owner != want.owner || name != want.name {
			t.Fatalf("%s: got %s/%s, want %s/%s", remote, owner, name, want.owner, want.name)
		}
	}
}

func TestOriginRepoSlugRejectsNonGitHub(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "remote", "add", "origin", "https://gitlab.com/acme/widget.git")
	if _, _, err := originRepoSlug(repo); err == nil {
		t.Fatal("non-GitHub origin should be rejected")
	}
}

func TestPRNumberFromURL(t *testing.T) {
	got, err := prNumberFromURL("https://github.com/operatorstack/boatstack/pull/143")
	if err != nil || got != "143" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := prNumberFromURL("https://github.com/operatorstack/boatstack"); err == nil {
		t.Fatal("a URL without a pull number should error")
	}
}

func TestCommentIDFromURL(t *testing.T) {
	if got := commentIDFromURL("https://github.com/o/n/pull/143#issuecomment-987654"); got != "987654" {
		t.Fatalf("got %q", got)
	}
	if got := commentIDFromURL(""); got != "" {
		t.Fatalf("empty URL should yield no id, got %q", got)
	}
}

func TestEvidenceBranchNameRejectsUnsafeKey(t *testing.T) {
	if _, err := evidenceBranchName("../escape"); err == nil {
		t.Fatal("path-traversal key must be rejected")
	}
	branch, err := evidenceBranchName("firm-status")
	if err != nil || branch != "boatstack-visual-evidence/firm-status" {
		t.Fatalf("got %q, %v", branch, err)
	}
}

func TestRawContentURLPinsCommit(t *testing.T) {
	url := rawContentURL("o", "n", "abc123", evidenceBlobPath("firm-status", "deadbeef"))
	want := "https://raw.githubusercontent.com/o/n/abc123/firm-status/deadbeef.png"
	if url != want {
		t.Fatalf("got %q, want %q", url, want)
	}
}

func TestComposeVisualEvidenceCommentRendersScenariosAndWarnings(t *testing.T) {
	manifest := PRVisualEvidenceManifest{
		Key:               "firm-status",
		SourceCommit:      "src123",
		ProductDiffSHA256: "diff456",
		Fingerprint:       "fp789",
		Scenarios: []PRVisualScenario{
			{ID: "VS-1", Entry: "/clients", State: "hover", Viewport: "1440x900", Expected: []string{"portal card is blue"}},
			{ID: "VS-2", Entry: "/clients", State: "default", Viewport: "1440x900", Expected: []string{"amber badge"}},
		},
		Items: []PRVisualEvidenceItem{
			{ScenarioID: "VS-1", SHA256: "hash1"},
			{ScenarioID: "VS-2", SHA256: "hash2"},
		},
	}
	body := composeVisualEvidenceComment("o", "n", "commitSHA", manifest)

	if !strings.HasPrefix(body, visualEvidenceCommentMarker("firm-status")) {
		t.Fatal("comment must open with the idempotency marker")
	}
	for _, want := range []string{
		"publicly accessible",        // standing privacy warning
		"human-review evidence",      // not-mechanical-proof warning
		"src123", "diff456", "fp789", // trust fingerprints
		"VS-1", "VS-2", // both scenarios
		"portal card is blue", "amber badge",
		rawContentURL("o", "n", "commitSHA", "firm-status/hash1.png"),
		rawContentURL("o", "n", "commitSHA", "firm-status/hash2.png"),
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("comment body missing %q\n---\n%s", want, body)
		}
	}
}

func TestComposeVisualEvidenceCommentSkipsUncapturedScenarios(t *testing.T) {
	manifest := PRVisualEvidenceManifest{
		Key: "firm-status",
		Scenarios: []PRVisualScenario{
			{ID: "VS-1", Viewport: "1440x900", Expected: []string{"x"}},
		},
		// No items → nothing captured.
	}
	body := composeVisualEvidenceComment("o", "n", "c", manifest)
	if strings.Contains(body, "![VS-1]") {
		t.Fatal("uncaptured scenario must not render an image")
	}
	if !strings.Contains(body, "No captured scenarios") {
		t.Fatal("expected an explicit empty-state note")
	}
}

func TestUploadToExternalHostPostsFormAndReturnsURL(t *testing.T) {
	dir := t.TempDir()
	png := filepath.Join(dir, "VS-1.png")
	writeTestPNG(t, png)

	var gotReqtype, gotTime, gotFilename string
	var gotBytes int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReqtype = r.FormValue("reqtype")
		gotTime = r.FormValue("time")
		file, header, err := r.FormFile("fileToUpload")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()
		gotFilename = header.Filename
		contents := make([]byte, 4096)
		n, _ := file.Read(contents)
		gotBytes = n
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("https://litter.catbox.moe/abc123.png\n"))
	}))
	defer server.Close()

	spec := externalHostSpec{endpoint: server.URL, label: "litter.catbox.moe", withExpiry: true}
	url, err := uploadToExternalHost(spec, "24h", png)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if url != "https://litter.catbox.moe/abc123.png" {
		t.Fatalf("unexpected URL (trailing newline not trimmed?): %q", url)
	}
	if gotReqtype != "fileupload" {
		t.Fatalf("reqtype = %q, want fileupload", gotReqtype)
	}
	if gotTime != "24h" {
		t.Fatalf("expiry field = %q, want 24h", gotTime)
	}
	if gotFilename != "VS-1.png" {
		t.Fatalf("filename = %q, want VS-1.png", gotFilename)
	}
	if gotBytes == 0 {
		t.Fatal("no PNG bytes reached the host")
	}
}

func TestUploadToExternalHostOmitsTimeForPermanentHost(t *testing.T) {
	dir := t.TempDir()
	png := filepath.Join(dir, "VS-1.png")
	writeTestPNG(t, png)

	sawTimeField := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("time") != "" {
			sawTimeField = true
		}
		_, _ = w.Write([]byte("https://files.catbox.moe/xyz.png"))
	}))
	defer server.Close()

	spec := externalHostSpec{endpoint: server.URL, label: "files.catbox.moe", withExpiry: false}
	if _, err := uploadToExternalHost(spec, "72h", png); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if sawTimeField {
		t.Fatal("a permanent host must not receive an expiry time field")
	}
}

func TestUploadToExternalHostRejectsNonURLResponse(t *testing.T) {
	dir := t.TempDir()
	png := filepath.Join(dir, "VS-1.png")
	writeTestPNG(t, png)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("something broke"))
	}))
	defer server.Close()

	spec := externalHostSpec{endpoint: server.URL, label: "litter.catbox.moe", withExpiry: true}
	if _, err := uploadToExternalHost(spec, "24h", png); err == nil {
		t.Fatal("a non-200 / non-URL response must be a failure, not a broken image")
	}
}

func TestComposeExternalHostCommentRendersInlineWithExpiryReminder(t *testing.T) {
	manifest := PRVisualEvidenceManifest{
		Key:               "firm-status",
		SourceCommit:      "src123",
		ProductDiffSHA256: "diff456",
		Fingerprint:       "fp789",
		Scenarios: []PRVisualScenario{
			{ID: "VS-1", Viewport: "1440x900", Expected: []string{"portal card is blue"}},
			{ID: "VS-2", Viewport: "1440x900", Expected: []string{"amber badge"}},
		},
		Items: []PRVisualEvidenceItem{{ScenarioID: "VS-1"}, {ScenarioID: "VS-2"}},
	}
	urls := map[string]string{
		"VS-1": "https://litter.catbox.moe/a.png",
		"VS-2": "https://litter.catbox.moe/b.png",
	}
	spec := externalHostSpec{label: "litter.catbox.moe", withExpiry: true}
	body := composeExternalHostComment(spec, "24h", urls, manifest)

	if !strings.HasPrefix(body, visualEvidenceCommentMarker("firm-status")) {
		t.Fatal("comment must open with the idempotency marker")
	}
	for _, want := range []string{
		"src123", "diff456", "fp789", // trust fingerprints
		"![VS-1](https://litter.catbox.moe/a.png)", // inline, not a click-through link
		"![VS-2](https://litter.catbox.moe/b.png)",
		"litter.catbox.moe",     // host named
		"24h",                   // expiry window named
		"third-party",           // standing privacy reminder
		"human-review evidence", // not-mechanical-proof caveat
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("comment body missing %q\n---\n%s", want, body)
		}
	}
}

func TestComposeExternalHostCommentPermanentHostOmitsExpiry(t *testing.T) {
	manifest := PRVisualEvidenceManifest{
		Key:       "firm-status",
		Scenarios: []PRVisualScenario{{ID: "VS-1", Viewport: "1440x900", Expected: []string{"x"}}},
		Items:     []PRVisualEvidenceItem{{ScenarioID: "VS-1"}},
	}
	urls := map[string]string{"VS-1": "https://files.catbox.moe/a.png"}
	spec := externalHostSpec{label: "files.catbox.moe", withExpiry: false}
	body := composeExternalHostComment(spec, "72h", urls, manifest)
	if strings.Contains(body, "auto-expire") {
		t.Fatal("a permanent host must not claim an expiry window")
	}
	if !strings.Contains(body, "permanent") || !strings.Contains(body, "third-party") {
		t.Fatalf("permanent host still needs its standing reminder\n---\n%s", body)
	}
}

func TestExternalHostPublishVisualEvidenceUploadsAndUpserts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake gh relies on a POSIX shell script on PATH")
	}
	repo := visualTestRepo(t)
	runGit(t, repo, "remote", "add", "origin", "https://github.com/o/n.git")

	png := filepath.Join(repo, "VS-1.png")
	writeTestPNG(t, png)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := r.FormFile("fileToUpload"); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte("https://litter.catbox.moe/hosted.png"))
	}))
	defer server.Close()

	original := visualExternalHosts["litterbox"]
	visualExternalHosts["litterbox"] = externalHostSpec{endpoint: server.URL, label: "litter.catbox.moe", withExpiry: true}
	defer func() { visualExternalHosts["litterbox"] = original }()

	fakeDir := t.TempDir()
	script := filepath.Join(fakeDir, "gh")
	scriptBody := `#!/bin/sh
if [ "$1" = "api" ]; then
  for a in "$@"; do
    if [ "$a" = "POST" ]; then echo "https://github.com/o/n/pull/9#issuecomment-555"; exit 0; fi
    if [ "$a" = "PATCH" ]; then echo "https://github.com/o/n/pull/9#issuecomment-555"; exit 0; fi
  done
  exit 0
fi
exit 1
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	manifest := PRVisualEvidenceManifest{
		Key:               "firm-status",
		SourceCommit:      "src",
		ProductDiffSHA256: "diff",
		Fingerprint:       "fp",
		Scenarios:         []PRVisualScenario{{ID: "VS-1", Viewport: "1440x900", Expected: []string{"blue"}}},
		Items:             []PRVisualEvidenceItem{{ScenarioID: "VS-1", Path: png}},
	}
	publisher := ExternalHostVisualEvidencePublisher{Host: "litterbox", Expiry: "24h"}
	commentURL, err := publisher.PublishVisualEvidence(repo, "https://github.com/o/n/pull/9", "", manifest)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if commentURL != "https://github.com/o/n/pull/9#issuecomment-555" {
		t.Fatalf("unexpected comment URL: %s", commentURL)
	}
}
