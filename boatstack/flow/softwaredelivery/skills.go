package softwaredelivery

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
)

func GenerateSkills(compiled controlprogram.Compiled, hosts []string) (map[string][]byte, error) {
	result := map[string][]byte{}
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
				result[root+"/SKILL.md"] = skill
				result[root+"/agents/openai.yaml"] = []byte(fmt.Sprintf("interface:\n  display_name: %q\n  short_description: %q\n  default_prompt: %q\npolicy:\n  allow_implicit_invocation: false\n", title(slug), entry.Description, "Use $"+slug+" to run the repository-owned Boatstack Flow entry."))
			case "claude":
				result[filepath.ToSlash(filepath.Join(".claude", "skills", slug, "SKILL.md"))] = skill
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
	if entry.ID == "run" && hasTargetEntry(compiled.Document.Entries, "safely-abandoned") {
		supersession = fmt.Sprintf(`
If the user requests different work, never retarget this run. When no objective
binding receipt exists, stop this unbound attempt and allow the inbox plan to be
replaced. Once the objective is bound, require explicit use of $%s-abandon for
the same delivery and wait for its abandonment receipt before selecting a new
plan and starting a new run.
`, compiled.Document.Program.ID)
	}
	return []byte(fmt.Sprintf(`---
name: %s
description: %q
---

# %s

Run the repository-owned Flow %q entry %q until its marked target %q is reached.
Boatstack does not interpret the entry name.

Start with `+"`boatstack next --repo . --flow %s --entry %s --host %s --format json`"+`.
Preserve the returned program fingerprint, entry, run ID, delivery, repository,
worktree, host, actor, authority receipts, prescription, and receipts through
every `+"`next`"+`, `+"`apply`"+`, recovery, question, and re-resolution.

Apply only the exact immediately preceding prescription and its declared
parameters. A question suspends this run: ask the user, submit only the typed
answer evidence, and resume the same run ID. Nothing continues in the
background while input is missing. Never synthesize authority.
%s

Stop only when Boatstack reports the marked target, a typed blocker, refusal,
unresolved recovery, or missing authority. This entry grants no merge or deploy
authority.
`, slug, description, title(slug), compiled.Document.Program.ID, entry.ID, entry.Target, compiled.Document.Program.ID, entry.ID, host, supersession))
}

func hasTargetEntry(entries []controlprogram.Entry, target string) bool {
	for _, entry := range entries {
		if entry.Target == target {
			return true
		}
	}
	return false
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
