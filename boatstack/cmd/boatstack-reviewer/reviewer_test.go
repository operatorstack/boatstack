package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/kernel"
)

// repositoryRoot is the real Boatstack repository, whose committed policy
// assets seed every scratch repository so the tests exercise the exact
// admitted contract.
func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(policyPromptPath))); err != nil {
		t.Fatalf("admitted policy prompt is unavailable at %s: %v", root, err)
	}
	return root
}

type scratchRepo struct {
	t    *testing.T
	repo *gitRepo
}

func (s *scratchRepo) git(args ...string) string {
	s.t.Helper()
	output, err := gitOutput(s.repo.Root, args...)
	if err != nil {
		s.t.Fatal(err)
	}
	return strings.TrimSpace(output)
}

func (s *scratchRepo) writeFile(relative, contents string) {
	s.t.Helper()
	path := filepath.Join(s.repo.Root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		s.t.Fatal(err)
	}
}

func (s *scratchRepo) commitAll(message string) string {
	s.t.Helper()
	s.git("add", "-A")
	s.git("-c", "user.email=review@test.invalid", "-c", "user.name=Review Test", "commit", "-q", "-m", message)
	return s.git("rev-parse", "HEAD")
}

// newScratchRepo builds a repository with the admitted policy on main and a
// feature branch containing one reviewable change to subject.go.
func newScratchRepo(t *testing.T) *scratchRepo {
	t.Helper()
	root := repositoryRoot(t)
	directory := t.TempDir()
	if _, err := gitOutput(directory, "init", "-q", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	repo, err := openRepo(directory)
	if err != nil {
		t.Fatal(err)
	}
	scratch := &scratchRepo{t: t, repo: repo}
	for _, asset := range []string{policyPromptPath, policySchemaPath} {
		contents, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(asset)))
		if err != nil {
			t.Fatal(err)
		}
		scratch.writeFile(asset, string(contents))
	}
	scratch.writeFile("subject.go", "package subject\n")
	scratch.commitAll("base")
	scratch.git("checkout", "-qb", "feature")
	scratch.writeFile("subject.go", "package subject\n\nfunc Value() int { return 1 }\n")
	scratch.commitAll("change")
	return scratch
}

func newTestLoop(t *testing.T, scratch *scratchRepo, policy Policy) *loopContext {
	t.Helper()
	program, err := compileReviewProgram(policy)
	if err != nil {
		t.Fatal(err)
	}
	store := newFileStore(scratch.repo.GitDir, "feature", program.Identity())
	domain := &reviewDomain{repo: scratch.repo, store: store, policy: policy, baseRef: "main"}
	return &loopContext{
		repo:     scratch.repo,
		policy:   policy,
		program:  program,
		store:    store,
		domain:   domain,
		operator: reviewOperator{store: store},
		instance: "feature",
		baseRef:  "main",
	}
}

