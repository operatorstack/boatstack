package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/operatorstack/boatstack/boatstack/kernel"
)

const sealedReceiptSchemaVersion = 1

// SealedReceipt is the one artifact that travels with a pull request. It
// binds the exact reviewed (receipt-excluded) tree, the exact admitted
// policy, the exact program identity, the round trajectory, the final
// review bytes, and the complete kernel receipt chain. CI verifies it
// deterministically; no reviewer runs there.
type SealedReceipt struct {
	SchemaVersion int                    `json:"schema_version"`
	Instance      string                 `json:"instance"`
	Program       kernel.ProgramIdentity `json:"program"`
	Policy        Policy                 `json:"policy"`
	BaseRef       string                 `json:"base_ref"`
	MergeBase     string                 `json:"merge_base"`
	HeadCommit    string                 `json:"head_commit"`
	ReviewedTree  string                 `json:"reviewed_tree"`
	Generation    int                    `json:"generation"`
	Rounds        []journalRound         `json:"rounds"`
	FinalReview   json.RawMessage        `json:"final_review"`
	ControlState  sealedControlState     `json:"control_state"`
	Receipts      []kernel.Receipt       `json:"kernel_receipts"`
	Honesty       sealedHonesty          `json:"honesty"`
	SealedAt      time.Time              `json:"sealed_at"`
	Fingerprint   string                 `json:"fingerprint"`
}

type sealedControlState struct {
	Mode     string `json:"mode"`
	Revision uint64 `json:"revision"`
}

// sealedHonesty mirrors work-package verification: this artifact proves the
// declared review program ran to convergence over the exact bound tree. It
// never proves the review was semantically right, and never proves who or
// what actually performed the review.
type sealedHonesty struct {
	SemanticCorrectness string `json:"semantic_correctness"`
	OriginAuthenticity  string `json:"origin_authenticity"`
}

func (r SealedReceipt) contentFingerprint() (string, error) {
	identity := r
	identity.Fingerprint = ""
	encoded, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	return sha256Hex(encoded), nil
}

// buildSealedReceipt assembles the sealed receipt from committed state. It
// refuses unless the instance is converged and the converged round binds the
// exact current reviewed tree.
func buildSealedReceipt(repo *gitRepo, store *fileStore, policy Policy, program kernel.Program, baseRef string, now time.Time) (SealedReceipt, error) {
	document, err := store.loadDocument()
	if err != nil {
		return SealedReceipt{}, err
	}
	if document.State.Mode != modeConverged {
		return SealedReceipt{}, fmt.Errorf("instance %s is %q, not converged; nothing to seal", document.State.InstanceID, document.State.Mode)
	}
	if document.State.Program != program.Identity() {
		return SealedReceipt{}, fmt.Errorf("committed state belongs to program %s, not the active program %s; the admitted policy or law changed after convergence", document.State.Program.Fingerprint, program.Fingerprint)
	}
	journal, err := store.loadJournal()
	if err != nil {
		return SealedReceipt{}, err
	}
	rounds := journal.currentRounds()
	if len(rounds) == 0 {
		return SealedReceipt{}, fmt.Errorf("converged instance has no recorded rounds")
	}
	final := rounds[len(rounds)-1]
	if final.Verdict != verdictCorrect || final.Transition != transitionConverge {
		return SealedReceipt{}, fmt.Errorf("last recorded round is not the converged round")
	}
	head, err := repo.headCommit()
	if err != nil {
		return SealedReceipt{}, err
	}
	reviewedTree, err := repo.reviewedTree(head)
	if err != nil {
		return SealedReceipt{}, err
	}
	if reviewedTree != final.ReviewedTree {
		return SealedReceipt{}, fmt.Errorf("current reviewed tree %s differs from the converged round's tree %s; re-run the review loop", reviewedTree, final.ReviewedTree)
	}
	finalReview, err := store.roundBytes(final.CandidateFingerprint)
	if err != nil {
		return SealedReceipt{}, fmt.Errorf("converged review bytes are unavailable: %w", err)
	}
	receipt := SealedReceipt{
		SchemaVersion: sealedReceiptSchemaVersion,
		Instance:      document.State.InstanceID,
		Program:       program.Identity(),
		Policy:        policy,
		BaseRef:       baseRef,
		MergeBase:     final.MergeBase,
		HeadCommit:    final.HeadCommit,
		ReviewedTree:  final.ReviewedTree,
		Generation:    journal.Generation,
		Rounds:        rounds,
		FinalReview:   json.RawMessage(finalReview),
		ControlState:  sealedControlState{Mode: document.State.Mode, Revision: document.State.Revision},
		Receipts:      document.Receipts,
		Honesty: sealedHonesty{
			SemanticCorrectness: "not-evaluated",
			OriginAuthenticity:  "not-proven",
		},
		SealedAt: now.UTC(),
	}
	receipt.Fingerprint, err = receipt.contentFingerprint()
	if err != nil {
		return SealedReceipt{}, err
	}
	return receipt, nil
}

