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

Boatstack is a programmable supervisory runtime over state-changing operators.
An agent, human, workflow, or service can propose what should happen next.
Boatstack owns the control law: it decides what is admissible, checks authority,
executes registered effects, verifies the resulting state, and commits
supervisory state with a durable receipt.

```text
Operator proposes.
Boatstack admits.
The effect executes.
Boatstack verifies and commits.
```

Coding agents are one operator type. The kernel is not built around prompts,
language models, or coding-agent semantics.

> [!WARNING]
> Boatstack is alpha software for experimentation. The CLI, Control Program
> ABI, configuration schema, generated host projections, and state format may change
> without a compatibility path. Audit it before using it on important work.

During alpha development, Boatstack does not preserve backward compatibility.
Breaking architecture changes update all in-tree consumers together instead of
adding compatibility shims. Existing local installations may need to be reset
or regenerated.

## Try it

Boatstack installs into an existing Git repository. The repository must have an
attached branch and at least one commit. macOS, Linux, and Windows binaries are
published with checksum sidecars.

macOS or Linux:

```sh
cd your-repository
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/operatorstack/boatstack/main/install.sh)"
boatstack doctor --repo . --format text
```

Windows PowerShell:

```powershell
cd your-repository
irm https://raw.githubusercontent.com/operatorstack/boatstack/main/install.ps1 | iex
boatstack doctor --repo . --format text
```

The installer verifies the latest release, pins that exact runtime to the
repository, creates the initial configuration, and generates integrations for
the enabled coding-agent hosts. Review and commit the generated
`.boatstack/` files and host projections before starting delivery.

Boatstack keeps runtime maintenance separate from repository delivery. The
installer generates the maintenance skill:

```text
$boatstack-update    # install a checksum-verified runtime update
```

A repository Flow declares its own entries. `boatstack flow compile` projects
those entries into host-native projections such as `$product-delivery-run`; Boatstack does
not interpret the word `run`. Compilation requires an explicitly selected,
absolute frontend path and never executes an automatically discovered
repository binary.

If the agent was already running during installation, start a fresh task so it
can discover the generated host projections. See [Getting started](docs/getting-started.md)
for the lower-level CLI path and [Configuration](docs/configuration.md) for the
repository policy schema.

## What Boatstack controls

| Concept | Meaning |
| --- | --- |
| **Control Program** | The complete executable control law: transitions, gates, authority requirements, recovery paths, objective rules, and marked states. The product-facing name for one complete program is a **Flow**. |
| **Supervisory state** | The small durable state owned by the kernel: program identity, control mode, exact objective binding, revision, and recovery state. Domain state stays outside the kernel. |
| **Objective** | External intent identified by an exact immutable revision and fingerprint. Changing intent requires an explicit control transition. |
| **Operator** | The component that performs one admitted operation. It may be an agent, human-mediated command, workflow, service, or deterministic program. |
| **Effect** | A registered state-changing operation with explicit capabilities and owned facets. |
| **Evidence** | Fresh observation and verification facts used to decide whether a transition may commit. |

## Software delivery is the first domain

Boatstack is not a generic platform for every agent system. Its first concrete
domain is software delivery, where StandardFlow governs repositories, plans,
worktrees, tests, reviews, publication, updates, and recovery. Git and
coding-agent concepts live in this domain layer, not in the general kernel.

## What ships today

Boatstack currently compiles 63 registered transitions into one executable
control graph. The complete list is generated from the registry in the
[transition catalog](docs/architecture/boatstack-transition-catalog.md).

### Kernel

| Surface | Shipped functionality |
| --- | --- |
| **Programs and relation** | Domain-neutral programs, control instances, objective bindings, observations, operators, marked states, targeted and untargeted resolution, priorities, and prerequisite selection. One immutable fingerprint binds each program's executable semantics. |
| **Admission and authority** | Capability-bearing authority receipts are fingerprinted, time-valid, and projected into admission. Required capabilities combine program declarations with a trusted mechanism classifier. |
| **Transactions** | Prescriptions bind the exact control instance, state revision, program, objective binding, observation, transition, and authority. Apply rechecks that boundary before execution. |
| **Verification and receipts** | Fresh postcondition verification, atomic state-and-receipt commits, and immutable transition facts. |
| **Recovery** | A durable effect attempt precedes execution. Interrupted or uncertain outcomes enter explicit recovery instead of blindly repeating an effect. |
| **Control debugging** | Read-only decision traces explain why a transition was selected, rejected, blocked, ambiguous, or waiting on authority without reconstructing lifecycle logic in the host. |
| **Conformance** | A reusable, domain-neutral suite verifies objective handling, authority, freshness, recovery, atomic commit, replay isolation, concurrency, and marked-state reachability against any explicitly mapped domain fixture. |

