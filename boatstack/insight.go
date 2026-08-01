package boatstack

// Independent insight captures.
//
// Boundary:          confirmed conversational value map -> tracked repository artifact
// Control law:       insight bytes enter only the repository insight inbox, and
//                    only when source and preview fingerprints match exactly
// Authorized actor:  explicit insight save/associate/bind/disposition commands
// Required evidence: valid value-map lineage, current preview, safe repository path
// Failure behavior:  fail closed without changing an existing capture
// Release condition: every deterministic validation succeeds
// control-law: confirmed-insight-becomes-reviewable-repository-diff

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode"
)

const insightSchemaVersion = 1

const (
	InsightUnclassified       = "UNCLASSIFIED"
	InsightPendingDelivery    = "PENDING_DELIVERY"
	InsightEvaluating         = "EVALUATING"
	InsightNeedsEvidence      = "NEEDS_EVIDENCE"
	InsightWaitingForTerminal = "WAITING_FOR_TERMINAL"
	InsightReadyToComplete    = "READY_TO_COMPLETE"
)

var (
	insightNow        = time.Now
	insightRandomRead = rand.Read
)

type InsightSource struct {
	Kind  string `json:"kind"`
	Exact string `json:"exact"`
}

type InsightSourceIdentity struct {
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
}

type InsightProjectedField struct {
	Text              string   `json:"text"`
	SourceRelationIDs []string `json:"source_relation_ids"`
}

type InsightValidationField struct {
	Text              string   `json:"text"`
	SourceRelationIDs []string `json:"source_relation_ids"`
	SuccessSignal     string   `json:"success_signal"`
}

type InsightSourceRelation struct {
	ID       string `json:"id"`
	Subject  string `json:"subject"`
	Relation string `json:"relation"`
	Object   string `json:"object"`
	Excerpt  string `json:"excerpt"`
}

type InsightRepositoryEvidence struct {
	ID          string `json:"id"`
	ClaimID     string `json:"claim_id"`
	Kind        string `json:"kind"`
	Path        string `json:"path"`
	Line        int    `json:"line"`
	Observation string `json:"observation"`
}

type InsightClaimAssessment struct {
	Claim       InsightSourceRelation `json:"claim"`
	Status      string                `json:"status"`
	EvidenceIDs []string              `json:"evidence_ids"`
}

type InsightResolvedUnknown struct {
	Unknown     InsightProjectedField `json:"unknown"`
	Resolution  string                `json:"resolution"`
	EvidenceIDs []string              `json:"evidence_ids"`
}

// InsightValueMapSnapshot mirrors the durable, human-confirmed portion of the
// conversational Product Value Map. Value Map itself remains read-only and
// writes nothing; this separately authorized snapshot is Boatstack input.
type InsightValueMapSnapshot struct {
	Operator          string                      `json:"operator"`
	Source            InsightSourceIdentity       `json:"source"`
	User              InsightProjectedField       `json:"user"`
	CurrentState      InsightProjectedField       `json:"current_state"`
	ValueGap          InsightProjectedField       `json:"value_gap"`
	DesiredOutcome    InsightProjectedField       `json:"desired_outcome"`
	ValueMechanism    InsightProjectedField       `json:"value_mechanism"`
	SmallestProof     InsightValidationField      `json:"smallest_proof"`
	Constraints       []InsightProjectedField     `json:"constraints"`
	Assessments       []InsightClaimAssessment    `json:"assessments"`
	Evidence          []InsightRepositoryEvidence `json:"evidence"`
	Contradictions    []InsightRepositoryEvidence `json:"contradictions"`
	Unknowns          []InsightProjectedField     `json:"unknowns"`
	ResolvedUnknowns  []InsightResolvedUnknown    `json:"resolved_unknowns"`
	FollowUpQuestions []string                    `json:"follow_up_questions"`
	Verdict           struct {
		Status        string `json:"status"`
		EvidenceGrade string `json:"evidence_grade"`
		Statement     string `json:"statement"`
	} `json:"verdict"`
}

type InsightCaptureDraft struct {
	SchemaVersion int                     `json:"schema_version"`
	Source        InsightSource           `json:"source"`
	ValueMap      InsightValueMapSnapshot `json:"value_map"`
	PrimaryTopic  string                  `json:"primary_topic,omitempty"`
	RelatedTopics []string                `json:"related_topics,omitempty"`
}

type InsightCapture struct {
	SchemaVersion      int                     `json:"schema_version"`
	ID                 string                  `json:"id"`
	CapturedAt         string                  `json:"captured_at"`
	PreviewNonce       string                  `json:"preview_nonce"`
	PreviewFingerprint string                  `json:"preview_fingerprint"`
	Source             InsightSource           `json:"source"`
	ValueMap           InsightValueMapSnapshot `json:"value_map"`
	PrimaryTopic       string                  `json:"primary_topic,omitempty"`
	RelatedTopics      []string                `json:"related_topics,omitempty"`
}

type InsightCheckResult struct {
	SchemaVersion      int                 `json:"schema_version"`
	VerificationStatus string              `json:"verification_status"`
	PreviewNonce       string              `json:"preview_nonce"`
	PreviewFingerprint string              `json:"preview_fingerprint"`
	SourceSHA256       string              `json:"source_sha256"`
	RepositoryPath     string              `json:"repository_path"`
	Draft              InsightCaptureDraft `json:"draft"`
}

