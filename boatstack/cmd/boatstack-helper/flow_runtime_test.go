package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	softwareflow "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
)

func flowRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	resolver, err := softwareflow.NewResolver(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	truth := true
	config := json.RawMessage(`{"path":".boatstack/plans/inbox","cardinality":"exactly-one"}`)
	document := controlprogram.Document{
		SchemaVersion: controlprogram.SchemaVersion,
		Program:       controlprogram.Program{ID: "product-delivery", Version: "1"},
		Declarations:  controlprogram.Declarations{InputResolvers: []string{"software-delivery.plan-inbox"}},
		Facets: []controlprogram.Facet{
			{ID: "publication", Kind: "string"}, {ID: "verification", Kind: "string"},
			{ID: "configuration", Kind: "string"}, {ID: "runtime", Kind: "string"},
		},
		Operators:   []controlprogram.Operator{{ID: "publication.observe", Binding: &controlprogram.OperatorBinding{Reference: "software-delivery/publication.observe", Version: "1"}}},
		Transitions: []controlprogram.Transition{{ID: "publication.observe", Operator: "publication.observe", Guard: controlprogram.Predicate{True: &truth}, Target: controlprogram.Predicate{True: &truth}, Priority: 77}},
		Targets: []controlprogram.Target{{ID: "published-pr", Predicate: controlprogram.Predicate{All: []controlprogram.Predicate{
			flowFact("verification", "current"), flowFact("configuration", "verified"), flowFact("runtime", "verified"), flowFact("publication", "open"),
		}}}},
		Entries: []controlprogram.Entry{{ID: "run", Target: "published-pr", Inputs: []controlprogram.EntryInput{{ID: "plan", Type: "markdown-file", Required: true, Resolver: "software-delivery.plan-inbox", Config: config}}}},
	}
	compiled, err := controlprogram.Compile(document, resolver)
	if err != nil {
		t.Fatal(err)
	}
	skills, err := softwareflow.GenerateSkills(compiled, []string{"codex", "claude"})
	if err != nil {
		t.Fatal(err)
	}
	sourcePath, lockPath := ".boatstack/flows/product-delivery.flow.ts", "package-lock.json"
	source, lock := []byte("flow source"), []byte("lock")
	for path, content := range map[string][]byte{sourcePath: source, lockPath: lock} {
		writeFixture(t, repository, path, content)
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
	writeFixture(t, repository, ".boatstack/flows/product-delivery.flow.ir.json", artifactRaw)
	return repository
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
				if err == nil || !strings.Contains(err.Error(), "FLOW_INPUT_MISMATCH") {
					t.Fatalf("CLI override result = %v", err)
				}
				return
			}
			_, err := bindRPCFlowEntry(context.Background(), surfaces.Request{
				SchemaVersion: surfaces.SchemaVersion, Operation: surfaces.OperationResolve, Repository: repository,
				Host: "claude", CorrelationID: "rpc-override", ProgramID: "product-delivery", EntryID: "run",
				TransitionID: "plan.create", Parameters: protocol.Parameters{{Name: "source_path", Value: other}},
			})
			if err == nil || !strings.Contains(err.Error(), "FLOW_INPUT_MISMATCH") {
				t.Fatalf("RPC override result = %v", err)
			}
		})
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

func TestFlowEntryBindsStableRunAndResumesManagedPlan(t *testing.T) {
	// control-law: questions-and-restarts-preserve-the-exact-plan-worktree-and-run
	repository := flowRepository(t)
	writeFixture(t, repository, ".boatstack/plans/inbox/delivery-one.md", []byte("exact plan"))
	initial, err := bindFlowEntry(context.Background(), commandOptions{repository: repository, programID: "product-delivery", entryID: "run", host: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(initial.runID, "run-") || initial.deliveryID != "delivery-one" || initial.objectiveKind != "open-or-updated-pr" || len(initial.parameters) != 0 {
		t.Fatalf("initial Flow context = %#v", initial)
	}
	writeFixture(t, repository, ".boatstack/plans/delivery-one.source", []byte("exact plan"))
	writeFixture(t, repository, ".boatstack/plans/inbox/unrelated.md", []byte("other plan"))
	writeFixture(t, repository, ".boatstack/plans/delivery-one.source", []byte("approved amendment"))
	resumed, err := bindFlowEntry(context.Background(), commandOptions{
		repository: repository, programID: "product-delivery", entryID: "run", runID: initial.runID, host: "codex",
		deliveryID: initial.deliveryID, objectiveKind: initial.objectiveKind, objectiveID: initial.objectiveID, transitionID: "plan.create",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.runID != initial.runID {
		t.Fatalf("run identity changed: %s != %s", resumed.runID, initial.runID)
	}
	parameters, err := parseParameters(resumed.parameters)
	if err != nil {
		t.Fatal(err)
	}
	if source, ok := parameters.Get("source_path"); !ok || source != filepath.Join(resumed.repository, ".boatstack", "plans", "delivery-one.source") {
		t.Fatalf("resumed source = %q, present=%t", source, ok)
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
	repository := t.TempDir()
	retained := []byte("retained")
	retired := []byte("retired")
	retainedPath := ".agents/skills/program-keep/SKILL.md"
	retiredPath := ".agents/skills/program-remove/SKILL.md"
	writeFixture(t, repository, retainedPath, retained)
	writeFixture(t, repository, retiredPath, retired)
	artifact := controlprogram.Artifact{
		SchemaVersion: controlprogram.ArtifactSchemaVersion, CompilerVersion: flowCompilerVersion,
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
	paths, err := obsoleteGeneratedSkills(repository, artifactPath, map[string]string{retainedPath: fileDigest(retained)})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != filepath.Join(repository, filepath.FromSlash(retiredPath)) {
		t.Fatalf("retired paths = %v", paths)
	}
	writeFixture(t, repository, retiredPath, []byte("user changed"))
	if _, err := obsoleteGeneratedSkills(repository, artifactPath, map[string]string{retainedPath: fileDigest(retained)}); err == nil || !strings.Contains(err.Error(), "was modified") {
		t.Fatalf("modified retired projection was not protected: %v", err)
	}
}
