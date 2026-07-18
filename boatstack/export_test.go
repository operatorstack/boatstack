package boatstack

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixtureDirEntry struct{ name string }

func (entry fixtureDirEntry) Name() string               { return entry.name }
func (entry fixtureDirEntry) IsDir() bool                { return false }
func (entry fixtureDirEntry) Type() fs.FileMode          { return 0 }
func (entry fixtureDirEntry) Info() (fs.FileInfo, error) { return nil, nil }

func TestBuildExportBundleNamesMalformedEmbeddedJSONTemplate(t *testing.T) {
	oldRead := readCanonical
	oldReadDir := readCanonicalDir
	defer func() { readCanonical, readCanonicalDir = oldRead, oldReadDir }()
	readCanonicalDir = func(path string) ([]fs.DirEntry, error) {
		entries, err := oldReadDir(path)
		return append(entries, fixtureDirEntry{name: "nul-fixture.json"}), err
	}
	readCanonical = func(path string) ([]byte, error) {
		if path == "assets/templates/nul-fixture.json" {
			return []byte{'{', 0, '}'}, nil
		}
		return oldRead(path)
	}
	config := testConfig()
	raw, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = BuildExportBundle(".boatstack-project.json", config, raw, "boatstack")
	if err == nil || !strings.Contains(err.Error(), "build export bundle from JSON template") || !strings.Contains(err.Error(), "assets/templates/nul-fixture.json") {
		t.Fatalf("malformed template error lacks operation or exact asset: %v", err)
	}
}