type InsightBinding struct {
	Feature  string   `json:"feature"`
	Criteria []string `json:"criteria"`
}

type InsightEvaluation struct {
	State    string   `json:"state"`
	Reason   string   `json:"reason"`
	Feature  string   `json:"feature,omitempty"`
	Criteria []string `json:"criteria,omitempty"`
	Terminal string   `json:"terminal,omitempty"`
}

type InsightDisposition struct {
	Outcome     string `json:"outcome"`
	Reason      string `json:"reason,omitempty"`
	DuplicateOf string `json:"duplicate_of,omitempty"`
}

type InsightEvent struct {
	SchemaVersion int                 `json:"schema_version"`
	ID            string              `json:"id"`
	Type          string              `json:"type"`
	RecordedAt    string              `json:"recorded_at"`
	PrimaryTopic  string              `json:"primary_topic,omitempty"`
	RelatedTopics []string            `json:"related_topics,omitempty"`
	Binding       *InsightBinding     `json:"binding,omitempty"`
	Evaluation    *InsightEvaluation  `json:"evaluation,omitempty"`
	Disposition   *InsightDisposition `json:"disposition,omitempty"`
}

type InsightView struct {
	Capture        InsightCapture      `json:"capture"`
	RepositoryPath string              `json:"repository_path"`
	PrimaryTopic   string              `json:"primary_topic,omitempty"`
	RelatedTopics  []string            `json:"related_topics,omitempty"`
	Binding        *InsightBinding     `json:"binding,omitempty"`
	Evaluation     InsightEvaluation   `json:"evaluation"`
	Disposition    *InsightDisposition `json:"disposition,omitempty"`
	Events         []InsightEvent      `json:"events,omitempty"`
}

type InsightFrontierRow struct {
	ID           string `json:"id"`
	PrimaryTopic string `json:"primary_topic,omitempty"`
	State        string `json:"state"`
	Reason       string `json:"reason"`
	NextActor    string `json:"next_actor"`
	NextAction   string `json:"next_action"`
}

type InsightFrontierReport struct {
	SchemaVersion int                  `json:"schema_version"`
	Rows          []InsightFrontierRow `json:"rows"`
}

func insightTimestamp() string {
	return insightNow().UTC().Truncate(time.Second).Format(time.RFC3339)
}

