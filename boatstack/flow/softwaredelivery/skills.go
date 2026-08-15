package softwaredelivery

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	"github.com/operatorstack/boatstack/boatstack/flow/skillprojection"
)

func GenerateSkills(compiled controlprogram.Compiled, hosts []string) (map[string][]byte, error) {
	result := map[string][]byte{}
	const exactCheckoutAttributes = "** -text"
	for _, entry := range compiled.Document.Entries {
		slug := flowSkillSlug(compiled.Document.Program.ID, entry.ID)
		if slug == "boatstack-update" {
			return nil, fmt.Errorf("generated Flow skill %q is reserved for kernel maintenance", slug)
		}
		for _, host := range hosts {
			skill := renderSkill(compiled, entry, slug, host)
			switch host {
			case "codex":
				root := filepath.ToSlash(filepath.Join(".agents", "skills", slug))
				result[root+"/.gitattributes"] = []byte(exactCheckoutAttributes)
				result[root+"/SKILL.md"] = skill
				result[root+"/agents/openai.yaml"] = []byte(fmt.Sprintf("interface:\n  display_name: %q\n  short_description: %q\n  default_prompt: %q\npolicy:\n  allow_implicit_invocation: false\n", title(slug), entry.Description, "Use $"+slug+" to run the repository-owned Boatstack Flow entry."))
			case "claude":
				root := filepath.ToSlash(filepath.Join(".claude", "skills", slug))
				result[root+"/.gitattributes"] = []byte(exactCheckoutAttributes)
				result[root+"/SKILL.md"] = skill
			default:
				return nil, fmt.Errorf("unsupported generated Flow skill host %q", host)
			}
		}
	}
	return result, nil
}

func flowSkillSlug(programID, entryID string) string {
	const encodedPrefix = "x0"
	encodedEntry := entryID
	if strings.Contains(entryID, "-") || strings.HasPrefix(entryID, encodedPrefix) {
		encodedEntry = encodedPrefix + hex.EncodeToString([]byte(entryID))
	}
	return programID + "-" + encodedEntry
}

