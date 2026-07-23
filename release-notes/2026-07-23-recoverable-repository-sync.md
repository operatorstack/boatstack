### Keep repository administration out of product delivery

Boatstack now routes branch synchronization and dirty-worktree cleanup through one agent-agnostic, recovery-backed workspace operation instead of auto-plan or repair. Raw destructive Git remains denied; the new helper preserves the original branch and local changes under verified Git refs before alignment.
