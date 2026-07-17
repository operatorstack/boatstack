package boatstack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testConfig() ProjectConfig {
	return ProjectConfig{
		SchemaVersion: 1,
		Project: Project{
			Name: "fixture", Commands: map[string]string{"test": "go test ./..."},
		},
		Workflow: Workflow{HumanPlanApproval: true, IndependentReviewForHighRisk: true, AllowPassWithGaps: true},
		Adapters: []string{"cursor", "claude", "codex", "github"},
		Integrations: map[string]IntegrationState{
			"gstack":   {Requested: false, Version: GStackRef},
			"spec-kit": {Requested: false, Version: SpecKitVersion},
		},
	}
}

func TestExportAndDriftCheck(t *testing.T) {
	repo := t.TempDir()
	config := testConfig()
	raw, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := BuildExportBundle(".boatstack-project.json", config, raw, "boatstack")
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteExport(repo, bundle.Files); err != nil {
		t.Fatal(err)
	}
	if err := CheckExport(repo, bundle.Files); err != nil {
		t.Fatal(err)
	}
	if err := InstallHostHooks(repo, config.Adapters); err != nil {
		t.Fatal(err)
	}
	if err := CheckHostHooks(repo, config.Adapters); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		".cursor/commands/boatstack-update.md",
		".cursor/commands/plan-gate.md",
		".cursor/commands/review.md",
		".claude/skills/boatstack/SKILL.md",
		".agents/skills/boatstack/SKILL.md",
		".product-loop/.gitignore",
		".product-loop/templates/plan.md",
		".product-loop/templates/approval.md",
		".product-loop/hooks/guard.sh",
		".product-loop/hooks/guard.ps1",
		".product-loop/hooks/cursor.fragment.json",
	} {
		if !fileExists(filepath.Join(repo, filepath.FromSlash(path))) {
			t.Fatalf("expected generated file %s", path)
		}
	}
	if _, exists := bundle.Files[".product-loop/tools/approve_plan.py"]; exists {
		t.Fatal("public export must not contain Python runtime tools")
	}
	if _, exists := bundle.Files[".product-loop/templates/plan.json"]; exists {
		t.Fatal("Markdown-native planning must not export plan.json")
	}
	autoPlan := string(bundle.Files[".cursor/commands/auto-plan.md"])
	planGate := string(bundle.Files[".cursor/commands/plan-gate.md"])
	build := string(bundle.Files[".cursor/commands/build.md"])
	responseOutcomes := map[string][]string{
		"auto-plan":        {"Plan ready", "I need your input"},
		"plan-gate":        {"Ready for your approval", "Approved — ready to build"},
		"build":            {"Build complete", "Build needs a decision"},
		"test-gate":        {"Tests passed", "Testing found a problem"},
		"review-gate":      {"Review passed", "Changes required"},
		"review":           {"Review passed", "Changes required"},
		"ship-gate":        {"PR ready", "PR opened"},
		"ship":             {"PR ready", "PR opened"},
		"retro":            {"Improvement proposed"},
		"boatstack-update": {"Boatstack is current", "Update postponed", "Boatstack update ready", "Update PR opened", "Update needs attention"},
	}
	for operation, outcomes := range responseOutcomes {
		command := string(bundle.Files[".cursor/commands/"+operation+".md"])
		for _, expected := range []string{"User-facing response contract", "### Next step", "Technical details", "do not expose them in the primary response"} {
			if !strings.Contains(command, expected) {
				t.Fatalf("%s adapter is missing response-DX rule %q", operation, expected)
			}
		}
		for _, outcome := range outcomes {
			if !strings.Contains(command, outcome) {
				t.Fatalf("%s adapter is missing outcome %q", operation, outcome)
			}
		}
	}
	if !strings.Contains(autoPlan, "Markdown-only") || !strings.Contains(autoPlan, "never silently choose a default") || !strings.Contains(autoPlan, "planning-write") || !strings.Contains(autoPlan, "PROPOSED") {
		t.Fatal("auto-plan adapter does not enforce the Markdown and question boundaries")
	}
	if !strings.Contains(planGate, "approval.md") || !strings.Contains(planGate, "Remain in Plan mode") || !strings.Contains(planGate, "record-approval") {
		t.Fatal("plan-gate adapter does not keep approval in Plan mode")
	}
	for _, expected := range []string{"normal user action is simply approve", "authenticated GitHub login", "never infer it from a filesystem username"} {
		if !strings.Contains(planGate, expected) {
			t.Fatalf("plan-gate adapter is missing approval identity rule %q", expected)
		}
	}
	if !strings.Contains(build, "activate-plan") || !strings.Contains(build, "READY_FOR_BUILD") || !strings.Contains(build, "without activating") || strings.Contains(build, "compile-plan") {
		t.Fatal("build adapter must activate the Markdown plan exactly once")
	}
	ship := string(bundle.Files[".cursor/commands/ship-gate.md"])
	for _, expected := range []string{"separate repair PR", "Never edit unrelated code", "exact title", "Reply open PR", "Reply update PR", "preview fingerprint"} {
		if !strings.Contains(ship, expected) {
			t.Fatalf("ship adapter is missing reviewer-ready PR rule %q", expected)
		}
	}
	if _, exists := bundle.Files[".cursor/commands/pr-brief.md"]; exists {
		t.Fatal("PR brief must remain natural-language behavior, not a public command")
	}
	update := string(bundle.Files[".cursor/commands/boatstack-update.md"])
	for _, expected := range []string{"check-update", "chore/update-boatstack-v", "BOATSTACK_MODE=update", "Reply open update PR", "Never merge"} {
		if !strings.Contains(update, expected) {
			t.Fatalf("update adapter is missing %q", expected)
		}
	}
	for _, operation := range []string{"auto-plan", "plan-gate", "build", "test-gate", "review-gate", "review", "retro"} {
		if strings.Contains(string(bundle.Files[".cursor/commands/"+operation+".md"]), "check-update") {
			t.Fatalf("%s must not check for Boatstack releases", operation)
		}
	}
	if strings.Contains(ship, "check-update") {
		t.Fatal("ship preview must not initiate a release check")
	}
	for _, expected := range []string{"UPDATE_AVAILABLE", "collapsed update notice", "Review the PR"} {
		if !strings.Contains(ship, expected) {
			t.Fatalf("ship adapter is missing post-publication update behavior %q", expected)
		}
	}
	cursorRule := string(bundle.Files[".cursor/rules/boatstack.mdc"])
	for _, expected := range []string{"naturally asks Boatstack", "evidence-limited ad-hoc PR brief", "not a /pr-brief command", "NOT_VERIFIED"} {
		if !strings.Contains(cursorRule, expected) {
			t.Fatalf("Cursor rule is missing ad-hoc PR behavior %q", expected)
		}
	}
	prTemplate := string(bundle.Files[".github/PULL_REQUEST_TEMPLATE/boatstack.md"])
	for _, expected := range []string{"## Why this change", "## What changed", "## Review order", "## Evidence", "## Operational safety", "## Known gaps and risks", "## Rollout and rollback", "<summary>Boatstack provenance</summary>"} {
		if !strings.Contains(prTemplate, expected) {
			t.Fatalf("generated PR template is missing %q", expected)
		}
	}
	if !strings.Contains(ship, "separate repair PR") || !strings.Contains(ship, "Never edit unrelated code") {
		t.Fatal("ship adapter permits unrelated scope expansion")
	}
	lock := string(bundle.Files[".product-loop/generated.lock.json"])
	if !strings.Contains(lock, `"source_commit"`) || !strings.Contains(lock, `"integrations"`) {
		t.Fatal("generated lock must record runtime provenance and integrations")
	}
	workflow := string(bundle.Files[".product-loop/workflow.md"])
	for _, expected := range []string{"## User-facing response contract", "Exactly one primary action", "gh api user --jq .login", "Never infer the approver"} {
		if !strings.Contains(workflow, expected) {
			t.Fatalf("canonical workflow is missing response contract %q", expected)
		}
	}
	for _, expected := range []string{"irreversible", "operator-only", "fix-forward", "least-privilege"} {
		if !strings.Contains(strings.ToLower(workflow), strings.ToLower(expected)) {
			t.Fatalf("canonical workflow is missing safety boundary %q", expected)
		}
	}
	for _, path := range []string{".agents/skills/boatstack/SKILL.md", ".claude/skills/boatstack/SKILL.md"} {
		adapter := string(bundle.Files[path])
		for _, expected := range []string{"User-facing response contract", "exactly one Next step", "Normal approval is simply approve", "filesystem username", "Never create or advertise a /pr-brief command", "Reply open PR", "Reply update PR", "boatstack-update", "open update PR"} {
			if !strings.Contains(adapter, expected) {
				t.Fatalf("%s is missing response-DX rule %q", path, expected)
			}
		}
	}
}

