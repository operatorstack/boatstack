### A blocked workflow is always sent to a recovery step that can clear it

When Boatstack blocks work at an invalid state, it names the next operation to run. Some of those
names could not help. The guard sent the operator to `repair-state` for problems `repair-state`
refuses — a valid saved plan, an orphaned PR preview, an unverifiable delivery, or a configuration
error. The prescribed step declined, and no other step advanced the state. The workflow was stuck
with no way forward inside the tool.

The rule now is that the step a blocked state names must be a step that accepts that state. Each cause
is routed to the recovery that clears it. A malformed unregistered draft still routes to `repair-state`,
which quarantines it. An orphaned preview, an unverifiable delivery, or invalid completed state routes
to `discard-delivery`, which archives the artifacts reversibly. A state that cannot be verified or a
broken configuration is a sensing fault, not a plant fault, so it routes to the read-only `doctor` to
diagnose the channel — no mutation step is asked to fix what it cannot.

`discard-delivery` is now admitted as a bounded recovery step at every stage, so a step the guard
names is always a step the guard permits. `discard-delivery` also clears an orphaned feature directory
that carries a preview but no plan lock, archiving it to a hidden sibling that is never re-scanned as
live work. The archive is reversible; committed history and merged pull requests are untouched.
