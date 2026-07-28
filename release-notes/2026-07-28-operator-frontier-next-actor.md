### Next steps now name their actor, so status replies never assign you the agent's work

Every prescribed next step is now typed by who performs it. A step belongs to
the operator only when it owes operator knowledge or authority — an approval,
a publish or cleanup reply, a feature choice, a source-plan path, a correction
fact. Every other step belongs to the agent, including steps whose evidence
the agent produces by doing the work, such as test runs and plan checks.

The rendered response marks agent-owned steps with "This step is mine to do"
and offers a single delegation key: reply `g` and the agent executes the step,
re-renders, and continues until the next step reaches the operator frontier.
A working response may no longer end by describing work the agent still has
to do; when a step repeats without progress the agent stops and reports the
block instead of looping. `flow next --json` exposes the typing as
`next_actor`, and the classifier fails closed to the operator, so a step it
cannot place behaves exactly as before.
