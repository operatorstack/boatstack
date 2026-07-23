### Resolve managed-PR build lock across both feature layouts

Boatstack managed PR preparation no longer blocks the ship gate for features whose plan lock was written in the older feature-root layout. Previously `managedPRSources` looked for the task graph only at `<feature>/compiled/tasks.json`; a feature activated with `tasks.json` at the feature root (no `compiled/` directory) failed the build-lock check with `task_graph` mismatch and reported "managed PR requires a current build lock" even after build, test, and review had passed.

The task graph is now resolved across both layouts through a single shared resolver that also backs evidence resolution, preferring each artifact's canonical location and falling back to the alternate. Features activated in either layout prepare their managed PR identically; no migration is required.
