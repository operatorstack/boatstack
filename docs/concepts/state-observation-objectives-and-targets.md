# State, observation, objectives, and targets

## Definitions

These values answer different questions:

| Value | Question |
| --- | --- |
| Objective | What external intent is being controlled? |
| Target | What accepted completion condition does this entry pursue? |
| Observation | What does the domain currently report? |
| Control state | What has the supervisor durably committed? |
| Domain state | What domain-owned facts exist outside the generic kernel? |
| Evidence status | Which observations have passed their declared checks? |

An Objective is external reference data. Only its exact identity, revision, and
fingerprint enter control state as an ObjectiveBinding.

## Control boundary

The general kernel stores control-instance identity, program identity, exact
objective binding, control mode, revision, and any recovery obligation. It does
not store software-delivery plans, worktrees, gates, publication state, or
other domain state.

## Invariants

- Observation is canonical input, not a committed fact by itself.
- Changing objective intent requires a new revision and explicit binding.
- A target is a predicate, not a hard-coded mode or command name.
- Domain state may change independently and must be freshly observed.
- Verification, not the effect implementation, accepts the postcondition.

## Lifecycle

Resolve loads committed control state and obtains a fresh observation. Apply
repeats both under the lock. The program-defined target determines marked
completion; otherwise the relation may continue, refuse, block, or recover.

## Current implementation anchors

- [Kernel types](../../boatstack/kernel/types.go)
- [Kernel program](../../boatstack/kernel/program.go)
- [Domain-neutral fixture](../../boatstack/kernel/conformance/integer.go)
