### The guard now judges destruction by what a command does, not by words in a file

The safety guard decides whether an action is destructive. It used to decide by
reading text: it matched database keywords such as DROP, TRUNCATE, and reset
anywhere in a command, a file it named, or a tool input. Text is not an effect.
Committing a schema file that contains DROP does not drop anything. Git never runs
SQL. So the guard denied safe, routine work whose only fault was that a file or a
message spelled a keyword.

Three everyday steps were blocked by mistake. Committing a generated schema dump —
a file that is legitimately full of DROP and DDL — was read as a live drop, so
`git add`, `git commit`, and even `git diff` on it were denied. Activating a
managed delivery scanned the committed diff and flagged the migration file the
same way, so a delivery that carried a migration could not activate. Editing a note
whose prose mentioned the keywords was denied too.

The guard now classifies by executor and effect. A database category applies only
when the command's executor actually runs SQL against a live database — a client
such as `psql` or `supabase`, a file that such a client or an interpreter
executes, or a tool that executes SQL. A file named as data by git, `cp`, or `cat`
is data, and its contents are never scanned. A committed migration or schema dump
in a delivery diff is a data artifact, applied later by the controlled deploy
pipeline, so it no longer blocks activation. A note or source edit that merely
mentions a keyword is a document.

The real boundary is unchanged. Running destructive SQL against a live database is
still denied: `psql -c "DROP SCHEMA public CASCADE"`, a client running a migration
file, an interpreter running code that issues DDL, and a live SQL tool are all
still blocked. Self-executing destruction that names its own executor — `rm -rf`,
`git reset --hard`, `terraform destroy`, `supabase db reset` — is unchanged,
including when committed into a script. Read-only status helpers may now be piped
for inspection, so ordinary compound commands work during recovery.
