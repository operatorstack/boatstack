package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	softwareflow "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery"
	"github.com/operatorstack/boatstack/boatstack/internal/buildinfo"
	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/effects"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/foregroundwork"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/plant"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
)

func TestExactProductDeliveryFlowReachesPublishedPRWithFakeProvider(t *testing.T) {
	// control-law: the exact reference product-delivery Flow can advance from
	// one inbox plan to its marked target without human-produced deterministic
	// parameters. Provider authority is exercised unchanged through a fake CLI.
	if runtime.GOOS == "windows" {
		t.Skip("the POSIX fake publication provider is exercised by the Linux and macOS jobs")
	}
	stateRoot, runtimeHome := t.TempDir(), t.TempDir()
	t.Setenv("BOATSTACK_STATE_ROOT", stateRoot)
	t.Setenv(boatstackruntime.HomeEnvironment, runtimeHome)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	runtimeRaw, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	runtimeIdentity := boatstackruntime.Identity{Version: buildinfo.Version, SHA256: hash(runtimeRaw), SourceRevision: buildRevision()}
	if _, err := boatstackruntime.InstallExecutable(executable, runtimeHome, runtimeIdentity); err != nil {
		t.Fatal(err)
	}

	document, sourceRaw, lockRaw, assets := exactProductDeliveryFixture(t)
	repository := t.TempDir()
	runFlowGit(t, repository, "init", "-q", "-b", "main")
	runFlowGit(t, repository, "config", "user.email", "fixture@example.invalid")
	runFlowGit(t, repository, "config", "user.name", "Fixture")
	writeFixture(t, repository, ".boatstack/project.json", []byte(`{"schema_version":5,"identity":{"default":"developer","roles":{"developer":{"kind":"literal","value":"operator"}}},"project":{"name":"todo","default_branch":"main","commands":{"build":"true","test":"true"}},"policy":{"plan_approval":"human-or-autonomy","visual_evidence":"optional","external_effect_authority":"human-or-autonomy-plus-provider","independent_review_for_high_risk":false},"hosts":["cli","codex","claude"],"projections":["codex","claude"]}`))
	writeFixture(t, repository, ".boatstack/plans/inbox/todo.md", []byte("# Add one todo\n"))
	for path, content := range assets {
		writeFixture(t, repository, path, content)
	}
	writeFixture(t, repository, ".boatstack/publication/todo.body.md", []byte("# Add one todo\n\nDeterministic test publication.\n"))
	sourcePath, lockPath := "boatstack/testdata/control-programs/product-delivery-planning-package.flow.ts", "package-lock.json"
	writeFixture(t, repository, sourcePath, sourceRaw)
	writeFixture(t, repository, lockPath, lockRaw)
	writeFlowArtifact(t, repository, document, sourcePath, sourceRaw, lockPath, lockRaw)
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "commit", "-q", "-m", "fixture")

	bare := filepath.Join(t.TempDir(), "todo.git")
	runFlowGit(t, repository, "init", "--bare", bare)
	runFlowGit(t, repository, "remote", "add", "origin", bare)
	runFlowGit(t, repository, "push", "-q", "-u", "origin", "main")
	installFakePublicationProvider(t)

	generatedSkill, err := os.ReadFile(filepath.Join(repository, ".agents", "skills", "product-delivery-run", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	generatedStart := "boatstack flow run --repo . --flow product-delivery --entry run --repository-authority --host codex --format json"
	if !strings.Contains(string(generatedSkill), generatedStart) {
		t.Fatalf("generated skill lacks exact start command %q", generatedStart)
	}

	// `next` remains a read-only resolver even when scoped to a Flow entry.
	// The generated execution skill must use `flow run` instead of changing
	// the observation contract of `next`.
	if _, err := captureRunOutput(t, "next", "--repo", repository, "--flow", "product-delivery", "--entry", "run", "--repository-authority", "--host", "codex", "--format", "json"); err != nil {
		t.Fatalf("read-only Flow next: %v", err)
	}
	resolver, err := plant.NewResolver("")
	if err != nil {
		t.Fatal(err)
	}
	invoking, err := resolver.ResolveInvocation(context.Background(), repository, "codex", "read-only-next")
	if err != nil {
		t.Fatal(err)
	}
	layout, _, err := resolver.ResolveLayout(context.Background(), invoking)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{layout.StatePath, layout.ReceiptPath} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("read-only Flow next wrote %s: %v", path, statErr)
		}
	}

	// The exact generated command supplies no actor. It must return a typed,
	// run-bound installation authority question instead of a zero-progress
	// candidate or a premature product delegation request.
	first, err := captureStdout(t, func() error {
		return run([]string{"flow", "run", "--repo", repository, "--flow", "product-delivery", "--entry", "run", "--repository-authority", "--host", "codex", "--format", "json"})
	})
	if err != nil {
		t.Fatalf("installation authority suspension: %v\n%s", err, first)
	}
	var installationQuestion surfaces.Response
	if err := json.Unmarshal(first, &installationQuestion); err != nil {
		t.Fatal(err)
	}
	if installationQuestion.Question == nil || installationQuestion.Question.TransitionID != "installation.initialize" || len(installationQuestion.Question.Authority) != 2 || installationQuestion.RunID == "" || installationQuestion.Authorization != nil || installationQuestion.Receipt != nil {
		t.Fatalf("installation authority response = %#v\n%s", installationQuestion, first)
	}
	runID := installationQuestion.RunID
	t.Logf("SUSPENSION question run=%s transition=%s authority=%v", runID, installationQuestion.Question.TransitionID, installationQuestion.Question.Authority)

	// Accepting installation may write the control bundle, but product authority
	// cannot bind to those bytes until an explicit Git commit establishes the
	// revision boundary.
	install, err := captureStdout(t, func() error {
		return run([]string{"flow", "run", "--repo", repository, "--flow", "product-delivery", "--entry", "run", "--run-id", runID, "--repository-authority", "--human", "operator", "--host", "codex", "--format", "json"})
	})
	if err != nil {
		t.Fatalf("automatic initialization: %v\n%s", err, install)
	}
	var commitSuspension surfaces.Response
	if err := json.Unmarshal(install, &commitSuspension); err != nil {
		t.Fatal(err)
	}
	if commitSuspension.CommitRequired == nil || commitSuspension.CommitRequired.Code != controlBundleCommitRequiredCode || commitSuspension.CommitRequired.RunID != runID || commitSuspension.CommitRequired.ControlBundleFingerprint == "" || commitSuspension.Authorization != nil || commitSuspension.Work != nil {
		t.Fatalf("control-bundle commit suspension = %#v\n%s", commitSuspension, install)
	}
	if !strings.Contains(string(generatedSkill), controlBundleCommitRequiredCode) || !strings.Contains(string(generatedSkill), "stay in the source\nrepository") {
		t.Fatalf("generated skill does not route the installation commit suspension to the source repository")
	}
	prerequisites := committedFlowReceipts(t, repository, runID)
	if len(prerequisites) != 1 || prerequisites[0].TransitionID != "installation.initialize" {
		t.Fatalf("pre-commit trace = %#v", prerequisites)
	}
	if _, statErr := os.Stat(filepath.Join(layout.FlowRoot, "work", runID)); !os.IsNotExist(statErr) {
		t.Fatalf("product work exists before bundle commit and delegation: %v", statErr)
	}
	workShowRaw, err := captureStdout(t, func() error {
		return runFlowWork([]string{
			"show", "--repo", repository, "--flow", "product-delivery", "--entry", "run", "--run-id", runID,
			"--work-id", "planning-package", "--host", "codex", "--format", "json",
		})
	})
	if err != nil {
		t.Fatalf("work show commit suspension: %v\n%s", err, workShowRaw)
	}
	var workShowSuspension surfaces.Response
	if err := json.Unmarshal(workShowRaw, &workShowSuspension); err != nil {
		t.Fatal(err)
	}
	if workShowSuspension.Operation != surfaces.OperationWorkShow || workShowSuspension.CommitRequired == nil || workShowSuspension.CommitRequired.Code != controlBundleCommitRequiredCode {
		t.Fatalf("work show commit suspension = %#v", workShowSuspension)
	}
	if status := runFlowGitOutput(t, repository, "status", "--short"); !strings.Contains(status, ".boatstack/runtime.json") || !strings.Contains(status, ".boatstack/host-projections.json") {
		t.Fatalf("automatic installation did not leave the exact bundle for explicit commit:\n%s", status)
	}
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "commit", "-q", "-m", "install Boatstack control bundle")
	runFlowGit(t, repository, "push", "-q", "origin", "main")

	delegationOutput, err := captureStdout(t, func() error {
		return run([]string{"flow", "run", "--repo", repository, "--flow", "product-delivery", "--entry", "run", "--run-id", runID, "--repository-authority", "--human", "operator", "--host", "codex", "--format", "json"})
	})
	if err != nil {
		t.Fatalf("delegation suspension: %v\n%s", err, delegationOutput)
	}
	var delegated surfaces.Response
	if err := json.Unmarshal(delegationOutput, &delegated); err != nil {
		t.Fatal(err)
	}
	if delegated.Authorization == nil || delegated.Authorization.RunID == "" {
		t.Fatalf("delegation response = %#v\n%s", delegated, delegationOutput)
	}
	if delegated.Authorization.RunID != runID {
		t.Fatalf("authorization run = %s, want %s", delegated.Authorization.RunID, runID)
	}
	if delegated.Authorization.Code != "ENTRY_ACTIVATION_AUTHORITY_REQUIRED" || !slices.Equal(delegated.Authorization.EntryActivationAuthorities, []catalog.AuthorityClass{catalog.AuthorityHuman}) || !slices.Equal(delegated.Authorization.DelegatedAuthorities, []catalog.AuthorityClass{catalog.AuthorityAutonomy}) {
		t.Fatalf("combined authorization = %#v", delegated.Authorization)
	}
	t.Logf("SUSPENSION authorization run=%s request=%s activation=%v delegation=%v", runID, delegated.Authorization.RequestFingerprint, delegated.Authorization.EntryActivationAuthorities, delegated.Authorization.DelegatedAuthorities)
	if delegated.Work != nil {
		t.Fatalf("product work crossed delegation: %#v", delegated.Work)
	}
	preDelegation := committedFlowReceipts(t, repository, runID)
	if len(preDelegation) != 1 || preDelegation[0].TransitionID != "installation.initialize" {
		t.Fatalf("product transition crossed delegation: %#v", preDelegation)
	}
	if _, err := captureStdout(t, func() error {
		return runFlowAuthorize([]string{
			"--repo", repository, "--flow", "product-delivery", "--entry", "run", "--run-id", runID,
			"--request-fingerprint", delegated.Authorization.RequestFingerprint,
			"--human-identity-provider-fingerprint", delegated.Authorization.HumanIdentity.ProviderFingerprint,
			"--human", "operator", "--host", "codex",
		})
	}); err != nil {
		t.Fatal(err)
	}
	t.Logf("AUTHORIZATION accepted activation=human delegation=autonomy actor=operator request=%s", delegated.Authorization.RequestFingerprint)

	workOutput, err := captureStdout(t, func() error {
		return runFlowContinuation([]string{"--repo", repository, "--flow", "product-delivery", "--entry", "run", "--run-id", runID, "--repository-authority", "--human", "operator", "--host", "codex", "--format", "json"})
	})
	if err != nil {
		t.Fatalf("planning suspension: %v\n%s", err, workOutput)
	}
	var workResponse surfaces.Response
	if err := json.Unmarshal(workOutput, &workResponse); err != nil {
		t.Fatal(err)
	}
	if workResponse.Work == nil || workResponse.Work.Status != foregroundwork.StatusRequested {
		t.Fatalf("foreground work response = %#v", workResponse)
	}
	t.Logf("SUSPENSION work run=%s request=%s transition=%s", runID, workResponse.Work.Request.Fingerprint, workResponse.Work.Request.TransitionID)
	writePlanningOutputs(t, *workResponse.Work)
	completeOutput, err := captureStdout(t, func() error {
		return runFlowWork([]string{
			"complete", "--repo", repository, "--flow", "product-delivery", "--entry", "run", "--run-id", runID,
			"--work-id", "planning-package", "--host", "codex", "--format", "json",
		})
	})
	if err != nil {
		t.Fatalf("complete planning work: %v\n%s", err, completeOutput)
	}
	var completed surfaces.Response
	if err := json.Unmarshal(completeOutput, &completed); err != nil {
		t.Fatal(err)
	}
	if completed.Work == nil || completed.Work.Result == nil {
		t.Fatalf("completed work response = %#v", completed)
	}
	t.Logf("WORK completed result=%s", completed.Work.Result.ResultFingerprint)

	continuation, err := parseOptions("flow run", []string{
		"--repo", repository, "--flow", "product-delivery", "--entry", "run", "--run-id", runID,
		"--repository-authority", "--host", "codex", "--format", "json",
	}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var final surfaces.Response
	for step := 0; step < 64; step++ {
		final, err = executeContinuationStep(context.Background(), continuation)
		if err != nil {
			if strings.Contains(err.Error(), "TRANSITION_INPUT_BLOCKED: canonical parameter artifact is unavailable") {
				if continuation.repository == repository {
					continuation.repository = managedFlowWorktree(t, repository)
				}
				writeGateInputs(t, continuation.repository, "todo")
				inspectActiveFlow(t, continuation.repository, runID)
				t.Logf("SUSPENSION evidence transition=gate.build.record reason=%s", err)
				continue
			}
			t.Fatalf("full continuation step %d: %v", step, err)
		}
		if final.RunID != "" {
			continuation.runID = final.RunID
		}
		if final.Objective.ID != "" {
			continuation.objectiveID = final.Objective.ID
			continuation.targetID = string(final.Objective.TargetID)
			continuation.trustedObjectiveClass = string(final.Objective.TrustedObjectiveClass())
			continuation.deliveryID = final.Objective.DeliveryID
		}
		if final.Receipt != nil {
			t.Logf("STEP %d transition=%s invocation=%s receipt=%s effects=%d outputs=%v", step, final.Receipt.TransitionID, final.Receipt.InvocationFingerprint, final.Receipt.ID, len(final.Receipt.CommittedEffects), final.Receipt.EffectOutputs)
		}
		if final.Decision != nil && final.Decision.Kind == "TERMINAL" {
			break
		}
		if final.Decision != nil && strings.Contains(final.Decision.Reason, "gate evidence must be") && final.Snapshot != nil {
			continuation.repository = final.Snapshot.Invocation.InvokingPath
			writeGateInputs(t, continuation.repository, "todo")
			t.Logf("SUSPENSION evidence transition=%s reason=%s", final.Invocation.TransitionID, final.Decision.Reason)
			continue
		}
		if final.InputRequest != nil {
			if len(final.InputRequest.Parameters) != 1 || final.InputRequest.Parameters[0].ID != "slice_id" {
				t.Fatalf("deterministic parameter requested human text: %#v", final.InputRequest)
			}
			answerPath := filepath.Join(t.TempDir(), "slice.json")
			if err := os.WriteFile(answerPath, []byte(`{"slice_id":"slice-one"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := captureStdout(t, func() error {
				return runFlowInput([]string{
					"answer", "--repo", continuation.repository, "--flow", "product-delivery", "--entry", "run", "--run-id", runID,
					"--request-fingerprint", final.InputRequest.Fingerprint, "--answer", answerPath, "--human", "operator", "--host", "codex", "--format", "json",
				})
			}); err != nil {
				t.Fatal(err)
			}
			t.Logf("SUSPENSION input transition=%s request=%s parameter=slice_id", final.InputRequest.TransitionID, final.InputRequest.Fingerprint)
			continue
		}
		if final.Receipt == nil || final.Prescription == nil {
			t.Fatalf("unexpected non-terminal continuation response at step %d: %#v", step, final)
		}
		if err := advanceContinuation(&continuation, final); err != nil {
			t.Fatal(err)
		}
	}
	if final.Decision == nil || final.Decision.Kind != "TERMINAL" || final.Snapshot == nil {
		t.Fatalf("terminal response = %#v", final)
	}
	if final.Snapshot.Verification.Value != model.VerificationCurrent || final.Snapshot.Configuration.Value != model.ConfigurationVerified || final.Snapshot.Runtime.Value != model.RuntimeVerified || final.Snapshot.Publication.Value != model.PublicationOpen {
		t.Fatalf("terminal marked state = verification=%s configuration=%s runtime=%s publication=%s", final.Snapshot.Verification.Value, final.Snapshot.Configuration.Value, final.Snapshot.Runtime.Value, final.Snapshot.Publication.Value)
	}
	receipts := committedFlowReceipts(t, continuation.repository, runID)
	if len(receipts) == 0 {
		t.Fatal("full Flow produced no committed receipts")
	}
	wantTrace := []string{
		"installation.initialize", "objective.bind", "engagement.begin",
		"planning.package.admit", "planning.package.approve", "planning.package.promote", "plan.activate",
		"workspace.cut", "workspace.activate",
		"gate.build.record", "gate.test.record", "gate.review.record",
		"publication.preview", "publication.execute", "publication.observe",
	}
	if len(receipts) != len(wantTrace) {
		t.Fatalf("committed transition count = %d, want %d", len(receipts), len(wantTrace))
	}
	for _, receipt := range receipts {
		classes := make([]string, 0, len(receipt.AuthoritySources))
		for _, source := range receipt.AuthoritySources {
			classes = append(classes, string(source.Class))
		}
		t.Logf("TRACE seq=%d transition=%s invocation=%s receipt=%s authority=%v effects=%d outputs=%v", receipt.Sequence, receipt.TransitionID, receipt.InvocationFingerprint, receipt.ID, classes, len(receipt.CommittedEffects), receipt.EffectOutputs)
	}
	for index, transitionID := range wantTrace {
		if receipts[index].TransitionID != catalog.TransitionID(transitionID) {
			t.Fatalf("trace transition %d = %s, want %s", index+1, receipts[index].TransitionID, transitionID)
		}
	}
	last := receipts[len(receipts)-1]
	if last.TransitionID != "publication.observe" || final.Snapshot.Publication.Value != model.PublicationOpen {
		t.Fatalf("final publication observation = receipt=%s state=%s", last.TransitionID, final.Snapshot.Publication.Value)
	}
	var replayReceipt protocol.TransitionReceipt
	for _, receipt := range receipts {
		if receipt.TransitionID == "gate.review.record" {
			replayReceipt = receipt
		}
	}
	if replayReceipt.ID == "" {
		t.Fatal("full Flow produced no gate-review receipt to replay")
	}
	if err := os.Remove(filepath.Join(continuation.repository, ".boatstack", "evidence", "todo", "review.input.json")); err != nil {
		t.Fatal(err)
	}
	replayedRequest, err := bindRPCFlowEntry(context.Background(), surfaces.Request{
		SchemaVersion: surfaces.SchemaVersion, Operation: surfaces.OperationApply,
		Repository: continuation.repository, Host: "codex", CorrelationID: "committed-replay",
		ProgramID: "product-delivery", ProgramFingerprint: documentFingerprint(t, document), EntryID: "run", FlowID: runID,
		TransitionID: replayReceipt.TransitionID, IdempotencyKey: replayReceipt.IdempotencyKey,
		Prescription: protocol.Prescription{ID: replayReceipt.PrescriptionID, InvocationFingerprint: replayReceipt.InvocationFingerprint},
	})
	if err != nil {
		t.Fatalf("committed replay rematerialized consumed producer input: %v", err)
	}
	if replayedRequest.InvocationEvidence != nil || replayedRequest.ControlBundle != nil || len(replayedRequest.Parameters) != 0 {
		t.Fatalf("committed replay crossed producer materialization: %#v", replayedRequest)
	}
	t.Logf("TERMINAL target=published-pr verification=%s configuration=%s runtime=%s publication=%s snapshot=%s", final.Snapshot.Verification.Value, final.Snapshot.Configuration.Value, final.Snapshot.Runtime.Value, final.Snapshot.Publication.Value, final.Snapshot.Fingerprint)
}

func documentFingerprint(t *testing.T, document controlprogram.Document) string {
	t.Helper()
	compiled, err := controlprogram.Compile(document, mustSoftwareResolver(t))
	if err != nil {
		t.Fatal(err)
	}
	return compiled.Fingerprint
}

func mustSoftwareResolver(t *testing.T) softwareflow.Resolver {
	t.Helper()
	resolver, err := softwareflow.NewResolver(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

func inspectActiveFlow(t *testing.T, repository, runID string) {
	t.Helper()
	resolver, err := plant.NewResolver("")
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := resolver.ResolveInvocation(context.Background(), repository, "codex", "inspect-active-flow")
	if err != nil {
		t.Fatal(err)
	}
	layout, invocation, err := resolver.ResolveLayout(context.Background(), invocation)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(layout.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	state, err := durable.DecodeState(raw)
	if err != nil {
		t.Fatal(err)
	}
	objective, ok := state.ActiveObjective()
	if !ok {
		t.Fatalf("managed worktree has no active objective: %#v", state)
	}
	receipt, found, err := effects.FindLatestCommittedFlowForObjective(layout, invocation, objective, state.Revision)
	if err != nil || !found || receipt.FlowID != runID {
		t.Fatalf("managed active Flow receipt = %#v found=%t err=%v want=%s state-revision=%d journal=%s", receipt, found, err, runID, state.Revision, layout.JournalRoot)
	}
}

func managedFlowWorktree(t *testing.T, repository string) string {
	t.Helper()
	canonicalRepository, err := filepath.EvalSymlinks(repository)
	if err != nil {
		t.Fatal(err)
	}
	output := runFlowGitOutput(t, repository, "worktree", "list", "--porcelain")
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, "worktree ") {
			continue
		}
		path := strings.TrimPrefix(line, "worktree ")
		canonicalPath, canonicalErr := filepath.EvalSymlinks(path)
		if canonicalErr != nil {
			t.Fatal(canonicalErr)
		}
		if canonicalPath != canonicalRepository {
			return canonicalPath
		}
	}
	t.Fatal("managed Flow worktree was not created")
	return ""
}

func writeGateInputs(t *testing.T, repository, deliveryID string) {
	t.Helper()
	revision := runFlowGitOutput(t, repository, "rev-parse", "HEAD")
	for _, gate := range []string{"build", "test", "review", "change", "journey"} {
		raw, err := json.Marshal(map[string]any{
			"schema_version": 1, "gate": gate, "source_revision": revision, "outcome": "passed",
			"producer": "deterministic-fake-provider", "completed_at": time.Unix(1_700_000_000, 0).UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, repository, filepath.Join(".boatstack", "evidence", deliveryID, gate+".input.json"), append(raw, '\n'))
	}
}

func exactProductDeliveryFixture(t *testing.T) (controlprogram.Document, []byte, []byte, map[string][]byte) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate exact product-delivery fixture")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	repositoryRoot := filepath.Dir(moduleRoot)
	source := filepath.Join(moduleRoot, "testdata", "control-programs", "product-delivery-planning-package.flow.ts")
	raw, err := os.ReadFile(filepath.Join(moduleRoot, "testdata", "control-programs", "product-delivery-planning-package.raw.json"))
	if err != nil {
		t.Fatalf("read checked exact product-delivery fixture: %v", err)
	}
	resolver, err := softwareflow.NewResolver(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := controlprogram.LoadWithAssets(bytes.NewReader(raw), resolver, controlprogram.RepositoryAssetResolver{Repository: repositoryRoot})
	if err != nil {
		t.Fatal(err)
	}
	sourceRaw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	lockRaw, err := os.ReadFile(filepath.Join(repositoryRoot, "package-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	assets := map[string][]byte{}
	for _, path := range []string{
		"boatstack/testdata/control-programs/assets/planning-package.md",
		"boatstack/testdata/control-programs/assets/planning-list.schema.json",
	} {
		raw, readErr := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(path)))
		if readErr != nil {
			t.Fatal(readErr)
		}
		assets[path] = raw
	}
	return compiled.Document, sourceRaw, lockRaw, assets
}

func installFakePublicationProvider(t *testing.T) {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	gitScript := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = remote ] && [ \"$2\" = get-url ] && [ \"$3\" = --push ] && [ \"$4\" = origin ]; then\n  echo git@github.com:operatorstack/todo.git\n  exit 0\nfi\nexec %q \"$@\"\n", realGit)
	ghScript := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = repo ] && [ \"$2\" = view ]; then\n  echo '{\"nameWithOwner\":\"operatorstack/todo\",\"url\":\"https://github.com/operatorstack/todo\",\"viewerPermission\":\"WRITE\"}'\n  exit 0\nfi\nif [ \"$1\" = pr ] && [ \"$2\" = create ]; then\n  echo 'https://github.com/operatorstack/todo/pull/17'\n  exit 0\nfi\nif [ \"$1\" = pr ] && [ \"$2\" = view ]; then\n  head=$(%q rev-parse HEAD)\n  branch=$(%q branch --show-current)\n  printf '{\"state\":\"OPEN\",\"url\":\"https://github.com/operatorstack/todo/pull/17\",\"number\":17,\"mergedAt\":\"\",\"baseRefName\":\"main\",\"headRefName\":\"%%s\",\"headRefOid\":\"%%s\",\"isCrossRepository\":false}\\n' \"$branch\" \"$head\"\n  exit 0\nfi\necho unsupported fake gh command >&2\nexit 2\n", realGit, realGit)
	for name, content := range map[string]string{"git": gitScript, "gh": ghScript} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func writePlanningOutputs(t *testing.T, record foregroundwork.Record) {
	t.Helper()
	for _, output := range record.Request.Contract.Outputs {
		if !output.Required {
			continue
		}
		path := filepath.Join(record.Request.StagingRoot, filepath.FromSlash(output.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		content := []byte("# Deterministic planning output\n")
		if output.MediaType == "application/json" {
			content = []byte(`{"items":[]}` + "\n")
		}
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func committedFlowReceipts(t *testing.T, repository, runID string) []protocol.TransitionReceipt {
	t.Helper()
	resolver, err := plant.NewResolver("")
	if err != nil {
		t.Fatal(err)
	}
	invoking, err := resolver.ResolveInvocation(context.Background(), repository, "codex", "e2e-trace")
	if err != nil {
		t.Fatal(err)
	}
	layout, _, err := resolver.ResolveLayout(context.Background(), invoking)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(layout.ReceiptPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var receipts []protocol.TransitionReceipt
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var receipt protocol.TransitionReceipt
		if err := json.Unmarshal(scanner.Bytes(), &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.FlowID == runID {
			receipts = append(receipts, receipt)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Slice(receipts, func(i, j int) bool { return receipts[i].Sequence < receipts[j].Sequence })
	return receipts
}