func insightRandomHex(bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := insightRandomRead(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func normalizeTopic(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if len(value) > 160 {
		return "", fmt.Errorf("feature topic exceeds 160 bytes")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("feature topic contains control characters")
		}
	}
	return value, nil
}

func normalizeTopics(primary string, related []string) (string, []string, error) {
	primary, err := normalizeTopic(primary)
	if err != nil {
		return "", nil, err
	}
	if len(related) > 20 {
		return "", nil, fmt.Errorf("at most 20 related feature topics are allowed")
	}
	seen := map[string]bool{}
	if primary != "" {
		seen[strings.ToLower(primary)] = true
	}
	result := make([]string, 0, len(related))
	for _, candidate := range related {
		candidate, err = normalizeTopic(candidate)
		if err != nil {
			return "", nil, err
		}
		if candidate == "" {
			return "", nil, fmt.Errorf("related feature topics must be non-empty")
		}
		key := strings.ToLower(candidate)
		if seen[key] {
			return "", nil, fmt.Errorf("feature topics must be unique")
		}
		seen[key] = true
		result = append(result, candidate)
	}
	sort.Strings(result)
	return primary, result, nil
}

func validateProjectedField(name string, field InsightProjectedField, claims map[string]bool) error {
	if strings.TrimSpace(field.Text) == "" || len(field.SourceRelationIDs) == 0 {
		return fmt.Errorf("value_map.%s requires text and source relation lineage", name)
	}
	for _, id := range field.SourceRelationIDs {
		if !claims[id] {
			return fmt.Errorf("value_map.%s references unknown source relation %s", name, id)
		}
	}
	return nil
}

func validateInsightDraft(draft InsightCaptureDraft) (InsightCaptureDraft, error) {
	if draft.SchemaVersion != insightSchemaVersion {
		return draft, fmt.Errorf("insight schema_version must be %d", insightSchemaVersion)
	}
	draft.Source.Kind = strings.TrimSpace(draft.Source.Kind)
	if draft.Source.Kind == "" || draft.Source.Exact == "" {
		return draft, fmt.Errorf("insight source kind and exact text are required")
	}
	primary, related, err := normalizeTopics(draft.PrimaryTopic, draft.RelatedTopics)
	if err != nil {
		return draft, err
	}
	draft.PrimaryTopic, draft.RelatedTopics = primary, related
	vm := &draft.ValueMap
	if vm.Operator != "product-value-projection" {
		return draft, fmt.Errorf("value_map.operator must be product-value-projection")
	}
	sourceBytes := []byte(draft.Source.Exact)
	if vm.Source.SHA256 != SHA256Bytes(sourceBytes) || vm.Source.Bytes != len(sourceBytes) {
		return draft, fmt.Errorf("value map source identity does not match the exact captured input")
	}
	claims := map[string]bool{}
	for _, assessment := range vm.Assessments {
		claim := assessment.Claim
		if strings.TrimSpace(claim.ID) == "" || claims[claim.ID] || strings.TrimSpace(claim.Excerpt) == "" {
			return draft, fmt.Errorf("value map assessments require unique claims with exact excerpts")
		}
		claims[claim.ID] = true
		switch assessment.Status {
		case "repo-supported", "repo-contradicted", "source-only":
		default:
			return draft, fmt.Errorf("value map assessment %s has invalid status", claim.ID)
		}
	}
	if len(claims) == 0 {
		return draft, fmt.Errorf("value map requires assessed source relations")
	}
	for name, field := range map[string]InsightProjectedField{
		"user": vm.User, "current_state": vm.CurrentState, "value_gap": vm.ValueGap,
		"desired_outcome": vm.DesiredOutcome, "value_mechanism": vm.ValueMechanism,
	} {
		if err := validateProjectedField(name, field, claims); err != nil {
			return draft, err
		}
	}
	if err := validateProjectedField("smallest_proof", InsightProjectedField{Text: vm.SmallestProof.Text, SourceRelationIDs: vm.SmallestProof.SourceRelationIDs}, claims); err != nil {
		return draft, err
	}
	if strings.TrimSpace(vm.SmallestProof.SuccessSignal) == "" {
		return draft, fmt.Errorf("value_map.smallest_proof requires an observable success signal")
	}
	for _, evidence := range append(append([]InsightRepositoryEvidence{}, vm.Evidence...), vm.Contradictions...) {
		if strings.TrimSpace(evidence.ID) == "" || !claims[evidence.ClaimID] || strings.TrimSpace(evidence.Path) == "" || evidence.Line < 1 || strings.TrimSpace(evidence.Observation) == "" {
			return draft, fmt.Errorf("value map repository evidence is incomplete or unbound")
		}
		if evidence.Kind != "supports" && evidence.Kind != "contradicts" {
			return draft, fmt.Errorf("value map repository evidence kind must support or contradict")
		}
	}
	switch vm.Verdict.Status {
	case "testable", "blocked":
	default:
		return draft, fmt.Errorf("value map verdict status must be testable or blocked")
	}
	switch vm.Verdict.EvidenceGrade {
	case "repo-grounded", "source-only", "contradicted", "insufficient":
	default:
		return draft, fmt.Errorf("value map evidence grade is invalid")
	}
	if strings.TrimSpace(vm.Verdict.Statement) == "" {
		return draft, fmt.Errorf("value map verdict statement is required")
	}
	return draft, nil
}

func requireInsightWorkspace(repoPath string) (string, WorkspaceContext, InsightPolicy, error) {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return "", WorkspaceContext{}, InsightPolicy{}, err
	}
	ctx, err := ResolveWorkspaceContext(repo)
	if err != nil {
		return "", WorkspaceContext{}, InsightPolicy{}, err
	}
	config, _, err := LoadConfig(ctx.ProjectConfigPath())
	if err != nil {
		return "", WorkspaceContext{}, InsightPolicy{}, err
	}
	if config.Insights == nil || !config.Insights.Enabled {
		return "", WorkspaceContext{}, InsightPolicy{}, fmt.Errorf("insights are not enabled for this Boatstack project")
	}
	if err := validateInsightConfig(config.Insights); err != nil {
		return "", WorkspaceContext{}, InsightPolicy{}, err
	}
	return repo, ctx, *config.Insights, nil
}

func CheckInsightCapture(repoPath string, input []byte) (InsightCheckResult, error) {
	_, ctx, _, err := requireInsightWorkspace(repoPath)
	if err != nil {
		return InsightCheckResult{}, err
	}
	var draft InsightCaptureDraft
	if err := DecodeJSON("check insight capture", "stdin", input, &draft); err != nil {
		return InsightCheckResult{}, err
	}
	draft, err = validateInsightDraft(draft)
	if err != nil {
		return InsightCheckResult{}, err
	}
	normalized, err := MarshalJSON(draft)
	if err != nil {
		return InsightCheckResult{}, err
	}
	nonce, err := insightRandomHex(16)
	if err != nil {
		return InsightCheckResult{}, err
	}
	fingerprint := SHA256Bytes(append(append([]byte{}, normalized...), []byte("\x00"+nonce)...))
	repositoryPath, err := insightRepositoryPath(ctx, insightCaptureID(fingerprint))
	if err != nil {
		return InsightCheckResult{}, err
	}
	return InsightCheckResult{
		SchemaVersion: insightSchemaVersion, VerificationStatus: "VERIFIED", PreviewNonce: nonce,
		PreviewFingerprint: fingerprint, SourceSHA256: draft.ValueMap.Source.SHA256, RepositoryPath: repositoryPath, Draft: draft,
	}, nil
}

func insightCaptureID(previewFingerprint string) string {
	return "ins-" + SHA256Bytes([]byte("insight-capture\x00" + previewFingerprint))[:24]
}

func insightCaptureDir(ctx WorkspaceContext, id string) (string, error) {
	id, err := safeCacheSegment(id, "insight id")
	if err != nil || !strings.HasPrefix(id, "ins-") {
		return "", fmt.Errorf("invalid insight id")
	}
	root, err := ctx.InsightDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, id)
	if err := rejectSymlinkComponents(ctx.RepoRoot, path); err != nil {
		return "", err
	}
	return path, nil
}

