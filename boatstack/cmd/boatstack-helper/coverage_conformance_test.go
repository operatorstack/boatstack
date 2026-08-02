package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

// control-law: registry-covers-real-delivery-machine
//
// The deliverycontrol registry is the authoritative projection of the real
// delivery state machine: the flow commands resolve the prescribed CLI command
// through it (Transition(id).CLIVerb). For that projection to be trustworthy, the
// registry must cover EXACTLY the real delivery machine — every registry CLIVerb
// must name a real dispatch verb, and every real delivery-mutation/observe
// dispatch verb must have a registry row. Nothing asserted this before, so a new
// delivery CLI verb could ship green with no registry row, invisible to the
// liveness/deadlock guarantee. These tests close that gap in both directions.
//
// The real dispatch inventory is read from the actual `run()` switch in main.go
// by parsing its AST, so it cannot drift from what the binary really accepts.
// Every dispatch verb must be classified: either it is a delivery-machine verb
// (a registry CLIVerb) or it is explicitly declared out of the delivery machine
// in nonDeliveryVerbs below. A new verb that is neither fails the suite — the
// author must consciously register it or declare it non-delivery (the Bypass
// guard). This list is behavior describing, not behavior defining: it names the
// dispatch verbs that are not transitions of the delivery state machine.
var nonDeliveryVerbs = map[string]bool{
	// Update / release / distribution lifecycle (not the per-feature delivery machine).
	"init":              true,
	"update":            true,
	"check-update":      true,
	"prepare-update-pr": true,
	"publish-update-pr": true,
	"release-classify":  true,
	"next-patch":        true,
	"export":            true,
	"migrate-config":    true,
	"hydrate-runtime":   true,
	"version":           true,
	// Planning phase, before a plan is activated into a delivery.
	"check-source-plan": true,
	"check-plan":        true,
	"planning-write":    true,
	"record-approval":   true,
	"record-autonomy":   true,
	// Read-only status / diagnostics (observe helpers, not modeled transitions).
	"repair-status":    true,
	"operation-status": true,
	"mutation-status":  true,
	"run-preflight":    true,
	"check-safety":     true,
	"doctor":           true,
	"diagnose-hook":    true,
	"render-denial":    true,
	"workspace-status": true,
	// Evidence / capability substrate (a separate tenant, not the delivery graph).
	"record-pr-visual-evidence":    true,
	"capture-evidence":             true,
	"provision-capability":         true,
	"capability-register":          true,
	"record-pr-visual-publication": true,
	"attach-evidence":              true,
	// PR construction / verification helpers reached around the ship gate.
	"check-pr": true,
	// Detached Supervision lifecycle (control-plane ownership, not delivery moves).
	"attach":          true,
	"detach":          true,
	"detached-status": true,
	"context":         true,
	"activate":        true,
	"deactivate":      true,
	// Safety hooks and workspace management (guard/scaffold, not delivery moves).
	"safety-hook":           true,
	"ambient-safety-hook":   true,
	"bootstrap-safety-hook": true,
	"workspace-cut":         true,
	"workspace-cleanup":     true,
	"workspace-reap":        true,
	"workspace-sync":        true,
	// Flow layer itself is read-only navigation over the machine, not a transition.
	"flow": true,
	// Insight capture is a detached control-plane tenant. Its append-only events
	// observe delivery evidence but never transition the delivery machine.
	"insight": true,
	// Retro derivation reads operator-supplied transcripts and proposes typed
	// promotions; it mutates nothing, so it registers no delivery transition.
	// control-law: retro-proposes-never-enforces
	"retro": true,
}

