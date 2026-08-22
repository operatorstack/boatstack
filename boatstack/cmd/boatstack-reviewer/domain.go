package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/operatorstack/boatstack/boatstack/kernel"
)

// observationValue is the canonical domain observation. Everything the
// relation needs to decide admissibility is recomputed here from the exact
// repository state, the exact staged candidate bytes, and the journal — so
// any commit, candidate change, or journal advance changes the observation
// fingerprint and stales pending prescriptions before their effect.
type observationValue struct {
	Instance         string            `json:"instance"`
	BaseRef          string            `json:"base_ref"`
	MergeBase        string            `json:"merge_base"`
	HeadCommit       string            `json:"head_commit"`
	ReviewedTree     string            `json:"reviewed_tree"`
	WorktreeDirty    bool              `json:"worktree_dirty"`
	DirtyFingerprint string            `json:"dirty_fingerprint,omitempty"`
	Generation       int               `json:"generation"`
	Candidate        *candidateSummary `json:"candidate,omitempty"`
	Rounds           []journalRound    `json:"rounds"`
}

// roundBlockingMeasures projects the recorded rounds onto the convergence
// law's driving quantity: the blocking measure. Round and stall bounds are
// computed over this sequence, so residual (non-blocking) findings can
// neither extend nor cut short a review generation.
func (v observationValue) roundBlockingMeasures() []int {
	measures := make([]int, 0, len(v.Rounds))
	for _, round := range v.Rounds {
		measures = append(measures, round.BlockingMeasure)
	}
	return measures
}

type reviewDomain struct {
	repo    *gitRepo
	store   *fileStore
	policy  Policy
	baseRef string
}

func (d *reviewDomain) observeValue() (observationValue, error) {
	head, err := d.repo.headCommit()
	if err != nil {
		return observationValue{}, err
	}
	mergeBase, err := d.repo.mergeBase(d.baseRef, head)
	if err != nil {
		return observationValue{}, err
	}
	reviewedTree, err := d.repo.reviewedTree(head)
	if err != nil {
		return observationValue{}, err
	}
	dirty, dirtyFingerprint, err := d.repo.worktreeStatus()
	if err != nil {
		return observationValue{}, err
	}
	journal, err := d.store.loadJournal()
	if err != nil {
		return observationValue{}, err
	}
	value := observationValue{
		Instance:         d.store.initial.InstanceID,
		BaseRef:          d.baseRef,
		MergeBase:        mergeBase,
		HeadCommit:       head,
		ReviewedTree:     reviewedTree,
		WorktreeDirty:    dirty,
		DirtyFingerprint: dirtyFingerprint,
		Generation:       journal.Generation,
		Rounds:           journal.currentRounds(),
	}
	if value.Rounds == nil {
		value.Rounds = []journalRound{}
	}
	candidateBytes, meta, staged, err := d.store.loadStagedCandidate()
	if err != nil {
		return observationValue{}, err
	}
	if staged {
		diff, diffErr := d.repo.pullRequestDiff(mergeBase, head)
		if diffErr != nil {
			return observationValue{}, diffErr
		}
		summary := evaluateCandidate(d.policy, candidateBytes, meta.ReviewedTree, d.repo.Root, diff)
		value.Candidate = &summary
	}
	return value, nil
}

func (d *reviewDomain) Observe(context.Context, string) (kernel.Observation, error) {
	value, err := d.observeValue()
	if err != nil {
		return kernel.Observation{}, err
	}
	return kernel.NewObservation(value)
}

// Admissible is the domain half of the canonical relation. Resolve and apply
// share it; nothing here mutates anything.
func (d *reviewDomain) Admissible(_ context.Context, evaluation kernel.Evaluation) (bool, string, error) {
	var observed observationValue
	if err := json.Unmarshal(evaluation.Observation.Value, &observed); err != nil {
		return false, "", err
	}
	switch evaluation.Transition.Operation {
	case transitionConverge, transitionRecord, transitionEscalate:
		allowed, reason := submissionDisposition(d.policy, observed)
		return allowed == evaluation.Transition.Operation, reason, nil
	case transitionReopen:
		return true, "reopening starts a fresh review generation", nil
	case transitionRecover:
		// A declared recovery transition stays admissible while its
		// recovery state is active, including after a partial cleanup.
		return evaluation.State.Recovery != nil, "recovery requires an active recovery state", nil
	default:
		return false, "unknown review operation", nil
	}
}

