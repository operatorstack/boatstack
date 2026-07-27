### Read-only inspection pipelines work during recovery

While a workflow is blocked, an operator often wants to look before acting — pipe a status command into
a filter to read the part that matters. This was rejected. The guard treated any command with a pipe as
possibly changing state, so ordinary inspection such as `recovery-status | jq`, `git diff | wc -l`, or
`git status | sort | uniq -c` was denied, and falling off the read-only list re-entered the mutation
checks. Looking before acting was blocked at the moment it was most needed.

The guard now judges a pipeline by the effect of each stage. A pipeline is allowed when every stage is
read-only by effect — a reader, a Boatstack status command, or a pure filter that reads its input and
writes only to the next stage (`wc`, `awk`, `sort`, `uniq`, `cut`, `tr`, `jq`, and similar). Any stage
that changes state is still denied.

The dangerous forms stay denied, because they change effect, not because they contain a special
character: writing to a file with `>`, running a subcommand with `$(...)`, chaining a real command with
`;` or `&&`, or piping into a writer such as `tee`. So `recovery-status | jq .next_operation` is allowed
while `git status && rm -rf build` is not.
