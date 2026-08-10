package boatstack

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// engagementSurfaceClasses inventories every production call that can decide,
// persist, expose, or route engagement. The reviewed digest makes an added,
// removed, or bypassing call site a deliberate contract change in CI.
var engagementSurfaceClasses = map[string]string{
	"ResolveEngagement":                "resolver",
	"syncEngagementLease":              "lease-writer",
	"clearEngagementLease":             "lease-writer",
	"HookDecision":                     "policy-entry",
	"EngagementProbeDecision":          "host-entry",
	"activeManagedOperationScope":      "operation-entry",
	"initializeDeliveryState":          "activation-writer",
	"MarkDeliveryPublished":            "release-writer",
	"DiscardDelivery":                  "release-writer",
	"guardShellScript":                 "shell-renderer",
	"guardPowerShellScript":            "shell-renderer",
	"desiredHostHookForEvent":          "host-renderer",
	"engagementProbeCommand":           "host-renderer",
	"engagementProbePowerShellCommand": "host-renderer",
}

func engagementCalledName(call *ast.CallExpr) string {
	switch function := call.Fun.(type) {
	case *ast.Ident:
		return function.Name
	case *ast.SelectorExpr:
		return function.Sel.Name
	default:
		return ""
	}
}

func TestEngagementSurfaceRegistryIsComplete(t *testing.T) {
	entries := []string{}
	set := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	commandFiles, err := filepath.Glob(filepath.Join("cmd", "boatstack-helper", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, commandFiles...)
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
				name := engagementCalledName(call)
				class, tracked := engagementSurfaceClasses[name]
				if !tracked {
					return true
				}
				counts[name]++
				entries = append(entries, filepath.ToSlash(path)+":"+function.Name.Name+":"+name+":"+class+":"+strconv.Itoa(counts[name]))
				return true
			})
		}
	}
	sort.Strings(entries)
	digest := SHA256Bytes([]byte(strings.Join(entries, "\n")))
	const expected = "965433e25f06d745b46c752e553beeec82ebe24f508fcf3422da6d273291c3b4"
	if digest != expected {
		t.Fatalf("engagement surface registry changed: got %s; classify the new or removed site and update the reviewed digest\n%s", digest, strings.Join(entries, "\n"))
	}
}
