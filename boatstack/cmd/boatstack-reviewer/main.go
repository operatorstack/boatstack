// Command boatstack-reviewer runs Boatstack's self-review as a supervisory
// control program over the domain-neutral kernel.
//
// The loop runs locally, driven by a coding agent or a human. The proposer
// is untrusted: it produces candidate review findings under the admitted
// review policy; this program owns admissibility, freshness, convergence,
// receipts, and recovery through the exact kernel relation. The sealed
// converged receipt travels with the pull request, and CI verifies it
// deterministically — no reviewer, model, or API key runs in CI.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/operatorstack/boatstack/boatstack/kernel"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "boatstack-reviewer:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) == 0 {
		return fmt.Errorf("usage: boatstack-reviewer <resolve|submit|status|show|seal|verify|reopen|recover|reset> [flags]")
	}
	command, rest := arguments[0], arguments[1:]
	switch command {
	case "resolve":
		return commandResolve(rest)
	case "submit":
		return commandSubmit(rest)
	case "status":
		return commandStatus(rest)
	case "show":
		return commandShow(rest)
	case "seal":
		return commandSeal(rest)
	case "verify":
		return commandVerify(rest)
	case "reopen":
		return commandReopen(rest)
	case "recover":
		return commandRecover(rest)
	case "reset":
		return commandReset(rest)
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

type loopContext struct {
	repo     *gitRepo
	policy   Policy
	program  kernel.Program
	store    *fileStore
	domain   *reviewDomain
	operator reviewOperator
	clock    systemClock
	instance string
	baseRef  string
}

func commonFlags(set *flag.FlagSet) (repoPath, delivery, baseRef, actor *string) {
	repoPath = set.String("repo", ".", "repository path")
	delivery = set.String("delivery", "", "control instance identity (default: derived from the current branch)")
	baseRef = set.String("base", "origin/main", "review base reference")
	actor = set.String("actor", "", "acting identity recorded on receipts")
	return repoPath, delivery, baseRef, actor
}

func newLoopContext(repoPath, delivery, baseRef string) (*loopContext, error) {
	repo, err := openRepo(repoPath)
	if err != nil {
		return nil, err
	}
	policy, err := loadWorktreePolicy(repo.Root)
	if err != nil {
		return nil, err
	}
	program, err := compileReviewProgram(policy)
	if err != nil {
		return nil, err
	}
	instance := delivery
	if instance == "" {
		branch, err := repo.output("rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return nil, err
		}
		branch = strings.TrimSpace(branch)
		if branch == "HEAD" {
			return nil, fmt.Errorf("detached HEAD has no branch identity; pass --delivery")
		}
		instance, err = instanceIDForBranch(branch)
		if err != nil {
			return nil, err
		}
	}
	store := newFileStore(repo.GitDir, instance, program.Identity())
	domain := &reviewDomain{repo: repo, store: store, policy: policy, baseRef: baseRef}
	return &loopContext{
		repo:     repo,
		policy:   policy,
		program:  program,
		store:    store,
		domain:   domain,
		operator: reviewOperator{store: store},
		instance: instance,
		baseRef:  baseRef,
	}, nil
}

func (c *loopContext) runtime() (kernel.Runtime, error) {
	return kernel.NewRuntime(
		c.program, c.domain, c.operator, reviewCapabilities{},
		c.store, directoryLocker{path: c.store.lockPath()}, c.clock,
	)
}

func (c *loopContext) authority(actor string, capabilities ...kernel.Capability) (kernel.Authority, error) {
	return localAuthority(actor, c.clock.Now(), capabilities...)
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

// dispositionView reports which submission transition the current
// observation admits (empty when none does) and the exact domain reason.
type dispositionView struct {
	Admits string `json:"admits,omitempty"`
	Reason string `json:"reason"`
}

type instructionsView struct {
	PromptPath    string `json:"prompt_path"`
	PromptSHA256  string `json:"prompt_sha256"`
	ReviewRange   string `json:"review_range"`
	ReviewedTree  string `json:"reviewed_tree"`
	SchemaPath    string `json:"output_schema_path"`
	SchemaSHA256  string `json:"output_schema_sha256"`
	SubmitCommand string `json:"submit_command"`
}

func commandResolve(arguments []string) error {
	set := flag.NewFlagSet("resolve", flag.ContinueOnError)
	repoPath, delivery, baseRef, actor := commonFlags(set)
	if err := set.Parse(arguments); err != nil {
		return err
	}
	if *actor == "" {
		*actor = "reviewer"
	}
	loop, err := newLoopContext(*repoPath, *delivery, *baseRef)
	if err != nil {
		return err
	}
	runtime, err := loop.runtime()
	if err != nil {
		return err
	}
	authority, err := loop.authority(*actor, capabilitySubmit)
	if err != nil {
		return err
	}
	resolution, err := runtime.Resolve(context.Background(), kernel.ResolveRequest{
		InstanceID: loop.instance,
		Authority:  authority,
	})
	if err != nil {
		return err
	}
	observed, err := loop.domain.observeValue()
	if err != nil {
		return err
	}
	// The kernel's decision reason is generic by design; the domain's
	// submission disposition names the exact blocking or admitting
	// condition so a driver never has to infer it from the observation.
	admits, reason := submissionDisposition(loop.policy, observed)
	return printJSON(struct {
		Instance     string                 `json:"instance"`
		Program      kernel.ProgramIdentity `json:"program"`
		State        kernel.ControlState    `json:"state"`
		Decision     kernel.Decision        `json:"decision"`
		Disposition  dispositionView        `json:"submission_disposition"`
		Prescription *kernel.Prescription   `json:"prescription,omitempty"`
		Observation  observationValue       `json:"observation"`
		Instructions instructionsView       `json:"instructions"`
	}{
		Instance:     loop.instance,
		Program:      loop.program.Identity(),
		State:        resolution.State,
		Decision:     resolution.Decision,
		Disposition:  dispositionView{Admits: admits, Reason: reason},
		Prescription: resolution.Prescription,
		Observation:  observed,
		Instructions: instructionsView{
			PromptPath:    policyPromptPath,
			PromptSHA256:  loop.policy.PromptSHA256,
			ReviewRange:   observed.MergeBase + ".." + observed.HeadCommit,
			ReviewedTree:  observed.ReviewedTree,
			SchemaPath:    policySchemaPath,
			SchemaSHA256:  loop.policy.SchemaSHA256,
			SubmitCommand: "boatstack-reviewer submit --findings <path> --actor <name>",
		},
	})
}

func commandSubmit(arguments []string) error {
	set := flag.NewFlagSet("submit", flag.ContinueOnError)
	repoPath, delivery, baseRef, actor := commonFlags(set)
	findings := set.String("findings", "", "path to the candidate review findings JSON")
	if err := set.Parse(arguments); err != nil {
		return err
	}
	if *findings == "" {
		return fmt.Errorf("submit requires --findings <path>")
	}
	loop, err := newLoopContext(*repoPath, *delivery, *baseRef)
	if err != nil {
		return err
	}
	candidateBytes, err := os.ReadFile(*findings)
	if err != nil {
		return err
	}
	head, err := loop.repo.headCommit()
	if err != nil {
		return err
	}
	reviewedTree, err := loop.repo.reviewedTree(head)
	if err != nil {
		return err
	}
	if err := loop.store.stageCandidate(candidateBytes, reviewedTree); err != nil {
		return err
	}
	runtime, err := loop.runtime()
	if err != nil {
		return err
	}
	authority, err := loop.authority(*actor, capabilitySubmit)
	if err != nil {
		return err
	}
	request := kernel.ResolveRequest{InstanceID: loop.instance, Authority: authority}
	resolution, err := runtime.Resolve(context.Background(), request)
	if err != nil {
		return err
	}
	if resolution.Decision.Kind != kernel.Prescribed || resolution.Prescription == nil {
		printJSON(struct {
			Instance string              `json:"instance"`
			State    kernel.ControlState `json:"state"`
			Decision kernel.Decision     `json:"decision"`
		}{loop.instance, resolution.State, resolution.Decision})
		return fmt.Errorf("submission refused: %s", resolution.Decision.Reason)
	}
	receipt, err := runtime.Apply(context.Background(), kernel.ApplyRequest{
		ResolveRequest: request,
		Prescription:   *resolution.Prescription,
	})
	if err != nil {
		return fmt.Errorf("submission did not commit: %w", err)
	}
	state, err := loop.store.Load(context.Background(), loop.instance)
	if err != nil {
		return err
	}
	observed, err := loop.domain.observeValue()
	if err != nil {
		return err
	}
	return printJSON(struct {
		Instance string         `json:"instance"`
		Mode     string         `json:"mode"`
		Receipt  kernel.Receipt `json:"receipt"`
		Rounds   []journalRound `json:"rounds"`
		Guidance string         `json:"guidance"`
	}{
		Instance: loop.instance,
		Mode:     state.Mode,
		Receipt:  receipt,
		Rounds:   observed.Rounds,
		Guidance: submissionGuidance(state.Mode),
	})
}

func submissionGuidance(mode string) string {
	switch mode {
	case modeConverged:
		return "review converged; run `boatstack-reviewer seal` and commit the sealed attestation"
	case modeFindingsOpen:
		return "blocking findings are open; fix them, commit, and submit a fresh review of the new tree"
	case modeEscalated:
		return "the loop escalated; a human must decide, then `boatstack-reviewer reopen --actor <name>`"
	default:
		return ""
	}
}

func commandStatus(arguments []string) error {
	set := flag.NewFlagSet("status", flag.ContinueOnError)
	repoPath, delivery, baseRef, _ := commonFlags(set)
	if err := set.Parse(arguments); err != nil {
		return err
	}
	loop, err := newLoopContext(*repoPath, *delivery, *baseRef)
	if err != nil {
		return err
	}
	state, err := loop.store.Load(context.Background(), loop.instance)
	if err != nil {
		return err
	}
	observed, err := loop.domain.observeValue()
	if err != nil {
		return err
	}
	stale := state.Program != loop.program.Identity()
	return printJSON(struct {
		Instance     string                 `json:"instance"`
		Program      kernel.ProgramIdentity `json:"program"`
		State        kernel.ControlState    `json:"state"`
		ProgramStale bool                   `json:"program_stale"`
		Observation  observationValue       `json:"observation"`
		Guidance     string                 `json:"guidance,omitempty"`
	}{
		Instance:     loop.instance,
		Program:      loop.program.Identity(),
		State:        state,
		ProgramStale: stale,
		Observation:  observed,
		Guidance: func() string {
			if stale {
				return "the admitted policy or law changed since this state was committed; `boatstack-reviewer reset --confirm` archives it"
			}
			return submissionGuidance(state.Mode)
		}(),
	})
}

// commandShow prints a recorded review itself — the exact archived findings
// bytes — without resolving or changing anything. By default it shows the
// latest recorded round of the current generation; --round selects an earlier
// one, and a staged, not-yet-submitted candidate is included when present.
func commandShow(arguments []string) error {
	set := flag.NewFlagSet("show", flag.ContinueOnError)
	repoPath, delivery, baseRef, _ := commonFlags(set)
	roundIndex := set.Int("round", 0, "round index to show (default: the latest recorded round)")
	if err := set.Parse(arguments); err != nil {
		return err
	}
	loop, err := newLoopContext(*repoPath, *delivery, *baseRef)
	if err != nil {
		return err
	}
	state, err := loop.store.Load(context.Background(), loop.instance)
	if err != nil {
		return err
	}
	journal, err := loop.store.loadJournal()
	if err != nil {
		return err
	}
	rounds := journal.currentRounds()

	var selected *journalRound
	if *roundIndex > 0 {
		for index := range rounds {
			if rounds[index].Index == *roundIndex {
				selected = &rounds[index]
				break
			}
		}
		if selected == nil {
			return fmt.Errorf("generation %d has no round %d (%d recorded)", journal.Generation, *roundIndex, len(rounds))
		}
	} else if len(rounds) > 0 {
		selected = &rounds[len(rounds)-1]
	}

	var review json.RawMessage
	if selected != nil {
		archived, err := loop.store.roundBytes(selected.CandidateFingerprint)
		if err != nil {
			return fmt.Errorf("archived review is unavailable: %w", err)
		}
		review = json.RawMessage(archived)
	}

	var staged *struct {
		Evaluation candidateSummary `json:"evaluation"`
		Review     json.RawMessage  `json:"review"`
	}
	if candidateBytes, _, ok, err := loop.store.loadStagedCandidate(); err != nil {
		return err
	} else if ok {
		observed, err := loop.domain.observeValue()
		if err != nil {
			return err
		}
		staged = &struct {
			Evaluation candidateSummary `json:"evaluation"`
			Review     json.RawMessage  `json:"review"`
		}{Evaluation: *observed.Candidate, Review: json.RawMessage(candidateBytes)}
	}

	if selected == nil && staged == nil {
		return fmt.Errorf("instance %s has no recorded rounds and no staged candidate; run `boatstack-reviewer resolve` for instructions", loop.instance)
	}
	return printJSON(struct {
		Instance   string          `json:"instance"`
		Mode       string          `json:"mode"`
		Generation int             `json:"generation"`
		Rounds     []journalRound  `json:"rounds"`
		Round      *journalRound   `json:"round,omitempty"`
		Review     json.RawMessage `json:"review,omitempty"`
		Staged     any             `json:"staged_candidate,omitempty"`
	}{
		Instance:   loop.instance,
		Mode:       state.Mode,
		Generation: journal.Generation,
		Rounds:     rounds,
		Round:      selected,
		Review:     review,
		Staged: func() any {
			if staged == nil {
				return nil
			}
			return staged
		}(),
	})
}

func commandSeal(arguments []string) error {
	set := flag.NewFlagSet("seal", flag.ContinueOnError)
	repoPath, delivery, baseRef, _ := commonFlags(set)
	output := set.String("output", "", "sealed receipt path (default: .github/reviews/<instance>.receipt.json)")
	if err := set.Parse(arguments); err != nil {
		return err
	}
	loop, err := newLoopContext(*repoPath, *delivery, *baseRef)
	if err != nil {
		return err
	}
	receipt, err := buildSealedReceipt(loop.repo, loop.store, loop.policy, loop.program, loop.baseRef, loop.clock.Now())
	if err != nil {
		return err
	}
	// The full receipt is verified here, at seal time, and archived in the
	// local store. Only the minimal attestation enters the commit.
	report := verifyFullReceipt(loop.repo, receipt, "", loop.baseRef, "HEAD")
	if !report.Verified {
		return fmt.Errorf("seal refused: the full receipt does not verify: %s", strings.Join(report.Failures, "; "))
	}
	archive := filepath.Join(loop.store.dir, "sealed-receipt.json")
	if err := writeSealedReceipt(archive, receipt); err != nil {
		return err
	}
	path := *output
	if path == "" {
		path = filepath.Join(loop.repo.receiptDirectoryPath(), loop.instance+".receipt.json")
	}
	if err := writeAttestation(path, attestationOf(receipt)); err != nil {
		return err
	}
	return printJSON(struct {
		Sealed       string `json:"sealed"`
		Fingerprint  string `json:"fingerprint"`
		ReviewedTree string `json:"reviewed_tree"`
		Archive      string `json:"full_receipt"`
		Guidance     string `json:"guidance"`
	}{path, receipt.Program.Fingerprint, receipt.ReviewedTree, archive,
		"commit the attestation with the pull request; CI verifies it deterministically — pushing can wait until you are ready"})
}

func commandVerify(arguments []string) error {
	set := flag.NewFlagSet("verify", flag.ContinueOnError)
	repoPath := set.String("repo", ".", "repository path")
	receiptPath := set.String("receipt", "", "sealed receipt path (default: scan --dir for the head tree)")
	directory := set.String("dir", receiptDirectory, "receipt directory to scan")
	baseRevision := set.String("base", "", "pull request base revision (policy admission source)")
	headRevision := set.String("head", "", "pull request head revision (tree binding target)")
	if err := set.Parse(arguments); err != nil {
		return err
	}
	if *baseRevision == "" || *headRevision == "" {
		return fmt.Errorf("verify requires --base and --head revisions")
	}
	repo, err := openRepo(*repoPath)
	if err != nil {
		return err
	}
	var attestation committedAttestation
	path := *receiptPath
	if path != "" {
		attestation, err = readAttestation(path)
	} else {
		scanDir := *directory
		if !filepath.IsAbs(scanDir) {
			scanDir = filepath.Join(repo.Root, filepath.FromSlash(scanDir))
		}
		attestation, path, err = findReceiptForHead(repo, scanDir, *headRevision)
	}
	if err != nil {
		printJSON(verificationReport{Failures: []string{err.Error()}, Checks: []string{}, Warnings: []string{}})
		return err
	}
	report := verifyAttestation(repo, attestation, path, *baseRevision, *headRevision)
	if err := printJSON(report); err != nil {
		return err
	}
	if !report.Verified {
		return fmt.Errorf("review verification failed: %s", strings.Join(report.Failures, "; "))
	}
	return nil
}

func commandReopen(arguments []string) error {
	return commandRequested(arguments, "reopen", transitionReopen, capabilityHuman)
}

func commandRecover(arguments []string) error {
	return commandRequested(arguments, "recover", transitionRecover, capabilityRecover)
}

func commandRequested(arguments []string, name, transition string, capability kernel.Capability) error {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	repoPath, delivery, baseRef, actor := commonFlags(set)
	if err := set.Parse(arguments); err != nil {
		return err
	}
	loop, err := newLoopContext(*repoPath, *delivery, *baseRef)
	if err != nil {
		return err
	}
	runtime, err := loop.runtime()
	if err != nil {
		return err
	}
	authority, err := loop.authority(*actor, capability)
	if err != nil {
		return err
	}
	request := kernel.ResolveRequest{
		InstanceID: loop.instance,
		Authority:  authority,
		Requested:  transition,
	}
	resolution, err := runtime.Resolve(context.Background(), request)
	if err != nil {
		return err
	}
	if resolution.Decision.Kind != kernel.Prescribed || resolution.Prescription == nil {
		printJSON(resolution.Decision)
		return fmt.Errorf("%s refused: %s", name, resolution.Decision.Reason)
	}
	receipt, err := runtime.Apply(context.Background(), kernel.ApplyRequest{
		ResolveRequest: request,
		Prescription:   *resolution.Prescription,
	})
	if err != nil {
		return fmt.Errorf("%s did not commit: %w", name, err)
	}
	return printJSON(receipt)
}

func commandReset(arguments []string) error {
	set := flag.NewFlagSet("reset", flag.ContinueOnError)
	repoPath, delivery, baseRef, _ := commonFlags(set)
	confirm := set.Bool("confirm", false, "confirm archiving the instance's local review state")
	if err := set.Parse(arguments); err != nil {
		return err
	}
	loop, err := newLoopContext(*repoPath, *delivery, *baseRef)
	if err != nil {
		return err
	}
	if !*confirm {
		return fmt.Errorf("reset archives %s; pass --confirm to proceed", loop.store.dir)
	}
	if _, err := os.Stat(loop.store.dir); os.IsNotExist(err) {
		return fmt.Errorf("instance %s has no local review state", loop.instance)
	}
	archived, err := loop.store.archive(time.Now().UTC().Format("20060102T150405Z"))
	if err != nil {
		return err
	}
	return printJSON(struct {
		Archived string `json:"archived"`
	}{archived})
}
