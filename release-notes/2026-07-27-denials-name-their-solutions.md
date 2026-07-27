### Denials now hand you the legal moves

A denial that only states the rule leaves an agent — especially a smaller model — retrying the same blocked call. Every guard denial now carries its computed solution set: a short "You can:" list of exact runnable commands that are legal from the position the denial describes, with owed human inputs marked. A plan-gate denial lists the planning channel commands for that stage; an operation denial lists the inspection commands; a protected-path denial additionally names the verbs that own the path, straight from the state-ownership map.

The picks are computed from the same declarations the guard enforces, and conformance sweeps keep the loop closed: every category of denial either enumerates picks or is a documented exception, and every pick passes the guard's own laws — the guard never hands out a command it would then deny.

The plain reason string carries up to three picks on every host; the full set rides on the opt-in structured payload, additively, under the same schema version.
