package control_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/control"
	"github.com/operatorstack/boatstack/boatstack/core"
	"github.com/operatorstack/boatstack/boatstack/distribution"
	"github.com/operatorstack/boatstack/boatstack/extension"
	"github.com/operatorstack/boatstack/boatstack/extension/releasenote"
	"github.com/operatorstack/boatstack/boatstack/flow/standard"
)

type staticProgramRuntimeDefinition struct {
	manifest control.ProgramRuntimeManifest
}

func (f staticProgramRuntimeDefinition) RuntimeManifest(context.Context) (control.ProgramRuntimeManifest, error) {
	return f.manifest, nil
}

func TestStandardProgramHasExplicitStableComposition(t *testing.T) {
	// control-law: compile-produces-one-immutable-registry-with-explicit-origins
	one, err := distribution.StandardProgram(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	two, err := distribution.StandardProgram(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if one.Fingerprint() != two.Fingerprint() {
		t.Fatalf("identical compilation drifted: %s != %s", one.Fingerprint(), two.Fingerprint())
	}
	summary := one.Summary()
	if summary.CoreTransitionCount != 33 || summary.RuntimeTransitionCount != 30 || summary.ExtensionTransitionCount != 0 || summary.TotalTransitionCount != 63 {
		t.Fatalf("compiled counts = %+v", summary)
	}
	counts := map[string]int{}
	for _, transition := range one.Transitions() {
		counts[string(transition.Origin.Kind)]++
		if transition.Owner != transition.Origin.ID || len(transition.Origin.ManifestFingerprint) != 64 {
			t.Fatalf("transition lost compiled ownership: %+v", transition)
		}
	}
	if counts["core-system"] != 33 || counts["control-program"] != 30 || counts["extension"] != 0 {
		t.Fatalf("origin counts = %#v", counts)
	}
}

func TestProgramFingerprintBindsCompositionAndPolicyInputs(t *testing.T) {
	// control-law: any-control-law-input-change-changes-program-identity
	base, err := distribution.StandardProgram(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	reference := releasenote.Definition()
	manifest, err := reference.ExtensionManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := map[string]control.ExtensionManifest{}
	version := cloneManifest(t, manifest)
	version.Version = "1.0.1"
	variants["extension-version"] = version
	executable := cloneManifest(t, manifest)
	executable.ExecutableSHA256 = strings.Repeat("a", 64)
	variants["executable"] = executable
	settings := cloneManifest(t, manifest)
	settings.Settings = json.RawMessage(`{"required":true}`)
	settings.SettingsSchema = json.RawMessage(`{"type":"object","properties":{"required":{"type":"boolean"}},"additionalProperties":false}`)
	variants["extension-settings"] = settings
	settingsSchema := cloneManifest(t, manifest)
	settingsSchema.SettingsSchema = json.RawMessage(`{"type":"object","description":"alternate valid schema","additionalProperties":false}`)
	variants["extension-settings-schema"] = settingsSchema
	resource := cloneManifest(t, manifest)
	resource.OwnedResources = []string{"boatstack.release-note.alternate-evidence"}
	resource.Transitions[0].OwnedResources = append([]string(nil), resource.OwnedResources...)
	variants["resource-ownership"] = resource
	verifier := cloneManifest(t, manifest)
	verifier.Verifiers = []string{"boatstack.release-note.alternate-verifier"}
	verifier.Transitions[0].Verifier = verifier.Verifiers[0]
	variants["verifier"] = verifier
	recovery := cloneManifest(t, manifest)
	recoveryTransition := manifest.Transitions[0]
	recoveryTransition.ID = "boatstack.release-note.recover"
	recoveryTransition.Class = control.EventRecovery
	recoveryTransition.SelectionClass = control.SelectionExtensionRecovery
	recoveryTransition.SourcePhases = []control.ProtocolPhase{control.PhaseRecovery}
	recoveryTransition.TargetPhases = []control.ProtocolPhase{control.PhaseActive}
	recoveryTransition.Effect = "boatstack.release-note.recover-effect"
	recoveryTransition.LocalEffects = []control.EffectID{recoveryTransition.Effect}
	recoveryTransition.Verifier = "boatstack.release-note.recover-verifier"
	recoveryTransition.Interruption.Recovery = "recovery.escalate"
	recoveryTransition.Priority = 0
	recovery.Transitions = append(recovery.Transitions, recoveryTransition)
	recovery.Effects = append(recovery.Effects, string(recoveryTransition.Effect))
	recovery.Verifiers = append(recovery.Verifiers, recoveryTransition.Verifier)
	recovery.RecoveryTransitions = []control.TransitionID{recoveryTransition.ID}
	variants["recovery-declaration"] = recovery

	for name, variant := range variants {
		t.Run(name, func(t *testing.T) {
			definition, err := extension.NewInProcess(variant, reference.Runtime())
			if err != nil {
				t.Fatal(err)
			}
			program, err := distribution.StandardProgram(context.Background(), definition)
			if err != nil {
				t.Fatal(err)
			}
			if program.Fingerprint() == base.Fingerprint() {
				t.Fatalf("%s did not change program fingerprint", name)
			}
		})
	}

	policyOne, err := control.Compile(context.Background(), control.CompileRequest{KernelVersion: "kernel", Core: core.System(), Runtime: standard.Definition(), Settings: map[string]any{"policy": "one"}})
	if err != nil {
		t.Fatal(err)
	}
	policyTwo, err := control.Compile(context.Background(), control.CompileRequest{KernelVersion: "kernel", Core: core.System(), Runtime: standard.Definition(), Settings: map[string]any{"policy": "two"}})
	if err != nil {
		t.Fatal(err)
	}
	if policyOne.Fingerprint() == policyTwo.Fingerprint() {
		t.Fatal("repository policy did not change program fingerprint")
	}
}

func TestCompileEnforcesDeclaredComponentSchemas(t *testing.T) {
	// control-law: component-settings-cannot-cross-runtime-boundaries-unvalidated
	reference := releasenote.Definition()
	manifest, err := reference.ExtensionManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, schema, settings string
	}{
		{"missing-required", `{"type":"object","required":["mode"]}`, `{}`},
		{"wrong-type", `{"type":"object","properties":{"mode":{"type":"string"}}}`, `{"mode":1}`},
		{"additional-property", `{"type":"object","additionalProperties":false}`, `{"mode":"strict"}`},
		{"invalid-schema", `{"type":"object","required":"mode"}`, `{}`},
	}
	for _, test := range cases {
		t.Run("extension-"+test.name, func(t *testing.T) {
			candidate := cloneManifest(t, manifest)
			candidate.SettingsSchema = json.RawMessage(test.schema)
			candidate.Settings = json.RawMessage(test.settings)
			definition, err := extension.NewInProcess(candidate, reference.Runtime())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := distribution.StandardProgram(context.Background(), definition); err == nil {
				t.Fatal("invalid extension settings or schema compiled")
			}
		})
	}
	flow, err := standard.Definition().RuntimeManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	flow.ConfigurationSchema = json.RawMessage(`{"type":"object","required":["mode"],"additionalProperties":false}`)
	flow.Settings = json.RawMessage(`{}`)
	if _, err := control.Compile(context.Background(), control.CompileRequest{KernelVersion: "kernel", Core: core.System(), Runtime: staticProgramRuntimeDefinition{manifest: flow}}); err == nil {
		t.Fatal("ProgramRuntime settings that violate ConfigurationSchema compiled")
	}
}

func TestComponentsMustDeclareTheirOwnSelectionSemantics(t *testing.T) {
	// control-law: generic-compiler-never-infers-flow-order-from-transition-ids
	flow, err := standard.Definition().RuntimeManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	flow.Transitions[0].SelectionClass = ""
	if _, err := control.Compile(context.Background(), control.CompileRequest{
		KernelVersion: "kernel", Core: core.System(), Runtime: staticFlow{manifest: flow},
	}); err == nil || !strings.Contains(err.Error(), "selection class") {
		t.Fatalf("flow without an explicit selection class was accepted: %v", err)
	}

	flow, err = standard.Definition().RuntimeManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	flow.Transitions[0].SelectionClass = control.SelectionSystemRecovery
	if _, err := control.Compile(context.Background(), control.CompileRequest{
		KernelVersion: "kernel", Core: core.System(), Runtime: staticFlow{manifest: flow},
	}); err == nil || !strings.Contains(err.Error(), "SYSTEM_RECOVERY") {
		t.Fatalf("flow claimed CoreSystem recovery precedence: %v", err)
	}
}

func TestProgramRuntimeCannotClaimCoreSystemResources(t *testing.T) {
	// control-law: every-resource-has-exactly-one-component-owner
	coreManifest, err := core.System().CoreManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var coreResource string
	for _, transition := range coreManifest.Transitions {
		if len(transition.OwnedResources) != 0 {
			coreResource = transition.OwnedResources[0]
			break
		}
	}
	if coreResource == "" {
		t.Fatal("CoreSystem fixture declares no owned resource")
	}
	flow, err := standard.Definition().RuntimeManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	flow.OwnedResources = append(flow.OwnedResources, coreResource)
	flow.Transitions[0].OwnedResources = append(flow.Transitions[0].OwnedResources, coreResource)
	if _, err := control.Compile(context.Background(), control.CompileRequest{
		KernelVersion: "kernel", Core: core.System(), Runtime: staticFlow{manifest: flow},
	}); err == nil || !strings.Contains(err.Error(), "overlapping owners") {
		t.Fatalf("ProgramRuntime claimed CoreSystem resource %q: %v", coreResource, err)
	}
}

func TestExtensionCompilationRejectsBoundaryViolations(t *testing.T) {
	// control-law: extensions-are-namespaced-additive-and-declarative
	reference := releasenote.Definition()
	manifest, err := reference.ExtensionManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*control.ExtensionManifest){
		"reserved-component-id": func(value *control.ExtensionManifest) { value.ID = standard.ID },
		"unnamespaced-fact":     func(value *control.ExtensionManifest) { value.Facts[0] = "present" },
		"raw-priority":          func(value *control.ExtensionManifest) { value.Transitions[0].Priority = 7 },
		"undeclared-effect":     func(value *control.ExtensionManifest) { value.Effects = nil },
		"undeclared-verifier":   func(value *control.ExtensionManifest) { value.Verifiers = nil },
		"phantom-recovery": func(value *control.ExtensionManifest) {
			value.RecoveryTransitions = []control.TransitionID{"boatstack.release-note.missing-recovery"}
		},
		"invalid-goal-status": func(value *control.ExtensionManifest) {
			value.GoalConstraints[0].Conditions[0].Statuses = []control.FactStatus{"invented"}
		},
		"goal-selection-without-matching-obligation": func(value *control.ExtensionManifest) {
			value.Transitions[0].GoalKinds = []control.GoalKind{control.GoalVerified}
		},
		"goal-selection-does-not-discharge-obligation": func(value *control.ExtensionManifest) {
			value.Transitions[0].TargetConditions[0].Values = []string{"missing"}
		},
		"recovery-selection-on-progress": func(value *control.ExtensionManifest) {
			value.Transitions[0].SelectionClass = control.SelectionExtensionRecovery
		},
		"program-reconciliation-claim": func(value *control.ExtensionManifest) {
			value.Transitions[0].Policy.ReconcilesProgram = true
		},
		"requested-goal-binding-claim": func(value *control.ExtensionManifest) {
			value.Transitions[0].Policy.BindsRequestedGoal = true
		},
		"foreign-target": func(value *control.ExtensionManifest) {
			value.Transitions[0].TargetConditions = []control.FacetCondition{control.KnownCondition(control.FacetPlan, "approved")}
		},
		"undeclared-owned-target": func(value *control.ExtensionManifest) {
			value.Transitions[0].TargetConditions = []control.FacetCondition{control.KnownCondition(control.FacetName(value.ID+".phantom"), "verified")}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			value := cloneManifest(t, manifest)
			mutate(&value)
			definition, err := extension.NewInProcess(value, reference.Runtime())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := distribution.StandardProgram(context.Background(), definition); err == nil {
				t.Fatalf("%s extension violation was accepted", name)
			}
		})
	}

	left := declarationOnlyExtension{id: "example.left", dependencies: []string{"example.right"}}
	right := declarationOnlyExtension{id: "example.right", dependencies: []string{"example.left"}}
	if _, err := distribution.StandardProgram(context.Background(), left, right); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("dependency cycle error = %v", err)
	}
}

