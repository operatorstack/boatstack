package boatstack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	latestReleaseEndpoint = "https://api.github.com/repos/operatorstack/boatstack/releases/latest"
	updateCacheTTL        = 24 * time.Hour
	updateReminderWindow  = 7 * 24 * time.Hour
)

var (
	updateNow            = time.Now
	fetchLatestRelease   = defaultFetchLatestRelease
	stableVersionPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)
)

type ReleaseInfo struct {
	Version string `json:"version"`
	Name    string `json:"name,omitempty"`
	Notes   string `json:"notes,omitempty"`
	URL     string `json:"url"`
}

type UpdateState struct {
	SchemaVersion       int       `json:"schema_version"`
	CurrentVersion      string    `json:"current_version"`
	LatestVersion       string    `json:"latest_version"`
	ReleaseName         string    `json:"release_name,omitempty"`
	ReleaseNotes        string    `json:"release_notes,omitempty"`
	ReleaseURL          string    `json:"release_url"`
	CheckedAt           time.Time `json:"checked_at"`
	LastNotifiedVersion string    `json:"last_notified_version,omitempty"`
	LastNotifiedAt      time.Time `json:"last_notified_at,omitempty"`
}

type UpdateCheckOptions struct {
	Repo   string
	Force  bool
	Notify bool
}

type UpdateCheckResult struct {
	Status         string
	CurrentVersion string
	LatestVersion  string
	ReleaseName    string
	ReleaseNotes   string
	ReleaseURL     string
	ShouldNotify   bool
	FromCache      bool
}

func parseStableVersion(value string) ([3]int, error) {
	match := stableVersionPattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return [3]int{}, fmt.Errorf("version must be a stable semantic version: %s", value)
	}
	parsed := [3]int{}
	for index := 0; index < 3; index++ {
		number, err := strconv.Atoi(match[index+1])
		if err != nil {
			return [3]int{}, fmt.Errorf("invalid semantic version: %s", value)
		}
		parsed[index] = number
	}
	return parsed, nil
}

func compareVersions(left, right string) (int, error) {
	a, err := parseStableVersion(left)
	if err != nil {
		return 0, err
	}
	b, err := parseStableVersion(right)
	if err != nil {
		return 0, err
	}
	for index := 0; index < 3; index++ {
		if a[index] < b[index] {
			return -1, nil
		}
		if a[index] > b[index] {
			return 1, nil
		}
	}
	return 0, nil
}

func normalizedVersion(value string) (string, error) {
	if _, err := parseStableVersion(value); err != nil {
		return "", err
	}
	return "v" + strings.TrimPrefix(strings.TrimSpace(value), "v"), nil
}

