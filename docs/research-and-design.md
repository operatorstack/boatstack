# Research and design: an evidence-engineered coding node

## Outcome

The proposed product is not a large prompt and not a Codex-, Cursor-, Claude-, or model-specific harness. It is a versioned engineering protocol with:

1. canonical project facts, artifacts, states, gates, and evidence under `.product-loop/`;
2. thin generated adapters for each host;
3. a human approval boundary between planning and executable work;
4. a separate evidence-gated loop for improving the protocol itself.

The initial implementation is in [`product-engineering-loop/`](product-engineering-loop/). Its exporter generates Cursor rules/commands, Claude Code and Codex skills, and a GitHub PR template from one source.

The public [Boatstack](https://github.com/operatorstack/boatstack) repository is a compiled distribution, not a second source of product/runtime truth. `scripts/build_boatstack.py` projects this package into a branded README, evidence-engineered-coding explanation, worked example, tests, and installable skill; `UPSTREAM.json` binds every projected file to its Intelligence Flow commit. A Boatstack-owned scheduled workflow polls this public source and proposes content changes by PR. Boatstack's `.github/workflows` directory is a deliberately separate control-plane slice: it originates and changes in Boatstack through ordinary, manually reviewed PRs and is never emitted, owned, or removed by the Intelligence Flow projector.

## Outcome sizing and where value emerges

For a feature, the minimal outcome definition is:

```text
one domain + one contract + one outcome + one next operator + one verifier
```

Because the coding node may become a shipped product, it keeps delivery and improvement as separate paths:

- **Delivery path:** developer intent -> questions -> draft -> human approval -> deterministic materialization -> build -> test -> review -> PR.
- **Improvement path:** run evidence -> failure mode -> proposed move -> paired representative gate -> promote/reject/wash.

Value emerges twice. The delivery path reduces assumption-driven code and produces reviewable evidence immediately. The improvement path lets recurring failures compound into better tooling without allowing one anecdote to pollute every future repository.

## Commands and the approval boundary

```text
Cursor/GitHub intent
  -> host Plan mode   saved source plan; no implementation
  -> /auto-plan       validate source plan, then Markdown-only draft package; no code
  -> /plan-gate       explicit human approve/change request
                     after approval: write Markdown approval receipt only
  -> /build           verify receipt, compile tasks/evidence + lock, then code
  -> /test-gate       requirement-derived independent evidence
  -> /review          diff + intent + invariant + risk + gap review
  -> /ship            PR preparation, not merge/deploy
  -> /retro           propose a harness move; never silently promote it
```

`/auto-plan` cannot infer acceptance from silence. The canonical `plan.md` contains human-readable reasoning and one marked structured block. `/plan-gate` records explicit acceptance in `approval.md` using a fingerprint over the source Plan-mode file, spec, and complete plan. At the normal Build transition, Boatstack verifies that receipt, compiles the machine task graph and evidence, then writes and checks the lock before code changes. Any planning edit invalidates the receipt and returns to approval. This turns agreement into a machine-checkable state transition without asking Plan mode to write executable state.

`/auto-plan` is deliberately not the first planning surface. It requires exactly one non-empty file produced by the active host's Plan mode and refuses to invent that input. It resolves the active plan from host/system conversation context first, then checks only bounded plan locations; zero or multiple candidates block instead of silently choosing the newest file. The source file is hash-bound through build; after build, test/review/ship consume the approved lock, diff, and evidence rather than repeatedly loading the exploratory plan.

## Why the workflow has no model conditions

The benchmark evidence shows that the binding failure mode changes by task, distribution, and intervention. It does not justify hardcoding “cheap model workflow” and “strong model workflow.” A model name, provider, or price is not an observed failure state.

The node therefore branches only on:

- unknown versus discoverable information;
- risk and reversibility;
- convergence versus thrashing;
- protocol/tool outcomes;
- test-oracle fidelity;
- repeated tactics;
- gate evidence.

This still allows a repository owner to choose any model or routing service. It means the engineering contract stays identical and performance differences become measurable rather than baked into prompts.

## Benchmark corpus coverage

The evidence was audited in two reproducible passes:

- [`BENCHMARK_CORPUS_AUDIT.md`](BENCHMARK_CORPUS_AUDIT.md): **3,571** historical per-trial results across 19 run/corpus groups; 3,540 signal streams; no unreadable results.
- [`BENCHMARK_SUBMISSION_2_1_AUDIT.md`](BENCHMARK_SUBMISSION_2_1_AUDIT.md): all **445** July Terminal-Bench 2.1 submission trials and all 445 signal streams.

Combined mechanical coverage is **4,016 trial results** and **3,985 signal streams**. The JSON companions preserve group-level outcomes, terminal reasons, protocol errors, timeouts, and loop-event aggregates.

This is not a claim that every transcript was manually read. Mechanism conclusions come from the preregistered comparisons, paired gates, experiment log, and representative trajectory inspections. The audit proves that every locally present result was included in aggregate coverage.

Four run IDs mentioned in the historical notes do not have raw corpora locally:

- `2026-06-10__03-21-28`: two-arm correctness-relief candidate; summary preserved in `IMPROVEMENTS.md`;
- `2026-06-10__05-59-14`: nine-trial retry-verified smoke; full table preserved in `RESEARCH_LOG.md` E8;
- `2026-06-10__15-05-24`: incomplete spend-cap false start, explicitly excluded from inference;
- `2026-06-11__15-09-20`: early Qwen probe; summary preserved in `ZERO_TO_QWEN.md`.

Those are **summary-only evidence** in this design. They are not represented as newly re-derived raw results.

## What the Terminal-Bench data actually encodes

| Observation | Evidence | Boatstack rule |
|---|---|---|
| Fatal command timeouts hid capability | Non-fatal timeout handling moved the original score from roughly 54% to 65.4% | Tool failures become observations when safely recoverable; external timeout remains authoritative |
| Malformed structured responses were recoverable | July screen repaired 56/63 malformed responses; full run repaired 490/567 exposures | Validate schemas and attempt bounded same-step parse repair; do not label protocol failure as reasoning failure |
| More verification wording did not create truth | Verify-before-finish and same-model repair variants washed | Self-review is evidence, never the sole oracle |
| Restarting destroyed partial progress | Failed retry-verified restarts retained roughly 53% mean fractional progress | Preserve last known-good state; use targeted repair and compare snapshots |
| Blind context trimming regressed | Windowing lost 7.2 binary points against the floor in its gate | Project minimal relevant context, but never discard state merely to reduce tokens |
| More steps changed the label, not correctness | Qwen 30->40 reduced exhaustion but converted failures into near misses/wrong answers | Increase budget only when trajectory evidence shows convergence; stop thrashing |
| Strict self-checking caused collateral damage | Strictness was a certified loss against its non-strict base | Stronger instructions are not monotonic; protect known-good output and verify against an independent contract |
| Spec-first helped a development slice for the wrong reason | Qwen dev gate gained 7 points, while the frozen test oracle had about 47% fidelity | Specs/tests can scaffold understanding, but model-authored tests do not become ground truth |
| The development result did not transfer | Spec-first was a statistical wash on the full board | Promotion requires representative distribution/holdout, not only a tuned dev slice |
| Model change relocated the bottleneck | Same harness exposed near-miss dominance on Gemini and step exhaustion on Qwen | Diagnose the active population each time; do not encode model-specific recipes |
| Mid-run aggregates changed direction | Qwen board interpretation moved as task coverage deepened | Compare paired completed coverage and uncertainty, not early aggregate rank |
| External failure invited destructive recovery | A sanitized partial schema apply failure led to an invented reset path before review removed it | Treat recovery authority as a deterministic boundary: preserve state, diagnose read-only, transact or fix forward |

Sources: [`RESEARCH_LOG.md`](../11-harbor-submit/RESEARCH_LOG.md), [`EXPERIMENT_GEMINI20_2026-07-15.md`](../11-harbor-submit/EXPERIMENT_GEMINI20_2026-07-15.md), [`ZERO_TO_QWEN.md`](../11-harbor-submit/ZERO_TO_QWEN.md), and [`docs/12-self-verification-fidelity.md`](../../docs/12-self-verification-fidelity.md).

The irreversible-operation boundary is a **PROPOSED** Move. The incident supports the target failure mechanism, while the benchmark campaign supports deterministic enforcement over stronger wording. Neither proves the new guard's net effect. Promotion requires a paired unguarded baseline, destructive and safe corpora, real host events, bounded latency, secret-free denials, and no workflow regression.

## What two example repositories add

Terminal-Bench supplies failure mechanics; the product repositories supply real engineering context.

Repository context remains canonical rather than being migrated into a Boatstack-owned memory. For desired outcome `Y`, source context `C`, and deterministic translation `T`, the data processing inequality gives `I(Y; T(C)) <= I(Y; C)`: translation cannot manufacture missing information and may discard provenance or relationships. This does not imply that every projection reduces model performance; selecting a smaller relevant slice can improve effective use of a finite context window. The implemented rule is therefore **preserve the source; project only the relevant slice**, with generated artifacts kept reviewable and traceable to source paths.

The first example repository demonstrates:

- durable non-negotiables for tenancy, evidence handling, audit logs, and resource caps;
- deterministic core state machines with an LLM behind a narrow adapter port;
- a docs/spec PR before the code PR for uncertain contracts;
- typed input slots instead of hardcoded client details;
- acceptance tests plus explicitly parked work.

The second example repository demonstrates:

- durable decision notes with insight, rationale, decision, risks, and open questions;
- known divergences kept visible rather than implied complete;
- mechanical CI rules derived from recurring real-world failure patterns;
- test plans spanning unit/build/staging/fail-soft behavior.

This is why ADRs are only one artifact. The evidence contract also needs a question ledger, feature spec, gap ledger, test plan, risk note, evidence ledger, and runbook when relevant.

## What is adopted from gstack and Spec Kit

From [gstack](https://github.com/garrytan/gstack/blob/main/docs/skills.md): forcing questions before planning; product, design, engineering, and developer-experience review lenses; plan artifacts as deliverables; review readiness; and a structured ship preflight. Its [`/autoplan`](https://github.com/garrytan/gstack/blob/main/autoplan/SKILL.md) is an integration option, not the canonical source.

From [GitHub Spec Kit](https://github.com/github/spec-kit): constitution, specify, clarify, plan, tasks, analyze, checklist, implement, and converge stages. Spec Kit can generate artifacts, but `.product-loop/` normalizes their meaning and preserves the explicit human plan gate.

The node does not adopt a universal “boil the ocean” policy. Completeness is required for the approved outcome; unrelated architecture remains outside its boundary.

## Host portability

- [Cursor project rules](https://docs.cursor.com/context/rules) live in `.cursor/rules`; project commands live in [`.cursor/commands`](https://docs.cursor.com/en/agent/chat/commands). Cursor is a first-class exported surface.
- Claude Code receives a project skill while `CLAUDE.md` stays repository-owned.
- Codex receives a repo skill under `.agents/skills`; [OpenAI recommends](https://learn.chatgpt.com/docs/customization/overview) keeping durable `AGENTS.md` guidance small and workflows in reusable skills.
- GitHub receives a reviewer-first fallback template. The active adapter generates an exact `pr.md` preview from the committed diff and available evidence, then requires a separate open/update confirmation. Managed work may cite current approval and gates; an ad-hoc branch must label missing evidence `NOT_VERIFIED`.

The exporter refuses to overwrite any non-generated file. Its lock records canonical version, config hash, adapters, and output hashes so a PR can show exactly what changed.

## Evaluation of the finished node

The best public primary benchmark is [FeatureBench](https://github.com/LiberCoders/FeatureBench), because it targets complex feature development and provides a 100-instance fast split plus agent integrations. Evaluate the same model and tasks with:

```text
plain host harness  vs  evidence-engineered coding node
```

Measure resolved rate, regression rate, tokens/cost, elapsed time, question count, plan revisions, stale-lock blocks, test-oracle independence, review findings, and ship-gate false accepts.

Add a private held-out feature set drawn from the two example repositories for the parts public executable benchmarks do not score: whether the right human questions were asked, durable decisions and gaps were classified correctly, project invariants were preserved, and the PR was actually usable. SWE-bench can be a supplemental bug-fix check, but it is less aligned with feature/product work. Do not use another Terminal-Bench run as the primary validation for this product.

## Open questions before private-repo productization

1. Should the canonical artifact store stay as versioned files or become a small local database with generated Markdown views?
2. Which project facts may be inferred during `init`, and which must always be human-confirmed?
3. What is the minimum independent oracle required at each risk level?
4. Should adapter updates be generated locally, by a GitHub App, or both?
5. How should private traces be redacted before entering the improvement corpus?
6. What promotion sample size/noise band should the product default to outside benchmarks?

The next valuable step is to install the exporter into a clean fixture repository, forward-test `host Plan mode -> saved plan -> /auto-plan -> /plan-gate -> /build` on one real feature, and only then apply it to the two example repositories.
