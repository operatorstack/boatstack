# Runtime selection

Boatstack selects a runtime from repository-owned identity, not from a mutable
global current version.

`.boatstack/runtime.json` records the admitted version, SHA-256 digest, source
revision, compiled-program fingerprint, and state schema version. It contains no
machine path. The stable `boatstack` dispatcher finds this pin from `--repo` or
the current directory, derives the host-store path, verifies the exact bytes,
and executes only that runtime.

The default host store is:

- `$BOATSTACK_HOME/runtimes` when `BOATSTACK_HOME` is set;
- `$XDG_DATA_HOME/boatstack/runtimes` on Unix when `XDG_DATA_HOME` is set;
- `~/.local/share/boatstack/runtimes` on Unix otherwise;
- `%LOCALAPPDATA%\Boatstack\runtimes` on Windows.

Each artifact is stored as
`<version>-<sha256>/boatstack-runtime[.exe]`. Existing identity slots are never
overwritten. A missing, symlinked, non-regular, or digest-mismatched artifact is
an error; the dispatcher does not fall back to another installed version.

An update first verifies and durably installs the candidate. The Kernel then
admits `installation.update` or `installation.reconcile-update` and atomically
commits the new pin with its state and generated projections. Refusal leaves the
old pin active. Recovery rollback restores the prior pin. Installing another
runtime or replacing the dispatcher cannot change a repository's selection.

The pin is worktree-visible and portable across clones. A newly created managed
worktree receives the governed pin. Different repositories and worktrees can
therefore select different admitted versions from the same host store.

Runtime garbage collection is intentionally outside the update transition. A
future collector must prove that no repository pin references an artifact
before removing it.
