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
- `examples/05-diagram-printer/` for executable examples and regression output;
- `package.json` and TypeScript configs for real validation commands.

The result is a draft, not code:

- [product request](request.md)
- [source Plan-mode file](source-plan.md)
- [question and decision ledger](questions.md)
- [feature specification](spec.md)
- [structured plan](plan.json)

## The missing human step

The agent presents the draft with the three contract decisions in
`questions.md`. A representative exchange is:

```text
Agent: The draft is ready. The recommended contract is an additive serializer,
a versioned public schema, and a compact run overlay. No code has been changed.
Approve this plan or tell me what to revise.

Example Maintainer: Approve this demonstration plan.
```

Only after that explicit answer does `/plan-gate` compile and lock the plan:

```bash
.product-loop/bin/boatstack-helper compile-plan \
  --plan plan.json \
  --out-dir compiled

.product-loop/bin/boatstack-helper approve-plan \
  --source-plan source-plan.md \
  --spec spec.md \
  --plan plan.json \
  --tasks compiled/tasks.json \
  --approved-by "Example Maintainer (simulated walkthrough)" \
  --output plan.lock.json
```

That produces:

- [compiled task graph](compiled/tasks.json)
- [requirement-to-test matrix](compiled/test-matrix.json)
- [evidence ledger](compiled/evidence.md)
- [content-addressed plan lock](plan.lock.json)

The lock is the deterministic boundary between agreement and implementation.
Editing `source-plan.md`, `spec.md`, `plan.json`, or the compiled task graph
makes its check fail before or during `/build`.

```bash
.product-loop/bin/boatstack-helper approve-plan \
  --source-plan source-plan.md \
  --spec spec.md \
  --plan plan.json \
  --tasks compiled/tasks.json \
  --output plan.lock.json \
  --check
```

## What happens next in a real feature

The remaining commands consume the same canonical artifacts regardless of the
coding model or host:

1. `/build` verifies the lock, implements one task at a time, and stops for any
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
