### Source plans are supplied explicitly, never discovered

Boatstack no longer scans directories for source plans, so it never retains context for work that was never shipped.

- `auto-plan` requires the plan produced in the host conversation to be passed explicitly with `--plan <path>`; the `.product-loop/intake/` staging area and the `.cursor/plans`, `.claude/plans`, and `.codex/plans` scan roots are removed.
- A saved plan-mode file left in the repository can no longer become ambient context or block `next-status` with an ambiguity stop; with no started feature the state is simply `NOT_STARTED` pointing to `auto-plan`.
- The `--plan` path must be a durable in-repo file. A path outside the repository is rejected up front, because `source_plan_path` is hashed into the plan fingerprint and re-checked through build, and an out-of-repo file cannot stay committed and hash-current.
- The `source_plan_path` and `source_plan_sha256` invariant is unchanged: the plan must still be a real, present, hash-current file from `auto-plan` through `build`.