func insightRepositoryPath(ctx WorkspaceContext, id string) (string, error) {
	directory, err := insightCaptureDir(ctx, id)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(ctx.RepoRoot, directory)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("insight path escapes repository boundary")
	}
	return filepath.ToSlash(relative), nil
}

func insightMarkdownFence(value string) string {
	fence := "```"
	for strings.Contains(value, fence) {
		fence += "`"
	}
	return fence
}

func renderInsightMarkdown(capture InsightCapture) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# Insight %s\n\n", capture.ID)
	fmt.Fprintf(&b, "- Captured: %s\n", capture.CapturedAt)
	fmt.Fprintf(&b, "- Source SHA-256: `%s`\n", capture.ValueMap.Source.SHA256)
	fmt.Fprintf(&b, "- Preview fingerprint: `%s`\n", capture.PreviewFingerprint)
	if capture.PrimaryTopic != "" {
		fmt.Fprintf(&b, "- Primary topic: %s\n", capture.PrimaryTopic)
	}
	if len(capture.RelatedTopics) > 0 {
		fmt.Fprintf(&b, "- Related topics: %s\n", strings.Join(capture.RelatedTopics, ", "))
	}
	b.WriteString("\n## Exact source\n\n")
	fence := insightMarkdownFence(capture.Source.Exact)
	fmt.Fprintf(&b, "%s\n%s\n%s\n", fence, capture.Source.Exact, fence)
	b.WriteString("\n## Product Value Map\n\n")
	fields := []struct{ label, value string }{
		{"User", capture.ValueMap.User.Text},
		{"Current state", capture.ValueMap.CurrentState.Text},
		{"Value gap", capture.ValueMap.ValueGap.Text},
		{"Desired outcome", capture.ValueMap.DesiredOutcome.Text},
		{"Mechanism", capture.ValueMap.ValueMechanism.Text},
		{"Smallest proof", capture.ValueMap.SmallestProof.Text},
		{"Success signal", capture.ValueMap.SmallestProof.SuccessSignal},
	}
	for _, field := range fields {
		fmt.Fprintf(&b, "### %s\n\n%s\n\n", field.label, html.EscapeString(field.value))
	}
	b.WriteString("## Verdict\n\n")
	fmt.Fprintf(&b, "- Status: `%s`\n- Evidence grade: `%s`\n\n%s\n", capture.ValueMap.Verdict.Status, capture.ValueMap.Verdict.EvidenceGrade, html.EscapeString(capture.ValueMap.Verdict.Statement))
	return []byte(b.String())
}

func SaveInsightCapture(repoPath string, input []byte, nonce, fingerprint string) (InsightView, error) {
	_, ctx, _, err := requireInsightWorkspace(repoPath)
	if err != nil {
		return InsightView{}, err
	}
	var draft InsightCaptureDraft
	if err := DecodeJSON("save insight capture", "stdin", input, &draft); err != nil {
		return InsightView{}, err
	}
	draft, err = validateInsightDraft(draft)
	if err != nil {
		return InsightView{}, err
	}
	normalized, err := MarshalJSON(draft)
	if err != nil {
		return InsightView{}, err
	}
	nonce = strings.TrimSpace(nonce)
	fingerprint = strings.TrimSpace(fingerprint)
	if nonce == "" || fingerprint == "" || SHA256Bytes(append(append([]byte{}, normalized...), []byte("\x00"+nonce)...)) != fingerprint {
		return InsightView{}, fmt.Errorf("insight preview fingerprint does not match the exact capture")
	}
	id := insightCaptureID(fingerprint)
	directory, err := insightCaptureDir(ctx, id)
	if err != nil {
		return InsightView{}, err
	}
	if existing, loadErr := loadInsightCapture(ctx, id); loadErr == nil {
		if existing.PreviewFingerprint != fingerprint {
			return InsightView{}, fmt.Errorf("existing insight identity does not match the confirmed preview")
		}
		return showInsightWithContext(repoPath, ctx, id)
	} else if !os.IsNotExist(loadErr) {
		return InsightView{}, loadErr
	}
	capture := InsightCapture{
		SchemaVersion: insightSchemaVersion, ID: id, CapturedAt: insightTimestamp(), PreviewFingerprint: fingerprint,
		PreviewNonce: nonce,
		Source:       draft.Source, ValueMap: draft.ValueMap, PrimaryTopic: draft.PrimaryTopic, RelatedTopics: draft.RelatedTopics,
	}
	value, err := MarshalJSON(capture)
	if err != nil {
		return InsightView{}, err
	}
	root, err := ctx.InsightDir()
	if err != nil {
		return InsightView{}, err
	}
	if err := rejectSymlinkComponents(ctx.RepoRoot, root); err != nil {
		return InsightView{}, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return InsightView{}, err
	}
	temporary, err := os.MkdirTemp(root, ".insight-*")
	if err != nil {
		return InsightView{}, err
	}
	defer os.RemoveAll(temporary)
	if err := os.Chmod(temporary, 0o755); err != nil {
		return InsightView{}, err
	}
	if err := atomicWriteMode(filepath.Join(temporary, "capture.json"), value, 0o644); err != nil {
		return InsightView{}, err
	}
	if err := atomicWriteMode(filepath.Join(temporary, "insight.md"), renderInsightMarkdown(capture), 0o644); err != nil {
		return InsightView{}, err
	}
	if err := atomicWriteMode(filepath.Join(temporary, "events.jsonl"), []byte{}, 0o644); err != nil {
		return InsightView{}, err
	}
	if err := os.Rename(temporary, directory); err != nil {
		if existing, loadErr := loadInsightCapture(ctx, id); loadErr == nil && existing.PreviewFingerprint == fingerprint {
			return showInsightWithContext(repoPath, ctx, id)
		}
		return InsightView{}, err
	}
	return showInsightWithContext(repoPath, ctx, id)
}

