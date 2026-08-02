package boatstack

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
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

// SelectVisualPublisher always chooses external hosting. Screenshot bytes never
// enter Git or a pull-request attachment path.
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
	if publish := visualPublishConfig(resolved); publish != nil {
		return ExternalHostVisualEvidencePublisher{Host: publish.Host, Expiry: publish.Expiry}
	}
	return ExternalHostVisualEvidencePublisher{Host: defaultExternalHost, Expiry: defaultExternalExpiry}
}

// visualPublishConfig reads the repository's visual-evidence publish preferences from
// the generated project config, returning nil when the config is absent, unreadable,
// or leaves the block unset so the caller uses the external-host defaults.
func visualPublishConfig(repo string) *VisualEvidencePublish {
	config, _, err := LoadConfig(WorkspaceFor(repo).ProjectConfigPath())
	if err != nil {
		return nil
	}
	return config.Workflow.VisualEvidencePublish
}

// PublishVisualEvidence remains as a compatibility entry point but uses the
// external-host path. No screenshot publisher writes image bytes to Git.
func (GitVisualEvidencePublisher) PublishVisualEvidence(repo, prURL, existingCommentURL string, manifest PRVisualEvidenceManifest) (string, error) {
	return (ExternalHostVisualEvidencePublisher{Host: defaultExternalHost, Expiry: defaultExternalExpiry}).PublishVisualEvidence(repo, prURL, existingCommentURL, manifest)
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

func visualEvidenceCommentMarker(key string) string {
	return "<!-- boatstack-visual-evidence:" + key + " -->"
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
	for _, item := range manifest.Items {
		if item.PrivacyStatus != "human-reviewed" {
			return "", fmt.Errorf("refusing external upload for scenario %s without HUMAN_REVIEWED privacy status", item.ScenarioID)
		}
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
		if err := verifyHostedVisualURL(spec, url); err != nil {
			return "", fmt.Errorf("verify hosted URL for %s: %w", item.ScenarioID, err)
		}
		urls[item.ScenarioID] = url
	}
	body := composeExternalHostComment(spec, expiry, urls, manifest)
	return upsertEvidenceComment(resolved, owner, name, prNumber, existingCommentURL, manifest.Key, body)
}

func verifyHostedVisualURL(spec externalHostSpec, value string) error {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" {
		return fmt.Errorf("host returned an invalid absolute HTTP URL")
	}
	endpoint, endpointErr := url.Parse(spec.endpoint)
	localTestHost := endpointErr == nil && (endpoint.Hostname() == "127.0.0.1" || endpoint.Hostname() == "localhost" || endpoint.Hostname() == "::1")
	if !localTestHost && !strings.EqualFold(parsed.Hostname(), spec.label) {
		return fmt.Errorf("host returned URL for unexpected domain %q", parsed.Hostname())
	}
	request, err := http.NewRequest(http.MethodGet, value, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		return fmt.Errorf("hosted URL returned HTTP %d", response.StatusCode)
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	if contentType != "" && !strings.HasPrefix(contentType, "image/") {
		return fmt.Errorf("hosted URL returned non-image content type %q", contentType)
	}
	return nil
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
		caption := visualCommentText(strings.Join(scenario.Expected, "; "))
		builder.WriteString(fmt.Sprintf("**%s** — %s (`%s`)\n\n", visualCommentText(scenario.ID), caption, visualCommentText(scenario.Viewport)))
		builder.WriteString(fmt.Sprintf("User context: %s  \nUser goal: %s  \nJourney step: %s  \nReviewer context: %s  \nEntry: `%s` · State: `%s` · Surface: `%s`\n\n", visualCommentText(scenario.UserContext), visualCommentText(scenario.UserGoal), visualCommentText(scenario.JourneyStep), visualCommentText(scenario.ReviewerContext), visualCommentText(scenario.Entry), visualCommentText(scenario.State), visualCommentText(scenario.Surface)))
		builder.WriteString(fmt.Sprintf("![%s](%s)\n\n", visualCommentText(scenario.ID), url))
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

func visualCommentText(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	value = html.EscapeString(value)
	return strings.NewReplacer("\\", "\\\\", "!", "\\!", "[", "\\[", "]", "\\]", "`", "\\`").Replace(value)
}
