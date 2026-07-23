### Stop miscounting shipped features as open plans

Boatstack no longer treats already-shipped or locked features as unresolved plan candidates. Previously, in repositories that use worktree delivery mode with `workspace.cleanup_after = merge`, shipping a feature removed the worktree that held its per-worktree delivery `state.json`, while the committed `plan.md` / `plan.lock.json` / `pr.md` survived. From a fresh build worktree, `next-status` then re-registered every historically shipped feature as an open plan and blocked with `AMBIGUOUS`.

The plan-candidate check now relies on the durable committed artifacts: any feature carrying a `plan.lock.json` (locked/built) or `pr.md` (shipped) is past planning and is excluded from open-plan candidates, regardless of whether its ephemeral `state.json` still exists. A feature with only `plan.md` correctly remains the single open candidate. Delivery-state locality and cross-worktree push-denial are unchanged.
