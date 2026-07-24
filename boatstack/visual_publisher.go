package boatstack

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// GitVisualEvidencePublisher publishes fingerprinted PNG bytes to a pull request
// without a signed-in host browser or a manual drag-drop. GitHub exposes no public
// API that mints user-attachments CDN URLs, so instead of uploading through the web
// UI this publisher commits the exact bytes to a dedicated, Boatstack-owned evidence
// branch on origin and references them from one Boatstack-owned PR comment via
// immutable raw.githubusercontent.com URLs pinned to the commit SHA.
//
// The approach only renders for public repositories: raw.githubusercontent.com does
// not serve private content to anonymous markdown renderers. SelectVisualPublisher
// therefore declines to return this publisher for a non-public origin, leaving the
// existing manual-attachment fallback in place rather than emitting broken images.
type GitVisualEvidencePublisher struct{}

var (
	prNumberPattern  = regexp.MustCompile(`/pull/(\d+)`)
	commentIDPattern = regexp.MustCompile(`issuecomment-(\d+)`)
	originSlugSSH    = regexp.MustCompile(`^git@github\.com:([^/]+)/(.+?)(?:\.git)?$`)
	originSlugHTTP   = regexp.MustCompile(`^https?://github\.com/([^/]+)/(.+?)(?:\.git)?$`)
)

// SelectVisualPublisher returns a programmatic publisher when the repository can
// actually render committed evidence — a GitHub origin with gh available and
// authenticated. The default publisher renders inline only for a PUBLIC repository
// (raw.githubusercontent.com does not serve private content to anonymous markdown
// renderers). A repository may opt into the external-host publisher via
// workflow.visual_evidence_publish.mode="external-host" to render inline even when
// private. Otherwise it returns nil so the caller's manual-attachment fallback stays
// in force (which never blocks a suggest-policy PR).
func SelectVisualPublisher(repo string) PRVisualEvidencePublisher {
	resolved, err := ResolveRepository(repo)
	if err != nil {
		return nil
	}
	if err := ghAvailable(resolved); err != nil {
		return nil
	}
	if _, _, err := originRepoSlug(resolved); err != nil {
		return nil
	}
	// External-host mode is opt-in only: it publishes screenshot bytes to a
	// third-party anonymous host, so it is never auto-selected — only this explicit
	// config value turns it on, and it works for a private origin too.
	if publish := visualPublishConfig(resolved); publish != nil && publish.Mode == "external-host" {
		return ExternalHostVisualEvidencePublisher{Host: publish.Host, Expiry: publish.Expiry}
	}
	visibility, err := commandOutput(resolved, "gh", "repo", "view", "--json", "visibility", "--jq", ".visibility")
	if err != nil || !strings.EqualFold(strings.TrimSpace(visibility), "public") {
		return nil
	}
	return GitVisualEvidencePublisher{}
}

// visualPublishConfig reads the repository's visual-evidence publish preferences from
// the generated project config, returning nil when the config is absent, unreadable,
// or leaves the block unset so the caller falls back to the default public-branch
// behavior.
func visualPublishConfig(repo string) *VisualEvidencePublish {
	config, _, err := LoadConfig(filepath.Join(repo, ".product-loop", "project.json"))
	if err != nil {
		return nil
	}
	return config.Workflow.VisualEvidencePublish
}

// PublishVisualEvidence commits the manifest's exact PNG bytes to the evidence
// branch, then posts or updates the single Boatstack-owned comment on the PR.
func (GitVisualEvidencePublisher) PublishVisualEvidence(repo, prURL, existingCommentURL string, manifest PRVisualEvidenceManifest) (string, error) {
	resolved, err := ResolveRepository(repo)
	if err != nil {
		return "", err
	}
	owner, name, err := originRepoSlug(resolved)
	if err != nil {
		return "", err
	}
	prNumber, err := prNumberFromURL(prURL)
	if err != nil {
		return "", err
	}
	if len(manifest.Items) == 0 {
		return "", fmt.Errorf("visual evidence has no screenshots to publish")
	}
	commitSHA, err := pushEvidenceCommit(resolved, manifest)
	if err != nil {
		return "", err
	}
	body := composeVisualEvidenceComment(owner, name, commitSHA, manifest)
	return upsertEvidenceComment(resolved, owner, name, prNumber, existingCommentURL, manifest.Key, body)
}

