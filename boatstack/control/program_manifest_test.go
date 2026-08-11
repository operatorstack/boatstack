package control_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"

	boatstack "github.com/operatorstack/boatstack/boatstack"
	"github.com/operatorstack/boatstack/boatstack/control"
	"github.com/operatorstack/boatstack/boatstack/internal/surfaces"
)

func TestProgramManifestCanonicalFingerprintContract(t *testing.T) {
	// control-law: validated-executable-semantics-exactly-determine-program-fingerprint
	base := programFixture()
	one := loadManifest(t, base)

	equivalent := programFixture()
	equivalent.ProgramVersion = "99"
	equivalent.RequiresRuntime = ">=0.9.0"
	reverse(equivalent.Capabilities.Effects)
	reverse(equivalent.Capabilities.Verifiers)
	reverse(equivalent.OwnedResources)
	reverse(equivalent.Transitions)
	reverse(equivalent.Transitions[0].RequiredIdentity)
	reverse(equivalent.Transitions[0].SourceConditions)
	two := loadManifest(t, equivalent)
	if one.Fingerprint() != two.Fingerprint() {
		t.Fatalf("representation-only or author-version changes changed executable fingerprint: %s != %s", one.Fingerprint(), two.Fingerprint())
	}

	raw, err := json.MarshalIndent(base, "", "    ")
	if err != nil {
		t.Fatal(err)
	}
	three, err := control.LoadProgram(bytes.NewReader(raw), runtimeFixture())
	if err != nil {
		t.Fatal(err)
	}
	if one.Fingerprint() != three.Fingerprint() {
		t.Fatal("whitespace changed executable fingerprint")
	}
	reordered := reorderTopLevelObject(t, raw)
	four, err := control.LoadProgram(bytes.NewReader(reordered), runtimeFixture())
	if err != nil {
		t.Fatal(err)
	}
	if one.Fingerprint() != four.Fingerprint() {
		t.Fatal("JSON object key order changed executable fingerprint")
	}

	mutations := map[string]func(*control.ProgramManifest){
		"program-id": func(value *control.ProgramManifest) { value.ProgramID = "alternate-program" },
		"source-phase": func(value *control.ProgramManifest) {
			value.Transitions[0].SourcePhases = []control.ProtocolPhase{control.PhaseObserved}
		},
		"target-phase": func(value *control.ProgramManifest) {
			value.Transitions[0].TargetPhases = []control.ProtocolPhase{control.PhaseFrontier}
		},
		"authority": func(value *control.ProgramManifest) {
			value.Transitions[0].Authority = []control.AuthorityClass{control.AuthorityHuman}
		},
		"effect": func(value *control.ProgramManifest) {
			value.Capabilities.Effects[0] = "alternate.effect"
			value.Transitions[0].Effect = "alternate.effect"
			value.Transitions[0].LocalEffects = []control.EffectID{"alternate.effect"}
		},
		"verifier": func(value *control.ProgramManifest) {
			value.Capabilities.Verifiers[1] = "alternate.verifier"
			value.Transitions[0].Verifier = "alternate.verifier"
		},
		"postcondition": func(value *control.ProgramManifest) {
			value.Transitions[0].TargetConditions[0].Values = []string{"alternate"}
		},
		"recovery": func(value *control.ProgramManifest) { value.Transitions[0].Interruption.Recovery = "alternate.recover" },
		"priority": func(value *control.ProgramManifest) { value.Transitions[0].Priority++ },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			candidate := programFixture()
			mutate(&candidate)
			if name == "recovery" {
				recovery := candidate.Transitions[1]
				recovery.ID = "alternate.recover"
				recovery.Interruption.Recovery = "alternate.recover"
				candidate.Transitions = append(candidate.Transitions, recovery)
			}
			program := loadManifest(t, candidate)
			if program.Fingerprint() == one.Fingerprint() {
				t.Fatalf("%s semantic change did not change fingerprint", name)
			}
		})
	}
}

