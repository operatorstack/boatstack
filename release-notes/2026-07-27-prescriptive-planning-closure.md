### Planning stages now name their next command

Before activation, `flow next` used to go quiet: the delivery oracle deliberately does not model planning, so the advisory printed only a soft operation name and no runnable command. An agent authoring a feature plan could drift to raw file writes, land in INVALID_STATE, and loop between rewriting and `repair-state` — because the denial named only the cleanup verb, never the correct authoring channel.

Every pre-activation stage now prescribes its exact runnable command: `check-source-plan` when nothing is started, `check-plan` on a saved draft, `activate-plan` (or `workspace-cut`) once approved, and the matching recovery verb when state is invalid. The flow oracle itself is unchanged and still scores delivery stages only; prescriptions carry explicit `planning.`/`recovery.` markers that can never become delivery moves or auto-execute.

Plan-gate denials now also name the owned authoring channel: after `repair-state`, re-author planning Markdown through `boatstack-helper planning-write` on stdin. The skill and workflow guidance were inverted to match — `planning-write` is the primary writer for feature artifacts, not a fallback for blocked hosts — so the correct move is prescribed up front instead of being discoverable only by failing.