func loadInsightCapture(ctx WorkspaceContext, id string) (InsightCapture, error) {
	directory, err := insightCaptureDir(ctx, id)
	if err != nil {
		return InsightCapture{}, err
	}
	path := filepath.Join(directory, "capture.json")
	value, err := os.ReadFile(path)
	if err != nil {
		return InsightCapture{}, err
	}
	var capture InsightCapture
	if err := DecodeJSON("load insight capture", path, value, &capture); err != nil {
		return InsightCapture{}, err
	}
	if capture.SchemaVersion != insightSchemaVersion || capture.ID != id || capture.PreviewNonce == "" || capture.PreviewFingerprint == "" {
		return InsightCapture{}, fmt.Errorf("insight capture %s is invalid", id)
	}
	draft, err := validateInsightDraft(InsightCaptureDraft{
		SchemaVersion: capture.SchemaVersion, Source: capture.Source, ValueMap: capture.ValueMap,
		PrimaryTopic: capture.PrimaryTopic, RelatedTopics: capture.RelatedTopics,
	})
	if err != nil {
		return InsightCapture{}, fmt.Errorf("insight capture %s is invalid: %w", id, err)
	}
	normalized, err := MarshalJSON(draft)
	if err != nil {
		return InsightCapture{}, err
	}
	wantFingerprint := SHA256Bytes(append(append([]byte{}, normalized...), []byte("\x00"+capture.PreviewNonce)...))
	if wantFingerprint != capture.PreviewFingerprint || insightCaptureID(wantFingerprint) != capture.ID {
		return InsightCapture{}, fmt.Errorf("insight capture %s fingerprint is invalid", id)
	}
	markdownPath := filepath.Join(directory, "insight.md")
	markdown, err := os.ReadFile(markdownPath)
	if err != nil {
		return InsightCapture{}, err
	}
	if string(markdown) != string(renderInsightMarkdown(capture)) {
		return InsightCapture{}, fmt.Errorf("insight capture %s human-readable projection is stale or modified", id)
	}
	return capture, nil
}

