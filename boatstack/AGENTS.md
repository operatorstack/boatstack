# Agent guide — Boatstack runtime

Read this before opening a PR that touches anything under `boatstack/`.

## Every Boatstack PR REQUIRES a new release note

CI runs `.github/scripts/release_notes.py check-policy` in the
`repository-contract` job. It fails any PR without **adding** a new release-note
fragment. This check is **not** part of
`go test ./...`, so a green local test run does **not** mean you are done. Skipping
the note costs a full CI round trip (fail → add note → push → re-run).

### Add the note in the same commit as your change

Create one new file per PR:

```
release-notes/YYYY-MM-DD-<slug>.md
```

Contract (enforced by `validate_release_note`):

- **Name:** `YYYY-MM-DD-<slug>.md`, slug lowercase `[a-z0-9]` words joined by `-`.
- **First line:** a level-three Markdown heading — `### <Title>` (no leading blank line).
- **Body:** at least one non-empty line after the heading describing **user impact**
  (what changed for someone using Boatstack, not the code mechanics).
- **Encoding/EOL:** UTF-8, and the file must end with a trailing newline.
- **Append-only:** never edit or delete an existing note. To correct a shipped
  note, add a new correction fragment. Only added (`A`) files under
  `release-notes/` are allowed in the diff.

### Verify locally before you push — avoid the CI round trip

Commit your change **and** the note, then run the same policy CI runs:

```
# format check on the notes directory
python3 .github/scripts/release_notes.py validate --repo .

# append-only + "note present for lab changes" against origin/main (needs a clean,
# committed tree — it inspects the committed PR diff, not the working tree)
python3 .github/scripts/release_notes.py \
  preflight --repo . --base-branch main
```

`preflight` fetches `origin/main` and checks the committed diff. `PASS` means the
`repository-contract` check will pass; `BLOCKED` prints exactly what to fix.

## Other checks that are not in `go test`

- **repository-contract** and the runtime jobs (Windows/macOS/Ubuntu) run in CI.
  Locally, run `go build ./...` and `go vet ./...` from `boatstack/`. Run the
  complete Go suite from the repository root with
  `python3 .github/scripts/run_go_tests.py`; it uses a CPU-aware local worker
  count and fails unless every enumerated test belongs to exactly one passing
  shard. Use `go test -run '<focused-pattern>' ./...` only while
  iterating. Also run
  `python3 -m unittest discover -s .github/tests -p 'test_*.py'` from the
  repository root.

## PR body honesty

Do not write "no release note required" for a Boatstack change — a note is
always required. State which note you added.

## Boundary Conformance Requirement

For every requested change, determine whether it creates, modifies, relies on,
or crosses a system boundary.

A boundary is any point where authority, state, data, effects, trust, or
responsibility moves between components, actors, processes, repositories,
services, or execution stages.

If the change affects a boundary, boundary conformance is part of the definition
of done. If it does not, say so explicitly (`Boundary-conformance impact: none`)
and do not invent artificial tests.

The standing rule, in three lines:

```text
Every boundary implies a control law.
Every control law implies conformance evidence.
Every relevant path must be shown to reach the boundary.
```

### 1. State the control law

Before implementation, describe the boundary and write the invariant it must
enforce, in this form:

```text
Boundary:          <where the transition occurs>
Control law:       <what must always be true>
Authorized actor:  <who may perform the transition>
Required evidence: <what must be verified before acceptance>
Failure behavior:  <deny, reject, retry, escalate, or fail closed>
Release condition: <what makes the transition admissible>
```

### 2. Enforce at the correct boundary

Do not only patch the observed symptom. Trace the paths that can violate the
control law and enforce it at the earliest safe shared boundary that closes the
failure class without unnecessary scope expansion.

If the correct fix requires materially broader work, do not silently expand the
change. Ask whether to (1) expand the current delivery, (2) split the shared
boundary into a prerequisite delivery, or (3) apply bounded local containment
and record the remaining risk.

State the law over the invariant and its failure class — never scoped to the one
call site where you found the bug. A single shared resource is usually crossed by
several boundaries; enumerate them all and extend the existing law to cover them
rather than minting a near-duplicate for the second one. See
[docs/control-law-scoping.md](docs/control-law-scoping.md) for the method and a
worked example.

### 3. Add boundary-conformance tests

Tests must prove the control law, not merely exercise the implementation. Add
the applicable classes:

- **Positive conformance** — the authorized actor completes the valid transition
  when all required evidence is present.
- **Negative conformance** — unauthorized actors, invalid states, missing/stale
  evidence, and malformed requests are rejected.
- **Relation conformance** — every relevant entry path reaches the intended
  boundary; test `request → boundary → decision → effect → resulting state`, not
  just the component in isolation.
- **Bypass conformance** — the protected effect cannot be reached through an
  alternate path that avoids the boundary.
- **Failure-state conformance** — rejection leaves the protected state unchanged
  and does not partially apply the effect.
- **Correlation and replay** (where relevant) — request/response identities
  match; receipts cannot cross runs; duplicate/reordered events fail correctly;
  replay reproduces the original decision; changed policy or evidence invalidates
  replay.
- **Idempotency and reversal** (where relevant) — repeating an accepted
  transition does not duplicate the effect; stale base state is rejected; a
  recorded mutation reverses deterministically when its post-state still matches.

### 4. Map tests to control laws

Every boundary test must identify which control law it proves. Use a name or a
`control-law: <slug>` comment. Avoid tests that pass without demonstrating the
invariant.

### 5. Preserve deterministic authority

The model may propose implementation and tests. The deterministic boundary
decides admissibility. Never treat an LLM assertion, completion claim, or
generated summary as conformance evidence unless policy explicitly allows it.
Prefer exact hashes, repository state, test results, validated schemas,
correlated receipts, deterministic path checks, and authoritative external state.

### 6. Report completion evidence

Before declaring the work complete, report:

```text
Boundary:
Control law:
Affected paths:
Tests added:
Positive case:
Negative case:
Relation or bypass proof:
Failure-state behavior:
Residual risk:
Conformance status:
```

The change is not complete while a material boundary control law remains
untested or unsupported by evidence.
