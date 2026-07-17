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
		preflight = `Before reading repository context or drafting artifacts, inspect the active host/system conversation for its Plan-mode file path. If present, run the project-local helper with ` + "`check-source-plan --repo . --plan <host-path>`" + `. Otherwise run ` + "`check-source-plan --repo .`" + `. Use its ` + "`SOURCE_PLAN`" + ` result. Fallback discovery searches only bounded Plan-mode locations and succeeds only for exactly one non-empty file. If discovery blocks, stop and show the candidates or ask the user to save the host plan under ` + "`.product-loop/intake/`" + `. Accept ` + "`/auto-plan <plan-file>`" + ` only as an ambiguity override. Do not create the missing source plan inside auto-plan. If the host blocks its ordinary Markdown write tool, pass each known planning document on stdin to ` + "`boatstack-helper planning-write`" + `; never bypass the host boundary with arbitrary shell redirection.`
	}
	return fmt.Sprintf(`# %s

Run the %s operation from @.product-loop/workflow.md.

%s

Read @.product-loop/project.json, @.product-loop/artifacts.md, and only the minimal repository context relevant to the current feature. %s

Use the gate semantics in the canonical workflow. Do not redefine them in this adapter. Auto-plan and plan-gate may create or update Markdown only. Classify authoritative repository facts as DISCOVERED, agent suggestions as PROPOSED, and only explicit human responses as ANSWERED. Every material proposal remains in blocking_questions; never label an agent default as answered. If a structured question tool is unavailable, ask 1-3 plain-text questions, return WAITING_FOR_INPUT internally, and never silently choose a default. Boatstack leaves implementation tactics open, but completion, approval, and shipping claims require current evidence. During managed delivery, read the active delivery slice, never push or mutate a PR directly, and require slice-scoped test and review receipts before ship-gate. A successful publication activates the next declared slice; parent-plan approval never skips its gates.

Follow the User-facing response contract in @.product-loop/workflow.md. Lead with its mapped plain-language outcome, show only decision-relevant content, end with exactly one `+"`### Next step`"+`, and put machine status, helper output, fingerprints, artifact paths, receipts, and locks inside collapsed `+"`Technical details`"+`. Treat helper names in this command as internal control machinery; do not expose them in the primary response.
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
	files[".product-loop/hooks/guard.sh"] = guardShellScript()
	files[".product-loop/hooks/guard.ps1"] = guardPowerShellScript()
	for _, host := range []string{"cursor", "claude", "codex"} {
		if contains(adapters, host) {
			fragment, fragmentErr := hookFragmentJSON(host)
			if fragmentErr != nil {
				return ExportBundle{}, fragmentErr
			}
			files[".product-loop/hooks/"+host+".fragment.json"] = fragment
		}
	}

	for _, name := range []string{"workflow.md", "artifacts.md", "failure-moves.md", "irreversible-operation-boundary.md"} {
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
		"auto-plan":        "Discover exactly one saved Plan-mode file and refine it into a Markdown-only draft feature package whose canonical structured artifact is plan.md. Run check-plan read-only. Record affected_paths and structured side_effects for external writes; use an immutable target identity, transactional or fix-forward recovery, and destructive=false. Keep internal phases as tasks in one delivery slice. Only when the accepted outcome explicitly needs multiple PRs, declare ordered delivery_slices and assign every task exactly once; plan approval never authorizes publication. Do not implement, create JSON or locks, or imply acceptance. If ready, respond with Plan ready and make Run /plan-gate the one next action. If decisions remain, respond with I need your input and ask only 1-3 material questions.",
		"plan-gate":        "Run check-plan read-only, present its fingerprint and all open decisions, and require explicit human approval. The normal user action is simply approve. Resolve approved_by from an explicit supplied identity, otherwise from the authenticated GitHub login when available; ask one short identity follow-up only when neither exists, and never infer it from a filesystem username, commit history, or agent identity. On approval invoke record-approval with the resolved human, RFC3339 timestamp, and exact displayed fingerprint so it writes only approval.md. While pending, respond Ready for your approval with Reply approve as the one next action. After recording, respond Approved — ready to build and make entering the host execution mode and running /build the one next action. Remain in Plan mode; do not compile or request an early mode switch.",
		"build":            "First confirm the host is in an execution-capable mode. If the mode transition is rejected or product-code writes remain unavailable, return READY_FOR_BUILD internally without activating the plan, compiling JSON, or writing a lock. Only then locate plan.md and approval.md and run activate-plan before the first product-code edit. Stop if it reports BLOCKED. Read delivery-status and implement only the active delivery slice task_ids. Run the internal repository safety check after operational or high-risk edits; a destructive capability blocks execution and gate progression but does not block reviewable source editing. Implementation tactics remain open inside the approved boundary, but push and PR mutation are never build tactics and are denied while managed delivery is active. On success respond Build complete and make Run /test-gate the one next action. When a new product decision blocks work, respond Build needs a decision and ask only that question.",
		"test-gate":        "Read delivery-status and test only the active delivery slice. Run the internal repository safety check, build a requirement-to-evidence matrix, and treat self-authored tests as evidence rather than the sole oracle. External writes require immutable target identity, transactional or fix-forward failure behavior, and an independent safety oracle. Commit the intentional slice product and evidence diff, then record-delivery-gate for the active feature and slice with --gate test and PASS or PASS_WITH_GAPS. Editing evidence Markdown alone never passes the gate. On pass respond Tests passed and make Run /review-gate the one next action. On failure respond Testing found a problem and make the required non-destructive repair the one next action.",
		"review-gate":      "Read delivery-status and review the active slice's actual diff against approved intent, invariants, risks, gaps, and test evidence. Run the internal repository safety check. Executable destructive capability is blocking even when ordinary tests pass. On pass invoke record-delivery-gate for the same feature and slice with --gate review; it must reject a changed or untested diff. Then respond Review passed and make Run /ship-gate the one next action. When blocked respond Changes required and make the highest-priority blocking repair the one next action.",
		"ship-gate":        "Prepare a reviewer-ready PR only; do not merge or deploy without separate authorization. Require the current managed feature approval, lock, test evidence, review evidence, and a passing repository safety scan, and commit the intentional product/artifact diff before projection. Internally run pr-context --repo . --feature <feature> in json and template formats, project the approved intent, actual committed diff, decisions, evidence, gaps, rollout, rollback, safety outcome, and operator-only recovery boundary into its required pr.md path, then run check-pr --repo . --preview <pr.md>. Always include why, what changed, review order, evidence, gaps/risks, rollout/rollback, and collapsed provenance; add UI evidence, security/privacy, migration, or operations sections only when the diff makes them relevant. Show the exact title and rendered body before any GitHub mutation. If PR_ACTION is open, respond PR ready with Reply open PR as the one next action; if update, use Reply update PR; if manual, preserve the preview and give one manual publication action. Only after that exact reply: commit only the reviewed pr.md, rerun check-pr and require the same preview fingerprint (PREVIEW_FINGERPRINT), then run publish-pr with --action open or update and that fingerprint. The publisher performs a non-force push and rechecks context before GitHub mutation. If the diff or evidence changes, regenerate instead. If a required check fails on the base branch too, record the evidence and recommend a separate repair PR. Never edit unrelated code in this approved feature branch; a policy-approved bypass requires explicit human authorization. After publication respond PR opened with the link and make Review the PR the one next action; never imply merge authorization. If publish-pr returns UPDATE_AVAILABLE, keep Review the PR as the only next action and append a collapsed update notice saying no files changed and /boatstack-update may be run from the clean default branch after this feature PR merges. Do not check for releases before successful publication.",
		"boatstack-update": "Prepare a visible Boatstack infrastructure update; never mix it into product work or merge it. First run the current helper doctor and force check-update. If current, respond Boatstack is current with No action required. Before mutation fetch the default ref, then require the current clean default branch whose HEAD equals origin/<default>; otherwise respond Update postponed and make finishing the current feature, switching to the clean default branch, and rerunning /boatstack-update the one action. Ensure no update PR or branch already exists, create chore/update-boatstack-v<latest>, then run the installer fetched from that exact release tag with BOATSTACK_MODE=update, BOATSTACK_VERSION=<latest>, BOATSTACK_REPO=<repo>, and BOATSTACK_YES=1. Use install.sh on macOS/Linux and install.ps1 on Windows. The verified update must preserve configuration, adapters, integrations, and user-owned host settings, run doctor, and touch only Boatstack infrastructure. Show the version transition, release notes and link, integration state, exact diff, changed paths, checksums, rollout, and rollback. Respond Boatstack update ready and make Reply open update PR the one next action. Only that exact reply authorizes staging the installer-reported paths, committing chore: update Boatstack to <version>, normal push, and opening a reviewer-ready update PR. If GitHub auth is unavailable, preserve the branch and give one manual publication action. After publication respond Update PR opened with the link and make Review the PR the one next action. On one collision or health failure, respond Update needs attention and make addressing that named problem the one next action. Never merge automatically.",
		"review":           "Alias of review-gate: review the actual diff against approved intent, invariants, risks, gaps, and test evidence. Use Review passed or Changes required and the same single-action routing as review-gate.",
		"ship":             "Alias of ship-gate: prepare and preview the exact reviewer-ready title and body before any GitHub mutation. Require Reply open PR or Reply update PR before publication, recheck the preview against current evidence, and never merge or deploy. Keep pre-existing unrelated failures out of the approved feature branch. Use PR ready before confirmation or PR opened after publication.",
		"retro":            "Classify evidence and propose a move; never promote it or change durable rules without a paired gate. Respond Improvement proposed and make reviewing or authorizing the experiment the one next action.",
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
Do not start build work until the explicit plan gate has produced approval.md and build activation has produced a valid plan lock.
Implementation methods are open. Claims of completion, approval, review, and shipping require evidence.
Plans may contain internal task phases without changing the one-PR flow. Multiple PRs require explicit ordered delivery_slices. Work only on the active slice; every slice must independently pass test-gate, review-gate, and confirmed ship-gate. Direct push and PR mutation are denied while managed delivery is active, and plan approval is never publication authority.
When the user naturally asks Boatstack to prepare, improve, summarize, or update an existing PR without a managed feature package, generate an evidence-limited ad-hoc PR brief. Use the committed branch diff and observed checks, label missing evidence NOT_VERIFIED, and never imply Boatstack approval or passed gates. This is natural-language behavior, not a /pr-brief command. Preview the exact title and body before asking for one open/update confirmation.
When the user asks to update Boatstack itself, use /boatstack-update. Release discovery is read-only and cached; repository mutation begins only from a clean current default branch and is isolated in a versioned chore/update-boatstack branch. Preview the exact infrastructure diff before requiring open update PR. Never mix a Boatstack update into product work or merge it automatically.
Do not branch behavior on model name, provider, or price; branch on observed work state and evidence.
Boatstack's repository hooks deny high-confidence irreversible operations across every agent call. There is no in-session bypass. Preserve failed external state, use read-only diagnosis and fix-forward recovery, and leave intentional destructive recovery to an operator-owned surface outside Boatstack.
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

Read .product-loop/project.json and .product-loop/workflow.md. The requested operation is supplied by the user; valid operations are auto-plan, plan-gate, build, test-gate, review-gate/review, ship-gate/ship, boatstack-update, and retro.

Follow the User-facing response contract in .product-loop/workflow.md for every operation. Lead with the mapped plain-language outcome, show only decision-relevant content, end with exactly one Next step, and move machine statuses, helper output, fingerprints, artifact paths, receipts, and locks into collapsed Technical details. Internal helper names must not appear in the primary response.

Ordinary product intent must first be explored in the host's Plan mode and saved as a file, preferably under .product-loop/intake/. Auto-plan runs bounded discovery before inspecting the repository and records the single result as source_plan_path. If no file exists or multiple candidates remain, auto-plan is BLOCKED; it must not guess or create a substitute. An explicit path is only an ambiguity override. Auto-plan and plan-gate write Markdown only: plan.md remains canonical and approval.md records explicit acceptance. If the host blocks its normal Markdown writer, use the bounded planning-write helper and never arbitrary shell redirection. Repository facts are DISCOVERED, agent suggestions are PROPOSED, and only human responses are ANSWERED; every material proposal remains blocking. At build, confirm the host can edit product code before activating the plan. A rejected mode transition returns READY_FOR_BUILD and creates no machine artifacts or lock. Once execution is available, activation compiles machine artifacts and the lock before the first product-code edit. The source plan remains required and hash-current through build. Test, review, and ship gates operate from the approved lock, diff, and evidence after build.

Internal phases are ordinary tasks inside one delivery slice. Multiple PRs require explicit ordered delivery_slices with every task assigned exactly once. After activation, read delivery-status and work only on the active slice. Test-gate and review-gate must record slice-scoped receipts bound to the current branches, commit, diff, and evidence. Direct push, PR mutation, and ad-hoc PR routing are denied while managed delivery is active. Successful confirmed publication advances exactly one slice; plan approval never authorizes later slices.

Normal approval is simply approve. Use an explicit supplied identity first; otherwise use the authenticated GitHub login when the repository is on GitHub and it is available. Ask once for a name or handle only when no trustworthy identity can be resolved. Never infer the approver from a filesystem username, commit history, or the coding agent. If identity is unavailable after approve, preserve the current approval intent, create no receipt, and ask only for identity; do not require approval again when the unchanged plan and identity are available.

Use .product-loop/artifacts.md for document boundaries and .product-loop/failure-moves.md for improvement experiments. If a structured question tool is unavailable, ask 1-3 plain-text questions and return WAITING_FOR_INPUT; never select defaults on the user's behalf. Do not implement from an unapproved or stale plan. Implementation tactics are open; completion, approval, and shipping claims require current evidence. Do not branch on model identity; use observable state and gate evidence.

Repository hooks enforce Boatstack's immutable deny policy across every agent call. Never request an in-session bypass for a blocked irreversible operation. After an external-write failure, preserve state, run only read-only diagnosis, and prefer transactional rollback or fix-forward recovery. Source code may be edited for review, but executable destructive capability blocks running it and blocks test, review, and ship progression.

At ship, prove whether a failing check is pre-existing by checking the base branch. Keep unrelated repairs in a separate PR; do not modify unrelated code under the approved feature lock. A repository-policy bypass requires explicit human authorization and recorded evidence.

When the user asks to update Boatstack, run the boatstack-update operation. Never prepare it on a feature branch or dirty worktree. A successful update is a separate versioned infrastructure branch whose exact diff is shown before requiring the explicit open update PR publication reply. Preserve current adapters, integrations, and project configuration; never merge the update automatically. After successful feature PR publication, surface UPDATE_AVAILABLE only as a collapsed informational notice while Review the PR remains the sole next action.

For a managed ship, use the internal pr-context operation with --feature to project the feature spec, accepted decisions, actual committed diff, evidence ledger, review findings, gaps, rollout, and rollback into the required pr.md artifact. Inspect the returned changed files, diff stat, high-risk matches, and the actual diff before writing claims; commits alone are not authoritative. Always include why, what changed, review order, evidence, gaps/risks, rollout/rollback, and collapsed provenance. Add UI evidence, security/privacy, migration, or operations sections only when relevant. For a natural-language request to improve an existing or ad-hoc PR, run pr-context without --feature and use the same reviewer-first format from observed branch facts, but mark unavailable approval or gate evidence as NOT_VERIFIED. Never create or advertise a /pr-brief command. Validate with check-pr and always show the exact title and rendered body before publication. Ask for exactly Reply open PR or Reply update PR. Only after that reply, commit only pr.md, revalidate the unchanged preview fingerprint, and invoke the internal publish-pr operation with the selected action. It may perform a normal push but never force-push. Any intervening product diff or evidence change invalidates the preview. Keep model attribution inside collapsed provenance. Internal helper names and hashes stay out of the primary response.

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
		files[fmt.Sprintf(".github/PULL_REQUEST_TEMPLATE/%s.md", adapterName)] = GeneratedMarkdown(`# Reviewer-ready change

## Why this change

Explain the user or engineering outcome, not merely the files edited.

## What changed

| Area | Before | After | Reviewer focus |
|---|---|---|---|
| | | | |

## Review order

1. Start with the contract, trust boundary, or user-visible behavior.

## Evidence

| Claim | Evidence | Result | Source |
|---|---|---|---|
| | | NOT_VERIFIED | |

## Operational safety

State the operational-diff safety result and keep destructive recovery operator-only.

## Known gaps and risks

List explicit gaps with impact and revisit trigger, or state that no material gaps are known.

## Rollout and rollback

- Rollout:
- Observability:
- Smallest safe rollback:

<details>
<summary>Boatstack provenance</summary>

- Mode: managed or evidence-limited ad-hoc
- Approval and gate evidence:
- Coding-host attribution:

</details>
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