func loadInsightEvents(ctx WorkspaceContext, id string) ([]InsightEvent, error) {
	directory, err := insightCaptureDir(ctx, id)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(directory, "events.jsonl")
	value, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	events := []InsightEvent{}
	for index, line := range strings.Split(strings.TrimSpace(string(value)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event InsightEvent
		if err := DecodeJSON("load insight event", fmt.Sprintf("%s:%d", path, index+1), []byte(line), &event); err != nil {
			return nil, err
		}
		if event.SchemaVersion != insightSchemaVersion || event.ID == "" || event.Type == "" || event.RecordedAt == "" {
			return nil, fmt.Errorf("insight event is invalid: %s:%d", path, index+1)
		}
		events = append(events, event)
	}
	return events, nil
}

func withInsightLock(ctx WorkspaceContext, id string, apply func() error) error {
	root, err := ctx.InsightDir()
	if err != nil {
		return err
	}
	lock := filepath.Join(root, ".locks", id+".lock")
	if err := rejectSymlinkComponents(ctx.RepoRoot, lock); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(lock), 0o700); err != nil {
		return err
	}
	for attempt := 0; attempt < 100; attempt++ {
		file, openErr := os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if openErr == nil {
			_, _ = fmt.Fprintf(file, "%d %s\n", os.Getpid(), insightTimestamp())
			_ = file.Close()
			defer os.Remove(lock)
			return apply()
		}
		if !isLockContention(openErr, lock) {
			return openErr
		}
		if info, statErr := os.Stat(lock); statErr == nil && insightNow().Sub(info.ModTime()) > time.Minute {
			_ = os.Remove(lock)
			continue
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("insight %s is busy", id)
}

func comparableInsightEvent(event InsightEvent) InsightEvent {
	event.ID, event.RecordedAt = "", ""
	return event
}

func appendInsightEvent(ctx WorkspaceContext, capture InsightCapture, event InsightEvent) (InsightEvent, error) {
	var accepted InsightEvent
	err := withInsightLock(ctx, capture.ID, func() error {
		events, err := loadInsightEvents(ctx, capture.ID)
		if err != nil {
			return err
		}
		candidate, err := MarshalJSON(comparableInsightEvent(event))
		if err != nil {
			return err
		}
		if len(events) > 0 {
			last, _ := MarshalJSON(comparableInsightEvent(events[len(events)-1]))
			if string(last) == string(candidate) {
				accepted = events[len(events)-1]
				return nil
			}
		}
		event.SchemaVersion = insightSchemaVersion
		event.RecordedAt = insightTimestamp()
		event.ID = "iev-" + SHA256Bytes([]byte(capture.ID + "\x00" + fmt.Sprint(len(events)) + "\x00" + string(candidate)))[:24]
		line, err := json.Marshal(event)
		if err != nil {
			return err
		}
		directory, err := insightCaptureDir(ctx, capture.ID)
		if err != nil {
			return err
		}
		path := filepath.Join(directory, "events.jsonl")
		existing, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if len(existing) > 0 && existing[len(existing)-1] != '\n' {
			existing = append(existing, '\n')
		}
		existing = append(existing, line...)
		existing = append(existing, '\n')
		if err := atomicWriteMode(path, existing, 0o644); err != nil {
			return err
		}
		accepted = event
		return nil
	})
	return accepted, err
}

func applyInsightEvents(capture InsightCapture, events []InsightEvent) InsightView {
	view := InsightView{Capture: capture, PrimaryTopic: capture.PrimaryTopic, RelatedTopics: append([]string{}, capture.RelatedTopics...), Events: events}
	for _, event := range events {
		switch event.Type {
		case "associated":
			view.PrimaryTopic = event.PrimaryTopic
			view.RelatedTopics = append([]string{}, event.RelatedTopics...)
		case "bound":
			if event.Binding != nil {
				copy := *event.Binding
				copy.Criteria = append([]string{}, event.Binding.Criteria...)
				view.Binding = &copy
			}
		case "evaluated":
			if event.Evaluation != nil {
				view.Evaluation = *event.Evaluation
			}
		case "dispositioned":
			if event.Disposition != nil {
				copy := *event.Disposition
				view.Disposition = &copy
			}
		}
	}
	return view
}

func showInsightWithContext(repoPath string, ctx WorkspaceContext, id string) (InsightView, error) {
	capture, err := loadInsightCapture(ctx, id)
	if err != nil {
		return InsightView{}, err
	}
	events, err := loadInsightEvents(ctx, id)
	if err != nil {
		return InsightView{}, err
	}
	view := applyInsightEvents(capture, events)
	view.RepositoryPath, err = insightRepositoryPath(ctx, id)
	if err != nil {
		return InsightView{}, err
	}
	view.Evaluation = evaluateInsightView(repoPath, view)
	return view, nil
}

func ShowInsight(repoPath, id string) (InsightView, error) {
	_, ctx, _, err := requireInsightWorkspace(repoPath)
	if err != nil {
		return InsightView{}, err
	}
	return showInsightWithContext(repoPath, ctx, id)
}

func ListInsights(repoPath string) ([]InsightView, error) {
	_, ctx, _, err := requireInsightWorkspace(repoPath)
	if err != nil {
		return nil, err
	}
	root, err := ctx.InsightDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return []InsightView{}, nil
	}
	if err != nil {
		return nil, err
	}
	views := []InsightView{}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "ins-") {
			continue
		}
		view, err := showInsightWithContext(repoPath, ctx, entry.Name())
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Capture.ID < views[j].Capture.ID })
	return views, nil
}

func ensureInsightOpen(view InsightView) error {
	if view.Disposition != nil {
		return fmt.Errorf("insight %s is already dispositioned as %s", view.Capture.ID, view.Disposition.Outcome)
	}
	return nil
}

func AssociateInsight(repoPath, id, primary string, related []string) (InsightView, error) {
	_, ctx, _, err := requireInsightWorkspace(repoPath)
	if err != nil {
		return InsightView{}, err
	}
	view, err := showInsightWithContext(repoPath, ctx, id)
	if err != nil {
		return InsightView{}, err
	}
	if err := ensureInsightOpen(view); err != nil {
		return InsightView{}, err
	}
	primary, related, err = normalizeTopics(primary, related)
	if err != nil {
		return InsightView{}, err
	}
	if primary == "" {
		return InsightView{}, fmt.Errorf("a primary feature topic is required")
	}
	if _, err := appendInsightEvent(ctx, view.Capture, InsightEvent{Type: "associated", PrimaryTopic: primary, RelatedTopics: related}); err != nil {
		return InsightView{}, err
	}
	return showInsightWithContext(repoPath, ctx, id)
}

func planCriterionIDs(repo, feature string) (map[string]bool, error) {
	plan, err := LoadPlan(filepath.Join(planningFeatureDir(repo, feature), "plan.md"))
	if err != nil {
		return nil, err
	}
	criteria, ok := objectSlice(plan["acceptance_criteria"])
	if !ok {
		return nil, fmt.Errorf("feature %s has no valid acceptance criteria", feature)
	}
	result := map[string]bool{}
	for _, criterion := range criteria {
		if id := strings.TrimSpace(stringValue(criterion["id"])); id != "" {
			result[id] = true
		}
	}
	return result, nil
}

