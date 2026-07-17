<!-- Generated from operatorstack/intelligence-flow. Edit the upstream public source, not this file. -->

# Why Boatstack has these steps

**For:** anyone who wants to see the work behind Boatstack's safeguards.
**Outcome:** understand what was observed, what Boatstack now does, and what has—or has not—been proven.

Boatstack was not designed by writing a long list of ideal engineering practices. Its safeguards were traced from benchmark trajectories and two product repositories, then turned into behavior that can be inspected and tested. This page keeps three different kinds of evidence separate:

- **Observed:** the problem appeared in recorded work.
- **Verified:** Boatstack's implementation behaves as stated in automated tests.
- **Still being evaluated:** the safeguard exists, but its effect on overall product-delivery outcomes has not yet been established.

Those labels prevent an implementation test from being presented as proof that the whole product improves engineering performance.

## Human decisions

**What happened.** A product request asked for a password-reset button in a passwordless product. A literal implementation would have created an interface for a capability that did not exist. Repository inspection could discover the conflict, but only a human could choose whether to introduce passwords or preserve the existing model.

**What Boatstack does.** Material product choices remain open until a human answers them. The reviewed plan is fingerprinted, explicit approval is recorded, and any subsequent planning change makes that approval stale.

**How we check it.** Planning, approval, and stale-plan tests verify that unanswered decisions block progress, approval cannot be inferred from silence, and changed inputs cannot reuse an old approval.

**Status:** observed in a sanitized product workflow; enforcement verified in automated tests. This does not yet quantify a change in feature success rate.

## Validation provenance

**What happened.** Terminal-Bench experiments showed that stronger self-verification wording and same-model repair did not reliably create truth. In another experiment, model-authored tests helped a development slice while the frozen evaluator had low fidelity, and the apparent gain did not transfer to the full board.

**What Boatstack does.** Every acceptance criterion must name a validation procedure and explain what makes that procedure meaningful. A test written with the implementation remains useful evidence, but it is not silently promoted into an independent source of truth.

**How we check it.** The plan compiler rejects uncovered criteria, incomplete validation records, and checks attached to work that does not serve the claimed outcome.

**Status:** experimental problem observed; compiler behavior verified. The best validation mix for different product risks remains an open evaluation question.

## Irreversible operations

**What happened.** After a partial database-schema apply failed, an agent introduced a reset path that could drop the public schema. The human stopped execution and the capability was removed in favor of target checks and transactional or fix-forward behavior.

**What Boatstack does.** Project hooks deny high-confidence destructive operations before execution and require read-only diagnosis after an external-write failure. There is no in-session bypass.

**How we check it.** Host-event fixtures cover direct and indirect destructive commands, malformed events, missing helpers, and safe controls. A blocked command must not create its sentinel side effect.

**Status:** incident observed and enforcement verified in fixtures. The net benefit, false-denial rate, and host coverage are **still being evaluated**; the safety documentation keeps that limitation visible.

## Reviewer-ready PR

**What happened.** Ordinary generated PR summaries described edited files and test commands but omitted important product decisions, accepted gaps, review findings, rollout, and rollback context accumulated during the feature.

**What Boatstack does.** At ship time, Boatstack projects the approved intent, committed diff, recorded evidence, decisions, gaps, rollout, and rollback into a reviewer-first preview. The human sees the exact title and body before GitHub is changed.

**How we check it.** Tests cover managed and ad-hoc branches, evidence-limited wording, stale previews, conditional risk sections, and explicit open/update confirmation.

**Status:** product-workflow problem observed; projection behavior verified. Reviewer speed and acceptance quality still need blinded product-delivery evaluation.

## What the experiments do and do not support

The current research covers thousands of locally available benchmark result records, preregistered comparisons, product-repository studies, and targeted trajectory inspection. It supports the mechanisms that Boatstack is designed to address. It does **not** yet support a claim that Boatstack improves feature success, cost, or delivery speed.

The next product evaluation compares the same model, task, budget, and coding host with and without Boatstack on a feature-building benchmark. Until that result exists, the homepage describes implemented behavior and evidence lineage—not performance uplift.

Technical readers can inspect the [research and design record](research-and-design.md), [validation model](validation-and-evidence.md), [safety evaluation status](safety.md), and [benchmark corpus audit](benchmark-corpus-audit.md).