func TestDecodeJSONAlwaysNamesOperationAndSource(t *testing.T) {
	for _, test := range []struct {
		operation string
		source    string
	}{
		{"load project configuration", "/repo/.boatstack-project.json"},
		{"validate generated export bundle", ".product-loop/generated.lock.json"},
		{"look up latest release", "GitHub releases/latest response"},
	} {
		t.Run(test.operation, func(t *testing.T) {
			var decoded any
			err := DecodeJSON(test.operation, test.source, []byte{'{', 0, '}'}, &decoded)
			if err == nil || !strings.Contains(err.Error(), "operation "+test.operation) || !strings.Contains(err.Error(), "parse JSON "+test.source) {
				t.Fatalf("JSON diagnostic lacks operation or source: %v", err)
			}
		})
	}
}

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
		".cursor/commands/boatstack-next.md",
		".cursor/commands/boatstack-update.md",
		".cursor/commands/repair.md",
		".cursor/commands/plan-gate.md",
		".cursor/commands/review.md",
		".claude/skills/boatstack/SKILL.md",
		".claude/skills/auto-plan/SKILL.md",
		".claude/skills/boatstack-update/SKILL.md",
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
	claudeSkillPaths := map[string]bool{}
	for path := range bundle.Files {
		if strings.HasPrefix(path, ".claude/skills/") && strings.HasSuffix(path, "/SKILL.md") {
			claudeSkillPaths[path] = true
		}
	}
	if len(claudeSkillPaths) != len(claudeVisibleSkills)+1 {
		t.Fatalf("generated %d Claude skills, want %d: %#v", len(claudeSkillPaths), len(claudeVisibleSkills)+1, claudeSkillPaths)
	}
	for _, spec := range claudeVisibleSkills {
		path := ".claude/skills/" + spec.Name + "/SKILL.md"
		skill := string(bundle.Files[path])
		for _, expected := range []string{
			"name: " + spec.Name,
			"description: " + spec.Description,
			"disable-model-invocation: true",
			"Run the " + spec.Name + " operation",
			".product-loop/workflow.md",
			"User-facing response contract",
		} {
			if !strings.Contains(skill, expected) {
				t.Fatalf("%s is missing %q", path, expected)
			}
		}
	}
	claudeAutoPlan := string(bundle.Files[".claude/skills/auto-plan/SKILL.md"])
	for _, expected := range []string{`argument-hint: "[plan-file]"`, "$ARGUMENTS", "/auto-plan <plan-file>"} {
		if !strings.Contains(claudeAutoPlan, expected) {
			t.Fatalf("Claude auto-plan skill is missing argument behavior %q", expected)
		}
	}
	claudeRouter := string(bundle.Files[".claude/skills/boatstack/SKILL.md"])
	if !strings.Contains(claudeRouter, "user-invocable: false") || strings.Contains(claudeRouter, "disable-model-invocation: true") {
		t.Fatal("Claude Boatstack router must be hidden from users but available to the model")
	}
	for _, operation := range []string{"retro", "review", "ship"} {
		path := ".claude/skills/" + operation + "/SKILL.md"
		if claudeSkillPaths[path] {
			t.Fatalf("internal or alias operation must not be a visible Claude skill: %s", path)
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
		"boatstack-run":    {"Start a Boatstack feature", "Feature complete"},
		"auto-plan":        {"Plan ready", "I need your input"},
		"plan-gate":        {"Ready for your approval", "Approved — ready to build"},
		"build":            {"Build complete", "Build needs a decision"},
		"repair":           {},
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
	if !strings.Contains(autoPlan, "Markdown-only") || !strings.Contains(autoPlan, "Never silently choose a default") || !strings.Contains(autoPlan, "planning-write") || !strings.Contains(autoPlan, "PROPOSED") {
		t.Fatal("auto-plan adapter does not enforce the Markdown and question boundaries")
	}
	for _, expected := range []string{"compact keys such as 1a/1b", "exactly one choice per question with (Recommended)", "offer r to accept all displayed recommendations", "echo the selected mapping"} {
		if !strings.Contains(autoPlan, expected) {
			t.Fatalf("auto-plan adapter is missing finite-question shortcut rule %q", expected)
		}
	}
	if !strings.Contains(planGate, "approval.md") || !strings.Contains(planGate, "Remain in Plan mode") || !strings.Contains(planGate, "record-approval") {
		t.Fatal("plan-gate adapter does not keep approval in Plan mode")
	}
	for _, expected := range []string{
		"normal user action is the exact standalone reply a",
		"Trim surrounding whitespace and match a case-insensitively",
		"do not treat [a] or an a embedded in other text as approval",
		"Continue accepting the full reply approve for compatibility",
		"do not advertise it in the user-facing response",
		"Reply `a` to approve.",
		"authenticated GitHub login",
		"never infer it from a filesystem username",
	} {
		if !strings.Contains(planGate, expected) {
			t.Fatalf("plan-gate adapter is missing approval identity rule %q", expected)
		}
	}
	if !strings.Contains(build, "activate-plan") || !strings.Contains(build, "READY_FOR_BUILD") || !strings.Contains(build, "without activating") || strings.Contains(build, "compile-plan") {
		t.Fatal("build adapter must activate the Markdown plan exactly once")
	}
	if !strings.Contains(build, "delivery-status") || !strings.Contains(build, "push and PR mutation are never build tactics") {
		t.Fatal("build adapter does not confine work to the active delivery slice")
	}
	if !strings.Contains(string(bundle.Files[".cursor/commands/test-gate.md"]), "record-delivery-gate") || !strings.Contains(string(bundle.Files[".cursor/commands/review-gate.md"]), "record-delivery-gate") {
		t.Fatal("test and review adapters do not record slice-scoped gate receipts")
	}
	ship := string(bundle.Files[".cursor/commands/ship-gate.md"])
	for _, expected := range []string{"separate repair PR", "Never edit unrelated code", "exact title", "Reply `o` to open PR.", "Reply `u` to update PR.", "full replies open PR and update PR for compatibility", "preview fingerprint"} {
		if !strings.Contains(ship, expected) {
			t.Fatalf("ship adapter is missing reviewer-ready PR rule %q", expected)
		}
	}
	if _, exists := bundle.Files[".cursor/commands/pr-brief.md"]; exists {
		t.Fatal("PR brief must remain natural-language behavior, not a public command")
	}
	run := string(bundle.Files[".cursor/commands/boatstack-run.md"])
	for _, expected := range []string{"run-preflight --repo . --json", "fetch", "next-status --repo . --json", "three complete automated repair-and-gate cycles", "automatically continue the run", "never merge or deploy"} {
		if !strings.Contains(run, expected) {
			t.Fatalf("boatstack-run adapter is missing %q", expected)
		}
	}
	update := string(bundle.Files[".cursor/commands/boatstack-update.md"])
	for _, expected := range []string{"check-update", "chore/update-boatstack-v", "BOATSTACK_MODE=update", "Reply `o` to open update PR.", "full reply open update PR for compatibility", "Never merge"} {
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
	for _, expected := range []string{"alwaysApply: true", "Before modifying product code", "active managed delivery", "repair operation", "ordinary language"} {
		if !strings.Contains(cursorRule, expected) {
			t.Fatalf("Cursor rule is missing conversational repair routing %q", expected)
		}
	}
	for _, expected := range []string{"delivery_slices", "active slice", "Direct push and PR mutation", "plan approval is never publication authority"} {
		if !strings.Contains(cursorRule, expected) {
			t.Fatalf("Cursor rule is missing phase-scoped delivery rule %q", expected)
		}
	}
	for _, expected := range []string{"naturally asks Boatstack", "evidence-limited ad-hoc PR brief", "not a /pr-brief command", "NOT_VERIFIED"} {
		if !strings.Contains(cursorRule, expected) {
			t.Fatalf("Cursor rule is missing ad-hoc PR behavior %q", expected)
		}
	}
	repair := string(bundle.Files[".cursor/commands/repair.md"])
	for _, expected := range []string{"next-status", "No active delivery to repair", "NOT_STARTED", "DRAFT_PLAN", "APPROVED", "record-change", "implementation_repair", "verification_repair", "requirement_amendment", "needs_clarification", "/test-gate", "/review-gate", "MainThreadShellExec not initialized", "Developer: Reload Window"} {
		if !strings.Contains(repair, expected) {
			t.Fatalf("repair adapter is missing %q", expected)
		}
	}
	runCommand := string(bundle.Files[".cursor/commands/boatstack-run.md"])
	for _, expected := range []string{"SOURCE_PLAN_READY", "NOT_STARTED", "auto-plan", "planning and plan-gate do not require", "MainThreadShellExec not initialized", "Developer: Reload Window"} {
		if !strings.Contains(runCommand, expected) {
			t.Fatalf("run adapter is missing startup recovery rule %q", expected)
		}
	}
	for _, path := range []string{".claude/skills/boatstack/SKILL.md", ".agents/skills/boatstack/SKILL.md"} {
		router := string(bundle.Files[path])
		if !strings.Contains(router, "automatically use repair") || !strings.Contains(router, "active managed delivery") {
			t.Fatalf("%s does not auto-route free-form delivery changes", path)
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
	for _, expected := range []string{
		"## User-facing response contract",
		"Exactly one primary action",
		"### Reply shortcuts",
		"| `a` | Reviewed plan awaiting approval",
		"| `o` | New feature, ad-hoc, or Boatstack-update PR preview",
		"| `u` | Existing PR preview",
		"| `r` | One or more finite questions",
		"match shortcuts case-insensitively against the complete reply",
		"Bracketed forms such as `[o]`, embedded letters, and shortcuts from another state",
		"Continue accepting the full replies for compatibility",
		"do not advertise them in user-facing responses",
		"Never interpret `r` as plan approval, PR publication, identity, secret input, permission escalation, policy bypass, destructive recovery authorization",
		"`1a`, `1b`, and `1c`",
		"exactly one recommendation",
		"echo the question-to-answer mapping",
		"Otherwise ask again without choosing",
		"Reply `a` to approve.",
		"gh api user --jq .login",
		"Never infer the approver",
	} {
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
		for _, expected := range []string{"User-facing response contract", "exactly one Next step", "a approves the pending plan", "o opens the currently previewed feature/ad-hoc/update PR", "u updates the currently previewed existing PR", "r accepts every recommendation", "Bracketed forms such as [o]", "Continue accepting approve, open PR, update PR, and open update PR for compatibility", "do not advertise them in user-facing responses", "Never interpret r as plan approval, PR publication, identity, secret input, permission escalation, policy bypass, destructive recovery authorization", "1a/1b/1c and 2a/2b/2c", "exactly one recommendation", "Echo the selected question-to-answer mapping", "filesystem username", "Never create or advertise a /pr-brief command", "state-scoped o to open or u to update", "boatstack-update"} {
			if !strings.Contains(adapter, expected) {
				t.Fatalf("%s is missing response-DX rule %q", path, expected)
			}
		}
	}
	questions := string(bundle.Files[".product-loop/templates/questions.md"])
	for _, expected := range []string{"`1a`, `1b`, `1c`", "(Recommended)", "use `r` to accept all displayed recommendations", "question-to-answer mapping is echoed", "never an agent-selected default"} {
		if !strings.Contains(questions, expected) {
			t.Fatalf("question template is missing shortcut rule %q", expected)
		}
	}
}

func TestPortableHostAdaptersShareWorkflowAndArtifactContract(t *testing.T) {
	config := testConfig()
	raw, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := BuildExportBundle(".boatstack-project.json", config, raw, "boatstack")
	if err != nil {
		t.Fatal(err)
	}

	workflow := string(bundle.Files[".product-loop/workflow.md"])
	artifacts := string(bundle.Files[".product-loop/artifacts.md"])
	for _, expected := range []string{"boatstack-next", "boatstack-run", "auto-plan", "plan-gate", "build", "test-gate", "review-gate", "ship-gate", "boatstack-update", "retro"} {
		if !strings.Contains(workflow, expected) {
			t.Fatalf("canonical portable workflow is missing %q", expected)
		}
		if _, exists := bundle.Files[".cursor/commands/"+expected+".md"]; !exists {
			t.Fatalf("Cursor does not expose portable operation %q", expected)
		}
	}
	for _, spec := range claudeVisibleSkills {
		if _, exists := bundle.Files[".claude/skills/"+spec.Name+"/SKILL.md"]; !exists {
			t.Fatalf("Claude does not expose user operation %q", spec.Name)
		}
	}
	for _, expected := range []string{"source plan", "plan.md", "approval.md", "evidence", "gaps", "review", "pr.md"} {
		if !strings.Contains(strings.ToLower(artifacts), strings.ToLower(expected)) {
			t.Fatalf("repository artifact contract is missing %q", expected)
		}
	}

	hostSurfaces := map[string]string{
		"cursor": string(bundle.Files[".cursor/rules/boatstack.mdc"]),
		"claude": string(bundle.Files[".claude/skills/boatstack/SKILL.md"]),
		"codex":  string(bundle.Files[".agents/skills/boatstack/SKILL.md"]),
	}
	for host, surface := range hostSurfaces {
		for _, expected := range []string{".product-loop/project.json", ".product-loop/workflow.md"} {
			if !strings.Contains(surface, expected) {
				t.Fatalf("%s adapter does not reference shared repository contract %q", host, expected)
			}
		}
	}
	for _, operation := range []string{"next", "boatstack-next", "run", "boatstack-run", "auto-plan", "plan-gate", "build", "test-gate", "review-gate", "ship-gate", "boatstack-update", "retro"} {
		if !strings.Contains(hostSurfaces["codex"], operation) {
			t.Fatalf("Codex router does not declare portable operation %q", operation)
		}
		if !strings.Contains(hostSurfaces["claude"], operation) {
			t.Fatalf("Claude natural-language router does not declare portable operation %q", operation)
		}
	}
}

func TestExportRefusesUserOwnedCollision(t *testing.T) {
	for _, relative := range []string{
		".cursor/rules/boatstack.mdc",
		".claude/skills/auto-plan/SKILL.md",
	} {
		t.Run(relative, func(t *testing.T) {
			repo := t.TempDir()
			path := filepath.Join(repo, filepath.FromSlash(relative))
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
			if err := WriteExport(repo, bundle.Files); err == nil || !strings.Contains(err.Error(), "user-owned") || !strings.Contains(err.Error(), relative) {
				t.Fatalf("expected named user-owned collision, got %v", err)
			}
			value, _ := os.ReadFile(path)
			if string(value) != "user owned\n" {
				t.Fatal("collision handling modified the user-owned file")
			}
		})
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
