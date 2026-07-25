### Flow control steers committed mutations away from proven friction (on by default, kill switch)

Committed delivery mutations now pass through a flow-control choke point before their handler runs.
When the delivery graph proves a move is friction — illegal from the current state, so the real state
machine would reject it anyway — the controller pre-denies it and points at the low-cost next move
instead of letting the attempt fail with a bare error. This is on by default and reversible with the
`BOATSTACK_FLOW_CONTROL=0` kill switch, which restores the prior behavior exactly.

Enforcement is deliberately conservative. It acts only on moves whose graph legality provably matches
the real guard — publishing before the review gate, and recording a review gate before the test gate —
so a pre-denied move is always one the state machine would have rejected regardless: guidance and
telemetry change, outcomes do not. It never acts when the flow position is unresolved, never touches
recovery or read-only verbs, and leaves mode-sensitive or re-entrant moves entirely to their existing
handlers. Every guarded attempt is recorded to the shadow trajectory log, so the friction the model was
built to measure is now measured on the real forward path.
