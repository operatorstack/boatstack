package boatstack

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// control-law: every-managed-path-has-a-declared-owner
//
// The state-ownership map (statemap.go) is the single declaration of every
// tree Boatstack manages: class, partition, owning verbs, guard protection.
// These tests hold the declaration, the WorkspaceContext resolvers, the
// guard's path classifiers, the exported ownership doc, and the package's
// hand-joined path literals to each other — so the next state-partitioning
// divergence fails here instead of surfacing as a live defect.

func statemapContext(t *testing.T) WorkspaceContext {
	t.Helper()
	repo := safetyTestRepo(t)
	w, err := ResolveWorkspaceContext(repo)
	if err != nil {
		t.Fatalf("resolve workspace context: %v", err)
	}
	return w
}

func entrySample(t *testing.T, w WorkspaceContext, entry StateEntry) string {
	t.Helper()
	sample, err := entry.Sample(w)
	if err != nil {
		t.Fatalf("entry %s sample: %v", entry.Name, err)
	}
	if sample == "" {
		t.Fatalf("entry %s resolves an empty sample", entry.Name)
	}
	return filepath.ToSlash(sample)
}

// Positive: every WorkspaceContext resolver's output is owned by exactly one
// registry entry of the expected class — the resolvers and the declaration
// cannot drift apart.
func TestEveryWorkspaceResolverIsDeclared(t *testing.T) {
	w := statemapContext(t)
	registry := StateRegistry()

	resolve := func(f func() (string, error)) string {
		path, err := f()
		if err != nil {
			t.Fatal(err)
		}
		return filepath.ToSlash(path)
	}
	resolverOutputs := map[string]struct {
		path      string
		wantClass PathClass
	}{
		"GeneratedRoot":     {filepath.ToSlash(w.GeneratedRoot()), ClassCommittedGenerated},
		"ProjectConfigPath": {filepath.ToSlash(w.ProjectConfigPath()), ClassCommittedGenerated},
		"SourceConfigPath":  {filepath.ToSlash(w.SourceConfigPath()), ClassCommittedGenerated},
		"DeliveryDir":       {resolve(w.DeliveryDir), ClassRuntimeWorktree},
		"OperationDir":      {resolve(w.OperationDir), ClassRuntimeWorktree},
		"FlowDir":           {resolve(w.FlowDir), ClassRuntimeWorktree},
		"GuardDir":          {resolve(w.GuardDir), ClassRuntimeWorktree},
		"InsightDir":        {resolve(w.InsightDir), ClassCommittedInsight},
		"RuntimeDir": {resolve(func() (string, error) {
			return w.RuntimeDir("v0.0.0", "0000000")
		}), ClassRuntimeShared},
	}

	for name, want := range resolverOutputs {
		owners := 0
		var ownerClass PathClass
		for _, entry := range registry {
			sample := entrySample(t, w, entry)
			// A resolver is "declared" when some entry's sample lives at or under
			// its output (the entry is the concrete artifact inside the tree).
			if sample == want.path || strings.HasPrefix(sample, want.path+"/") {
				if entry.Class != ownerClass {
					owners++
					ownerClass = entry.Class
				}
			}
		}
		if owners == 0 {
			t.Errorf("resolver %s (%s) has no declared owner in the state registry", name, want.path)
			continue
		}
		if ownerClass != want.wantClass && name != "GeneratedRoot" {
			t.Errorf("resolver %s owned by class %s, want %s", name, ownerClass, want.wantClass)
		}
	}
	// GeneratedRoot hosts multiple classes by design (generated bundle, planning,
	// checkout runtime) — assert it is covered, which the loop above did.
}

// Negative: the declaration is well-formed — unique names, non-empty owners,
// and no two entries of different classes resolve the same sample path.
func TestStateRegistryIsWellFormed(t *testing.T) {
	w := statemapContext(t)
	seenNames := map[string]bool{}
	seenSamples := map[string]PathClass{}
	for _, entry := range StateRegistry() {
		if seenNames[entry.Name] {
			t.Errorf("duplicate entry name %s", entry.Name)
		}
		seenNames[entry.Name] = true
		if len(entry.OwnerVerbs) == 0 {
			t.Errorf("entry %s declares no owner", entry.Name)
		}
		if entry.Partition == "" || entry.Class == "" {
			t.Errorf("entry %s missing partition or class: %+v", entry.Name, entry)
		}
		sample := entrySample(t, w, entry)
		if class, dup := seenSamples[sample]; dup && class != entry.Class {
			t.Errorf("sample %s claimed by two classes: %s and %s", sample, class, entry.Class)
		}
		seenSamples[sample] = entry.Class
	}
}