func TestExportRefusesUserOwnedCollision(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, ".cursor", "rules", "boatstack.mdc")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("user owned\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := testConfig()
	raw, _ := MarshalJSON(config)
	bundle, err := BuildExportBundle("config.json", config, raw, "boatstack")
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteExport(repo, bundle.Files); err == nil || !strings.Contains(err.Error(), "user-owned") {
		t.Fatalf("expected user-owned collision, got %v", err)
	}
	value, _ := os.ReadFile(path)
	if string(value) != "user owned\n" {
		t.Fatal("collision handling modified the user-owned file")
	}
}

func TestExportAdoptsLegacyGeneratedFiles(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, ".cursor", "rules", "boatstack.mdc")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := "<!-- Generated by product-engineering-loop exporter. Do not edit; change canonical source or project.json. -->\n"
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	config := testConfig()
	raw, _ := MarshalJSON(config)
	bundle, err := BuildExportBundle("config.json", config, raw, "boatstack")
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteExport(repo, bundle.Files); err != nil {
		t.Fatalf("legacy generated file should be safely replaceable: %v", err)
	}
}

func TestExportRemovesOnlyUnmodifiedStaleGeneratedPath(t *testing.T) {
	repo := t.TempDir()
	config := testConfig()
	raw, _ := MarshalJSON(config)
	bundle, _ := BuildExportBundle("config.json", config, raw, "boatstack")
	if err := WriteExport(repo, bundle.Files); err != nil {
		t.Fatal(err)
	}
	stale := ".cursor/commands/retro.md"
	delete(bundle.Files, stale)
	if err := WriteExport(repo, bundle.Files); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(stale))); !os.IsNotExist(err) {
		t.Fatal("unmodified stale generated path was not removed")
	}
}
