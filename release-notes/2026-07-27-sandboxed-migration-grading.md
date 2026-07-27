### Migrations are graded by running them, not by reading them

The guard treats a committed migration as data. A migration file, however destructive its SQL looks,
does not change any database by sitting in a diff; the deploy pipeline applies it later. So the guard
allows it, which is why committing a migration no longer blocks work. The open question was how to catch
a migration that is genuinely unsafe, since its text alone cannot tell you.

This release adds effect grading. A project can declare how its migrations are applied and verified. The
grader runs those commands against a fresh, disposable database, then reads the result: the migration
passes only if it applies and the verification holds, and fails otherwise. The database is provisioned
per run and torn down after, so the effect is real but contained. A project that declares no such
commands is unaffected — grading is skipped.

This puts the judgment where it belongs. The static guard never guesses a migration's effect from its
text; the effect is observed by executing it in a sandbox, the same way the deploy pipeline would. A
safe forward change grades clean, and a change that drops a populated table is caught — the exact case
the text-only guard cannot and should not decide.
