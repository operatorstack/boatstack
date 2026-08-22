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

// committedAttestation is the one artifact that travels with a pull request,
// reduced to the minimal facts CI cannot re-derive: which receipt-excluded
// tree the converged review bound, and under which program it converged.
// Everything else — policy hashes, convergence bounds, weights, transition
// law — is already inside the program fingerprint, which the verifier
// recompiles from the base-revision admitted policy. The full sealed
// receipt (round trajectory, kernel receipt chain, final review bytes)
// stays in the local review store and never enters a commit.
type committedAttestation struct {
	ReviewedTree       string `json:"reviewed_tree"`
	ProgramFingerprint string `json:"program_fingerprint"`
}

// SealedReceipt is the full local sealing artifact. It binds the exact
// reviewed (receipt-excluded) tree, the exact admitted policy, the exact
// program identity, the round trajectory, the final review bytes, and the
// complete kernel receipt chain. It is verified in full at seal time and
// archived in the local store; only the committedAttestation travels with
// the pull request.
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
	if final.Transition != transitionConverge || final.BlockingMeasure != 0 {
		return SealedReceipt{}, fmt.Errorf("last recorded round is not a converged round with zero blocking findings")
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

func attestationOf(receipt SealedReceipt) committedAttestation {
	return committedAttestation{
		ReviewedTree:       receipt.ReviewedTree,
		ProgramFingerprint: receipt.Program.Fingerprint,
	}
}

func writeAttestation(path string, attestation committedAttestation) error {
	encoded, err := json.MarshalIndent(attestation, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(encoded, '\n'))
}

// readAttestation decodes strictly: an attestation with unknown fields or
// missing facts is not the admitted artifact and must not verify.
func readAttestation(path string) (committedAttestation, error) {
	value, err := os.ReadFile(path)
	if err != nil {
		return committedAttestation{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(value)))
	decoder.DisallowUnknownFields()
	var attestation committedAttestation
	if err := decoder.Decode(&attestation); err != nil {
		return committedAttestation{}, fmt.Errorf("review attestation %s does not decode: %w", path, err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err == nil {
		return committedAttestation{}, fmt.Errorf("review attestation %s carries trailing content", path)
	}
	if attestation.ReviewedTree == "" || attestation.ProgramFingerprint == "" {
		return committedAttestation{}, fmt.Errorf("review attestation %s is missing required facts", path)
	}
	return attestation, nil
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

// verifyAttestation is the CI-side control law:
//
//	a pull request head is review-verified only when a committed
//	attestation binds its exact receipt-excluded tree under the review
//	program recompiled from the policy admitted at the pull request base
//	revision.
//
// The attestation carries nothing else, so there is nothing else to check
// here: the program fingerprint already hashes the policy assets, the
// convergence bounds, the weights, and the transition law. The round
// trajectory, kernel receipt chain, and final review bytes were verified in
// full at seal time and stay in the local review store.
func verifyAttestation(repo *gitRepo, attestation committedAttestation, receiptPath, baseRevision, headRevision string) verificationReport {
	report := verificationReport{
		ReceiptPath: receiptPath,
		Program:     attestation.ProgramFingerprint,
		Checks:      []string{},
		Failures:    []string{},
		Warnings:    []string{},
	}
	pass := func(check string) { report.Checks = append(report.Checks, check) }
	fail := func(format string, args ...any) {
		report.Failures = append(report.Failures, fmt.Sprintf(format, args...))
	}

	// Program identity: recompile the review program from the policy
	// admitted at the pull request base revision under this binary's
	// transition law. The attestation's fingerprint is never trusted as a
	// description of anything; it must equal the recompiled identity.
	basePolicy, err := loadRevisionPolicy(repo, baseRevision)
	if err != nil {
		fail("base revision policy admission failed: %v", err)
		return report
	}
	program, err := compileReviewProgram(basePolicy)
	if err != nil {
		fail("admitted review program does not compile: %v", err)
		return report
	}
	if program.Fingerprint != attestation.ProgramFingerprint {
		fail("attested program fingerprint %s does not match the program recompiled from the base-revision admitted policy %s", attestation.ProgramFingerprint, program.Fingerprint)
	} else {
		pass("attested program matches the base-revision admitted policy and transition law")
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
	if reviewedTree != attestation.ReviewedTree {
		fail("attestation binds tree %s but the head reviewed tree is %s; the reviewed content changed after convergence", attestation.ReviewedTree, reviewedTree)
	} else {
		pass("attestation binds the exact head reviewed tree")
	}

	report.Verified = len(report.Failures) == 0
	return report
}

// verifyFullReceipt is the seal-time control law over the full local
// receipt:
//
//	a converged loop seals only when the receipt binds the exact
//	receipt-excluded head tree, under the review program whose policy
//	assets are admitted at the base revision, with an integral kernel
//	receipt chain ending in convergence and final review bytes that
//	revalidate.
func verifyFullReceipt(repo *gitRepo, receipt SealedReceipt, receiptPath, baseRevision, headRevision string) verificationReport {
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
	// policy bytes under this binary's transition law and compiled
	// convergence bounds. The receipt's declared bounds are never trusted:
	// a receipt sealed under weakened bounds or weights must not verify.
	admittedPolicy := basePolicy
	if receipt.Policy.MaxRounds != admittedPolicy.MaxRounds ||
		receipt.Policy.StallWindow != admittedPolicy.StallWindow ||
		receipt.Policy.Weights != admittedPolicy.Weights ||
		receipt.Policy.Blocking != admittedPolicy.Blocking {
		fail("sealed receipt declares convergence bounds (rounds %d, stall %d, weights %v, blocking %v) that differ from the admitted policy (rounds %d, stall %d, weights %v, blocking %v)",
			receipt.Policy.MaxRounds, receipt.Policy.StallWindow, receipt.Policy.Weights, receipt.Policy.Blocking,
			admittedPolicy.MaxRounds, admittedPolicy.StallWindow, admittedPolicy.Weights, admittedPolicy.Blocking)
	} else {
		pass("convergence bounds, weights, and blocking boundary match the admitted policy")
	}
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
	if finalRound.Transition != transitionConverge || finalRound.BlockingMeasure != 0 {
		fail("the final recorded round is not a convergence with zero blocking findings")
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
		} else if summary.BlockingMeasure != 0 {
			fail("final review leaves blocking findings open (blocking measure %d recomputed from the exact bytes)", summary.BlockingMeasure)
		} else {
			pass("final review bytes revalidate with zero blocking findings")
		}
	}

	report.Verified = len(report.Failures) == 0
	return report
}

// findReceiptForHead scans a receipt directory for the committed
// attestation that binds the exact head reviewed tree. Files that are not
// strict attestations (including receipts of the superseded full format)
// are skipped: they are not the admitted artifact.
func findReceiptForHead(repo *gitRepo, directory, headRevision string) (committedAttestation, string, error) {
	head, err := repo.revParse(headRevision)
	if err != nil {
		return committedAttestation{}, "", err
	}
	reviewedTree, err := repo.reviewedTree(head)
	if err != nil {
		return committedAttestation{}, "", err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return committedAttestation{}, "", fmt.Errorf("no review attestation binds reviewed tree %s: %s does not exist; run the local review loop and commit the sealed attestation", reviewedTree, directory)
		}
		return committedAttestation{}, "", err
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
		attestation, err := readAttestation(path)
		if err != nil {
			continue
		}
		if attestation.ReviewedTree == reviewedTree {
			return attestation, path, nil
		}
	}
	return committedAttestation{}, "", fmt.Errorf("no review attestation in %s binds reviewed tree %s; run the local review loop to convergence, seal, and commit the attestation", directory, reviewedTree)
}
