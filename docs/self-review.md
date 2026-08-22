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
  and `.github/codex/review-output-schema.json` (output contract), admitted
  from the pull request base revision — by the local loop and by CI alike.
  A branch that changes the policy assets is therefore reviewed under the
  currently-admitted policy, and the change governs only after merge;
  `resolve` surfaces a policy note when the worktree assets have drifted
  from the admitted ones. The exact admitted bytes, the round bound, the
  stall window, the priority weights, and the blocking boundary are hashed
  into the program fingerprint; any change stales every prior prescription
  and receipt.
- **Blocking boundary:** priorities P0 and P1 block; P2 and P3 are
  residuals. Convergence is deterministic on this boundary, not on the
  verdict wording: a candidate converges exactly when its blocking measure
  is zero, and an open blocking finding keeps the loop running even under a
  "patch is correct" verdict. Residual findings are recorded with the round
  (and travel into the sealed receipt's round record) but never demand
  another round. A "patch is incorrect" verdict with zero findings is an
  invalid candidate.
- **Convergence measure:** each finding weighs by priority (P0 1000, P1 100,
  P2 10, P3 1). The stall and round bounds run over the blocking measure
  only, so residual churn can neither drive rounds nor trigger escalation:
  the loop escalates on the third consecutive submission that fails to
  decrease the blocking measure, or after sixteen rounds in one generation.
  The bounds are calibrated against mined review history
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
3. Fix the recorded blocking findings, commit, and repeat. The blocking
   measure must trend down; convergence requires a fresh review of the fixed
   tree with no open P0/P1 finding. Residual P2/P3 findings do not need to
   be fixed for convergence — they are recorded for the user to weigh.
4. `boatstack-reviewer seal` — verifies the full receipt (round trajectory,
   kernel receipt chain, final review bytes) locally, archives it in the
   local store, and writes only a minimal attestation to
   `.github/reviews/<instance>.receipt.json`: the reviewed tree and the
   program fingerprint — the two facts CI cannot re-derive. Commit that file
   with the pull request; pushing can wait until you are ready. The receipt
   directory is excluded from the tree binding, so committing the
   attestation does not invalidate it.
5. `boatstack-reviewer show` prints a recorded review itself — the exact
   archived findings of the latest round (`--round <n>` for earlier ones) and
   any staged, not-yet-submitted candidate — without resolving or changing
   anything. `status`, `reopen --actor <name>` (human capability, after
   escalation or to re-review a settled generation), and `recover --actor
   <name>` (after an interrupted effect) complete the surface.

## Yield skill workflows

Two [Yield](https://yield.operatorstack.systems/) workflows wrap this surface
so any registered coding agent drives the loop through recorded, resumable
operations instead of remembering the command order (adapters are registered
for Cursor, Codex, and Claude Code under their skill directories):

- `skills/self-review` — report-only: builds the reviewer from the current
  tree, resolves the control state, has the agent review exactly the
  admitted range under the admitted schema, submits, and reports the
  recorded verdict in the conversation — plus the titles of any residual
  P2/P3 findings when the round converged. It changes no code, verifies
  afterwards that no tracked file changed, and never seals, commits, or
  pushes.
- `skills/self-review-solve` — drives to convergence: fixes open blocking
  (P0/P1) findings in code (committed by the agent), re-reviews the fixed
  tree, repeats within a bounded attempt budget, then seals and commits the
  minimal attestation locally. Residual P2/P3 findings are never fixed by
  the loop; they are listed in the completion payload for the user to
  decide about. It never pushes; pushing is the user's decision. An
  escalated loop asks the human before reopening.

Run either with `.yield/bin/yskill run 'skills/<name>'` from the repository
root. `yskill doctor 'skills/<name>' --test` exercises each workflow against
a scratch repository (fixture-created, sentinel-switched), so testing never
touches real review state.

CI (`.github/workflows/review-verified.yml`) rebuilds the verifier and runs
`boatstack-reviewer verify --dir .github/reviews --base <base> --head <head>`,
which re-admits the policy from the base revision, recompiles the program
fingerprint, recomputes the receipt-excluded head tree, and checks that the
committed attestation names exactly those two facts. Nothing else travels
with the pull request: the attestation is deliberately minimal because the
program fingerprint already hashes the prompt bytes, the schema bytes, the
round bound, the stall window, the priority weights, and the blocking
boundary.

Changing any admitted policy asset or bound produces a new program
fingerprint, so existing local review instances become stale. That is the
expected one-time step after such a change: `boatstack-reviewer reset
--confirm` archives the stale instance, and the next review starts fresh
under the new program identity.

## What the attestation does and does not prove

Like work-package verification, the committed attestation is honest about
its boundary: it proves a review program with the base-admitted identity
sealed a convergence over the exact bound tree. The full evidence — round
trajectory, kernel receipt chain, final review bytes, and the honesty
markers `semantic_correctness: not-evaluated` and `origin_authenticity:
not-proven` — is verified in full at seal time and archived in the local
store, not in the commit. The attestation does not prove the review was
semantically right, and it does not prove who performed it. Branch
protection and human judgment remain the authority for merging.

Local review state, including the archived full receipt, lives under
`.git/boatstack-review/<instance>/` and never enters a commit.