func BindInsight(repoPath, id, feature string, criteria []string) (InsightView, error) {
	repo, ctx, _, err := requireInsightWorkspace(repoPath)
	if err != nil {
		return InsightView{}, err
	}
	view, err := showInsightWithContext(repo, ctx, id)
	if err != nil {
		return InsightView{}, err
	}
	if err := ensureInsightOpen(view); err != nil {
		return InsightView{}, err
	}
	if strings.TrimSpace(view.PrimaryTopic) == "" {
		return InsightView{}, fmt.Errorf("insight requires a primary feature topic before delivery binding")
	}
	feature = strings.TrimSpace(feature)
	if !featureSlugPattern.MatchString(feature) {
		return InsightView{}, fmt.Errorf("invalid managed feature id")
	}
	state, err := LoadDeliveryState(repo, feature)
	if err != nil {
		return InsightView{}, fmt.Errorf("managed feature %s is unavailable: %w", feature, err)
	}
	if err := checkDeliveryPlanLock(repo, feature, state); err != nil {
		return InsightView{}, err
	}
	available, err := planCriterionIDs(repo, feature)
	if err != nil {
		return InsightView{}, err
	}
	if len(criteria) == 0 {
		return InsightView{}, fmt.Errorf("at least one acceptance criterion is required")
	}
	seen := map[string]bool{}
	normalized := make([]string, 0, len(criteria))
	for _, criterion := range criteria {
		criterion = strings.TrimSpace(criterion)
		if criterion == "" || !available[criterion] {
			return InsightView{}, fmt.Errorf("feature %s has no current acceptance criterion %s", feature, criterion)
		}
		if !seen[criterion] {
			seen[criterion] = true
			normalized = append(normalized, criterion)
		}
	}
	sort.Strings(normalized)
	if _, err := appendInsightEvent(ctx, view.Capture, InsightEvent{Type: "bound", Binding: &InsightBinding{Feature: feature, Criteria: normalized}}); err != nil {
		return InsightView{}, err
	}
	return showInsightWithContext(repo, ctx, id)
}

