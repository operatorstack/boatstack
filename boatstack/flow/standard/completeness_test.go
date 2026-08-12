package standard_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/testprogram"
)

func sourceRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate source inventory")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func TestEveryControllingFacetAndEventIsClassifiedByTheRuntimeCatalog(t *testing.T) {
	// control-law: runtime-catalog-is-the-complete-executable-model
	registry := testprogram.StandardRegistry()
	classified := map[model.FacetName]bool{model.FacetPhase: true}
	families := map[string]int{}
	for _, transition := range registry.All() {
		for _, condition := range append(append([]catalog.FacetCondition(nil), transition.SourceConditions...), transition.TargetConditions...) {
			classified[condition.Facet] = true
		}
		family := familyFor(transition.ID)
		families[family]++
		if transition.Controllable() && transition.Effect != catalog.EffectID(transition.ID) {
			t.Errorf("transition %s effect=%s; effect identity must be exact", transition.ID, transition.Effect)
		}
		if transition.Class == catalog.EventOwnedExternal && !containsAuthority(transition.AuthorityAll, catalog.AuthorityProvider) {
			t.Errorf("external transition %s does not require provider authority", transition.ID)
		}
	}
	for _, facet := range model.ControllingFacets() {
		if !classified[facet] {
			t.Errorf("controlling facet %s is absent from executable predicates", facet)
		}
	}
	want := map[string]int{"invocation-engagement": 6, "installation-runtime-configuration": 9, "catalog": 1, "objective-plan": 9, "workspace": 8, "gate-evidence-delivery": 8, "publication": 6, "recovery": 3, "external": 13}
	for family, count := range want {
		if families[family] != count {
			t.Errorf("family %s=%d, want %d", family, families[family], count)
		}
	}
}

func containsAuthority(values []catalog.AuthorityClass, wanted catalog.AuthorityClass) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func familyFor(id catalog.TransitionID) string {
	value := string(id)
	switch {
	case strings.HasPrefix(value, "engagement."), strings.HasPrefix(value, "invocation."), strings.HasPrefix(value, "repository."):
		return "invocation-engagement"
	case strings.HasPrefix(value, "installation."), strings.HasPrefix(value, "runtime."), strings.HasPrefix(value, "configuration."):
		return "installation-runtime-configuration"
	case strings.HasPrefix(value, "catalog."):
		return "catalog"
	case strings.HasPrefix(value, "objective."), strings.HasPrefix(value, "plan."):
		return "objective-plan"
	case strings.HasPrefix(value, "workspace."):
		return "workspace"
	case strings.HasPrefix(value, "gate."), strings.HasPrefix(value, "evidence."), strings.HasPrefix(value, "delivery."):
		return "gate-evidence-delivery"
	case strings.HasPrefix(value, "publication."):
		return "publication"
	case strings.HasPrefix(value, "recovery."):
		return "recovery"
	case strings.HasPrefix(value, "external."):
		return "external"
	default:
		return "unclassified"
	}
}

