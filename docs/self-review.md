# Self-review: the supervisory-control review loop

Boatstack reviews its own pull requests with a control program built on the
domain-neutral kernel: `boatstack-reviewer`
(`boatstack/cmd/boatstack-reviewer`). The loop runs locally — normally driven
by a coding agent — and CI verifies the result deterministically. No reviewer,
model, or API key runs in CI.

## Control story

**Boundary:** a pull request head enters review-verified state.

**Control law:** a head is review-verified only when a sealed receipt binds
its exact receipt-excluded tree, produced by the review program whose policy
assets are admitted at the pull request base revision, through a kernel
receipt chain that ends in convergence.

The proposer (agent or human) is untrusted. It produces candidate findings
under the admitted review policy; the reviewer owns admissibility, freshness,
the convergence measure, receipts, and recovery through the kernel's
resolve/apply relation.

- **Modes:** `unreviewed` → `findings-open` → `converged` (the only marked
  mode), with `escalated` for bounded non-convergence.
- **Admitted policy:** `.github/codex/review-prompt.md` (review instructions)
  and `.github/codex/review-output-schema.json` (output contract). Their exact
  bytes, the round bound, the stall window, and the priority weights are
  hashed into the program fingerprint; any change stales every prior
  prescription and receipt.
- **Convergence measure:** each finding weighs by priority (P0 1000, P1 100,
  P2 10, P3 1). A submission that fails to decrease the measure extends a
  stall run; the loop escalates on the third consecutive non-improving
  submission or after sixteen rounds in one generation. The bounds are
  calibrated against mined review history
  (`boatstack/cmd/boatstack-reviewer/testdata/review_rounds.json`).
- **Freshness:** every submission binds the exact committed tree it reviewed.
  A dirty worktree, a new commit, or an edited candidate refuses admission
  instead of recording a stale round.

## Driving the loop (agent entry point)

Work on a branch, commit your change, then:

1. `boatstack-reviewer resolve --actor <name>` — prints the control state and
   the instructions: the admitted prompt path, the exact review range
   (`merge-base..HEAD`), and the output contract. Perform the review the
   prompt describes over exactly that range and write the findings JSON to a
   file, conforming to the output schema.
2. `boatstack-reviewer submit --findings <path> --actor <name>` — the
   reviewer validates the candidate (schema, diff anchors, tree binding) and
   the kernel commits one transition: findings recorded, converged, or
   escalated. Refusals name their reason and record nothing.
3. Fix the recorded findings, commit, and repeat. The measure must trend
   down; convergence requires a fresh review of the fixed tree whose verdict
   is `patch is correct`.
4. `boatstack-reviewer seal` — writes
   `.github/reviews/<instance>.receipt.json`. Commit that file with the pull
   request. The receipt directory is excluded from the tree binding, so
   committing the receipt does not invalidate it.
5. `boatstack-reviewer show` prints a recorded review itself — the exact
   archived findings of the latest round (`--round <n>` for earlier ones) and
   any staged, not-yet-submitted candidate — without resolving or changing
   anything. `status`, `reopen --actor <name>` (human capability, after
   escalation or to re-review a settled generation), and `recover --actor
   <name>` (after an interrupted effect) complete the surface.

CI (`.github/workflows/review-verified.yml`) rebuilds the verifier and runs
`boatstack-reviewer verify --dir .github/reviews --base <base> --head <head>`,
which re-admits the policy from the base revision, recompiles the program
fingerprint, recomputes the receipt-excluded head tree, and checks the kernel
receipt chain and the final review bytes.

## What the receipt does and does not prove

Like work-package verification, the sealed receipt is honest about its
boundary: it proves the declared review program ran to convergence over the
exact bound tree under the admitted policy. It does not prove the review was
semantically right, and it does not prove who performed it — the receipt
records `semantic_correctness: not-evaluated` and `origin_authenticity:
not-proven`. Branch protection and human judgment remain the authority for
merging.

Local review state lives under `.git/boatstack-review/<instance>/` and never
enters a commit.
