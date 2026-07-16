package boatstack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var adapterNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

var allowedAdapters = map[string]bool{
	"cursor": true,
	"claude": true,
	"codex":  true,
	"github": true,
}

type ExportBundle struct {
	Files  map[string][]byte
	Config ProjectConfig
}

func LoadConfig(path string) (ProjectConfig, []byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ProjectConfig{}, nil, err
	}
	var config ProjectConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return ProjectConfig{}, nil, err
	}
	if err := ValidateConfig(config); err != nil {
		return ProjectConfig{}, nil, err
	}
	return config, raw, nil
}

func ValidateConfig(config ProjectConfig) error {
	if config.SchemaVersion != 1 {
		return fmt.Errorf("project config schema_version must be 1")
	}
	if strings.TrimSpace(config.Project.Name) == "" {
		return fmt.Errorf("project.name is required")
	}
	if strings.TrimSpace(config.Project.Commands["test"]) == "" {
		return fmt.Errorf("project.commands.test is required; Boatstack will not invent it")
	}
	for _, adapter := range normalizedAdapters(config.Adapters) {
		if !allowedAdapters[adapter] {
			return fmt.Errorf("unsupported adapter: %s", adapter)
		}
	}
	return nil
}

func normalizedAdapters(adapters []string) []string {
	if len(adapters) == 0 {
		return []string{"claude", "codex", "cursor", "github"}
	}
	seen := map[string]bool{}
	for _, adapter := range adapters {
		adapter = strings.TrimSpace(adapter)
		if adapter != "" {
			seen[adapter] = true
		}
	}
	result := make([]string, 0, len(seen))
	for adapter := range seen {
		result = append(result, adapter)
	}
	sort.Strings(result)
	return result
}

func commandBody(operation, extra string) string {
	preflight := ""
	if operation == "auto-plan" {
		preflight = `Before reading repository context or drafting artifacts, inspect the active host/system conversation for its Plan-mode file path. If present, run the project-local helper with ` + "`check-source-plan --repo . --plan <host-path>`" + `. Otherwise run ` + "`check-source-plan --repo .`" + `. Use its ` + "`SOURCE_PLAN`" + ` result. Fallback discovery searches only bounded Plan-mode locations and succeeds only for exactly one non-empty file. If discovery blocks, stop and show the candidates or ask the user to save the host plan under ` + "`.product-loop/intake/`" + `. Accept ` + "`/auto-plan <plan-file>`" + ` only as an ambiguity override. Do not create the missing source plan inside auto-plan.`
	}
	return fmt.Sprintf(`# %s

Run the %s operation from @.product-loop/workflow.md.

%s

Read @.product-loop/project.json, @.product-loop/artifacts.md, and only the minimal repository context relevant to the current feature. %s

Use the gate semantics in the canonical workflow. Do not redefine them in this adapter. Boatstack leaves implementation tactics open, but completion, approval, and shipping claims require current evidence.
`, operation, operation, preflight, extra)
}

