# Getting started with Boatstack V2

## Install once

Run the checksum-verifying installer from the repository root:

```sh
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/operatorstack/boatstack/main/install.sh)"
boatstack doctor --repo . --format text
```

Windows users run `install.ps1` in PowerShell. The kernel creates
`.boatstack/project.json`; review and commit that file before feature work.

## Configure one exact objective

Every managed delivery has a stable objective ID, delivery ID, and terminal kind.
This example targets a verified implementation:

```sh
boatstack objective-bind --repo . \
  --objective-id search-timeout --objective-kind verified-implementation \
  --delivery search-timeout --human alice \
  --param objective_kind=verified-implementation \
  --param delivery_id=search-timeout
```

Human authority is command-scoped. Repository-policy authority is derived from
the current, independently hashed `.boatstack/project.json`.

## Enter managed scope

```sh
boatstack next --repo . --transition engagement.begin \
  --objective-id search-timeout --objective-kind verified-implementation \
  --delivery search-timeout --repository-authority --format json

boatstack apply --repo . --transition engagement.begin --flow search-timeout \
  --objective-id search-timeout --objective-kind verified-implementation \
  --delivery search-timeout --repository-authority \
  --correlation <correlation> --prescription-id <prescription-id> \
  --expected-state-revision <revision> \
  --expected-program-fingerprint <program-sha256> \
  --expected-snapshot-fingerprint <snapshot-sha256>
```

Friendly transition commands resolve and consume one exact prescription in the
same invocation. Integrations that call raw `apply` or `recover` must forward
the prescription ID, state revision, program fingerprint, snapshot fingerprint,
and correlation returned by `next` without modification.

A saved plan alone never engages Boatstack. Use `status` or `next` at any
time; both are read-only.

## Create, validate, approve, and activate the plan

```sh
boatstack plan-create --repo . \
  --objective-id search-timeout --objective-kind verified-implementation \
  --delivery search-timeout --human alice \
  --param source_path=/absolute/path/to/plan.md \
  --param delivery_id=search-timeout

boatstack plan-validate --repo . \
  --objective-id search-timeout --objective-kind verified-implementation \
  --delivery search-timeout --repository-authority
```

Read the plan fingerprint from `status --format json`, then approve those exact
bytes:

```sh
boatstack plan-approve --repo . \
  --objective-id search-timeout --objective-kind verified-implementation \
  --delivery search-timeout --human alice \
  --param plan_fingerprint=<sha256> --param actor=alice

boatstack plan-activate --repo . \
  --objective-id search-timeout --objective-kind verified-implementation \
  --delivery search-timeout --human alice
```

## Record current evidence

Build, test, and review receipts bind the current Git revision, the product
working-tree fingerprint, and exact typed evidence bytes. A gate evidence file
is strict JSON:

```json
{
  "schema_version": 1,
  "gate": "build",
  "source_revision": "<git-sha>",
  "outcome": "passed",
  "producer": "ci-or-local-runner",
  "completed_at": "2026-08-11T10:00:00Z"
}
```

Hash that file, then record it. For build and test gates, the same admitted
transition also executes the matching `project.commands` entry; only a zero exit
status installs the evidence and receipt. Command output is never persisted:

```sh
boatstack record-build --repo . --repository-authority \
  --objective-id search-timeout --objective-kind verified-implementation \
  --delivery search-timeout \
  --param source_revision="$(git rev-parse HEAD)" \
  --param evidence_path=/absolute/path/to/build-evidence.json \
  --param evidence_fingerprint=<sha256>
```

Use `record-test` and `record-review` in the same form. The configured
verified terminal is established only when build, test, and review evidence is
current for one revision. If `policy.visual_evidence` is `required`, attach an
exact manifest for that same revision with `evidence.visual.attach`,
`manifest_path`, `privacy_receipt`, and `source_revision`.

## Use a linked worktree

`workspace-cut` verifies that the base contains the current V2 configuration,
creates the worktree, transfers controller state to its exact identity, and
parks the source checkout:

```sh
boatstack workspace-cut --repo . --human alice \
  --objective-id search-timeout --objective-kind verified-implementation \
  --delivery search-timeout \
  --param branch=feature/search-timeout \
  --param base_ref=origin/main \
  --param destination=/absolute/path/to/search-timeout
```

Continue only from the returned destination. Cleanup is admitted only after
merged publication evidence or explicit abandonment; verification then returns
to the preserved source checkout.

## Publish

Publication uses `publication.preview`, `publication.execute`, and
`publication.observe`. The external execute step requires both:

- human or autonomy authority; and
- an unexpired `external-provider` authority receipt supplied with
  `--authority-receipt`.

Boatstack does not infer provider authority from `gh` being installed or
authenticated. The provider receipt fingerprint must equal the exact
`preview_fingerprint` returned by `publication.preview`; correction receipts
likewise bind the admitted body SHA-256. It never merges a pull request.

## Inspect receipts

```sh
boatstack events --repo . --format jsonl
boatstack events --repo . --follow --format jsonl
```

The stream contains allowlisted transition metadata only. It never contains
prompts, source, diffs, arbitrary command output, documents, or secrets.