// pushEvidenceCommit builds a commit carrying the exact PNG bytes with Git plumbing
// against a temporary index — never touching the working tree — and pushes it to the
// Boatstack-owned evidence branch, accumulating onto the branch's prior tip when one
// exists. It returns the commit SHA, which pins immutable raw content URLs.
func pushEvidenceCommit(repo string, manifest PRVisualEvidenceManifest) (string, error) {
	branch, err := evidenceBranchName(manifest.Key)
	if err != nil {
		return "", err
	}
	indexFile, err := os.CreateTemp("", "boatstack-evidence-index-*")
	if err != nil {
		return "", err
	}
	indexPath := indexFile.Name()
	indexFile.Close()
	os.Remove(indexPath) // git wants to create the index itself
	defer os.Remove(indexPath)
	git := func(arguments ...string) (string, error) {
		return gitIndexedCommand(repo, indexPath, arguments...)
	}

	var parent string
	if tip := strings.TrimSpace(gitOutput(repo, "ls-remote", "origin", "refs/heads/"+branch)); tip != "" {
		parent = strings.Fields(tip)[0]
		if _, err := commandOutput(repo, "git", "-C", repo, "fetch", "origin", branch); err != nil {
			return "", fmt.Errorf("cannot fetch the visual-evidence branch to extend it: %w", err)
		}
		if _, err := git("read-tree", parent); err != nil {
			return "", err
		}
	}
	for _, item := range manifest.Items {
		blob, err := git("hash-object", "-w", "--", item.Path)
		if err != nil {
			return "", err
		}
		path := evidenceBlobPath(manifest.Key, item.SHA256)
		if _, err := git("update-index", "--add", "--cacheinfo", "100644,"+strings.TrimSpace(blob)+","+path); err != nil {
			return "", err
		}
	}
	tree, err := git("write-tree")
	if err != nil {
		return "", err
	}
	tree = strings.TrimSpace(tree)
	message := "boatstack: visual evidence for " + manifest.Key + " (" + manifest.Fingerprint + ")"
	commitArgs := []string{"commit-tree", tree, "-m", message}
	if parent != "" {
		commitArgs = append(commitArgs, "-p", parent)
	}
	commit, err := git(commitArgs...)
	if err != nil {
		return "", err
	}
	commit = strings.TrimSpace(commit)
	if _, err := commandOutput(repo, "git", "-C", repo, "push", "origin", commit+":refs/heads/"+branch); err != nil {
		return "", fmt.Errorf("cannot push the visual-evidence commit without rewriting history: %w", err)
	}
	return commit, nil
}

// upsertEvidenceComment posts the composed body to exactly one Boatstack-owned
// comment: it reuses the recorded comment when known, otherwise finds the prior
// comment by its hidden marker so a lost URL never orphans a duplicate.
func upsertEvidenceComment(repo, owner, name, prNumber, existingCommentURL, key, body string) (string, error) {
	commentID := commentIDFromURL(existingCommentURL)
	if commentID == "" {
		marker := visualEvidenceCommentMarker(key)
		found, err := commandOutput(repo, "gh", "api", "--paginate",
			fmt.Sprintf("repos/%s/%s/issues/%s/comments", owner, name, prNumber),
			"--jq", `.[] | select(.body | contains("`+marker+`")) | .id`)
		if err != nil {
			return "", err
		}
		if lines := strings.Fields(found); len(lines) > 0 {
			commentID = lines[0]
		}
	}
	bodyFile, err := os.CreateTemp("", "boatstack-evidence-comment-*.md")
	if err != nil {
		return "", err
	}
	bodyPath := bodyFile.Name()
	defer os.Remove(bodyPath)
	if _, err := bodyFile.WriteString(body); err != nil {
		bodyFile.Close()
		return "", err
	}
	if err := bodyFile.Close(); err != nil {
		return "", err
	}
	if commentID != "" {
		return commandOutput(repo, "gh", "api", "--method", "PATCH",
			fmt.Sprintf("repos/%s/%s/issues/comments/%s", owner, name, commentID),
			"-F", "body=@"+bodyPath, "--jq", ".html_url")
	}
	return commandOutput(repo, "gh", "api", "--method", "POST",
		fmt.Sprintf("repos/%s/%s/issues/%s/comments", owner, name, prNumber),
		"-F", "body=@"+bodyPath, "--jq", ".html_url")
}

// gitIndexedCommand runs git against a scoped, temporary index so evidence commits
// never disturb the repository's real index or working tree.
func gitIndexedCommand(repo, indexPath string, arguments ...string) (string, error) {
	return commandOutputEnv(repo, []string{"GIT_INDEX_FILE=" + indexPath}, "git", append([]string{"-C", repo}, arguments...)...)
}

func originRepoSlug(repo string) (string, string, error) {
	remote, err := commandOutput(repo, "git", "-C", repo, "remote", "get-url", "origin")
	if err != nil {
		return "", "", err
	}
	remote = strings.TrimSpace(remote)
	for _, pattern := range []*regexp.Regexp{originSlugSSH, originSlugHTTP} {
		if match := pattern.FindStringSubmatch(remote); match != nil {
			return match[1], strings.TrimSuffix(match[2], "/"), nil
		}
	}
	return "", "", fmt.Errorf("origin %q is not a recognizable GitHub repository", remote)
}