func renderSkill(compiled controlprogram.Compiled, entry controlprogram.Entry, slug, host string) []byte {
	description := entry.Description
	if description == "" {
		description = "Run repository Flow entry " + entry.ID + " to target " + entry.Target + "."
	}
	description += " Use only when the user explicitly selects this repository Flow entry."
	supersession := ""
	delegation := ""
	diagnostics := ""
	workProtocol := ""
	publication := ""
	if len(compiled.Document.Work) != 0 {
		workProtocol = fmt.Sprintf(`
When a response contains a `+"`work`"+` request, treat it as foreground work for
the selected transition, not as a second Flow. Read its exact instruction,
input bindings, output manifest, and staging root. Write only the declared
outputs beneath that staging root and stay within each media type and size
bound.

If human input is required, record the typed suspension with:

`+"`boatstack flow work input-required --repo . --flow %s --entry %s --run-id <run-id> --work-id <work-id> --prompt <question> --host %s --format json`"+`

Ask the user and wait. Store the answer as bounded JSON, then submit it with
`+"`boatstack flow work answer ... --question-id <question-id> --answer <json-path>`"+`.
An answer is evidence, never authority. If work succeeds, run
`+"`boatstack flow work complete ...`"+`; if it cannot continue, run
`+"`boatstack flow work block ... --reason <reason>`"+`. Resume the same entry
and run ID afterward. Never edit the work record directly or continue in the
background while a question is open.
`, compiled.Document.Program.ID, entry.ID, host)
	}
	if entry.Diagnostics != nil && entry.Diagnostics.ExplainOnSuspend {
		diagnostics = fmt.Sprintf(`
If Boatstack suspends this run without reaching the target or prescribing an
applicable action, invoke the read-only debugger with the exact same context:

`+"`boatstack explain --repo . --flow %s --entry %s --run-id <run-id> --host %s --format json`"+`

Preserve its canonical trace exactly. Use it only to report the factual blocker,
ask for the exact missing evidence, or perform an action already prescribed by
Boatstack. An explanation is not authority: never grant authority, fabricate a
run ID, reconstruct the transition graph, or act on a rejected candidate.
`, compiled.Document.Program.ID, entry.ID, host)
	}
	if entry.Delegation != nil {
		delegation = fmt.Sprintf(`
The first `+"`next`"+` returns a typed `+"`DELEGATION_REQUIRED`"+` response before
managed state changes. Display its exact run ID, request fingerprint, requested
authorities, and description. Obtain one explicit human approval for that exact
request, then run:

`+"`boatstack flow authorize --repo . --flow %s --entry %s --run-id <run-id> --request-fingerprint <fingerprint> --human <actor> --host %s`"+`

After authorization, use `+"`boatstack flow run --repo . --flow %s --entry %s --run-id <run-id> --repository-authority --host %s --format json`"+`.
Do not request approval again after a restart or typed suspension. Resume the
same run and delegation unless Boatstack reports revocation, expiry, drift, or
terminal completion. Never authorize on the user's behalf.
`, compiled.Document.Program.ID, entry.ID, host, compiled.Document.Program.ID, entry.ID, host)
	}
	if entry.Target == "published-pr" {
		publication = `
If Boatstack reports ` + "`WORKSPACE_COMMIT_REQUIRED`" + `, stay in the same
managed worktree and run. Commit only the intended delivery changes on the
current managed branch, excluding generated runtime and publication artifacts
unless they are deliberately part of the delivery, then resume this entry.
Never fabricate an external-provider receipt. Boatstack derives provider
capability through its trusted GitHub boundary and reports a typed blocker when
that capability is unavailable.
`
		abandonmentSkill, ok := targetEntrySkill(compiled.Document.Program.ID, compiled.Document.Entries, "safely-abandoned")
		if ok {
			supersession = fmt.Sprintf(`
If the user requests different work, never retarget this run. When no objective
binding receipt exists, stop this unbound attempt and allow the inbox plan to be
replaced. Once the objective is bound, require explicit use of $%s for
the same delivery and wait for its abandonment receipt before selecting a new
plan and starting a new run.
`, abandonmentSkill)
		}
	}
	return []byte(fmt.Sprintf(`---
name: %s
description: %q
---

# %s

Run the repository-owned Flow %q entry %q until its marked target %q is reached.
Boatstack does not interpret the entry name.

%s

Start with `+"`boatstack next --repo . --flow %s --entry %s --repository-authority --host %s --format json`"+`.
Preserve the returned program fingerprint, entry, run ID, delivery, repository,
worktree, host, actor, authority receipts, prescription, and receipts through
every `+"`next`"+`, `+"`apply`"+`, recovery, question, and re-resolution.

Apply only the exact immediately preceding prescription and its declared
parameters. A question suspends this run: ask the user, submit only the typed
answer evidence, and resume the same run ID. Nothing continues in the
background while input is missing. Never synthesize authority.
%s
%s
%s
%s
%s

Stop only when Boatstack reports the marked target, a typed blocker, refusal,
unresolved recovery, or missing authority. This entry grants no merge or deploy
authority.
`, slug, description, title(slug), compiled.Document.Program.ID, entry.ID, entry.Target, skillprojection.BootstrapContract(), compiled.Document.Program.ID, entry.ID, host, delegation, supersession, diagnostics, workProtocol, publication))
}

func targetEntrySkill(programID string, entries []controlprogram.Entry, target string) (string, bool) {
	for _, entry := range entries {
		if entry.Target == target {
			return flowSkillSlug(programID, entry.ID), true
		}
	}
	return "", false
}

func title(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '-' || r == '_' || r == '.' })
	for index := range parts {
		if parts[index] != "" {
			parts[index] = strings.ToUpper(parts[index][:1]) + parts[index][1:]
		}
	}
	return strings.Join(parts, " ")
}
