# Boatstack configuration

`.boatstack/project.json` is the repository-owned policy input. Boatstack accepts only
schema version 5. Unknown fields, unsupported policy values, duplicate
hosts or projections, trailing JSON, and missing required fields fail closed.

```json
{
  "schema_version": 5,
  "identity": {
    "default": "developer",
    "roles": {
      "developer": {
        "kind": "command",
        "command": "gh",
        "args": ["api", "user", "--jq", ".login"]
      },
      "release-manager": {"kind": "literal", "value": "release-operator"}
    }
  },
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
  "hosts": ["cli", "cursor", "codex", "claude", "gemini", "mcp", "sdk"],
  "projections": ["codex", "claude", "cursor", "gemini"]
}
```

## Required values

- `project.name`, `project.default_branch`, and `project.commands`;
- `identity.default` and a nonempty `identity.roles` object whose keys match
  `^[a-z][a-z0-9._-]*$` and are at most 128 bytes;
- `policy.plan_approval`: `human` or `human-or-autonomy`;
- `policy.visual_evidence`: `off`, `optional`, or `required`;
- at least the `cli` host.
- an explicit `projections` array; `[]` is valid, and every selected projection
  must be one of `codex`, `claude`, `cursor`, or `gemini` with its matching host
  enabled.

`hosts` controls runtime admission. `projections` controls only generated
repository files and never grants runtime authority. Projection order is
non-semantic: Boatstack sorts the IDs before computing the SHA-256 selection
fingerprint over `{"schema_version":1,"projections":[...]}`.

The only accepted external-effect authority policy is
`human-or-autonomy-plus-provider`. Provider authority is an independent
mandatory clause; it cannot be replaced by a human receipt.

## Named human identity roles

Each role tells a host how to obtain the proposed actor for a human-authority
request. `identity.default` is used only by non-Flow maintenance. A Flow selects
its own role explicitly through `human_identity`; Boatstack never substitutes
the default. Roles, actors, approvals, and provider capabilities are distinct.
A literal descriptor is:

```json
{"kind": "literal", "value": "alice"}
```

A command descriptor contains an executable name and an exact argument array:

```json
{"kind": "command", "command": "gh", "args": ["api", "user", "--jq", ".login"]}
```

Boatstack validates, fingerprints, and exposes this data but never executes the
command. The descriptor is untrusted repository data. A Flow or delegation
request does not authorize its execution. A host may submit the exact command
and arguments to its own command permission boundary, without a shell or
interpolation, and execute them only when that boundary independently permits
the action. It accepts only a zero exit status and one non-empty actor line of
at most 1 KiB after removing at most one trailing LF or CRLF. If execution is
not permitted or resolution fails, the host must ask the user for an actor; it
must not infer an operating-system or Git identity. This explicit fallback does
not replace the verified descriptor. The host retains its exact provider
fingerprint and still requires separate approval of the exact authority request.

The host displays the selected role, resolved actor, exact request, and requested authority,
then asks for explicit approval. The authorization command still requires
`--human <actor>`. The descriptor fingerprint records how the actor was
proposed. It does not prove approval, identity ownership, provider permission,
or external-provider authority. In particular, resolving an actor through
`gh` does not create a GitHub provider receipt.

When neither trusted source exists—at true bootstrap before
`installation.initialize`, or while `configuration.initialize`,
`configuration.mutate`, or
`configuration.reconcile` repairs unverified configuration—Boatstack preserves
the human authority question but omits `human_identity`. The host must ask for
an explicit actor and must not infer one. A missing identity on any other human
authority boundary is an error. Configuration mutation with verified
configuration uses the current default. Program replacement uses the persisted
role admitted with the prior program, so candidate code cannot select its own
approver. Removing that role from a candidate configuration is rejected;
changing its descriptor is allowed and invalidates prior authorization through
configuration and bundle drift.

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

## Changing hosts or projections

Host and projection selection changes use the governed configuration boundary:

1. Write a candidate schema-5 configuration with the desired `hosts` and
   `projections`.
2. Apply it through `configuration.mutate`. Boatstack installs that exact config
   and its selected maintenance projections atomically.
3. Keep product work suspended while compiling and checking every Flow against
   the new selection.
4. Run the normal installation update to admit the exact new control bundle.
   Projection-only changes do not require program-change acceptance;
   `installation.reconcile-update` is only for independent compiled-program
   drift.
5. Resume the original Flow only after configuration, maintenance manifest,
   Flow artifacts, projections, and control bundle all verify.

Retirement removes only manifest- or ownership-bound files whose current bytes
still match their recorded hashes. Modified or unrelated files fail closed and
remain present; host directories are never removed.

## Optional additive extensions

Repository configuration may enable checksum-bound subprocess extensions, but
it cannot select or replace the trusted program runtime:

```json
{
  "extensions": [
    {
      "id": "example.security",
      "version": "1.0.0",
      "executable": "/absolute/symlink-free/path/security-extension",
      "sha256": "<64 lowercase hexadecimal characters>",
      "manifest": {
        "id": "example.security",
        "version": "1.0.0",
        "protocol_version": 1,
        "settings_schema": {"type": "object", "additionalProperties": false},
        "privacy_classification": "metadata-only",
        "telemetry_classification": "transition-receipt"
      },
      "settings": {"profile": "strict"},
      "deadline_millis": 5000,
      "stdout_bytes": 1048576,
      "stderr_bytes": 65536
    }
  ]
}
```

The declarative manifest is compiled without starting the executable. Once the
exact configuration and ControlProgram binding is current, the executable is
invoked directly without a shell from a private copy of the exact bytes hashed
for that invocation. It receives only the bounded versioned JSON protocol and
fixed locale variables. Crossing either output bound cancels the subprocess
immediately and fails the operation closed. It is a trusted executable
boundary, not an OS sandbox.
Changing its set, version, executable bytes, settings, or limits changes the
ControlProgram fingerprint and therefore fails closed as program drift for an
active flow.

`project.commands` names the repository's canonical product checks. Build and
test gate transitions execute those exact repository-owned commands inside the
effect boundary and install evidence only after a zero exit status. The command
is screened by the same constitutional guard first, and its output is never
persisted. Gate authority also requires a strict revision-bound passed-evidence
document; an arbitrary fingerprint string is insufficient.

To change configuration, write a candidate file elsewhere, then request
`configuration.mutate`. The CLI derives `config_sha256` from the strict decoded
schema-5 value in canonical JSON form. Formatting, object-key order, and LF/CRLF
checkout conversion therefore retain the same authority, while any controlling
value change produces a new fingerprint. The kernel still copies the exact
candidate bytes, installs state last, re-observes the tracked file, and accepts
success only if its semantic fingerprint remains current.

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
  --objective-id bootstrap --target-id approved-plan --delivery bootstrap \
  --param topology=detached --param config_authority=external
```

Earlier configuration schemas are intentionally unsupported. Reinstall or supply a
new Boatstack document; no compatibility conversion runs.
