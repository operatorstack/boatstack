package main

import (
	"bytes"
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
	document := productDeliveryDocument("product-delivery")
	sourcePath, lockPath := ".boatstack/flows/product-delivery.flow.ts", "package-lock.json"
	source, lock := []byte("flow source"), []byte("lock")
	for path, content := range map[string][]byte{sourcePath: source, lockPath: lock} {
		writeFixture(t, repository, path, content)
	}
	writeFlowArtifact(t, repository, document, sourcePath, source, lockPath, lock)
	return repository
}

func writeFlowArtifact(t *testing.T, repository string, document controlprogram.Document, sourcePath string, source []byte, lockPath string, lock []byte) {
	t.Helper()
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
		SchemaVersion: controlprogram.SchemaVersion,
		Program:       controlprogram.Program{ID: programID, Version: "1"},
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
	if err == nil || !strings.Contains(err.Error(), "FLOW_INPUT_MISMATCH") {
		t.Fatalf("untargeted override result = %v", err)
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
	if len(artifact.GeneratedSkills) != 3 {
		t.Fatalf("generated skills = %v", artifact.GeneratedSkills)
	}
	for path := range artifact.GeneratedSkills {
		if strings.Contains(path, "--") {
			t.Fatalf("artifact contains invalid skill path %s", path)
		}
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
	for _, transitionID := range []string{"objective.bind", "plan.create"} {
		preManaged, err := bindFlowEntry(context.Background(), commandOptions{
			repository: repository, programID: "product-delivery", entryID: "run", runID: initial.runID, host: "codex",
			deliveryID: initial.deliveryID, objectiveKind: initial.objectiveKind, objectiveID: initial.objectiveID, transitionID: transitionID,
		})
		if err != nil {
			t.Fatalf("pre-materialization %s binding failed: %v", transitionID, err)
		}
		if transitionID == "plan.create" {
			parameters, parseErr := parseParameters(preManaged.parameters)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			expected := filepath.Join(initial.repository, ".boatstack", "plans", "inbox", "delivery-one.md")
			if source, ok := parameters.Get("source_path"); !ok || source != expected {
				t.Fatalf("pre-materialization source = %q, present=%t", source, ok)
			}
		}
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
		deliveryID: initial.deliveryID, objectiveKind: initial.objectiveKind, objectiveID: initial.objectiveID, transitionID: "plan.create",
	})
	if err != nil {
		t.Fatal(err)
	}
	parameters, err := parseParameters(resumed.parameters)
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(initial.repository, ".boatstack", "plans", "inbox", "delivery.MD")
	if source, ok := parameters.Get("source_path"); !ok || source != expected {
		t.Fatalf("resumed source = %q, present=%t; want %q", source, ok, expected)
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
		deliveryID: initial.deliveryID, objectiveKind: initial.objectiveKind, objectiveID: initial.objectiveID, transitionID: "plan.create",
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
		runID: initial.runID, deliveryID: initial.deliveryID, objectiveKind: initial.objectiveKind,
		objectiveID: "objective-substituted", transitionID: "objective.bind",
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
		deliveryID: initial.deliveryID, objectiveKind: initial.objectiveKind, objectiveID: initial.objectiveID,
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
	paths, artifactExpectation, priorSkills, err := obsoleteGeneratedSkills(repository, artifactPath, map[string]string{retainedPath: fileDigest(retained)})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0].Path != filepath.Join(repository, filepath.FromSlash(retiredPath)) || !artifactExpectation.Exists {
		t.Fatalf("retired paths = %v", paths)
	}
	if priorSkills[retainedPath] != fileDigest(retained) || priorSkills[retiredPath] != fileDigest(retired) {
		t.Fatalf("prior generated skills = %v", priorSkills)
	}
	writeFixture(t, repository, retiredPath, []byte("user changed"))
	if _, _, _, err := obsoleteGeneratedSkills(repository, artifactPath, map[string]string{retainedPath: fileDigest(retained)}); err == nil || !strings.Contains(err.Error(), "was modified") {
		t.Fatalf("modified retired projection was not protected: %v", err)
	}
	if err := os.Remove(filepath.Join(repository, filepath.FromSlash(retiredPath))); err != nil {
		t.Fatal(err)
	}
	paths, _, _, err = obsoleteGeneratedSkills(repository, artifactPath, map[string]string{retainedPath: fileDigest(retained)})
	if err != nil || len(paths) != 1 || !paths[0].AllowMissing {
		t.Fatalf("interrupted retirement was not retryable: %v, %v", paths, err)
	}
}
