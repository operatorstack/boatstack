package standard_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
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
	want := map[string]int{"invocation-engagement": 6, "installation-runtime-configuration": 9, "catalog": 1, "goal-plan": 9, "workspace": 8, "gate-evidence-delivery": 8, "publication": 6, "recovery": 3, "external": 13}
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
	case strings.HasPrefix(value, "goal."), strings.HasPrefix(value, "plan."):
		return "goal-plan"
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
			if importPath == "os" && writerCalls[selector.Sel.Name] && !strings.HasPrefix(relative, "internal/effects/") && !strings.HasPrefix(relative, "internal/runtime/") {
				t.Errorf("managed writer os.%s escaped effects package in %s", selector.Sel.Name, relative)
			}
			if importPath == "os/exec" && (selector.Sel.Name == "Command" || selector.Sel.Name == "CommandContext") {
				if relative != "internal/effects/command_boundary.go" && relative != "internal/plant/resolver.go" && relative != "extension/subprocess/subprocess.go" && relative != "internal/runtime/exec_windows.go" {
					t.Errorf("unclassified command boundary in %s", relative)
				}
			}
			if strings.HasSuffix(importPath, "/internal/kernel/model") && lifecycleSelector(selector.Sel.Name) &&
				!strings.HasPrefix(relative, "internal/kernel/") && !strings.HasPrefix(relative, "internal/effects/") && !strings.HasPrefix(relative, "internal/plant/") &&
				!strings.HasPrefix(relative, "control/") && !strings.HasPrefix(relative, "flow/") && !strings.HasPrefix(relative, "extension/") {
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

func TestEveryControllableRuntimeEventHasAnExecutableStateReducer(t *testing.T) {
	// control-law: registry-entry-cannot-exist-without-runtime-effect-reduction
	path := filepath.Join(sourceRoot(t), "internal", "effects", "state_reducer.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	covered := map[catalog.TransitionID]bool{}
	registry := testprogram.StandardRegistry()
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "applyStateTransition" {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			clause, ok := node.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expression := range clause.List {
				literal, ok := expression.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				value, unquoteErr := strconv.Unquote(literal.Value)
				if unquoteErr != nil {
					t.Fatal(unquoteErr)
				}
				id := catalog.TransitionID(value)
				if transition, exists := registry.Lookup(id); exists && transition.Controllable() {
					covered[id] = true
				}
			}
			return true
		})
	}
	for _, transition := range registry.All() {
		if transition.Controllable() && !covered[transition.ID] {
			t.Errorf("controllable transition %s has no applyStateTransition reducer case", transition.ID)
		}
	}
}

func TestPackageImportsPreserveControlProgramDependencyDirection(t *testing.T) {
	// control-law: kernel-mechanism-cannot-depend-on-standard-flow-or-product-surfaces
	root := sourceRoot(t)
	forbiddenKernel := []string{"/flow/standard", "/distribution", "/sdk", "/cmd/boatstack-helper"}
	forbiddenFlow := []string{"/distribution", "/sdk", "/cmd/boatstack-helper", "/internal/surfaces"}
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
		kernelOwned := relative == "kernel.go" || relative == "program_effects.go" || relative == "program_observer.go" || strings.HasPrefix(relative, "internal/kernel/")
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
	return relative == "kernel.go" || relative == "program_effects.go" || relative == "program_observer.go" || strings.HasPrefix(relative, "cmd/boatstack-helper/") ||
		strings.HasPrefix(relative, "control/") || strings.HasPrefix(relative, "core/") ||
		strings.HasPrefix(relative, "flow/") || strings.HasPrefix(relative, "distribution/") || strings.HasPrefix(relative, "extension/") ||
		strings.HasPrefix(relative, "internal/kernel/") || strings.HasPrefix(relative, "internal/plant/") ||
		strings.HasPrefix(relative, "internal/effects/") || strings.HasPrefix(relative, "internal/surfaces/") ||
		strings.HasPrefix(relative, "internal/buildinfo/") || strings.HasPrefix(relative, "internal/runtime/") ||
		strings.HasPrefix(relative, "internal/retromine/") || strings.HasPrefix(relative, "internal/testprogram/") || strings.HasPrefix(relative, "sdk/") ||
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
