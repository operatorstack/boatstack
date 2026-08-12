package delivery_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"

	boatstack "github.com/operatorstack/boatstack/boatstack"
	"github.com/operatorstack/boatstack/boatstack/delivery"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
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
	reverse(equivalent.Capabilities.CapabilitySurface)
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
	three, err := delivery.LoadProgram(bytes.NewReader(raw), runtimeFixture())
	if err != nil {
		t.Fatal(err)
	}
	if one.Fingerprint() != three.Fingerprint() {
		t.Fatal("whitespace changed executable fingerprint")
	}
	if err := three.SupervisoryProgram().Validate(); err != nil {
		t.Fatalf("validated manifest exposed an invalid supervisory program: %v", err)
	}
	if three.SupervisoryProgram().Fingerprint != three.Fingerprint() {
		t.Fatal("manifest and supervisory program fingerprints diverged")
	}
	reordered := reorderTopLevelObject(t, raw)
	four, err := delivery.LoadProgram(bytes.NewReader(reordered), runtimeFixture())
	if err != nil {
		t.Fatal(err)
	}
	if one.Fingerprint() != four.Fingerprint() {
		t.Fatal("JSON object key order changed executable fingerprint")
	}

	mutations := map[string]func(*delivery.ProgramManifest){
		"program-id": func(value *delivery.ProgramManifest) { value.ProgramID = "alternate-program" },
		"source-phase": func(value *delivery.ProgramManifest) {
			value.Transitions[0].SourcePhases = []delivery.ProtocolPhase{delivery.PhaseObserved}
		},
		"target-phase": func(value *delivery.ProgramManifest) {
			value.Transitions[0].TargetPhases = []delivery.ProtocolPhase{delivery.PhaseFrontier}
		},
		"authority": func(value *delivery.ProgramManifest) {
			value.Transitions[0].Authority = []delivery.AuthorityClass{delivery.AuthorityHuman}
		},
		"capability-surface": func(value *delivery.ProgramManifest) {
			value.Capabilities.CapabilitySurface = append(value.Capabilities.CapabilitySurface, delivery.CapabilityHumanApprove)
		},
		"effect": func(value *delivery.ProgramManifest) {
			value.Capabilities.Effects[0] = "alternate.effect"
			value.Transitions[0].Effect = "alternate.effect"
			value.Transitions[0].LocalEffects = []delivery.EffectID{"alternate.effect"}
		},
		"verifier": func(value *delivery.ProgramManifest) {
			value.Capabilities.Verifiers[1] = "alternate.verifier"
			value.Transitions[0].Verifier = "alternate.verifier"
		},
		"postcondition": func(value *delivery.ProgramManifest) {
			value.Transitions[0].TargetConditions[0].Values = []string{"alternate"}
		},
		"recovery": func(value *delivery.ProgramManifest) {
			value.Transitions[0].Interruption.Recovery = "alternate.recover"
		},
		"priority": func(value *delivery.ProgramManifest) { value.Transitions[0].Priority++ },
		"owned-facets": func(value *delivery.ProgramManifest) {
			value.Transitions[0].OwnedFacets = append(value.Transitions[0].OwnedFacets, delivery.StateFacetProduct)
		},
		"state-effect": func(value *delivery.ProgramManifest) {
			phase := string(delivery.PhaseTerminal)
			value.Transitions[0].StateEffect.Assignments = []delivery.StateAssignment{{Facet: "phase", Value: &phase}}
		},
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
		if !transition.RuntimeExecution {
			t.Fatalf("repository-authored transition %s escaped protocol-runtime classification", transition.ID)
		}
	}

	cases := []struct {
		name    string
		mutate  func(*delivery.ProgramManifest)
		runtime delivery.RuntimeCompatibility
		code    delivery.ProgramErrorCode
	}{
		{"unsupported-schema", func(value *delivery.ProgramManifest) { value.SchemaVersion = delivery.ProgramSchemaVersion + 1 }, runtimeFixture(), delivery.ProgramSchemaUnsupported},
		{"invalid-schema", func(value *delivery.ProgramManifest) { value.SchemaVersion = 0 }, runtimeFixture(), delivery.ProgramInvalid},
		{"runtime-too-old", func(value *delivery.ProgramManifest) { value.RequiresRuntime = ">=2.0.0" }, runtimeFixture(), delivery.RuntimeTooOld},
		{"malformed-runtime", func(value *delivery.ProgramManifest) { value.RequiresRuntime = "^1" }, runtimeFixture(), delivery.ProgramInvalid},
		{"malformed-program-id", func(value *delivery.ProgramManifest) { value.ProgramID = "../program" }, runtimeFixture(), delivery.ProgramInvalid},
		{"ambiguous-transition-id", func(value *delivery.ProgramManifest) { value.Transitions[0].ID = "other/advance" }, runtimeFixture(), delivery.ProgramInvalid},
		{"duplicate-transition", func(value *delivery.ProgramManifest) {
			value.Transitions = append(value.Transitions, value.Transitions[0])
		}, runtimeFixture(), delivery.ProgramInvalid},
		{"duplicate-capability", func(value *delivery.ProgramManifest) {
			value.Capabilities.Effects = append(value.Capabilities.Effects, value.Capabilities.Effects[0])
		}, runtimeFixture(), delivery.ProgramInvalid},
		{"unknown-authority-capability", func(value *delivery.ProgramManifest) {
			value.Capabilities.CapabilitySurface = []delivery.Capability{"production.nuke"}
		}, runtimeFixture(), delivery.ProgramInvalid},
		{"under-declared-kernel-effect", func(value *delivery.ProgramManifest) {
			value.Capabilities.CapabilitySurface = []delivery.Capability{delivery.CapabilityRepositoryWrite}
		}, runtimeFixture(), delivery.ProgramInvalid},
		{"host-native-state-handler", func(value *delivery.ProgramManifest) {
			value.Transitions[0].StateEffect = delivery.StateEffect{Kind: delivery.StateEffectNative, NativeHandler: "abandon-delivery"}
		}, runtimeFixture(), delivery.ProgramInvalid},
		{"duplicate-condition", func(value *delivery.ProgramManifest) {
			value.Transitions[0].SourceConditions = append(value.Transitions[0].SourceConditions, value.Transitions[0].SourceConditions[0])
		}, runtimeFixture(), delivery.ProgramInvalid},
		{"missing-runtime-capability", func(*delivery.ProgramManifest) {}, delivery.RuntimeCompatibility{Version: "v1.2.3"}, delivery.ProgramInvalid},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			manifest := programFixture()
			test.mutate(&manifest)
			_, err := delivery.ValidateProgram(manifest, test.runtime)
			var programErr delivery.ProgramError
			if !errors.As(err, &programErr) || programErr.Code != test.code {
				t.Fatalf("error = %v, want %s", err, test.code)
			}
		})
	}

	exact := programFixture()
	exact.RequiresRuntime = ">=1.2.3"
	if _, err := delivery.ValidateProgram(exact, runtimeFixture()); err != nil {
		t.Fatalf("exact minimum was rejected: %v", err)
	}
	above := programFixture()
	above.RequiresRuntime = ">=1.0.0"
	if _, err := delivery.ValidateProgram(above, runtimeFixture()); err != nil {
		t.Fatalf("runtime above minimum was rejected: %v", err)
	}
	prerelease := programFixture()
	prerelease.RequiresRuntime = ">=1.2.3"
	candidateRuntime := runtimeFixture()
	candidateRuntime.Version = "v1.2.3-dev"
	if _, err := delivery.ValidateProgram(prerelease, candidateRuntime); err == nil {
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
	if _, err := delivery.LoadProgram(bytes.NewReader(unknown), runtimeFixture()); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown control-law field was not rejected: %v", err)
	}
	unknownTransition := bytes.Replace(raw, []byte(`"priority":1`), []byte(`"requires_human":true,"priority":1`), 1)
	if _, err := delivery.LoadProgram(bytes.NewReader(unknownTransition), runtimeFixture()); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown transition field was not rejected: %v", err)
	}
	duplicate := bytes.Replace(raw, []byte(`"program_id":"test-program"`), []byte(`"program_id":"test-program","program_id":"weaker-program"`), 1)
	if _, err := delivery.LoadProgram(bytes.NewReader(duplicate), runtimeFixture()); err == nil || !strings.Contains(err.Error(), "duplicate JSON field") {
		t.Fatalf("duplicate JSON field was not rejected: %v", err)
	}
	if _, err := delivery.LoadProgram(bytes.NewReader(append(raw, []byte(` {}`)...)), runtimeFixture()); err == nil {
		t.Fatal("trailing JSON was accepted")
	}
}