func writeSealedReceipt(path string, receipt SealedReceipt) error {
	encoded, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(encoded, '\n'))
}

func readSealedReceipt(path string) (SealedReceipt, error) {
	value, err := os.ReadFile(path)
	if err != nil {
		return SealedReceipt{}, err
	}
	var receipt SealedReceipt
	if err := json.Unmarshal(value, &receipt); err != nil {
		return SealedReceipt{}, fmt.Errorf("sealed receipt %s does not decode: %w", path, err)
	}
	return receipt, nil
}

// verificationReport is the deterministic CI answer.
type verificationReport struct {
	Verified    bool     `json:"verified"`
	ReceiptPath string   `json:"receipt_path,omitempty"`
	Instance    string   `json:"instance,omitempty"`
	Program     string   `json:"program_fingerprint,omitempty"`
	Checks      []string `json:"checks"`
	Failures    []string `json:"failures"`
	Warnings    []string `json:"warnings"`
}

// verifySealedReceipt is the CI-side control law:
//
//	a pull request head is review-verified only when a sealed receipt
//	binds its exact receipt-excluded tree, under the review program
//	whose policy assets are admitted at the pull request base revision,
//	with an integral kernel receipt chain ending in convergence.
func verifySealedReceipt(repo *gitRepo, receipt SealedReceipt, receiptPath, baseRevision, headRevision string) verificationReport {
	report := verificationReport{
		ReceiptPath: receiptPath,
		Instance:    receipt.Instance,
		Program:     receipt.Program.Fingerprint,
		Checks:      []string{},
		Failures:    []string{},
		Warnings:    []string{},
	}
	pass := func(check string) { report.Checks = append(report.Checks, check) }
	fail := func(format string, args ...any) {
		report.Failures = append(report.Failures, fmt.Sprintf(format, args...))
	}

	if receipt.SchemaVersion != sealedReceiptSchemaVersion {
		fail("sealed receipt schema version %d is unsupported", receipt.SchemaVersion)
		return report
	}
	if fingerprint, err := receipt.contentFingerprint(); err != nil || fingerprint != receipt.Fingerprint {
		fail("sealed receipt content fingerprint does not identify its exact content")
		return report
	}
	pass("sealed receipt content identity is exact")

	// Policy admission: the assets named by the receipt must hash-match the
	// assets committed at the pull request base revision.
	basePolicy, err := loadRevisionPolicy(repo, baseRevision)
	if err != nil {
		fail("base revision policy admission failed: %v", err)
		return report
	}
	if receipt.Policy.PromptSHA256 != basePolicy.PromptSHA256 || receipt.Policy.SchemaSHA256 != basePolicy.SchemaSHA256 {
		fail("sealed receipt policy assets differ from the base-revision admitted policy (prompt %s vs %s, schema %s vs %s)",
			receipt.Policy.PromptSHA256, basePolicy.PromptSHA256, receipt.Policy.SchemaSHA256, basePolicy.SchemaSHA256)
	} else {
		pass("policy assets match the base-revision admitted policy")
	}

	// Program identity: recompile the review program from the admitted
	// policy bytes plus the receipt's declared bounds under this binary's
	// transition law.
	admittedPolicy := basePolicy
	admittedPolicy.MaxRounds = receipt.Policy.MaxRounds
	admittedPolicy.StallWindow = receipt.Policy.StallWindow
	admittedPolicy.Weights = receipt.Policy.Weights
	program, err := compileReviewProgram(admittedPolicy)
	if err != nil {
		fail("admitted review program does not compile: %v", err)
		return report
	}
	if program.Fingerprint != receipt.Program.Fingerprint {
		fail("sealed receipt program fingerprint %s does not match the admitted review program %s", receipt.Program.Fingerprint, program.Fingerprint)
	} else {
		pass("program fingerprint matches the admitted policy and transition law")
	}

	// Tree binding: the pull request head, receipts excluded, must be the
	// exact tree the converged review bound.
	head, err := repo.revParse(headRevision)
	if err != nil {
		fail("head revision %s is unavailable: %v", headRevision, err)
		return report
	}
	reviewedTree, err := repo.reviewedTree(head)
	if err != nil {
		fail("reviewed tree of %s is unavailable: %v", head, err)
		return report
	}
	if reviewedTree != receipt.ReviewedTree {
		fail("sealed receipt binds tree %s but the head reviewed tree is %s; the reviewed content changed after convergence", receipt.ReviewedTree, reviewedTree)
	} else {
		pass("sealed receipt binds the exact head reviewed tree")
	}

	// Kernel receipt chain integrity.
	if len(receipt.Receipts) == 0 {
		fail("sealed receipt carries no kernel receipts")
		return report
	}
	var previousResult uint64
	for index, committed := range receipt.Receipts {
		if err := committed.Validate(); err != nil {
			fail("kernel receipt %d is invalid: %v", index, err)
			return report
		}
		if committed.InstanceID != receipt.Instance {
			fail("kernel receipt %d belongs to instance %q, not %q", index, committed.InstanceID, receipt.Instance)
		}
		if committed.Program.Fingerprint != receipt.Program.Fingerprint {
			fail("kernel receipt %d was committed under program %s, not %s", index, committed.Program.Fingerprint, receipt.Program.Fingerprint)
		}
		if index == 0 {
			previousResult = committed.ResultStateRevision
			continue
		}
		if committed.PriorStateRevision != previousResult {
			fail("kernel receipt %d does not chain from the previous committed revision", index)
		}
		previousResult = committed.ResultStateRevision
	}
	last := receipt.Receipts[len(receipt.Receipts)-1]
	if last.TransitionID != transitionConverge {
		fail("the final kernel receipt committed %q, not the converge transition", last.TransitionID)
	}
	if previousResult != receipt.ControlState.Revision || receipt.ControlState.Mode != modeConverged {
		fail("sealed control state does not match the committed receipt chain")
	}
	if len(report.Failures) == 0 {
		pass("kernel receipt chain is integral and ends in convergence")
	}

	// Round trajectory: each recorded round must be committed by exactly one
	// round-bearing kernel receipt with the same candidate fingerprint, in
	// order, and the final round must be the convergence.
	var roundReceipts []kernel.Receipt
	for _, committed := range receipt.Receipts {
		switch committed.TransitionID {
		case transitionConverge, transitionRecord, transitionEscalate:
			roundReceipts = append(roundReceipts, committed)
		}
	}
	if len(roundReceipts) < len(receipt.Rounds) {
		fail("%d rounds are recorded but only %d round-bearing kernel receipts exist", len(receipt.Rounds), len(roundReceipts))
	} else {
		// The receipts for the sealed generation are the trailing ones;
		// earlier generations may also have committed rounds.
		tail := roundReceipts[len(roundReceipts)-len(receipt.Rounds):]
		matched := true
		for index, round := range receipt.Rounds {
			committed := tail[index]
			if committed.TransitionID != round.Transition ||
				len(committed.Effects) != 1 ||
				committed.Effects[0].Fingerprint != round.CandidateFingerprint {
				fail("round %d (%s) is not committed by its kernel receipt", index+1, round.CandidateFingerprint)
				matched = false
			}
		}
		if matched {
			pass("every recorded round is committed by a matching kernel receipt")
		}
	}
	if len(receipt.Rounds) == 0 {
		fail("sealed receipt records no rounds")
		return report
	}
	finalRound := receipt.Rounds[len(receipt.Rounds)-1]
	if finalRound.Transition != transitionConverge || finalRound.Verdict != verdictCorrect {
		fail("the final recorded round is not a convergence")
	}
	if finalRound.ReviewedTree != receipt.ReviewedTree {
		fail("the converged round bound tree %s, not the sealed tree %s", finalRound.ReviewedTree, receipt.ReviewedTree)
	}

	// Final review bytes: exact hash correspondence and verdict recomputed
	// from the bytes themselves, validated against the admitted schema.
	if candidateFingerprint(receipt.FinalReview) != finalRound.CandidateFingerprint {
		fail("final review bytes do not hash to the converged round's candidate fingerprint")
	} else {
		mergeBase, mergeErr := repo.mergeBase(baseRevision, head)
		diffBase := receipt.MergeBase
		if mergeErr == nil && mergeBase != receipt.MergeBase {
			report.Warnings = append(report.Warnings, fmt.Sprintf(
				"the review used merge base %s but the pull request merge base is %s; the bound tree is identical, the diff basis moved", receipt.MergeBase, mergeBase))
		}
		diff, diffErr := repo.pullRequestDiff(diffBase, head)
		if diffErr != nil {
			report.Warnings = append(report.Warnings, "the recorded diff basis is not reachable here; anchor revalidation skipped")
			diff = ""
		}
		summary := evaluateCandidate(admittedPolicy, receipt.FinalReview, receipt.ReviewedTree, repo.Root, diff)
		if !summary.Valid && diffErr == nil {
			fail("final review does not revalidate against the admitted schema and diff: %s", strings.Join(summary.InvalidReasons, "; "))
		} else if summary.Verdict != verdictCorrect {
			fail("final review verdict is %q, not %q", summary.Verdict, verdictCorrect)
		} else {
			pass("final review bytes revalidate and accept the patch")
		}
	}

	report.Verified = len(report.Failures) == 0
	return report
}

// findReceiptForHead scans a receipt directory for the sealed receipt that
// binds the exact head reviewed tree.
func findReceiptForHead(repo *gitRepo, directory, headRevision string) (SealedReceipt, string, error) {
	head, err := repo.revParse(headRevision)
	if err != nil {
		return SealedReceipt{}, "", err
	}
	reviewedTree, err := repo.reviewedTree(head)
	if err != nil {
		return SealedReceipt{}, "", err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return SealedReceipt{}, "", fmt.Errorf("no sealed review receipt binds reviewed tree %s: %s does not exist; run the local review loop and commit the sealed receipt", reviewedTree, directory)
		}
		return SealedReceipt{}, "", err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".receipt.json") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(directory, name)
		receipt, err := readSealedReceipt(path)
		if err != nil {
			continue
		}
		if receipt.ReviewedTree == reviewedTree {
			return receipt, path, nil
		}
	}
	return SealedReceipt{}, "", fmt.Errorf("no sealed review receipt in %s binds reviewed tree %s; run the local review loop to convergence, seal, and commit the receipt", directory, reviewedTree)
}