func BuildExportBundle(configPath string, config ProjectConfig, rawConfig []byte, adapterName string) (ExportBundle, error) {
	if !adapterNamePattern.MatchString(adapterName) {
		return ExportBundle{}, fmt.Errorf("adapter name must be a lowercase kebab-case slug")
	}
	if err := ValidateConfig(config); err != nil {
		return ExportBundle{}, err
	}
	adapters := normalizedAdapters(config.Adapters)
	config.Adapters = adapters
	files := map[string][]byte{}

	projectJSON, err := GeneratedJSON(config)
	if err != nil {
		return ExportBundle{}, err
	}
	files[".product-loop/project.json"] = projectJSON
	files[".product-loop/.gitignore"] = []byte("bin/\n")
	files[".product-loop/intake/.gitkeep"] = []byte{}

	for _, name := range []string{"workflow.md", "artifacts.md", "failure-moves.md"} {
		value, err := ReadCanonical("references/" + name)
		if err != nil {
			return ExportBundle{}, err
		}
		files[".product-loop/"+name] = GeneratedMarkdown(string(value))
	}

	entries, err := ReadCanonicalDir("assets/templates")
	if err != nil {
		return ExportBundle{}, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		value, err := ReadCanonical("assets/templates/" + entry.Name())
		if err != nil {
			return ExportBundle{}, err
		}
		path := ".product-loop/templates/" + entry.Name()
		if strings.HasSuffix(entry.Name(), ".json") {
			var decoded any
			if err := json.Unmarshal(value, &decoded); err != nil {
				return ExportBundle{}, err
			}
			files[path], err = GeneratedJSON(decoded)
			if err != nil {
				return ExportBundle{}, err
			}
		} else {
			files[path] = GeneratedMarkdown(string(value))
		}
	}

	operations := map[string]string{
		"auto-plan":   "Discover exactly one saved Plan-mode file, refine it into a draft feature package, and record its path as source_plan_path. Do not implement and do not imply the user accepted it.",
		"plan-gate":   "Require the source Plan-mode file and explicit human approval. Only then run the project-local Boatstack helper to compile the executable task/evidence package and approval lock.",
		"build":       "Before editing, locate the source Plan-mode file, feature spec, structured plan, compiled tasks, and plan lock; pass all of them to the project-local Boatstack helper when checking the lock. Stop if it reports BLOCKED. Implementation tactics remain open inside the approved boundary.",
		"test-gate":   "Build a requirement-to-evidence matrix and treat self-authored tests as evidence rather than the sole oracle.",
		"review-gate": "Review the actual diff against approved intent, invariants, risks, gaps, and test evidence.",
		"ship-gate":   "Prepare a PR only; do not merge or deploy without separate authorization.",
		"review":      "Alias of review-gate: review the actual diff against approved intent, invariants, risks, gaps, and test evidence.",
		"ship":        "Alias of ship-gate: prepare a PR only; do not merge or deploy without separate authorization.",
		"retro":       "Classify evidence and propose a move; never promote it or change durable rules without a paired gate.",
	}

	if contains(adapters, "cursor") {
		rule := `---
description: Use Boatstack for evidence-engineered planning, explicit approval, open implementation, evidence gates, and PR preparation.
globs:
alwaysApply: false
---

The source of truth is @.product-loop/workflow.md and @.product-loop/project.json.
Use @.product-loop/artifacts.md for document meanings and @.product-loop/failure-moves.md for improvement experiments.
Ordinary product intent starts in the host's Plan mode. Save the completed plan under .product-loop/intake/. Auto-plan discovers exactly one saved plan from bounded host locations, validates it, and must not invent a substitute. Keep the source plan present and current through build.
Do not start build work until the explicit plan gate has produced a valid plan lock.
Implementation methods are open. Claims of completion, approval, review, and shipping require evidence.
Do not branch behavior on model name, provider, or price; branch on observed work state and evidence.
`
		files[fmt.Sprintf(".cursor/rules/%s.mdc", adapterName)], err = GeneratedFrontmatter(rule)
		if err != nil {
			return ExportBundle{}, err
		}
		for operation, extra := range operations {
			files[fmt.Sprintf(".cursor/commands/%s.md", operation)] = GeneratedMarkdown(commandBody(operation, extra))
		}
	}

	adapterSkill := fmt.Sprintf(`---
name: %s
description: Run Boatstack's evidence-engineered coding node for question-led planning, explicit approval, open implementation, evidence gates, and PR preparation.
---

# Boatstack adapter

Read .product-loop/project.json and .product-loop/workflow.md. The requested operation is supplied by the user; valid operations are auto-plan, plan-gate, build, test-gate, review-gate/review, ship-gate/ship, and retro.

Ordinary product intent must first be explored in the host's Plan mode and saved as a file, preferably under .product-loop/intake/. Auto-plan runs bounded discovery before inspecting the repository and records the single result as source_plan_path. If no file exists or multiple candidates remain, auto-plan is BLOCKED; it must not guess or create a substitute. An explicit path is only an ambiguity override. The source plan remains required and hash-current through plan-gate and build. Test, review, and ship gates operate from the approved lock, diff, and evidence after build.

Use .product-loop/artifacts.md for document boundaries and .product-loop/failure-moves.md for improvement experiments. Do not implement from an unapproved or stale plan. Implementation tactics are open; completion, approval, and shipping claims require current evidence. Do not branch on model identity; use observable state and gate evidence.

If gstack is enabled, use only its namespaced /gstack-* specialist lenses inside Boatstack operations. If Spec Kit is enabled, use it to generate or cross-check artifacts; never invoke speckit.implement to bypass Boatstack's plan approval and build gate.
`, adapterName)
	if contains(adapters, "claude") {
		files[fmt.Sprintf(".claude/skills/%s/SKILL.md", adapterName)], err = GeneratedFrontmatter(adapterSkill)
		if err != nil {
			return ExportBundle{}, err
		}
	}
	if contains(adapters, "codex") {
		files[fmt.Sprintf(".agents/skills/%s/SKILL.md", adapterName)], err = GeneratedFrontmatter(adapterSkill)
		if err != nil {
			return ExportBundle{}, err
		}
	}
	if contains(adapters, "github") {
		files[fmt.Sprintf(".github/PULL_REQUEST_TEMPLATE/%s.md", adapterName)] = GeneratedMarkdown(`# Evidence-engineered change

## Approved intent

- Feature spec:
- Approved plan hash:
- Human approver:
- Linked ADRs/questions:

## Outcome

- User-visible change:
- Non-goals preserved:

## Gate evidence

- Test gate: BLOCKED
- Review gate: BLOCKED
- Ship gate: BLOCKED
- Evidence ledger:

## Known gaps

- Gap ledger:
- PASS_WITH_GAPS rationale, owner, and revisit trigger:

## Rollout and rollback

- Rollout:
- Observability:
- Rollback:

## Generated adapter update

- Boatstack version:
- Config hash:
- Export check:
`)
	}

	hashes := map[string]string{}
	for path, value := range files {
		hashes[path] = SHA256Bytes(value)
	}
	lock := map[string]any{
		"schema_version":    1,
		"generator":         Generator,
		"boatstack_version": Version,
		"config_source":     filepath.Base(configPath),
		"config_sha256":     SHA256Bytes(rawConfig),
		"adapters":          adapters,
		"integrations":      config.Integrations,
		"runtime": map[string]any{
			"source_commit":    SourceCommit,
			"checksums_sha256": ChecksumsSHA256,
		},
		"files": hashes,
	}
	files[".product-loop/generated.lock.json"], err = GeneratedJSON(lock)
	if err != nil {
		return ExportBundle{}, err
	}
	return ExportBundle{Files: files, Config: config}, nil
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func owned(value []byte, path string) bool {
	text := string(value)
	if strings.Contains(text, Marker) || strings.Contains(text, "Generated by product-engineering-loop exporter.") {
		return true
	}
	if strings.HasSuffix(path, ".json") {
		decoded := map[string]any{}
		if json.Unmarshal(value, &decoded) == nil {
			generator := decoded["_generated_by"]
			return generator == Generator || generator == "product-engineering-loop-exporter"
		}
	}
	return false
}

func previousFiles(repo string) map[string]string {
	value, err := os.ReadFile(filepath.Join(repo, ".product-loop/generated.lock.json"))
	if err != nil {
		return map[string]string{}
	}
	var lock struct {
		Files map[string]string `json:"files"`
	}
	if json.Unmarshal(value, &lock) != nil || lock.Files == nil {
		return map[string]string{}
	}
	return lock.Files
}

func ExportCollisions(repo string, files map[string][]byte) []string {
	problems := []string{}
	for _, relative := range sortedKeys(files) {
		target := filepath.Join(repo, filepath.FromSlash(relative))
		current, err := os.ReadFile(target)
		if os.IsNotExist(err) || (err == nil && string(current) == string(files[relative])) {
			continue
		}
		if err != nil || !owned(current, relative) {
			problems = append(problems, relative)
		}
	}
	previous := previousFiles(repo)
	for relative, expectedHash := range previous {
		if _, stillGenerated := files[relative]; stillGenerated {
			continue
		}
		current, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(relative)))
		if err == nil && SHA256Bytes(current) != expectedHash {
			problems = append(problems, "stale generated path modified downstream: "+relative)
		}
	}
	sort.Strings(problems)
	return problems
}

