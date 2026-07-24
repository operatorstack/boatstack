### Boatstack can attach visual evidence to a pull request on its own

Until now, captured screenshots reached a pull request only through a signed-in host
browser or a manual drag-drop — Boatstack recorded the evidence locally but left the
last step to a human. GitHub exposes no public API that mints its user-attachments
CDN URLs, so there was a real browserless gap.

`publish-pr` now closes it for public repositories. When it opens or updates a PR,
Boatstack commits the exact captured PNG bytes to a dedicated, Boatstack-owned
evidence branch on `origin` (built with Git plumbing against a temporary index, so it
never disturbs the working tree), then posts or updates one Boatstack-owned comment
that renders each scenario from an immutable `raw.githubusercontent.com` URL pinned to
the evidence commit. The comment carries the same trust fingerprints as the manifest
— source commit, product diff, and manifest fingerprint — and repeats the standing
warning that public-branch screenshots are publicly accessible.

The publisher is idempotent: a recorded comment is reused, and a lost comment URL is
recovered by a hidden marker so an update never orphans a duplicate. It engages only
when it can actually render — a GitHub origin, `gh` authenticated, and a public
repository. For a private or non-GitHub repository the existing manual-attachment
fallback stays in force, so a suggest-policy PR is never blocked by a limitation
Boatstack cannot overcome. On any publication failure the flow fixes forward: the PR
is preserved and the manifest records the pending state instead of losing evidence.
