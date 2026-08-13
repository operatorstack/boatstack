package controlprogram_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
)

func incidentProgram() controlprogram.Document {
	mitigated := "mitigated"
	return controlprogram.Document{
		SchemaVersion: controlprogram.SchemaVersion,
		Program:       controlprogram.Program{ID: "incident-response", Version: "1", Description: "human text"},
		Description:   "incident control program",
		Declarations: controlprogram.Declarations{
			Capabilities: []string{"service.restart"}, Authorities: []string{"incident-commander"},
			Effects: []string{"service.restart"}, Verifiers: []string{"healthcheck"}, InputResolvers: []string{"incident.input"},
		},
		Facets: []controlprogram.Facet{
			{ID: "service", Kind: "enum", Values: []string{"healthy", "degraded"}, Description: "service health"},
			{ID: "incident", Kind: "enum", Values: []string{"open", "mitigated"}},
		},
		Evidence: []controlprogram.Evidence{{ID: "healthcheck", Subject: "service", Kind: "observation", Description: "observed health"}},
		Operators: []controlprogram.Operator{{
			ID: "restart", Capabilities: []string{"service.restart"}, Authority: []string{"incident-commander"},
			Effects: []string{"service.restart"}, Verifier: "healthcheck", Recovery: "restart",
			Description: "restart the service",
			StateEffect: &controlprogram.StateEffect{Kind: "assignments", Assignments: []controlprogram.StateAssignment{{Facet: "incident", Value: &mitigated}}},
		}},
		Transitions: []controlprogram.Transition{{
			ID: "restart", Operator: "restart", Priority: 10,
			Guard: fact("incident", "open"), Target: fact("incident", "mitigated"), Description: "restart service",
		}},
		Targets: []controlprogram.Target{{ID: "mitigated", Predicate: fact("incident", "mitigated"), Description: "incident mitigated"}},
		Entries: []controlprogram.Entry{{ID: "respond", Target: "mitigated", Description: "respond to incident", Inputs: []controlprogram.EntryInput{{ID: "incident", Type: "json", Required: true, Resolver: "incident.input", Config: json.RawMessage(`{"b":2,"a":1}`)}}}},
	}
}

func fact(facet, value string) controlprogram.Predicate {
	return controlprogram.Predicate{Fact: &controlprogram.FactPredicate{Facet: facet, Statuses: []string{"known"}, Values: []string{value}}}
}