func defaultFetchLatestRelease() (ReleaseInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseEndpoint, nil)
	if err != nil {
		return ReleaseInfo{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "operatorstack-boatstack-update-check")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return ReleaseInfo{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ReleaseInfo{}, fmt.Errorf("latest release lookup returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		TagName    string `json:"tag_name"`
		Name       string `json:"name"`
		Body       string `json:"body"`
		HTMLURL    string `json:"html_url"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return ReleaseInfo{}, fmt.Errorf("invalid latest release response: %w", err)
	}
	if payload.Draft || payload.Prerelease {
		return ReleaseInfo{}, fmt.Errorf("latest release response is not a stable published release")
	}
	version, err := normalizedVersion(payload.TagName)
	if err != nil {
		return ReleaseInfo{}, err
	}
	if strings.TrimSpace(payload.HTMLURL) == "" {
		return ReleaseInfo{}, fmt.Errorf("latest release response is missing its URL")
	}
	return ReleaseInfo{
		Version: version, Name: strings.TrimSpace(payload.Name), Notes: strings.TrimSpace(payload.Body),
		URL: strings.TrimSpace(payload.HTMLURL),
	}, nil
}

func updateStatePath(repo string) string {
	return filepath.Join(repo, ".product-loop", "bin", "update-state.json")
}

func loadUpdateState(repo string) (UpdateState, error) {
	value, err := os.ReadFile(updateStatePath(repo))
	if err != nil {
		return UpdateState{}, err
	}
	var state UpdateState
	if err := json.Unmarshal(value, &state); err != nil {
		return UpdateState{}, err
	}
	if state.SchemaVersion != 1 {
		return UpdateState{}, fmt.Errorf("unsupported update-state schema")
	}
	return state, nil
}

func writeUpdateState(repo string, state UpdateState) error {
	path := updateStatePath(repo)
	if err := rejectSymlinkComponents(repo, path); err != nil {
		return err
	}
	value, err := MarshalJSON(state)
	if err != nil {
		return err
	}
	return atomicWrite(path, value)
}

func resultFromState(state UpdateState, fromCache bool) (UpdateCheckResult, error) {
	comparison, err := compareVersions(state.CurrentVersion, state.LatestVersion)
	if err != nil {
		return UpdateCheckResult{}, err
	}
	status := "current"
	if comparison < 0 {
		status = "available"
	}
	return UpdateCheckResult{
		Status: status, CurrentVersion: state.CurrentVersion, LatestVersion: state.LatestVersion,
		ReleaseName: state.ReleaseName, ReleaseNotes: state.ReleaseNotes,
		ReleaseURL: state.ReleaseURL, FromCache: fromCache,
	}, nil
}

func CheckForUpdate(options UpdateCheckOptions) (UpdateCheckResult, error) {
	repo, err := ResolveRepository(options.Repo)
	if err != nil {
		return UpdateCheckResult{}, err
	}
	current, err := normalizedVersion(Version)
	if err != nil {
		return UpdateCheckResult{}, fmt.Errorf("cannot check updates for Boatstack %s: %w", Version, err)
	}
	now := updateNow().UTC()
	state, stateErr := loadUpdateState(repo)
	previousState := state
	useCache := stateErr == nil && state.CurrentVersion == current && !options.Force && now.Sub(state.CheckedAt) >= 0 && now.Sub(state.CheckedAt) < updateCacheTTL
	if !useCache {
		release, fetchErr := fetchLatestRelease()
		if fetchErr != nil {
			return UpdateCheckResult{}, fetchErr
		}
		state = UpdateState{
			SchemaVersion: 1, CurrentVersion: current, LatestVersion: release.Version,
			ReleaseName: release.Name, ReleaseNotes: release.Notes,
			ReleaseURL: release.URL, CheckedAt: now,
		}
		if stateErr == nil {
			state.LastNotifiedVersion = previousState.LastNotifiedVersion
			state.LastNotifiedAt = previousState.LastNotifiedAt
		}
	}
	result, err := resultFromState(state, useCache)
	if err != nil {
		return UpdateCheckResult{}, err
	}
	if options.Notify && result.Status == "available" {
		newVersion := state.LastNotifiedVersion != state.LatestVersion
		reminderDue := state.LastNotifiedAt.IsZero() || now.Sub(state.LastNotifiedAt) >= updateReminderWindow
		result.ShouldNotify = newVersion || reminderDue
		if result.ShouldNotify {
			state.LastNotifiedVersion = state.LatestVersion
			state.LastNotifiedAt = now
		}
	}
	if !useCache || result.ShouldNotify {
		if err := writeUpdateState(repo, state); err != nil {
			return UpdateCheckResult{}, err
		}
	}
	return result, nil
}

func CachedUpdate(repoPath string) (UpdateCheckResult, bool) {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return UpdateCheckResult{}, false
	}
	state, err := loadUpdateState(repo)
	if err != nil {
		return UpdateCheckResult{}, false
	}
	result, err := resultFromState(state, true)
	if err != nil || result.Status != "available" {
		return UpdateCheckResult{}, false
	}
	return result, true
}

// PostShipUpdateNotice is deliberately best-effort: release discovery can add
// information after a successful publication, but it cannot change that result.
func PostShipUpdateNotice(repo string) (UpdateCheckResult, bool) {
	result, err := CheckForUpdate(UpdateCheckOptions{Repo: repo, Notify: true})
	if err != nil || result.Status != "available" || !result.ShouldNotify {
		return UpdateCheckResult{}, false
	}
	return result, true
}

func CheckPreviousGeneratedState(repo string) error {
	value, err := os.ReadFile(filepath.Join(repo, ".product-loop", "generated.lock.json"))
	if err != nil {
		return fmt.Errorf("missing generated provenance: %w", err)
	}
	var lock struct {
		Files map[string]string `json:"files"`
	}
	if err := json.Unmarshal(value, &lock); err != nil || len(lock.Files) == 0 {
		return fmt.Errorf("invalid generated provenance")
	}
	problems := []string{}
	for relative, expected := range lock.Files {
		current, readErr := os.ReadFile(filepath.Join(repo, filepath.FromSlash(relative)))
		if readErr != nil || SHA256Bytes(current) != expected {
			problems = append(problems, relative)
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("generated files changed since installation: %s", strings.Join(problems, ", "))
	}
	return nil
}

func CheckExistingInstallProvenance(repo string) error {
	value, err := os.ReadFile(filepath.Join(repo, ".product-loop", "bin", "install.lock.json"))
	if err != nil {
		return fmt.Errorf("missing previous local install lock: %w", err)
	}
	var lock installLock
	if err := json.Unmarshal(value, &lock); err != nil {
		return fmt.Errorf("invalid previous local install lock: %w", err)
	}
	if _, err := parseStableVersion(lock.BoatstackVersion); err != nil || strings.TrimSpace(lock.SourceCommit) == "" {
		return fmt.Errorf("previous local install lock has invalid release provenance")
	}
	binaryPath, err := resolveRepositoryRelativePath(repo, lock.BinaryPath)
	if err != nil {
		return fmt.Errorf("invalid previous helper path: %w", err)
	}
	actual, err := SHA256File(binaryPath)
	if err != nil || actual != lock.BinarySHA256 {
		return fmt.Errorf("previous Boatstack helper does not match its install lock")
	}
	return nil
}

func ValidateUpdateWorkspace(repo string, config ProjectConfig) error {
	version, err := normalizedVersion(Version)
	if err != nil {
		return err
	}
	wantBranch := "chore/update-boatstack-" + version
	branch := gitOutput(repo, "branch", "--show-current")
	if branch != wantBranch {
		return fmt.Errorf("update must run on %s; current branch is %s", wantBranch, branch)
	}
	if gitOutput(repo, "status", "--porcelain") != "" {
		return fmt.Errorf("update branch must start with a clean worktree")
	}
	defaultBranch := strings.TrimSpace(config.Project.DefaultBranch)
	if defaultBranch == "" {
		return fmt.Errorf("project.default_branch is required for updates")
	}
	head := gitOutput(repo, "rev-parse", "HEAD")
	remoteHead := gitOutput(repo, "rev-parse", "origin/"+defaultBranch)
	if head == "" || remoteHead == "" || head != remoteHead {
		return fmt.Errorf("update branch must start from the current origin/%s", defaultBranch)
	}
	if err := CheckPreviousGeneratedState(repo); err != nil {
		return err
	}
	if err := CheckInstalledHostHooks(repo, config.Adapters); err != nil {
		return fmt.Errorf("host-hook drift blocks update: %w", err)
	}
	return CheckExistingInstallProvenance(repo)
}
