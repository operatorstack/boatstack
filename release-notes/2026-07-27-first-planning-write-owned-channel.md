### The first planning write goes through the owned channel

Until now the guard latched the planning tree only after a plan draft had registered. The very first raw write of a planning file — a host Write tool, a `cp`, a shell redirect — landed unchecked, and if it produced a malformed draft the workflow fell into INVALID_STATE and a repair loop before the agent discovered the owned `planning-write` channel.

The latch now covers the first write too. Any raw write into `.product-loop/features/` is denied even before a candidate exists, and the denial names the exact command that succeeds: `boatstack-helper planning-write` with the document on stdin. The deny is path-scoped — ordinary product writes are untouched — and the guard admits `planning-write` at that same stage, so the prescribed exit is always reachable.

This closes the loop the earlier prescriptive-closure release opened: planning guidance, denials, and now enforcement all point at the same single authoring channel, so the wrong first move is redirected immediately instead of discovered through failure.
