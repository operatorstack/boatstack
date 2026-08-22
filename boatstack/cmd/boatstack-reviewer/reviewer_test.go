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
	policy, err := loadRevisionPolicy(scratch.repo, "main")
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func reviewWith(verdict string, findings ...string) string {
	return fmt.Sprintf(`{
  "findings": [%s],
  "overall_correctness": %q,
  "overall_explanation": "Explanation of the verdict.",
  "overall_confidence_score": 0.9
}`, strings.Join(findings, ","), verdict)
}

func correctReview() string {
	return reviewWith(verdictCorrect)
}

func incorrectReview(findings ...string) string {
	return reviewWith(verdictIncorrect, findings...)
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
	// The blocking boundary is part of the convergence law: moving it must
	// also move the program identity.
	reblocked := policy
	reblocked.Blocking = [4]bool{true, true, true, false}
	reblockedProgram, err := compileReviewProgram(reblocked)
	if err != nil {
		t.Fatal(err)
	}
	if reblockedProgram.Fingerprint == program.Fingerprint {
		t.Fatal("changing the blocking boundary did not change the program identity")
	}
	// A policy where nothing blocks would converge on any valid candidate;
	// it must not validate.
	unblocked := policy
	unblocked.Blocking = [4]bool{}
	if err := unblocked.validate(); err == nil {
		t.Fatal("a policy with no blocking priority validated")
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

func TestConvergesWithResidualFindingsUnderEitherVerdict(t *testing.T) {
	// The blocking boundary decides convergence, not the verdict wording:
	// a review carrying only P2/P3 findings converges even when the
	// proposer wrote "patch is incorrect", and the residuals are recorded
	// with the round and travel into the sealed receipt.
	scratch := newScratchRepo(t)
	policy := testPolicy(t, scratch)
	loop := newTestLoop(t, scratch, policy)

	residuals := reviewWith(verdictIncorrect,
		finding("residual P2", 2, "subject.go", 3),
		finding("residual P3", 3, "subject.go", 3))
	_, receipt, err := submit(t, loop, residuals)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.TransitionID != transitionConverge {
		t.Fatalf("a residual-only review committed %q, not convergence", receipt.TransitionID)
	}
	if mode(t, loop) != modeConverged {
		t.Fatalf("mode is %q after residual-only convergence", mode(t, loop))
	}

	sealed, err := buildSealedReceipt(scratch.repo, loop.store, policy, loop.program, "main", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if report := verifyFullReceipt(scratch.repo, sealed, "", "main", "HEAD"); !report.Verified {
		t.Fatalf("residual-only converged receipt did not verify at seal time: %v", report.Failures)
	}
	final := sealed.Rounds[len(sealed.Rounds)-1]
	if final.BlockingMeasure != 0 {
		t.Fatalf("converged round records blocking measure %d", final.BlockingMeasure)
	}
	if final.Priorities != [4]int{0, 0, 1, 1} {
		t.Fatalf("residual priorities are not recorded in the sealed round: %v", final.Priorities)
	}
	if final.Measure != policy.Weights[2]+policy.Weights[3] {
		t.Fatalf("total measure %d does not carry the residual weights", final.Measure)
	}
}

func TestBlockingFindingRefusesConvergenceDespiteCorrectVerdict(t *testing.T) {
	// The inverse of the residual case: an open P1 keeps the loop running
	// even when the proposer wrote "patch is correct". The verdict wording
	// cannot converge past a blocking finding.
	scratch := newScratchRepo(t)
	policy := testPolicy(t, scratch)
	loop := newTestLoop(t, scratch, policy)

	optimistic := reviewWith(verdictCorrect, finding("open P1", 1, "subject.go", 3))
	_, receipt, err := submit(t, loop, optimistic)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.TransitionID != transitionRecord {
		t.Fatalf("a review with an open P1 committed %q, not a recorded round", receipt.TransitionID)
	}
	if mode(t, loop) != modeFindingsOpen {
		t.Fatalf("mode is %q with a blocking finding open", mode(t, loop))
	}
}

func TestIncorrectVerdictWithoutFindingsIsInvalid(t *testing.T) {
	// An empty assertion of incorrectness carries nothing to act on and
	// nothing to bind; it must not become a recorded round.
	scratch := newScratchRepo(t)
	policy := testPolicy(t, scratch)
	loop := newTestLoop(t, scratch, policy)

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
	summary := evaluateCandidate(policy, []byte(incorrectReview()), tree, scratch.repo.Root, diff)
	if summary.Valid {
		t.Fatal("an incorrect verdict with zero findings was admitted")
	}

	if _, _, err := submit(t, loop, incorrectReview()); err == nil {
		t.Fatal("an incorrect verdict with zero findings was prescribed")
	}
	if mode(t, loop) != modeUnreviewed {
		t.Fatalf("a refused candidate moved the mode to %q", mode(t, loop))
	}
}

func TestResidualChurnCannotMaskABlockingStall(t *testing.T) {
	// The stall law runs over the blocking measure. Three submissions keep
	// the same P1 open while the P2 count shrinks each round: the total
	// measure decreases, but the blocking measure is flat, so the third
	// submission escalates instead of recording another round.
	scratch := newScratchRepo(t)
	policy := testPolicy(t, scratch)
	loop := newTestLoop(t, scratch, policy)

	rounds := []string{
		reviewWith(verdictIncorrect,
			finding("stuck P1", 1, "subject.go", 3),
			finding("nit one", 2, "subject.go", 3),
			finding("nit two", 2, "subject.go", 3)),
		reviewWith(verdictIncorrect,
			finding("stuck P1", 1, "subject.go", 3),
			finding("nit one", 2, "subject.go", 3)),
		reviewWith(verdictIncorrect,
			finding("stuck P1", 1, "subject.go", 3)),
	}
	var last *kernel.Receipt
	for _, candidate := range rounds {
		_, receipt, err := submit(t, loop, candidate)
		if err != nil {
			t.Fatal(err)
		}
		last = receipt
	}
	if last.TransitionID != transitionEscalate {
		t.Fatalf("a flat blocking measure behind shrinking residuals committed %q, not escalation", last.TransitionID)
	}
	if mode(t, loop) != modeEscalated {
		t.Fatalf("mode is %q after the blocking stall", mode(t, loop))
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

	// Seal: the full receipt verifies at seal time; only the minimal
	// attestation is committed, and committing it must not invalidate its
	// own tree binding.
	sealed, err := buildSealedReceipt(scratch.repo, loop.store, policy, loop.program, "main", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if report := verifyFullReceipt(scratch.repo, sealed, "", "main", "HEAD"); !report.Verified {
		t.Fatalf("full receipt did not verify at seal time: %v", report.Failures)
	}
	receiptPath := filepath.Join(scratch.repo.receiptDirectoryPath(), "feature.receipt.json")
	if err := writeAttestation(receiptPath, attestationOf(sealed)); err != nil {
		t.Fatal(err)
	}
	scratch.commitAll("sealed review attestation")
	report := verifyAttestation(scratch.repo, attestationOf(sealed), receiptPath, "main", "HEAD")
	if !report.Verified {
		t.Fatalf("converged attestation did not verify: %v", report.Failures)
	}

	// The committed artifact carries exactly the two admitted facts.
	var raw map[string]json.RawMessage
	value, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(value, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 2 {
		t.Fatalf("the committed attestation carries %d fields, not the minimal 2: %v", len(raw), raw)
	}

	// Directory scan finds the same attestation for the head tree.
	found, foundPath, err := findReceiptForHead(scratch.repo, scratch.repo.receiptDirectoryPath(), "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if found != attestationOf(sealed) || foundPath != receiptPath {
		t.Fatal("directory scan did not find the attestation for the head tree")
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
// attestation and returns the full receipt with the attestation path.
func converge(t *testing.T, scratch *scratchRepo, loop *loopContext) (SealedReceipt, string) {
	t.Helper()
	if _, _, err := submit(t, loop, correctReview()); err != nil {
		t.Fatal(err)
	}
	sealed, err := buildSealedReceipt(scratch.repo, loop.store, loop.policy, loop.program, "main", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if report := verifyFullReceipt(scratch.repo, sealed, "", "main", "HEAD"); !report.Verified {
		t.Fatalf("baseline full receipt did not verify at seal time: %v", report.Failures)
	}
	path := filepath.Join(scratch.repo.receiptDirectoryPath(), "feature.receipt.json")
	if err := writeAttestation(path, attestationOf(sealed)); err != nil {
		t.Fatal(err)
	}
	scratch.commitAll("sealed review attestation")
	if report := verifyAttestation(scratch.repo, attestationOf(sealed), path, "main", "HEAD"); !report.Verified {
		t.Fatalf("baseline attestation did not verify: %v", report.Failures)
	}
	return sealed, path
}

func TestVerificationRejectsTamperingForgeryAndBypass(t *testing.T) {
	scratch := newScratchRepo(t)
	policy := testPolicy(t, scratch)
	loop := newTestLoop(t, scratch, policy)
	sealed, path := converge(t, scratch, loop)

	// Seal-time law — tampered content: any edit breaks the content
	// identity of the full receipt.
	tampered := sealed
	tampered.Rounds = append([]journalRound(nil), sealed.Rounds...)
	tampered.Rounds[0].Verdict = verdictIncorrect
	if report := verifyFullReceipt(scratch.repo, tampered, path, "main", "HEAD"); report.Verified {
		t.Fatal("a tampered receipt verified")
	}

	// Seal-time law — forged verdict: rewriting the final review and
	// re-fingerprinting the envelope still fails, because the kernel
	// receipt chain committed the original candidate fingerprint.
	forged := sealed
	forged.FinalReview = json.RawMessage(strings.Replace(
		string(sealed.FinalReview), "patch is correct", "patch is incorrect", 1))
	refingered, err := forged.contentFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	forged.Fingerprint = refingered
	if report := verifyFullReceipt(scratch.repo, forged, path, "main", "HEAD"); report.Verified {
		t.Fatal("a forged final review verified")
	}

	// CI law — attestation tampering: a mutated tree or fingerprint fails.
	wrongTree := attestationOf(sealed)
	wrongTree.ReviewedTree = strings.Repeat("0", 40)
	if report := verifyAttestation(scratch.repo, wrongTree, path, "main", "HEAD"); report.Verified {
		t.Fatal("an attestation for a different tree verified")
	}
	wrongProgram := attestationOf(sealed)
	wrongProgram.ProgramFingerprint = strings.Repeat("0", 64)
	if report := verifyAttestation(scratch.repo, wrongProgram, path, "main", "HEAD"); report.Verified {
		t.Fatal("an attestation under an unadmitted program verified")
	}

	// CI law — strictness: unknown fields, missing facts, and receipts of
	// the superseded full format are not the admitted artifact.
	padded := filepath.Join(scratch.repo.receiptDirectoryPath(), "padded.receipt.json")
	if err := os.WriteFile(padded, []byte(`{"reviewed_tree":"`+sealed.ReviewedTree+`","program_fingerprint":"`+sealed.Program.Fingerprint+`","note":"extra"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readAttestation(padded); err == nil {
		t.Fatal("an attestation with unknown fields decoded")
	}
	if err := os.WriteFile(padded, []byte(`{"reviewed_tree":"`+sealed.ReviewedTree+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readAttestation(padded); err == nil {
		t.Fatal("an attestation missing the program fingerprint decoded")
	}
	fullFormat := filepath.Join(scratch.repo.receiptDirectoryPath(), "old-format.receipt.json")
	if err := writeSealedReceipt(fullFormat, sealed); err != nil {
		t.Fatal(err)
	}
	if _, err := readAttestation(fullFormat); err == nil {
		t.Fatal("a superseded full-format receipt decoded as an attestation")
	}
	if err := os.Remove(padded); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fullFormat); err != nil {
		t.Fatal(err)
	}

	// Bypass: new commits after convergence leave the attestation bound to
	// the old tree, so the new head is not review-verified.
	scratch.writeFile("subject.go", "package subject\n\nfunc Value() int { return 99 }\n")
	scratch.commitAll("unreviewed change")
	if report := verifyAttestation(scratch.repo, attestationOf(sealed), path, "main", "HEAD"); report.Verified {
		t.Fatal("an attestation for an older tree verified a new head")
	}
	if _, _, err := findReceiptForHead(scratch.repo, scratch.repo.receiptDirectoryPath(), "HEAD"); err == nil {
		t.Fatal("directory scan bound an unreviewed head to an old attestation")
	}

	// Policy drift: a base whose admitted prompt differs recompiles to a
	// different program, so the attested fingerprint refuses.
	scratch.git("checkout", "-q", "main")
	scratch.writeFile(policyPromptPath, "entirely different review policy\n")
	scratch.commitAll("policy change on main")
	scratch.git("checkout", "-q", "feature")
	if report := verifyAttestation(scratch.repo, attestationOf(sealed), path, "main", "HEAD"); report.Verified {
		t.Fatal("an attestation sealed under a superseded policy verified")
	}
}

func TestPolicyChangingBranchSealsAVerifiableAttestation(t *testing.T) {
	// Regression for the policy-admission split: a branch that changes the
	// admitted policy assets must still seal an attestation CI can verify.
	// The loop admits the policy from the base revision — the same admission
	// verification performs — so the changed prompt governs only after
	// merge, and the sealed fingerprint recomputes identically from base.
	scratch := newScratchRepo(t)
	scratch.writeFile(policyPromptPath, "an entirely rewritten review policy for the next generation\n")
	scratch.commitAll("change the review policy on the branch")

	policy := testPolicy(t, scratch)
	worktreePrompt, err := os.ReadFile(filepath.Join(scratch.repo.Root, filepath.FromSlash(policyPromptPath)))
	if err != nil {
		t.Fatal(err)
	}
	if sha256Hex(worktreePrompt) == policy.PromptSHA256 {
		t.Fatal("the fixture did not actually diverge the worktree prompt from the base-admitted prompt")
	}
	if note := worktreePolicyNote(scratch.repo.Root, policy); note == "" {
		t.Fatal("the resolve instructions do not surface the policy drift")
	}

	loop := newTestLoop(t, scratch, policy)
	if _, receipt, err := submit(t, loop, correctReview()); err != nil {
		t.Fatal(err)
	} else if receipt.TransitionID != transitionConverge {
		t.Fatalf("the policy-changing branch committed %q", receipt.TransitionID)
	}
	sealed, err := buildSealedReceipt(scratch.repo, loop.store, policy, loop.program, "main", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if report := verifyFullReceipt(scratch.repo, sealed, "", "main", "HEAD"); !report.Verified {
		t.Fatalf("the full receipt did not verify at seal time: %v", report.Failures)
	}
	path := filepath.Join(scratch.repo.receiptDirectoryPath(), "feature.receipt.json")
	if err := writeAttestation(path, attestationOf(sealed)); err != nil {
		t.Fatal(err)
	}
	scratch.commitAll("sealed review attestation")
	if report := verifyAttestation(scratch.repo, attestationOf(sealed), path, "main", "HEAD"); !report.Verified {
		t.Fatalf("the attestation of a policy-changing branch did not verify against the base admission: %v", report.Failures)
	}

	// The inverse remains refused: an attestation fingerprinted under the
	// worktree (changed) policy is not the base-admitted program.
	worktreeSchema, err := os.ReadFile(filepath.Join(scratch.repo.Root, filepath.FromSlash(policySchemaPath)))
	if err != nil {
		t.Fatal(err)
	}
	worktreeProgram, err := compileReviewProgram(newPolicy(worktreePrompt, worktreeSchema))
	if err != nil {
		t.Fatal(err)
	}
	forged := attestationOf(sealed)
	forged.ProgramFingerprint = worktreeProgram.Fingerprint
	if report := verifyAttestation(scratch.repo, forged, path, "main", "HEAD"); report.Verified {
		t.Fatal("an attestation fingerprinted under the unadmitted worktree policy verified")
	}
}

func TestVerificationRejectsReceiptDeclaredBoundsDrift(t *testing.T) {
	// Regression for round 1, finding 1: a receipt sealed under weakened
	// convergence bounds must not verify against the admitted policy —
	// neither at seal time (bounds check) nor in CI, where the weakened
	// bounds change the program fingerprint the attestation carries.
	scratch := newScratchRepo(t)
	weakened := testPolicy(t, scratch)
	weakened.MaxRounds = 1000
	weakened.StallWindow = 500
	loop := newTestLoop(t, scratch, weakened)
	if _, _, err := submit(t, loop, correctReview()); err != nil {
		t.Fatal(err)
	}
	sealed, err := buildSealedReceipt(scratch.repo, loop.store, weakened, loop.program, "main", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(scratch.repo.receiptDirectoryPath(), "feature.receipt.json")
	if err := writeAttestation(path, attestationOf(sealed)); err != nil {
		t.Fatal(err)
	}
	scratch.commitAll("sealed under weakened bounds")

	fullReport := verifyFullReceipt(scratch.repo, sealed, "", "main", "HEAD")
	if fullReport.Verified {
		t.Fatal("a full receipt sealed under weakened convergence bounds verified at seal time")
	}
	boundsNamed := false
	for _, failure := range fullReport.Failures {
		if strings.Contains(failure, "convergence bounds") {
			boundsNamed = true
		}
	}
	if !boundsNamed {
		t.Fatalf("the seal-time failure does not name the bounds drift: %v", fullReport.Failures)
	}

	report := verifyAttestation(scratch.repo, attestationOf(sealed), path, "main", "HEAD")
	if report.Verified {
		t.Fatal("an attestation sealed under weakened convergence bounds verified")
	}
	programNamed := false
	for _, failure := range report.Failures {
		if strings.Contains(failure, "does not match the program recompiled") {
			programNamed = true
		}
	}
	if !programNamed {
		t.Fatalf("the CI failure does not name the program mismatch: %v", report.Failures)
	}
}

func TestResolveSurfacesTheDomainRefusalReason(t *testing.T) {
	// Regression for round 1, finding 2: an untargeted resolve must name the
	// exact domain condition, not only the kernel's generic reason.
	scratch := newScratchRepo(t)
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = writer
	runErr := run([]string{"resolve", "--repo", scratch.repo.Root, "--delivery", "feature", "--base", "main"})
	os.Stdout = original
	writer.Close()
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if runErr != nil {
		t.Fatal(runErr)
	}
	var resolved struct {
		Disposition dispositionView `json:"submission_disposition"`
	}
	if err := json.Unmarshal(output, &resolved); err != nil {
		t.Fatalf("resolve output does not decode: %v", err)
	}
	if resolved.Disposition.Admits != "" {
		t.Fatalf("nothing is staged but disposition admits %q", resolved.Disposition.Admits)
	}
	if !strings.Contains(resolved.Disposition.Reason, "no candidate review is staged") {
		t.Fatalf("disposition reason does not name the staging requirement: %q", resolved.Disposition.Reason)
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
		runErr := run(append([]string{"show", "--repo", scratch.repo.Root, "--delivery", "feature", "--base", "main"}, arguments...))
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
	if !strings.Contains(err.Error(), "no review attestation") {
		t.Fatalf("failure does not tell the operator what to do: %v", err)
	}
}

func TestGitBinaryIsAvailableForThisSuite(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("this test suite requires git on PATH")
	}
}