### Software delivery

| Surface | Shipped functionality |
| --- | --- |
| **StandardFlow** | A first-party product-delivery Flow covering installation, repository attachment, configuration, objectives, planning, worktrees, build/test/review evidence, publication, cleanup, and recovery. |
| **Delivery authority** | Separate human, autonomy, repository-policy, and external-provider receipts. Delivery programs declare a maximum capability surface but cannot grant themselves authority. |
| **Delivery transactions** | Idempotent replay of committed transition receipts, with recovery required when the transaction state is not settled. |
| **Repository topology** | Embedded, detached, and linked-worktree identity; verified state transfer when a workspace is cut; cleanup only after proved landing or explicit abandonment. |
| **Publication** | Preview, provider-authorized execution, observation, correction, and reconciliation. Boatstack does not infer provider authority from `gh` authentication and never grants merge authority. |
| **Runtime updates** | Per-repository immutable runtime pins, checksum verification, atomic program-drift reconciliation, rollback, and multiple repository versions in one host store. |
| **Safety guard** | One command-intent classifier for supported hosts. High-confidence destructive commands are denied and managed effects are routed through kernel admission. |

### Developer surfaces

| Surface | Shipped functionality |
| --- | --- |
| **Protocol and SDK** | One versioned protocol shared by the CLI, RPC, MCP, Go SDK, Cursor, Codex, Claude Code, and Gemini CLI. Hosts do not maintain independent delivery state machines. |
| **Flow IR and TypeScript frontend** | A domain-neutral, canonical Control Program IR plus `@operatorstack/boatstack`. Trusted software-delivery bindings live in the separate `@operatorstack/boatstack-software-delivery` package. |
| **Extensions** | Additive, checksum-bound subprocess extensions with declarative manifests, JSON-schema settings, bounded I/O, deadlines, capability checks, and exact-byte execution. |
| **Analysis** | Passive retrospective analysis, generated Markdown and Mermaid catalogs, privacy-safe events, and checked Locus safety/liveness models. Formal whole-system claims remain advisory. |

## The control loop

The public protocol is deliberately small:

```sh
# Observe or resolve. These commands do not mutate managed state.
boatstack status --repo . --format json
boatstack next --repo . --objective-id <objective> --target-id <kind> \
  --delivery <delivery> --format json

# Resolve one repository-owned entry.
boatstack next --repo . --flow product-delivery --entry run --format json

# Explain the current decision without executing an effect.
boatstack explain --repo . --flow product-delivery --entry run

# Inspect the exact program and transition surface.
boatstack doctor --repo . --format text
boatstack catalog --format json
boatstack events --repo . --format jsonl

# Low-level integrations forward the complete prescription unchanged.
boatstack apply --repo . --transition <stable-id> --run-id <run> \
  --flow <program> --entry <entry> \
  --prescription-id <id> --expected-state-revision <revision> \
  --expected-program-fingerprint <sha256> \
  --expected-snapshot-fingerprint <sha256> --format json
```

`status`, `next`, `explain`, `doctor`, `catalog`, and `events` are read-only.
The three Flow surfaces answer different questions: `flow check` verifies that
the artifact is a valid executable Control Program; `next` and `flow run`
resolve or execute the controller; `explain` reports why the current controller
decision occurred. It does not grant authority or recommend a fix. Friendly
commands such as `plan-create`, `workspace-cut`, `record-test`, and `publish-pr`
resolve and consume one exact prescription in the same invocation.

Untargeted resolution selects
only a transition that advances the configured objective. Maintenance,
correction, abandonment, provider actions, and merge authority are never
invented as a way around a frontier. After an operation is
selected, generated host drivers keep one command-scoped objective, repository,
worktree, program, entry, run, actor, and authority context through every resolution, effect,
recovery, and re-resolution.

## Objectives and control state

Objectives are external intent. Boatstack stores only an exact objective
binding—identity, revision, and fingerprint—in supervisory state. A new prompt
or command cannot silently reinterpret that state: binding, replacing, or
clearing an objective is an explicit transition governed by the active Control
Program.

Domain state remains outside the kernel. The kernel retains only the minimum
state needed to control progress: program identity, control mode, objective
binding, revision, and recovery obligation.

## Internals

Boatstack separates inference, control, execution, and verification:

