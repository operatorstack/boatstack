package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	softwareflow "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery"
	"github.com/operatorstack/boatstack/boatstack/internal/buildinfo"
	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/delegation"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/effects"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/plant"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/supervisor"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
	"github.com/operatorstack/boatstack/boatstack/internal/testprogram"
	"github.com/operatorstack/boatstack/boatstack/kernel"
)

func flowRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	if err := os.Mkdir(filepath.Join(repository, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	document := productDeliveryDocument("product-delivery")
	sourcePath, lockPath := ".boatstack/flows/product-delivery.flow.ts", "package-lock.json"
	source, lock := []byte("flow source"), []byte("lock")
	for path, content := range map[string][]byte{sourcePath: source, lockPath: lock} {
		writeFixture(t, repository, path, content)
	}
	writeFlowArtifact(t, repository, document, sourcePath, source, lockPath, lock)
	return repository
}

func flowRepositoryWithHumanSlice(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	if err := os.Mkdir(filepath.Join(repository, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	document := productDeliveryDocument("product-delivery")
	truth := true
	document.Operators = append(document.Operators, controlprogram.Operator{ID: "delivery.slice.advance", Binding: &controlprogram.OperatorBinding{Reference: "software-delivery/delivery.slice.advance", Version: "1"}})
	document.Transitions = append(document.Transitions, controlprogram.Transition{
		ID: "delivery.slice.advance", Operator: "delivery.slice.advance", Guard: controlprogram.Predicate{True: &truth}, Target: controlprogram.Predicate{True: &truth}, Priority: 76,
		Parameters: []controlprogram.TransitionParameterBinding{
			{Parameter: "slice_id", Producer: controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceHostInput, Request: &controlprogram.HostInputRequest{ID: "delivery-slice", Description: "Select the next bounded delivery slice.", Authorities: []string{"human", "autonomy"}, Scope: "transition"}}},
			{Parameter: "source_revision", Producer: controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceTrustedResolver, Binding: &controlprogram.ParameterResolverBinding{Reference: softwareflow.ParameterResolverPrefix + "current-source-revision", Version: "1"}}},
		},
	})
	resolver, err := softwareflow.NewResolver(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolver.ResolveOperator("software-delivery/delivery.slice.advance", "1")
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	for _, facet := range document.Facets {
		declared[facet.ID] = true
	}
	for _, condition := range resolved.StateEffect.Preconditions {
		if !declared[condition.Facet] {
			document.Facets = append(document.Facets, controlprogram.Facet{ID: condition.Facet, Kind: "string"})
			declared[condition.Facet] = true
		}
	}
	for _, assignment := range resolved.StateEffect.Assignments {
		if !declared[assignment.Facet] {
			document.Facets = append(document.Facets, controlprogram.Facet{ID: assignment.Facet, Kind: "string"})
			declared[assignment.Facet] = true
		}
	}
	sourcePath, lockPath := ".boatstack/flows/product-delivery.flow.ts", "package-lock.json"
	source, lock := []byte("flow source"), []byte("lock")
	for path, content := range map[string][]byte{sourcePath: source, lockPath: lock} {
		writeFixture(t, repository, path, content)
	}
	writeFlowArtifact(t, repository, document, sourcePath, source, lockPath, lock)
	return repository
}

func TestFlowEntryCanonicalizesRepositoryRoot(t *testing.T) {
	repository := flowRepository(t)
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery-one.md", []byte("plan"))
	noncanonical := repository + string(os.PathSeparator) + "."
	bound, err := bindFlowEntry(context.Background(), commandOptions{
		repository: noncanonical, programID: "product-delivery", entryID: "run", host: "codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	exact, err := filepath.EvalSymlinks(repository)
	if err != nil {
		t.Fatal(err)
	}
	if bound.repository != exact {
		t.Fatalf("repository = %q, want exact root %q", bound.repository, exact)
	}
}

func runFlowGit(t *testing.T, repository string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repository}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}

func runFlowGitOutput(t *testing.T, repository string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repository}, arguments...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
	return strings.TrimSpace(string(output))
}

func writeAdmittedFlowProgramState(t *testing.T, repository, programFingerprint string) {
	t.Helper()
	resolver, err := plant.NewResolver("")
	if err != nil {
		t.Fatal(err)
	}
	invoking, err := resolver.ResolveInvocation(context.Background(), repository, "codex", "fixture-program-state")
	if err != nil {
		t.Fatal(err)
	}
	layout, invoking, err := resolver.ResolveLayout(context.Background(), invoking)
	if err != nil {
		t.Fatal(err)
	}
	state := durable.Default(invoking, time.Now().UTC())
	state.ProgramFingerprint = programFingerprint
	raw, err := durable.EncodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(layout.StatePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.StatePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func captureRunOutput(t *testing.T, arguments ...string) ([]byte, error) {
	t.Helper()
	return captureStdout(t, func() error { return run(arguments) })
}

func captureStdout(t *testing.T, action func() error) ([]byte, error) {
	t.Helper()
	oldStdout := os.Stdout
	readSide, writeSide, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	type captureResult struct {
		output []byte
		err    error
	}
	captured := make(chan captureResult, 1)
	go func() {
		output, readErr := io.ReadAll(readSide)
		captured <- captureResult{output: output, err: readErr}
	}()
	os.Stdout = writeSide
	runErr := action()
	os.Stdout = oldStdout
	if err := writeSide.Close(); err != nil {
		t.Fatal(err)
	}
	result := <-captured
	_ = readSide.Close()
	if result.err != nil {
		t.Fatal(result.err)
	}
	return result.output, runErr
}

func TestCaptureStdoutDrainsWhileActionWrites(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 1<<20)
	output, err := captureStdout(t, func() error {
		_, writeErr := os.Stdout.Write(payload)
		return writeErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output, payload) {
		t.Fatalf("captured %d bytes, want %d", len(output), len(payload))
	}
}

func TestExplanationTextPreservesAuthorityAlgebra(t *testing.T) {
	response := surfaces.Response{
		Operation: surfaces.OperationExplain, ProgramID: "product-delivery", EntryID: "run", RunID: "run-fixture",
		Objective: model.Objective{TargetID: model.ObjectiveOpenPR},
		Trace: &kernel.DecisionTrace{
			StateRevision: 7, CurrentMode: string(model.PhaseFrontier),
			Decision: kernel.DecisionTraceValue{Kind: string(supervisor.DecisionFrontier), Transition: "publication.execute", Reason: "transition requires unavailable capabilities"},
			Candidates: []kernel.CandidateTrace{{
				TransitionID: "publication.execute", Disposition: kernel.DispositionAuthorityFrontier,
				Authority: kernel.AuthorityTrace{
					RequiredAny: []kernel.Capability{"authority.autonomy", "authority.human"}, RequiredAll: []kernel.Capability{"authority.external-provider"},
					MissingAll: []kernel.Capability{"authority.external-provider"}, AnySatisfied: true,
				},
			}},
		},
	}
	output, err := captureStdout(t, func() error { return renderResponse(response, "text") })
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	for _, wanted := range []string{"any-of: autonomy, human", "all-of: external-provider", "Missing:\n  external-provider", "No effect was executed."} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("authority explanation lacks %q:\n%s", wanted, text)
		}
	}
	if strings.Contains(strings.ToLower(text), "you should") {
		t.Fatalf("authority explanation became prescriptive:\n%s", text)
	}
}

func TestExplanationTextUsesAuthoritativeCandidateOutcomes(t *testing.T) {
	tests := []struct {
		name       string
		decision   kernel.DecisionTraceValue
		candidates []kernel.CandidateTrace
		want       string
	}{
		{
			name:     "ambiguity",
			decision: kernel.DecisionTraceValue{Kind: string(supervisor.DecisionFrontier), Candidates: []string{"one", "two"}, Reason: "multiple candidates remain ambiguous"},
			candidates: []kernel.CandidateTrace{
				{TransitionID: "one", Disposition: kernel.DispositionAmbiguous},
				{TransitionID: "two", Disposition: kernel.DispositionAmbiguous},
			},
			want: "candidate is part of an unresolved canonical ambiguity",
		},
		{
			name:     "selected candidate refused by preflight",
			decision: kernel.DecisionTraceValue{Kind: string(supervisor.DecisionUnresolved), Reason: "transition \"one\" failed deterministic effect preflight: malformed artifact"},
			candidates: []kernel.CandidateTrace{
				{TransitionID: "one", Disposition: kernel.DispositionSelected},
			},
			want: "failed deterministic effect preflight: malformed artifact",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := surfaces.Response{Operation: surfaces.OperationExplain, Trace: &kernel.DecisionTrace{
				StateRevision: 1, CurrentMode: string(model.PhaseObserved), Decision: test.decision, Candidates: test.candidates,
			}}
			output, err := captureStdout(t, func() error { return renderResponse(response, "text") })
			if err != nil {
				t.Fatal(err)
			}
			text := string(output)
			if !strings.Contains(text, test.want) || strings.Contains(text, "another canonical candidate was preferred") {
				t.Fatalf("candidate explanation is not outcome-bound:\n%s", text)
			}
		})
	}
}

func bindSharedGitCommon(t *testing.T, repository, gitDirectory, commonDirectory string) {
	t.Helper()
	if err := os.Remove(filepath.Join(repository, ".git")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(gitDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(gitDirectory, commonDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDirectory, "commondir"), []byte(relative+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, ".git"), []byte("gitdir: "+gitDirectory+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeFlowArtifact(t *testing.T, repository string, document controlprogram.Document, sourcePath string, source []byte, lockPath string, lock []byte) {
	t.Helper()
	projectPath := filepath.Join(repository, ".boatstack", "project.json")
	if _, err := os.Stat(projectPath); os.IsNotExist(err) {
		writeFixture(t, repository, ".boatstack/project.json", []byte(`{"schema_version":2,"project":{"name":"fixture","default_branch":"main","commands":{}},"policy":{"plan_approval":"human-or-autonomy","visual_evidence":"optional"},"hosts":["cli","codex","claude"]}`))
	}
	resolver, err := softwareflow.NewResolver(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := controlprogram.Compile(document, resolver)
	if err != nil {
		t.Fatal(err)
	}
	skills, err := softwareflow.GenerateSkills(compiled, []string{"codex", "claude"})
	if err != nil {
		t.Fatal(err)
	}
	for path, content := range skills {
		writeFixture(t, repository, path, content)
	}
	_, artifactRaw, err := controlprogram.NewArtifact(compiled, controlprogram.ArtifactInput{
		CompilerVersion: flowCompilerVersion, SourcePath: sourcePath, Source: source, DependencyLockPath: lockPath, DependencyLock: lock, GeneratedSkills: skills,
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, repository, ".boatstack/flows/"+document.Program.ID+".flow.ir.json", artifactRaw)
}

func productDeliveryDocument(programID string) controlprogram.Document {
	truth := true
	config := json.RawMessage(`{"path":".boatstack/plans/inbox","cardinality":"exactly-one"}`)
	return controlprogram.Document{
		Schema: controlprogram.SchemaName, SchemaRevision: controlprogram.SchemaRevision,
		Program:      controlprogram.Program{ID: programID, Version: "1"},
		Declarations: controlprogram.Declarations{InputResolvers: []string{"software-delivery.plan-inbox"}},
		Facets: []controlprogram.Facet{
			{ID: "publication", Kind: "string"}, {ID: "verification", Kind: "string"},
			{ID: "configuration", Kind: "string"}, {ID: "runtime", Kind: "string"},
			{ID: "preview_fingerprint", Kind: "string"}, {ID: "publication_id", Kind: "string"},
		},
		Operators: []controlprogram.Operator{{ID: "publication.observe", Binding: &controlprogram.OperatorBinding{Reference: "software-delivery/publication.observe", Version: "1"}}},
		Transitions: []controlprogram.Transition{{
			ID: "publication.observe", Operator: "publication.observe", Guard: controlprogram.Predicate{True: &truth}, Target: controlprogram.Predicate{True: &truth}, Priority: 77,
			Parameters: []controlprogram.TransitionParameterBinding{{Parameter: "publication_id", Producer: controlprogram.ParameterProducer{
				Kind: controlprogram.ParameterSourceState, Facet: "publication_id", AvailableWhen: ptrPredicate(flowKnown("publication_id")),
			}}},
		}},
		Targets: []controlprogram.Target{{ID: "published-pr", Predicate: controlprogram.Predicate{All: []controlprogram.Predicate{
			flowFact("verification", "current"), flowFact("configuration", "verified"), flowFact("runtime", "verified"), flowFact("publication", "open"),
		}}}},
		Entries: []controlprogram.Entry{{ID: "run", Target: "published-pr", Inputs: []controlprogram.EntryInput{{ID: "plan", Type: "markdown-file", Required: true, Resolver: "software-delivery.plan-inbox", Config: config}}}},
	}
}

type publicationReconcileRunner struct{ output []byte }

func (r publicationReconcileRunner) CombinedOutput(context.Context, string, string, ...string) ([]byte, error) {
	return r.output, nil
}

func recoveryMaterializationDocument(programID string) controlprogram.Document {
	document := productDeliveryDocument(programID)
	truth := true
	document.Facets = append(document.Facets, controlprogram.Facet{ID: softwareflow.RecoveryTransactionFacet, Kind: "string"})
	document.Operators = []controlprogram.Operator{{ID: "publication.reconcile", Binding: &controlprogram.OperatorBinding{Reference: "software-delivery/publication.reconcile", Version: "1"}}}
	document.Transitions = []controlprogram.Transition{{
		ID: "publication.reconcile", Operator: "publication.reconcile", Guard: controlprogram.Predicate{True: &truth}, Target: controlprogram.Predicate{True: &truth}, Priority: 1,
		Parameters: []controlprogram.TransitionParameterBinding{{Parameter: "transaction_id", Producer: controlprogram.ParameterProducer{
			Kind: controlprogram.ParameterSourceState, Facet: softwareflow.RecoveryTransactionFacet, AvailableWhen: ptrPredicate(flowKnown(softwareflow.RecoveryTransactionFacet)),
		}}},
	}}
	return document
}

func publicationExecuteAdmission(t *testing.T, invocationContext model.InvocationContext, stateRevision uint64, programFingerprint, sourceRevision string) (protocol.Admission, catalog.Transition) {
	t.Helper()
	now := time.Now().UTC()
	evidence := model.Evidence{Source: "git:fixture", Revision: sourceRevision, Fingerprint: strings.Repeat("f", 64), ObservedAt: now}
	objective := model.Objective{ID: "objective-product-delivery-run-plan", TargetID: model.ObjectiveOpenPR, DeliveryID: "plan"}
	observation := model.Observation{
		SchemaVersion: model.SnapshotSchemaVersion, StateRevision: stateRevision, RecordedProgramFingerprint: programFingerprint, Invocation: invocationContext,
		Phase: model.Known(model.PhaseActive, evidence), Engagement: model.Known(model.EngagementActive, evidence), Delivery: model.Known(model.DeliveryActive, evidence),
		Workspace: model.Known(model.WorkspaceActive, evidence), Plan: model.Known(model.PlanLocked, evidence),
		Configuration: model.Known(model.ConfigurationVerified, evidence), Runtime: model.Known(model.RuntimeVerified, evidence),
		ConfigurationPolicy: model.Known(model.ConfigurationPolicy{PlanApproval: "human", VisualEvidence: "optional", ExternalEffectAuthority: "human-or-autonomy-plus-provider", Hosts: []string{"cli", "codex"}}, evidence),
		Publication:         model.Known(model.PublicationCandidate, evidence), Verification: model.Known(model.VerificationCurrent, evidence),
		Recovery: model.Known(model.RecoveryNone, evidence), Transaction: model.Known(model.TransactionNone, evidence),
		RecoveryInfo: model.Absent[model.RecoveryContext]("none", evidence), TransactionInfo: model.Absent[model.TransactionContext]("none", evidence),
		Terminal: model.Known(model.TerminalNonterminal, evidence), Objective: model.Known(objective, evidence), ObservedAt: now,
	}
	snapshot, err := model.CanonicalizeForProgram(observation, programFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	transition, ok := testprogram.StandardRegistry().Lookup("publication.execute")
	if !ok {
		t.Fatal("publication execute transition is unavailable")
	}
	previewFingerprint := strings.Repeat("a", 64)
	authority := protocol.AuthorityBundle{Receipts: []protocol.AuthorityReceipt{
		{ID: "human-publication", Class: catalog.AuthorityHuman, Subject: "operator", Fingerprint: "human-publication", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)},
		{ID: "provider-publication", Class: catalog.AuthorityProvider, Subject: "github:fixture", Fingerprint: previewFingerprint, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)},
	}}
	capabilities, err := protocol.ProjectCapabilities(snapshot, transition, authority, now)
	if err != nil {
		t.Fatal(err)
	}
	prescription, err := protocol.NewPrescription(snapshot, transition, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := protocol.NewAdmission(snapshot, objective, transition, prescription, authority, protocol.Parameters{{Name: "preview_fingerprint", Value: previewFingerprint}}, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return admission, transition
}

func TestUntargetedReconciliationMaterializesPendingJournalAdmissionID(t *testing.T) {
	// control-law: recovery admissibility and its transaction parameter must
	// come from the same pending-journal observation when durable state was not
	// advanced before an external outcome became unknown.
	repository := flowRepository(t)
	runFlowGit(t, repository, "init", "-q", "-b", "main")
	runFlowGit(t, repository, "config", "user.name", "Boatstack Tests")
	runFlowGit(t, repository, "config", "user.email", "boatstack@example.invalid")
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "commit", "-q", "-m", "fixture")

	document := recoveryMaterializationDocument("product-delivery")
	resolver, err := softwareflow.NewResolver(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := controlprogram.Compile(document, resolver)
	if err != nil {
		t.Fatal(err)
	}
	plantResolver, err := plant.NewResolver("")
	if err != nil {
		t.Fatal(err)
	}
	invoking, err := plantResolver.ResolveInvocation(context.Background(), repository, "codex", "pending-journal-reconcile")
	if err != nil {
		t.Fatal(err)
	}
	layout, invoking, err := plantResolver.ResolveLayout(context.Background(), invoking)
	if err != nil {
		t.Fatal(err)
	}
	state := durable.Default(invoking, time.Now().UTC())
	state.Revision = 1
	state.ProgramFingerprint = compiled.Fingerprint
	raw, err := durable.EncodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(layout.StatePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.StatePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	admission, execute := publicationExecuteAdmission(t, invoking, state.Revision, compiled.Fingerprint, runFlowGitOutput(t, repository, "rev-parse", "HEAD"))
	journal, err := effects.NewJournal(plantResolver, effects.Clock{})
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Begin(context.Background(), admission, execute); err != nil {
		t.Fatal(err)
	}
	if err := journal.Mark(context.Background(), admission.ID, "executing"); err != nil {
		t.Fatal(err)
	}
	if err := journal.RequireRecovery(context.Background(), admission.ID, "provider outcome unknown"); err != nil {
		t.Fatal(err)
	}
	if state.TransactionID != "" || state.Transaction != model.TransactionNone {
		t.Fatalf("fixture advanced durable transaction state: %#v", state)
	}

	materialized, err := materializeFlowInvocation(context.Background(), compiled, compiled.Document.Entries[0], commandOptions{
		repository: repository, host: "codex", runID: "run-pending-journal", deliveryID: "plan",
		targetID: "published-pr", transitionID: "publication.reconcile",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "transaction_id=" + admission.ID
	if len(materialized.parameters) != 1 || materialized.parameters[0] != want || materialized.invocationEvidence == nil {
		t.Fatalf("observed recovery materialization = parameters %#v evidence %#v, want %q", materialized.parameters, materialized.invocationEvidence, want)
	}
}

func TestPublicationReconciliationWithoutExecuteReceiptMaterializesObservation(t *testing.T) {
	// control-law: an unknown publication effect may reconcile a durable
	// publication identity without manufacturing an execute receipt; the next
	// observation must consume that durable identity.
	repository := flowRepository(t)
	runFlowGit(t, repository, "init", "-q", "-b", "main")
	runFlowGit(t, repository, "config", "user.name", "Boatstack Tests")
	runFlowGit(t, repository, "config", "user.email", "boatstack@example.invalid")
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "commit", "-q", "-m", "fixture")

	document := productDeliveryDocument("product-delivery")
	resolver, err := softwareflow.NewResolver(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := controlprogram.Compile(document, resolver)
	if err != nil {
		t.Fatal(err)
	}
	plantResolver, err := plant.NewResolver("")
	if err != nil {
		t.Fatal(err)
	}
	invoking, err := plantResolver.ResolveInvocation(context.Background(), repository, "codex", "publication-reconcile-fixture")
	if err != nil {
		t.Fatal(err)
	}
	layout, invoking, err := plantResolver.ResolveLayout(context.Background(), invoking)
	if err != nil {
		t.Fatal(err)
	}
	state := durable.Default(invoking, time.Now().UTC())
	state.ProgramFingerprint = compiled.Fingerprint
	state.TransactionID = "publication-unknown"
	state.TransactionTransition = "publication.execute"
	state.Phase = model.PhaseRecovery
	state.Recovery = model.RecoveryReconcile
	state.RecoveryCause = "publication result was not parseable"
	state.RecoverySourcePhase = model.PhaseExecutingExternal
	state.RecoveryResumption = model.PhaseActive
	state.RecoveryBudget = 3
	state.Transaction = model.TransactionExternalUncertain

	if _, _, found, err := effects.FindLatestCommittedTransitionOutput(layout, "run-publication-unknown", invoking, "publication.execute", "publication_id", state.Revision); err != nil || found {
		t.Fatalf("interrupted execute receipt found=%t err=%v", found, err)
	}
	boundary, err := effects.NewNativeBoundaryWithRunner(publicationReconcileRunner{output: []byte(`{"state":"OPEN","url":"https://github.com/operatorstack/todo/pull/9","number":9,"mergedAt":null,"baseRefName":"main","headRefName":"main","headRefOid":"` + runFlowGitOutput(t, repository, "rev-parse", "HEAD") + `","isCrossRepository":false}`)})
	if err != nil {
		t.Fatal(err)
	}
	reconcile, ok := testprogram.StandardRegistry().Lookup("publication.reconcile")
	if !ok {
		t.Fatal("publication reconciliation transition is unavailable")
	}
	admission := protocol.Admission{
		Invocation: invoking, SourceRevision: runFlowGitOutput(t, repository, "rev-parse", "HEAD"),
		Parameters: protocol.Parameters{{Name: "transaction_id", Value: state.TransactionID}},
	}
	admission.RequiredCapabilities = catalog.RequiredCapabilities(reconcile)
	admission.EffectiveCapabilities = admission.RequiredCapabilities
	if err := boundary.PrepareObservation(context.Background(), admission, reconcile, layout, &state); err != nil {
		t.Fatal(err)
	}
	if state.PublicationID != "9" {
		t.Fatalf("reconciled publication ID = %q", state.PublicationID)
	}
	raw, err := durable.EncodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(layout.StatePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.StatePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	materialized, err := materializeFlowInvocation(context.Background(), compiled, compiled.Document.Entries[0], commandOptions{
		repository: repository, host: "codex", runID: "run-publication-unknown", deliveryID: "delivery-one",
		targetID: "published-pr", transitionID: "publication.observe",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(materialized.parameters) != 1 || materialized.parameters[0] != "publication_id=9" || materialized.invocationEvidence == nil {
		t.Fatalf("state-backed observation materialization = parameters %#v evidence %#v", materialized.parameters, materialized.invocationEvidence)
	}
}

func ptrPredicate(value controlprogram.Predicate) *controlprogram.Predicate { return &value }
func flowKnown(facet string) controlprogram.Predicate {
	return controlprogram.Predicate{Fact: &controlprogram.FactPredicate{Facet: facet, Statuses: []string{"known"}}}
}

func writeFixture(t *testing.T, repository, relative string, content []byte) {
	t.Helper()
	path := filepath.Join(repository, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func flowFact(facet, value string) controlprogram.Predicate {
	return controlprogram.Predicate{Fact: &controlprogram.FactPredicate{Facet: facet, Statuses: []string{"known"}, Values: []string{value}}}
}

func TestFlowEntryRejectsPlanCardinalityBeforeManagedState(t *testing.T) {
	// control-law: a run is not created until exactly one repository plan is selected
	for name, count := range map[string]int{"none": 0, "multiple": 2} {
		t.Run(name, func(t *testing.T) {
			repository := flowRepository(t)
			for index := 0; index < count; index++ {
				writeFixture(t, repository, filepath.Join(".boatstack/plans/inbox", string(rune('a'+index))+".md"), []byte("plan"))
			}
			_, err := bindFlowEntry(context.Background(), commandOptions{repository: repository, programID: "product-delivery", entryID: "run", host: "codex"})
			if err == nil {
				t.Fatal("invalid plan cardinality was accepted")
			}
			if count == 0 && !strings.Contains(err.Error(), "PLAN_REQUIRED") {
				t.Fatalf("zero-plan error = %v", err)
			}
			if count > 1 && !strings.Contains(err.Error(), "PLAN_SELECTION_REQUIRED") {
				t.Fatalf("multiple-plan error = %v", err)
			}
			if _, statErr := os.Stat(filepath.Join(repository, ".boatstack", "state.json")); !os.IsNotExist(statErr) {
				t.Fatalf("plan blocker created managed state: %v", statErr)
			}
		})
	}
}

func TestFlowEntryRejectsAdditionalRequiredInputs(t *testing.T) {
	config := json.RawMessage(`{"path":".boatstack/plans/inbox","cardinality":"exactly-one"}`)
	entry := controlprogram.Entry{ID: "run", Inputs: []controlprogram.EntryInput{
		{ID: "first", Required: true, Resolver: "software-delivery.plan-inbox", Config: config},
		{ID: "second", Required: true, Resolver: "software-delivery.plan-inbox", Config: config},
	}}
	if _, _, err := resolvePlanInput(t.TempDir(), entry); err == nil || !strings.Contains(err.Error(), "exactly one plan input") {
		t.Fatalf("multiple required inputs result = %v", err)
	}
}

func TestRPCFlowEntryRejectsUnknownEntryAndInvalidInboxBeforeManagedState(t *testing.T) {
	// control-law: every-surface-binds-the-repository-entry-before-resolution-or-effects
	for name, entry := range map[string]string{"unknown-entry": "missing", "empty-inbox": "run"} {
		t.Run(name, func(t *testing.T) {
			repository := flowRepository(t)
			_, err := bindRPCFlowEntry(context.Background(), surfaces.Request{
				SchemaVersion: surfaces.SchemaVersion, Operation: surfaces.OperationResolve, Repository: repository,
				Host: "claude", CorrelationID: "rpc-binding", ProgramID: "product-delivery", EntryID: entry, FlowID: "run-caller-supplied",
			})
			if err == nil {
				t.Fatal("unbound RPC Flow entry was accepted")
			}
			if _, statErr := os.Stat(filepath.Join(repository, ".boatstack", "state.json")); !os.IsNotExist(statErr) {
				t.Fatalf("RPC entry refusal created managed state: %v", statErr)
			}
		})
	}
}

func TestPrescribedRepositoryTransitionRebindsBeforeExposure(t *testing.T) {
	// control-law: a-selected-repository-transition-cannot-return-or-apply-an-unbound-prescription
	repository := flowRepositoryWithHumanSlice(t)
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery-one.md", []byte("plan"))
	if err := os.RemoveAll(filepath.Join(repository, ".git")); err != nil {
		t.Fatal(err)
	}
	runFlowGit(t, repository, "init", "-q")
	runFlowGit(t, repository, "config", "user.email", "fixture@example.invalid")
	runFlowGit(t, repository, "config", "user.name", "Fixture")
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "commit", "-q", "-m", "fixture")
	writeAdmittedFlowProgramState(t, repository, strings.Repeat("f", 64))
	bound, err := bindFlowEntry(context.Background(), commandOptions{
		repository: repository, programID: "product-delivery", entryID: "run", host: "codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := buildRequest(surfaces.OperationResolve, bound)
	if err != nil {
		t.Fatal(err)
	}
	response := surfaces.Response{Prescription: &protocol.Prescription{
		SchemaVersion: protocol.PrescriptionSchemaVersion,
		TransitionID:  "delivery.slice.advance",
	}}
	rebound, changed, err := bindPrescribedRepositoryInvocation(context.Background(), request, response)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || rebound.TransitionID != "delivery.slice.advance" || rebound.Prescription.ID != "" {
		t.Fatalf("selected prescription was not rebound: changed=%t request=%#v", changed, rebound)
	}
	if rebound.InputRequest == nil || rebound.InvocationEvidence != nil {
		t.Fatalf("host-input transition crossed selection without its exact input request: request=%#v evidence=%#v", rebound.InputRequest, rebound.InvocationEvidence)
	}
	if rebound.InputRequest.ProgramFingerprint != rebound.ProgramFingerprint || rebound.InputRequest.ExecutionProgramFingerprint != strings.Repeat("f", 64) || rebound.InputRequest.ProgramFingerprint == rebound.InputRequest.ExecutionProgramFingerprint {
		t.Fatalf("input request collapsed definition and executable program identities: %#v", rebound.InputRequest)
	}
	_, suspended, changed, err := stabilizeRepositoryPrescription(context.Background(), request, response)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || suspended.InputRequest == nil || suspended.Prescription != nil {
		t.Fatalf("unstabilized prescription escaped the shared resolution boundary: changed=%t response=%#v", changed, suspended)
	}
}

func TestRPCFlowEntryPreservesObjectiveEvidenceAndStopContext(t *testing.T) {
	// control-law: entry-binding-preserves-nonidentity-objective-context
	repository := flowRepository(t)
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery-one.md", []byte("plan"))
	bound, err := bindRPCFlowEntry(context.Background(), surfaces.Request{
		SchemaVersion: surfaces.SchemaVersion, Operation: surfaces.OperationResolve, Repository: repository,
		Host: "claude", CorrelationID: "rpc-context", ProgramID: "product-delivery", EntryID: "run",
		Objective: model.Objective{EvidenceFingerprint: strings.Repeat("a", 64), FrontierIsStop: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if bound.Objective.EvidenceFingerprint != strings.Repeat("a", 64) || !bound.Objective.FrontierIsStop {
		t.Fatalf("RPC binding dropped objective context: %#v", bound.Objective)
	}
}

func TestFlowRunIdentitySurvivesWorkspaceTransfer(t *testing.T) {
	// control-law: a repository Flow run retains one identity when workspace.cut transfers authority
	common := filepath.Join(t.TempDir(), "repository.git")
	if err := os.MkdirAll(filepath.Join(common, "worktrees"), 0o700); err != nil {
		t.Fatal(err)
	}
	source, destination := flowRepository(t), flowRepository(t)
	bindSharedGitCommon(t, source, filepath.Join(common, "worktrees", "source"), common)
	bindSharedGitCommon(t, destination, filepath.Join(common, "worktrees", "destination"), common)
	plan := []byte("# Exact plan\n")
	writeFixture(t, source, ".boatstack/plans/inbox/delivery-one.md", plan)
	writeFixture(t, destination, ".boatstack/plans/delivery-one.source", plan)

	initial, err := bindFlowEntry(context.Background(), commandOptions{
		repository: source, programID: "product-delivery", entryID: "run", host: "codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := bindFlowEntry(context.Background(), commandOptions{
		repository: destination, programID: "product-delivery", entryID: "run", host: "codex",
		flowProgramFingerprint: initial.flowProgramFingerprint, runID: initial.runID,
		deliveryID: initial.deliveryID, targetID: initial.targetID, objectiveID: initial.objectiveID,
		activeFlowBound: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.runID != initial.runID {
		t.Fatalf("workspace transfer changed Flow run identity: %q != %q", resumed.runID, initial.runID)
	}
	if source, ok := resumed.workInputs["plan"]; !ok || source.Value != filepath.Join(resumed.repository, ".boatstack", "plans", "delivery-one.source") {
		t.Fatalf("destination entry input = %#v, %t", source, ok)
	}

	continuation := commandOptions{
		repository: source, transitionID: "workspace.cut",
		parameters:     []string{"base_ref=HEAD", "destination=" + destination},
		prescriptionID: "prescription-cut", expectedInstanceID: "instance-source",
		expectedStateRevision: 7, expectedProgramFingerprint: strings.Repeat("a", 64),
		expectedSnapshotFingerprint: strings.Repeat("b", 64), expectedObjectiveBindingFingerprint: strings.Repeat("c", 64),
		authorityFingerprint: strings.Repeat("d", 64), requiredCapabilities: []string{"workspace.write"},
		effectiveCapabilities: []string{"workspace.write"}, idempotencyKey: "idem-cut",
	}
	resulting := model.InvocationContext{
		RepositoryID: "repository", GitCommonID: "git-common", WorktreeID: "destination", Ref: "refs/heads/feature",
		ControllerID: "controller", InvokingPath: destination, RuntimeVersion: "runtime", RuntimePath: destination,
		RuntimeFingerprint: strings.Repeat("e", 64), Topology: model.TopologyEmbedded, Host: "codex", Correlation: "continuation",
	}
	if err := advanceContinuation(&continuation, surfaces.Response{Receipt: &protocol.TransitionReceipt{ExecutionContext: "advance", ResultingInvocation: &resulting}}); err != nil {
		t.Fatal(err)
	}
	if continuation.repository != destination || continuation.transitionID != "" || len(continuation.parameters) != 0 || continuation.prescriptionID != "" || continuation.idempotencyKey != "" || len(continuation.requiredCapabilities) != 0 || len(continuation.effectiveCapabilities) != 0 {
		t.Fatalf("continuation retained source context or transition parameters: %#v", continuation)
	}
}

func TestWorkspaceCutRejectsControlBundleThatIsNotInBaseRevision(t *testing.T) {
	// control-law: workspace-cut-preserves-the-exact-active-control-bundle
	repository := flowRepository(t)
	repository, err := filepath.EvalSymlinks(repository)
	if err != nil {
		t.Fatal(err)
	}
	runFlowGit(t, repository, "init", "-q")
	runFlowGit(t, repository, "config", "user.name", "Boatstack Tests")
	runFlowGit(t, repository, "config", "user.email", "boatstack@example.invalid")
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "commit", "-q", "-m", "control bundle")

	artifactPath := filepath.Join(repository, ".boatstack", "flows", "product-delivery.flow.ir.json")
	raw, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := controlprogram.LoadArtifact(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := buildRepositoryControlBundle(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	if err := boatstackruntime.VerifyControlBundleRevision(context.Background(), repository, "HEAD", bundle); err != nil {
		t.Fatalf("committed control bundle rejected: %v", err)
	}

	var skillPath string
	for path := range artifact.GeneratedSkills {
		skillPath = path
		break
	}
	if skillPath == "" {
		t.Fatal("fixture generated no skills")
	}
	if err := os.WriteFile(filepath.Join(repository, filepath.FromSlash(skillPath)), []byte("regenerated but uncommitted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = buildRepositoryControlBundle(context.Background(), repository)
	if err == nil || !strings.Contains(err.Error(), skillPath) {
		t.Fatalf("uncommitted generated skill result = %v", err)
	}
}

func TestWorkspaceCutRejectsUncommittedRuntimePinBeforeEffect(t *testing.T) {
	// control-law: a runtime pin cannot outrun the committed Flow projection
	repository := flowRepository(t)
	repository, err := filepath.EvalSymlinks(repository)
	if err != nil {
		t.Fatal(err)
	}
	runFlowGit(t, repository, "init", "-q")
	runFlowGit(t, repository, "config", "user.name", "Boatstack Tests")
	runFlowGit(t, repository, "config", "user.email", "boatstack@example.invalid")
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "commit", "-q", "-m", "control bundle")
	pinRaw, err := boatstackruntime.EncodePin(boatstackruntime.NewPin(
		boatstackruntime.Identity{Version: "v-test", SHA256: strings.Repeat("a", 64), SourceRevision: "test-revision"},
		strings.Repeat("b", 64), durable.StateSchemaVersion,
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, ".boatstack", "runtime.json"), pinRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "workspace")
	_, _, err = bindControlBundle(context.Background(), repository, "workspace.cut", protocol.Parameters{
		{Name: "base_ref", Value: "HEAD"}, {Name: "branch", Value: "feature/bundle"}, {Name: "destination", Value: destination},
	})
	if err == nil || !strings.Contains(err.Error(), "WORKSPACE_CONTROL_BUNDLE_UNCOMMITTED") || !strings.Contains(err.Error(), ".boatstack/runtime.json") {
		t.Fatalf("uncommitted runtime pin result = %v", err)
	}
	if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("workspace was created before bundle admission: %v", statErr)
	}
}

func TestWorkspaceCutFreezesMovingBaseReference(t *testing.T) {
	// control-law: a moving branch cannot change an already admitted workspace base
	repository := flowRepository(t)
	repository, err := filepath.EvalSymlinks(repository)
	if err != nil {
		t.Fatal(err)
	}
	runFlowGit(t, repository, "init", "-q")
	runFlowGit(t, repository, "config", "user.name", "Boatstack Tests")
	runFlowGit(t, repository, "config", "user.email", "boatstack@example.invalid")
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "commit", "-q", "-m", "control bundle")
	want := strings.TrimSpace(runFlowGitOutput(t, repository, "rev-parse", "HEAD"))
	contract, _, err := bindControlBundle(context.Background(), repository, "workspace.cut", protocol.Parameters{{Name: "base_ref", Value: "HEAD"}})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, repository, "README.md", []byte("move branch\n"))
	runFlowGit(t, repository, "add", "README.md")
	runFlowGit(t, repository, "commit", "-q", "-m", "move branch")
	if contract.TargetRevision != want {
		t.Fatalf("target revision changed with branch: got %s want %s", contract.TargetRevision, want)
	}
}

func TestWorkspaceCutResolvesConfiguredBaseFromOriginTrackingBranch(t *testing.T) {
	// control-law: a semantic PR base does not require a redundant local branch
	repository := flowRepository(t)
	repository, err := filepath.EvalSymlinks(repository)
	if err != nil {
		t.Fatal(err)
	}
	runFlowGit(t, repository, "init", "-q")
	runFlowGit(t, repository, "config", "user.name", "Boatstack Tests")
	runFlowGit(t, repository, "config", "user.email", "boatstack@example.invalid")
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "commit", "-q", "-m", "control bundle")
	runFlowGit(t, repository, "branch", "-M", "main")
	want := strings.TrimSpace(runFlowGitOutput(t, repository, "rev-parse", "HEAD"))
	runFlowGit(t, repository, "update-ref", "refs/remotes/origin/main", want)
	runFlowGit(t, repository, "switch", "-q", "-c", "feature")
	runFlowGit(t, repository, "branch", "-D", "main")

	contract, _, err := bindControlBundle(context.Background(), repository, "workspace.cut", protocol.Parameters{{Name: "base_ref", Value: "main"}})
	if err != nil {
		t.Fatal(err)
	}
	if contract.TargetRevision != want {
		t.Fatalf("target revision = %s, want origin/main revision %s", contract.TargetRevision, want)
	}
}

func TestOneStaleFlowBlocksMultiFlowControlBundle(t *testing.T) {
	// control-law: a repository control bundle is complete across every Flow
	repository := flowRepository(t)
	document := productDeliveryDocument("secondary-delivery")
	sourcePath := ".boatstack/flows/secondary-delivery.flow.ts"
	source := []byte("secondary source")
	lockPath := "package-lock.json"
	lockRaw, err := os.ReadFile(filepath.Join(repository, lockPath))
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, repository, sourcePath, source)
	writeFlowArtifact(t, repository, document, sourcePath, source, lockPath, lockRaw)
	artifactRaw, err := os.ReadFile(filepath.Join(repository, ".boatstack", "flows", "secondary-delivery.flow.ir.json"))
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := controlprogram.LoadArtifact(bytes.NewReader(artifactRaw))
	if err != nil {
		t.Fatal(err)
	}
	for path := range artifact.GeneratedSkills {
		writeFixture(t, repository, path, []byte("stale secondary projection\n"))
		if _, bundleErr := buildRepositoryControlBundle(context.Background(), repository); bundleErr == nil || !strings.Contains(bundleErr.Error(), path) {
			t.Fatalf("stale secondary Flow did not block complete bundle: %v", bundleErr)
		}
		return
	}
	t.Fatal("secondary Flow generated no entry skill")
}

func TestFlowEntryRejectsCallerOverridesOfResolvedInputs(t *testing.T) {
	// control-law: entry-resolved-inputs-cannot-be-replaced-by-callers
	for _, surface := range []string{"cli", "rpc"} {
		t.Run(surface, func(t *testing.T) {
			repository := flowRepository(t)
			writeFixture(t, repository, ".boatstack/plans/inbox/delivery-one.md", []byte("plan"))
			other := filepath.Join(repository, "other.md")
			writeFixture(t, repository, "other.md", []byte("other plan"))
			if surface == "cli" {
				_, err := bindFlowEntry(context.Background(), commandOptions{
					repository: repository, programID: "product-delivery", entryID: "run", host: "codex",
					transitionID: "plan.create", parameters: []string{"source_path=" + other},
				})
				if err == nil || !strings.Contains(err.Error(), "FLOW_PARAMETER_BYPASS") {
					t.Fatalf("CLI override result = %v", err)
				}
				return
			}
			_, err := bindRPCFlowEntry(context.Background(), surfaces.Request{
				SchemaVersion: surfaces.SchemaVersion, Operation: surfaces.OperationResolve, Repository: repository,
				Host: "claude", CorrelationID: "rpc-override", ProgramID: "product-delivery", EntryID: "run",
				TransitionID: "plan.create", Parameters: protocol.Parameters{{Name: "source_path", Value: other}},
			})
			if err == nil || !strings.Contains(err.Error(), "FLOW_PARAMETER_BYPASS") {
				t.Fatalf("RPC override result = %v", err)
			}
		})
	}
}

func TestFlowEntryRejectsCallerOverridesDuringUntargetedResolution(t *testing.T) {
	// control-law: untargeted-resolution-and-apply-share-the-exact-entry-input-boundary
	repository := flowRepository(t)
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery-one.md", []byte("plan"))
	other := filepath.Join(repository, "other.md")
	writeFixture(t, repository, "other.md", []byte("other plan"))
	_, err := bindFlowEntry(context.Background(), commandOptions{
		repository: repository, programID: "product-delivery", entryID: "run", host: "codex",
		parameters: []string{"source_path=" + other},
	})
	if err == nil || !strings.Contains(err.Error(), "FLOW_PARAMETER_BYPASS") {
		t.Fatalf("untargeted override result = %v", err)
	}
}

func TestFlowEntryDoesNotMaterializeInternalKernelTransition(t *testing.T) {
	// control-law: repository invocation contracts govern only transitions in
	// canonical Flow IR; internal kernel transitions retain their trusted path.
	repository := flowRepository(t)
	runFlowGit(t, repository, "init", "-q")
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery-one.md", []byte("plan"))
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "fixture")

	bound, err := bindFlowEntry(context.Background(), commandOptions{
		repository: repository, programID: "product-delivery", entryID: "run", host: "codex", transitionID: "objective.bind",
	})
	if err != nil {
		t.Fatal(err)
	}
	if bound.invocationEvidence != nil || bound.inputRequest != nil {
		t.Fatalf("internal transition acquired repository invocation state: evidence=%#v request=%#v", bound.invocationEvidence, bound.inputRequest)
	}
	parameters, err := parseParameters(bound.parameters)
	if err != nil {
		t.Fatal(err)
	}
	if target, ok := parameters.Get("target_id"); !ok || target != "published-pr" {
		t.Fatalf("target context = %q, %t", target, ok)
	}
	if delivery, ok := parameters.Get("delivery_id"); !ok || delivery != "delivery-one" {
		t.Fatalf("delivery context = %q, %t", delivery, ok)
	}
}

func TestFlowRefreshPreservesTrustedMaintenanceParameters(t *testing.T) {
	// control-law: Flow refresh rematerializes repository transition values but
	// preserves parameters already bound by a trusted maintenance command.
	repository := flowRepository(t)
	document := productDeliveryDocument("product-delivery")
	document.Entries[0].Delegation = &controlprogram.DelegationBinding{Reference: "software-delivery/delegation/autonomy", Version: "1"}
	writeFlowArtifact(t, repository, document, ".boatstack/flows/product-delivery.flow.ts", []byte("flow source"), "package-lock.json", []byte("lock"))
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery-one.md", []byte("plan"))
	parameters := []string{
		"source_revision=exact-source",
		"runtime_version=v-test",
		"runtime_sha256=" + strings.Repeat("a", 64),
		"accept_obligation_change=true",
	}
	options, err := bindFlowEntry(context.Background(), commandOptions{
		repository: repository, programID: "product-delivery", entryID: "run", host: "codex",
		transitionID: "installation.reconcile-update", parameters: parameters,
		maintenanceParameterSurface: true, humanActor: "operator",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(options.delegationAuthorities) != 0 || options.delegationRequestFingerprint != "" {
		t.Fatalf("program reconciliation acquired product delegation: authorities=%v request=%q", options.delegationAuthorities, options.delegationRequestFingerprint)
	}
	prior, err := buildRequest(surfaces.OperationApply, options)
	if err != nil {
		t.Fatal(err)
	}
	refreshed, _, err := refreshFlowInvocation(context.Background(), surfaces.OperationApply, prior, options)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"source_revision", "runtime_version", "runtime_sha256", "accept_obligation_change"} {
		if _, ok := refreshed.Parameters.Get(name); !ok {
			t.Fatalf("CLI refresh dropped trusted maintenance parameter %q", name)
		}
	}
	rpc, err := refreshRPCFlowInvocation(context.Background(), prior)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rpc.Parameters, refreshed.Parameters) {
		t.Fatalf("RPC and CLI maintenance refresh differ:\nRPC: %#v\nCLI: %#v", rpc.Parameters, refreshed.Parameters)
	}
}

func TestProgramChangePreflightRequiresExactTypedRecoverySurface(t *testing.T) {
	// control-law: product delegation is delayed only for a complete, exact
	// program-drift suspension with an explicit reconciliation transition.
	response := surfaces.Response{
		Decision: &supervisor.Decision{Kind: supervisor.DecisionUnresolved, Reason: supervisor.ReasonProgramDrift},
		ProgramChange: &surfaces.ProgramChange{
			PriorProgramFingerprint: strings.Repeat("a", 64), CandidateProgramFingerprint: strings.Repeat("b", 64),
			ProgramDeltaFingerprint: strings.Repeat("c", 64), RequiredTransition: "installation.reconcile-update", AcceptanceFlag: "--accept-program-change",
		},
	}
	if !isExactProgramChangeSuspension(response) {
		t.Fatal("complete program-drift suspension was not recognized")
	}
	response.ProgramChange.AcceptanceFlag = "--implicit"
	if isExactProgramChangeSuspension(response) {
		t.Fatal("noncanonical acceptance surface delayed delegation")
	}
}

func TestAcceptedProgramReconciliationReprojectsSameFlowRun(t *testing.T) {
	// control-law: an accepted program mutation is a hard reprojection boundary;
	// the next product resolution uses the new program and preserves the run.
	t.Setenv("BOATSTACK_STATE_ROOT", t.TempDir())
	runtimeHome := t.TempDir()
	t.Setenv(boatstackruntime.HomeEnvironment, runtimeHome)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	runtimeRaw, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := boatstackruntime.InstallExecutable(executable, runtimeHome, boatstackruntime.Identity{Version: buildinfo.Version, SHA256: hash(runtimeRaw), SourceRevision: buildRevision()}); err != nil {
		t.Fatal(err)
	}
	repository := flowRepository(t)
	runFlowGit(t, repository, "init", "-q")
	runFlowGit(t, repository, "config", "user.email", "fixture@example.invalid")
	runFlowGit(t, repository, "config", "user.name", "Fixture")
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery-one.md", []byte("plan"))
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "commit", "-q", "-m", "fixture")
	output, err := captureRunOutput(t,
		"init", "--repo", repository, "--flow", "product-delivery", "--entry", "run",
		"--param", "config_path="+filepath.Join(repository, ".boatstack", "project.json"), "--human", "operator", "--host", "codex", "--format", "json",
	)
	if err != nil {
		t.Fatalf("initialize old program: %v\n%s", err, output)
	}
	bound, err := bindFlowEntry(context.Background(), commandOptions{repository: repository, programID: "product-delivery", entryID: "run", host: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	output, err = captureRunOutput(t,
		"objective-bind", "--repo", repository, "--flow", "product-delivery", "--entry", "run", "--run-id", bound.runID,
		"--human", "operator", "--host", "codex", "--format", "json",
	)
	if err != nil {
		t.Fatalf("bind old-program objective: %v\n%s", err, output)
	}
	document := productDeliveryDocument("product-delivery")
	document.Program.Version = "2"
	writeFlowArtifact(t, repository, document, ".boatstack/flows/product-delivery.flow.ts", []byte("flow source"), "package-lock.json", []byte("lock"))
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "commit", "-q", "-m", "update flow")

	output, err = captureRunOutput(t,
		"reconcile-update", "--repo", repository, "--flow", "product-delivery", "--entry", "run", "--run-id", bound.runID,
		"--accept-program-change", "--human", "operator", "--host", "codex", "--format", "json",
	)
	if err != nil {
		t.Fatalf("accepted reconciliation: %v\n%s", err, output)
	}
	var reconciled surfaces.Response
	if err := json.Unmarshal(output, &reconciled); err != nil {
		t.Fatal(err)
	}
	if reconciled.Receipt == nil || reconciled.Receipt.TransitionID != "installation.reconcile-update" || reconciled.RunID != bound.runID {
		t.Fatalf("reconciliation response = %#v", reconciled)
	}

	output, err = captureRunOutput(t,
		"next", "--repo", repository, "--flow", "product-delivery", "--entry", "run", "--run-id", bound.runID,
		"--host", "codex", "--format", "json",
	)
	if err != nil && strings.Contains(err.Error(), "INVOCATION_DRIFT") {
		t.Fatalf("post-reconciliation resolution reused stale invocation: %v\n%s", err, output)
	}
	var projected surfaces.Response
	if decodeErr := json.Unmarshal(output, &projected); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if projected.RunID != bound.runID || projected.ProgramID != "product-delivery" || projected.ProgramChange != nil {
		t.Fatalf("post-reconciliation projection changed run or Flow program: %#v", projected)
	}
}

func TestFlowCompileRejectsSourceChangedDuringFrontend(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	repository, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, repository, ".boatstack/flows/product-delivery.flow.ts", []byte("source A"))
	writeFixture(t, repository, "package-lock.json", []byte("lock"))
	frontend := filepath.Join(repository, "frontend.sh")
	script := []byte("#!/bin/sh\ncat >/dev/null\nprintf 'source B' > \"$2\"\nprintf '{}\\n'\n")
	if err := os.WriteFile(frontend, script, 0o700); err != nil {
		t.Fatal(err)
	}
	err = compileFlow(context.Background(), flowCommandOptions{
		repository: repository, source: ".boatstack/flows/product-delivery.flow.ts", lock: "package-lock.json", frontend: frontend,
	})
	if err == nil || !strings.Contains(err.Error(), "FLOW_COMPILE_INPUT_CHANGED") {
		t.Fatalf("source replacement result = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(repository, ".boatstack/flows/product-delivery.flow.ir.json")); !os.IsNotExist(statErr) {
		t.Fatalf("source race created an artifact: %v", statErr)
	}
}

func TestFlowCompileDoesNotAutomaticallyExecuteRepositoryFrontend(t *testing.T) {
	// control-law: repository-content-cannot-authorize-ambient-frontend-execution
	repository, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(repository, "sentinel")
	writeFixture(t, repository, ".boatstack/flows/product-delivery.flow.ts", []byte("source"))
	writeFixture(t, repository, "package-lock.json", []byte("lock"))
	frontend := filepath.Join(repository, "node_modules", ".bin", "boatstack-flow-frontend")
	if runtime.GOOS == "windows" {
		frontend += ".cmd"
	}
	if err := os.MkdirAll(filepath.Dir(frontend), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(frontend, []byte("#!/bin/sh\nprintf executed > '"+sentinel+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	err = compileFlow(context.Background(), flowCommandOptions{repository: repository, lock: "package-lock.json"})
	if err == nil || !strings.Contains(err.Error(), "FLOW_FRONTEND_REQUIRED") {
		t.Fatalf("automatic frontend result = %v", err)
	}
	if _, statErr := os.Stat(sentinel); !os.IsNotExist(statErr) {
		t.Fatalf("repository frontend executed: %v", statErr)
	}
}

func TestFlowCompileNamesDefaultArtifactFromProgramID(t *testing.T) {
	// control-law: compiled-artifact-is-discoverable-by-declared-program-identity
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	repository, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	documentRaw, err := json.Marshal(productDeliveryDocument("bar"))
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, repository, ".boatstack/flows/foo.flow.ts", []byte("declarative source"))
	writeFixture(t, repository, ".git/keep", nil)
	writeFixture(t, repository, "package-lock.json", []byte("lock"))
	writeFixture(t, repository, "raw-ir.json", documentRaw)
	frontend := filepath.Join(repository, "frontend.sh")
	script := []byte("#!/bin/sh\ncat >/dev/null\ncat '" + filepath.Join(repository, "raw-ir.json") + "'\n")
	if err := os.WriteFile(frontend, script, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := compileFlow(context.Background(), flowCommandOptions{
		repository: repository, source: ".boatstack/flows/foo.flow.ts", lock: "package-lock.json", frontend: frontend,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repository, ".boatstack", "flows", "bar.flow.ir.json")); err != nil {
		t.Fatalf("program artifact missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repository, ".boatstack", "flows", "foo.flow.ir.json")); !os.IsNotExist(err) {
		t.Fatalf("source-stem artifact exists: %v", err)
	}
}

func TestFlowCompileProjectsHyphenatedEntryIdentity(t *testing.T) {
	// control-law: every valid IR entry identity has an injective artifact-valid skill path
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	repository, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	document := productDeliveryDocument("product-delivery")
	document.Entries[0].ID = "run-now"
	documentRaw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, repository, ".boatstack/flows/product-delivery.flow.ts", []byte("declarative source"))
	writeFixture(t, repository, ".git/keep", nil)
	writeFixture(t, repository, "package-lock.json", []byte("lock"))
	writeFixture(t, repository, "raw-ir.json", documentRaw)
	frontend := filepath.Join(repository, "frontend.sh")
	script := []byte("#!/bin/sh\ncat >/dev/null\ncat '" + filepath.Join(repository, "raw-ir.json") + "'\n")
	if err := os.WriteFile(frontend, script, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := compileFlow(context.Background(), flowCommandOptions{
		repository: repository, source: ".boatstack/flows/product-delivery.flow.ts", lock: "package-lock.json", frontend: frontend,
	}); err != nil {
		t.Fatal(err)
	}
	artifactRaw, err := os.ReadFile(filepath.Join(repository, ".boatstack", "flows", "product-delivery.flow.ir.json"))
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := controlprogram.LoadArtifact(bytes.NewReader(artifactRaw))
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.GeneratedSkills) != 5 {
		t.Fatalf("generated skills = %v", artifact.GeneratedSkills)
	}
	for path := range artifact.GeneratedSkills {
		if strings.Contains(path, "--") {
			t.Fatalf("artifact contains invalid skill path %s", path)
		}
	}
}

func TestFlowCompileRejectsDependencyLockProjectionOverlap(t *testing.T) {
	// control-law: compile-inputs-cannot-be-replaced-or-retired-by-their-own-projection
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	repository, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	documentRaw, err := json.Marshal(productDeliveryDocument("product-delivery"))
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := ".boatstack/flows/product-delivery.flow.ts"
	artifactPath := ".boatstack/flows/product-delivery.flow.ir.json"
	writeFixture(t, repository, ".git/keep", nil)
	writeFixture(t, repository, sourcePath, []byte("declarative source"))
	writeFixture(t, repository, "package-lock.json", []byte("lock"))
	writeFixture(t, repository, "raw-ir.json", documentRaw)
	frontend := filepath.Join(repository, "frontend.sh")
	if err := os.WriteFile(frontend, []byte("#!/bin/sh\ncat >/dev/null\ncat '"+filepath.Join(repository, "raw-ir.json")+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	options := flowCommandOptions{repository: repository, source: sourcePath, lock: "package-lock.json", frontend: frontend}
	if err := compileFlow(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(repository, filepath.FromSlash(artifactPath)))
	if err != nil {
		t.Fatal(err)
	}
	options.lock = artifactPath
	err = compileFlow(context.Background(), options)
	if err == nil || !strings.Contains(err.Error(), "FLOW_COMPILE_INPUT_OVERLAP") {
		t.Fatalf("overlapping lock result = %v", err)
	}
	after, err := os.ReadFile(filepath.Join(repository, filepath.FromSlash(artifactPath)))
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("overlapping compile changed artifact: %v", err)
	}
	if err := checkFlow(context.Background(), flowCommandOptions{repository: repository}); err != nil {
		t.Fatalf("preserved artifact no longer checks: %v", err)
	}
}

func TestFlowCompileRefusesUnmanagedGeneratedSkill(t *testing.T) {
	// control-law: first-compile-cannot-adopt-or-overwrite-unmanaged-skill-bytes
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	repository, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	documentRaw, err := json.Marshal(productDeliveryDocument("product-delivery"))
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, repository, ".git/keep", nil)
	writeFixture(t, repository, ".boatstack/flows/product-delivery.flow.ts", []byte("declarative source"))
	writeFixture(t, repository, "package-lock.json", []byte("lock"))
	writeFixture(t, repository, "raw-ir.json", documentRaw)
	skillPath := ".agents/skills/product-delivery-run/SKILL.md"
	writeFixture(t, repository, skillPath, []byte("user-owned skill"))
	frontend := filepath.Join(repository, "frontend.sh")
	script := []byte("#!/bin/sh\ncat >/dev/null\ncat '" + filepath.Join(repository, "raw-ir.json") + "'\n")
	if err := os.WriteFile(frontend, script, 0o700); err != nil {
		t.Fatal(err)
	}
	err = compileFlow(context.Background(), flowCommandOptions{
		repository: repository, source: ".boatstack/flows/product-delivery.flow.ts", lock: "package-lock.json", frontend: frontend,
	})
	if err == nil || !strings.Contains(err.Error(), "FLOW_PROJECTION_WRITE_UNAUTHORIZED") {
		t.Fatalf("unmanaged skill compile result = %v", err)
	}
	if actual, readErr := os.ReadFile(filepath.Join(repository, filepath.FromSlash(skillPath))); readErr != nil || string(actual) != "user-owned skill" {
		t.Fatalf("unmanaged skill changed: %q, %v", actual, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(repository, ".boatstack", "flows", "product-delivery.flow.ir.json")); !os.IsNotExist(statErr) {
		t.Fatalf("unmanaged skill compile published artifact: %v", statErr)
	}
}

func TestFlowCompileRejectsForgedArtifactOwnership(t *testing.T) {
	// control-law: repository-artifacts-cannot-grant-generated-output-ownership
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	repository, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	document := productDeliveryDocument("product-delivery")
	documentRaw, _ := json.Marshal(document)
	source, lock := []byte("declarative source"), []byte("lock")
	writeFixture(t, repository, ".git/keep", nil)
	writeFixture(t, repository, ".boatstack/flows/product-delivery.flow.ts", source)
	writeFixture(t, repository, "package-lock.json", lock)
	writeFixture(t, repository, "raw-ir.json", documentRaw)
	unrelatedPath := ".agents/skills/unrelated/SKILL.md"
	unrelated := []byte("user-owned skill")
	writeFixture(t, repository, unrelatedPath, unrelated)
	resolver, _ := softwareflow.NewResolver(context.Background())
	compiled, _ := controlprogram.Compile(document, resolver)
	skills, _ := softwareflow.GenerateSkills(compiled, []string{"codex", "claude"})
	for path, content := range skills {
		writeFixture(t, repository, path, content)
	}
	forged, _, err := controlprogram.NewArtifact(compiled, controlprogram.ArtifactInput{
		CompilerVersion: flowCompilerVersion, SourcePath: ".boatstack/flows/product-delivery.flow.ts", Source: source,
		DependencyLockPath: "package-lock.json", DependencyLock: lock, GeneratedSkills: skills,
	})
	if err != nil {
		t.Fatal(err)
	}
	forged.GeneratedSkills[unrelatedPath] = fileDigest(unrelated)
	forgedRaw, _ := json.Marshal(forged)
	writeFixture(t, repository, ".boatstack/flows/product-delivery.flow.ir.json", forgedRaw)
	frontend := filepath.Join(repository, "frontend.sh")
	if err := os.WriteFile(frontend, []byte("#!/bin/sh\ncat >/dev/null\ncat '"+filepath.Join(repository, "raw-ir.json")+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	err = compileFlow(context.Background(), flowCommandOptions{repository: repository, source: ".boatstack/flows/product-delivery.flow.ts", lock: "package-lock.json", frontend: frontend})
	if err == nil || !strings.Contains(err.Error(), "FLOW_PROJECTION") {
		t.Fatalf("forged ownership result = %v", err)
	}
	if actual, readErr := os.ReadFile(filepath.Join(repository, filepath.FromSlash(unrelatedPath))); readErr != nil || !bytes.Equal(actual, unrelated) {
		t.Fatalf("unrelated skill changed: %q, %v", actual, readErr)
	}
}

func TestFlowExecutionLeaseSerializesProjectionPublicationThroughEffect(t *testing.T) {
	// control-law: official-flow-publication-cannot-cross-apply-or-recovery
	repository, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, repository, ".git/keep", nil)
	lease, err := acquireFlowExecutionLease(surfaces.Request{ProgramID: "product-delivery", Operation: surfaces.OperationApply, Repository: repository})
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(repository, ".boatstack", "flows", "product-delivery.flow.ir.json")
	err = boatstackruntime.ApplyFlowProjection(repository, []boatstackruntime.ProjectionWrite{{Path: target, Content: []byte("program B"), Mode: 0o644}}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "FLOW_PROJECTION_BUSY") {
		t.Fatalf("publication during effect result = %v", err)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("blocked publication changed artifact: %v", statErr)
	}
	lease.Release()
}

func TestFlowValidationRejectsMissingProductionRecoveryClosure(t *testing.T) {
	// control-law: published-flows-close-recovery-in-the-production-composition
	document := productDeliveryDocument("product-delivery")
	available := flowKnown("preview_fingerprint")
	document.Operators[0] = controlprogram.Operator{ID: "publication.execute", Binding: &controlprogram.OperatorBinding{Reference: "software-delivery/publication.execute", Version: "1"}}
	document.Transitions[0] = controlprogram.Transition{
		ID: "publication.execute", Operator: "publication.execute", Guard: document.Transitions[0].Guard, Target: document.Transitions[0].Target, Priority: 77,
		Parameters: []controlprogram.TransitionParameterBinding{{Parameter: "preview_fingerprint", Producer: controlprogram.ParameterProducer{
			Kind: controlprogram.ParameterSourceState, Facet: "preview_fingerprint", AvailableWhen: &available,
		}}},
	}
	resolver, err := softwareflow.NewResolver(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolver.ResolveOperator("software-delivery/publication.execute", "1")
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	for _, facet := range document.Facets {
		declared[facet.ID] = true
	}
	for _, precondition := range resolved.StateEffect.Preconditions {
		if !declared[precondition.Facet] {
			document.Facets = append(document.Facets, controlprogram.Facet{ID: precondition.Facet, Kind: "string"})
			declared[precondition.Facet] = true
		}
	}
	for _, assignment := range resolved.StateEffect.Assignments {
		if !declared[assignment.Facet] {
			document.Facets = append(document.Facets, controlprogram.Facet{ID: assignment.Facet, Kind: "string"})
			declared[assignment.Facet] = true
		}
	}
	compiled, err := controlprogram.Compile(document, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSoftwareFlow(context.Background(), t.TempDir(), compiled, resolver); err == nil || !strings.Contains(err.Error(), "FLOW_RUNTIME_INVALID") {
		t.Fatalf("missing recovery closure result = %v", err)
	}
}

func TestFlowCompileRetiresProjectionWhenSourceChangesProgramID(t *testing.T) {
	// control-law: one-flow-source-owns-one-current-program-projection
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	repository, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := ".boatstack/flows/delivery.flow.ts"
	writeFixture(t, repository, ".git/keep", nil)
	writeFixture(t, repository, sourcePath, []byte("declarative source"))
	writeFixture(t, repository, "package-lock.json", []byte("lock"))
	frontend := filepath.Join(repository, "frontend.sh")
	if err := os.WriteFile(frontend, []byte("#!/bin/sh\ncat >/dev/null\ncat '"+filepath.Join(repository, "raw-ir.json")+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, programID := range []string{"foo", "bar"} {
		raw, _ := json.Marshal(productDeliveryDocument(programID))
		writeFixture(t, repository, "raw-ir.json", raw)
		if err := compileFlow(context.Background(), flowCommandOptions{repository: repository, source: sourcePath, lock: "package-lock.json", frontend: frontend}); err != nil {
			t.Fatalf("compile %s: %v", programID, err)
		}
	}
	for _, stale := range []string{".boatstack/flows/foo.flow.ir.json", ".agents/skills/foo-run/SKILL.md", ".agents/skills/foo-run/agents/openai.yaml", ".claude/skills/foo-run/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(repository, filepath.FromSlash(stale))); !os.IsNotExist(err) {
			t.Fatalf("stale projection remains at %s: %v", stale, err)
		}
	}
	if err := checkFlow(context.Background(), flowCommandOptions{repository: repository}); err != nil {
		t.Fatalf("renamed projection check failed: %v", err)
	}
}

func TestFlowCompileAndCheckRejectRuntimeInvalidSoftwareFlow(t *testing.T) {
	// control-law: compiled-and-checked-artifacts-are-admissible-by-the-production-adapter
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	document := productDeliveryDocument("product-delivery")
	document.Transitions[0].ID = "observe-alias"
	for _, operation := range []string{"compile", "check"} {
		t.Run(operation, func(t *testing.T) {
			repository, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			sourcePath, lockPath := ".boatstack/flows/product-delivery.flow.ts", "package-lock.json"
			source, lock := []byte("declarative source"), []byte("lock")
			writeFixture(t, repository, sourcePath, source)
			writeFixture(t, repository, lockPath, lock)
			if operation == "compile" {
				documentRaw, marshalErr := json.Marshal(document)
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				writeFixture(t, repository, "raw-ir.json", documentRaw)
				frontend := filepath.Join(repository, "frontend.sh")
				script := []byte("#!/bin/sh\ncat >/dev/null\ncat '" + filepath.Join(repository, "raw-ir.json") + "'\n")
				if err := os.WriteFile(frontend, script, 0o700); err != nil {
					t.Fatal(err)
				}
				err = compileFlow(context.Background(), flowCommandOptions{repository: repository, source: sourcePath, lock: lockPath, frontend: frontend})
				if err == nil || !strings.Contains(err.Error(), "FLOW_RUNTIME_INVALID") {
					t.Fatalf("runtime-invalid compile result = %v", err)
				}
				if _, statErr := os.Stat(filepath.Join(repository, ".boatstack", "flows", "product-delivery.flow.ir.json")); !os.IsNotExist(statErr) {
					t.Fatalf("runtime-invalid compile published an artifact: %v", statErr)
				}
				return
			}
			resolver, err := softwareflow.NewResolver(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			compiled, err := controlprogram.Compile(document, resolver)
			if err != nil {
				t.Fatal(err)
			}
			skills, err := softwareflow.GenerateSkills(compiled, []string{"codex", "claude"})
			if err != nil {
				t.Fatal(err)
			}
			for path, content := range skills {
				writeFixture(t, repository, path, content)
			}
			_, artifactRaw, err := controlprogram.NewArtifact(compiled, controlprogram.ArtifactInput{
				CompilerVersion: flowCompilerVersion, SourcePath: sourcePath, Source: source,
				DependencyLockPath: lockPath, DependencyLock: lock, GeneratedSkills: skills,
			})
			if err != nil {
				t.Fatal(err)
			}
			writeFixture(t, repository, ".boatstack/flows/product-delivery.flow.ir.json", artifactRaw)
			err = checkFlow(context.Background(), flowCommandOptions{repository: repository})
			if err == nil || !strings.Contains(err.Error(), "FLOW_RUNTIME_INVALID") {
				t.Fatalf("runtime-invalid check result = %v", err)
			}
		})
	}
}

func TestFlowCompileAndCheckRejectUnbindableEntryInputs(t *testing.T) {
	// control-law: artifact-admission-and-runtime-resolution-share-one-entry-input-contract
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	document := productDeliveryDocument("product-delivery")
	document.Entries[0].Inputs = nil
	for _, operation := range []string{"compile", "check"} {
		t.Run(operation, func(t *testing.T) {
			repository, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			sourcePath, lockPath := ".boatstack/flows/product-delivery.flow.ts", "package-lock.json"
			source, lock := []byte("declarative source"), []byte("lock")
			writeFixture(t, repository, sourcePath, source)
			writeFixture(t, repository, lockPath, lock)
			if operation == "compile" {
				documentRaw, marshalErr := json.Marshal(document)
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				writeFixture(t, repository, "raw-ir.json", documentRaw)
				frontend := filepath.Join(repository, "frontend.sh")
				script := []byte("#!/bin/sh\ncat >/dev/null\ncat '" + filepath.Join(repository, "raw-ir.json") + "'\n")
				if err := os.WriteFile(frontend, script, 0o700); err != nil {
					t.Fatal(err)
				}
				err = compileFlow(context.Background(), flowCommandOptions{repository: repository, source: sourcePath, lock: lockPath, frontend: frontend})
				if err == nil || !strings.Contains(err.Error(), "FLOW_RUNTIME_INVALID") {
					t.Fatalf("input-invalid compile result = %v", err)
				}
				if _, statErr := os.Stat(filepath.Join(repository, ".boatstack", "flows", "product-delivery.flow.ir.json")); !os.IsNotExist(statErr) {
					t.Fatalf("input-invalid compile published an artifact: %v", statErr)
				}
				return
			}
			resolver, err := softwareflow.NewResolver(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			compiled, err := controlprogram.Compile(document, resolver)
			if err != nil {
				t.Fatal(err)
			}
			skills, err := softwareflow.GenerateSkills(compiled, []string{"codex", "claude"})
			if err != nil {
				t.Fatal(err)
			}
			for path, content := range skills {
				writeFixture(t, repository, path, content)
			}
			_, artifactRaw, err := controlprogram.NewArtifact(compiled, controlprogram.ArtifactInput{
				CompilerVersion: flowCompilerVersion, SourcePath: sourcePath, Source: source,
				DependencyLockPath: lockPath, DependencyLock: lock, GeneratedSkills: skills,
			})
			if err != nil {
				t.Fatal(err)
			}
			writeFixture(t, repository, ".boatstack/flows/product-delivery.flow.ir.json", artifactRaw)
			err = checkFlow(context.Background(), flowCommandOptions{repository: repository})
			if err == nil || !strings.Contains(err.Error(), "FLOW_RUNTIME_INVALID") {
				t.Fatalf("input-invalid check result = %v", err)
			}
		})
	}
}

func TestFreshFlowEntryPreservesInboxProducerAcrossDelegationContext(t *testing.T) {
	// control-law: authority-suspension-cannot-switch-a-fresh-run-from-its-inbox-plan-to-prior-managed-input
	repository := flowRepository(t)
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery-one.md", []byte("exact plan"))
	initial, err := bindFlowEntry(context.Background(), commandOptions{repository: repository, programID: "product-delivery", entryID: "run", host: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(initial.runID, "run-") || initial.deliveryID != "delivery-one" || initial.targetID != "published-pr" || initial.trustedObjectiveClass != "open-or-updated-pr" || len(initial.parameters) != 0 {
		t.Fatalf("initial Flow context = %#v", initial)
	}
	writeFixture(t, repository, ".boatstack/plans/delivery-one.source", []byte("prior managed plan"))
	resumed, err := bindFlowEntry(context.Background(), commandOptions{
		repository: repository, programID: "product-delivery", entryID: "run", runID: initial.runID, host: "codex",
		deliveryID: initial.deliveryID, targetID: initial.targetID, objectiveID: initial.objectiveID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.runID != initial.runID {
		t.Fatalf("run identity changed: %s != %s", resumed.runID, initial.runID)
	}
	if source, ok := resumed.workInputs["plan"]; !ok || source.Value != filepath.Join(resumed.repository, ".boatstack", "plans", "inbox", "delivery-one.md") {
		t.Fatalf("resumed entry input = %#v, present=%t", source, ok)
	}
}

func TestActiveFlowEntryResumesManagedPlan(t *testing.T) {
	// control-law: only a durably active Flow may resume through its materialized plan
	repository := flowRepository(t)
	repository, err := filepath.EvalSymlinks(repository)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery-one.md", []byte("inbox plan"))
	managed := filepath.Join(repository, ".boatstack", "plans", "delivery-one.source")
	writeFixture(t, repository, ".boatstack/plans/delivery-one.source", []byte("active managed plan"))
	plan, deliveryID, err := resolveBoundPlan(repository, controlprogram.Entry{ID: "run"}, softwareflow.EntryObjective{}, commandOptions{
		activeFlowBound: true,
		deliveryID:      "delivery-one",
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan != managed || deliveryID != "delivery-one" {
		t.Fatalf("active plan = %q delivery = %q; want %q and delivery-one", plan, deliveryID, managed)
	}
}

func TestExplainUsesFlowContextAndCreatesNoStateEffectOrReceipt(t *testing.T) {
	// control-law: controller-observation-cannot-change-controller
	repository := flowRepository(t)
	runFlowGit(t, repository, "init")
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery-one.md", []byte("private plan text must not appear"))
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "fixture")
	before := repositoryBytes(t, repository)
	output, runErr := captureRunOutput(t, "explain", "--repo", repository, "--flow", "product-delivery", "--entry", "run", "--host", "codex", "--format", "json")
	if runErr != nil {
		t.Fatalf("explain failed: %v\n%s", runErr, output)
	}
	var response surfaces.Response
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatalf("decode explain response: %v\n%s", err, output)
	}
	if response.Operation != surfaces.OperationExplain || response.Trace == nil || response.RunID == "" || response.ProgramID != "product-delivery" || response.EntryID != "run" {
		t.Fatalf("explain context = %#v", response)
	}
	if response.Snapshot != nil || response.Prescription != nil || response.Admission != nil || response.Receipt != nil {
		t.Fatalf("explain exposed mutable or raw runtime payloads: %#v", response)
	}
	if strings.Contains(string(output), "private plan text must not appear") {
		t.Fatalf("explain leaked raw repository input: %s", output)
	}
	textOutput, textErr := captureRunOutput(t, "explain", "--repo", repository, "--flow", "product-delivery", "--entry", "run", "--host", "codex", "--format", "text")
	if textErr != nil || !strings.Contains(string(textOutput), "No effect was executed.") || strings.Contains(string(textOutput), "private plan text must not appear") {
		t.Fatalf("text explanation is unsafe or incomplete: %v\n%s", textErr, textOutput)
	}
	after := repositoryBytes(t, repository)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("explain changed repository state:\nbefore=%v\nafter=%v", before, after)
	}
}

func repositoryBytes(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() && relative == ".git" {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(relative)] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result[".git/@semantic/HEAD"] = runFlowGitOutput(t, root, "rev-parse", "--verify", "HEAD")
	result[".git/@semantic/symbolic-HEAD"] = runFlowGitOutput(t, root, "symbolic-ref", "--quiet", "HEAD")
	result[".git/@semantic/refs"] = runFlowGitOutput(t, root, "for-each-ref", "--format=%(refname)%09%(objectname)")
	result[".git/@semantic/index"] = runFlowGitOutput(t, root, "ls-files", "--stage")
	return result
}

func TestRepositoryBytesExcludesGitInternals(t *testing.T) {
	repository := t.TempDir()
	runFlowGit(t, repository, "init")
	writeFixture(t, repository, ".git/objects/maintenance.lock", []byte("transient"))
	writeFixture(t, repository, ".boatstack/controller.json", []byte("managed"))
	writeFixture(t, repository, "README.md", []byte("repository"))
	runFlowGit(t, repository, "add", ".boatstack/controller.json", "README.md")
	runFlowGit(t, repository, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "fixture")

	snapshot := repositoryBytes(t, repository)
	if _, exists := snapshot[".git/objects/maintenance.lock"]; exists {
		t.Fatal("repository snapshot included transient Git internals")
	}
	if snapshot[".boatstack/controller.json"] != "managed" || snapshot["README.md"] != "repository" {
		t.Fatalf("repository snapshot omitted managed or ordinary files: %#v", snapshot)
	}
	if snapshot[".git/@semantic/HEAD"] == "" || snapshot[".git/@semantic/symbolic-HEAD"] == "" || snapshot[".git/@semantic/refs"] == "" || snapshot[".git/@semantic/index"] == "" {
		t.Fatalf("repository snapshot omitted semantic Git state: %#v", snapshot)
	}
}

func TestRepositoryBytesDetectsSemanticGitMutation(t *testing.T) {
	repository := t.TempDir()
	runFlowGit(t, repository, "init")
	writeFixture(t, repository, "README.md", []byte("repository"))
	runFlowGit(t, repository, "add", "README.md")
	runFlowGit(t, repository, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "fixture")

	before := repositoryBytes(t, repository)
	runFlowGit(t, repository, "update-ref", "refs/heads/semantic-test", "HEAD")
	after := repositoryBytes(t, repository)
	if reflect.DeepEqual(before, after) {
		t.Fatal("repository snapshot missed semantic Git ref mutation")
	}
}

func TestRepositoryBytesDetectsSymbolicHeadMutation(t *testing.T) {
	repository := t.TempDir()
	runFlowGit(t, repository, "init")
	writeFixture(t, repository, "README.md", []byte("repository"))
	runFlowGit(t, repository, "add", "README.md")
	runFlowGit(t, repository, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "fixture")
	runFlowGit(t, repository, "branch", "same-commit")

	before := repositoryBytes(t, repository)
	runFlowGit(t, repository, "symbolic-ref", "HEAD", "refs/heads/same-commit")
	after := repositoryBytes(t, repository)
	if reflect.DeepEqual(before, after) {
		t.Fatal("repository snapshot missed symbolic HEAD mutation")
	}
}

func TestRepositoryBytesDetectsIndexOnlyMutation(t *testing.T) {
	repository := t.TempDir()
	runFlowGit(t, repository, "init")
	writeFixture(t, repository, "README.md", []byte("repository"))
	runFlowGit(t, repository, "add", "README.md")
	runFlowGit(t, repository, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "fixture")

	before := repositoryBytes(t, repository)
	writeFixture(t, repository, "README.md", []byte("staged"))
	runFlowGit(t, repository, "add", "README.md")
	writeFixture(t, repository, "README.md", []byte("repository"))
	after := repositoryBytes(t, repository)
	if reflect.DeepEqual(before, after) {
		t.Fatal("repository snapshot missed index-only mutation")
	}
}

// Legacy transition-specific parameter rebinding was removed. Repository Flows now
// materialize only compiled producer declarations.

func TestCommittedActiveRunRehydratesExactDeliveryWhenRunIDIsSupplied(t *testing.T) {
	// control-law: a resumed run resolves inputs from its committed delivery before selecting work
	active := model.Objective{
		ID: "objective-product-delivery-run-delivery-one", TargetID: "published-pr",
		TrustedClass: model.ObjectiveOpenPR, DeliveryID: "delivery-one",
	}
	receipt := protocol.TransitionReceipt{FlowID: "run-committed"}
	bound, err := bindCommittedActiveRun(commandOptions{runID: "run-committed"}, active, receipt)
	if err != nil {
		t.Fatal(err)
	}
	if !bound.activeFlowBound || bound.deliveryID != "delivery-one" || bound.objectiveID != active.ID || bound.targetID != "published-pr" || bound.trustedObjectiveClass != string(model.ObjectiveOpenPR) {
		t.Fatalf("active run was not rehydrated: %#v", bound)
	}
	if _, err := bindCommittedActiveRun(commandOptions{runID: "run-other"}, active, receipt); err == nil || !strings.Contains(err.Error(), "FLOW_RUN_MISMATCH") {
		t.Fatalf("conflicting run identity result = %v", err)
	}
}

func TestCommittedActiveRunIdentitySurvivesApprovedPlanTransformation(t *testing.T) {
	// control-law: trusted plan transformation cannot rename the already-committed run
	active := commandOptions{entryID: "run", runID: "run-committed", activeFlowBound: true}
	bound, err := bindSelectedPlanRun(active, filepath.Join(t.TempDir(), "not-a-repository"), strings.Repeat("a", 64), "delivery", strings.Repeat("b", 64))
	if err != nil || bound.runID != "run-committed" {
		t.Fatalf("active run transformation result = %#v err=%v", bound, err)
	}
}

func TestRepositoryNamedAbandonmentEntryUsesCompiledObjective(t *testing.T) {
	entry := controlprogram.Entry{ID: "cancel", Target: "safely-abandoned"}
	plan, delivery, err := resolveBoundPlan(t.TempDir(), entry, softwareflow.EntryObjective{
		TargetID: model.TargetID("safely-abandoned"), TrustedClass: model.ObjectiveAbandoned,
	}, commandOptions{
		entryID: "cancel", activeFlowBound: true, deliveryID: "delivery-one",
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan != "" || delivery != "delivery-one" {
		t.Fatalf("repository-named abandonment resolved plan=%q delivery=%q", plan, delivery)
	}
}

func TestAbandonmentEntryCanReplacePreFlowActiveObjectiveWithoutReceipt(t *testing.T) {
	// control-law: a trusted durable objective that predates Flow receipts can
	// be abandoned in the same repository without deleting controller state.
	repository := t.TempDir()
	runFlowGit(t, repository, "init", "-q")
	document := productDeliveryDocument("product-delivery")
	truth := true
	document.Facets = append(document.Facets,
		controlprogram.Facet{ID: "delivery", Kind: "string"},
		controlprogram.Facet{ID: "workspace", Kind: "string"},
	)
	document.Operators = append(document.Operators, controlprogram.Operator{
		ID: "plan.abandon", Binding: &controlprogram.OperatorBinding{Reference: "software-delivery/plan.abandon", Version: "1"},
	})
	document.Transitions = append(document.Transitions, controlprogram.Transition{
		ID: "plan.abandon", Operator: "plan.abandon", Guard: controlprogram.Predicate{True: &truth}, Target: controlprogram.Predicate{True: &truth}, Priority: 31,
	})
	document.Targets = append(document.Targets, controlprogram.Target{ID: "safely-abandoned", Predicate: controlprogram.Predicate{All: []controlprogram.Predicate{
		flowFact("delivery", "discarded"),
		{Fact: &controlprogram.FactPredicate{Facet: "workspace", Statuses: []string{"known"}, Values: []string{"abandoned", "absent"}}},
	}}})
	document.Entries = append(document.Entries, controlprogram.Entry{
		ID: "abandon", Target: "safely-abandoned",
		Inputs: []controlprogram.EntryInput{{
			ID: "plan", Type: "markdown-file", Required: true, Resolver: "software-delivery.plan-inbox",
			Config: json.RawMessage(`{"path":".boatstack/plans/inbox","cardinality":"exactly-one"}`),
		}},
	})
	sourcePath, lockPath := ".boatstack/flows/product-delivery.flow.ts", "package-lock.json"
	source, lock := []byte("flow source"), []byte("lock")
	writeFixture(t, repository, sourcePath, source)
	writeFixture(t, repository, lockPath, lock)
	writeFlowArtifact(t, repository, document, sourcePath, source, lockPath, lock)
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "fixture")

	resolver, err := plant.NewResolver("")
	if err != nil {
		t.Fatal(err)
	}
	invoking, err := resolver.ResolveInvocation(context.Background(), repository, "codex", "pre-flow-active-objective")
	if err != nil {
		t.Fatal(err)
	}
	layout, invoking, err := resolver.ResolveLayout(context.Background(), invoking)
	if err != nil {
		t.Fatal(err)
	}
	state := durable.Default(invoking, time.Now().UTC())
	state.ProgramFingerprint = strings.Repeat("a", 64)
	state.Revision = 9
	state.Phase = model.PhaseActive
	state.Engagement = model.EngagementCommand
	state.Delivery = model.DeliveryActive
	state.Objective = model.Objective{
		ID: "objective-product-delivery-run-delivery-one", TargetID: "published-pr",
		TrustedClass: model.ObjectiveOpenPR, DeliveryID: "delivery-one",
	}
	raw, err := durable.EncodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(layout.StatePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.StatePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	bound, err := bindFlowEntry(context.Background(), commandOptions{
		repository: repository, programID: "product-delivery", entryID: "abandon", host: "codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bound.activeFlowBound || !strings.HasPrefix(bound.runID, "run-") || bound.deliveryID != "delivery-one" || bound.targetID != "safely-abandoned" || bound.trustedObjectiveClass != string(model.ObjectiveAbandoned) {
		t.Fatalf("pre-Flow abandonment binding = %#v", bound)
	}
	rebound, err := bindFlowEntry(context.Background(), commandOptions{
		repository: repository, programID: "product-delivery", entryID: "abandon", host: "codex", runID: bound.runID,
	})
	if err != nil || rebound.runID != bound.runID {
		t.Fatalf("stable abandonment binding = %#v err=%v", rebound, err)
	}
}

func TestFlowEntryRejectsSelectedPlanContentSubstitution(t *testing.T) {
	// control-law: one-flow-run-binds-the-exact-selected-plan-bytes
	repository := flowRepository(t)
	planPath := ".boatstack/plans/inbox/delivery-one.md"
	writeFixture(t, repository, planPath, []byte("plan A"))
	initial, err := bindFlowEntry(context.Background(), commandOptions{repository: repository, programID: "product-delivery", entryID: "run", host: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, repository, planPath, []byte("plan B"))
	_, err = bindFlowEntry(context.Background(), commandOptions{
		repository: repository, programID: "product-delivery", entryID: "run", runID: initial.runID, host: "codex",
		deliveryID: initial.deliveryID, targetID: initial.targetID, objectiveID: initial.objectiveID,
	})
	if err == nil || !strings.Contains(err.Error(), "FLOW_RUN_MISMATCH") {
		t.Fatalf("plan substitution result = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(repository, ".boatstack", "plans", "delivery-one.source")); !os.IsNotExist(statErr) {
		t.Fatalf("plan substitution produced a managed source: %v", statErr)
	}
}

func TestFlowEntryPreservesSelectedPlanFilenameBeforeMaterialization(t *testing.T) {
	// control-law: an-admitted-plan-filename-remains-resolvable-for-the-same-run
	repository := flowRepository(t)
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery.MD", []byte("exact plan"))
	initial, err := bindFlowEntry(context.Background(), commandOptions{repository: repository, programID: "product-delivery", entryID: "run", host: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := bindFlowEntry(context.Background(), commandOptions{
		repository: repository, programID: "product-delivery", entryID: "run", runID: initial.runID, host: "codex",
		deliveryID: initial.deliveryID, targetID: initial.targetID, objectiveID: initial.objectiveID,
	})
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(initial.repository, ".boatstack", "plans", "inbox", "delivery.MD")
	if source, ok := resumed.workInputs["plan"]; !ok || source.Value != expected {
		t.Fatalf("resumed entry input = %#v, present=%t; want %q", source, ok, expected)
	}
}

func TestFlowEntryResumeIgnoresUnrelatedNewInboxPlan(t *testing.T) {
	// control-law: one-flow-run-retains-its-selected-plan-identity-before-materialization
	repository := flowRepository(t)
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery.md", []byte("selected plan"))
	initial, err := bindFlowEntry(context.Background(), commandOptions{repository: repository, programID: "product-delivery", entryID: "run", host: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, repository, ".boatstack/plans/inbox/unrelated.md", []byte("different plan"))
	resumed, err := bindFlowEntry(context.Background(), commandOptions{
		repository: repository, programID: "product-delivery", entryID: "run", runID: initial.runID, host: "codex",
		deliveryID: initial.deliveryID, targetID: initial.targetID, objectiveID: initial.objectiveID,
	})
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(initial.repository, ".boatstack", "plans", "inbox", "delivery.md")
	if source, ok := resumed.workInputs["plan"]; !ok || source.Value != expected {
		t.Fatalf("resumed entry input = %#v, present=%t; want %q", source, ok, expected)
	}
}

func TestFlowEntryRejectsAmbiguousPlanFilenameOnResume(t *testing.T) {
	// control-law: a-run-cannot-resume-through-a-different-case-colliding-plan-identity
	repository := flowRepository(t)
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery.MD", []byte("selected plan"))
	initial, err := bindFlowEntry(context.Background(), commandOptions{repository: repository, programID: "product-delivery", entryID: "run", host: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery.md", []byte("different plan"))
	entries, err := os.ReadDir(filepath.Join(repository, ".boatstack", "plans", "inbox"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Skip("filesystem does not preserve case-colliding filenames")
	}
	_, err = bindFlowEntry(context.Background(), commandOptions{
		repository: repository, programID: "product-delivery", entryID: "run", runID: initial.runID, host: "codex",
		deliveryID: initial.deliveryID, targetID: initial.targetID, objectiveID: initial.objectiveID,
	})
	if err == nil || !strings.Contains(err.Error(), "FLOW_INPUT_INVALID") {
		t.Fatalf("ambiguous resume result = %v", err)
	}
}

func TestFlowEntryRejectsObjectiveSubstitutionWithinRun(t *testing.T) {
	// control-law: one-flow-run-retains-one-exact-product-objective
	repository := flowRepository(t)
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery-one.md", []byte("plan"))
	initial, err := bindFlowEntry(context.Background(), commandOptions{repository: repository, programID: "product-delivery", entryID: "run", host: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = bindFlowEntry(context.Background(), commandOptions{
		repository: repository, programID: "product-delivery", entryID: "run", host: "codex",
		runID: initial.runID, deliveryID: initial.deliveryID, targetID: initial.targetID,
		objectiveID: "objective-substituted",
	})
	if err == nil || !strings.Contains(err.Error(), "FLOW_CONTEXT_MISMATCH") {
		t.Fatalf("objective substitution result = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(repository, ".boatstack", "state.json")); !os.IsNotExist(statErr) {
		t.Fatalf("objective substitution created managed state: %v", statErr)
	}
}

func TestFlowEntryRejectsManagedPlanSymlinkEscape(t *testing.T) {
	// control-law: resumed-run-plan-remains-a-regular-repository-file
	repository := flowRepository(t)
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery-one.md", []byte("exact plan"))
	initial, err := bindFlowEntry(context.Background(), commandOptions{repository: repository, programID: "product-delivery", entryID: "run", host: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(external, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(repository, ".boatstack", "plans", "delivery-one.source")
	if err := os.MkdirAll(filepath.Dir(managed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, managed); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err = bindFlowEntry(context.Background(), commandOptions{
		repository: repository, programID: "product-delivery", entryID: "run", runID: initial.runID, host: "codex",
		deliveryID: initial.deliveryID, targetID: initial.targetID, objectiveID: initial.objectiveID,
		activeFlowBound: true,
	})
	if err == nil || !strings.Contains(err.Error(), "regular non-symlink") {
		t.Fatalf("managed symlink result = %v", err)
	}
}

func TestFlowKernelRejectsArtifactChangedAfterEntryBinding(t *testing.T) {
	// control-law: run-entry-kernel-and-receipts-bind-one-exact-program-artifact
	repository := flowRepository(t)
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery-one.md", []byte("plan"))
	bound, err := bindFlowEntry(context.Background(), commandOptions{repository: repository, programID: "product-delivery", entryID: "run", host: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	request, err := buildRequest(surfaces.OperationResolve, bound)
	if err != nil {
		t.Fatal(err)
	}
	changed := productDeliveryDocument("product-delivery")
	changed.Transitions[0].Priority++
	writeFlowArtifact(t, repository, changed, ".boatstack/flows/product-delivery.flow.ts", []byte("flow source"), "package-lock.json", []byte("lock"))
	if _, err := standardKernel(context.Background(), request); err == nil || !strings.Contains(err.Error(), "FLOW_PROGRAM_DRIFT") {
		t.Fatalf("artifact drift result = %v", err)
	}
	for _, path := range []string{".boatstack/state.json", ".boatstack/receipts"} {
		if _, statErr := os.Stat(filepath.Join(repository, filepath.FromSlash(path))); !os.IsNotExist(statErr) {
			t.Fatalf("artifact drift created managed output %s: %v", path, statErr)
		}
	}
}

func TestFlowEntryRejectsPlanInboxSymlinkEscape(t *testing.T) {
	// control-law: repository-input-resolution-cannot-follow-an-external-inbox
	repository := flowRepository(t)
	external := t.TempDir()
	writeFixture(t, external, "outside.md", []byte("outside"))
	inbox := filepath.Join(repository, ".boatstack", "plans", "inbox")
	if err := os.MkdirAll(filepath.Dir(inbox), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, inbox); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := bindFlowEntry(context.Background(), commandOptions{repository: repository, programID: "product-delivery", entryID: "run", host: "codex"})
	if err == nil || !strings.Contains(err.Error(), "escapes the repository") {
		t.Fatalf("external inbox result = %v", err)
	}
}

func TestFlowCompileRetiresOnlyUnmodifiedPriorGeneratedSkills(t *testing.T) {
	// control-law: removing-an-entry-cannot-leave-a-stale-authority-bearing-skill
	repository, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, repository, ".git/keep", nil)
	retained := []byte("retained")
	retired := []byte("retired")
	retainedPath := ".agents/skills/program-keep/SKILL.md"
	retiredPath := ".agents/skills/program-remove/SKILL.md"
	writeFixture(t, repository, retainedPath, retained)
	writeFixture(t, repository, retiredPath, retired)
	artifact := controlprogram.Artifact{
		Schema: controlprogram.ArtifactSchemaName, SchemaRevision: controlprogram.ArtifactSchemaRevision, CompilerVersion: flowCompilerVersion,
		SourcePath: ".boatstack/flows/program.flow.ts", SourceSHA256: strings.Repeat("a", 64),
		DependencyLockPath: "package-lock.json", DependencyLockSHA256: strings.Repeat("b", 64),
		ProgramFingerprint: strings.Repeat("c", 64),
		GeneratedSkills:    map[string]string{retainedPath: fileDigest(retained), retiredPath: fileDigest(retired)},
	}
	raw, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	artifactPath := filepath.Join(repository, ".boatstack", "flows", "program.flow.ir.json")
	writeFixture(t, repository, ".boatstack/flows/program.flow.ir.json", raw)
	sourcePath := ".boatstack/flows/program.flow.ts"
	priorOwnership, err := boatstackruntime.LoadFlowProjectionOwnership(repository, sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	ownership := boatstackruntime.NewFlowProjectionOwnership(sourcePath, ".boatstack/flows/program.flow.ir.json", raw, map[string][]byte{retainedPath: retained, retiredPath: retired})
	if err := boatstackruntime.ApplyOwnedFlowProjection(repository, []boatstackruntime.ProjectionWrite{
		{Path: filepath.Join(repository, filepath.FromSlash(retainedPath)), Content: retained, Mode: 0o600},
		{Path: filepath.Join(repository, filepath.FromSlash(retiredPath)), Content: retired, Mode: 0o600},
		{Path: artifactPath, Content: raw, Mode: 0o600, PublishLast: true},
	}, nil, nil, priorOwnership, ownership); err != nil {
		t.Fatal(err)
	}
	paths, artifactPrevious, priorSkills, _, err := ownedProjectionChanges(repository, sourcePath, artifactPath, map[string]string{retainedPath: fileDigest(retained)})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0].Path != filepath.Join(repository, filepath.FromSlash(retiredPath)) || artifactPrevious != fileDigest(raw) {
		t.Fatalf("retired paths = %v", paths)
	}
	if priorSkills[retainedPath] != fileDigest(retained) || priorSkills[retiredPath] != fileDigest(retired) {
		t.Fatalf("prior generated skills = %v", priorSkills)
	}
	writeFixture(t, repository, retiredPath, []byte("user changed"))
	paths, _, _, _, err = ownedProjectionChanges(repository, sourcePath, artifactPath, map[string]string{retainedPath: fileDigest(retained)})
	if err != nil || len(paths) != 1 {
		t.Fatalf("owned retirement plan = %v, %v", paths, err)
	}
	if err := os.Remove(filepath.Join(repository, filepath.FromSlash(retiredPath))); err != nil {
		t.Fatal(err)
	}
	paths, _, _, _, err = ownedProjectionChanges(repository, sourcePath, artifactPath, map[string]string{retainedPath: fileDigest(retained)})
	if err != nil || len(paths) != 1 || !paths[0].AllowMissing {
		t.Fatalf("interrupted retirement was not retryable: %v, %v", paths, err)
	}
}

func TestDelegationIsRequiredAndRevocationWinsBetweenNextAndApply(t *testing.T) {
	// control-law: repository-declaration-cannot-self-grant-and-apply-reloads-revocation-before-effects
	repository := t.TempDir()
	runFlowGit(t, repository, "init", "-q")
	runFlowGit(t, repository, "config", "user.email", "test@example.com")
	runFlowGit(t, repository, "config", "user.name", "Test User")
	runFlowGit(t, repository, "config", "core.autocrlf", "true")
	document := productDeliveryDocument("product-delivery")
	document.Entries[0].Delegation = &controlprogram.DelegationBinding{Reference: "software-delivery/delegation/autonomy", Version: "1"}
	sourcePath, lockPath := ".boatstack/flows/product-delivery.flow.ts", "package-lock.json"
	source, dependencyLock := []byte("flow source"), []byte("lock")
	writeFixture(t, repository, sourcePath, source)
	writeFixture(t, repository, lockPath, dependencyLock)
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery.md", []byte("# Delivery"))
	writeFlowArtifact(t, repository, document, sourcePath, source, lockPath, dependencyLock)
	writeFixture(t, repository, "README.md", []byte("fixture\n"))
	runFlowGit(t, repository, "add", ".")
	runFlowGit(t, repository, "commit", "-q", "-m", "fixture")
	t.Setenv("BOATSTACK_STATE_ROOT", t.TempDir())

	bound, err := bindFlowEntry(context.Background(), commandOptions{repository: repository, programID: "product-delivery", entryID: "run", host: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	request, err := buildRequest(surfaces.OperationResolve, bound)
	if err != nil {
		t.Fatal(err)
	}
	lock, suspension, err := prepareDelegation(context.Background(), &request)
	if err != nil || lock != nil || suspension == nil || suspension.Delegation == nil || suspension.Delegation.Code != "DELEGATION_REQUIRED" || suspension.Delegation.RequestFingerprint != bound.delegationRequestFingerprint {
		t.Fatalf("delegation suspension = lock=%v response=%#v err=%v", lock, suspension, err)
	}
	explainRequest, err := buildRequest(surfaces.OperationExplain, bound)
	if err != nil {
		t.Fatal(err)
	}
	explainLock, explainSuspension, err := prepareDelegation(context.Background(), &explainRequest)
	if err != nil || explainLock != nil || explainSuspension != nil || explainRequest.Authority.Set(time.Now().UTC())[catalog.AuthorityAutonomy] {
		t.Fatalf("explain created missing delegation authority: lock=%v response=%#v authority=%#v err=%v", explainLock, explainSuspension, explainRequest.Authority, err)
	}

	resolver, err := plant.NewResolver("")
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := resolver.ResolveInvocation(context.Background(), repository, "codex", "authorize-test")
	if err != nil {
		t.Fatal(err)
	}
	layout, _, err := resolver.ResolveLayout(context.Background(), invocation)
	if err != nil {
		t.Fatal(err)
	}
	recordPath, err := delegation.Path(layout.FlowRoot, bound.runID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(recordPath); !os.IsNotExist(err) {
		t.Fatalf("explain created a delegation record: %v", err)
	}
	now := time.Now().UTC()
	record := delegation.Record{Schema: delegation.Schema, SchemaRevision: delegation.SchemaRevision, Request: bound.delegationRequest, RequestFingerprint: bound.delegationRequestFingerprint, ReceiptID: "authorization-test", Actor: "human@example.com", AuthorizedAt: now, Revision: 1, Status: "active"}
	if err := effects.StoreDelegationRecord(recordPath, record); err != nil {
		t.Fatal(err)
	}
	recordBeforeExplain, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	explainRequest, err = buildRequest(surfaces.OperationExplain, bound)
	if err != nil {
		t.Fatal(err)
	}
	explainLock, explainSuspension, err = prepareDelegation(context.Background(), &explainRequest)
	if err != nil || explainLock != nil || explainSuspension != nil || !explainRequest.Authority.Set(time.Now().UTC())[catalog.AuthorityAutonomy] {
		t.Fatalf("explain did not project existing delegation: lock=%v response=%#v authority=%#v err=%v", explainLock, explainSuspension, explainRequest.Authority, err)
	}
	recordAfterExplain, err := os.ReadFile(recordPath)
	if err != nil || !bytes.Equal(recordBeforeExplain, recordAfterExplain) {
		t.Fatalf("explain changed delegation record: %v", err)
	}
	lock, suspension, err = prepareDelegation(context.Background(), &request)
	if err != nil || lock != nil || suspension != nil || !request.Authority.Set(time.Now().UTC())[catalog.AuthorityAutonomy] {
		t.Fatalf("authorized resolve = lock=%v response=%#v authority=%#v err=%v", lock, suspension, request.Authority, err)
	}
	refreshedApply, _, err := refreshFlowInvocation(context.Background(), surfaces.OperationApply, request, bound)
	if err != nil {
		t.Fatal(err)
	}
	if !refreshedApply.Authority.Set(time.Now().UTC())[catalog.AuthorityAutonomy] || !reflect.DeepEqual(refreshedApply.Authority, request.Authority) {
		t.Fatalf("direct CLI refresh dropped admitted delegation authority:\nprior=%#v\nfresh=%#v", request.Authority, refreshedApply.Authority)
	}
	var replayedReceipt protocol.AuthorityReceipt
	for _, receipt := range request.Authority.Receipts {
		if strings.HasPrefix(receipt.ID, "delegation-") {
			replayedReceipt = receipt
			break
		}
	}
	if replayedReceipt.ID == "" {
		t.Fatal("authorized resolve did not materialize a delegation receipt")
	}
	if err := os.Remove(recordPath); err != nil {
		t.Fatal(err)
	}
	replayedExplain, err := buildRequest(surfaces.OperationExplain, bound)
	if err != nil {
		t.Fatal(err)
	}
	replayedExplain.Authority.Receipts = append(replayedExplain.Authority.Receipts, replayedReceipt)
	replayedLock, replayedSuspension, err := prepareDelegation(context.Background(), &replayedExplain)
	if err != nil || replayedLock != nil || replayedSuspension != nil || replayedExplain.Authority.Set(time.Now().UTC())[catalog.AuthorityAutonomy] {
		t.Fatalf("missing-record explain replayed delegation authority: lock=%v response=%#v authority=%#v err=%v", replayedLock, replayedSuspension, replayedExplain.Authority, err)
	}
	replayedKernel, err := standardKernel(context.Background(), replayedExplain)
	if err != nil {
		t.Fatal(err)
	}
	replayedResponse, err := replayedKernel.Handle(context.Background(), replayedExplain)
	if err != nil || replayedResponse.Decision == nil || replayedResponse.Decision.Kind != supervisor.DecisionFrontier {
		t.Fatalf("missing-record explain decision = %#v, err=%v", replayedResponse.Decision, err)
	}
	if _, err := os.Stat(recordPath); !os.IsNotExist(err) {
		t.Fatalf("missing-record explain recreated delegation: %v", err)
	}
	if err := effects.StoreDelegationRecord(recordPath, record); err != nil {
		t.Fatal(err)
	}
	record.ExpiresAt = now.Add(-time.Second)
	if err := effects.StoreDelegationRecord(recordPath, record); err != nil {
		t.Fatal(err)
	}
	if expiredLock, expiredSuspension, expiredErr := prepareDelegation(context.Background(), &request); expiredLock != nil || expiredSuspension != nil || expiredErr == nil || !strings.Contains(expiredErr.Error(), "DELEGATION_EXPIRED") {
		t.Fatalf("expired delegation = lock=%v response=%#v err=%v", expiredLock, expiredSuspension, expiredErr)
	}
	renewedAt := time.Now().UTC()
	renewed, changed, err := authorizeDelegation(&record, bound.delegationRequest, bound.delegationRequestFingerprint, record.Actor, time.Hour, renewedAt, false)
	if err != nil || !changed || renewed.Revision != record.Revision+1 || renewed.ReceiptID == record.ReceiptID || !renewed.ExpiresAt.Equal(renewedAt.Add(time.Hour)) {
		t.Fatalf("renewed delegation = record=%#v changed=%v err=%v", renewed, changed, err)
	}
	if idempotent, changedAgain, idempotentErr := authorizeDelegation(&renewed, bound.delegationRequest, bound.delegationRequestFingerprint, record.Actor, time.Hour, renewedAt.Add(time.Second), false); idempotentErr != nil || changedAgain || idempotent.ReceiptID != renewed.ReceiptID {
		t.Fatalf("idempotent renewal = record=%#v changed=%v err=%v", idempotent, changedAgain, idempotentErr)
	}
	if _, _, conflictErr := authorizeDelegation(&renewed, bound.delegationRequest, bound.delegationRequestFingerprint, "other-actor", time.Hour, renewedAt, false); conflictErr == nil || !strings.Contains(conflictErr.Error(), "DELEGATION_CONFLICT") {
		t.Fatalf("conflicting renewal = %v", conflictErr)
	}
	if err := effects.StoreDelegationRecord(recordPath, renewed); err != nil {
		t.Fatal(err)
	}
	record = renewed
	lock, suspension, err = prepareDelegation(context.Background(), &request)
	if err != nil || lock != nil || suspension != nil || !request.Authority.Set(time.Now().UTC())[catalog.AuthorityAutonomy] {
		t.Fatalf("renewed resolve = lock=%v response=%#v authority=%#v err=%v", lock, suspension, request.Authority, err)
	}
	otherWorktree := filepath.Join(t.TempDir(), "other-worktree")
	runFlowGit(t, repository, "worktree", "add", "-q", "-b", "other-worktree", otherWorktree)
	if _, otherErr := bindFlowEntry(context.Background(), commandOptions{repository: otherWorktree, programID: "product-delivery", entryID: "run", runID: bound.runID, deliveryID: bound.deliveryID, host: "codex"}); otherErr == nil || !strings.Contains(otherErr.Error(), "DELEGATION_DRIFT") {
		t.Fatalf("unauthorized worktree bundle was not rejected: %v", otherErr)
	}
	runFlowGit(t, repository, "checkout", "-q", "-b", "changed-ref")
	refBound, err := bindFlowEntry(context.Background(), commandOptions{repository: repository, programID: "product-delivery", entryID: "run", runID: bound.runID, deliveryID: bound.deliveryID, host: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	refRequest, err := buildRequest(surfaces.OperationResolve, refBound)
	if err != nil {
		t.Fatal(err)
	}
	refLock, refSuspension, refErr := prepareDelegation(context.Background(), &refRequest)
	if refLock != nil || refSuspension != nil || refErr == nil || !strings.Contains(refErr.Error(), "DELEGATION_CONTEXT_UNAUTHORIZED") {
		t.Fatalf("unauthorized ref = lock=%v response=%#v err=%v", refLock, refSuspension, refErr)
	}
	runFlowGit(t, repository, "checkout", "-q", strings.TrimPrefix(record.Request.InitialRef, "refs/heads/"))

	request.Operation = surfaces.OperationApply
	lock, suspension, err = prepareDelegation(context.Background(), &request)
	if err != nil || lock == nil || suspension != nil {
		t.Fatalf("authorized apply preflight = lock=%v response=%#v err=%v", lock, suspension, err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	record.Status, record.Revision, record.RevokedAt = "revoked", record.Revision+1, time.Now().UTC()
	if err := effects.StoreDelegationRecord(recordPath, record); err != nil {
		t.Fatal(err)
	}
	lock, suspension, err = prepareDelegation(context.Background(), &request)
	if lock != nil || suspension != nil || err == nil || !strings.Contains(err.Error(), "DELEGATION_REVOKED") {
		t.Fatalf("revoked apply preflight = lock=%v response=%#v err=%v", lock, suspension, err)
	}
	record.Status, record.Revision, record.RevokedAt = "active", record.Revision+1, time.Time{}
	if err := effects.StoreDelegationRecord(recordPath, record); err != nil {
		t.Fatal(err)
	}
	committed := surfaces.Response{Receipt: &protocol.TransitionReceipt{ID: "target-receipt"}}
	delegationLockPath, err := delegation.LockPath(layout.LockRoot, bound.runID)
	if err != nil {
		t.Fatal(err)
	}
	heldDelegationLock, err := effects.AcquireExclusivePath(context.Background(), delegationLockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := settleDelegationAtTarget(context.Background(), request, committed, true, true); err != nil {
		t.Fatal(err)
	}
	if err := heldDelegationLock.Release(); err != nil {
		t.Fatal(err)
	}
	completed, err := delegation.Load(recordPath)
	if err != nil || completed.Status != "completed" || completed.EndReason != "target-met" || completed.Revision != record.Revision+1 {
		t.Fatalf("target settlement = record=%#v err=%v", completed, err)
	}
	request.Operation = surfaces.OperationResolve
	lock, suspension, err = prepareDelegation(context.Background(), &request)
	if lock != nil || suspension != nil || err != nil || request.Authority.Set(time.Now().UTC())[catalog.AuthorityAutonomy] {
		t.Fatalf("completed resolve replay = lock=%v response=%#v authority=%#v err=%v", lock, suspension, request.Authority, err)
	}
	request.Operation = surfaces.OperationApply
	lock, suspension, err = prepareDelegation(context.Background(), &request)
	if lock != nil || suspension != nil || err == nil || !strings.Contains(err.Error(), "DELEGATION_REVOKED") {
		t.Fatalf("post-target apply preflight = lock=%v response=%#v err=%v", lock, suspension, err)
	}
}

func TestExplicitAuthorizationCanReplaceRevokedPreReconciliationRequest(t *testing.T) {
	// control-law: revocation remains effective for its exact request, while an
	// explicitly authorized post-installation request creates new authority.
	prior := delegation.Request{
		RunID: "run-example", ProgramID: "product-delivery", ProgramFingerprint: strings.Repeat("a", 64), ControlBundleFingerprint: strings.Repeat("b", 64),
		EntryID: "run", TargetID: "published-pr", ObjectiveID: "objective", DeliveryID: "delivery", InputFingerprints: []string{"plan"},
		RepositoryID: "repository", GitCommonID: "common", InitialWorktreeID: "worktree", InitialRef: "refs/heads/main",
		BindingFingerprint: strings.Repeat("c", 64), RequestedAuthorities: []string{"autonomy"}, Description: "Run product delivery",
	}
	priorFingerprint, err := prior.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	existing := delegation.Record{
		Schema: delegation.Schema, SchemaRevision: delegation.SchemaRevision, Request: prior, RequestFingerprint: priorFingerprint,
		ReceiptID: "authorization-prior", Actor: "operator", AuthorizedAt: time.Unix(1_700_000_000, 0).UTC(), Revision: 3, Status: "revoked",
	}
	current := prior
	current.ProgramFingerprint, current.ControlBundleFingerprint = strings.Repeat("d", 64), strings.Repeat("e", 64)
	currentFingerprint, err := current.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_100, 0).UTC()
	refreshed, changed, err := authorizeDelegation(&existing, current, currentFingerprint, "operator", 0, now, true)
	if err != nil || !changed || refreshed.Status != "active" || refreshed.Revision != 4 || refreshed.RequestFingerprint != currentFingerprint || refreshed.ReceiptID == existing.ReceiptID {
		t.Fatalf("reprojected authorization = %#v changed=%t err=%v", refreshed, changed, err)
	}
	if _, _, err := authorizeDelegation(&existing, current, currentFingerprint, "operator", 0, now, false); err == nil {
		t.Fatal("revoked authority was replaced without an admitted reprojection")
	}
}

func TestDelegationReprojectionRequiresAChangedControlBundle(t *testing.T) {
	// control-law: ordinary input or context drift cannot be relabeled as an
	// installation reprojection when the installed control bundle is unchanged.
	request := delegation.Request{
		RunID: "run-example", ProgramID: "product-delivery", ProgramFingerprint: strings.Repeat("a", 64), ControlBundleFingerprint: strings.Repeat("b", 64),
		EntryID: "run", TargetID: "published-pr", ObjectiveID: "objective", DeliveryID: "delivery",
		RepositoryID: "repository", GitCommonID: "common",
	}
	changedInput := request
	changedInput.InputFingerprints = []string{"changed-plan"}
	admitted, err := canReprojectDelegation(ports.ControllerLayout{}, model.InvocationContext{}, request, changedInput)
	if err != nil || admitted {
		t.Fatalf("unchanged-bundle reprojection admitted=%t err=%v", admitted, err)
	}
	changedObjective := request
	changedObjective.ControlBundleFingerprint = strings.Repeat("d", 64)
	changedObjective.ObjectiveID = "objective-other"
	admitted, err = canReprojectDelegation(ports.ControllerLayout{}, model.InvocationContext{}, request, changedObjective)
	if err != nil || admitted {
		t.Fatalf("changed-objective reprojection admitted=%t err=%v", admitted, err)
	}
}
