<!-- Generated from operatorstack/intelligence-flow. Edit the upstream product-loop source, not this file. -->

<p align="center">
  <img src="assets/boatstack-mark.svg" width="96" height="96" alt="Boatstack stacked-node B mark">
</p>

<h1 align="center">Boatstack</h1>

<p align="center"><strong>Build freely. Prove it. Ship.</strong></p>

Boatstack is **evidence-engineered coding**: a model-neutral coding node that turns product intent and repository context into an explicitly approved, tested, reviewable change. It does not prescribe the model, implementation technique, tools, or document structure. It governs what may be claimed, approved, or shipped. Its behavior is generated from [Intelligence Flow at `29f36332e8e249528c6b088473d65e1190ff00b8`](https://github.com/operatorstack/intelligence-flow/tree/29f36332e8e249528c6b088473d65e1190ff00b8/examples/12-product-engineering-loop).

> **You are free in how you build. Only claims of completion require evidence.**

It is not a claim that a longer prompt writes better code. Here is what the node actually makes observable.

## Install in a repository

Install Boatstack on a clean infrastructure branch and merge that PR before starting product work. This keeps the one-time host adapters and repository policy separate from every feature diff.

macOS or Linux:

```bash
git switch -c chore/install-boatstack
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/operatorstack/boatstack/main/install.sh)"
```

Windows PowerShell:

```powershell
git switch -c chore/install-boatstack
irm https://raw.githubusercontent.com/operatorstack/boatstack/main/install.ps1 | iex
```

The installer previews the generated paths, verifies the platform helper, asks about optional gstack and Spec Kit integrations, runs a smoke check, and prints the exact infrastructure commit commands. Boatstack core requires no Python, Node, Go, or package manager. The helper is repository-local and ignored; the adapters and policy are committed.

**New here?** [Install and ship your first feature](docs/getting-started.md) · [Understand generated files](docs/generated-files.md) · [Troubleshoot](docs/troubleshooting.md) · [See the real account-recovery walkthrough](docs/account-recovery-walkthrough.md)

**Go deeper:** [Validation and evidence](docs/validation-and-evidence.md) · [gstack and Spec Kit](#use-boatstack-with-gstack-and-github-spec-kit) · [Evidence-engineered coding](docs/evidence-engineered-coding.md)

```text
idea -> Plan mode -> /auto-plan -> questions -> /plan-gate
     -> approve -> Build -> /build -> /test-gate
     -> /review-gate -> /ship-gate -> PR
```

## Plan first, then auto-plan

Start with ordinary product intent **inside Cursor, Codex, or Claude Plan mode**:

```text
Add machine-readable JSON output to the diagram printer while preserving the current text output.
```

Save the host's plan. When the host exposes the active plan path, Boatstack reads it from conversation context; otherwise save it under `.product-loop/intake/`. Then run:

```text
/auto-plan
```

`/auto-plan` must validate a real, non-empty source plan before it reads repository context. Its fallback searches only bounded Plan-mode locations and succeeds for exactly one file. When discovery finds none or several, it returns `BLOCKED` and asks for the intended path; it never guesses from recency or creates the missing plan itself. It then inspects the smallest relevant code boundary and makes contract choices visible:

```text
Q1  Public API?       sibling serializeFlowGraph() | change printFlowGraph()
Q2  Stability?        versioned schema            | internal object dump
Q3  Run data?         compact overlay              | entire execution trace
```

The accepted answers become observable criteria and tasks—not hidden assumptions. They live in the single marked structured block inside human-readable `plan.md`:

```json
{
  "source_plan_path": "source-plan.md",
  "spec_path": "feature-spec.md",
  "blocking_questions": [],
  "acceptance_criteria": [
    {"id": "AC-1", "text": "Return parseable schema-versioned graph JSON."},
    {"id": "AC-4", "text": "Keep existing ASCII output byte-compatible."}
  ],
  "tasks": [{
    "id": "T-3",
    "acceptance_criteria": ["AC-1", "AC-4"],
    "validation": [
      {
        "criteria": ["AC-1"],
        "run": "pnpm exec tsx examples/05-diagram-printer/json-check.ts",
        "origin": "AC-1 and the approved v1 JSON contract",
        "oracle": "parser and schema assertions against the approved contract",
        "independence": "contract-derived"
      },
      {
        "criteria": ["AC-4"],
        "run": "diff -u expected-output.txt actual-output.txt",
        "origin": "AC-4 and the repository's existing ASCII behavior",
        "oracle": "pre-feature golden fixture",
        "independence": "pre-existing"
      }
    ]
  }]
}
```

## Where validation comes from

Boatstack does not choose a command after seeing the implementation and call that proof. Validation is derived before approval:

```text
product intent or invariant
  -> observable acceptance claim
  -> oracle that could falsify the claim
  -> executable or human procedure
  -> recorded evidence
```

`criteria` limits which claims the check can support. The compiler rejects a criterion with no mapped validation and rejects a validation attached to a criterion its task does not serve. The `origin` identifies why the check is required: an acceptance criterion, existing repository invariant, explicit human decision, risk analysis, or external contract. The `oracle` identifies what makes the result meaningful: a pre-existing fixture, approved schema, independent system, measurable threshold, review rubric, or named human judgment. `independence` makes circular evidence visible; an implementation-authored test is useful evidence, but is not automatically an independent oracle.

Ambiguous claims cannot pass unchanged:

| Ambiguous claim | Required resolution before approval |
|---|---|
| “It should be fast” | Named workload, environment, metric, and threshold such as p95 under 200 ms |
| “The design should look good” | Approved reference states, review rubric, named reviewer, and captured evidence |
| “The migration should be safe” | Enumerated invariants, rehearsal/rollback procedure, and observable failure conditions |

`/auto-plan` asks only the questions needed to establish that resolution. If no defensible oracle or authorized human judgment exists, the criterion remains `BLOCKED`; the model cannot validate its own interpretation by restating it. gstack and Spec Kit may help propose criteria and checks, but Boatstack still requires their provenance and evidence contract.

See [Validation and evidence](docs/validation-and-evidence.md) for validation forms, ambiguity handling, independence levels, gate outcomes, and the benchmark observations behind this contract.

The developer-facing transition stays inside the coding host:

```text
/plan-gate
Approve the displayed plan.
Choose the host's normal Build action.
```

`/plan-gate` presents a fingerprint over the complete source plan, spec, and `plan.md` and requires a named human. Explicit approval creates only `approval.md`, so the developer remains in Plan mode. The host's normal Build transition then validates and activates the exact approved plan before editing code.

The source plan remains required and hash-checked through `/build`; later gates rely on the resulting lock, actual diff, and accumulated evidence.

<details>
<summary>Internal deterministic boundary</summary>

The generated adapter invokes the repository-local helper; users do not need to learn these commands:

```bash
.product-loop/bin/boatstack-helper check-plan \
  --plan .product-loop/features/diagram-json/plan.md

.product-loop/bin/boatstack-helper activate-plan \
  --plan .product-loop/features/diagram-json/plan.md \
  --approval .product-loop/features/diagram-json/approval.md \
  --out-dir .product-loop/features/diagram-json/compiled \
  --output .product-loop/features/diagram-json/plan.lock.json
```

Activation compiles the machine task graph and writes the lock last. A changed planning input cannot reuse the receipt:

```console
$ .product-loop/bin/boatstack-helper activate-plan ...
PASS: approved Markdown plan activated and locked

# after plan.md prose, its structured block, the spec, or source plan changes
$ .product-loop/bin/boatstack-helper activate-plan ...
BLOCKED: stale approval receipt
```

</details>

That is the approval boundary in code: conversation cannot silently turn a draft into permission to build, and Plan mode never needs to write JSON or executable state.

See the complete, linked [worked example](examples/diagram-json/README.md).

## Bring your own product context

**Bring your context as it is.** Boatstack does not impose a documentation structure or maintain a second product memory. Keep feature briefs, vision, roadmaps, ADRs, gaps, and engineering rules wherever they already live in the repository. Cursor, Codex, or Claude discovers the relevant surrounding code and documents; Boatstack controls how that context becomes an approved change.

Boatstack treats the repository as canonical and creates only temporary, reviewable, provenance-linked task projections. This matters because a deterministic translation `T` cannot add information about the desired outcome `Y` that was not present in the source context `C`:

```text
I(Y; T(C)) <= I(Y; C)
```

This data-processing bound motivates source preservation; it does **not** prove that every transformation is harmful. A well-chosen projection can improve a finite-context model's effective performance by removing irrelevant material. The rule is therefore: **preserve the source; project only the relevant slice.**

Point the host at an existing product document:

```text
/auto-plan
Product brief: docs/features/team-notifications.md
Relevant decisions: docs/architecture/notifications.md
```

If no product document exists, the host Plan-mode file can begin with only the ordinary request. Boatstack then inspects the smallest relevant repository slice, separates discoverable facts from product questions, and produces the consistent handoff:

```text
existing product docs + code -> questions -> feature spec -> approval -> engineering plan
```

Product documents define what and why. ADRs record durable technical decisions. Gaps record known incomplete work. Boatstack references these sources without replacing them. Any generated spec or plan must remain traceable to its sources and reviewable as a lossy task projection. No context map or documentation migration is required in V1; the project config may list useful starting paths when a repository wants stable defaults.

## Use Boatstack with gstack and GitHub Spec Kit

Boatstack is primarily a **control and evidence layer** over your coding host and optional planning/review tools. It does not need to reproduce everything those projects already do well:

```text
product intent + repository context
                 |
        [ BOATSTACK CONTRACT ]
          /                  \
 gstack review lenses   Spec Kit artifacts
          \                  /
       normalized spec + plan + decisions
                 |
  Markdown approval -> build activation -> evidence gates -> PR
```

| Layer | What it contributes | What remains Boatstack-owned |
|---|---|---|
| Coding host: Cursor, Codex, or Claude | Plan mode, repository exploration, implementation, tool execution | Cross-host artifact meanings and transition rules |
| [gstack](https://github.com/garrytan/gstack) | Product/CEO, design, engineering, and developer-experience review lenses; adversarial plan critique | Which findings change the approved plan, validation provenance, approval hashing, and gate outcomes |
| [GitHub Spec Kit](https://github.com/github/spec-kit) | Constitution, specify, clarify, plan, tasks, analyze, checklist, and related spec-driven artifacts | Normalization into Boatstack's criterion/validation contract and explicit human plan gate |
| Boatstack core | Source-plan discovery, provenance, question/gap boundaries, Markdown approval, deterministic build activation, drift locks, evidence mapping, review/ship gates | The completion and shipping claim itself |

### With gstack

When installed, Boatstack invokes gstack through namespaced `/gstack-*` review skills inside `auto-plan`, review, or retrospective work. gstack can challenge product premises, design states, architecture, failure modes, and developer experience. Its findings are proposals: Boatstack records accepted decisions, maps resulting claims to validation, and re-runs approval when semantics change. gstack never becomes an implicit approval signal.

### With GitHub Spec Kit

Spec Kit can generate or cross-check the constitution, specification, clarification answers, implementation plan, tasks, analysis, and checklists. Boatstack sits above those artifacts as the authority/evidence layer:

```text
speckit.specify / clarify / plan / tasks / analyze / checklist
                              |
                              v
        Boatstack criteria + oracle + validation normalization
                              |
                     explicit /plan-gate
```

`speckit.implement` does not bypass Boatstack's approval receipt, build activation, or `/build` boundary. If Spec Kit changes accepted semantics, Boatstack invalidates the receipt and returns to approval. This preserves Spec Kit's artifact-generation value without allowing a generator to approve or validate its own output.

### Core only

Both integrations are optional. Boatstack core still performs Plan-mode source discovery, question-led specification, structured Markdown planning, explicit approval receipts, deterministic build activation, validation/evidence mapping, test/review gates, and PR preparation. Integration failure is recorded as partial installation and does not roll back the working core.

The installer creates one canonical `.product-loop/` runtime and thin adapters for:

```text
.cursor/commands/{auto-plan,plan-gate,build,test-gate,review,ship,retro}.md
.cursor/rules/boatstack.mdc
.agents/skills/boatstack/SKILL.md
.claude/skills/boatstack/SKILL.md
.github/PULL_REQUEST_TEMPLATE/boatstack.md
```

It refuses to overwrite user-owned host files. The small platform helper lives under ignored `.product-loop/bin/`; users continue to operate Boatstack through their coding host. Re-run the installer to restore the helper on a fresh clone.

## Freedom inside, evidence at the edges

Boatstack is a mathematically modeled composite node inside an Intelligence Flow graph:

```text
product intent + repository state
              |
              v
         [ BOATSTACK ]
              |
              v
diff + evidence + decisions + known gaps
```

Inside the node, a model, developer, team, or tool may use any suitable implementation method. At the edges, Boatstack makes authority, acceptance, evidence, and known gaps explicit.

- A first implementation that passes is a **linear path**.
- Evidence that causes revision creates a **feedback path**.
- Multiple agents or approaches form a **branch/merge path**.

Boatstack can participate in a loop, but it is not constrained to one. The graph topology follows the work. The invariant is evidence at transitions, not repeated ceremony.

- **Optimization:** select the smallest context and ceremony that preserve required quality and evidence.
- **Control:** represent state explicitly and verify approval, acceptance, and shipping transitions.
- **Model neutrality:** route on ambiguity, risk, convergence, tool results, and evidence—not model brand, price, or a guessed capability tier.

The full mapping from equations to files and checks is in [Evidence-engineered coding](docs/evidence-engineered-coding.md).

## Evidence, with boundaries

The rules were informed by a mechanically audited local corpus of **4,016 benchmark trial results** and **3,985 signal streams**, plus two real product-repository studies. For example:

| Observed failure | Encoded move |
|---|---|
| Restarting discarded partial progress | Preserve known-good state; repair locally |
| Structured-output errors hid useful work | Validate and perform bounded same-step repair |
| Stronger verification wording regressed | Treat self-review as evidence, not the oracle |
| Blind context trimming lost accuracy | Select relevant context without deleting required state |
| A development-slice gain did not transfer | Require representative gates before promoting a move |

Read the [research and design record](docs/research-and-design.md) and [corpus audit](docs/benchmark-corpus-audit.md). This evidence motivates the loop; it does not prove that every future feature or model will improve.

## Context has a budget

The three canonical runtime references currently total approximately **4625 estimated tokens** using `ceil(characters / 4)`. That is a stable compactness signal, not provider billing. Host adapters stay thin and load the operation-specific slice on demand.

## Status

Boatstack is an alpha research distribution. It can generate host adapters, keep planning and approval Markdown-native, compile traceable task/test artifacts at Build, detect stale approvals, and preserve provenance. The next proof boundary is a paired feature-development evaluation against a plain host harness.
