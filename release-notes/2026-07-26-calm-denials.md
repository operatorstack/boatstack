### Denials read as a guardrail, not a crash

A Boatstack denial used to arrive as one long red sentence. It looked like something
broke, even though nothing did. Every denial is now one message with a clear shape: a
`Blocked by Boatstack` badge, the reason, a line that tells you what happens next, and —
when the action was stopped before any change — a line that says nothing was written.

The same message renders to fit each surface. In a coding host, the denial reason is a
short, calm, multi-line note. In a real terminal — the CLI and the guard scripts — it is
a soft-coral badge instead of alarm red. When output is piped or captured, the plain text
is emitted unchanged, so logs and scripts are not affected.

Two settings control the terminal color: `BOATSTACK_COLOR` (`auto`, `always`, or `never`)
and the standard `NO_COLOR`. A new command, `boatstack-helper render-denial --demo`, prints
sample denials so you can preview the plain, Markdown, and terminal forms.

The denial text carries the same information as before, including the recovery command and
the machine markers that tooling matches. This change is presentation only; no gate,
authority, verification, or recovery behavior changes.