// submissionDisposition decides which single submission transition the
// current observation admits, with the refusal reason when none applies.
// Convergence, recording, and escalation are mutually exclusive by
// construction, so the relation can never reach an ambiguity frontier.
func submissionDisposition(policy Policy, observed observationValue) (string, string) {
	if observed.Candidate == nil {
		return "", "no candidate review is staged; run submit with --findings"
	}
	if observed.WorktreeDirty {
		return "", "the worktree has uncommitted tracked changes; a review binds only a committed tree"
	}
	candidate := *observed.Candidate
	if candidate.ReviewedTree != observed.ReviewedTree {
		return "", "the staged candidate was produced for a different reviewed tree; re-review the current commit"
	}
	if !candidate.Valid {
		reasons := "candidate is invalid"
		for _, reason := range candidate.InvalidReasons {
			reasons += "; " + reason
		}
		return "", reasons
	}
	// Convergence is deterministic on the blocking boundary: zero open
	// blocking findings converges regardless of the verdict wording, and
	// any open blocking finding keeps the loop running regardless of it.
	// The verdict and residual (non-blocking) findings are recorded data.
	if candidate.BlockingMeasure == 0 {
		if residuals := residualCount(policy, candidate.Priorities); residuals > 0 {
			return transitionConverge, fmt.Sprintf("no blocking findings remain; %d residual non-blocking findings are recorded", residuals)
		}
		return transitionConverge, "no blocking findings remain"
	}
	rounds := observed.roundBlockingMeasures()
	if len(rounds) >= policy.MaxRounds {
		return transitionEscalate, fmt.Sprintf("round bound %d is exhausted", policy.MaxRounds)
	}
	if stalled(policy, rounds, candidate.BlockingMeasure) {
		return transitionEscalate, fmt.Sprintf("blocking measure has not decreased for %d consecutive submissions", policy.StallWindow)
	}
	return transitionRecord, "candidate records open blocking findings with a decreasing blocking measure"
}

// residualCount counts findings in non-blocking priority classes.
func residualCount(policy Policy, priorities [4]int) int {
	count := 0
	for priority, findings := range priorities {
		if !policy.Blocking[priority] {
			count += findings
		}
	}
	return count
}

// reviewOperator applies the one admitted operation. It receives only
// admitted operations from the kernel; the candidate it archives is the
// exact staged bytes the observation was computed from.
type reviewOperator struct {
	store *fileStore
}

