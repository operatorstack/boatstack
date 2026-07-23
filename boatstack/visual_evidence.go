package boatstack

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const visualEvidenceSchemaVersion = 1

type PRVisualScenario struct {
	ID       string   `json:"id"`
	Entry    string   `json:"entry"`
	State    string   `json:"state"`
	Viewport string   `json:"viewport"`
	Expected []string `json:"expected"`
}

type PRVisualEvidenceItem struct {
	ScenarioID    string `json:"scenario_id"`
	Path          string `json:"path"`
	SHA256        string `json:"sha256"`
	MIMEType      string `json:"mime_type"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	DurationMS    int    `json:"duration_ms"`
	Viewport      string `json:"viewport"`
	CapturedAt    string `json:"captured_at"`
	Status        string `json:"status"`
	PrivacyStatus string `json:"privacy_status"`
}

type PRVisualPublication struct {
	State      string `json:"state"`
	PRURL      string `json:"pr_url,omitempty"`
	CommentURL string `json:"comment_url,omitempty"`
	UpdatedAt  string `json:"updated_at,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

type PRVisualEvidenceManifest struct {
	SchemaVersion     int                    `json:"schema_version"`
	Key               string                 `json:"key"`
	Policy            string                 `json:"policy"`
	Relevance         string                 `json:"relevance"`
	RelevanceSource   string                 `json:"relevance_source"`
	Reason            string                 `json:"reason,omitempty"`
	Status            string                 `json:"status"`
	SourceCommit      string                 `json:"source_commit"`
	ProductDiffSHA256 string                 `json:"product_diff_sha256"`
	Scenarios         []PRVisualScenario     `json:"scenarios,omitempty"`
	Items             []PRVisualEvidenceItem `json:"items,omitempty"`
	Publication       PRVisualPublication    `json:"publication"`
	Fingerprint       string                 `json:"fingerprint"`
}

type PRVisualCapabilityReceipt struct {
	SchemaVersion      int    `json:"schema_version"`
	BoatstackVersion   string `json:"boatstack_version"`
	LockfileSHA256     string `json:"lockfile_sha256,omitempty"`
	LaunchCommandHash  string `json:"launch_command_sha256,omitempty"`
	BrowserVersion     string `json:"browser_version,omitempty"`
	FrameworkConfigSHA string `json:"framework_config_sha256,omitempty"`
	HealthStatus       string `json:"health_status"`
	VerifiedAt         string `json:"verified_at"`
}

type PRVisualCaptureCapability struct {
	Kind    string `json:"kind"`
	Command string `json:"command,omitempty"`
}

// ResolvePRVisualCaptureCapability implements the portable capability cut for the
// visual capability. It selects repository-owned tooling (via the generic
// ResolveCapability spine) before host or machine-local capabilities. The
// browser-specific fallbacks below the repository cut are visual-only.
func ResolvePRVisualCaptureCapability(repo string, config ProjectConfig, hostBrowser bool, suppliedLaunch string, expectedReceipt PRVisualCapabilityReceipt) (PRVisualCaptureCapability, error) {
	resolution, err := ResolveCapability("visual", config)
	if err != nil {
		return PRVisualCaptureCapability{}, err
	}
	if resolution.Kind == "repository-command" {
		return PRVisualCaptureCapability{Kind: "repository-command", Command: resolution.Command}, nil
	}
	if hostBrowser {
		return PRVisualCaptureCapability{Kind: "host-browser"}, nil
	}
	if suppliedLaunch = strings.TrimSpace(suppliedLaunch); suppliedLaunch != "" {
		return PRVisualCaptureCapability{Kind: "supplied-launch", Command: suppliedLaunch}, nil
	}
	if _, err := LoadPRVisualCapability(repo, expectedReceipt); err == nil {
		return PRVisualCaptureCapability{Kind: "machine-runtime"}, nil
	}
	return PRVisualCaptureCapability{Kind: "unavailable"}, nil
}

func ProbePRVisualReadiness(parent context.Context, url string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := (&http.Client{Timeout: timeout}).Do(request)
	if err != nil {
		return fmt.Errorf("visual readiness probe failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		return fmt.Errorf("visual readiness probe returned HTTP %d", response.StatusCode)
	}
	return nil
}

func normalizedPRVisualEvidencePolicy(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "off"
	}
	return value
}

func visualEvidenceKey(mode, feature, head string) (string, error) {
	key := feature
	if mode == "ad-hoc" {
		key = previewSlug(head)
	}
	return safeCacheSegment(key, "visual evidence key")
}

func visualEvidenceDirectory(repo, key string) (string, error) {
	key, err := safeCacheSegment(key, "visual evidence key")
	if err != nil {
		return "", err
	}
	common, err := gitCommonDir(repo)
	if err != nil {
		return "", err
	}
	return filepath.Join(common, "boatstack", "visual-evidence", key), nil
}

func visualEvidenceManifestPath(repo, key string) (string, error) {
	directory, err := visualEvidenceDirectory(repo, key)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "manifest.json"), nil
}

