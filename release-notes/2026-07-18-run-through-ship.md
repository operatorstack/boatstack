### Run a verified feature through ship

Run `$boatstack run` in Codex or `/boatstack-run` in Cursor and Claude Code to drive a small verified feature through every declared delivery slice. Boatstack fetches `origin` and blocks on stale or diverged Git state before mutation, reuses the existing build and evidence gates, retries up to three same-intent repair cycles per slice, and pauses for plan approval, product decisions, and exact PR publication confirmation. It opens or updates reviewer-ready PRs but never merges or deploys.