// dispatchVerbs parses main.go and returns the set of command verbs the run()
// switch actually dispatches. It fails loudly rather than returning an empty set,
// so the coverage guarantee can never silently pass by finding nothing.
func dispatchVerbs(t *testing.T) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	verbs := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "run" {
			return true
		}
		ast.Inspect(fn.Body, func(inner ast.Node) bool {
			sw, ok := inner.(*ast.SwitchStmt)
			if !ok || !switchesOnArgs(sw.Tag) {
				return true
			}
			for _, stmt := range sw.Body.List {
				clause, ok := stmt.(*ast.CaseClause)
				if !ok {
					continue
				}
				for _, expr := range clause.List {
					if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						verbs[mustUnquote(t, lit.Value)] = true
					}
				}
			}
			return false
		})
		return false
	})
	if len(verbs) < 10 {
		t.Fatalf("dispatch switch parse found only %d verbs; expected the full run() command set — the coverage guard would be vacuous", len(verbs))
	}
	return verbs
}

// switchesOnArgs reports whether a switch tag is an index into os.Args (the
// command dispatch), e.g. `os.Args[1]`.
func switchesOnArgs(tag ast.Expr) bool {
	index, ok := tag.(*ast.IndexExpr)
	if !ok {
		return false
	}
	sel, ok := index.X.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Args"
}

func mustUnquote(t *testing.T, quoted string) string {
	t.Helper()
	if len(quoted) < 2 {
		t.Fatalf("malformed string literal %q in dispatch switch", quoted)
	}
	return quoted[1 : len(quoted)-1]
}

func registryVerbs() map[string]bool {
	verbs := map[string]bool{}
	for _, tr := range deliverycontrol.Transitions() {
		if tr.CLIVerb != "" {
			verbs[tr.CLIVerb] = true
		}
	}
	return verbs
}

// Positive: every CLIVerb the registry declares names a real dispatch verb, so
// the prescribed command a resolver emits through Transition(id).CLIVerb is
// always a command the binary actually accepts.
func TestRegistryCLIVerbsAreRealDispatchVerbs(t *testing.T) {
	dispatch := dispatchVerbs(t)
	for verb := range registryVerbs() {
		if !dispatch[verb] {
			t.Errorf("registry declares CLIVerb %q that main.go does not dispatch (prescribed command would be unrunnable)", verb)
		}
	}
}

// Bypass / Negative: every dispatch verb must be classified — a delivery-machine
// verb (registry CLIVerb) or explicitly non-delivery. A new delivery CLI verb
// added to the dispatch switch without a registry row (or a conscious
// non-delivery declaration) fails here; it cannot ship invisibly to the machine.
func TestEveryDispatchVerbIsClassified(t *testing.T) {
	dispatch := dispatchVerbs(t)
	registry := registryVerbs()
	for verb := range dispatch {
		if registry[verb] {
			continue
		}
		if nonDeliveryVerbs[verb] {
			continue
		}
		t.Errorf("dispatch verb %q is neither a registry delivery transition nor declared in nonDeliveryVerbs; register it or classify it before shipping", verb)
	}
}

// Relation: the delivery-machine dispatch verbs (all dispatch verbs minus the
// declared non-delivery ones) equal the registry CLIVerb set exactly — the two
// inventories agree with no orphan on either side.
func TestDeliveryDispatchVerbsEqualRegistry(t *testing.T) {
	dispatch := dispatchVerbs(t)
	registry := registryVerbs()

	deliveryDispatch := map[string]bool{}
	for verb := range dispatch {
		if !nonDeliveryVerbs[verb] {
			deliveryDispatch[verb] = true
		}
	}
	if diff := symmetricDiff(deliveryDispatch, registry); len(diff) != 0 {
		sort.Strings(diff)
		t.Errorf("delivery dispatch verbs and registry CLIVerbs disagree: %v", diff)
	}

	// A non-delivery declaration must name a verb that is actually dispatched;
	// a stale entry (verb renamed/removed) is drift and must be cleaned up.
	for verb := range nonDeliveryVerbs {
		if !dispatch[verb] {
			t.Errorf("nonDeliveryVerbs names %q which main.go no longer dispatches (stale allowlist entry)", verb)
		}
	}
}

func symmetricDiff(a, b map[string]bool) []string {
	var diff []string
	for k := range a {
		if !b[k] {
			diff = append(diff, "only-in-dispatch:"+k)
		}
	}
	for k := range b {
		if !a[k] {
			diff = append(diff, "only-in-registry:"+k)
		}
	}
	return diff
}
