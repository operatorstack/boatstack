<p align="center">
  <img src="./assets/boatstack-mark.svg" width="88" alt="Boatstack logo">
</p>

<h1 align="center">Boatstack</h1>

<p align="center">
  Programmable supervisory control for state-changing operators.
</p>

<p align="center">
  <strong>Alpha · active development · expect breaking changes</strong>
</p>

Boatstack is a controller over discrete, named state transitions. A model,
human, service, workflow, or deterministic program may propose an operation;
Boatstack decides whether the exact transition is currently admissible.

```text
Operator proposes.
Boatstack admits.
The effect executes.
Boatstack verifies and commits.
```

Coding agents are one operator type. Prompts, tools, and agent sessions are not
the defining abstraction of the kernel.

> [!WARNING]
> Boatstack is alpha software. Its CLI, Control Program ABI, configuration,
> generated projections, and persisted formats may change without a
> compatibility path. Audit it before using it on important work.

## The supervisory loop

```text
objective + supervisory state + observation + authority
                         ↓
                canonical relation
                         ↓
                     decision
                         ↓
                   prescription
                         ↓
                 fresh admission
                         ↓
                 operator → effect
                         ↓
               fresh verification
                         ↓
                  state + receipt
```

A proposal never becomes an effect directly. Resolution selects an admissible
transition and produces a content-bound prescription. Apply rechecks the same
state, program, observation, objective, and authority under the instance lock.
Only a verified result may commit durable supervisory state and a receipt.
Interrupted or uncertain effects enter explicit recovery instead of being
silently treated as committed.

## Core concepts

- A **Control Program** is one complete executable control law. A **Flow** is
  the product-facing name for a complete Control Program authored for a domain.
- An **entry** selects a target and inputs. A **target** is a marked predicate
  defining completion for that invocation.
- **Supervisory state** is the small durable state owned by the kernel. Domain
  state is observed through a domain port and is not embedded in generic state.
- A **transition** is a candidate state relation. An **operator** realizes one
  admitted operation. An **effect** is its bounded consequence.
- **Authority** is trusted evidence permitting admission. A **capability** is
  the permission that authority exposes at an enforceable boundary.
- A **receipt** is an immutable fact emitted only after successful verification
  and atomic commit. It is evidence, not authority.

See the [glossary](docs/glossary.md) and [concepts](docs/concepts/index.md) for
the canonical terminology.

## Ownership boundaries

The general kernel owns program and instance identity, objective binding,
freshness, the canonical relation, capability admission, verification, durable
revision, receipts, marked modes, and recovery state. A Control Program owns
its transitions, targets, entries, invocation contracts, authority
requirements, and recovery mappings. A domain owns observations,
domain-specific admissibility, operators, effects, and postconditions.

The TypeScript SDK is a restricted authoring frontend. It produces declarative
Control Program IR; runtime commands load checked canonical artifacts rather
than executing repository TypeScript.

## Architecture at a glance

```text
┌──────────────────────────────────────────────────────────────┐
│ Host surfaces                                                │
│ CLI · RPC · MCP · SDK · generated agent projections          │
└──────────────────────────────┬───────────────────────────────┘
                               │ versioned request
┌──────────────────────────────▼───────────────────────────────┐
│ General kernel                                               │
│ observe → relate → prescribe → admit                         │
│ persist attempt → execute → verify → commit state + receipt  │
└───────────────┬──────────────────────────────┬───────────────┘
                │                              │
┌───────────────▼──────────────┐  ┌────────────▼───────────────┐
│ Control Program / Flow       │  │ Domain                     │
│ transitions · objectives    │  │ observations · operators   │
│ authority · marked targets  │  │ effects · verification     │
└──────────────────────────────┘  └────────────────────────────┘
```

```text
Flow TypeScript
      ↓ restricted frontend
raw Control Program IR
      ↓ validate + canonicalize + fingerprint
committed canonical artifact
      ├──→ runtime control bundle
      └──→ Claude · Codex · Cursor · Gemini projections
```