func TestProgramManifestNamespaceAndCompatibilityBoundary(t *testing.T) {
	// control-law: only-compatible-validated-programs-reach-the-runtime-registry
	first := programFixture()
	first.ProgramID = "first-program"
	second := programFixture()
	second.ProgramID = "second-program"
	one := loadManifest(t, first)
	two := loadManifest(t, second)
	if one.Transitions()[0].ID == two.Transitions()[0].ID {
		t.Fatal("equal local transition IDs collided across programs")
	}
	for _, transition := range one.Transitions() {
		if !strings.HasPrefix(string(transition.ID), "first-program/") {
			t.Fatalf("transition is not program-qualified: %s", transition.ID)
		}
	}

	cases := []struct {
		name    string
		mutate  func(*control.ProgramManifest)
		runtime control.RuntimeCompatibility
		code    control.ProgramErrorCode
	}{
		{"unsupported-schema", func(value *control.ProgramManifest) { value.SchemaVersion = 2 }, runtimeFixture(), control.ProgramSchemaUnsupported},
		{"invalid-schema", func(value *control.ProgramManifest) { value.SchemaVersion = 0 }, runtimeFixture(), control.ProgramInvalid},
		{"runtime-too-old", func(value *control.ProgramManifest) { value.RequiresRuntime = ">=2.0.0" }, runtimeFixture(), control.RuntimeTooOld},
		{"malformed-runtime", func(value *control.ProgramManifest) { value.RequiresRuntime = "^1" }, runtimeFixture(), control.ProgramInvalid},
		{"malformed-program-id", func(value *control.ProgramManifest) { value.ProgramID = "../program" }, runtimeFixture(), control.ProgramInvalid},
		{"ambiguous-transition-id", func(value *control.ProgramManifest) { value.Transitions[0].ID = "other/advance" }, runtimeFixture(), control.ProgramInvalid},
		{"duplicate-transition", func(value *control.ProgramManifest) {
			value.Transitions = append(value.Transitions, value.Transitions[0])
		}, runtimeFixture(), control.ProgramInvalid},
		{"duplicate-capability", func(value *control.ProgramManifest) {
			value.Capabilities.Effects = append(value.Capabilities.Effects, value.Capabilities.Effects[0])
		}, runtimeFixture(), control.ProgramInvalid},
		{"duplicate-condition", func(value *control.ProgramManifest) {
			value.Transitions[0].SourceConditions = append(value.Transitions[0].SourceConditions, value.Transitions[0].SourceConditions[0])
		}, runtimeFixture(), control.ProgramInvalid},
		{"missing-runtime-capability", func(*control.ProgramManifest) {}, control.RuntimeCompatibility{Version: "v1.2.3"}, control.ProgramInvalid},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			manifest := programFixture()
			test.mutate(&manifest)
			_, err := control.ValidateProgram(manifest, test.runtime)
			var programErr control.ProgramError
			if !errors.As(err, &programErr) || programErr.Code != test.code {
				t.Fatalf("error = %v, want %s", err, test.code)
			}
		})
	}

	exact := programFixture()
	exact.RequiresRuntime = ">=1.2.3"
	if _, err := control.ValidateProgram(exact, runtimeFixture()); err != nil {
		t.Fatalf("exact minimum was rejected: %v", err)
	}
	above := programFixture()
	above.RequiresRuntime = ">=1.0.0"
	if _, err := control.ValidateProgram(above, runtimeFixture()); err != nil {
		t.Fatalf("runtime above minimum was rejected: %v", err)
	}
	prerelease := programFixture()
	prerelease.RequiresRuntime = ">=1.2.3"
	candidateRuntime := runtimeFixture()
	candidateRuntime.Version = "v1.2.3-dev"
	if _, err := control.ValidateProgram(prerelease, candidateRuntime); err == nil {
		t.Fatal("prerelease runtime incorrectly satisfied the matching stable minimum")
	}
}

func TestProgramSourceParserFailsClosed(t *testing.T) {
	// control-law: uninterpreted-source-cannot-reach-the-executable-program
	manifest := programFixture()
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	unknown := bytes.Replace(raw, []byte(`"program_id"`), []byte(`"requires_human":true,"program_id"`), 1)
	if _, err := control.LoadProgram(bytes.NewReader(unknown), runtimeFixture()); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown control-law field was not rejected: %v", err)
	}
	unknownTransition := bytes.Replace(raw, []byte(`"priority":1`), []byte(`"requires_human":true,"priority":1`), 1)
	if _, err := control.LoadProgram(bytes.NewReader(unknownTransition), runtimeFixture()); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown transition field was not rejected: %v", err)
	}
	duplicate := bytes.Replace(raw, []byte(`"program_id":"test-program"`), []byte(`"program_id":"test-program","program_id":"weaker-program"`), 1)
	if _, err := control.LoadProgram(bytes.NewReader(duplicate), runtimeFixture()); err == nil || !strings.Contains(err.Error(), "duplicate JSON field") {
		t.Fatalf("duplicate JSON field was not rejected: %v", err)
	}
	if _, err := control.LoadProgram(bytes.NewReader(append(raw, []byte(` {}`)...)), runtimeFixture()); err == nil {
		t.Fatal("trailing JSON was accepted")
	}
}

func TestValidatedProgramIsTheKernelRegistry(t *testing.T) {
	// control-law: kernel-consumes-the-exact-validated-fingerprinted-registry
	program := loadManifest(t, programFixture())
	kernel, err := boatstack.NewKernel(t.TempDir(), program)
	if err != nil {
		t.Fatal(err)
	}
	response, err := kernel.Handle(t.Context(), surfaces.Request{SchemaVersion: surfaces.SchemaVersion, Operation: surfaces.OperationCatalog})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Catalog) != len(programFixture().Transitions) {
		t.Fatalf("kernel catalog count = %d", len(response.Catalog))
	}
	for _, transition := range response.Catalog {
		if !strings.HasPrefix(string(transition.ID), "test-program/") || transition.Origin.ManifestFingerprint != program.Fingerprint() {
			t.Fatalf("kernel reached around validated identity: %+v", transition)
		}
	}
}