func testPolicy(t *testing.T, scratch *scratchRepo) Policy {
	t.Helper()
	policy, err := loadWorktreePolicy(scratch.repo.Root)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func correctReview() string {
	return `{
  "findings": [],
  "overall_correctness": "patch is correct",
  "overall_explanation": "No remaining actionable findings.",
  "overall_confidence_score": 0.95
}`
}

func incorrectReview(findings ...string) string {
	return fmt.Sprintf(`{
  "findings": [%s],
  "overall_correctness": "patch is incorrect",
  "overall_explanation": "Actionable findings remain.",
  "overall_confidence_score": 0.9
}`, strings.Join(findings, ","))
}

func finding(title string, priority int, path string, line int) string {
	return fmt.Sprintf(`{
    "title": %q,
    "body": "Invariant, failure mode, and regression test are described here in enough detail.",
    "confidence_score": 0.9,
    "priority": %d,
    "code_location": {
      "absolute_file_path": %q,
      "side": "RIGHT",
      "line_range": {"start": %d, "end": %d}
    }
  }`, title, priority, path, line, line)
}

// submit stages candidate bytes against the current head tree and drives one
// resolve+apply through the kernel runtime, returning the resolution and the
// committed receipt when the kernel prescribed a transition.
func submit(t *testing.T, loop *loopContext, candidate string) (kernel.Resolution, *kernel.Receipt, error) {
	t.Helper()
	head, err := loop.repo.headCommit()
	if err != nil {
		t.Fatal(err)
	}
	tree, err := loop.repo.reviewedTree(head)
	if err != nil {
		t.Fatal(err)
	}
	if err := loop.store.stageCandidate([]byte(candidate), tree); err != nil {
		t.Fatal(err)
	}
	runtime, err := loop.runtime()
	if err != nil {
		t.Fatal(err)
	}
	authority, err := loop.authority("test-actor", capabilitySubmit)
	if err != nil {
		t.Fatal(err)
	}
	request := kernel.ResolveRequest{InstanceID: loop.instance, Authority: authority}
	resolution, err := runtime.Resolve(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Decision.Kind != kernel.Prescribed || resolution.Prescription == nil {
		return resolution, nil, fmt.Errorf("refused: %s", resolution.Decision.Reason)
	}
	receipt, err := runtime.Apply(context.Background(), kernel.ApplyRequest{
		ResolveRequest: request,
		Prescription:   *resolution.Prescription,
	})
	if err != nil {
		return resolution, nil, err
	}
	return resolution, &receipt, nil
}

func mode(t *testing.T, loop *loopContext) string {
	t.Helper()
	state, err := loop.store.Load(context.Background(), loop.instance)
	if err != nil {
		t.Fatal(err)
	}
	return state.Mode
}

func TestReviewProgramControlLaw(t *testing.T) {
	scratch := newScratchRepo(t)
	policy := testPolicy(t, scratch)
	program, err := compileReviewProgram(policy)
	if err != nil {
		t.Fatal(err)
	}
	if program.InitialMode != modeUnreviewed {
		t.Fatalf("initial mode is %q", program.InitialMode)
	}
	if len(program.MarkedModes) != 1 || program.MarkedModes[0] != modeConverged {
		t.Fatalf("marked modes are %v; only convergence is a resting point", program.MarkedModes)
	}
	// Recompiling under a changed policy must change the program identity,
	// so prescriptions and sealed receipts can never survive a policy edit.
	changed := policy
	changed.MaxRounds++
	reprogram, err := compileReviewProgram(changed)
	if err != nil {
		t.Fatal(err)
	}
	if reprogram.Fingerprint == program.Fingerprint {
		t.Fatal("changing the convergence bound did not change the program identity")
	}
}

func TestCalibrationCoversMinedReviewHistory(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("testdata", "review_rounds.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		PullRequests []struct {
			PR     int `json:"pr"`
			Rounds []struct {
				Verdict string `json:"verdict"`
			} `json:"rounds"`
		} `json:"pull_requests"`
		Observed struct {
			MaxRounds int `json:"max_rounds"`
		} `json:"observed"`
	}
	if err := json.Unmarshal(contents, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.PullRequests) == 0 {
		t.Fatal("fixture carries no mined pull requests")
	}
	longest := 0
	for _, pullRequest := range fixture.PullRequests {
		if len(pullRequest.Rounds) > longest {
			longest = len(pullRequest.Rounds)
		}
	}
	if longest != fixture.Observed.MaxRounds {
		t.Fatalf("fixture summary says max %d rounds but the data holds %d", fixture.Observed.MaxRounds, longest)
	}
	if defaultMaxRounds <= fixture.Observed.MaxRounds {
		t.Fatalf("round bound %d does not cover the observed maximum of %d rounds", defaultMaxRounds, fixture.Observed.MaxRounds)
	}
}

func TestCandidateAdmissionIsSoundAndComplete(t *testing.T) {
	scratch := newScratchRepo(t)
	policy := testPolicy(t, scratch)
	head := scratch.git("rev-parse", "HEAD")
	mergeBase := scratch.git("merge-base", "main", "HEAD")
	diff, err := scratch.repo.pullRequestDiff(mergeBase, head)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := scratch.repo.reviewedTree(head)
	if err != nil {
		t.Fatal(err)
	}
	evaluate := func(candidate string) candidateSummary {
		return evaluateCandidate(policy, []byte(candidate), tree, scratch.repo.Root, diff)
	}

	// Completeness: a well-formed candidate anchored inside the diff is
	// admitted, and its measure follows the priority weights.
	admitted := evaluate(incorrectReview(finding("P1 on changed line", 1, "subject.go", 3)))
	if !admitted.Valid {
		t.Fatalf("well-formed candidate rejected: %v", admitted.InvalidReasons)
	}
	if admitted.Measure != policy.Weights[1] {
		t.Fatalf("measure %d does not follow the priority weights", admitted.Measure)
	}

	// Soundness: every out-of-contract candidate is refused with a reason.
	rejections := map[string]string{
		"not JSON at all":       "{",
		"schema verdict enum":   strings.Replace(correctReview(), "patch is correct", "looks fine", 1),
		"line outside diff":     incorrectReview(finding("outside", 1, "subject.go", 1)),
		"unknown file":          incorrectReview(finding("ghost", 1, "ghost.go", 3)),
		"path escapes the repo": incorrectReview(finding("escape", 1, "../outside.go", 3)),
		"absolute foreign path": incorrectReview(finding("foreign", 1, "/etc/hosts", 3)),
	}
	for name, candidate := range rejections {
		summary := evaluate(candidate)
		if summary.Valid {
			t.Errorf("%s: out-of-contract candidate was admitted", name)
		}
		if len(summary.InvalidReasons) == 0 {
			t.Errorf("%s: refusal carries no reason", name)
		}
	}

	// The wrong diff side is refused even when the line number exists.
	leftSide := strings.Replace(
		incorrectReview(finding("wrong side", 1, "subject.go", 3)), `"side": "RIGHT"`, `"side": "LEFT"`, 1)
	if evaluate(leftSide).Valid {
		t.Error("a finding anchored to the unchanged side was admitted")
	}
}

func TestStallLaw(t *testing.T) {
	policy := Policy{StallWindow: 3}
	cases := []struct {
		name     string
		recorded []int
		next     int
		stalled  bool
	}{
		{"first round never stalls", nil, 100, false},
		{"improvement resets", []int{100, 90}, 80, false},
		{"two flat submissions are tolerated", []int{100}, 100, false},
		{"three flat submissions stall", []int{100, 100}, 100, true},
		{"three non-improving submissions stall", []int{100, 110}, 120, true},
		{"an old stall does not count after progress", []int{100, 100, 100, 50}, 40, false},
	}
	for _, testCase := range cases {
		if got := stalled(policy, testCase.recorded, testCase.next); got != testCase.stalled {
			t.Errorf("%s: stalled=%v, want %v", testCase.name, got, testCase.stalled)
		}
	}
}

func TestLoopConvergesSealsAndVerifies(t *testing.T) {
	scratch := newScratchRepo(t)
	policy := testPolicy(t, scratch)
	loop := newTestLoop(t, scratch, policy)

	// Round 1: open findings are recorded and the loop stays open.
	if _, receipt, err := submit(t, loop, incorrectReview(finding("open", 1, "subject.go", 3))); err != nil {
		t.Fatal(err)
	} else if receipt.TransitionID != transitionRecord {
		t.Fatalf("round 1 committed %q", receipt.TransitionID)
	}
	if mode(t, loop) != modeFindingsOpen {
		t.Fatalf("mode is %q after recording findings", mode(t, loop))
	}

	// The fix lands as a commit; a stale re-submission of the old tree's
	// verdict must not converge the new tree (freshness).
	scratch.writeFile("subject.go", "package subject\n\nfunc Value() int { return 2 }\n")
	scratch.commitAll("fix")

	// Round 2: the corrected tree converges.
	if _, receipt, err := submit(t, loop, correctReview()); err != nil {
		t.Fatal(err)
	} else if receipt.TransitionID != transitionConverge {
		t.Fatalf("round 2 committed %q", receipt.TransitionID)
	}
	if mode(t, loop) != modeConverged {
		t.Fatalf("mode is %q after convergence", mode(t, loop))
	}

	// Seal, commit the receipt, and verify: committing the sealed receipt
	// must not invalidate its own tree binding.
	sealed, err := buildSealedReceipt(scratch.repo, loop.store, policy, loop.program, "main", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	receiptPath := filepath.Join(scratch.repo.receiptDirectoryPath(), "feature.receipt.json")
	if err := writeSealedReceipt(receiptPath, sealed); err != nil {
		t.Fatal(err)
	}
	scratch.commitAll("sealed review receipt")
	report := verifySealedReceipt(scratch.repo, sealed, receiptPath, "main", "HEAD")
	if !report.Verified {
		t.Fatalf("converged receipt did not verify: %v", report.Failures)
	}

	// Directory scan finds the same receipt for the head tree.
	found, foundPath, err := findReceiptForHead(scratch.repo, scratch.repo.receiptDirectoryPath(), "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if found.Fingerprint != sealed.Fingerprint || foundPath != receiptPath {
		t.Fatal("directory scan did not find the sealed receipt for the head tree")
	}
}

func TestStaleCandidateAndDirtyWorktreeAreRefused(t *testing.T) {
	scratch := newScratchRepo(t)
	policy := testPolicy(t, scratch)
	loop := newTestLoop(t, scratch, policy)

	// Stale: the candidate was produced for the previous tree.
	head, err := scratch.repo.headCommit()
	if err != nil {
		t.Fatal(err)
	}
	oldTree, err := scratch.repo.reviewedTree(head)
	if err != nil {
		t.Fatal(err)
	}
	if err := loop.store.stageCandidate([]byte(correctReview()), oldTree); err != nil {
		t.Fatal(err)
	}
	scratch.writeFile("subject.go", "package subject\n\nfunc Value() int { return 3 }\n")
	scratch.commitAll("moved on")
	runtime, err := loop.runtime()
	if err != nil {
		t.Fatal(err)
	}
	authority, err := loop.authority("test-actor", capabilitySubmit)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := runtime.Resolve(context.Background(), kernel.ResolveRequest{
		InstanceID: loop.instance, Authority: authority,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Decision.Kind == kernel.Prescribed {
		t.Fatal("a candidate produced for a different tree was prescribed")
	}

	// Dirty: tracked uncommitted changes refuse every submission.
	scratch.writeFile("subject.go", "package subject\n\nfunc Value() int { return 4 }\n")
	if _, _, err := submit(t, loop, correctReview()); err == nil {
		t.Fatal("a dirty worktree accepted a submission")
	}
	if mode(t, loop) != modeUnreviewed {
		t.Fatalf("refusals must not move the mode; mode is %q", mode(t, loop))
	}
}

func TestStallEscalatesAndHumanReopens(t *testing.T) {
	scratch := newScratchRepo(t)
	policy := testPolicy(t, scratch)
	loop := newTestLoop(t, scratch, policy)

	// Three submissions with non-decreasing measures: the third escalates
	// instead of recording another round.
	titles := []string{"first", "second", "third"}
	var last *kernel.Receipt
	for _, title := range titles {
		_, receipt, err := submit(t, loop, incorrectReview(finding(title, 1, "subject.go", 3)))
		if err != nil {
			t.Fatal(err)
		}
		last = receipt
	}
	if last.TransitionID != transitionEscalate {
		t.Fatalf("third non-improving submission committed %q, not escalation", last.TransitionID)
	}
	if mode(t, loop) != modeEscalated {
		t.Fatalf("mode is %q after escalation", mode(t, loop))
	}

	// A submit-capable actor cannot reopen; reopening requires the human
	// capability, and a fresh generation starts empty.
	runtime, err := loop.runtime()
	if err != nil {
		t.Fatal(err)
	}
	submitOnly, err := loop.authority("test-actor", capabilitySubmit)
	if err != nil {
		t.Fatal(err)
	}
	refused, err := runtime.Resolve(context.Background(), kernel.ResolveRequest{
		InstanceID: loop.instance, Authority: submitOnly, Requested: transitionReopen,
	})
	if err != nil {
		t.Fatal(err)
	}
	if refused.Decision.Kind == kernel.Prescribed {
		t.Fatal("reopen was prescribed without the human capability")
	}
	human, err := loop.authority("human-reviewer", capabilityHuman)
	if err != nil {
		t.Fatal(err)
	}
	request := kernel.ResolveRequest{InstanceID: loop.instance, Authority: human, Requested: transitionReopen}
	resolution, err := runtime.Resolve(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Decision.Kind != kernel.Prescribed || resolution.Prescription == nil {
		t.Fatalf("human reopen refused: %s", resolution.Decision.Reason)
	}
	if _, err := runtime.Apply(context.Background(), kernel.ApplyRequest{
		ResolveRequest: request, Prescription: *resolution.Prescription,
	}); err != nil {
		t.Fatal(err)
	}
	if mode(t, loop) != modeUnreviewed {
		t.Fatalf("mode is %q after reopen", mode(t, loop))
	}
	observed, err := loop.domain.observeValue()
	if err != nil {
		t.Fatal(err)
	}
	if observed.Generation != 2 || len(observed.Rounds) != 0 {
		t.Fatalf("reopen did not start a fresh empty generation: generation %d, %d rounds", observed.Generation, len(observed.Rounds))
	}
}

// converge drives a scratch repository to a sealed, committed, verified
// receipt and returns it with its path.
func converge(t *testing.T, scratch *scratchRepo, loop *loopContext) (SealedReceipt, string) {
	t.Helper()
	if _, _, err := submit(t, loop, correctReview()); err != nil {
		t.Fatal(err)
	}
	sealed, err := buildSealedReceipt(scratch.repo, loop.store, loop.policy, loop.program, "main", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(scratch.repo.receiptDirectoryPath(), "feature.receipt.json")
	if err := writeSealedReceipt(path, sealed); err != nil {
		t.Fatal(err)
	}
	scratch.commitAll("sealed review receipt")
	if report := verifySealedReceipt(scratch.repo, sealed, path, "main", "HEAD"); !report.Verified {
		t.Fatalf("baseline receipt did not verify: %v", report.Failures)
	}
	return sealed, path
}

func TestVerificationRejectsTamperingForgeryAndBypass(t *testing.T) {
	scratch := newScratchRepo(t)
	policy := testPolicy(t, scratch)
	loop := newTestLoop(t, scratch, policy)
	sealed, path := converge(t, scratch, loop)

	// Tampered content: any edit breaks the content identity.
	tampered := sealed
	tampered.Rounds = append([]journalRound(nil), sealed.Rounds...)
	tampered.Rounds[0].Verdict = verdictIncorrect
	if report := verifySealedReceipt(scratch.repo, tampered, path, "main", "HEAD"); report.Verified {
		t.Fatal("a tampered receipt verified")
	}

	// Forged verdict: rewriting the final review and re-fingerprinting the
	// envelope still fails, because the kernel receipt chain committed the
	// original candidate fingerprint.
	forged := sealed
	forged.FinalReview = json.RawMessage(strings.Replace(
		string(sealed.FinalReview), "patch is correct", "patch is incorrect", 1))
	refingered, err := forged.contentFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	forged.Fingerprint = refingered
	report := verifySealedReceipt(scratch.repo, forged, path, "main", "HEAD")
	if report.Verified {
		t.Fatal("a forged final review verified")
	}

	// Bypass: new commits after convergence leave the receipt bound to the
	// old tree, so the new head is not review-verified.
	scratch.writeFile("subject.go", "package subject\n\nfunc Value() int { return 99 }\n")
	scratch.commitAll("unreviewed change")
	if report := verifySealedReceipt(scratch.repo, sealed, path, "main", "HEAD"); report.Verified {
		t.Fatal("a receipt for an older tree verified a new head")
	}
	if _, _, err := findReceiptForHead(scratch.repo, scratch.repo.receiptDirectoryPath(), "HEAD"); err == nil {
		t.Fatal("directory scan bound an unreviewed head to an old receipt")
	}

	// Policy drift: a base whose admitted prompt differs refuses admission.
	scratch.git("checkout", "-q", "main")
	scratch.writeFile(policyPromptPath, "entirely different review policy\n")
	scratch.commitAll("policy change on main")
	scratch.git("checkout", "-q", "feature")
	if report := verifySealedReceipt(scratch.repo, sealed, path, "main", "HEAD"); report.Verified {
		t.Fatal("a receipt sealed under a superseded policy verified")
	}
}

func TestRecoveryClearsInterruptedEffect(t *testing.T) {
	scratch := newScratchRepo(t)
	policy := testPolicy(t, scratch)
	loop := newTestLoop(t, scratch, policy)

	// Simulate a crash between BeginEffect and CommitTransition: the store
	// holds an attempt revision with an active recovery state.
	state, err := loop.store.Load(context.Background(), loop.instance)
	if err != nil {
		t.Fatal(err)
	}
	attempt := state
	attempt.Revision = state.Revision + 1
	attempt.Recovery = &kernel.RecoveryState{
		PrescriptionID: "interrupted-prescription",
		TransitionID:   transitionRecord,
		Reason:         "simulated crash between effect and commit",
	}
	if err := loop.store.BeginEffect(context.Background(), state.Revision, attempt); err != nil {
		t.Fatal(err)
	}
	if err := loop.store.stageCandidate([]byte(correctReview()), "half-recorded"); err != nil {
		t.Fatal(err)
	}

	// Ordinary submission is refused while recovery is pending; the
	// declared recovery transition clears the staging and restores control.
	runtime, err := loop.runtime()
	if err != nil {
		t.Fatal(err)
	}
	recoverAuthority, err := loop.authority("test-actor", capabilityRecover)
	if err != nil {
		t.Fatal(err)
	}
	request := kernel.ResolveRequest{
		InstanceID: loop.instance, Authority: recoverAuthority, Requested: transitionRecover,
	}
	resolution, err := runtime.Resolve(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Decision.Kind != kernel.Prescribed || resolution.Prescription == nil {
		t.Fatalf("recovery refused: %s", resolution.Decision.Reason)
	}
	receipt, err := runtime.Apply(context.Background(), kernel.ApplyRequest{
		ResolveRequest: request, Prescription: *resolution.Prescription,
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.TransitionID != transitionRecover {
		t.Fatalf("recovery committed %q", receipt.TransitionID)
	}
	if mode(t, loop) != modeUnreviewed {
		t.Fatalf("mode is %q after recovery", mode(t, loop))
	}
	if _, _, staged, err := loop.store.loadStagedCandidate(); err != nil || staged {
		t.Fatalf("recovery left staging behind (staged=%v, err=%v)", staged, err)
	}
}

func TestShowDisplaysTheRecordedReviewWithoutResolving(t *testing.T) {
	scratch := newScratchRepo(t)
	policy := testPolicy(t, scratch)
	loop := newTestLoop(t, scratch, policy)
	if _, _, err := submit(t, loop, incorrectReview(finding("visible finding", 1, "subject.go", 3))); err != nil {
		t.Fatal(err)
	}

	capture := func(arguments ...string) (string, error) {
		t.Helper()
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		original := os.Stdout
		os.Stdout = writer
		runErr := run(append([]string{"show", "--repo", scratch.repo.Root, "--delivery", "feature"}, arguments...))
		os.Stdout = original
		writer.Close()
		output, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		return string(output), runErr
	}

	output, err := capture()
	if err != nil {
		t.Fatal(err)
	}
	var shown struct {
		Mode   string `json:"mode"`
		Round  *journalRound
		Review json.RawMessage `json:"review"`
	}
	if err := json.Unmarshal([]byte(output), &shown); err != nil {
		t.Fatalf("show output does not decode: %v", err)
	}
	if shown.Mode != modeFindingsOpen {
		t.Fatalf("show reports mode %q", shown.Mode)
	}
	if !strings.Contains(string(shown.Review), "visible finding") {
		t.Fatal("show did not display the recorded review findings")
	}
	if candidateFingerprint(shown.Review) == "" {
		t.Fatal("shown review is empty")
	}

	if _, err := capture("--round", "99"); err == nil {
		t.Fatal("show accepted a round that was never recorded")
	}
}

func TestCLIVerifyCommandExitsNonZeroWithoutReceipt(t *testing.T) {
	scratch := newScratchRepo(t)
	head := scratch.git("rev-parse", "HEAD")
	err := run([]string{"verify", "--repo", scratch.repo.Root, "--base", "main", "--head", head})
	if err == nil {
		t.Fatal("verify succeeded with no sealed receipt present")
	}
	if !strings.Contains(err.Error(), "no sealed review receipt") {
		t.Fatalf("failure does not tell the operator what to do: %v", err)
	}
}

func TestGitBinaryIsAvailableForThisSuite(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("this test suite requires git on PATH")
	}
}