// Relation: the guard's path classifiers agree with the declaration exactly.
// deliveryStatePathPattern denies precisely the GuardProtected trees, and the
// planning first-write latch covers precisely the features/-resident
// committed-planning trees.
func TestGuardClassifiersMatchDeclaredOwnership(t *testing.T) {
	w := statemapContext(t)
	repoRoot := filepath.ToSlash(w.RepoRoot)
	for _, entry := range StateRegistry() {
		sample := entrySample(t, w, entry)
		gotProtected := deliveryStatePathPattern.MatchString(sample) || insightArtifactPathPattern.MatchString(sample)
		if gotProtected != entry.GuardProtected {
			t.Errorf("guard pattern(%s)=%t but declaration says GuardProtected=%t (sample %s)", entry.Name, gotProtected, entry.GuardProtected, sample)
		}
		if entry.Class == ClassCommittedPlanning {
			relative := strings.TrimPrefix(strings.TrimPrefix(sample, repoRoot), "/")
			underFeatures := strings.HasPrefix(relative, ".product-loop/features/")
			if featureScopedPath(relative) != underFeatures {
				t.Errorf("first-write latch scope disagrees for %s (%s)", entry.Name, relative)
			}
		}
	}
}

// Bypass: no NEW file may hand-join ".product-loop" paths without consciously
// extending this allowlist — the frozen inventory of declaring files. Growth
// pressure should flow toward WorkspaceContext/statemap, not new literals.
func TestProductLoopLiteralsStayInDeclaredFiles(t *testing.T) {
	allowed := map[string]string{
		"activation.go": "controller-syntax", "delivery.go": "controller-syntax", "export.go": "controller-bundle",
		"hooks.go":    "embedded-installation",
		"launcher.go": "embedded-installation",
		"init.go":     "embedded-installation", "installation_repair.go": "embedded-installation", "mutation_undo.go": "controller-syntax",
		"paths.go": "canonical-owner", "planning.go": "product-diff-syntax", "pr.go": "product-diff-syntax",
		"recovery.go": "product-diff-syntax", "runtime_cache.go": "embedded-installation", "safety.go": "policy-syntax",
		"update.go": "embedded-installation", "update_publication.go": "embedded-installation",
		"workspace.go": "repository-workspace",
		// denial.go names .product-loop/features/ only in user-facing denial
		// copy (the owned-channel guidance), never as a joined path.
		"denial.go": "user-guidance",
	}
	validClass := map[string]bool{"canonical-owner": true, "controller-bundle": true, "controller-syntax": true, "embedded-installation": true, "policy-syntax": true, "product-diff-syntax": true, "repository-workspace": true, "user-guidance": true}

	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	offenders := map[string]bool{}
	scanned := 0
	for _, item := range entries {
		name := item.Name()
		if item.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		scanned++
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if strings.Contains(value, ".product-loop") && allowed[name] == "" {
				offenders[name] = true
			}
			return true
		})
	}
	if scanned < 20 {
		t.Fatalf("scanned only %d files; the literal guarantee would be vacuous", scanned)
	}
	if len(offenders) > 0 {
		names := make([]string, 0, len(offenders))
		for name := range offenders {
			names = append(names, name)
		}
		sort.Strings(names)
		t.Fatalf("new hand-joined .product-loop literals outside the declared files: %v — route the path through WorkspaceContext/statemap or consciously extend the allowlist", names)
	}
	// Reverse check: a file on the allowlist that no longer carries a literal is
	// stale — shrink the list so the freeze stays honest.
	for name, class := range allowed {
		if !validClass[class] {
			t.Errorf("allowlist entry %s has unknown ownership class %q", name, class)
		}
		content, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("allowlisted file %s unreadable: %v", name, err)
		}
		if !strings.Contains(string(content), ".product-loop") {
			t.Errorf("allowlist entry %s is stale — it no longer names .product-loop", name)
		}
	}
}

// Failure-state / doc drift: the exported ownership table mirrors the registry
// exactly, and the stale pre-isolation ledger path is gone from the doc.
func TestOwnershipDocMirrorsRegistry(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("references", "artifacts.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(content)
	if strings.Contains(doc, "Git-common `boatstack/operations/v1`") {
		t.Fatal("artifacts.md still documents the orphaned Git-common operations/v1 ledger as current")
	}

	documented := map[string]bool{}
	inTable := false
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "## State ownership") {
			inTable = true
			continue
		}
		if inTable && strings.HasPrefix(line, "## ") {
			break
		}
		if !inTable || !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| ---") || strings.HasPrefix(line, "| Name") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) > 1 {
			documented[strings.TrimSpace(cells[1])] = true
		}
	}

	registry := map[string]bool{}
	for _, entry := range StateRegistry() {
		registry[entry.Name] = true
		if !documented[entry.Name] {
			t.Errorf("registry entry %s missing from the artifacts.md ownership table", entry.Name)
		}
	}
	for name := range documented {
		if !registry[name] {
			t.Errorf("artifacts.md documents %s which is not in the registry", name)
		}
	}
}