```text
┌──────────────────────────────────────────────────────────────┐
│ Host surface                                                │
│ CLI · RPC · MCP · SDK · coding-agent skills                 │
└──────────────────────────────┬───────────────────────────────┘
                               │ versioned request
┌──────────────────────────────▼───────────────────────────────┐
│ General kernel                                              │
│ observe → relate → prescribe → admit                        │
│ persist attempt → execute → verify → commit state + receipt │
└───────────────┬──────────────────────────────┬───────────────┘
                │                              │
┌───────────────▼──────────────┐  ┌────────────▼───────────────┐
│ Control Program             │  │ Software-delivery domain   │
│ transitions · objectives    │  │ Git · files · processes    │
│ laws · marked states        │  │ plans · tests · PRs        │
└──────────────────────────────┘  └────────────────────────────┘
```

The kernel owns mechanism. A Control Program owns policy. The product calls a
complete Control Program a **Flow**; the rules encoded by it are its **control
law**. See the [general kernel boundary](docs/architecture/general-supervisory-kernel.md).

Boatstack deliberately uses ordinary systems primitives for runtime pinning,
transactions, capabilities, versioning, and recovery. The distinguishing
boundary is supervisory: operators may perform work, while a deterministic
Control Program governs which observed state transitions may commit.

The current authoring boundary already includes:

- a strict JSON [Control Program ABI](docs/architecture/control-program-abi.md);
- the domain-neutral Go runtime in `boatstack/kernel`;
- software-delivery contracts in `boatstack/delivery`;
- `sdk.New(...)` for StandardFlow and `sdk.NewProgramClient(...)` for an explicit
  trusted Program Runtime;
- canonical program identity and runtime compatibility checks;
- one kernel Program fingerprint binding the complete software-domain ABI;
- one transition relation and freshness envelope shared by the generic runtime
  and the software-delivery adapter;
- program-qualified transitions, objective contracts, resource ownership,
  capabilities, effects, verifiers, recovery, and context predicates;
- a protocol execution boundary for repository-authored transitions.

Repository Flows are authored in `.boatstack/flows/*.flow.ts` and compiled into
committed `.flow.ir.json` artifacts. Runtime commands load only canonical IR.
Compilation parses a restricted declaration subset and never executes
repository modules. Local Flow imports fail closed.
The TypeScript SDK and IR remain alpha APIs.

## Safety model

Boatstack treats `absent`, `unknown`, `stale`, `ambiguous`, and `conflicting` as
different evidence states. None grants permission to publish, delete,
overwrite, approve, or advance.

- A prescription carries no authority.
- Authority is typed, scoped, expiring, and checked separately at admission.
- Drift before apply produces zero managed effects and requires re-resolution.
- Local effects stage reversible resources and install authoritative state last.
- External outcomes can remain unknown; they are observed or reconciled, never
  blindly retried.
- Command guards are defense in depth, not a sandbox.

Read [Safety](docs/safety.md), the
[prescription transaction boundary](docs/architecture/prescription-transactions.md),
and the [capability and authority boundary](docs/architecture/capability-authority-boundary.md)
for the exact contracts.

## Repository map

```text
boatstack/kernel/           Domain-neutral supervisory runtime
boatstack/delivery/         Software-delivery program contracts
boatstack/core/             Software-delivery operational transitions
boatstack/flow/standard/    First-party StandardFlow
boatstack/internal/softwaredelivery/
                            repository model, observation, effects, and recovery
boatstack/internal/runtime/ immutable runtime selection and dispatch
boatstack/sdk/              public Go protocol client
docs/architecture/          executable contracts and generated evidence
```

Start with the [architecture specification](docs/architecture/boatstack-kernel.md)
for the full internal model. The generated [StandardFlow graph](docs/architecture/boatstack-standard-flow.mmd)
and [Mermaid catalog](docs/architecture/boatstack-transition-catalog.mmd)
come from the same executable registry used at runtime.

## Develop

Boatstack is written in Go. Python tests enforce repository, release, generated
artifact, and host-projection contracts.

```sh
python3 .github/scripts/run_go_tests.py
python3 -m unittest discover -s .github/tests -p 'test_*.py'

cd boatstack
go test -race ./...
go vet ./...
go build ./...
```

Every pull request that changes Boatstack adds an append-only release note. See
[CONTRIBUTING.md](CONTRIBUTING.md).

### TypeScript SDK documentation

The [TypeScript Flow authoring reference](https://operatorstack.github.io/boatstack/)
documents both `@operatorstack/boatstack` and
`@operatorstack/boatstack-software-delivery`. Build the same site locally with
`npm run docs:build`, then open `build/docs/html/index.html`.

## Status

Boatstack is being built in public and is not ready to promise compatibility.
The project is currently focused on making repository-authored Control Programs
safe to load and execute without moving authority out of the kernel.

Issues and design feedback are welcome. Production stability, polished Flow
authoring, and compatibility guarantees are not here yet.

## License

[MIT](LICENSE)
