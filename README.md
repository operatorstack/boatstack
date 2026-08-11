# Boatstack

Boatstack is a programmable supervisory control runtime for software delivery,
with a first-party standard delivery flow. It compiles one CoreSystem, one
explicit primary flow, and optional conservative extensions into an immutable
ControlProgram before it observes a repository, resolves one legal transition,
binds exact authority, executes owned effects, verifies the result, and records
a receipt.

Cursor, Codex, Claude Code, Gemini CLI, MCP, the CLI, and the Go SDK use the same
versioned protocol. They do not keep separate workflow state machines.

> V2 is a flag-day replacement. It does not read or migrate V1 machine state,
> commands, internal APIs, caches, leases, or detached bindings. Reinstall or
> reattach a repository.

## Why V2

V1 repeatedly reconstructed lifecycle, identity, publication, runtime, and
recovery state in different commands. V2 replaces that distributed authority:

```text
explicit invocation
  -> read-only observation
  -> canonical snapshot
  -> deterministic supervisor
  -> exact admission
  -> registered effect
  -> fresh observation and postcondition
  -> immutable receipt
```

The immediate value is simple: every host consumes one executable delivery law.
The [technical specification](docs/architecture/boatstack-v2-kernel.md) records
the complete contract and the historical failure synthesis.

## Install

On macOS or Linux:

```sh
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/operatorstack/boatstack/main/install.sh)"
```

On Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/operatorstack/boatstack/main/install.ps1 | iex
```

The installer verifies the release checksum, runs the registered
`installation.initialize` transition, and then installs the launcher. Review
and commit `.boatstack/project.json`, `.boatstack/host-skills.json`, and the
generated host skills; machine-local controller state stays outside the
worktree.

Run the independent health query:

```sh
boatstack doctor --repo . --format text
```

See [Getting started](docs/getting-started.md) and
[Configuration](docs/configuration.md) for the first delivery.

## Product surface

```sh
boatstack status --repo . --format json
boatstack catalog --format json
boatstack apply --repo . --transition <stable-id> --format json
```

- `status`, `next`, `doctor`, `catalog`, and `events` are read-only.
- `apply` and `recover` request stable transition IDs from the 62-event
  executable catalog.
- Friendly aliases such as `plan-create`, `plan-approve`,
  `workspace-cut`, `record-test`, and `publish-pr` map to those IDs.
- `guard` is the shared safety-hook query. It blocks high-confidence
  destruction and routes active managed effects through admission.
- `rpc` is the strict JSON boundary for hooks, MCP, and host adapters.
- `sdk` is the public Go protocol client.
- `retro` is passive analysis. It cannot decide lifecycle or write managed
  delivery state.
- Visual capture is normalized to `evidence.visual.attach`; the independent V1
  capture and insight writers are removed.

The generated [transition catalog](docs/architecture/boatstack-v2-transition-catalog.md)
and [Mermaid inventory](docs/architecture/boatstack-v2-transition-catalog.mmd)
come directly from the runtime registry. The generated
[StandardFlow graph](docs/architecture/boatstack-standard-flow.mmd) filters the
same compiled registry by primary-flow origin; it is not a second graph.
The [replacement closure report](docs/architecture/boatstack-v2-closure-report.md)
records the deleted V1 authority and its V2 evidence.

The Go SDK keeps the standard distribution ergonomic with `sdk.New(...)`.
Custom applications use `sdk.NewKernel(..., sdk.WithFlow(flow),
sdk.WithExtension(extension))`; the lower-level constructor requires an
explicit trusted in-process primary flow and never inserts StandardFlow.

## Coding-agent skills

Boatstack exposes exactly three operation skills on every supported interactive
coding host:

- `boatstack-autoplan` reaches an approved plan.
- `boatstack-run` drives delivery through a normal PR open or update and never
  grants merge authority.
- `boatstack-update` runs the checksum-verified installation update path.

Codex and compatible Agent Skills hosts show `$boatstack-autoplan`,
`$boatstack-run`, and `$boatstack-update`. Claude Code and Gemini CLI receive
the same three skill identities. Cursor receives the same three slash commands.
The initializer and updater project them from one canonical driver, so a host
cannot acquire a separate workflow state machine.

The initial `status` read is observation only. After an operation is selected,
the host driver keeps one command-scoped goal, flow, worktree, actor, and authority
context through resolution and effects. An authority-free diagnostic frontier
cannot end an otherwise authorized invocation. Untargeted resolution selects
only a transition that advances the configured goal; maintenance, correction,
abandonment, and caller-defined markers require separate explicit intent.
The host never derives slice order from plan prose or provider authority from an
authenticated GitHub session.

## Safety

Unknown, absent, stale, ambiguous, and conflicting evidence are different
states. None grants permission to delete, publish, overwrite, or advance.
External publication requires human or autonomy authority **and** a current
provider receipt. Cleanup requires proved landing or explicit abandonment.

Boatstack never grants merge authority. See [Safety](docs/safety.md).

## Develop

```sh
python3 .github/scripts/run_go_tests.py
python3 -m unittest discover -s .github/tests -p 'test_*.py'
cd boatstack
go test -race ./...
go vet ./...
```

Every pull request adds one release note. See [CONTRIBUTING.md](CONTRIBUTING.md).