func containsCriterion(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func evaluateInsightView(repo string, view InsightView) InsightEvaluation {
	if strings.TrimSpace(view.PrimaryTopic) == "" {
		return InsightEvaluation{State: InsightUnclassified, Reason: "The capture needs a human-confirmed primary feature topic."}
	}
	if view.Binding == nil {
		return InsightEvaluation{State: InsightPendingDelivery, Reason: "The primary feature topic is not bound to a managed delivery."}
	}
	binding := *view.Binding
	result := InsightEvaluation{Feature: binding.Feature, Criteria: append([]string{}, binding.Criteria...)}
	state, err := LoadDeliveryState(repo, binding.Feature)
	if err != nil {
		result.State, result.Reason = InsightNeedsEvidence, "The bound managed delivery is missing or unreadable."
		return result
	}
	if err := checkDeliveryPlanLock(repo, binding.Feature, state); err != nil {
		result.State, result.Reason = InsightNeedsEvidence, "The bound managed delivery no longer has a current plan lock."
		return result
	}
	available, err := planCriterionIDs(repo, binding.Feature)
	if err != nil {
		result.State, result.Reason = InsightNeedsEvidence, "The bound feature plan cannot be inspected."
		return result
	}
	for _, criterion := range binding.Criteria {
		if !available[criterion] {
			result.State, result.Reason = InsightNeedsEvidence, "A bound acceptance criterion is stale or missing."
			return result
		}
	}
	relevant := map[int]bool{}
	for _, criterion := range binding.Criteria {
		found := false
		for index, slice := range state.Slices {
			if containsCriterion(slice.AcceptanceCriteria, criterion) {
				relevant[index], found = true, true
			}
		}
		if !found {
			result.State, result.Reason = InsightNeedsEvidence, "A bound acceptance criterion is not assigned to a delivery slice."
			return result
		}
	}
	for index := range relevant {
		if state.Slices[index].Status != StatusPublished {
			result.State, result.Reason = InsightEvaluating, "The mapped delivery slice has not completed its test, review, and publication gates."
			return result
		}
	}
	terminal := resolveDeliveryTerminal(repo, binding.Feature)
	result.Terminal = string(terminal)
	for index := range relevant {
		prState := strings.ToUpper(strings.TrimSpace(state.Slices[index].PRState))
		if prState == "CLOSED" || prState == "PUBLISHED_CLOSED" {
			result.State, result.Reason = InsightNeedsEvidence, "A mapped delivery pull request closed without satisfying the terminal goal."
			return result
		}
	}
	if terminal == TerminalPublished {
		result.State, result.Reason = InsightReadyToComplete, "Mapped acceptance evidence passed and the delivery pull request is published."
		return result
	}
	for index := range relevant {
		prState := strings.ToUpper(strings.TrimSpace(state.Slices[index].PRState))
		if prState != "MERGED" && prState != "PUBLISHED_MERGED" {
			result.State, result.Reason = InsightWaitingForTerminal, "Mapped acceptance evidence passed; the delivery is waiting for a merged pull request."
			return result
		}
	}
	result.State, result.Reason = InsightReadyToComplete, "Mapped acceptance evidence passed and the delivery pull request is merged."
	return result
}

func EvaluateInsight(repoPath, id string) (InsightEvaluation, error) {
	view, err := ShowInsight(repoPath, id)
	if err != nil {
		return InsightEvaluation{}, err
	}
	return view.Evaluation, nil
}

func reconcileInsightsForFeature(repoPath, feature string) {
	_, ctx, policy, err := requireInsightWorkspace(repoPath)
	if err != nil || !policy.EvaluateOnPR {
		return
	}
	views, err := ListInsights(repoPath)
	if err != nil {
		return
	}
	for _, view := range views {
		if view.Binding == nil || view.Binding.Feature != feature || view.Disposition != nil {
			continue
		}
		evaluation := evaluateInsightView(repoPath, view)
		if len(view.Events) > 0 {
			last := view.Events[len(view.Events)-1]
			if last.Type == "evaluated" && last.Evaluation != nil && reflect.DeepEqual(*last.Evaluation, evaluation) {
				continue
			}
		}
		_, _ = appendInsightEvent(ctx, view.Capture, InsightEvent{Type: "evaluated", Evaluation: &evaluation})
	}
}

func DisposeInsight(repoPath, id, outcome, reason, duplicateOf string) (InsightView, error) {
	_, ctx, _, err := requireInsightWorkspace(repoPath)
	if err != nil {
		return InsightView{}, err
	}
	view, err := showInsightWithContext(repoPath, ctx, id)
	if err != nil {
		return InsightView{}, err
	}
	if err := ensureInsightOpen(view); err != nil {
		return InsightView{}, err
	}
	outcome = strings.ToLower(strings.TrimSpace(outcome))
	reason = strings.TrimSpace(reason)
	duplicateOf = strings.TrimSpace(duplicateOf)
	switch outcome {
	case "completed":
		if view.Evaluation.State != InsightReadyToComplete && reason == "" {
			return InsightView{}, fmt.Errorf("early completion requires a recorded reason")
		}
	case "deferred", "rejected":
		if reason == "" {
			return InsightView{}, fmt.Errorf("%s disposition requires a reason", outcome)
		}
	case "duplicate":
		if reason == "" || duplicateOf == "" || duplicateOf == id {
			return InsightView{}, fmt.Errorf("duplicate disposition requires a reason and another capture id")
		}
		if _, err := loadInsightCapture(ctx, duplicateOf); err != nil {
			return InsightView{}, fmt.Errorf("duplicate target is unavailable: %w", err)
		}
	default:
		return InsightView{}, fmt.Errorf("insight outcome must be completed, deferred, rejected, or duplicate")
	}
	disposition := &InsightDisposition{Outcome: outcome, Reason: reason, DuplicateOf: duplicateOf}
	if _, err := appendInsightEvent(ctx, view.Capture, InsightEvent{Type: "dispositioned", Disposition: disposition}); err != nil {
		return InsightView{}, err
	}
	return showInsightWithContext(repoPath, ctx, id)
}

func InsightFrontier(repoPath string) (InsightFrontierReport, error) {
	_, _, policy, err := requireInsightWorkspace(repoPath)
	if err != nil {
		return InsightFrontierReport{}, err
	}
	if !policy.PendingFrontier {
		return InsightFrontierReport{}, fmt.Errorf("insights.pending_frontier is not enabled")
	}
	views, err := ListInsights(repoPath)
	if err != nil {
		return InsightFrontierReport{}, err
	}
	report := InsightFrontierReport{SchemaVersion: insightSchemaVersion, Rows: []InsightFrontierRow{}}
	for _, view := range views {
		if view.Disposition != nil {
			continue
		}
		row := InsightFrontierRow{ID: view.Capture.ID, PrimaryTopic: view.PrimaryTopic, State: view.Evaluation.State, Reason: view.Evaluation.Reason}
		switch view.Evaluation.State {
		case InsightUnclassified:
			row.NextActor, row.NextAction = "human", "confirm a primary feature topic"
		case InsightPendingDelivery:
			row.NextActor, row.NextAction = "human", "bind the primary topic to a managed delivery"
		case InsightEvaluating:
			row.NextActor, row.NextAction = "delivery", "continue the mapped Boatstack delivery"
		case InsightNeedsEvidence:
			row.NextActor, row.NextAction = "human", "review the named evidence gap"
		case InsightWaitingForTerminal:
			row.NextActor, row.NextAction = "delivery", "wait for the configured delivery terminal"
		case InsightReadyToComplete:
			row.NextActor, row.NextAction = "human", "confirm insight completion"
		}
		report.Rows = append(report.Rows, row)
	}
	rank := map[string]int{InsightReadyToComplete: 0, InsightNeedsEvidence: 1, InsightUnclassified: 2, InsightPendingDelivery: 3, InsightEvaluating: 4, InsightWaitingForTerminal: 5}
	sort.Slice(report.Rows, func(i, j int) bool {
		if rank[report.Rows[i].State] == rank[report.Rows[j].State] {
			return report.Rows[i].ID < report.Rows[j].ID
		}
		return rank[report.Rows[i].State] < rank[report.Rows[j].State]
	})
	return report, nil
}

func FormatInsightFrontier(report InsightFrontierReport) string {
	if len(report.Rows) == 0 {
		return "Insight frontier: no pending captures.\n"
	}
	var b strings.Builder
	b.WriteString("Insight frontier\n")
	for _, row := range report.Rows {
		fmt.Fprintf(&b, "- %s [%s] %s — next: %s (%s)\n", row.ID, row.State, row.Reason, row.NextAction, row.NextActor)
	}
	return b.String()
}
