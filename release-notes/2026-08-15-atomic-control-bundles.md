### Bind execution contexts to one repository control bundle

Boatstack now fingerprints the runtime pin, project configuration, host skills,
Flow sources, locks, assets, artifacts, and generated entry skills as one
control bundle. Runtime changes and worktree transfers verify the complete
source and target bundles before committing state or lineage.

Workspace creation now resolves its base to one commit and checks out that
exact revision. Generated entry skills also use the repository-pinned release
tag, so changing only the Boatstack build version no longer changes skill
bytes.
