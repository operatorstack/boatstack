<p align="center">
  <img src="./assets/boatstack-mark.svg" width="88" alt="Boatstack logo">
</p>

<h1 align="center">Boatstack</h1>

<p align="center">
  Programmable supervisory control for software delivery.
</p>

<p align="center">
  <strong>Alpha · active development · expect breaking changes</strong>
</p>

Boatstack is a programmable supervisory runtime over state-changing operators.
Software delivery is its first production domain: an agent can propose what
happens next, while a deterministic kernel decides what is admissible, executes
registered effects, verifies fresh evidence, and records a durable receipt.

```text
agent intent
    ↓
read-only observation → canonical snapshot → deterministic resolution
                                              ↓
durable receipt ← postcondition verification ← admitted effect
```

> [!WARNING]
> Boatstack is alpha software for experimentation. The CLI, Control Program
> ABI, configuration schema, generated skills, and state format may change
> without a compatibility path. Audit it before using it on important work.

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
`.boatstack/` files and host skills before starting delivery.

Then use one of the exactly three operation skills from a supported coding
agent:

```text
$boatstack-autoplan  # create, validate, and approve a plan
$boatstack-run       # deliver to an open or updated pull request; never merge
$boatstack-update    # install a checksum-verified runtime update
```

If the agent was already running during installation, start a fresh task so it
can discover the generated skills. See [Getting started](docs/getting-started.md)
for the lower-level CLI path and [Configuration](docs/configuration.md) for the
repository policy schema.

## What ships today

Boatstack currently compiles 63 registered transitions into one executable
control graph. The complete list is generated from the registry in the
[transition catalog](docs/architecture/boatstack-v2-transition-catalog.md).

| Surface | Shipped functionality |
| --- | --- |
| **General kernel** | Domain-neutral programs, control instances, objective bindings, observations, capabilities, operators, verification, recovery, marked states, and receipts. The kernel does not require Git or a coding agent. |
| **Control Programs** | One immutable program fingerprint binds executable semantics, objective contracts, resource ownership, capabilities, and transition IDs. |
| **StandardFlow** | A first-party product-delivery Flow covering installation, repository attachment, configuration, objectives, planning, worktrees, build/test/review evidence, publication, cleanup, and recovery. |
| **Deterministic supervisor** | Targeted and untargeted resolution, explicit accepted objectives, transition priorities, prerequisite selection, and typed `PRESCRIBED`, `CANDIDATE`, `FRONTIER`, `BLOCKED`, `REFUSED`, `UNRESOLVED`, and `TERMINAL` decisions. |
| **Authority and capabilities** | Separate human, autonomy, repository-policy, and external-provider receipts. Programs declare a maximum capability surface but cannot grant themselves authority. |
| **Transactional effects** | Prescriptions bind the exact state revision, program fingerprint, snapshot fingerprint, transition, and correlation. Apply rechecks that compare-and-swap boundary under a repository lock before any managed effect. |
| **Durable state ownership** | Installation, program, control, and product state are separate facets. A transition fails closed if it attempts to mutate a facet outside its Kernel-owned policy. |
| **Verification and receipts** | Fresh postcondition checks, revision-bound build/test/review evidence, immutable transition facts, idempotent replay, and privacy-safe JSONL event projections. |
| **Recovery** | Restart-safe journals, reversible local mutations, explicit resume/rollback/escalation paths, and preserved unknown settlement for external effects. |
| **Repository topology** | Embedded, detached, and linked-worktree identity; verified state transfer when a workspace is cut; cleanup only after proved landing or explicit abandonment. |
| **Publication** | Preview, provider-authorized execution, observation, correction, and reconciliation. Boatstack does not infer provider authority from `gh` authentication and never grants merge authority. |
| **Runtime updates** | Per-repository immutable runtime pins, checksum verification, atomic program-drift reconciliation, rollback, and multiple repository versions in one host store. |
| **Safety guard** | One command-intent classifier for supported hosts. High-confidence destructive commands are denied and managed effects are routed through Kernel admission. |
| **Integrations** | One versioned protocol shared by the CLI, RPC, MCP, Go SDK, Cursor, Codex, Claude Code, and Gemini CLI. Hosts do not maintain independent delivery state machines. |
| **Extensions** | Additive, checksum-bound subprocess extensions with declarative manifests, JSON-schema settings, bounded I/O, deadlines, capability checks, and exact-byte execution. |
| **Analysis** | Passive retrospective analysis, generated Markdown and Mermaid catalogs, privacy-safe events, and checked Locus safety/liveness models. Formal whole-system claims remain advisory. |

## The control loop

The public protocol is deliberately small:

```sh
# Observe or resolve. These commands do not mutate managed state.
boatstack status --repo . --format json
boatstack next --repo . --objective-id <objective> --objective-kind <kind> \
  --delivery <delivery> --format json

# Inspect the exact program and transition surface.
boatstack doctor --repo . --format text
boatstack catalog --format json
boatstack events --repo . --format jsonl

# Low-level integrations forward the complete prescription unchanged.
boatstack apply --repo . --transition <stable-id> --flow <flow> \
  --prescription-id <id> --expected-state-revision <revision> \
  --expected-program-fingerprint <sha256> \
  --expected-snapshot-fingerprint <sha256> --format json
```

`status`, `next`, `doctor`, `catalog`, and `events` are read-only. Friendly
commands such as `plan-create`, `workspace-cut`, `record-test`, and `publish-pr`
resolve and consume one exact prescription in the same invocation.

Untargeted resolution selects
only a transition that advances the configured objective. Maintenance, correction,
abandonment, provider actions, and merge authority are never invented as a way
around a frontier. After an operation is selected, generated host drivers keep
one command-scoped objective, repository, worktree, flow, actor, and authority
context through every resolution, effect, recovery, and re-resolution.

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
│ observe → relate → prescribe → admit → execute → verify     │
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

The ergonomic repository Flow authoring experience—project layout, authoring
tools, examples, diagnostics, and a complete guide—is still under active
development. StandardFlow remains the only first-party Flow. The ABI and SDK
are useful for exploring the model today, but they are not stable APIs yet.

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

Start with the [architecture specification](docs/architecture/boatstack-v2-kernel.md)
for the full internal model. The generated [StandardFlow graph](docs/architecture/boatstack-standard-flow.mmd)
and [Mermaid catalog](docs/architecture/boatstack-v2-transition-catalog.mmd)
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

## Status

Boatstack is being built in public and is not ready to promise compatibility.
The project is currently focused on making repository-authored Control Programs
safe to load and execute without moving authority out of the Kernel.

Issues and design feedback are welcome. Production stability, polished Flow
authoring, and compatibility guarantees are not here yet.

## License

[MIT](LICENSE)
