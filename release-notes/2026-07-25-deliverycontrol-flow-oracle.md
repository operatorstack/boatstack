### Flow-navigation oracle and regret meter over the delivery graph (shadow, read-only)

The delivery transition registry now projects into a costed graph, and a deterministic
shortest-path oracle scores it: given a start and a goal delivery state it returns the lowest-cost
sequence of moves, or reports the pair as unresolved rather than guess a route it cannot prove. A
session's actual moves can be recorded as an append-only trajectory and measured against that oracle,
yielding the navigation cost of the walk, the oracle's cost for the same endpoints, and the regret
between them — with a blocked mutation priced as friction, matching the costed-graph design note.

This is shadow-only and best-effort: the trajectory recorder never changes a command's behavior or
exit code, is disabled by a kill switch, and nothing consumes the meter at runtime. It is the
measurement substrate for a future flow-navigation advisor, and a replay test pins the meter to the
worked incident in the design note.
