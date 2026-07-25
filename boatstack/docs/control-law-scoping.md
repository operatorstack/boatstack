# Scoping control laws so bug fixes don't under-constrain them

A companion to the **Boundary Conformance Requirement** in `AGENTS.md`. That
section tells you to state a control law and add conformance tests. This guide is
about the mistake that section does *not* catch: writing a control law that is
**correct but too narrow** — scoped to the one call site where you found the bug,
so a sibling boundary keeps violating the same invariant.

## The failure mode of control laws themselves

When you fix a bug, the tempting move is to describe the law in terms of *where
you were standing when you found it*:

> "The ignored-deliveries filter is applied at the read-only **ResolveNext**
> boundary before invalidity becomes fatal."

That reads like a law. It is really a **description of one implementation**. The
actual invariant has nothing to do with `ResolveNext`:

> A stale/invalid delivery in the shared store must never block resolution of an
> unrelated delivery — at **any** read-only resolution boundary.

The two sound the same until a second boundary shows up. `ResolveRecovery` scans
the same shared store, so it is bound by the same invariant — but the law named
`ResolveNext`, the recovery path got no conformance test, and it shipped
fail-closed. A field session then hit exactly that: one abandoned, *already
ignored* delivery blocked recovery of a completely healthy feature. The code was
"fixed" months earlier; the fix just never reached the second door into the same
room.

**A control law scoped to a call site is a latent bug at every other call site
that crosses the same boundary.**

## The rule

> State the law over the **invariant and its failure class**, then enumerate
> **every boundary** where that class can occur. The fix is done when a
> conformance test guards the law at each of them — not when the reported symptom
> stops reproducing.

## Method: four questions before you call a fix done

1. **What is the invariant, with no function name in it?**
   Rewrite your law until it names only actors, state, and effects — never a
   specific function. If you can't remove the function name without the sentence
   going false, you have described an implementation, not a law.

2. **What is the failure *class*, not the failure instance?**
   "recovery-status blocked on `agentic-l3-full`" is an instance. "a read-only
   resolver fails closed on an unrelated invalid delivery" is the class. Fix the
   class.

3. **Which boundaries cross this invariant?**
   Grep for the shared resource, not the symptom. Here the resource was the
   delivery-state store; every function that scans it (`ResolveNext`,
   `ResolveRecovery`, and the strict mutation enumerator `ActiveManagedDeliveries`)
   is a boundary the law touches. Enumerate them explicitly — in the law's comment
   or the conformance file header — so the next reader sees the full set.

4. **Does each boundary get the treatment its role demands?**
   The same invariant resolves differently by role. Read-only resolvers
   (`ResolveNext`, `ResolveRecovery`) **partition and tolerate**; the mutation
   boundary (`ActiveManagedDeliveries`) **stays fail-closed** so corrupt state
   can't be laundered into a write. "Account for every boundary" does not mean
   "apply the same branch everywhere" — it means each boundary has a *stated,
   tested* behavior under the law.

## How to write it down so it can't narrow again

- **One law, many boundaries, one name.** Keep a single `control-law: <slug>` and
  list its boundaries under it. Do **not** mint a second law for the second
  boundary — that fragments one invariant across two names and neither owner sees
  the whole set. (This is why the recovery fix *generalized*
  `stale-delivery-cannot-block-unrelated-feature` instead of adding a
  `recovery-cannot-block-...` twin.)
- **Co-locate the conformance tests.** All tests for a law live together (see
  `delivery_boundary_conformance_test.go`) with the boundaries called out in the
  header, so an incomplete boundary set is visible as a gap in one file rather
  than an absence spread across the tree.
- **Test the classes, per boundary.** For each boundary assert positive,
  negative, relation, and bypass conformance (per `AGENTS.md`). The recovery twin
  of each `ResolveNext` test is what would have caught the original miss.
- **Reference the law from every enforcing site.** Each function that upholds the
  law carries a `// control-law: <slug>` comment. A boundary that touches the
  shared resource but carries no such comment is your prompt to ask whether it,
  too, is in scope.

## Checklist

Before marking a boundary bug fixed:

- [ ] The law is stated with no function name in it.
- [ ] I fixed the failure *class*, not just the reported instance.
- [ ] I listed every boundary that crosses the shared resource/invariant.
- [ ] Each boundary has a stated behavior under the law (tolerate vs. fail-closed).
- [ ] Each boundary has conformance tests (positive/negative/relation/bypass).
- [ ] I extended the existing law rather than minting a near-duplicate.
- [ ] Every enforcing site carries the `control-law:` reference comment.

## Worked example: `stale-delivery-cannot-block-unrelated-feature`

| | |
|---|---|
| **Reported instance** | `recovery-status` blocked on the ignored, orphaned `agentic-l3-full` delivery while recovering an unrelated healthy PR. |
| **Failure class** | A read-only resolver fails closed on the *first* unreadable delivery in the shared store, before applying the operator's ignore policy. |
| **Invariant** | A stale/invalid delivery must never block resolution of an unrelated delivery at any read-only resolution boundary. |
| **Boundaries** | `ResolveNext` (tolerate) · `ResolveRecovery` (tolerate) · `ActiveManagedDeliveries` (fail-closed, by design). |
| **Original miss** | Law named only `ResolveNext`; `ResolveRecovery` had zero conformance tests and shipped fail-closed. |
| **Generalization** | Reworded the one law to cover *every* read-only resolution boundary; taught `allManagedDeliveryStates` to partition + ignore-filter like `scanManagedDeliveries`; added the recovery twins of the existing tests under the same law. |
