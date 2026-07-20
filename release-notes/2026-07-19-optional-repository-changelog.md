### Keep a readable repository changelog

Repositories can now opt into changelog maintenance with
`workflow.maintain_changelog`. When enabled, Boatstack requires each managed
delivery slice and Boatstack-prepared ad-hoc PR to add a categorized entry under
`CHANGELOG.md` → `Unreleased`, giving readers a useful history without requiring
them to inspect PRs or delivery artifacts. Existing repositories remain unchanged
unless they enable the policy, and Boatstack installation and update PRs are
exempt.
