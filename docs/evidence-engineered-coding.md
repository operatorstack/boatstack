<!-- Generated from operatorstack/intelligence-flow. -->

# Evidence-engineered coding

Boatstack is a mathematically modeled coding node, not a prescribed loop. It leaves implementation open and makes authority, evidence, and accepted outcomes observable at the node boundary.

```text
product intent + repository state
              |
              v
         [ BOATSTACK ]
              |
              v
diff + evidence + decisions + known gaps
```

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

Delivery phases use the same separation. Internal phases are tasks inside one slice.
If the accepted result truly needs multiple PRs, the plan defines ordered delivery
slices, but the control state activates only one. Every active slice must produce
fresh diff-bound test and review receipts and receive its own ship confirmation;
approval of the parent plan carries scope, not publication authority.

The entry state is not an unstructured chat message. Ordinary product intent is first explored in the active host's Plan mode and saved as a source plan. The host passes that file's path to `auto-plan` explicitly (`--plan <path>`); Boatstack never scans directories for plans, and requires that file before projecting repository context:

```text
ordinary intent --host Plan mode--> source plan file --auto-plan--> reviewable feature package
```

The source plan is neither approval nor executable truth. It is the provenance-bearing proposal that Boatstack questions, grounds in repository evidence, and refines. Its file and hash are required through build. After build, evidence gates operate from the approved lock and actual change, so they do not depend on continuing to load the original source plan.

## Preserve the source; project the slice

Context is potential value, not guaranteed value. Let `C` be canonical repository context, `Y` the desired outcome, and `T(C)` a deterministic translation into another representation. The data processing inequality gives the motivating bound:

```text
I(Y; T(C)) <= I(Y; C)
```

A translation cannot create information about `Y` that was absent from `C`, and it may lose relationships, provenance, or constraints. This is a mathematical bound on information, not proof that every summary or projection makes an AI system perform worse. Under a finite context window and finite attention, a relevant projection can improve effective task performance by excluding noise.

Boatstack therefore keeps two distinct context slices:

1. **Canonical product knowledge:** repository-owned documents, code, decisions, and history remain authoritative and keep their existing structure.
2. **Temporary task projection:** the smallest relevant subset is selected for the current operator, with source references and reviewable transformations.

Generated specs and plans may clarify or canonicalize the task, but they never silently replace their sources. Value emerges from lowering task entropy while retaining a path back to the information from which each claim was derived.

## Open execution, controlled claims

Boatstack does not constrain the implementation operator. A model may explore, edit, test, backtrack, delegate, or select any suitable method inside the approved boundary. What it cannot do is silently convert activity into authority or a completion claim.

```text
open:        implementation method, model, tools, local tactics
controlled:  approved intent, evidence, acceptance, review, shipping authority
```

This is the central separation: **you are free in how you build; only claims of completion require evidence.**

## Optimization is constrained, not blind compression

Boatstack aims to minimize the cost of context and ceremony subject to an accepted outcome:

```text
minimize   C(context) + C(ceremony) + C(rework)
subject to acceptance criteria pass
           project invariants hold
           required evidence exists
           approval is current
```

That is why context trimming is not automatically an optimization. If removing state increases rework or false acceptance, total cost rises. The canonical runtime references are approximately **15145 estimated tokens**, while host adapters point to one operation at a time.

## Control appears at transitions

The Plan-mode approval gate is a concrete controller boundary:

```text
fingerprint = hash(source_plan + spec + complete_plan_md)
if fingerprint != approval_md.approval_fingerprint:
    BLOCKED: planning input changed after approval
```

Build activation adds the machine coverage boundary:

```text
uncovered = acceptance_ids - task_acceptance_ids
if uncovered:
    BLOCKED: acceptance criteria lack tasks or verification
```

The test gate maps each claim to evidence instead of asking the implementer whether it feels finished:

```text
AC-4: ASCII output stays byte-compatible
  -> diff -u expected-output.txt actual-output.txt
  -> PASS | FAIL | BLOCKED
```

These boundaries do not guarantee correct software. They make missing authority, missing coverage, stale state, and failed evidence observable before shipping.

The full oracle, ambiguity, and evidence model is documented in [Validation and evidence](validation-and-evidence.md).

## The graph follows the work

Boatstack does not force every change through a feedback cycle:

```text
linear:       intent -> Boatstack -> accepted change
feedback:     evidence -> revision -> new evidence
branch/merge: path A --\
                        compare -> accepted change
              path B --/
```

A successful first pass stays linear. A failed or incomplete check creates a feedback edge. Parallel implementation or review creates branches. Boatstack can therefore be a node in a loop without defining itself by the loop.

Delivery and system improvement also remain separate. A failed task may suggest a better harness move, but one anecdote cannot silently rewrite every future instruction. Promotion requires a representative comparison and a non-regression boundary.

## What is evidence-backed

The current moves were derived from the Intelligence Flow benchmark corpus and product-repository studies. The generated source commit is [`3e8ad57097eea39260ae67102997bdbd316faf17`](https://github.com/operatorstack/intelligence-flow/tree/3e8ad57097eea39260ae67102997bdbd316faf17/labs/12-product-engineering-loop).

The evidence supports specific failure mechanisms and guardrails. It does not establish that Boatstack is optimal, that control-theory notation proves software quality, or that one workflow dominates every team. Those are evaluation questions, so the distribution preserves measurements, provenance, gaps, and negative results.
