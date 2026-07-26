### Detached Supervision can guide and guard a live coding session

Detached Supervision keeps Boatstack's controller state outside the repository. A
coding agent working in a detached repository cannot read the in-repo references it
would normally rely on, and a guard that runs for every repository must not control
the ones you never attached. This change adds the two pieces that let a live session
run under detached supervision.

First, a bounded context projection. `boatstack-helper context --repo .` returns a
small, read-only view of the current supervisory position for the operation about to
run: the ownership mode, the attached repository and its verified delivery state, the
active slice, the recommended next operation, and the exact next command. It reuses
Boatstack's authoritative resolver and deterministic next-move oracle rather than
asking the agent to reconstruct the workflow, so the guidance is identical to
embedded mode. An attached repository whose binding no longer verifies reports a
blocked position with one recovery action instead of a normal one.

Second, a developer-level guard entry. `boatstack-helper ambient-safety-hook` runs
Boatstack's full safety policy only on managed repositories — those with a detached
attachment or an embedded install — and allows everything else with no Boatstack
decision. This lets a single user-level hook protect your attached repositories while
leaving every other repository you open completely uncontrolled. On a managed
repository the guard's decisions are exactly those of the in-repo guard.

To wire the guard, `boatstack-helper activate --repo .` prints the exact per-agent
developer-level configuration to add — the config location and the precise
Boatstack-owned snippet — for every supported coding agent. It never rewrites your
global host configuration silently, so activation is transparent and cannot clobber
your existing hooks.

Both surfaces are host-neutral and behave the same for every supported coding agent.
