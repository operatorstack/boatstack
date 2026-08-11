# Boatstack V2 configuration

`.boatstack/project.json` is the repository-owned policy input. V2 accepts only
schema version 2. Unknown top-level fields, unsupported policy values, duplicate
hosts, trailing JSON, and missing required fields fail closed.

```json
{
  "schema_version": 2,
  "project": {
    "name": "example-product",
    "default_branch": "main",
    "context": ["README.md", "docs/architecture/"],
    "commands": {
      "build": "npm run build",
      "test": "npm test"
    },
    "high_risk_paths": ["migrations/**", "billing/**"]
  },
  "policy": {
    "plan_approval": "human",
    "independent_review_for_high_risk": true,
    "visual_evidence": "optional",
    "external_effect_authority": "human-or-autonomy-plus-provider"
  },
  "hosts": ["cli", "cursor", "codex", "claude", "gemini", "mcp"]
}
```

## Required values

- `project.name`, `project.default_branch`, and `project.commands`;
- `policy.plan_approval`: `human` or `human-or-autonomy`;
- `policy.visual_evidence`: `off`, `optional`, or `required`;
- at least the `cli` host.

The only accepted external-effect authority policy is
`human-or-autonomy-plus-provider`. Provider authority is an independent
mandatory clause; it cannot be replaced by a human receipt.

The canonical snapshot carries this policy projection as controlling evidence.
`human` plan approval rejects autonomy receipts. `human-or-autonomy` accepts
either class. When independent high-risk review is enabled, the observer derives
the changed paths from `default_branch...HEAD` plus tracked and untracked working
tree changes; a matching `high_risk_paths` glob makes `gate.review.record`
require human authority. A required visual policy prevents the verified terminal
until a revision-bound `evidence.visual.attach` receipt exists, while `off`
refuses attachment. A host omitted from `hosts` cannot request managed
transitions. If the configured default branch cannot be inspected, the
high-risk derivation fails closed whenever that policy is active.

`project.commands` names the repository's canonical product checks. Build and
test gate transitions execute those exact repository-owned commands inside the
effect boundary and install evidence only after a zero exit status. The command
is screened by the same constitutional guard first, and its output is never
persisted. Gate authority also requires a strict revision-bound passed-evidence
document; an arbitrary fingerprint string is insufficient.

To change configuration, write a candidate file elsewhere, hash it, then request
`configuration.mutate`. The kernel copies the exact bytes, installs state last,
re-observes the tracked file, and accepts success only if its fingerprint remains
current.

## Configuration authority and topology

Embedded repositories read `.boatstack/project.json`. `repository.attach`
requires an explicit `config_authority` of `repository` or `external`:

- `repository` keeps the committed repository document authoritative;
- `external` transactionally copies the currently verified bytes into the
  clone-family external controller before installing the detached binding.

`repository.detach` performs the inverse verified-byte transfer when external
authority is active, then removes the binding last. A missing, invalid, or
fingerprint-mismatched source is never promoted as verified configuration.
The detached binding records the selected authority, so readers never choose a
configuration source by first-match path discovery.

```sh
boatstack attach --repo . --human alice \
  --goal-id bootstrap --goal-kind approved-plan --delivery bootstrap \
  --param topology=detached --param config_authority=external
```

V1 configuration schemas are intentionally unsupported. Reinstall or supply a
new V2 document; no compatibility conversion runs.