func WriteExport(repo string, files map[string][]byte) error {
	if problems := ExportCollisions(repo, files); len(problems) > 0 {
		return fmt.Errorf("refusing to overwrite user-owned files: %s", strings.Join(problems, ", "))
	}
	for relative, expectedHash := range previousFiles(repo) {
		if _, stillGenerated := files[relative]; stillGenerated {
			continue
		}
		target := filepath.Join(repo, filepath.FromSlash(relative))
		current, err := os.ReadFile(target)
		if err == nil && SHA256Bytes(current) == expectedHash {
			if err := os.Remove(target); err != nil {
				return err
			}
		}
	}
	for _, relative := range sortedKeys(files) {
		if err := writeFile(filepath.Join(repo, filepath.FromSlash(relative)), files[relative], 0o644); err != nil {
			return err
		}
	}
	return nil
}

func CheckExport(repo string, files map[string][]byte) error {
	problems := []string{}
	for _, relative := range sortedKeys(files) {
		current, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(relative)))
		if os.IsNotExist(err) {
			problems = append(problems, "missing "+relative)
		} else if err != nil {
			problems = append(problems, fmt.Sprintf("unreadable %s: %v", relative, err))
		} else if string(current) != string(files[relative]) {
			problems = append(problems, "drift "+relative)
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("generated output is stale: %s", strings.Join(problems, ", "))
	}
	return nil
}
