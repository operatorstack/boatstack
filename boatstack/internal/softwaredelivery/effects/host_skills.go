package effects

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
)

const hostSkillManifestSchema = 1

type hostSkillManifest struct {
	SchemaVersion int               `json:"schema_version"`
	Files         map[string]string `json:"files"`
}

type hostSkillMode struct {
	Slug              string
	DisplayName       string
	Description       string
	Target            string
	Extra             string
	AuthorityContract string
}

var hostSkillModes = []hostSkillMode{
	{
		Slug: "boatstack-update", DisplayName: "Boatstack Update",
		Description:       "Apply a checksum-verified Boatstack update.",
		Target:            "`installation.update` or, after exact human acceptance of program drift, `installation.reconcile-update`",
		Extra:             "This trigger does not reclassify or advance a product delivery.",
		AuthorityContract: updateAuthorityContract,
	},
}

const updateAuthorityContract = `For this operation, request only checksum-verified installation authority. Do not
request or materialize repository, provider, publication, product-delivery, or
merge authority. Installation receipts cannot be reused to broaden this scope.

If the candidate reports exact compiled-program drift, preserve the healthy admitted
runtime and present the prior program fingerprint, candidate program
fingerprint, and program-delta fingerprint. Do not accept the delta implicitly.
After explicit human acceptance, rerun the same checksum-bound update with
` + "`--accept-program-change`" + ` so the Kernel uses the single atomic
` + "`installation.reconcile-update`" + ` boundary. If the update has an interrupted local
transaction and ` + "`recovery.rollback`" + ` is permitted, carry the same human authority
through that rollback, preserve its complete receipt, and retry once from the
restored healthy prior runtime. Never acquire repository authority to escape an
update recovery frontier.`

func renderHostSkill(mode hostSkillMode) []byte {
	return []byte(fmt.Sprintf(`---
name: %s
description: %s Use only when the user explicitly selects this Boatstack operation.
---

# %s

Select %s. %s

Run `+
		"`boatstack status --repo . --format json`"+` once for observation. An authority-free
`+"`FRONTIER`"+` from status is diagnostic only and cannot terminate this selected operation.

Bind one command-scoped context containing the exact objective, delivery, repository,
worktree, flow, actor, and supplied authority receipts. Preserve that context
through every `+"`next`"+`, `+"`apply`"+`, `+"`recover`"+`, and re-resolution. Never synthesize missing
authority or infer it from authentication, files, branches, or prior conversation.
Within that context, track requested authority sources separately from currently
materialized authority receipts.

%s

Begin each cycle with an untargeted authority-bearing `+"`next`"+`. A `+"`CANDIDATE`"+`
identifies the next transition but is not permission to apply it: bind only its
declared parameters and re-resolve that exact transition. Apply only the stable
transition ID from the immediately preceding `+"`PRESCRIBED`"+` result and only its
declared parameters. Carry that result's prescription ID, expected state revision,
expected program fingerprint, expected snapshot fingerprint, and correlation
unchanged into `+"`apply`"+` or `+"`recover`"+`. Never construct, reuse, or omit those
bindings. If the Kernel returns `+"`STALE_PRESCRIPTION`"+`, preserve its complete
diagnostic, perform no effect, discard the prescription, and re-resolve once from
the same command context. Preserve the complete apply response and stderr, including
admission, receipt, postcondition, error, recovery, and transaction fields.
Re-resolve with the same context after every complete receipt.

Evaluate a frontier only after every requested authority source is materialized
or conclusively rejected against the post-receipt state. Stop only on an
authority-bearing `+"`FRONTIER`"+`, `+"`BLOCKED`"+`, `+"`REFUSED`"+`, or
`+"`UNRESOLVED`"+` result for this operation. Treat `+"`TERMINAL`"+` as exact objective evidence.
If recovery is active, use only a transition in `+"`recovery_info.permitted`"+` and
the exact transaction ID. Never choose maintenance, correction, abandonment,
merge, provider, or destructive authority as an escape from a frontier.
`, mode.Slug, mode.Description, mode.DisplayName, mode.Target, mode.Extra, mode.AuthorityContract))
}

func renderOpenAIMetadata(mode hostSkillMode) []byte {
	return []byte(fmt.Sprintf(`interface:
  display_name: %q
  short_description: %q
  default_prompt: %q
policy:
  allow_implicit_invocation: false
`, mode.DisplayName, mode.Description, "Use $"+mode.Slug+" to follow the authority-preserving Boatstack driver."))
}

