<!-- Generated from operatorstack/intelligence-flow. Edit the upstream public source, not this file. -->

# Example: account recovery in a passwordless product

**For:** someone who wants to see why Boatstack asks questions before code.
**Outcome:** follow a real product conflict through decision, approval, repair, and PR preparation.

This is a sanitized product-repository sequence. It demonstrates observed behavior, not a benchmark claim about Boatstack's overall performance.

## The request conflicts with the product

The request was:

```text
Add a password reset button on the homepage.
```

The repository used passwordless email-code authentication, had no password-reset route, and promised that users needed no password. A literal implementation would have created a button for a capability that did not exist.

Boatstack therefore stopped and asked:

```text
Q-1  Clarify email-code recovery, introduce passwords, or choose another behavior?
Q-2  If passwords are introduced, do they replace email codes or sit alongside them?
```

The human chose password authentication alongside the existing passwordless flow. Repository facts were recorded as discovered; only the human responses became answered decisions.

## Approval defines the change

The revised plan kept passwordless login, added password login and recovery routes, preserved passwordless signup, updated misleading copy, and required route and authentication tests. It also kept an operational redirect gap visible rather than implying it was solved.

The plan gate displayed the outcome, exclusions, decisions, checks, gaps, and exact fingerprint. The human replied `approve`. No product code changed until the host entered its execution mode and build activated that approved plan.

## Review finds what the tests missed

The targeted suite initially passed. Review then found that the reset screen accepted any authenticated session as proof of password recovery. An ordinarily signed-in user could reach a form intended only for a recovery event.

Boatstack blocked progression. The implementation was repaired to unlock the form only for the recovery event, a regression test was added, and review passed with the separate operational gap still visible.

## Shipping keeps unrelated work separate

At ship time, a pre-push type check failed in code unrelated to the feature. The correct choices were to prove it existed on the base branch and then either repair it separately or use a repository-policy bypass with explicit human authorization.

Silently editing unrelated code would have widened the approved feature and made the PR harder to review.

## What the sequence shows

```text
vague request
  -> discover a product conflict
  -> ask the person responsible
  -> approve one clear change
  -> build
  -> let evidence force a repair
  -> keep unrelated failures outside the feature
```

The safeguard behavior is covered by planning, approval, review, and PR-projection tests. Whether the complete Boatstack workflow improves product-delivery success remains a separate paired evaluation.

Next: [install and ship a first feature](getting-started.md) or read [why these steps exist](why-these-steps.md).
