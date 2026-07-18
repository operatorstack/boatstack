package boatstack

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const deliveryStateSchemaVersion = 1

type DeliverySlice struct {
	ID                 string   `json:"id"`
	Title              string   `json:"title"`
	TaskIDs            []string `json:"task_ids"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	AffectedPaths      []string `json:"affected_paths,omitempty"`
	Status             string   `json:"status"`
	BaseBranch         string   `json:"base_branch,omitempty"`
	HeadBranch         string   `json:"head_branch,omitempty"`
	PRURL              string   `json:"pr_url,omitempty"`
}

type DeliveryState struct {
	SchemaVersion int             `json:"schema_version"`
	Feature       string          `json:"feature"`
	PlanLockHash  string          `json:"plan_lock_sha256"`
	ActiveIndex   int             `json:"active_index"`
	Slices        []DeliverySlice `json:"slices"`
}

type DeliveryGateReceipt struct {
	SchemaVersion int    `json:"schema_version"`
	Feature       string `json:"feature"`
	SliceID       string `json:"slice_id"`
	Gate          string `json:"gate"`
	Status        string `json:"status"`
	BaseBranch    string `json:"base_branch"`
	HeadBranch    string `json:"head_branch"`
	HeadCommit    string `json:"head_commit"`
	DiffSHA256    string `json:"diff_sha256"`
	EvidencePath  string `json:"evidence_path"`
	EvidenceHash  string `json:"evidence_sha256"`
	RecordedAt    string `json:"recorded_at"`
}

type DeliveryGateOptions struct {
	Repo         string
	Feature      string
	SliceID      string
	Gate         string
	Status       string
	BaseBranch   string
	EvidencePath string
}

func deliveryEvidenceGateStatus(value, gate, sliceID string, explicit bool) string {
	if !explicit {
		return evidenceGateStatus(value, gate)
	}
	pattern := regexp.MustCompile(`(?mi)^\s*-\s*` + regexp.QuoteMeta(gate) + `\s+gate\s*\(\s*` + regexp.QuoteMeta(sliceID) + `\s*\)\s*:\s*` + "`?" + `([A-Z_]+)` + "`?" + `\s*$`)
	if match := pattern.FindStringSubmatch(value); len(match) == 2 {
		return strings.ToUpper(match[1])
	}
	return ""
}

func deliveryDefinitions(plan map[string]any) ([]DeliverySlice, error) {
	tasks, _ := objectSlice(plan["tasks"])
	if plan["delivery_slices"] == nil {
		taskIDs := make([]string, 0, len(tasks))
		criteria := []string{}
		seenCriteria := map[string]bool{}
		for _, task := range tasks {
			taskIDs = append(taskIDs, stringValue(task["id"]))
			mapped, _ := stringSlice(task["acceptance_criteria"])
			for _, criterion := range mapped {
				if !seenCriteria[criterion] {
					seenCriteria[criterion] = true
					criteria = append(criteria, criterion)
				}
			}
		}
		return []DeliverySlice{{ID: "delivery", Title: "Feature delivery", TaskIDs: taskIDs, AcceptanceCriteria: criteria, Status: "BUILD"}}, nil
	}
	items, ok := objectSlice(plan["delivery_slices"])
	if !ok || len(items) == 0 {
		return nil, fmt.Errorf("delivery_slices must be a non-empty list")
	}
	taskIDs := map[string]bool{}
	taskCriteria := map[string][]string{}
	taskPaths := map[string][]string{}
	for _, task := range tasks {
		id := stringValue(task["id"])
		taskIDs[id] = true
		taskCriteria[id], _ = stringSlice(task["acceptance_criteria"])
		taskPaths[id], _ = stringSlice(task["affected_paths"])
	}
	seenSlices := map[string]bool{}
	assigned := map[string]string{}
	result := make([]DeliverySlice, 0, len(items))
	for index, item := range items {
		id := strings.TrimSpace(stringValue(item["id"]))
		if !featureSlugPattern.MatchString(id) || seenSlices[id] {
			return nil, fmt.Errorf("delivery slice ids must be unique lowercase kebab-case values")
		}
		seenSlices[id] = true
		title := strings.TrimSpace(stringValue(item["title"]))
		if title == "" {
			return nil, fmt.Errorf("delivery slice %s requires a title", id)
		}
		mapped, mappedOK := stringSlice(item["task_ids"])
		if !mappedOK || len(mapped) == 0 {
			return nil, fmt.Errorf("delivery slice %s requires task_ids", id)
		}
		criteria := []string{}
		affectedPaths := []string{}
		seenCriteria := map[string]bool{}
		seenPaths := map[string]bool{}
		for _, taskID := range mapped {
			if !taskIDs[taskID] {
				return nil, fmt.Errorf("delivery slice %s maps unknown task %s", id, taskID)
			}
			if owner := assigned[taskID]; owner != "" {
				return nil, fmt.Errorf("task %s is assigned to delivery slices %s and %s", taskID, owner, id)
			}
			assigned[taskID] = id
			if len(taskPaths[taskID]) == 0 {
				return nil, fmt.Errorf("task %s in explicit delivery slice %s requires affected_paths", taskID, id)
			}
			for _, path := range taskPaths[taskID] {
				if !seenPaths[path] {
					seenPaths[path] = true
					affectedPaths = append(affectedPaths, path)
				}
			}
			for _, criterion := range taskCriteria[taskID] {
				if !seenCriteria[criterion] {
					seenCriteria[criterion] = true
					criteria = append(criteria, criterion)
				}
			}
		}
		result = append(result, DeliverySlice{
			ID: id, Title: title, TaskIDs: mapped, AcceptanceCriteria: criteria, AffectedPaths: affectedPaths,
			Status: "PENDING", BaseBranch: strings.TrimSpace(stringValue(item["base_branch"])),
			HeadBranch: strings.TrimSpace(stringValue(item["head_branch"])),
		})
		if index == 0 {
			result[index].Status = "BUILD"
		}
	}
	unassigned := []string{}
	for taskID := range taskIDs {
		if assigned[taskID] == "" {
			unassigned = append(unassigned, taskID)
		}
	}
	if len(unassigned) > 0 {
		sort.Strings(unassigned)
		return nil, fmt.Errorf("tasks missing a delivery slice: %s", strings.Join(unassigned, ", "))
	}
	sliceIndex := map[string]int{}
	for index, slice := range result {
		sliceIndex[slice.ID] = index
	}
	for _, task := range tasks {
		taskID := stringValue(task["id"])
		dependencies, _ := stringSlice(task["depends_on"])
		for _, dependency := range dependencies {
			if sliceIndex[assigned[dependency]] > sliceIndex[assigned[taskID]] {
				return nil, fmt.Errorf("task %s in delivery slice %s depends on future slice task %s", taskID, assigned[taskID], dependency)
			}
		}
	}
	return result, nil
}

func deliveryStateDirectory(repo string) (string, error) {
	gitDirectory := gitOutput(repo, "rev-parse", "--path-format=absolute", "--git-dir")
	if gitDirectory == "" {
		gitDirectory = gitOutput(repo, "rev-parse", "--git-dir")
	}
	if gitDirectory == "" {
		return "", fmt.Errorf("cannot resolve the Git worktree directory")
	}
	if !filepath.IsAbs(gitDirectory) {
		gitDirectory = filepath.Join(repo, gitDirectory)
	}
	return filepath.Join(filepath.Clean(gitDirectory), "boatstack", "deliveries"), nil
}

func deliveryStatePath(repo, feature string) (string, error) {
	if !featureSlugPattern.MatchString(feature) {
		return "", fmt.Errorf("delivery state requires a lowercase kebab-case feature")
	}
	directory, err := deliveryStateDirectory(repo)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, feature, "state.json"), nil
}

func deliveryReceiptPath(repo, feature, sliceID, gate string) (string, error) {
	statePath, err := deliveryStatePath(repo, feature)
	if err != nil {
		return "", err
	}
	if !featureSlugPattern.MatchString(sliceID) || (gate != "test" && gate != "review") {
		return "", fmt.Errorf("invalid delivery receipt identity")
	}
	return filepath.Join(filepath.Dir(statePath), "receipts", sliceID, gate+".json"), nil
}

func saveDeliveryState(repo string, state DeliveryState) error {
	path, err := deliveryStatePath(repo, state.Feature)
	if err != nil {
		return err
	}
	value, err := MarshalJSON(state)
	if err != nil {
		return err
	}
	return atomicWriteMode(path, value, 0o644)
}

func LoadDeliveryState(repo, feature string) (DeliveryState, error) {
	path, err := deliveryStatePath(repo, feature)
	if err != nil {
		return DeliveryState{}, err
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return DeliveryState{}, fmt.Errorf("managed delivery state is missing: %w", err)
	}
	var state DeliveryState
	if err := DecodeJSON("load managed delivery state", path, value, &state); err != nil {
		return DeliveryState{}, err
	}
	if state.SchemaVersion != deliveryStateSchemaVersion || state.Feature != feature || len(state.Slices) == 0 || state.ActiveIndex < 0 || state.ActiveIndex > len(state.Slices) {
		return DeliveryState{}, fmt.Errorf("managed delivery state is invalid")
	}
	return state, nil
}

func initializeDeliveryState(repo, feature, planPath, lockPath string) error {
	plan, err := LoadPlan(planPath)
	if err != nil {
		return err
	}
	slices, err := deliveryDefinitions(plan)
	if err != nil {
		return err
	}
	lockHash, err := SHA256File(lockPath)
	if err != nil {
		return err
	}
	if existing, loadErr := LoadDeliveryState(repo, feature); loadErr == nil && existing.PlanLockHash == lockHash {
		return nil
	}
	return saveDeliveryState(repo, DeliveryState{
		SchemaVersion: deliveryStateSchemaVersion, Feature: feature, PlanLockHash: lockHash,
		ActiveIndex: 0, Slices: slices,
	})
}

func activeDeliverySlice(state DeliveryState) (DeliverySlice, error) {
	if state.ActiveIndex >= len(state.Slices) {
		return DeliverySlice{}, fmt.Errorf("all delivery slices are already published")
	}
	return state.Slices[state.ActiveIndex], nil
}

func checkDeliveryPlanLock(repo, feature string, state DeliveryState) error {
	lockPath := filepath.Join(repo, ".product-loop", "features", feature, "plan.lock.json")
	lockHash, err := SHA256File(lockPath)
	if err != nil {
		return fmt.Errorf("managed delivery requires its current plan lock: %w", err)
	}
	if state.PlanLockHash == "" || state.PlanLockHash != lockHash {
		return fmt.Errorf("managed delivery state is stale for the current plan lock; reactivate the approved plan")
	}
	return nil
}

func CurrentDeliveryState(repoPath, feature string) (DeliveryState, error) {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return DeliveryState{}, err
	}
	state, err := LoadDeliveryState(repo, feature)
	if err != nil {
		return DeliveryState{}, err
	}
	if err := checkDeliveryPlanLock(repo, feature, state); err != nil {
		return DeliveryState{}, err
	}
	return state, nil
}

func currentDiffIdentity(repo, base, previewPath string) (string, string, string, []string, error) {
	head, err := gitCommand(repo, "branch", "--show-current")
	if err != nil || head == "" {
		return "", "", "", nil, fmt.Errorf("delivery gate requires a named branch")
	}
	baseCommit, err := resolveBaseCommit(repo, base)
	if err != nil {
		return "", "", "", nil, err
	}
	mergeBase, err := gitCommand(repo, "merge-base", baseCommit, "HEAD")
	if err != nil || mergeBase == "" {
		return "", "", "", nil, fmt.Errorf("cannot determine delivery diff against %s", base)
	}
	diff, changed, err := productDiff(repo, mergeBase, previewPath)
	if err != nil {
		return "", "", "", nil, err
	}
	if len(changed) == 0 {
		return "", "", "", nil, fmt.Errorf("delivery slice has no committed changes relative to %s", base)
	}
	headCommit, err := gitCommand(repo, "rev-parse", "HEAD")
	if err != nil {
		return "", "", "", nil, err
	}
	return head, headCommit, SHA256Bytes(diff), changed, nil
}

func pathMatchesDeliveryScope(path string, patterns []string) bool {
	path = filepath.ToSlash(path)
	for _, pattern := range patterns {
		pattern = filepath.ToSlash(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		if pattern == "**" || pattern == "*" {
			return true
		}
		if strings.HasSuffix(pattern, "/**") {
			root := strings.TrimSuffix(pattern, "/**")
			if path == root || strings.HasPrefix(path, root+"/") {
				return true
			}
		}
		prefix := strings.TrimSuffix(pattern, "/")
		matched, _ := filepath.Match(filepath.FromSlash(pattern), filepath.FromSlash(path))
		if matched || path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func validateDeliveryScope(feature string, slice DeliverySlice, changed []string) error {
	if len(slice.AffectedPaths) == 0 {
		return nil
	}
	unexpected := []string{}
	artifactPrefix := ".product-loop/features/" + feature + "/"
	for _, path := range changed {
		path = filepath.ToSlash(path)
		if strings.HasPrefix(path, artifactPrefix) || pathMatchesDeliveryScope(path, slice.AffectedPaths) {
			continue
		}
		unexpected = append(unexpected, path)
	}
	if len(unexpected) > 0 {
		sort.Strings(unexpected)
		return fmt.Errorf("delivery slice %s contains changes outside its affected_paths: %s", slice.ID, strings.Join(unexpected, ", "))
	}
	return nil
}

func readDeliveryReceipt(repo, feature, sliceID, gate string) (DeliveryGateReceipt, error) {
	path, err := deliveryReceiptPath(repo, feature, sliceID, gate)
	if err != nil {
		return DeliveryGateReceipt{}, err
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return DeliveryGateReceipt{}, fmt.Errorf("%s gate receipt is missing for delivery slice %s", gate, sliceID)
	}
	var receipt DeliveryGateReceipt
	if err := DecodeJSON("load delivery gate receipt", path, value, &receipt); err != nil {
		return DeliveryGateReceipt{}, err
	}
	if receipt.SchemaVersion != deliveryStateSchemaVersion || receipt.Feature != feature || receipt.SliceID != sliceID || receipt.Gate != gate {
		return DeliveryGateReceipt{}, fmt.Errorf("%s gate receipt is invalid for delivery slice %s", gate, sliceID)
	}
	return receipt, nil
}

func RecordDeliveryGate(options DeliveryGateOptions) (DeliveryGateReceipt, error) {
	repo, err := ResolveRepository(options.Repo)
	if err != nil {
		return DeliveryGateReceipt{}, err
	}
	gate := strings.ToLower(strings.TrimSpace(options.Gate))
	status := strings.ToUpper(strings.TrimSpace(options.Status))
	if gate != "test" && gate != "review" {
		return DeliveryGateReceipt{}, fmt.Errorf("delivery gate must be test or review")
	}
	if status != "PASS" && status != "PASS_WITH_GAPS" {
		return DeliveryGateReceipt{}, fmt.Errorf("a delivery gate receipt may record only PASS or PASS_WITH_GAPS")
	}
	state, err := LoadDeliveryState(repo, options.Feature)
	if err != nil {
		return DeliveryGateReceipt{}, err
	}
	if err := checkDeliveryPlanLock(repo, options.Feature, state); err != nil {
		return DeliveryGateReceipt{}, err
	}
	slice, err := activeDeliverySlice(state)
	if err != nil {
		return DeliveryGateReceipt{}, err
	}
	if options.SliceID != "" && options.SliceID != slice.ID {
		return DeliveryGateReceipt{}, fmt.Errorf("delivery slice %s is not active; current slice is %s", options.SliceID, slice.ID)
	}
	base := strings.TrimSpace(options.BaseBranch)
	if base == "" {
		base = slice.BaseBranch
	}
	if base == "" {
		base = defaultPRBase(repo)
	}
	previewPath, _ := expectedPRPreviewPath("managed", options.Feature, "")
	head, headCommit, diffHash, changed, err := currentDiffIdentity(repo, base, previewPath)
	if err != nil {
		return DeliveryGateReceipt{}, err
	}
	if err := validateDeliveryScope(options.Feature, slice, changed); err != nil {
		return DeliveryGateReceipt{}, err
	}
	if slice.HeadBranch != "" && slice.HeadBranch != head {
		return DeliveryGateReceipt{}, fmt.Errorf("delivery slice %s requires head branch %s; current branch is %s", slice.ID, slice.HeadBranch, head)
	}
	if gate == "review" {
		if slice.Status != "TEST_PASSED" {
			return DeliveryGateReceipt{}, fmt.Errorf("delivery slice %s must pass its test gate before review", slice.ID)
		}
		testReceipt, receiptErr := readDeliveryReceipt(repo, options.Feature, slice.ID, "test")
		if receiptErr != nil {
			return DeliveryGateReceipt{}, receiptErr
		}
		if testReceipt.HeadCommit != headCommit || testReceipt.DiffSHA256 != diffHash || testReceipt.BaseBranch != base {
			return DeliveryGateReceipt{}, fmt.Errorf("delivery diff changed after the test gate; rerun test-gate for slice %s", slice.ID)
		}
	}
	evidencePath := strings.TrimSpace(options.EvidencePath)
	if evidencePath == "" {
		evidencePath = filepath.Join(repo, ".product-loop", "features", options.Feature, "evidence.md")
	} else if !filepath.IsAbs(evidencePath) {
		evidencePath = filepath.Join(repo, evidencePath)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(evidencePath); resolveErr == nil {
		evidencePath = resolved
	}
	evidenceHash, err := SHA256File(evidencePath)
	if err != nil {
		return DeliveryGateReceipt{}, fmt.Errorf("delivery gate requires current evidence: %w", err)
	}
	evidenceValue, err := os.ReadFile(evidencePath)
	if err != nil {
		return DeliveryGateReceipt{}, err
	}
	explicit := len(state.Slices) > 1 || state.Slices[0].ID != "delivery"
	gateLabel := strings.ToUpper(gate[:1]) + gate[1:]
	if recorded := deliveryEvidenceGateStatus(string(evidenceValue), gateLabel, slice.ID, explicit); recorded != status {
		return DeliveryGateReceipt{}, fmt.Errorf("evidence ledger must mark the %s gate for delivery slice %s as %s; found %q", gate, slice.ID, status, recorded)
	}
	relEvidence, err := repositoryRelativePath(repo, evidencePath)
	if err != nil {
		return DeliveryGateReceipt{}, err
	}
	receipt := DeliveryGateReceipt{
		SchemaVersion: deliveryStateSchemaVersion, Feature: options.Feature, SliceID: slice.ID,
		Gate: gate, Status: status, BaseBranch: base, HeadBranch: head, HeadCommit: headCommit,
		DiffSHA256: diffHash, EvidencePath: relEvidence, EvidenceHash: evidenceHash,
		RecordedAt: time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
	}
	path, _ := deliveryReceiptPath(repo, options.Feature, slice.ID, gate)
	value, _ := MarshalJSON(receipt)
	if err := atomicWriteMode(path, value, 0o644); err != nil {
		return DeliveryGateReceipt{}, err
	}
	state.Slices[state.ActiveIndex].BaseBranch = base
	state.Slices[state.ActiveIndex].HeadBranch = head
	if gate == "test" {
		state.Slices[state.ActiveIndex].Status = "TEST_PASSED"
		if reviewPath, pathErr := deliveryReceiptPath(repo, options.Feature, slice.ID, "review"); pathErr == nil {
			_ = os.Remove(reviewPath)
		}
	} else {
		state.Slices[state.ActiveIndex].Status = "REVIEW_PASSED"
	}
	if err := saveDeliveryState(repo, state); err != nil {
		return DeliveryGateReceipt{}, err
	}
	return receipt, nil
}

func CheckDeliveryReadyForShip(repo, feature, base, head, diffHash string, changed []string) (DeliveryState, DeliverySlice, []PRSource, error) {
	state, err := LoadDeliveryState(repo, feature)
	if err != nil {
		return DeliveryState{}, DeliverySlice{}, nil, err
	}
	if err := checkDeliveryPlanLock(repo, feature, state); err != nil {
		return DeliveryState{}, DeliverySlice{}, nil, err
	}
	slice, err := activeDeliverySlice(state)
	if err != nil {
		return DeliveryState{}, DeliverySlice{}, nil, err
	}
	if slice.Status != "REVIEW_PASSED" {
		return DeliveryState{}, DeliverySlice{}, nil, fmt.Errorf("delivery slice %s has not passed test and review gates", slice.ID)
	}
	if err := validateDeliveryScope(feature, slice, changed); err != nil {
		return DeliveryState{}, DeliverySlice{}, nil, err
	}
	sources := []PRSource{}
	for _, gate := range []string{"test", "review"} {
		receipt, receiptErr := readDeliveryReceipt(repo, feature, slice.ID, gate)
		if receiptErr != nil {
			return DeliveryState{}, DeliverySlice{}, nil, receiptErr
		}
		if receipt.BaseBranch != base || receipt.HeadBranch != head || receipt.DiffSHA256 != diffHash {
			return DeliveryState{}, DeliverySlice{}, nil, fmt.Errorf("stale delivery receipt: diff changed after the %s gate; rerun gates for slice %s", gate, slice.ID)
		}
		path, _ := deliveryReceiptPath(repo, feature, slice.ID, gate)
		hash, _ := SHA256File(path)
		sources = append(sources, PRSource{Kind: gate + "_gate_receipt", Path: ".git/boatstack/deliveries/" + feature + "/receipts/" + slice.ID + "/" + gate + ".json", SHA256: hash})
	}
	return state, slice, sources, nil
}

func MarkDeliveryPublished(repo, feature, sliceID, url string) error {
	state, err := LoadDeliveryState(repo, feature)
	if err != nil {
		return err
	}
	if err := checkDeliveryPlanLock(repo, feature, state); err != nil {
		return err
	}
	slice, err := activeDeliverySlice(state)
	if err != nil {
		return err
	}
	if slice.ID != sliceID || slice.Status != "REVIEW_PASSED" {
		return fmt.Errorf("delivery slice %s is not ready to publish", sliceID)
	}
	state.Slices[state.ActiveIndex].Status = "PUBLISHED"
	state.Slices[state.ActiveIndex].PRURL = url
	state.ActiveIndex++
	if state.ActiveIndex < len(state.Slices) {
		state.Slices[state.ActiveIndex].Status = "BUILD"
	}
	return saveDeliveryState(repo, state)
}

func ActiveManagedDeliveries(repo string) ([]string, error) {
	directory, err := deliveryStateDirectory(repo)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	active := []string{}
	for _, entry := range entries {
		if !entry.IsDir() || !featureSlugPattern.MatchString(entry.Name()) {
			continue
		}
		state, loadErr := LoadDeliveryState(repo, entry.Name())
		if loadErr != nil {
			return nil, fmt.Errorf("invalid managed delivery state for %s: %w", entry.Name(), loadErr)
		}
		if state.ActiveIndex < len(state.Slices) {
			active = append(active, entry.Name())
		}
	}
	sort.Strings(active)
	return active, nil
}
