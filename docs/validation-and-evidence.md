<!-- Generated from operatorstack/intelligence-flow. -->

# Validation and evidence

## External-write safety evidence

External-write claims require more than a successful happy path. Record the immutable target identity, transaction or fix-forward behavior, and an oracle independent of the implementation. The operational diff must contain no agent-executable reset, drop, broad delete, or destructive recovery. A `--dry-run` is useful, but cannot by itself prove which live target was selected or how a partial failure recovers.

Boatstack separates producing a change from proving a claim about that change. A successful command is evidence only when its relationship to an approved requirement and a falsifiable oracle is explicit.

```text
claim -> origin -> oracle -> procedure -> observation -> gate result
```

## The validation contract

Every planned validation records:

| Field | Question it answers |
|---|---|
| `criteria` | Which exact acceptance claims may this evidence support? |
| `run` | What command or human/external procedure produces the observation? |
| `origin` | Why is this check required? |
| `oracle` | What independent fact, fixture, threshold, rubric, or judgment can falsify the claim? |
| `independence` | How separate is the oracle from the implementation that is being judged? |

The compiler rejects validation without those fields, a validation mapped outside its task's criteria, or an acceptance criterion with no validation procedure. This prevents a broad task-level test from being presented as proof for every claim the task touches.

## Where validation originates

Validation is derived before implementation from one or more sources:

1. **Product intent:** observable outcomes and explicit human decisions.
2. **Existing repository behavior:** public contracts, fixtures, tests, schemas, type checks, builds, and documented invariants.
3. **Risk and failure analysis:** security boundaries, rollback requirements, destructive paths, recovery behavior, and previously observed failure modes.
4. **External contracts:** provider schemas, standards, deployment state, compatibility targets, and downstream consumers.

gstack and Spec Kit can propose questions, criteria, checks, or review rubrics. Boatstack treats those as generators. Their output becomes authoritative only through the same approval, provenance, oracle, and evidence contract as any other proposal.

## Validation forms

| Form | Suitable oracle | Typical evidence |
|---|---|---|
| Static/build | Existing compiler, linter, schema, package build | Exit code and diagnostic output |
| Unit/contract | Approved interface behavior or pre-existing fixture | Test result plus named contract rows |
| Differential/property | Independent implementation, invariant, or generated property | Compared outputs, counterexamples, mutation score |
| Integration/runtime | Real dependency or representative environment | Requests, responses, logs, traces, screenshots |
| Operational | Rollout, rollback, alert, recovery, and migration invariants | Rehearsal output, monitored state, recovery timing |
| Human/product | Approved rubric, reference states, scenarios, named decision owner | Review record, annotated screenshots, acceptance decision |
| External state | Authoritative third-party system or downstream consumer | API query, deployment observation, linked external record |

Not every criterion should be forced into an automated test. Subjective or externally controlled outcomes can use a named human or external procedure, but the rubric, owner, artifact, and decision must be observable.

## Ambiguity is a state, not a test result

An ambiguous phrase cannot be validated by repeating it:

```text
"fast"       -> workload + environment + metric + threshold
"looks good" -> approved states + rubric + reviewer + captured artifact
"safe"       -> named invariants + failure cases + rollback/recovery evidence
```

`auto-plan` first asks whether the missing information is discoverable from the repository. If not, and different answers materially change the contract, it asks the responsible human. A reversible assumption may be recorded with an expiry trigger. A material unresolved ambiguity remains `BLOCKED` at the plan gate; the implementer cannot declare its own interpretation correct.

## Independence is graded

Evidence is not simply independent or circular:

```text
pre-existing/external oracle
        > contract-derived check
        > implementation-authored check
        > same-agent narrative self-review
```

The ordering is a risk signal, not a universal scoring formula. Implementation-authored tests are valuable, but higher-risk changes need another oracle: a pre-existing fixture, independent contract, differential system, mutation/property check, representative runtime, external authority, or named human review.

## Gate outcomes

- `PASS`: every required claim has current evidence from its mapped validation contract.
- `FAIL`: an observation contradicts the accepted claim or invariant.
- `BLOCKED`: required evidence, authority, environment, or ambiguity resolution is missing.
- `PASS_WITH_GAPS`: allowed only when repository policy permits it, every gap is explicit and owned, and no gap is critical.

Skipped checks do not disappear. They remain blocked or become an explicitly accepted gap with impact, owner, and revisit trigger.

## Why Boatstack uses this structure

The benchmark program did not show that stronger self-verification language reliably creates truth:

- verify-before-finish and same-model repair variants washed;
- strict self-checking caused collateral damage against its non-strict base;
- a spec-first development slice improved by 7 points while its frozen test oracle had only about 47% fidelity, and the intervention washed on the full board;
- structured-response repair recovered 56 of 63 malformed responses in the screen and 490 of 567 exposures in the full run, showing that protocol validation can recover useful work without proving task correctness.

These observations motivate—not mathematically prove—the separation between protocol success, implementation activity, and correctness evidence. See [Research and design](research-and-design.md), [Benchmark corpus audit](benchmark-corpus-audit.md), and [Terminal-Bench 2.1 submission audit](benchmark-submission-audit.md) for scope and evidence boundaries.

## ZCA translation

For ordinary work, Boatstack projects the smallest implementation-relevant slice. For something shipped as an SDK, API, CLI, or reusable product, it also requires a representative verifier/consumer slice. Value emerges where the implementation claim meets an oracle capable of disproving it—not from adding ceremony to the implementation itself.

The same rule applies to phased delivery. Internal phases stay inside one delivery slice. Multiple PRs require explicit ordered delivery slices, and each slice gets fresh test and review receipts bound to its own committed diff before a separately confirmed ship action. A parent-plan approval or previous slice receipt cannot authorize the next PR.