func visualCapabilityPath(repo string) (string, error) {
	common, err := gitCommonDir(repo)
	if err != nil {
		return "", err
	}
	return filepath.Join(common, "boatstack", "visual-evidence", "capability.json"), nil
}

func visualManifestFingerprint(manifest PRVisualEvidenceManifest) (string, error) {
	copy := manifest
	copy.Fingerprint = ""
	raw, err := MarshalJSON(copy)
	if err != nil {
		return "", err
	}
	return SHA256Bytes(raw), nil
}

func validateVisualManifest(manifest PRVisualEvidenceManifest) error {
	if manifest.SchemaVersion != visualEvidenceSchemaVersion {
		return fmt.Errorf("visual evidence schema_version must be %d", visualEvidenceSchemaVersion)
	}
	if _, err := safeCacheSegment(manifest.Key, "visual evidence key"); err != nil {
		return err
	}
	policy := normalizedPRVisualEvidencePolicy(manifest.Policy)
	if policy != "off" && policy != "suggest" && policy != "require" {
		return fmt.Errorf("visual evidence policy must be off, suggest, or require")
	}
	if manifest.Relevance != "relevant" && manifest.Relevance != "not_relevant" && manifest.Relevance != "unresolved" {
		return fmt.Errorf("visual evidence relevance must be relevant, not_relevant, or unresolved")
	}
	if manifest.RelevanceSource != "managed-plan" && manifest.RelevanceSource != "human-provided" && manifest.RelevanceSource != "repository-evidenced" && manifest.RelevanceSource != "agent-proposed" {
		return fmt.Errorf("unsupported visual evidence relevance source")
	}
	if manifest.Relevance == "not_relevant" && strings.TrimSpace(manifest.Reason) == "" {
		return fmt.Errorf("not-relevant visual evidence requires a reason")
	}
	if len(manifest.Scenarios) > 3 || len(manifest.Items) > 3 {
		return fmt.Errorf("visual evidence supports at most three scenarios and screenshots")
	}
	allowedStatus := map[string]bool{"PASS": true, "PASS_WITH_GAPS": true, "NOT_VERIFIED": true, "NOT_APPLICABLE": true, "BLOCKED": true}
	if !allowedStatus[manifest.Status] {
		return fmt.Errorf("unsupported visual evidence status %q", manifest.Status)
	}
	seenScenarios := map[string]bool{}
	scenarioViewports := map[string]string{}
	for _, scenario := range manifest.Scenarios {
		if scenario.ID == "" || seenScenarios[scenario.ID] || scenario.Entry == "" || scenario.State == "" || scenario.Viewport == "" || len(scenario.Expected) == 0 {
			return fmt.Errorf("visual evidence scenarios require unique ids, entry, state, viewport, and expected outcomes")
		}
		seenScenarios[scenario.ID] = true
		scenarioViewports[scenario.ID] = scenario.Viewport
	}
	seenItems := map[string]bool{}
	for _, item := range manifest.Items {
		if !seenScenarios[item.ScenarioID] || seenItems[item.ScenarioID] || item.MIMEType != "image/png" || item.DurationMS != 0 || item.SHA256 == "" || item.Width < 1 || item.Height < 1 {
			return fmt.Errorf("visual evidence items must reference a scenario and describe a valid PNG")
		}
		seenItems[item.ScenarioID] = true
		if item.Status != "captured" || item.Viewport != scenarioViewports[item.ScenarioID] {
			return fmt.Errorf("visual evidence items require captured status and the approved scenario viewport")
		}
		if item.PrivacyStatus != "clean" && item.PrivacyStatus != "human-reviewed" {
			return fmt.Errorf("visual evidence items require privacy_status clean or human-reviewed")
		}
		if _, err := time.Parse(time.RFC3339, item.CapturedAt); err != nil {
			return fmt.Errorf("visual evidence captured_at must be RFC3339: %w", err)
		}
	}
	if manifest.Status == "PASS" && (manifest.Relevance != "relevant" || len(manifest.Items) != len(manifest.Scenarios)) {
		return fmt.Errorf("PASS visual evidence requires one screenshot for every relevant scenario")
	}
	return nil
}

