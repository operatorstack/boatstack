package controlprogram_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
)

type delegationResolver struct {
	fingerprint string
	authorities []string
	delegable   bool
}

func (r delegationResolver) ResolveOperator(string, string) (controlprogram.ResolvedOperator, error) {
	return controlprogram.ResolvedOperator{}, nil
}

func (r delegationResolver) ResolveDelegation(reference, version string) (controlprogram.ResolvedDelegation, error) {
	if reference != "incident/delegation/autonomy" || version != "1" {
		return controlprogram.ResolvedDelegation{}, os.ErrNotExist
	}
	return controlprogram.ResolvedDelegation{Fingerprint: r.fingerprint, Authorities: r.authorities, Delegable: r.delegable}, nil
}

func incidentProgram() controlprogram.Document {
	mitigated := "mitigated"
	return controlprogram.Document{
		Schema: controlprogram.SchemaName, SchemaRevision: controlprogram.SchemaRevision,
		Program:     controlprogram.Program{ID: "incident-response", Version: "1", Description: "human text"},
		Description: "incident control program",
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
			ID: "restart", Capabilities: []string{"service.restart"}, Authority: controlprogram.AuthorityRequirement{AnyOf: []string{"incident-commander"}},
			Effects: []string{"service.restart"}, Verifier: "healthcheck", Recovery: "restart",
			Description: "restart the service", ExecutionContext: "preserve",
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

func incidentWorkProgram() controlprogram.Document {
	document := incidentProgram()
	instructions := "Inspect the incident and produce the declared diagnosis."
	digest := sha256.Sum256([]byte(instructions))
	document.Work = []controlprogram.WorkContract{{
		ID: "diagnose", Instructions: controlprogram.WorkAsset{Path: "instructions.md", SHA256: hex.EncodeToString(digest[:]), Content: instructions},
		Inputs:      []controlprogram.WorkInput{{ID: "incident", EntryInput: "incident"}},
		Outputs:     []controlprogram.WorkOutput{{ID: "diagnosis", Path: "diagnosis.md", MediaType: "text/markdown", Required: true, MaxBytes: 4096}},
		Description: "presentation only",
	}}
	document.Transitions[0].Work = "diagnose"
	return document
}

func TestForegroundWorkIsDomainNeutralAndFingerprintBound(t *testing.T) {
	// control-law: exact foreground-work semantics contribute to canonical program identity
	base, err := controlprogram.Compile(incidentWorkProgram(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(base.Document.Work) != 1 || base.Document.Transitions[0].Work != "diagnose" {
		t.Fatalf("compiled work = %#v", base.Document.Work)
	}
	description := incidentWorkProgram()
	description.Work[0].Description = "different prose"
	equivalent, err := controlprogram.Compile(description, nil)
	if err != nil || equivalent.Fingerprint != base.Fingerprint {
		t.Fatalf("work description changed executable identity: %v %s != %s", err, equivalent.Fingerprint, base.Fingerprint)
	}
	changed := incidentWorkProgram()
	changed.Work[0].Instructions.Content += " Verify recovery."
	digest := sha256.Sum256([]byte(changed.Work[0].Instructions.Content))
	changed.Work[0].Instructions.SHA256 = hex.EncodeToString(digest[:])
	semantic, err := controlprogram.Compile(changed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if semantic.Fingerprint == base.Fingerprint {
		t.Fatal("instruction asset change preserved executable identity")
	}
}

func TestForegroundWorkRejectsUnboundAssetsInputsAndTransitions(t *testing.T) {
	// control-law: every foreground-work dependency is declared and exactly referenced
	for name, mutate := range map[string]func(*controlprogram.Document){
		"unresolved-asset": func(value *controlprogram.Document) {
			value.Work[0].Instructions.Content, value.Work[0].Instructions.SHA256 = "", ""
		},
		"unknown-entry-input": func(value *controlprogram.Document) { value.Work[0].Inputs[0].EntryInput = "missing" },
		"unreferenced-work":   func(value *controlprogram.Document) { value.Transitions[0].Work = "" },
		"unknown-work":        func(value *controlprogram.Document) { value.Transitions[0].Work = "missing" },
	} {
		t.Run(name, func(t *testing.T) {
			document := incidentWorkProgram()
			mutate(&document)
			if _, err := controlprogram.Compile(document, nil); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
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

func TestEntryDiagnosticsAreArtifactBoundButNotExecutableControlLaw(t *testing.T) {
	document := incidentProgram()
	without, err := controlprogram.Compile(document, nil)
	if err != nil {
		t.Fatal(err)
	}
	document.Entries[0].Diagnostics = &controlprogram.EntryDiagnostics{ExplainOnSuspend: true}
	with, err := controlprogram.Compile(document, nil)
	if err != nil {
		t.Fatal(err)
	}
	if with.Fingerprint != without.Fingerprint {
		t.Fatalf("presentation preference changed executable fingerprint: %s != %s", with.Fingerprint, without.Fingerprint)
	}
	if bytes.Equal(with.Canonical, without.Canonical) || !bytes.Contains(with.Canonical, []byte(`"explain_on_suspend": true`)) {
		t.Fatal("diagnostic preference is not preserved in the artifact projection")
	}
}

func TestDelegationBindingIsResolvedAndFingerprintBound(t *testing.T) {
	// control-law: repository-source-can-request-but-cannot-grant-authority
	document := incidentProgram()
	document.Entries[0].Delegation = &controlprogram.DelegationBinding{Reference: "incident/delegation/autonomy", Version: "1"}
	resolver := delegationResolver{fingerprint: strings.Repeat("d", 64), authorities: []string{"autonomy"}, delegable: true}
	compiled, err := controlprogram.Compile(document, resolver)
	if err != nil {
		t.Fatal(err)
	}
	binding := compiled.Document.Entries[0].Delegation
	if binding.Fingerprint != resolver.fingerprint || len(binding.Authorities) != 1 || binding.Authorities[0] != "autonomy" {
		t.Fatalf("resolved delegation = %#v", binding)
	}
	if _, err := controlprogram.Compile(compiled.Document, delegationResolver{fingerprint: strings.Repeat("e", 64), authorities: []string{"autonomy"}, delegable: true}); err == nil || !strings.Contains(err.Error(), "drift") {
		t.Fatalf("delegation drift result = %v", err)
	}
	if _, err := controlprogram.Compile(document, delegationResolver{fingerprint: strings.Repeat("d", 64), authorities: []string{"autonomy"}, delegable: false}); err == nil || !strings.Contains(err.Error(), "not delegable") {
		t.Fatalf("nondelegable result = %v", err)
	}
}

func TestAuthorityAlgebraAndRepositoryStrengtheningAreExecutableSemantics(t *testing.T) {
	// control-law: alternatives-and-mandatory-authority-never-flatten
	base := incidentProgram()
	base.Declarations.Authorities = []string{"autonomy", "external-provider", "human", "incident-commander"}
	base.Operators[0].Authority = controlprogram.AuthorityRequirement{AnyOf: []string{"human", "autonomy"}, AllOf: []string{"external-provider"}}
	base.Transitions[0].Requires.Authorities = []string{"incident-commander"}
	compiled, err := controlprogram.Compile(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Document.Operators[0].Authority.AnyOf) != 2 || len(compiled.Document.Operators[0].Authority.AllOf) != 1 || len(compiled.Document.Transitions[0].Requires.Authorities) != 1 {
		t.Fatalf("authority semantics = %#v %#v", compiled.Document.Operators[0].Authority, compiled.Document.Transitions[0].Requires)
	}
	changed := clone(t, base)
	changed.Transitions[0].Requires.Authorities = nil
	withoutStrengthening, err := controlprogram.Compile(changed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Fingerprint == withoutStrengthening.Fingerprint {
		t.Fatal("repository authority strengthening did not change executable fingerprint")
	}
}

func TestOnlyTrustedBindingsMayAdvanceExecutionContext(t *testing.T) {
	document := incidentProgram()
	document.Operators[0].ExecutionContext = "advance"
	if _, err := controlprogram.Compile(document, nil); err == nil || !strings.Contains(err.Error(), "trusted bindings") {
		t.Fatalf("untrusted execution advance result = %v", err)
	}
}

func TestStrictLoaderRejectsUnknownAndDuplicateFields(t *testing.T) {
	// control-law: only-the-closed-ir-schema-crosses-the-compiler-boundary
	valid, _ := json.Marshal(incidentProgram())
	unknown := bytes.Replace(valid, []byte(`"schema"`), []byte(`"unknown":true,"schema"`), 1)
	if _, err := controlprogram.Load(bytes.NewReader(unknown), nil); err == nil {
		t.Fatal("unknown field was accepted")
	}
	duplicate := bytes.Replace(valid, []byte(`"schema"`), []byte(`"schema":"control-program","schema"`), 1)
	if _, err := controlprogram.Load(bytes.NewReader(duplicate), nil); err == nil {
		t.Fatal("duplicate field was accepted")
	}
}

func TestGenerationLabelledControlProgramAndArtifactFormatsAreRejected(t *testing.T) {
	legacyProgram := []byte(`{"schema_version":"control-program/v1"}`)
	if _, err := controlprogram.Load(bytes.NewReader(legacyProgram), nil); err == nil {
		t.Fatal("generation-labelled Control Program format was accepted")
	}
	legacyArtifact := []byte(`{"schema_version":1,"compiler_version":"control-program/v1.compiler.1"}`)
	if _, err := controlprogram.LoadArtifact(bytes.NewReader(legacyArtifact)); err == nil {
		t.Fatal("generation-labelled artifact format was accepted")
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
	generate := func(controlprogram.Compiled) (map[string][]byte, error) {
		return map[string][]byte{skillPath: skill}, nil
	}
	if _, err := controlprogram.CheckArtifact(repository, artifact, "compiler-1", nil, generate); err != nil {
		t.Fatal(err)
	}
	forgedSkill := []byte("forged skill")
	forgedArtifact, _, err := controlprogram.NewArtifact(compiled, controlprogram.ArtifactInput{
		CompilerVersion: "compiler-1", SourcePath: sourcePath, Source: source, DependencyLockPath: lockPath, DependencyLock: lock,
		GeneratedSkills: map[string][]byte{skillPath: forgedSkill},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, filepath.FromSlash(skillPath)), forgedSkill, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := controlprogram.CheckArtifact(repository, forgedArtifact, "compiler-1", nil, generate); err == nil || !strings.Contains(err.Error(), "derived from compiled program") {
		t.Fatalf("self-consistent forged projection result = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repository, filepath.FromSlash(skillPath)), skill, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, sourcePath), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := controlprogram.CheckArtifact(repository, artifact, "compiler-1", nil, generate); err == nil || !strings.Contains(err.Error(), "source") {
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

func TestArtifactBindsExactForegroundWorkAssets(t *testing.T) {
	// control-law: runtime admits only the instruction and schema assets compiled into the artifact
	repository := t.TempDir()
	document := incidentWorkProgram()
	files := map[string][]byte{
		"flow.ts":           []byte("source"),
		"package-lock.json": []byte("lock"),
		"instructions.md":   []byte(document.Work[0].Instructions.Content),
	}
	for path, raw := range files {
		if err := os.WriteFile(filepath.Join(repository, path), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	compiled, err := controlprogram.Compile(document, nil)
	if err != nil {
		t.Fatal(err)
	}
	artifact, _, err := controlprogram.NewArtifact(compiled, controlprogram.ArtifactInput{
		CompilerVersion: "compiler-1", SourcePath: "flow.ts", Source: files["flow.ts"],
		DependencyLockPath: "package-lock.json", DependencyLock: files["package-lock.json"], GeneratedSkills: map[string][]byte{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controlprogram.CheckArtifact(repository, artifact, "compiler-1", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "instructions.md"), []byte("different instructions"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := controlprogram.CheckArtifact(repository, artifact, "compiler-1", nil, nil); err == nil || !strings.Contains(err.Error(), "work asset") {
		t.Fatalf("changed work asset result = %v", err)
	}
}