func (o reviewOperator) Execute(_ context.Context, operation kernel.Operation) (kernel.Effect, error) {
	var observed observationValue
	if err := json.Unmarshal(operation.Observation.Value, &observed); err != nil {
		return kernel.Effect{}, err
	}
	switch operation.Transition.Operation {
	case transitionConverge, transitionRecord, transitionEscalate:
		if observed.Candidate == nil {
			return kernel.Effect{}, fmt.Errorf("no staged candidate to archive")
		}
		candidateBytes, meta, staged, err := o.store.loadStagedCandidate()
		if err != nil {
			return kernel.Effect{}, err
		}
		if !staged || meta.Fingerprint != observed.Candidate.Fingerprint {
			return kernel.Effect{}, fmt.Errorf("staged candidate changed after resolution")
		}
		round := journalRound{
			CandidateFingerprint: observed.Candidate.Fingerprint,
			ReviewedTree:         observed.ReviewedTree,
			HeadCommit:           observed.HeadCommit,
			MergeBase:            observed.MergeBase,
			Verdict:              observed.Candidate.Verdict,
			Measure:              observed.Candidate.Measure,
			BlockingMeasure:      observed.Candidate.BlockingMeasure,
			FindingCount:         observed.Candidate.FindingCount,
			Priorities:           observed.Candidate.Priorities,
			Transition:           operation.Transition.ID,
		}
		if err := o.store.archiveRound(candidateBytes, round); err != nil {
			return kernel.Effect{}, err
		}
		return kernel.Effect{Facts: []kernel.EffectFact{{
			Facet:       facetRound,
			Operation:   operation.Transition.Operation,
			Fingerprint: observed.Candidate.Fingerprint,
		}}}, nil
	case transitionReopen:
		if err := o.store.nextGeneration(); err != nil {
			return kernel.Effect{}, err
		}
		journal, err := o.store.loadJournal()
		if err != nil {
			return kernel.Effect{}, err
		}
		return kernel.Effect{Facts: []kernel.EffectFact{{
			Facet:       facetRound,
			Operation:   operation.Transition.Operation,
			Fingerprint: fmt.Sprintf("generation-%d", journal.Generation),
		}}}, nil
	case transitionRecover:
		if err := o.store.clearStagedCandidate(); err != nil {
			return kernel.Effect{}, err
		}
		return kernel.Effect{Facts: []kernel.EffectFact{{
			Facet:       facetRound,
			Operation:   operation.Transition.Operation,
			Fingerprint: "staging-cleared",
		}}}, nil
	default:
		return kernel.Effect{}, fmt.Errorf("unknown review operation %q", operation.Transition.Operation)
	}
}

// Verify checks the fresh post-effect observation against the transition's
// declared postcondition. The operator's own summary is never trusted.
func (d *reviewDomain) Verify(_ context.Context, evaluation kernel.Evaluation, effect kernel.Effect, target kernel.Observation) error {
	var before, after observationValue
	if err := json.Unmarshal(evaluation.Observation.Value, &before); err != nil {
		return err
	}
	if err := json.Unmarshal(target.Value, &after); err != nil {
		return err
	}
	switch evaluation.Transition.Operation {
	case transitionConverge, transitionRecord, transitionEscalate:
		if before.Candidate == nil {
			return fmt.Errorf("no candidate was under review")
		}
		if after.Candidate != nil {
			return fmt.Errorf("staged candidate survived its own admission")
		}
		if len(after.Rounds) != len(before.Rounds)+1 {
			return fmt.Errorf("exactly one round must be recorded; journal grew from %d to %d", len(before.Rounds), len(after.Rounds))
		}
		recorded := after.Rounds[len(after.Rounds)-1]
		if recorded.CandidateFingerprint != before.Candidate.Fingerprint ||
			recorded.ReviewedTree != before.ReviewedTree ||
			recorded.Verdict != before.Candidate.Verdict ||
			recorded.Measure != before.Candidate.Measure ||
			recorded.BlockingMeasure != before.Candidate.BlockingMeasure ||
			recorded.Transition != evaluation.Transition.ID {
			return fmt.Errorf("recorded round does not match the admitted candidate")
		}
		archived, err := d.store.roundBytes(recorded.CandidateFingerprint)
		if err != nil {
			return fmt.Errorf("archived candidate is unavailable: %w", err)
		}
		if candidateFingerprint(archived) != recorded.CandidateFingerprint {
			return fmt.Errorf("archived candidate bytes do not match the recorded fingerprint")
		}
		if len(effect.Facts) != 1 || effect.Facts[0].Fingerprint != recorded.CandidateFingerprint {
			return fmt.Errorf("effect facts do not identify the archived candidate")
		}
		return nil
	case transitionReopen:
		if after.Generation != before.Generation+1 {
			return fmt.Errorf("reopen must advance exactly one generation")
		}
		if len(after.Rounds) != 0 {
			return fmt.Errorf("a fresh generation must start with no rounds")
		}
		if after.Candidate != nil {
			return fmt.Errorf("reopen must clear any staged candidate")
		}
		return nil
	case transitionRecover:
		if after.Candidate != nil {
			return fmt.Errorf("recovery must clear the staged candidate")
		}
		return nil
	default:
		return fmt.Errorf("unknown review operation %q", evaluation.Transition.Operation)
	}
}