// SavePRVisualEvidence copies exact PNG bytes into Git-common Boatstack state,
// normalizes their metadata, and atomically records a fingerprinted manifest.
func SavePRVisualEvidence(repo string, manifest PRVisualEvidenceManifest) (PRVisualEvidenceManifest, error) {
	repo, err := ResolveRepository(repo)
	if err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	manifest.SchemaVersion = visualEvidenceSchemaVersion
	manifest.Policy = normalizedPRVisualEvidencePolicy(manifest.Policy)
	directory, err := visualEvidenceDirectory(repo, manifest.Key)
	if err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	if err := rejectSymlinkComponents(filepath.Dir(filepath.Dir(directory)), directory); err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	if previous, loadErr := LoadPRVisualEvidence(repo, manifest.Key); loadErr == nil && manifest.Publication.CommentURL == "" {
		manifest.Publication.PRURL = previous.Publication.PRURL
		manifest.Publication.CommentURL = previous.Publication.CommentURL
		if manifest.Publication.State == "" {
			manifest.Publication.State = "pending"
		}
	}
	for index := range manifest.Items {
		item := &manifest.Items[index]
		info, err := os.Lstat(item.Path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return PRVisualEvidenceManifest{}, fmt.Errorf("visual evidence PNG is missing or unsafe: %s", item.Path)
		}
		value, err := os.ReadFile(item.Path)
		if err != nil {
			return PRVisualEvidenceManifest{}, err
		}
		configuration, err := png.DecodeConfig(bytes.NewReader(value))
		if err != nil {
			return PRVisualEvidenceManifest{}, fmt.Errorf("visual evidence must be a valid PNG: %w", err)
		}
		hash := SHA256Bytes(value)
		destination := filepath.Join(directory, "assets", hash+".png")
		if err := atomicWriteMode(destination, value, 0o600); err != nil {
			return PRVisualEvidenceManifest{}, err
		}
		item.Path = destination
		item.SHA256 = hash
		item.MIMEType = "image/png"
		item.Width = configuration.Width
		item.Height = configuration.Height
		item.DurationMS = 0
	}
	sort.Slice(manifest.Items, func(i, j int) bool { return manifest.Items[i].ScenarioID < manifest.Items[j].ScenarioID })
	manifest.Fingerprint = ""
	if err := validateVisualManifest(manifest); err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	manifest.Fingerprint, err = visualManifestFingerprint(manifest)
	if err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	raw, err := MarshalJSON(manifest)
	if err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	path, err := visualEvidenceManifestPath(repo, manifest.Key)
	if err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	if err := atomicWriteMode(path, raw, 0o600); err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	return manifest, nil
}

func ImportPRVisualEvidence(repo, manifestPath string) (PRVisualEvidenceManifest, error) {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	var manifest PRVisualEvidenceManifest
	if err := DecodeJSON("import PR visual evidence", manifestPath, raw, &manifest); err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	return SavePRVisualEvidence(repo, manifest)
}

