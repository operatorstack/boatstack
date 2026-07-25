### `boatstack flow next --execute` — run only the moves that are provably safe

`flow next` already prescribes the exact next command toward a published delivery. It now takes an
opt-in `--execute` flag that lets the driver *run* that move for you — but only when the move is proven
safe: its arguments follow entirely from state, and there is nothing to fabricate. At the first move
that owes human input — a gate's status and evidence, a reviewer's identity, the human-confirmed PR
preview fingerprint — the driver prescribes-and-stops and hands you the exact command to run.

Execute is off by default; it happens only when you pass `--execute`, and `BOATSTACK_FLOW_DRIVE=0`
refuses execution even then. Today every forward delivery move owes human input, so the driver
correctly prescribes-and-stops at the very first step — the shipped auto-drive allowlist is
deliberately empty of forward moves. This release is the mechanism and its guardrails: a move runs only
if it is BOTH on the allowlist AND has an explicitly registered executor, so nothing runs by accident,
and the driver never invents evidence, a gate status, a fingerprint, or an identity.

The driver re-reads ground truth after each executed step, so it never acts on an assumed position, and
a step budget bounds the loop as a backstop against any cycle in the model.
