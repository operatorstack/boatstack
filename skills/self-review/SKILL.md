---
name: self-review
description: "Run the Boatstack supervisory-control self-review for the current branch and report the verdict without changing code."
---

Run one review round for the current branch against `origin/main` and report
the recorded verdict — nothing else. This skill is report-only: the review is
performed read-only, `boatstack-reviewer` admits or refuses the candidate,
and the round is recorded in the repository's local review store. It never
seals a receipt, never commits, and never pushes; when the run completes,
report the verdict in the conversation and stop. Sealing and committing
belong to the `self-review-solve` skill or an explicit user request.

Convergence is decided by the blocking boundary: only P0/P1 findings block.
A converged round may carry residual P2/P3 findings; the skill reports their
titles for the user to weigh — they are recorded data, not demanded work.

Run from the repository root:

    .yield/bin/yskill run 'skills/self-review'

If `.yield/bin/yskill` is missing, install the pinned runtime first:

    go install github.com/operatorstack/yield/cmd/yskill@v0.1.38 && yskill init skills/self-review --language go

Follow each returned operation exactly. Answer it directly:

    .yield/bin/yskill respond <run-id> --value <answer> --skill 'skills/self-review'

Do not skip an operation or invent a response. When the run asks for the
review (the `review` agent task), read the prompt file it names, review only
the committed range it names, and return only the schema-valid JSON object.