func prNumberFromURL(prURL string) (string, error) {
	if match := prNumberPattern.FindStringSubmatch(prURL); match != nil {
		return match[1], nil
	}
	return "", fmt.Errorf("cannot determine the pull-request number from %q", prURL)
}

func commentIDFromURL(commentURL string) string {
	if match := commentIDPattern.FindStringSubmatch(commentURL); match != nil {
		return match[1]
	}
	return ""
}

func evidenceBranchName(key string) (string, error) {
	safe, err := safeCacheSegment(key, "visual evidence key")
	if err != nil {
		return "", err
	}
	return "boatstack-visual-evidence/" + safe, nil
}

func evidenceBlobPath(key, sha string) string {
	return key + "/" + sha + ".png"
}

func rawContentURL(owner, name, commitSHA, path string) string {
	return "https://raw.githubusercontent.com/" + owner + "/" + name + "/" + commitSHA + "/" + path
}

func visualEvidenceCommentMarker(key string) string {
	return "<!-- boatstack-visual-evidence:" + key + " -->"
}

// composeVisualEvidenceComment renders the single Boatstack-owned comment: a hidden
// marker for idempotent reuse, the trust fingerprints, the standing public-repository
// privacy warning, and one image per scenario pinned to the evidence commit.
func composeVisualEvidenceComment(owner, name, commitSHA string, manifest PRVisualEvidenceManifest) string {
	itemsByScenario := make(map[string]PRVisualEvidenceItem, len(manifest.Items))
	for _, item := range manifest.Items {
		itemsByScenario[item.ScenarioID] = item
	}
	var builder strings.Builder
	builder.WriteString(visualEvidenceCommentMarker(manifest.Key) + "\n")
	builder.WriteString("### Visual evidence\n\n")
	builder.WriteString("Screenshots are human-review evidence, not mechanical proof. These images are committed to a public branch and are publicly accessible.\n\n")
	builder.WriteString(fmt.Sprintf("Source commit `%s` · product diff `%s` · fingerprint `%s`\n\n", manifest.SourceCommit, manifest.ProductDiffSHA256, manifest.Fingerprint))
	rendered := 0
	for _, scenario := range manifest.Scenarios {
		item, ok := itemsByScenario[scenario.ID]
		if !ok {
			continue
		}
		caption := strings.Join(scenario.Expected, "; ")
		builder.WriteString(fmt.Sprintf("**%s** — %s (`%s`)\n\n", scenario.ID, caption, scenario.Viewport))
		url := rawContentURL(owner, name, commitSHA, evidenceBlobPath(manifest.Key, item.SHA256))
		builder.WriteString(fmt.Sprintf("![%s](%s)\n\n", scenario.ID, url))
		rendered++
	}
	if rendered == 0 {
		builder.WriteString("_No captured scenarios to display._\n")
	}
	return builder.String()
}

// externalHostSpec describes an anonymous image host used by external-host mode.
type externalHostSpec struct {
	endpoint   string
	label      string
	withExpiry bool // the host auto-deletes uploads after a caller-chosen window
}

// visualExternalHosts enumerates the supported anonymous hosts. Both accept the same
// multipart form (reqtype=fileupload, fileToUpload=<png>) and return the hosted URL
// as plain text — a URL GitHub's camo proxy can fetch unauthenticated so the comment
// renders inline. It is a var, not a const table, so tests can point an endpoint at a
// local httptest server.
var visualExternalHosts = map[string]externalHostSpec{
	"litterbox": {endpoint: "https://litterbox.catbox.moe/resources/internals/api.php", label: "litter.catbox.moe", withExpiry: true},
	"catbox":    {endpoint: "https://catbox.moe/user/api.php", label: "files.catbox.moe", withExpiry: false},
}

const (
	defaultExternalHost   = "litterbox"
	defaultExternalExpiry = "72h"
)

// ExternalHostVisualEvidencePublisher renders visual evidence inline on ANY repo —
// including a private one — by uploading the exact PNG bytes to an anonymous expiring
// host whose returned URL GitHub's camo proxy fetches unauthenticated. It is opt-in
// only (workflow.visual_evidence_publish.mode="external-host") because it publishes
// screenshot bytes to a third party; the comment carries a standing reminder naming
// the host and its expiry so reviewers know the images are external and temporary.
type ExternalHostVisualEvidencePublisher struct {
	Host   string
	Expiry string
}