func TestValidatedProgramIsTheKernelRegistry(t *testing.T) {
	// control-law: kernel-consumes-the-exact-validated-fingerprinted-registry
	program := loadManifest(t, programFixture())
	kernel, err := boatstack.NewDeliveryController(t.TempDir(), program)
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

func programFixture() delivery.ProgramManifest {
	recovery := delivery.ProgramTransition{
		ID: "recover", Version: 1, SelectionClass: delivery.SelectionProgramRecovery, Class: delivery.EventRecovery,
		SourcePhases: []delivery.ProtocolPhase{delivery.PhaseRecovery}, TargetPhases: []delivery.ProtocolPhase{delivery.PhaseActive},
		RequiredIdentity: []string{"repository-id"}, Authority: []delivery.AuthorityClass{delivery.AuthorityRepository}, RequiredCapabilities: []delivery.Capability{delivery.CapabilityRepositoryWrite}, RequiredEvidence: []string{"snapshot"},
		OwnedResources: []string{"program.state"}, OwnedFacets: []delivery.StateFacet{delivery.StateFacetControl}, StateEffect: delivery.StateEffect{Kind: delivery.StateEffectAssignments},
		Effect: "program.recover", LocalEffects: []delivery.EffectID{"program.recover"}, Idempotent: true,
		Prescription: delivery.Prescription{Operation: "recover", ExpectedPostcondition: "active"}, SourcePredicate: "recovery-required",
		SourceConditions: []delivery.FacetCondition{delivery.KnownCondition(delivery.FacetRecovery, "required")}, AdmissionPredicate: "exact-admission",
		TargetPredicate: "active", TargetConditions: []delivery.FacetCondition{delivery.KnownCondition(delivery.FacetProgram, "current")}, Verifier: "program.current",
		Interruption: interruption("recover"), Reversibility: delivery.Reversible, TerminalEffect: "none",
		PrivacyClassification: "metadata-only", TelemetryClassification: "transition-receipt", CostClass: "local", Policy: delivery.PolicyContract{ObjectiveScope: delivery.ObjectiveScopeOptionalPreserve}, Priority: 1,
	}
	advance := recovery
	advance.ID = "advance"
	advance.SelectionClass = delivery.SelectionProgramProgress
	advance.Class = delivery.EventOwnedLocal
	advance.SourcePhases = []delivery.ProtocolPhase{delivery.PhaseActive}
	advance.TargetPhases = []delivery.ProtocolPhase{delivery.PhaseTerminal}
	advance.ObjectiveKinds = []delivery.ObjectiveKind{delivery.ObjectiveVerified}
	advance.Authority = []delivery.AuthorityClass{delivery.AuthorityHuman, delivery.AuthorityRepository}
	advance.Effect = "program.advance"
	advance.LocalEffects = []delivery.EffectID{"program.advance"}
	advance.Prescription = delivery.Prescription{Operation: "advance", Arguments: []string{"--exact"}, ExpectedPostcondition: "terminal"}
	advance.SourcePredicate = "active"
	advance.SourceConditions = []delivery.FacetCondition{delivery.KnownCondition(delivery.FacetProgram, "current"), delivery.KnownCondition(delivery.FacetDelivery, "active")}
	advance.TargetPredicate = "terminal"
	advance.TargetConditions = []delivery.FacetCondition{delivery.KnownCondition(delivery.FacetDelivery, "terminal")}
	advance.Verifier = "program.terminal"
	advance.Policy.ObjectiveScope = delivery.ObjectiveScopeBoundExact
	return delivery.ProgramManifest{
		SchemaVersion: delivery.ProgramSchemaVersion, ProgramID: "test-program", ProgramVersion: "1", RequiresRuntime: ">=1.0.0",
		Capabilities: delivery.ProgramCapabilities{
			Effects: []string{"program.advance", "program.recover"}, Verifiers: []string{"program.current", "program.terminal"},
			CapabilitySurface: []delivery.Capability{delivery.CapabilityRepositoryWrite, delivery.CapabilityCommandExecute},
		},
		OwnedResources: []string{"program.state"}, ObjectiveContracts: []delivery.ObjectiveContract{{ObjectiveKind: delivery.ObjectiveVerified, Conditions: []delivery.FacetCondition{delivery.KnownCondition(delivery.FacetDelivery, "terminal")}}},
		Transitions: []delivery.ProgramTransition{advance, recovery},
	}
}

func runtimeFixture() delivery.RuntimeCompatibility {
	return delivery.RuntimeCompatibility{Version: "v1.2.3", Effects: []string{"program.advance", "program.recover", "alternate.effect"}, Verifiers: []string{"program.current", "program.terminal", "alternate.verifier"}, Capabilities: []delivery.Capability{delivery.CapabilityRepositoryWrite, delivery.CapabilityCommandExecute, delivery.CapabilityHumanApprove}}
}

func loadManifest(t *testing.T, manifest delivery.ProgramManifest) delivery.ControlProgram {
	t.Helper()
	program, err := delivery.ValidateProgram(manifest, runtimeFixture())
	if err != nil {
		t.Fatal(err)
	}
	return program
}

func interruption(recovery delivery.TransitionID) delivery.InterruptionContract {
	return delivery.InterruptionContract{Points: []string{"after-effect"}, PartialState: []string{"effect-may-exist"}, Detection: "fresh-observation", ResumeContract: "resume", RollbackContract: "rollback", CompensationContract: "compensate", Recovery: recovery, RecoveryAuthority: "repository-policy", ResumptionPredicate: "exact-state"}
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
