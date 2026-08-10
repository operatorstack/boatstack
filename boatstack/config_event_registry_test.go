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

// configEventClasses is the public event-completeness registry. Every call to a
// configuration reader, renderer, writer, resolver, or admission boundary is
// inventoried below by the AST digest. Adding an unreviewed call changes the
// digest and fails CI.
var configEventClasses = map[string]string{
	"LoadConfig":                   "reader",
	"SourceConfigPath":             "resolver",
	"ProjectConfigPath":            "resolver",
	"BuildExportBundle":            "renderer",
	"WriteExport":                  "writer",
	"WriteExportForRepair":         "writer",
	"writeExport":                  "writer",
	"MigrateConfigBytes":           "writer",
	"ResolveConfigurationTopology": "resolver",
	"RequireManagedConfiguration":  "admission",
	"ConfigRebind":                 "writer",
}

func calledName(call *ast.CallExpr) string {
	switch function := call.Fun.(type) {
	case *ast.Ident:
		return function.Name
	case *ast.SelectorExpr:
		return function.Sel.Name
	default:
		return ""
	}
}

func TestConfigurationEventRegistryIsComplete(t *testing.T) {
	entries := []string{}
	set := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(set, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			counts := map[string]int{}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := calledName(call)
				class, tracked := configEventClasses[name]
				if !tracked {
					return true
				}
				counts[name]++
				entries = append(entries, filepath.Base(path)+":"+function.Name.Name+":"+name+":"+class+":"+strconv.Itoa(counts[name]))
				return true
			})
		}
	}
	sort.Strings(entries)
	digest := SHA256Bytes([]byte(strings.Join(entries, "\n")))
	const expected = "dfcbdc50fdfbda0d505ce7b04f55f6289622adad74a8fd01fc5fb13e6a00979b"
	if digest != expected {
		_ = os.WriteFile(filepath.Join(t.TempDir(), "config-events.txt"), []byte(strings.Join(entries, "\n")+"\n"), 0o644)
		t.Fatalf("configuration event registry changed: got %s; classify the new or removed site and update the reviewed digest", digest)
	}
}