// PublishVisualEvidence uploads the manifest's exact PNG bytes to the configured
// anonymous host, then posts or updates the single Boatstack-owned comment on the PR
// with inline images and the standing hosting reminder.
func (p ExternalHostVisualEvidencePublisher) PublishVisualEvidence(repo, prURL, existingCommentURL string, manifest PRVisualEvidenceManifest) (string, error) {
	resolved, err := ResolveRepository(repo)
	if err != nil {
		return "", err
	}
	owner, name, err := originRepoSlug(resolved)
	if err != nil {
		return "", err
	}
	prNumber, err := prNumberFromURL(prURL)
	if err != nil {
		return "", err
	}
	if len(manifest.Items) == 0 {
		return "", fmt.Errorf("visual evidence has no screenshots to publish")
	}
	host := strings.TrimSpace(p.Host)
	if host == "" {
		host = defaultExternalHost
	}
	spec, ok := visualExternalHosts[host]
	if !ok {
		return "", fmt.Errorf("unknown external evidence host %q", host)
	}
	expiry := strings.TrimSpace(p.Expiry)
	if expiry == "" {
		expiry = defaultExternalExpiry
	}
	urls := make(map[string]string, len(manifest.Items))
	for _, item := range manifest.Items {
		url, err := uploadToExternalHost(spec, expiry, item.Path)
		if err != nil {
			return "", err
		}
		urls[item.ScenarioID] = url
	}
	body := composeExternalHostComment(spec, expiry, urls, manifest)
	return upsertEvidenceComment(resolved, owner, name, prNumber, existingCommentURL, manifest.Key, body)
}

// uploadToExternalHost POSTs one PNG to an anonymous host and returns the hosted URL.
// The host answers with the URL as plain text; any non-200 or non-URL body is a
// failure so the caller can fix forward without emitting a broken image.
func uploadToExternalHost(spec externalHostSpec, expiry, pngPath string) (string, error) {
	contents, err := os.ReadFile(pngPath)
	if err != nil {
		return "", err
	}
	var payload bytes.Buffer
	form := multipart.NewWriter(&payload)
	if err := form.WriteField("reqtype", "fileupload"); err != nil {
		return "", err
	}
	if spec.withExpiry {
		if err := form.WriteField("time", expiry); err != nil {
			return "", err
		}
	}
	part, err := form.CreateFormFile("fileToUpload", filepath.Base(pngPath))
	if err != nil {
		return "", err
	}
	if _, err := part.Write(contents); err != nil {
		return "", err
	}
	if err := form.Close(); err != nil {
		return "", err
	}
	request, err := http.NewRequest(http.MethodPost, spec.endpoint, &payload)
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", form.FormDataContentType())
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	url := strings.TrimSpace(string(raw))
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(url, "http") {
		return "", fmt.Errorf("upload to %s failed (HTTP %d): %s", spec.label, response.StatusCode, boundedObservation(url))
	}
	return url, nil
}

// composeExternalHostComment renders the single Boatstack-owned comment for
// external-host mode: the idempotency marker, the trust fingerprints, one inline
// image per scenario pinned to its hosted URL, and a standing reminder naming the
// third-party host and (when the host expires uploads) the expiry window.
func composeExternalHostComment(spec externalHostSpec, expiry string, urls map[string]string, manifest PRVisualEvidenceManifest) string {
	var builder strings.Builder
	builder.WriteString(visualEvidenceCommentMarker(manifest.Key) + "\n")
	builder.WriteString("### Visual evidence\n\n")
	builder.WriteString("Screenshots are human-review evidence, not mechanical proof.\n\n")
	builder.WriteString(fmt.Sprintf("Source commit `%s` · product diff `%s` · fingerprint `%s`\n\n", manifest.SourceCommit, manifest.ProductDiffSHA256, manifest.Fingerprint))
	rendered := 0
	for _, scenario := range manifest.Scenarios {
		url, ok := urls[scenario.ID]
		if !ok {
			continue
		}
		caption := strings.Join(scenario.Expected, "; ")
		builder.WriteString(fmt.Sprintf("**%s** — %s (`%s`)\n\n", scenario.ID, caption, scenario.Viewport))
		builder.WriteString(fmt.Sprintf("![%s](%s)\n\n", scenario.ID, url))
		rendered++
	}
	if rendered == 0 {
		builder.WriteString("_No captured scenarios to display._\n")
	}
	builder.WriteString("---\n\n")
	if spec.withExpiry {
		builder.WriteString(fmt.Sprintf("📌 These images are hosted on **%s** and auto-expire in **%s** — merge or re-run before then. They are uploaded to a third-party anonymous host, so do not use this mode for sensitive screenshots.\n", spec.label, expiry))
	} else {
		builder.WriteString(fmt.Sprintf("📌 These images are hosted on **%s** (permanent, public). They are uploaded to a third-party anonymous host, so do not use this mode for sensitive screenshots.\n", spec.label))
	}
	return builder.String()
}
