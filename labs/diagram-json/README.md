# Worked example: JSON output for diagrams

This is a worked demonstration of the evidence-engineered coding node. The feature is
intentionally small and uses code already in this repository:

> Add machine-readable JSON output to the diagram printer while preserving the
> current text output.

No product code is changed by this example. The named approval below is a
simulated walkthrough record, not authorization to implement or ship the
feature.

## What a developer does

First, install the Boatstack adapters in a repository and open the coding agent's
Plan mode. The developer types the product request in ordinary language:

```text
Add machine-readable JSON output to the diagram printer while preserving the
current text output.
```

The host explores that intent without implementing it and saves
[the initial Plan-mode file](source-plan.md). The active host context identifies
that plan, so the normal command needs no path:

```text
/auto-plan
```

If the host does not expose a path, Boatstack checks its bounded plan locations;
an explicit path is only the ambiguity fallback. If the file is absent or empty,
`/auto-plan` is `BLOCKED`. With the file present, the agent should inspect only:

- `src/diagram.ts` for the current contract and rendering behavior;
- `src/index.ts` for the public export boundary;
- `labs/05-diagram-printer/` for executable examples and regression output;
- `package.json` and TypeScript configs for real validation commands.

The result is a draft, not code:

- [product request](request.md)
- [source Plan-mode file](source-plan.md)
- [question and decision ledger](questions.md)
- [feature specification](spec.md)
- [canonical Markdown plan](plan.md)

## The missing human step

The agent first validates `plan.md` without creating files:

```bash
.product-loop/bin/boatstack-helper check-plan --plan plan.md
```

It then presents the draft, its fingerprint, and the three contract decisions
in `questions.md`. A representative exchange is:

```text
Agent: The draft is ready. The recommended contract is an additive serializer,
a versioned public schema, and a compact run overlay. No code has been changed.
Approve this plan or tell me what to revise.

Example Maintainer: Approve this demonstration plan.
```

Only after that explicit answer does `/plan-gate` write the Markdown
[approval receipt](approval.md). No JSON, lock, executable state, or product
code is created in Plan mode.

When the developer uses the host's normal Build transition, `/build` activates
the exact approved plan before its first product-code edit:

```bash
.product-loop/bin/boatstack-helper activate-plan \
  --plan plan.md \
  --approval approval.md \
  --out-dir compiled \
  --output plan.lock.json
```

That produces:

- [compiled task graph](compiled/tasks.json)
- [requirement-to-test matrix](compiled/test-matrix.json)
- [evidence ledger](compiled/evidence.md)
- [content-addressed plan lock](plan.lock.json)

The approval receipt is the persisted human boundary; activation turns that
approved state into deterministic machine artifacts. Editing `source-plan.md`,
`spec.md`, or any part of `plan.md` changes the fingerprint and blocks before
implementation.

```bash
.product-loop/bin/boatstack-helper activate-plan \
  --plan plan.md \
  --approval approval.md \
  --out-dir compiled \
  --output plan.lock.json
# BLOCKED: stale approval receipt
```

## What happens next in a real feature

The remaining commands consume the same canonical artifacts regardless of the
coding model or host:

1. `/build` activates and verifies the lock, implements one task at a time, and stops for any
   newly discovered product decision.
2. `/test-gate` runs the matrix and attaches command output or fixture evidence
   to each acceptance criterion.
3. `/review` examines the actual diff for compatibility, schema stability,
   unhandled failures, and evidence gaps.
4. `/ship` prepares a PR containing the spec, decisions, evidence, gaps,
   rollout, and rollback. It does not merge or deploy.
5. `/retro` classifies unexpected failures and proposes a separately evaluated
   loop improvement instead of silently rewriting the workflow.

In Cursor these appear as generated slash commands. Claude Code and Codex get
thin skill adapters, while GitHub receives a matching PR template. The workflow
itself remains in `.product-loop/`, so the contract is not hardcoded to a host.

## Why this example has immediate value

The one-sentence request is reduced to one public API boundary and five
observable acceptance criteria. The human makes the three decisions that would
otherwise be guessed; everything after approval becomes traceable and
machine-checkable. That is the useful unit: not a large process framework, but
a small feature package that can prove what was agreed, built, tested, reviewed,
and shipped.
