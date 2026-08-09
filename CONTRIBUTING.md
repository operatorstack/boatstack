# Contributing

Boatstack is developed directly in this repository. Propose runtime, workflow, documentation, test, and presentation changes here.

Every pull request must pass the cross-platform runtime checks and the repository contract. Review tests, adapter changes, public claims, and context-size changes with the product diff.

Repository-specific examples and outcome reports can be proposed here as new evidence. A failure becomes a durable move only after its mechanism and non-regression gate are documented.

## Public-facing changes

Any user-facing upgrade must state the user problem, supporting observation or requirement, current evidence status, and the README or guide it changes. If no public document changes, explain why the behavior is internal. Material public claims must appear in `docs/public-claims.json` and link to a readable explanation.

Every Boatstack pull request must add one release-level Markdown fragment under `release-notes/`. Name it `YYYY-MM-DD-<slug>.md`, begin with a level-three heading, and describe user impact rather than commits, diffs, or test commands. Fragments are append-only after merge; publish a new correction fragment instead of rewriting history.

Write each fragment in Simplified Technical English, the same standard the README follows. Keep sentences short. Use the active voice and the present tense. State one idea per sentence, put the condition first, and choose the simple, common word. Write for a reader who translates or skims the note.

Use Huashu Design for README and beginner-guide review when it is installed. The portable requirements remain in [the public-surface contract](docs/public-surface.md): plain outcomes first, one dominant product journey, progressive disclosure, accessible assets, no invented proof, and explicit separation between verified behavior and outcomes still being evaluated.
