package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"sort"
	"strconv"
)

const kernelPackage = "github.com/operatorstack/boatstack/boatstack/kernel"

var runtimeSymbols = map[string]bool{
	"NewRuntime": true,
	"Runtime":    true,
	"Store":      true,
	"Domain":     true,
	"Operator":   true,
	"Receipt":    true,
}

type metadata struct {
	Path             string   `json:"path"`
	Imports          []string `json:"imports"`
	RuntimeConsumers []string `json:"runtime_consumers"`
	Vocabulary       []string `json:"vocabulary"`
}

func main() {
	results := make([]metadata, 0, len(os.Args)-1)
	for _, filename := range os.Args[1:] {
		if filename == "--" {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), filename, nil, parser.AllErrors|parser.ParseComments)
		if err != nil {
			fatalf("parse %s: %v", filename, err)
		}
		item := metadata{Path: filename, Imports: []string{}, RuntimeConsumers: []string{}, Vocabulary: []string{}}
		kernelAliases := map[string]bool{}
		for _, spec := range parsed.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				fatalf("decode import in %s: %v", filename, err)
			}
			item.Imports = append(item.Imports, importPath)
			if importPath != kernelPackage || (spec.Name != nil && spec.Name.Name == "_") {
				continue
			}
			alias := path.Base(importPath)
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			kernelAliases[alias] = true
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.ImportSpec:
				return false
			case *ast.SelectorExpr:
				alias, ok := value.X.(*ast.Ident)
				if ok && kernelAliases[alias.Name] && runtimeSymbols[value.Sel.Name] {
					item.RuntimeConsumers = append(item.RuntimeConsumers, value.Sel.Name)
				}
			case *ast.Ident:
				item.Vocabulary = append(item.Vocabulary, value.Name)
				if kernelAliases["."] && runtimeSymbols[value.Name] {
					item.RuntimeConsumers = append(item.RuntimeConsumers, value.Name)
				}
			case *ast.BasicLit:
				if value.Kind == token.STRING {
					decoded, err := strconv.Unquote(value.Value)
					if err == nil {
						item.Vocabulary = append(item.Vocabulary, decoded)
					}
				}
			case *ast.Comment:
				item.Vocabulary = append(item.Vocabulary, value.Text)
			}
			return true
		})
		sort.Strings(item.Imports)
		sort.Strings(item.RuntimeConsumers)
		sort.Strings(item.Vocabulary)
		results = append(results, item)
	}
	if err := json.NewEncoder(os.Stdout).Encode(results); err != nil {
		fatalf("encode metadata: %v", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
