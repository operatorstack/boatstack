### Boatstack can now detect the instructions you keep repeating to your agent

A new offline analysis engine reads coding-agent transcripts — Claude Code session files, plain-text logs, or a neutral event format any tool can emit — and finds the operator instructions that recur across sessions. Repetition inside one conversation does not count; the signal is the same instruction shape appearing in session after session, because an instruction you keep restating is evidence the system is missing a typed control, not a prompt to be saved.

The engine is deterministic and capability-free by construction: no network, no subprocesses, no filesystem access, no clocks — its imports are conformance-tested to grant no I/O at all, and identical transcripts in any order produce identical results. Nothing is exposed to you yet; the user-facing `retro derive` command that turns detected recurrence into reviewable proposals arrives in the next update.
