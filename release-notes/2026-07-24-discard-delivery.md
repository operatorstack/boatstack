### A stale delivery no longer blocks new work, and you can clear one

A single stale or abandoned managed delivery in Boatstack's local store used to
block *every* operation — including planning an unrelated new feature — because
one unverifiable delivery poisoned the whole status scan, and the
`ignored_deliveries` escape hatch was applied too late to help.

Two changes fix this:

- **Ignore is now honored before a stale delivery can block you.** `next-status`
  applies your `ignored_deliveries` list *before* reporting an unverifiable
  delivery, so an ignored delivery can no longer stop unrelated new work. A
  genuinely unverifiable delivery still blocks, but now names itself and points
  you at the fix. (Mutation and publish paths remain strict and fail closed on
  corrupt state.)
- **New `discard-delivery` command.** Run
  `boatstack-helper discard-delivery --feature <slug>` to clear a stuck or
  abandoned delivery so you can rebuild or re-plan it. It archives the state
  (reversible) and never touches your git history, merged PRs, plan, or lock. A
  delivery that has already published a slice is refused unless you pass
  `--force`.