func LoadPRVisualEvidence(repo, key string) (PRVisualEvidenceManifest, error) {
	path, err := visualEvidenceManifestPath(repo, key)
	if err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	var manifest PRVisualEvidenceManifest
	if err := DecodeJSON("load PR visual evidence", path, raw, &manifest); err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	if err := validateVisualManifest(manifest); err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	expected, err := visualManifestFingerprint(manifest)
	if err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	if manifest.Fingerprint != expected {
		return PRVisualEvidenceManifest{}, fmt.Errorf("visual evidence manifest fingerprint is stale")
	}
	for _, item := range manifest.Items {
		if hash, err := SHA256File(item.Path); err != nil || hash != item.SHA256 {
			return PRVisualEvidenceManifest{}, fmt.Errorf("visual evidence screenshot is missing or stale: %s", item.ScenarioID)
		}
	}
	return manifest, nil
}

func recordPRVisualPublication(repo string, manifest PRVisualEvidenceManifest, publication PRVisualPublication) (PRVisualEvidenceManifest, error) {
	manifest.Publication = publication
	manifest.Fingerprint = ""
	if err := validateVisualManifest(manifest); err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	var err error
	manifest.Fingerprint, err = visualManifestFingerprint(manifest)
	if err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	raw, err := MarshalJSON(manifest)
	if err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	path, err := visualEvidenceManifestPath(repo, manifest.Key)
	if err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	if err := atomicWriteMode(path, raw, 0o600); err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	return manifest, nil
}

func RecordPRVisualPublication(repo, key, prURL, commentURL string) (PRVisualEvidenceManifest, error) {
	if strings.TrimSpace(prURL) == "" || strings.TrimSpace(commentURL) == "" {
		return PRVisualEvidenceManifest{}, fmt.Errorf("PR and visual evidence comment URLs are required")
	}
	manifest, err := LoadPRVisualEvidence(repo, key)
	if err != nil {
		return PRVisualEvidenceManifest{}, err
	}
	return recordPRVisualPublication(repo, manifest, PRVisualPublication{
		State: "published", PRURL: strings.TrimSpace(prURL), CommentURL: strings.TrimSpace(commentURL),
		UpdatedAt: time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
	})
}

func SavePRVisualCapability(repo string, receipt PRVisualCapabilityReceipt) error {
	receipt.SchemaVersion = visualEvidenceSchemaVersion
	receipt.BoatstackVersion = Version
	if receipt.VerifiedAt == "" {
		receipt.VerifiedAt = time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	}
	if receipt.HealthStatus != "ready" && receipt.HealthStatus != "unavailable" {
		return fmt.Errorf("visual capability health_status must be ready or unavailable")
	}
	path, err := visualCapabilityPath(repo)
	if err != nil {
		return err
	}
	raw, err := MarshalJSON(receipt)
	if err != nil {
		return err
	}
	return atomicWriteMode(path, raw, 0o600)
}

func LoadPRVisualCapability(repo string, expected PRVisualCapabilityReceipt) (PRVisualCapabilityReceipt, error) {
	path, err := visualCapabilityPath(repo)
	if err != nil {
		return PRVisualCapabilityReceipt{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return PRVisualCapabilityReceipt{}, err
	}
	var actual PRVisualCapabilityReceipt
	if err := DecodeJSON("load PR visual capability", path, raw, &actual); err != nil {
		return PRVisualCapabilityReceipt{}, err
	}
	if actual.SchemaVersion != visualEvidenceSchemaVersion || actual.BoatstackVersion != Version || actual.HealthStatus != "ready" ||
		actual.LockfileSHA256 != expected.LockfileSHA256 || actual.LaunchCommandHash != expected.LaunchCommandHash ||
		actual.BrowserVersion != expected.BrowserVersion || actual.FrameworkConfigSHA != expected.FrameworkConfigSHA {
		return PRVisualCapabilityReceipt{}, fmt.Errorf("visual evidence capability receipt is stale")
	}
	if _, err := time.Parse(time.RFC3339, actual.VerifiedAt); err != nil {
		return PRVisualCapabilityReceipt{}, fmt.Errorf("visual capability verified_at must be RFC3339: %w", err)
	}
	return actual, nil
}
