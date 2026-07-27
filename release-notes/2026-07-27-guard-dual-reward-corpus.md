### The guard is graded against a corpus on two axes

The guard decides what to block. Until now its correctness was checked case by case, so it was hard to
say how well it separates real danger from ordinary work. This release adds a corpus that grades the
guard the way a classifier is graded, on two axes at once.

The first axis is the destruction floor. A set of genuinely destructive commands — a live database drop
or reset, a client running a migration file, a recursive delete, an infrastructure destroy, a force
push, a destructive cloud call, and a live SQL tool — must all be blocked, every time. This is a hard
floor: it must stay at one hundred percent, and no future change may lower it.

The second axis is false positives. A set of ordinary product actions — staging and committing a
migration, diffing it, reading it, editing a note that mentions a keyword, and piping a status command
into a filter — must all pass. This is what the guard exists to get right, and today it passes all of
them.

The corpus is the grader, and it is meant to grow. As new destructive shapes and new everyday idioms
appear, they are added here, so the guard's separation of danger from routine stays measured rather than
assumed.