func desiredHostSkillFiles(hosts []string) map[string][]byte {
	desired := map[string][]byte{}
	for _, host := range hosts {
		for _, mode := range hostSkillModes {
			skill := renderHostSkill(mode)
			switch host {
			case "codex":
				root := filepath.ToSlash(filepath.Join(".agents", "skills", mode.Slug))
				desired[root+"/SKILL.md"] = skill
				desired[root+"/agents/openai.yaml"] = renderOpenAIMetadata(mode)
			case "claude":
				desired[filepath.ToSlash(filepath.Join(".claude", "skills", mode.Slug, "SKILL.md"))] = skill
			case "gemini":
				desired[filepath.ToSlash(filepath.Join(".gemini", "skills", mode.Slug, "SKILL.md"))] = skill
			case "cursor":
				desired[filepath.ToSlash(filepath.Join(".cursor", "commands", mode.Slug+".md"))] = skill
			}
		}
	}
	return desired
}

// ProjectedHostSkillFiles returns the exact runtime-owned host projection and
// manifest bytes without mutating a repository.
func ProjectedHostSkillFiles(hosts []string) (map[string][]byte, []byte, error) {
	desired := desiredHostSkillFiles(hosts)
	manifest := hostSkillManifest{SchemaVersion: hostSkillManifestSchema, Files: map[string]string{}}
	for path, raw := range desired {
		manifest.Files[path] = sha256Bytes(raw)
	}
	manifestRaw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return desired, append(manifestRaw, '\n'), nil
}

func prepareHostSkillMutations(repository string, hosts []string) ([]ports.ResourceMutation, error) {
	desired := desiredHostSkillFiles(hosts)
	manifestPath := filepath.Join(repository, ".boatstack", "host-skills.json")
	manifestRaw, manifestExists, _, err := readAllIfExists(manifestPath)
	if err != nil {
		return nil, err
	}
	prior := hostSkillManifest{Files: map[string]string{}}
	if manifestExists {
		if err := json.Unmarshal(manifestRaw, &prior); err != nil || prior.SchemaVersion != hostSkillManifestSchema || prior.Files == nil {
			return nil, fmt.Errorf("Boatstack host-skill manifest is malformed")
		}
	}

	for relative, expected := range prior.Files {
		absolute, pathErr := managedHostSkillPath(repository, relative)
		if pathErr != nil {
			return nil, pathErr
		}
		current, exists, _, readErr := readAllIfExists(absolute)
		if readErr != nil {
			return nil, readErr
		}
		if !exists || sha256Bytes(current) != expected {
			return nil, fmt.Errorf("Boatstack host skill %s changed outside the managed projection", relative)
		}
	}

	var mutations []ports.ResourceMutation
	paths := make([]string, 0, len(desired))
	for relative := range desired {
		paths = append(paths, relative)
	}
	sort.Strings(paths)
	next := hostSkillManifest{SchemaVersion: hostSkillManifestSchema, Files: map[string]string{}}
	for _, relative := range paths {
		absolute, pathErr := managedHostSkillPath(repository, relative)
		if pathErr != nil {
			return nil, pathErr
		}
		current, exists, _, readErr := readAllIfExists(absolute)
		if readErr != nil {
			return nil, readErr
		}
		if exists && !manifestExists && !strings.EqualFold(sha256Bytes(current), sha256Bytes(desired[relative])) {
			return nil, fmt.Errorf("unmanaged file collides with Boatstack host skill %s", relative)
		}
		if !exists || !strings.EqualFold(sha256Bytes(current), sha256Bytes(desired[relative])) {
			mutation, mutationErr := mutationFor(absolute, desired[relative], 0o644, false, false)
			if mutationErr != nil {
				return nil, mutationErr
			}
			mutations = append(mutations, mutation)
		}
		next.Files[relative] = sha256Bytes(desired[relative])
	}

	for relative := range prior.Files {
		if _, keep := desired[relative]; keep {
			continue
		}
		absolute, pathErr := managedHostSkillPath(repository, relative)
		if pathErr != nil {
			return nil, pathErr
		}
		mutation, mutationErr := mutationFor(absolute, nil, 0o644, false, true)
		if mutationErr != nil {
			return nil, mutationErr
		}
		mutations = append(mutations, mutation)
	}

	nextRaw, err := encodeJSON(next)
	if err != nil {
		return nil, err
	}
	if !manifestExists || string(manifestRaw) != string(nextRaw) {
		mutation, mutationErr := mutationFor(manifestPath, nextRaw, 0o644, true, false)
		if mutationErr != nil {
			return nil, mutationErr
		}
		mutations = append(mutations, mutation)
	}
	return mutations, nil
}

func managedHostSkillPath(repository, relative string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(relative))
	if filepath.IsAbs(clean) || clean == "." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid managed host-skill path %q", relative)
	}
	return filepath.Join(repository, clean), nil
}