func TestControlProgramAccessorsCannotMutateCompiledBytes(t *testing.T) {
	// control-law: compiled-control-program-is-immutable
	program, err := distribution.StandardProgram(context.Background(), releasenote.Definition())
	if err != nil {
		t.Fatal(err)
	}
	originalFingerprint := program.Fingerprint()
	transitions := program.Transitions()
	transitions[0].SourcePhases[0] = control.PhaseAbandoned
	transitions[0].SourceConditions[0].Values = []string{"mutated"}
	extensions := program.Extensions()
	extensions[0].Manifest.Facts[0] = "mutated.fact.id"
	extensions[0].Manifest.Settings = json.RawMessage(`{"mutated":true}`)
	flow := program.ProgramRuntime()
	flow.Manifest.GoalContracts[0].Conditions[0].Values = []string{"mutated"}

	if program.Fingerprint() != originalFingerprint || program.Transitions()[0].SourcePhases[0] == control.PhaseAbandoned ||
		program.Extensions()[0].Manifest.Facts[0] == "mutated.fact.id" || program.ProgramRuntime().Manifest.GoalContracts[0].Conditions[0].Values[0] == "mutated" {
		t.Fatal("public accessor mutated the compiled ControlProgram")
	}
}

func TestExtensionGoalConditionsAreConjunctive(t *testing.T) {
	// control-law: extension-terminal-set-is-a-subset-of-control-program-terminal-set
	base, err := distribution.StandardProgram(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	extended, err := distribution.StandardProgram(context.Background(), releasenote.Definition())
	if err != nil {
		t.Fatal(err)
	}
	conditionCount := func(program control.ControlProgram) int {
		for _, contract := range program.RuntimeGoalContracts().All() {
			if contract.GoalKind == control.GoalVerified {
				return len(contract.Conditions)
			}
		}
		return 0
	}
	if conditionCount(extended) != conditionCount(base) {
		t.Fatalf("release-note extension unexpectedly changed the verified goal")
	}
	for _, goal := range []control.GoalKind{control.GoalOpenPR, control.GoalMerged} {
		baseCount, extendedCount := 0, 0
		for _, contract := range base.RuntimeGoalContracts().All() {
			if contract.GoalKind == goal {
				baseCount = len(contract.Conditions)
			}
		}
		for _, contract := range extended.RuntimeGoalContracts().All() {
			if contract.GoalKind == goal {
				extendedCount = len(contract.Conditions)
			}
		}
		if extendedCount != baseCount+1 {
			t.Fatalf("goal %s conditions: base=%d extended=%d", goal, baseCount, extendedCount)
		}
	}
}

func TestExtensionOrderDoesNotChangeProgramIdentity(t *testing.T) {
	// control-law: extension-observation-and-compilation-order-is-deterministic
	left := declarationOnlyExtension{id: "example.left", goalConditions: []control.FacetCondition{control.KnownCondition(control.FacetPlan, "locked")}}
	right := declarationOnlyExtension{id: "example.right", goalConditions: []control.FacetCondition{control.KnownCondition(control.FacetConfiguration, "verified")}}
	one, err := distribution.StandardProgram(context.Background(), left, right)
	if err != nil {
		t.Fatal(err)
	}
	two, err := distribution.StandardProgram(context.Background(), right, left)
	if err != nil {
		t.Fatal(err)
	}
	if one.Fingerprint() != two.Fingerprint() {
		t.Fatalf("extension order changed program identity: %s != %s", one.Fingerprint(), two.Fingerprint())
	}
}

type declarationOnlyExtension struct {
	id             string
	dependencies   []string
	goalConditions []control.FacetCondition
}

type staticFlow struct {
	manifest control.ProgramRuntimeManifest
}

func (s staticFlow) RuntimeManifest(context.Context) (control.ProgramRuntimeManifest, error) {
	return s.manifest, nil
}

func cloneManifest(t *testing.T, value control.ExtensionManifest) control.ExtensionManifest {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result control.ExtensionManifest
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func (e declarationOnlyExtension) ExtensionManifest(context.Context) (control.ExtensionManifest, error) {
	var constraints []control.GoalConstraint
	if len(e.goalConditions) != 0 {
		constraints = []control.GoalConstraint{{GoalKind: control.GoalOpenPR, Conditions: append([]control.FacetCondition(nil), e.goalConditions...)}}
	}
	return control.ExtensionManifest{
		ID: e.id, Version: "1.0.0", ProtocolVersion: control.ExtensionProtocolVersion,
		SettingsSchema:        json.RawMessage(`{"type":"object"}`),
		PrivacyClassification: "metadata-only", TelemetryClassification: "transition-receipt",
		Dependencies: append([]string(nil), e.dependencies...), GoalConstraints: constraints,
	}, nil
}
