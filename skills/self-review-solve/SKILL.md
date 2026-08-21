---
name: self-review-solve
description: "Resolve the Boatstack self-review: fix open findings or run a fresh review, converge the loop, and seal the receipt."
---

Drive the supervisory-control self-review of the current branch to
convergence. The workflow decides from the committed control state what is
needed: open findings are fixed in code and committed, an unreviewed tree
gets a fresh review, an escalated loop asks before reopening, and a
converged loop is sealed and the receipt committed.

Run from the repository root:

    .yield/bin/yskill run 'skills/self-review-solve'

If `.yield/bin/yskill` is missing, install the pinned runtime first:

    go install github.com/operatorstack/yield/cmd/yskill@v0.1.38 && yskill init skills/self-review-solve --language go

Follow each returned operation exactly. Answer it directly:

    .yield/bin/yskill respond <run-id> --value <answer> --skill 'skills/self-review-solve'

Do not skip an operation or invent a response. When the run asks you to fix
findings, edit the code, run the relevant tests, and commit before
responding. When it asks for a review, read the prompt file it names, review
only the committed range it names, and return only the schema-valid JSON
object.
