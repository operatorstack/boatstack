package boatstack

// Boundary conformance for independent insights.
// control-law: confirmed-insight-becomes-reviewable-repository-diff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func configureInsights(t *testing.T, repo string, terminal DeliveryTerminal) WorkspaceContext {
	t.Helper()
	stateRoot := t.TempDir()
	t.Setenv("BOATSTACK_STATE_ROOT", stateRoot)
	config := testConfig()
	config.Insights = &InsightPolicy{
		Enabled: true, CaptureMode: "manual", ValueMap: "required", SuggestFeatures: true,
		EvaluateOnPR: true, PendingFrontier: true, CompletionMode: "human_confirmed",
	}
	if terminal != "" {
		config.Delivery = &DeliveryPolicy{Terminal: string(terminal)}
	}
	value, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "project.json")
	if err := os.WriteFile(configPath, value, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AttachDetached(AttachOptions{Repo: repo, ConfigPath: configPath}); err != nil {
		t.Fatal(err)
	}
	return WorkspaceFor(repo)
}

func configureEmbeddedInsights(t *testing.T, repo string, terminal DeliveryTerminal) WorkspaceContext {
	t.Helper()
	ctx := embeddedWorkspace(repo)
	config := testConfig()
	config.Insights = &InsightPolicy{
		Enabled: true, CaptureMode: "manual", ValueMap: "required", SuggestFeatures: true,
		EvaluateOnPR: true, PendingFrontier: true, CompletionMode: "human_confirmed",
	}
	if terminal != "" {
		config.Delivery = &DeliveryPolicy{Terminal: string(terminal)}
	}
	value, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(ctx.ProjectConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ctx.ProjectConfigPath(), value, 0o644); err != nil {
		t.Fatal(err)
	}
	invalidateWorkspaceCache()
	return ctx
}

