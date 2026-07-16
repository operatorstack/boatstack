<!-- Generated from operatorstack/intelligence-flow. Edit the upstream product-loop source, not this file. -->

# Boatstack

**Plan the route. Prove the work. Ship.**

Boatstack is loop engineering for coding agents: a model-neutral path from a product request to an explicitly approved, tested, reviewed pull request. Its behavior is generated from [Intelligence Flow at `fcc2faa55ac3c2332ce4a19293d89023f578784d`](https://github.com/operatorstack/intelligence-flow/tree/fcc2faa55ac3c2332ce4a19293d89023f578784d/examples/12-product-engineering-loop).

It is not a claim that a longer prompt writes better code. Here is what the loop actually does.

## One request, as executable state

Start with ordinary product intent:

```text
Add machine-readable JSON output to the diagram printer while preserving the current text output.
```

`/auto-plan` inspects the smallest relevant code boundary and makes contract choices visible:

```text
Q1  Public API?       sibling serializeFlowGraph() | change printFlowGraph()
Q2  Stability?        versioned schema            | internal object dump
Q3  Run data?         compact overlay              | entire execution trace
```

The accepted answers become observable criteria and tasks—not hidden assumptions:

```json
{
  "acceptance_criteria": [
    {"id": "AC-1", "text": "Return parseable schema-versioned graph JSON."},
    {"id": "AC-4", "text": "Keep existing ASCII output byte-compatible."}
  ],
  "tasks": [{
    "id": "T-3",
    "acceptance_criteria": ["AC-1", "AC-4"],
    "validation": [
      "pnpm exec tsx examples/05-diagram-printer/json-check.ts",
      "diff -u expected-output.txt actual-output.txt"
    ]
  }]
}
```

The compiler refuses a criterion with no task or verification. Then `/plan-gate` requires a named human and binds approval to content hashes:

```bash
python3 boatstack/scripts/compile_plan.py \
  --plan .product-loop/features/diagram-json/plan.json \
  --out-dir .product-loop/features/diagram-json/compiled

python3 boatstack/scripts/approve_plan.py \
  --spec .product-loop/features/diagram-json/spec.md \
  --plan .product-loop/features/diagram-json/plan.json \
  --tasks .product-loop/features/diagram-json/compiled/tasks.json \
  --approved-by "Boateng Opoku-Yeboah" \
  --output .product-loop/features/diagram-json/plan.lock.json
```

Build work checks that lock first:

```console
$ python3 boatstack/scripts/approve_plan.py ... --check
PASS: approved plan lock matches the current artifacts

# after plan.json changes
$ python3 boatstack/scripts/approve_plan.py ... --check
BLOCKED: stale or invalid plan lock: plan
```

That is the approval boundary in code: conversation cannot silently turn a draft into permission to build.

See the complete, linked [worked example](examples/diagram-json/README.md).

## Bring your own product context

Boatstack does not impose a documentation structure or maintain a second product memory. Keep feature briefs, vision, roadmaps, ADRs, gaps, and engineering rules wherever they already live in the repository. Cursor, Codex, or Claude discovers the relevant surrounding code and documents; Boatstack controls how that context becomes an approved change.

Point the host at an existing product document:

```text
/auto-plan Build team notification preferences.
Product brief: docs/features/team-notifications.md
Relevant decisions: docs/architecture/notifications.md
```

Or begin with only the request and let the host inspect the smallest relevant repository slice. Boatstack separates discoverable facts from product questions, then produces the consistent handoff:

```text
existing product docs + code -> questions -> feature spec -> approval -> engineering plan
```

Product documents define what and why. ADRs record durable technical decisions. Gaps record known incomplete work. Boatstack references these sources without rewriting them. No context map or documentation migration is required in V1; the project config may list useful starting paths when a repository wants stable defaults.

## Install into a repository

```bash
git clone https://github.com/operatorstack/boatstack.git && cd boatstack
cp project.example.json /path/to/product/.boatstack-project.json
# Replace the example paths and commands with facts from the product repository.

python3 boatstack/scripts/export_repo.py \
  --repo /path/to/product \
  --config /path/to/product/.boatstack-project.json \
  --adapter-name boatstack

# Review the dry run, then materialize it on a branch.
python3 boatstack/scripts/export_repo.py \
  --repo /path/to/product \
  --config /path/to/product/.boatstack-project.json \
  --adapter-name boatstack \
  --write
```

The exporter creates one canonical `.product-loop/` runtime and thin adapters for:

```text
.cursor/commands/{auto-plan,plan-gate,build,test-gate,review,ship,retro}.md
.cursor/rules/boatstack.mdc
.agents/skills/boatstack/SKILL.md
.claude/skills/boatstack/SKILL.md
.github/PULL_REQUEST_TEMPLATE/boatstack.md
```

It refuses to overwrite user-owned host files. Run the same export with `--check` in CI to detect drift.

## Why “loop engineering”

A coding model is one operator inside a controlled path:

```text
intent -> questions -> spec -> plan -> human approval -> build
       -> test evidence -> review evidence -> PR -> failure analysis
                                      ^                     |
                                      +--- promoted moves ---+
```

- **Optimization:** select the smallest context and ceremony that preserve the required quality and evidence constraints.
- **Control:** represent state explicitly, gate transitions, verify outputs, preserve known-good progress, and feed observed failures into separately tested improvements.
- **Model neutrality:** route on ambiguity, risk, convergence, tool results, and evidence—not model brand, price, or a guessed capability tier.

The full mapping from equations to files and checks is in [Loop engineering](docs/loop-engineering.md).

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

The three canonical runtime references currently total approximately **3371 estimated tokens** using `ceil(characters / 4)`. That is a stable compactness signal, not provider billing. Host adapters stay thin and load the operation-specific slice on demand.

## Status

Boatstack is an alpha research distribution. It can generate host adapters, compile traceable task/test artifacts, hash-lock explicit approval, detect stale plans, and preserve provenance. The next proof boundary is a paired feature-development evaluation against a plain host harness.
