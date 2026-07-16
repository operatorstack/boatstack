<!-- Generated from operatorstack/intelligence-flow. -->

# Loop engineering

Boatstack treats software delivery as a feedback system around a coding model. The model matters, but it is not the whole system.

## The minimal state

For current repository state `x_t`, select only the task-relevant slice:

```text
z_t = P_s(x_t)
```

Canonicalize that slice into one domain, one contract, one outcome, and one next operator:

```text
r_t = R(z_t)
u_t = f(r_t)
x_(t+1) = V(u_t, acceptance, invariants)
```

In the repository, those terms are not decorative notation:

| Term | Boatstack artifact or check |
|---|---|
| `P_s` | context paths in `project.json` plus the current feature boundary |
| `R` | question ledger, feature spec, acceptance criteria, structured plan |
| `f` | one operation: auto-plan, plan-gate, build, test, review, or ship |
| `V` | requirement/test matrix, command evidence, review findings, plan hashes |
| `x_(t+1)` | locked plan, bounded diff, gate result, PR, or recorded gap |

ZCA creates immediate value by reducing a vague feature request to one verifiable slice. For a shipped SDK, API, or CLI, Boatstack uses two slices: the implementation boundary and one representative consumer path.

## Optimization is constrained, not blind compression

Boatstack aims to minimize the cost of context and ceremony subject to an accepted outcome:

```text
minimize   C(context) + C(ceremony) + C(rework)
subject to acceptance criteria pass
           project invariants hold
           required evidence exists
           approval is current
```

That is why context trimming is not automatically an optimization. If removing state increases rework or false acceptance, total cost rises. The canonical runtime references are approximately **3371 estimated tokens**, while host adapters point to one operation at a time.

## Control appears at state transitions

The plan gate is a concrete controller boundary:

```python
if sha256(current_plan) != approval["plan_sha256"]:
    return "BLOCKED: plan changed after approval"
```

The plan compiler is another:

```python
uncovered = acceptance_ids - task_acceptance_ids
if uncovered:
    raise ValueError(f"uncovered acceptance criteria: {sorted(uncovered)}")
```

The test gate maps each claim to evidence instead of asking the implementer whether it feels finished:

```text
AC-4: ASCII output stays byte-compatible
  -> diff -u expected-output.txt actual-output.txt
  -> PASS | FAIL | BLOCKED
```

These boundaries do not guarantee correct software. They make missing authority, missing coverage, stale state, and failed evidence observable before shipping.

## There are two loops

Delivery and loop improvement remain separate:

```text
delivery:    intent -> approved plan -> code -> gates -> PR
improvement: failure evidence -> mechanism -> candidate move -> paired gate
                                                       -> promote | wash | reject
```

A failed task can suggest a better move, but one anecdote cannot silently rewrite every future prompt. Promotion requires a representative comparison and a non-regression boundary.

## What is evidence-backed

The current moves were derived from the Intelligence Flow benchmark corpus and product-repository studies. The generated source commit is [`5e1bbedab7d5bae49dd0e5cdda1e4b7804ab057f`](https://github.com/operatorstack/intelligence-flow/tree/5e1bbedab7d5bae49dd0e5cdda1e4b7804ab057f/examples/12-product-engineering-loop).

The evidence supports specific failure mechanisms and guardrails. It does not establish that Boatstack is optimal, that control-theory notation proves software quality, or that one workflow dominates every team. Those are evaluation questions, so the distribution preserves measurements, provenance, gaps, and negative results.