func syntheticInsightDraft(t *testing.T, exact, primary string) []byte {
	t.Helper()
	relation := InsightSourceRelation{ID: "R-1", Subject: "operator", Relation: "needs", Object: "observable progress", Excerpt: "needs observable progress"}
	field := InsightProjectedField{Text: "A user needs observable progress.", SourceRelationIDs: []string{"R-1"}}
	draft := InsightCaptureDraft{
		SchemaVersion: insightSchemaVersion,
		Source:        InsightSource{Kind: "pasted", Exact: exact},
		PrimaryTopic:  primary,
		RelatedTopics: []string{"Reporting"},
	}
	draft.ValueMap.Operator = "product-value-projection"
	draft.ValueMap.Source = InsightSourceIdentity{SHA256: SHA256Bytes([]byte(exact)), Bytes: len([]byte(exact))}
	draft.ValueMap.User = field
	draft.ValueMap.CurrentState = field
	draft.ValueMap.ValueGap = field
	draft.ValueMap.DesiredOutcome = field
	draft.ValueMap.ValueMechanism = field
	draft.ValueMap.SmallestProof = InsightValidationField{Text: field.Text, SourceRelationIDs: field.SourceRelationIDs, SuccessSignal: "The pending state is visible."}
	draft.ValueMap.Assessments = []InsightClaimAssessment{{Claim: relation, Status: "source-only", EvidenceIDs: []string{}}}
	draft.ValueMap.Constraints = []InsightProjectedField{}
	draft.ValueMap.Evidence = []InsightRepositoryEvidence{}
	draft.ValueMap.Contradictions = []InsightRepositoryEvidence{}
	draft.ValueMap.Unknowns = []InsightProjectedField{}
	draft.ValueMap.ResolvedUnknowns = []InsightResolvedUnknown{}
	draft.ValueMap.FollowUpQuestions = []string{}
	draft.ValueMap.Verdict.Status = "testable"
	draft.ValueMap.Verdict.EvidenceGrade = "source-only"
	draft.ValueMap.Verdict.Statement = "This is a testable source-only value hypothesis."
	value, err := MarshalJSON(draft)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func saveSyntheticInsight(t *testing.T, repo string, input []byte) InsightView {
	t.Helper()
	check, err := CheckInsightCapture(repo, input)
	if err != nil {
		t.Fatal(err)
	}
	view, err := SaveInsightCapture(repo, input, check.PreviewNonce, check.PreviewFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

// Positive, relation, bypass, replay, and independent-identity conformance:
// confirmation creates only tracked repository artifacts and no detached data.
func TestInsightCaptureRepositoryBoundary(t *testing.T) {
	repo := detachedTestRepo(t, "https://example.invalid/insight-capture.git")
	ctx := configureInsights(t, repo, TerminalMerged)
	input := syntheticInsightDraft(t, "A vague external observation.", "Message association")
	root, err := ctx.InsightDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("insight check precondition changed: %v", err)
	}
	check, err := CheckInsightCapture(repo, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("read-only insight check created durable state")
	}
	if !strings.HasPrefix(check.RepositoryPath, "docs/insights/ins-") {
		t.Fatalf("preview did not disclose its repository target: %+v", check)
	}
	first, err := SaveInsightCapture(repo, input, check.PreviewNonce, check.PreviewFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := SaveInsightCapture(repo, input, check.PreviewNonce, check.PreviewFingerprint)
	if err != nil || replayed.Capture.ID != first.Capture.ID {
		t.Fatalf("confirmed preview replay was not idempotent: %v %+v", err, replayed)
	}
	second := saveSyntheticInsight(t, repo, input)
	if second.Capture.ID == first.Capture.ID || second.Capture.ValueMap.Source.SHA256 != first.Capture.ValueMap.Source.SHA256 {
		t.Fatal("identical independent captures did not retain distinct ids and equal source fingerprints")
	}
	directory, err := insightCaptureDir(ctx, first.Capture.ID)
	if err != nil {
		t.Fatal(err)
	}
	resolvedRepo, err := ResolveRepository(repo)
	if err != nil {
		t.Fatal(err)
	}
	relativeDirectory, err := filepath.Rel(resolvedRepo, directory)
	if err != nil || relativeDirectory == ".." || strings.HasPrefix(relativeDirectory, ".."+string(filepath.Separator)) {
		t.Fatalf("capture did not become a repository artifact: %s", directory)
	}
	if first.RepositoryPath != filepath.ToSlash(filepath.Join("docs", "insights", first.Capture.ID)) {
		t.Fatalf("capture did not report its PR-ready path: %s", first.RepositoryPath)
	}
	status := gitPorcelain(t, repo)
	for _, name := range []string{"capture.json", "insight.md", "events.jsonl"} {
		want := filepath.ToSlash(filepath.Join("docs", "insights", first.Capture.ID, name))
		if !strings.Contains(status, want) {
			t.Fatalf("save did not create a tracked diff for %s: %s", want, status)
		}
	}
	if _, err := os.Stat(filepath.Join(ctx.controlRoot, "insights")); !os.IsNotExist(err) {
		t.Fatalf("insight data leaked into detached control state: %v", err)
	}
}

// Negative and failure-state conformance: stale bytes fail without a partial
// artifact, while embedded and detached supervision both use the same repo path.
func TestInsightCaptureRejectsStaleInputAndSupportsEmbeddedMode(t *testing.T) {
	repo := detachedTestRepo(t, "https://example.invalid/insight-negative.git")
	ctx := configureInsights(t, repo, TerminalMerged)
	input := syntheticInsightDraft(t, "Original exact bytes.", "Import quality")
	check, err := CheckInsightCapture(repo, input)
	if err != nil {
		t.Fatal(err)
	}
	tampered := syntheticInsightDraft(t, "Changed exact bytes.", "Import quality")
	if _, err := SaveInsightCapture(repo, tampered, check.PreviewNonce, check.PreviewFingerprint); err == nil {
		t.Fatal("stale preview accepted changed source bytes")
	}
	root, _ := ctx.InsightDir()
	entries, err := os.ReadDir(root)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "ins-") {
			t.Fatal("rejected save left a partial capture")
		}
	}

	embedded := detachedTestRepo(t, "https://example.invalid/insight-embedded.git")
	configureEmbeddedInsights(t, embedded, TerminalPublished)
	embeddedView := saveSyntheticInsight(t, embedded, input)
	if !strings.HasPrefix(filepath.FromSlash(embeddedView.RepositoryPath), filepath.Join("docs", "insights")) {
		t.Fatalf("embedded capture did not use the repository inbox: %+v", embeddedView)
	}
}

func installInsightDelivery(t *testing.T, repo string, ctx WorkspaceContext, feature string, terminal DeliveryTerminal) {
	t.Helper()
	directory := ctx.FeatureDir(feature)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	plan := map[string]any{
		"feature_id":          feature,
		"acceptance_criteria": []any{map[string]any{"id": "AC-1", "description": "The visible outcome is observed."}},
	}
	writeMarkdownPlan(t, filepath.Join(directory, "plan.md"), plan, true)
	lock := []byte("{\"schema_version\":1}\n")
	if err := os.WriteFile(filepath.Join(directory, "plan.lock.json"), lock, 0o600); err != nil {
		t.Fatal(err)
	}
	state := DeliveryState{
		SchemaVersion: deliveryStateSchemaVersion, Feature: feature, PlanLockHash: SHA256Bytes(lock), ActiveIndex: 0,
		RepairCounters: map[string]int{}, Goal: string(terminal),
		Slices: []DeliverySlice{{ID: "delivery", Title: "Feature delivery", AcceptanceCriteria: []string{"AC-1"}, Status: StatusBuild}},
	}
	if err := saveDeliveryState(repo, state); err != nil {
		t.Fatal(err)
	}
}

// Relation conformance: capture -> topic -> delivery criterion -> PR lifecycle
// -> readiness, while related topics never become completion gates.
func TestInsightEvaluationAndHumanDisposition(t *testing.T) {
	repo := detachedTestRepo(t, "https://example.invalid/insight-evaluation.git")
	ctx := configureInsights(t, repo, TerminalMerged)
	feature := "observable-progress"
	installInsightDelivery(t, repo, ctx, feature, TerminalMerged)
	view := saveSyntheticInsight(t, repo, syntheticInsightDraft(t, "Show pending work.", "Progress visibility"))
	if view.Evaluation.State != InsightPendingDelivery {
		t.Fatalf("unexpected initial evaluation: %+v", view.Evaluation)
	}
	runGit(t, repo, "add", filepath.FromSlash(view.RepositoryPath))
	view, err := AssociateInsight(repo, view.Capture.ID, "Progress visibility", []string{"Secondary topic", "Reporting"})
	if err != nil {
		t.Fatal(err)
	}
	if status := gitPorcelain(t, repo); !strings.Contains(status, "AM "+filepath.ToSlash(filepath.Join(view.RepositoryPath, "events.jsonl"))) {
		t.Fatalf("association did not become a repository diff: %s", status)
	}
	view, err = BindInsight(repo, view.Capture.ID, feature, []string{"AC-1"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Evaluation.State != InsightEvaluating {
		t.Fatalf("bound build should be evaluating: %+v", view.Evaluation)
	}
	if _, err := DisposeInsight(repo, view.Capture.ID, "completed", "", ""); err == nil {
		t.Fatal("non-ready completion without a reason was accepted")
	}
	state, err := LoadDeliveryState(repo, feature)
	if err != nil {
		t.Fatal(err)
	}
	state.Slices[0].Status = StatusPublished
	state.Slices[0].PRState = "OPEN"
	state.ActiveIndex = 1
	if err := saveDeliveryState(repo, state); err != nil {
		t.Fatal(err)
	}
	evaluation, err := EvaluateInsight(repo, view.Capture.ID)
	if err != nil || evaluation.State != InsightWaitingForTerminal {
		t.Fatalf("open PR should wait for merged terminal: %v %+v", err, evaluation)
	}
	state.Slices[0].PRState = "MERGED"
	if err := saveDeliveryState(repo, state); err != nil {
		t.Fatal(err)
	}
	evaluation, _ = EvaluateInsight(repo, view.Capture.ID)
	if evaluation.State != InsightReadyToComplete {
		t.Fatalf("merged evidence should be ready: %+v", evaluation)
	}
	completed, err := DisposeInsight(repo, view.Capture.ID, "completed", "", "")
	if err != nil || completed.Disposition == nil || completed.Disposition.Outcome != "completed" {
		t.Fatalf("human completion failed: %v %+v", err, completed)
	}
}

func TestInsightFrontierAndDuplicatePreserveCaptures(t *testing.T) {
	repo := detachedTestRepo(t, "https://example.invalid/insight-frontier.git")
	configureInsights(t, repo, TerminalPublished)
	unclassified := saveSyntheticInsight(t, repo, syntheticInsightDraft(t, "First observation.", ""))
	original := saveSyntheticInsight(t, repo, syntheticInsightDraft(t, "Second observation.", "Search"))
	duplicate := saveSyntheticInsight(t, repo, syntheticInsightDraft(t, "Second observation repeated.", "Search"))
	if _, err := DisposeInsight(repo, duplicate.Capture.ID, "duplicate", "Same underlying observation.", original.Capture.ID); err != nil {
		t.Fatal(err)
	}
	report, err := InsightFrontier(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rows) != 2 || report.Rows[0].ID != unclassified.Capture.ID || report.Rows[0].State != InsightUnclassified {
		t.Fatalf("unexpected frontier ordering or duplicate filtering: %+v", report.Rows)
	}
	views, err := ListInsights(repo)
	if err != nil || len(views) != 3 {
		t.Fatalf("duplicate disposition removed an independent capture: %v %+v", err, views)
	}
}

func TestInsightEvaluationFailureAndPublishedTerminalStates(t *testing.T) {
	repo := detachedTestRepo(t, "https://example.invalid/insight-published.git")
	ctx := configureInsights(t, repo, TerminalPublished)
	feature := "published-insight"
	installInsightDelivery(t, repo, ctx, feature, TerminalPublished)
	view := saveSyntheticInsight(t, repo, syntheticInsightDraft(t, "A published delivery may satisfy this insight.", ""))
	if view.Evaluation.State != InsightUnclassified {
		t.Fatalf("capture without a primary topic should be unclassified: %+v", view.Evaluation)
	}
	view, err := AssociateInsight(repo, view.Capture.ID, "Published insight", []string{"Related only"})
	if err != nil || view.Evaluation.State != InsightPendingDelivery {
		t.Fatalf("associated capture should wait for delivery: %v %+v", err, view.Evaluation)
	}
	if _, err := BindInsight(repo, view.Capture.ID, feature, []string{"AC-stale"}); err == nil {
		t.Fatal("missing acceptance criterion was accepted")
	}
	view, err = BindInsight(repo, view.Capture.ID, feature, []string{"AC-1"})
	if err != nil || view.Evaluation.State != InsightEvaluating {
		t.Fatalf("valid binding should evaluate: %v %+v", err, view.Evaluation)
	}
	state, err := LoadDeliveryState(repo, feature)
	if err != nil {
		t.Fatal(err)
	}
	state.Slices[0].Status = StatusPublished
	state.Slices[0].PRState = "CLOSED"
	state.ActiveIndex = 1
	if err := saveDeliveryState(repo, state); err != nil {
		t.Fatal(err)
	}
	evaluation, err := EvaluateInsight(repo, view.Capture.ID)
	if err != nil || evaluation.State != InsightNeedsEvidence {
		t.Fatalf("closed PR should need evidence: %v %+v", err, evaluation)
	}
	state.Slices[0].PRState = "OPEN"
	if err := saveDeliveryState(repo, state); err != nil {
		t.Fatal(err)
	}
	reconcileInsightsForFeature(repo, feature)
	view, err = ShowInsight(repo, view.Capture.ID)
	if err != nil || view.Evaluation.State != InsightReadyToComplete || view.Disposition != nil {
		t.Fatalf("published terminal should be ready but never complete automatically: %v %+v", err, view)
	}
	foundEvaluation := false
	for _, event := range view.Events {
		foundEvaluation = foundEvaluation || event.Type == "evaluated"
	}
	if !foundEvaluation {
		t.Fatal("PR reconciliation did not record an evaluation event")
	}
	directory, err := insightCaptureDir(ctx, view.Capture.ID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(directory, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InsightFrontier(repo); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(directory, "events.jsonl"))
	if err != nil || string(before) != string(after) {
		t.Fatalf("read-only frontier changed insight events: %v", err)
	}
}

func TestInsightCaptureRejectsSymlinkEscape(t *testing.T) {
	repo := detachedTestRepo(t, "https://example.invalid/insight-symlink.git")
	ctx := configureInsights(t, repo, TerminalPublished)
	root, err := ctx.InsightDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	id := "ins-0123456789abcdef01234567"
	if err := os.Symlink(t.TempDir(), filepath.Join(root, id)); err != nil {
		t.Fatal(err)
	}
	if _, err := ShowInsight(repo, id); err == nil || !strings.Contains(strings.ToLower(err.Error()), "symlink") {
		t.Fatalf("symlinked capture escaped repository storage checks: %v", err)
	}

	escapeRepo := detachedTestRepo(t, "https://example.invalid/insight-root-symlink.git")
	configureInsights(t, escapeRepo, TerminalPublished)
	if err := os.MkdirAll(filepath.Join(escapeRepo, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(escapeRepo, "docs", "insights")); err != nil {
		t.Fatal(err)
	}
	input := syntheticInsightDraft(t, "A root symlink must not receive this insight.", "Safety")
	check, err := CheckInsightCapture(escapeRepo, input)
	if err == nil {
		_, err = SaveInsightCapture(escapeRepo, input, check.PreviewNonce, check.PreviewFingerprint)
	}
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "symlink") {
		t.Fatalf("repository inbox symlink escape was not rejected: %v", err)
	}
}

// Failure-state conformance: the machine capture and human projection are one
// immutable object. Tampering with either makes the capture unreadable instead
// of silently presenting a different PR artifact.
func TestInsightHumanProjectionIsFingerprintBound(t *testing.T) {
	repo := detachedTestRepo(t, "https://example.invalid/insight-projection.git")
	ctx := configureInsights(t, repo, TerminalPublished)
	view := saveSyntheticInsight(t, repo, syntheticInsightDraft(t, "Show this exact source in review.", "Review intake"))
	directory, err := insightCaptureDir(ctx, view.Capture.ID)
	if err != nil {
		t.Fatal(err)
	}
	markdownPath := filepath.Join(directory, "insight.md")
	markdown, err := os.ReadFile(markdownPath)
	if err != nil || !strings.Contains(string(markdown), "Show this exact source in review.") {
		t.Fatalf("human projection omitted the exact source: %v", err)
	}
	if err := os.WriteFile(markdownPath, append(markdown, []byte("\nchanged outside Boatstack\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ShowInsight(repo, view.Capture.ID); err == nil || !strings.Contains(err.Error(), "projection") {
		t.Fatalf("modified human projection was accepted: %v", err)
	}
}

// Bypass conformance: only the insight transition may edit tracked insight
// artifacts. Read and Git staging remain available for review and publication.
func TestInsightArtifactGuardBlocksRawWritesButAllowsReview(t *testing.T) {
	repo := detachedTestRepo(t, "https://example.invalid/insight-guard.git")
	configureInsights(t, repo, TerminalPublished)
	path := "docs/insights/ins-example/capture.json"
	for _, command := range []string{
		"printf bad > " + path,
		"rm " + path,
		"sed -i.bak s/a/b/ " + path,
	} {
		findings := ClassifyCommand(repo, command)
		if len(findings) == 0 || findings[0].Source != "insight-state" {
			t.Fatalf("raw insight mutation escaped the guard: %q %+v", command, findings)
		}
	}
	if findings := ClassifyCommand(repo, "git add "+path); len(findings) != 0 {
		t.Fatalf("Git staging of a reviewed insight diff was denied: %+v", findings)
	}
	if findings := ClassifyCommand(repo, "git diff -- "+path); len(findings) != 0 {
		t.Fatalf("review of an insight diff was denied: %+v", findings)
	}
	findings := ClassifyTool(repo, "write_file", map[string]any{"file_path": path, "content": "bad"})
	protected := false
	for _, finding := range findings {
		protected = protected || finding.Source == "insight-state"
	}
	if !protected {
		t.Fatalf("direct file tool escaped insight ownership: %+v", findings)
	}
}

func TestInsightConfigValidation(t *testing.T) {
	config := testConfig()
	config.Insights = &InsightPolicy{Enabled: true, CaptureMode: "automatic"}
	if err := ValidateConfig(config); err == nil || !strings.Contains(err.Error(), "capture_mode") {
		t.Fatalf("invalid automatic capture was accepted: %v", err)
	}
	config.Insights = nil
	if err := ValidateConfig(config); err != nil {
		t.Fatalf("absent insight configuration changed compatibility: %v", err)
	}
}
