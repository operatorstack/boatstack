### Writing about a command is no longer treated as running it

The guard used to scan a file write's entire body with the same text rules it applies to shell commands. Saving a runbook that mentions `terraform destroy`, a policy note about `git push --force`, or documentation that names `.git/boatstack/` paths was denied as if the words were the act.

Written content is now graded as data: a file-writer tool call is judged by its name and structural fields — the target path, destinations — never by prose inside the document body. The boundary itself is unchanged. The same strings still block when they are a live command, a SQL executor's arguments are still executed capability, and a write that targets managed runtime state is still denied no matter what the body says: redaction can never launder the target.

This extends the executed-effect philosophy — grade what a call does, not what its text mentions — from shell commands to host file tools, and removes the largest remaining source of false-positive denials during ordinary documentation work.