func TestSourceInventoryHasNoWriterOrLifecycleAuthorityOutsideOwnedPackages(t *testing.T) {
	// control-law: adapters-cannot-grow-a-second-controller-or-writer
	root := sourceRoot(t)
	writerCalls := map[string]bool{
		"WriteFile": true, "Rename": true, "Remove": true, "RemoveAll": true, "Mkdir": true, "MkdirAll": true,
		"Create": true, "CreateTemp": true, "MkdirTemp": true, "OpenFile": true,
	}
	classifiedFiles := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if !classifiedProductionFile(relative) {
			t.Errorf("production source %s has no V2 ownership classification", relative)
		}
		classifiedFiles++
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		imports := map[string]string{}
		for _, item := range parsed.Imports {
			value, _ := strconv.Unquote(item.Path.Value)
			name := filepath.Base(value)
			if item.Name != nil {
				name = item.Name.Name
			}
			imports[name] = value
			if strings.Contains(value, "/internal/deliverycontrol") {
				t.Errorf("%s imports deleted shadow controller %s", relative, value)
			}
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			owner, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}
			importPath := imports[owner.Name]
			if importPath == "os" && writerCalls[selector.Sel.Name] && !strings.HasPrefix(relative, "internal/softwaredelivery/effects/") && !strings.HasPrefix(relative, "internal/runtime/") {
				t.Errorf("managed writer os.%s escaped effects package in %s", selector.Sel.Name, relative)
			}
			if importPath == "os/exec" && (selector.Sel.Name == "Command" || selector.Sel.Name == "CommandContext") {
				if relative != "internal/softwaredelivery/effects/command_boundary.go" && relative != "internal/softwaredelivery/plant/resolver.go" && relative != "extension/subprocess/subprocess.go" && relative != "internal/runtime/exec_windows.go" && relative != "internal/runtime/flow_files.go" {
					t.Errorf("unclassified command boundary in %s", relative)
				}
			}
			if strings.HasSuffix(importPath, "/internal/softwaredelivery/model") && lifecycleSelector(selector.Sel.Name) &&
				!strings.HasPrefix(relative, "internal/softwaredelivery/") && !strings.HasPrefix(relative, "internal/softwaredelivery/effects/") && !strings.HasPrefix(relative, "internal/softwaredelivery/plant/") &&
				!strings.HasPrefix(relative, "delivery/") && !strings.HasPrefix(relative, "flow/") && !strings.HasPrefix(relative, "extension/") {
				t.Errorf("lifecycle selector model.%s escaped kernel/plant/effects in %s", selector.Sel.Name, relative)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if classifiedFiles == 0 {
		t.Fatal("source inventory was empty")
	}
	if entries, err := os.ReadDir(filepath.Join(root, "internal", "deliverycontrol")); err == nil && len(entries) != 0 {
		t.Fatalf("deleted shadow controller still contains %d entries", len(entries))
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestFlowCompilerMutationSitesMapToFlowCompileEvent(t *testing.T) {
	// control-law: every-flow-projection-mutation-is-one-classified-authoring-event
	root := sourceRoot(t)
	expected := map[string]map[string]int{
		"cmd/boatstack-helper/flow_command.go": {
			"runtime.AtomicWrite": 2, "runtime.RemoveGeneratedFile": 1,
		},
		"internal/runtime/flow_files.go": {
			"os.MkdirAll": 1, "os.CreateTemp": 1, "os.Remove": 2, "os.Rename": 1,
		},
	}
	for relative, wanted := range expected {
		path := filepath.Join(root, filepath.FromSlash(relative))
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		imports := map[string]string{}
		for _, item := range parsed.Imports {
			value, _ := strconv.Unquote(item.Path.Value)
			name := filepath.Base(value)
			if item.Name != nil {
				name = item.Name.Name
			}
			imports[name] = value
		}
		observed := map[string]int{}
		ast.Inspect(parsed, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			owner, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}
			importPath := imports[owner.Name]
			switch {
			case importPath == "os" && writerCallsForInventory(selector.Sel.Name):
				observed["os."+selector.Sel.Name]++
			case strings.HasSuffix(importPath, "/internal/runtime") && (selector.Sel.Name == "AtomicWrite" || selector.Sel.Name == "RemoveGeneratedFile"):
				observed["runtime."+selector.Sel.Name]++
			}
			return true
		})
		if !reflect.DeepEqual(observed, wanted) {
			t.Fatalf("flow.compile mutation inventory for %s = %v, want %v", relative, observed, wanted)
		}
	}
}

func writerCallsForInventory(name string) bool {
	switch name {
	case "WriteFile", "Rename", "Remove", "RemoveAll", "Mkdir", "MkdirAll", "Create", "CreateTemp", "MkdirTemp", "OpenFile":
		return true
	default:
		return false
	}
}

func TestEveryControllableRuntimeEventHasAnExecutableStateReducer(t *testing.T) {
	// control-law: registry-entry-cannot-exist-without-declared-runtime-effect-reduction
	registry := testprogram.StandardRegistry()
	for _, transition := range registry.All() {
		if !transition.Controllable() {
			if transition.StateEffect.Kind != "" || len(transition.OwnedFacets) != 0 {
				t.Errorf("observed transition %s owns a durable state effect", transition.ID)
			}
			continue
		}
		if len(transition.OwnedFacets) == 0 {
			t.Errorf("controllable transition %s has no declared durable state facets", transition.ID)
		}
		if transition.StateEffect.Kind != catalog.StateEffectAssignments && transition.StateEffect.Kind != catalog.StateEffectNative {
			t.Errorf("controllable transition %s has no executable declared state effect", transition.ID)
		}
	}
}

func TestMalformedDeclaredStateEffectsFailClosedAtCatalogBoundary(t *testing.T) {
	// control-law: malformed-state-declarations-never-reach-effect-preparation
	cases := []struct {
		name   string
		mutate func(*catalog.Transition)
	}{
		{"unknown-field", func(value *catalog.Transition) { value.StateEffect.Assignments[0].Facet = "not_a_state_field" }},
		{"unowned-field", func(value *catalog.Transition) { value.OwnedFacets = []model.StateFacet{model.StateFacetControl} }},
		{"undeclared-parameter", func(value *catalog.Transition) {
			value.StateEffect.Assignments[0].Value = nil
			value.StateEffect.Assignments[0].ValueFrom.Parameter = "not_declared"
		}},
		{"optional-assignment-parameter", func(value *catalog.Transition) {
			value.Parameters = append(value.Parameters, catalog.ParameterSpec{Name: "optional_state", Required: false})
			value.StateEffect.Assignments[0].Value = nil
			value.StateEffect.Assignments[0].ValueFrom.Parameter = "optional_state"
		}},
		{"unknown-admission-source", func(value *catalog.Transition) {
			value.StateEffect.Assignments[0].Value = nil
			value.StateEffect.Assignments[0].ValueFrom.Admission = "not_admitted"
		}},
		{"invalid-state-literal", func(value *catalog.Transition) { *value.StateEffect.Assignments[0].Value = "NOT_A_PHASE" }},
		{"target-mismatched-state-literal", func(value *catalog.Transition) { *value.StateEffect.Assignments[0].Value = string(model.PhaseDormant) }},
		{"unmodeled-apply-precondition", func(value *catalog.Transition) {
			value.StateEffect.Preconditions = []catalog.StatePrecondition{{Facet: "phase", Values: []string{string(model.PhaseDormant)}}}
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			transitions := testprogram.StandardRegistry().All()
			for index := range transitions {
				if transitions[index].ID == "plan.create" {
					test.mutate(&transitions[index])
					break
				}
			}
			if _, err := catalog.New(transitions); err == nil {
				t.Fatal("malformed declared state effect reached the runtime registry")
			}
		})
	}
}

func TestNativeStateHandlersAreBoundToAuthorizedSemantics(t *testing.T) {
	// control-law: a named native handler cannot grant semantics beyond its component, effect, facets, or objective policy
	cases := []struct {
		name       string
		transition catalog.TransitionID
		mutate     func(*catalog.Transition)
	}{
		{"unknown-handler", "plan.approve", func(value *catalog.Transition) { value.StateEffect.NativeHandler = "unknown-handler" }},
		{"untrusted-component", "plan.approve", func(value *catalog.Transition) { value.Origin.ID = "repository-program" }},
		{"mismatched-effect", "plan.approve", func(value *catalog.Transition) { value.Effect = "plan.create" }},
		{"mismatched-facets", "plan.approve", func(value *catalog.Transition) { value.OwnedFacets = []model.StateFacet{model.StateFacetControl} }},
		{"objective-bind-policy", "objective.bind", func(value *catalog.Transition) {
			value.Policy.BindsRequestedObjective = false
			value.Policy.ObjectiveScope = catalog.ObjectiveScopeBoundExact
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			transitions := testprogram.StandardRegistry().All()
			for index := range transitions {
				if transitions[index].ID == test.transition {
					test.mutate(&transitions[index])
					break
				}
			}
			if _, err := catalog.New(transitions); err == nil {
				t.Fatal("invalid native handler contract reached the runtime registry")
			}
		})
	}
}

func TestDeclarativeAssignmentsCloseDurableStateInvariants(t *testing.T) {
	// control-law: every accepted assignment set preserves durable-state validity for every resolver-matching source
	cases := []struct {
		name       string
		transition catalog.TransitionID
		mutate     func(*catalog.Transition)
	}{
		{"verified-runtime-without-source", "installation.update", func(value *catalog.Transition) {
			value.StateEffect.Assignments = removeAssignment(value.StateEffect.Assignments, "runtime_source")
		}},
		{"managed-workspace-without-source-identity", "workspace.cut", func(value *catalog.Transition) {
			value.StateEffect.Assignments = removeAssignment(value.StateEffect.Assignments, "workspace_source_ref")
		}},
		{"recovery-without-cause", "recovery.escalate", func(value *catalog.Transition) {
			value.StateEffect.Assignments = removeAssignment(value.StateEffect.Assignments, "recovery_cause")
		}},
		{"terminal-with-active-phase", "workspace.abandon", func(value *catalog.Transition) {
			active := string(model.PhaseActive)
			value.TargetPhases = []model.ProtocolPhase{model.PhaseActive}
			for index := range value.StateEffect.Assignments {
				if value.StateEffect.Assignments[index].Facet == "phase" {
					value.StateEffect.Assignments[index].Value = &active
				}
			}
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			transitions := testprogram.StandardRegistry().All()
			for index := range transitions {
				if transitions[index].ID == test.transition {
					test.mutate(&transitions[index])
					break
				}
			}
			if _, err := catalog.New(transitions); err == nil {
				t.Fatal("durably incomplete assignment set reached the runtime registry")
			}
		})
	}
}

func removeAssignment(values []catalog.StateAssignment, facet string) []catalog.StateAssignment {
	result := make([]catalog.StateAssignment, 0, len(values))
	for _, value := range values {
		if value.Facet != facet {
			result = append(result, value)
		}
	}
	return result
}

func TestPackageImportsPreserveControlProgramDependencyDirection(t *testing.T) {
	// control-law: general-kernel-cannot-depend-on-software-delivery
	root := sourceRoot(t)
	forbiddenKernel := []string{"/internal/softwaredelivery", "/flow/standard", "/distribution", "/sdk", "/cmd/boatstack-helper"}
	forbiddenFlow := []string{"/distribution", "/sdk", "/cmd/boatstack-helper", "/internal/softwaredelivery/surfaces"}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		kernelOwned := strings.HasPrefix(relative, "kernel/")
		flowOwned := strings.HasPrefix(relative, "flow/")
		if !kernelOwned && !flowOwned {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, item := range parsed.Imports {
			value, _ := strconv.Unquote(item.Path.Value)
			for _, forbidden := range forbiddenKernel {
				if kernelOwned && strings.Contains(value, forbidden) {
					t.Errorf("kernel mechanism %s imports forbidden layer %s", relative, value)
				}
			}
			for _, forbidden := range forbiddenFlow {
				if flowOwned && strings.Contains(value, forbidden) {
					t.Errorf("program runtime %s imports forbidden surface %s", relative, value)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func classifiedProductionFile(relative string) bool {
	return relative == "delivery_controller.go" || relative == "program_effects.go" || relative == "program_observer.go" || strings.HasPrefix(relative, "cmd/boatstack-helper/") ||
		strings.HasPrefix(relative, "controlprogram/") ||
		strings.HasPrefix(relative, "delivery/") || strings.HasPrefix(relative, "core/") ||
		strings.HasPrefix(relative, "flow/") || strings.HasPrefix(relative, "distribution/") || strings.HasPrefix(relative, "extension/") ||
		strings.HasPrefix(relative, "internal/softwaredelivery/") ||
		strings.HasPrefix(relative, "internal/buildinfo/") || strings.HasPrefix(relative, "internal/runtime/") ||
		strings.HasPrefix(relative, "internal/retromine/") || strings.HasPrefix(relative, "internal/testprogram/") || strings.HasPrefix(relative, "kernel/") || strings.HasPrefix(relative, "sdk/") ||
		strings.HasPrefix(relative, "analysis/")
}

func lifecycleSelector(name string) bool {
	for _, prefix := range []string{"Phase", "Engagement", "Delivery", "Workspace", "Plan", "Configuration", "Runtime", "Publication", "Verification", "Recovery", "Transaction", "Terminal"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
