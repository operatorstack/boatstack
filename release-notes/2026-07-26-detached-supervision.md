### Attach Boatstack to a repository without adding Boatstack to it

Until now, using Boatstack meant adopting its control plane into the repository: `init`
wrote `.boatstack-project.json`, a `.product-loop/` tree, host adapter directories, and a
pull-request template into the working tree. That is the right choice for a team that owns
Boatstack, but not for a developer who wants a personal delivery supervisor on a repository
they are only evaluating, a client repository, an open-source checkout, or a large monorepo.

Boatstack now supports a second ownership mode, **Detached Supervision**. In this mode the
controller — configuration, plans, delivery state, operation and mutation receipts, evidence,
flow traces, generated references, and the runtime — lives under an external, developer-local
control root, not inside the target repository. The repository stays free of Boatstack-owned
files; the supervisor still changes it only through the same approved product and delivery
actuators.

Three new commands manage an attachment. `boatstack-helper attach --repo . --mode detached`
inspects the repository, detects its test command, and writes the controller state and a
binding to the external control root, leaving the working tree and `.git` byte-for-byte
unchanged. `detached-status` reports whether a repository is attached and whether its binding
verifies. `detach` removes the attachment, and its state unless you pass `--preserve-state`.
Use `--state-root` to point at a specific control root; otherwise Boatstack uses the standard
per-OS user state directory.

The layout is host-neutral: it works the same for every supported coding agent. Every
repository is bound by a stable identity derived from its origin and history, so one
repository's controller state can never be applied to another, and two worktrees keep
isolated mutable state. If a bound repository's identity no longer matches — a corrupt or
mismatched binding — Boatstack fails closed rather than silently rebinding. The safety guard
now protects the external control root from direct model mutation exactly as it protects the
embedded runtime state.

Existing embedded installations are unchanged: every controller path now flows through one
resolver that returns today's exact locations in embedded mode. This release delivers the
attach/detach lifecycle and the detached control plane; host activation for a running coding
session is delivered separately.
