package boatstack

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// lifecycleFieldClasses is the reviewed projection of every durable delivery
// dimension. Adding a field without deciding whether it controls authority is
// a CI failure, preventing a slice-only model from silently becoming stale.
var lifecycleFieldClasses = map[string]string{
	"SchemaVersion":       "identity",
	"Feature":             "identity",
	"PlanLockHash":        "authority",
	"PreviousPlanLocks":   "history",
	"ActiveIndex":         "control",
	"Slices":              "control",
	"Mode":                "control",
	"ResumeStage":         "control",
	"ActiveObservationID": "authority",
	"RepairCounters":      "control",
	"RepairAttempt":       "derived",
	"SupersededReceipts":  "evidence",
	"ParentDelivery":      "lineage",
	"Goal":                "control",
}

// lifecycleEventClasses inventories the code sites that read, resolve, render,
// admit, or mutate lifecycle authority. The reviewed digest below changes when
// a new entry path bypasses the canonical composite resolver.
var lifecycleEventClasses = map[string]string{
	"LoadDeliveryState":         "reader",
	"CurrentDeliveryState":      "verified-reader",
	"saveDeliveryState":         "writer",
	"ResolveLifecycleSnapshot":  "resolver",
	"ResolveNext":               "status",
	"nextForDelivery":           "status",
	"NextControl":               "renderer",
	"nextControlFromStatus":     "renderer",
	"ResolvePlanningBootstrap":  "renderer",
	"controlledPhaseTransition": "admission",
	"HookDecision":              "host-admission",
	"WritePlanningArtifact":     "writer",
	"ActivatePlan":              "writer",
	"RecordApproval":            "writer",
	"RecordChangeObservation":   "writer",
	"RecordDeliveryGate":        "writer",
}

func TestEveryDeliveryStateFieldHasReviewedLifecycleSemantics(t *testing.T) {
	set := token.NewFileSet()
	parsed, err := parser.ParseFile(set, "delivery.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]bool{}
	ast.Inspect(parsed, func(node ast.Node) bool {
		typeSpec, ok := node.(*ast.TypeSpec)
		if !ok || typeSpec.Name.Name != "DeliveryState" {
			return true
		}
		structure, ok := typeSpec.Type.(*ast.StructType)
		if !ok {
			t.Fatal("DeliveryState is not a struct")
		}
		for _, field := range structure.Fields.List {
			for _, name := range field.Names {
				fields[name.Name] = true
			}
		}
		return false
	})
	for field := range fields {
		if lifecycleFieldClasses[field] == "" {
			t.Errorf("DeliveryState.%s has no reviewed lifecycle classification", field)
		}
	}
	for field := range lifecycleFieldClasses {
		if !fields[field] {
			t.Errorf("lifecycle field registry contains removed DeliveryState.%s", field)
		}
	}
}

func TestLifecycleEventRegistryIsComplete(t *testing.T) {
	entries := []string{}
	set := token.NewFileSet()
	files := []string{}
	if err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	}); err != nil {
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
				class, tracked := lifecycleEventClasses[name]
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
	const expected = "98c48b24b87e5c95fcbd9d64e93ce05b42a484e221df01b57e5f7bf3d7be4ec4"
	if digest != expected {
		t.Fatalf("lifecycle event registry changed: got %s; classify the new or removed site and update the reviewed digest\n%s", digest, strings.Join(entries, "\n"))
	}
}