The first diagram shows runtime ownership; the second shows the authoring and
projection boundary. See [Current architecture](docs/architecture/index.md)
for the implementation-level map.

## Shipped boundaries

### Kernel

The domain-neutral kernel owns the canonical relation, freshness, admission,
prescriptions, verification, atomic control-state commit, receipts, recovery,
and conformance. Untargeted resolution selects
only a transition that advances the configured objective.

### Software delivery

The software-delivery domain distinguishes human, autonomy,
repository-policy, and external-provider authority. A program declares a
maximum capability surface; runtime admission still requires external
authority. Exact idempotent replay is a domain transaction behavior, not a
generic-kernel promise.

### Developer surfaces

CLI, RPC, MCP, the Go SDK, and generated projections carry the same complete
prescription. The installer generates the maintenance skill
`$boatstack-update`. A repository Flow declares its own entries. Boatstack does
not interpret the word `run`.

```sh
# Read-only controller and catalog views.
boatstack status --repo . --format json
boatstack catalog --format json

# Low-level integrations apply one previously resolved prescription.
boatstack apply --repo . --transition <stable-id> \
  --prescription-id <id> --expected-state-revision <revision> \
  --expected-program-fingerprint <sha256> \
  --expected-snapshot-fingerprint <sha256> --format json
```

## Software delivery

Software delivery is the current domain implementation. It adds repository and
Git observation, plans, worktrees, gates, evidence, publication, provider
authority, durable domain state, and reconciliation. Repository authors choose
which trusted operations belong to their Flow; package names and the built-in
lifecycle are not part of the general kernel model.

```ts
import { defineFlow, entry, fact, marked } from "@operatorstack/boatstack";
import {
  softwareDelivery,
  trustedDelegation,
} from "@operatorstack/boatstack-software-delivery";

export default defineFlow(softwareDelivery({
  id: "product-delivery",
  version: "1",
  humanIdentity: "developer",
  lifecycle: [{ id: "plan.activate", priority: 50 }],
  targets: [marked("active", fact("plan", ["active"]))],
  entries: [entry({
    id: "run",
    target: "active",
    requires: { authorities: ["human"] },
    delegation: trustedDelegation("autonomy"),
  })],
}));
```

## Documentation

- [Documentation map](docs/index.md)
- [Concepts](docs/concepts/index.md)
- [Current architecture](docs/architecture/index.md)
- [Product Delivery authoring](docs/product-delivery/index.md)
- [TypeScript SDK documentation](docs/typescript/index.md)
- [Getting started](docs/getting-started.md)
- [Configuration reference](docs/configuration.md)
- [Safety boundaries](docs/safety.md)

Exact commands, schemas, paths, and generated-file layouts live in reference
documents. Generated catalogs remain machine-owned. The
[history policy](docs/history/index.md) explains why retired V1 specifications
are not retained and why historical material cannot define current behavior.

## Repository map

```text
boatstack/kernel/                    domain-neutral supervisor
boatstack/controlprogram/            IR canonicalization and compilation
boatstack/invocation/                invocation materialization and suspension
boatstack/internal/softwaredelivery/ software-delivery runtime
packages/boatstack/                  domain-neutral TypeScript authoring SDK
packages/boatstack-software-delivery/ software-delivery authoring bindings
docs/                                concepts, architecture, guides, reference
```

## Develop

Read [the Boatstack contributor guide](boatstack/AGENTS.md) before changing the
runtime. The repository requires boundary-conformance evidence and an
append-only release note for every Boatstack pull request.

```sh
npm ci
npm run test:flow-sdk
npm run docs:check
python3 -m unittest discover -s .github/tests -p 'test_*.py' -v
python3 .github/scripts/run_go_tests.py
```

## License

[MIT](LICENSE)