func programFixture() control.ProgramManifest {
	recovery := control.ProgramTransition{
		ID: "recover", Version: 1, SelectionClass: control.SelectionProgramRecovery, Class: control.EventRecovery,
		SourcePhases: []control.ProtocolPhase{control.PhaseRecovery}, TargetPhases: []control.ProtocolPhase{control.PhaseActive},
		RequiredIdentity: []string{"repository-id"}, Authority: []control.AuthorityClass{control.AuthorityRepository}, RequiredEvidence: []string{"snapshot"},
		OwnedResources: []string{"program.state"}, Effect: "program.recover", LocalEffects: []control.EffectID{"program.recover"}, Idempotent: true,
		Prescription: control.Prescription{Operation: "recover", ExpectedPostcondition: "active"}, SourcePredicate: "recovery-required",
		SourceConditions: []control.FacetCondition{control.KnownCondition(control.FacetRecovery, "required")}, AdmissionPredicate: "exact-admission",
		TargetPredicate: "active", TargetConditions: []control.FacetCondition{control.KnownCondition(control.FacetProgram, "current")}, Verifier: "program.current",
		Interruption: interruption("recover"), Reversibility: control.Reversible, TerminalEffect: "none",
		PrivacyClassification: "metadata-only", TelemetryClassification: "transition-receipt", CostClass: "local", Priority: 1,
	}
	advance := recovery
	advance.ID = "advance"
	advance.SelectionClass = control.SelectionProgramProgress
	advance.Class = control.EventOwnedLocal
	advance.SourcePhases = []control.ProtocolPhase{control.PhaseActive}
	advance.TargetPhases = []control.ProtocolPhase{control.PhaseTerminal}
	advance.GoalKinds = []control.GoalKind{control.GoalVerified}
	advance.Authority = []control.AuthorityClass{control.AuthorityHuman, control.AuthorityRepository}
	advance.Effect = "program.advance"
	advance.LocalEffects = []control.EffectID{"program.advance"}
	advance.Prescription = control.Prescription{Operation: "advance", Arguments: []string{"--exact"}, ExpectedPostcondition: "terminal"}
	advance.SourcePredicate = "active"
	advance.SourceConditions = []control.FacetCondition{control.KnownCondition(control.FacetProgram, "current"), control.KnownCondition(control.FacetDelivery, "active")}
	advance.TargetPredicate = "terminal"
	advance.TargetConditions = []control.FacetCondition{control.KnownCondition(control.FacetDelivery, "terminal")}
	advance.Verifier = "program.terminal"
	return control.ProgramManifest{
		SchemaVersion: control.ProgramSchemaVersion, ProgramID: "test-program", ProgramVersion: "1", RequiresRuntime: ">=1.0.0",
		Capabilities:   control.ProgramCapabilities{Effects: []string{"program.advance", "program.recover"}, Verifiers: []string{"program.current", "program.terminal"}},
		OwnedResources: []string{"program.state"}, GoalContracts: []control.GoalContract{{GoalKind: control.GoalVerified, Conditions: []control.FacetCondition{control.KnownCondition(control.FacetDelivery, "terminal")}}},
		Transitions: []control.ProgramTransition{advance, recovery},
	}
}

func runtimeFixture() control.RuntimeCompatibility {
	return control.RuntimeCompatibility{Version: "v1.2.3", Effects: []string{"program.advance", "program.recover", "alternate.effect"}, Verifiers: []string{"program.current", "program.terminal", "alternate.verifier"}}
}

func loadManifest(t *testing.T, manifest control.ProgramManifest) control.ControlProgram {
	t.Helper()
	program, err := control.ValidateProgram(manifest, runtimeFixture())
	if err != nil {
		t.Fatal(err)
	}
	return program
}

func interruption(recovery control.TransitionID) control.InterruptionContract {
	return control.InterruptionContract{Points: []string{"after-effect"}, PartialState: []string{"effect-may-exist"}, Detection: "fresh-observation", ResumeContract: "resume", RollbackContract: "rollback", CompensationContract: "compensate", Recovery: recovery, RecoveryAuthority: "repository-policy", ResumptionPredicate: "exact-state"}
}

func reverse[T any](values []T) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

func reorderTopLevelObject(t *testing.T, raw []byte) []byte {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(keys)))
	var result bytes.Buffer
	result.WriteByte('{')
	for index, key := range keys {
		if index != 0 {
			result.WriteByte(',')
		}
		encodedKey, _ := json.Marshal(key)
		result.Write(encodedKey)
		result.WriteByte(':')
		result.Write(fields[key])
	}
	result.WriteByte('}')
	return result.Bytes()
}