func clone(t *testing.T, value controlprogram.Document) controlprogram.Document {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result controlprogram.Document
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestDomainNeutralIncidentProgramCompiles(t *testing.T) {
	// control-law: generic-control-program-ir-has-no-software-delivery-dependency
	compiled, err := controlprogram.Compile(incidentProgram(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Fingerprint) != 64 || !bytes.Contains(compiled.Canonical, []byte(`"incident-response"`)) {
		t.Fatalf("compiled incident program = %#v", compiled)
	}
	if compiled.Document.Description == "" || compiled.Document.Program.Description == "" ||
		compiled.Document.Facets[1].Description == "" || compiled.Document.Evidence[0].Description == "" ||
		compiled.Document.Operators[0].Description == "" || compiled.Document.Transitions[0].Description == "" ||
		compiled.Document.Targets[0].Description == "" || compiled.Document.Entries[0].Description == "" {
		t.Fatalf("compilation removed declared descriptions: %#v", compiled.Document)
	}
}

func TestCanonicalFingerprintIgnoresOrderingAndDescriptions(t *testing.T) {
	// control-law: canonical-program-identity-binds-executable-semantics-only
	base, err := controlprogram.Compile(incidentProgram(), nil)
	if err != nil {
		t.Fatal(err)
	}
	equivalent := clone(t, incidentProgram())
	equivalent.Description = "different prose"
	equivalent.Program.Description = "different program prose"
	equivalent.Transitions[0].Description = "different transition prose"
	equivalent.Facets[0], equivalent.Facets[1] = equivalent.Facets[1], equivalent.Facets[0]
	equivalent.Entries[0].Inputs[0].Config = json.RawMessage(`{"a":1,"b":2}`)
	equivalent.Declarations.Capabilities = append([]string(nil), equivalent.Declarations.Capabilities...)
	other, err := controlprogram.Compile(equivalent, nil)
	if err != nil {
		t.Fatal(err)
	}
	if other.Fingerprint != base.Fingerprint {
		t.Fatalf("equivalent fingerprints differ: %s != %s", other.Fingerprint, base.Fingerprint)
	}
	changed := clone(t, incidentProgram())
	changed.Transitions[0].Priority++
	semantic, err := controlprogram.Compile(changed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if semantic.Fingerprint == base.Fingerprint {
		t.Fatal("executable change preserved fingerprint")
	}
}

func TestStrictLoaderRejectsUnknownAndDuplicateFields(t *testing.T) {
	// control-law: only-the-closed-ir-schema-crosses-the-compiler-boundary
	valid, _ := json.Marshal(incidentProgram())
	unknown := bytes.Replace(valid, []byte(`"schema_version"`), []byte(`"unknown":true,"schema_version"`), 1)
	if _, err := controlprogram.Load(bytes.NewReader(unknown), nil); err == nil {
		t.Fatal("unknown field was accepted")
	}
	duplicate := bytes.Replace(valid, []byte(`"schema_version"`), []byte(`"schema_version":"control-program/v1","schema_version"`), 1)
	if _, err := controlprogram.Load(bytes.NewReader(duplicate), nil); err == nil {
		t.Fatal("duplicate field was accepted")
	}
}

func TestStrictLoadersRejectOversizedTrailingInput(t *testing.T) {
	// control-law: size-limited-loaders-never-treat-truncation-as-eof
	documentRaw, err := json.Marshal(incidentProgram())
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := controlprogram.Compile(incidentProgram(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, artifactRaw, err := controlprogram.NewArtifact(compiled, controlprogram.ArtifactInput{
		CompilerVersion: "compiler-1", SourcePath: "flow.ts", Source: []byte("source"),
		DependencyLockPath: "package-lock.json", DependencyLock: []byte("lock"), GeneratedSkills: map[string][]byte{},
	})
	if err != nil {
		t.Fatal(err)
	}
	oversized := func(raw []byte, limit int) []byte {
		if len(raw) >= limit {
			t.Fatalf("fixture length %d exceeds limit %d", len(raw), limit)
		}
		result := append([]byte(nil), raw...)
		result = append(result, bytes.Repeat([]byte(" "), limit-len(result))...)
		return append(result, 'x')
	}
	if _, err := controlprogram.Load(bytes.NewReader(oversized(documentRaw, 16<<20)), nil); err == nil || !strings.Contains(err.Error(), "exceeds 16 MiB") {
		t.Fatalf("oversized IR result = %v", err)
	}
	if _, err := controlprogram.LoadArtifact(bytes.NewReader(oversized(artifactRaw, 32<<20))); err == nil || !strings.Contains(err.Error(), "exceeds 32 MiB") {
		t.Fatalf("oversized artifact result = %v", err)
	}
}

func TestCompilerRejectsUndeclaredEffectAndMissingRecovery(t *testing.T) {
	for name, mutate := range map[string]func(*controlprogram.Document){
		"undeclared-effect": func(value *controlprogram.Document) { value.Operators[0].Effects = []string{"undeclared"} },
		"missing-recovery":  func(value *controlprogram.Document) { value.Operators[0].Recovery = "" },
		"invalid-reference": func(value *controlprogram.Document) { value.Transitions[0].Operator = "missing" },
		"recovery-gap":      func(value *controlprogram.Document) { value.Operators[0].Recovery = "missing" },
	} {
		t.Run(name, func(t *testing.T) {
			value := incidentProgram()
			mutate(&value)
			if _, err := controlprogram.Compile(value, nil); err == nil {
				t.Fatal("invalid program compiled")
			}
		})
	}
}

func TestArtifactBindsSourceLockSkillsAndCompiler(t *testing.T) {
	// control-law: runtime-admits-only-an-exact-source-lock-artifact-projection
	repository := t.TempDir()
	sourcePath, lockPath, skillPath := "flow.ts", "package-lock.json", ".agents/skills/respond/SKILL.md"
	source, lock, skill := []byte("source"), []byte("lock"), []byte("skill")
	for path, content := range map[string][]byte{sourcePath: source, lockPath: lock, skillPath: skill} {
		absolute := filepath.Join(repository, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	compiled, err := controlprogram.Compile(incidentProgram(), nil)
	if err != nil {
		t.Fatal(err)
	}
	artifact, _, err := controlprogram.NewArtifact(compiled, controlprogram.ArtifactInput{
		CompilerVersion: "compiler-1", SourcePath: sourcePath, Source: source, DependencyLockPath: lockPath, DependencyLock: lock,
		GeneratedSkills: map[string][]byte{skillPath: skill},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controlprogram.CheckArtifact(repository, artifact, "compiler-1", nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, sourcePath), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := controlprogram.CheckArtifact(repository, artifact, "compiler-1", nil); err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("stale source result = %v", err)
	}
}

func TestArtifactRejectsGeneratedPathsOutsideHostSkillRoots(t *testing.T) {
	compiled, err := controlprogram.Compile(incidentProgram(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = controlprogram.NewArtifact(compiled, controlprogram.ArtifactInput{
		CompilerVersion: "compiler-1", SourcePath: "flow.ts", Source: []byte("source"),
		DependencyLockPath: "package-lock.json", DependencyLock: []byte("lock"),
		GeneratedSkills: map[string][]byte{"README.md": []byte("delete me")},
	})
	if err == nil {
		t.Fatal("artifact accepted an arbitrary generated deletion path")
	}
}
