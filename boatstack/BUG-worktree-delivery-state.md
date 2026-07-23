# Bug: shipped features re-register as ambiguous plan candidates from worktrees

**Component:** `boatstack-helper` (labs/12-product-engineering-loop/product-engineering-loop)
**Observed in:** v0.7.45 (commit 91e33a95)
**Severity:** blocks `next-status` (BLOCKED / AMBIGUOUS) for any repo that has shipped >1 feature and uses worktree delivery mode.
**Goal of fix:** stop miscounting shipped features as unshipped, WITHOUT deleting the committed
`plan.md` / `plan.lock.json` / `pr.md` context. (A prune workaround was applied in taxweave PR #304 —
this fix should let that be reverted.)

## Symptom

From a fresh build worktree, `next-status --repo . --json` returns:

```
BLOCKED / AMBIGUOUS  "More than one saved feature plan is available"
```

listing every historically shipped feature as a candidate — in taxweave, 22 shipped features + the
1 genuinely-active feature = 23 candidates.

## Root cause

Two facts combine:

### 1. Delivery state is resolved against the worktree-local git dir

`deliveryStateDirectory` (delivery.go ~L250) uses `git rev-parse --git-dir`, which inside a linked
worktree returns the **worktree-local** dir, not the shared repo dir:

```
--git-dir          → <repo>/.git/worktrees/<name>          (per-worktree, ephemeral)
--git-common-dir   → <repo>/.git                           (shared, durable)
```

So delivery state is written to and read from:

```
<repo>/.git/worktrees/<name>/boatstack/deliveries/<slug>/state.json
```

Both `saveDeliveryState` and `deliveryStatePath` go through `deliveryStateDirectory`, so **write and
read both target the ephemeral per-worktree location.**

### 2. Shipping a feature deletes the worktree that holds its state.json

`workspace.cleanup_after = merge` removes the feature's worktree on ship. That deletes
`<repo>/.git/worktrees/<name>/…/state.json` along with it — i.e. **shipping a feature destroys its
own "I am shipped" record.** The committed artifacts (`plan.md`, `plan.lock.json`, `pr.md`) live in
the repo tree and survive forever.

### 3. The candidate check reads "planned + no state.json" as "unshipped"

`featurePlanCandidates` (next.go ~L42):

```go
if fileExists(<dir>/plan.md) && !fileExists(deliveryStatePath(repo, name)) {
    features = append(features, name)   // counted as an open candidate
}
```

Every shipped feature now matches (`plan.md` present, `state.json` gone), so from any new worktree
they all re-register as open candidates → ambiguity. `ignored_deliveries` (PR #301) filters the
*active-delivery* ambiguity path, NOT this feature-plan-candidate path.

## Evidence

- `git -C <worktree> rev-parse --git-dir` → `<repo>/.git/worktrees/wt-firm-status`
- `git -C <worktree> rev-parse --git-common-dir` → `<repo>/.git`
- The 22 blocked dirs each have `plan.lock.json`; 15 also have `pr.md`. The 1 legitimately-open
  feature (`firm-status-finance-aesthetic`) has neither.
- Precedent: `hooks.go` (L120, L175) already uses `--git-common-dir` for exactly this "shared,
  survives worktrees" reason. Delivery state is the outlier still on `--git-dir`.

## Proposed fix — Part B only (Part A REJECTED, see below)

### DECISION (2026-07-23): do Part B alone. Do NOT do Part A.

An earlier draft of this spec proposed Part A (move delivery state to `--git-common-dir`) **and**
Part B. Investigation in the source rejected Part A:

- `ActiveManagedDeliveries` (delivery.go:854) feeds `ClassifyCommand` push-denial
  (safety.go:222/269/543), `next`, `run`, and `pr`.
- Per-worktree isolation of delivery state is **intentional**:
  `TestManagedDeliveryStateDoesNotBlockUnrelatedWorktrees` (delivery_test.go:550) asserts an
  unrelated worktree neither sees another feature's active delivery nor has its pushes denied.
  Operations, by contrast, are deliberately shared/serialized (operation_test.go:267).
- Part A would make every active delivery visible in **all** worktrees — breaking that test and
  changing cross-worktree push-denial semantics.

Part A is also **unnecessary**: its only purpose was durability of the "shipped" record. Part B
derives shipped-ness from the **committed, durable** `plan.lock.json`/`pr.md` instead of the
ephemeral `state.json`, so the shipped record is durable by construction and the disappearance of a
shipped feature's `state.json` on cleanup becomes harmless. Do not move delivery state.

### Part B — treat locked/shipped dirs as not-candidates

Make `featurePlanCandidates` exclude any feature that is demonstrably past planning —
i.e. carries a `plan.lock.json` (locked/built) and/or `pr.md` (shipped):

```go
if !fileExists(plan.md) { continue }
if fileExists(plan.lock.json) || fileExists(pr.md) { continue } // shipped/locked, not an open plan
if !fileExists(statePath) { features = append(features, name) }
```

This is consistent with the existing `orphanedFeatureArtifacts` helper (next.go), which already
distinguishes `pr.md` + `plan.lock.json` presence. It also lets taxweave **revert PR #304** and keep
all the rich shipped-feature context in-repo.

`firm-status-finance-aesthetic` has no `plan.lock.json`/`pr.md`, so it correctly stays the single
open candidate.

## Status (2026-07-23): FIXED via Part B

`featurePlanCandidates` (next.go) now skips any feature dir carrying `plan.lock.json` or `pr.md`
before the `state.json` check, so locked/shipped features can no longer re-register as open plan
candidates. No change was made to `deliveryStateDirectory`, worktree isolation, or
`ActiveManagedDeliveries` / push-denial. Part A was **not** implemented (see rejection above).

## Tests added (`next_test.go`)

- `TestFeaturePlanCandidatesExcludesLockedAndShippedFeatures` — unit: a `plan.md`+`plan.lock.json`
  dir and a `plan.md`+`plan.lock.json`+`pr.md` dir (both without `state.json`) are NOT returned; a
  `plan.md`-only dir IS returned.
- `TestResolveNextIgnoresShippedFeatureCandidates` — reproduction: one open feature plus several
  shipped dirs resolves to the single open candidate (`DRAFT_PLAN` / `plan-gate`), not `AMBIGUOUS`.
- `TestResolveNextIgnoresShippedFeaturesFromLinkedWorktree` — worktree conformance: same result from
  a fresh linked build worktree (the exact reported symptom).
- `TestManagedDeliveryStateDoesNotBlockUnrelatedWorktrees` (delivery_test.go:550) — unchanged and
  still green, confirming delivery-state locality was not touched.

## Rollout

1. Land Part B + tests, cut a new helper version.
2. In taxweave: bump the helper, revert PR #304 (restore the 22 shipped feature dirs).
3. Confirm `next-status` resolves `firm-status-finance-aesthetic` as the single candidate.
