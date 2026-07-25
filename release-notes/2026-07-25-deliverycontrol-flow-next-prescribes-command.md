### `boatstack flow next` — the exact command, not just the next state

`flow next` now prints the exact command to run for the lowest-cost next move toward a published
delivery, not just the name of the next state. The command is resolved through the delivery registry —
the same single declaration the conformance and liveness checks hold faithful — so the verb it emits is
always a real command the binary accepts, and always the legal next transition the oracle chose.

Arguments that follow from state (the feature, the addressable slice, the gate, the preview path) are
filled in for you. Arguments that must come from a human or CI — the gate status, the evidence ledger,
the human-confirmed preview fingerprint, the reviewer identity — are listed explicitly as required and
are never fabricated: they appear as `<REQUIRED>` placeholders in the printed command, and the `--json`
form carries them in a separate `requires_human_input` field alongside an `auto_derivable` flag. When
the flow position cannot be placed, `flow next` prescribes nothing rather than guessing a command.

This turns the delivery flow from something the operator has to reassemble each step into a single
readable instruction: the next command is derivable from the recorded state, so the tool states it.
