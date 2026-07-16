<!-- Generated from operatorstack/intelligence-flow. Edit the upstream product-loop source, not this file. -->

# Walkthrough: account recovery in a passwordless product

This sanitized real-world sequence shows why Boatstack asks questions before it turns a request into code.

## Intent collides with repository reality

The product request was:

```text
Add a password reset button on the homepage.
```

Minimal repository inspection found passwordless email-code authentication, no password reset route, and copy promising that users needed no password. A literal implementation would have produced a button for a capability that did not exist.

`/auto-plan` therefore stopped and asked two product questions in plain text:

```text
Q-1  Clarify email-code recovery, introduce passwords, or choose another behavior?
Q-2  If passwords are introduced, do they replace email codes or sit alongside them?
```

The human chose password authentication alongside the existing passwordless flow. Those responses became `ANSWERED`; the repository facts were `DISCOVERED`. Boatstack did not treat its own recommendation as an answer.

## Approval turns the choice into a bounded change

The refined plan kept passwordless login, added password login and recovery routes, preserved passwordless signup, updated misleading copy, and required route and authentication tests. `/plan-gate` displayed the exact scope, non-goals, operational redirect gap, and fingerprint. An explicit `approve` created only `approval.md`.

After the host entered its execution-capable mode, `/build` activated that exact plan and implemented the feature. The targeted suite initially passed.

## Review falsifies a completion claim

`/review-gate` inspected the actual diff and found that the reset screen accepted any authenticated session as proof of password recovery. A normally signed-in user could reach a form intended only for a recovery event.

The gate returned `BLOCKED`. The implementation was repaired to unlock the form only for the recovery event, a regression test was added, and `/review-gate` then passed with the separate operator redirect gap still explicit.

## Shipping respects repository boundaries

At `/ship-gate`, a pre-push type check failed in code unrelated to the approved feature. The correct response is to prove whether the failure exists on the base branch and then either:

1. repair it in a separate PR; or
2. use a repository-policy bypass only with explicit human authorization and recorded evidence.

Changing unrelated code in the feature branch would silently widen the approved scope.

## What this demonstrates

```text
vague intent
  -> discover conflicting repository fact
  -> ask the product owner
  -> approve one observable slice
  -> build freely inside that boundary
  -> let evidence force a local repair
  -> keep unrelated repository failures outside the feature
```

The value did not come from a larger prompt. It came from preserving the original intent, separating discovered facts from human decisions, and requiring the implementation to survive an evidence boundary before shipping.
