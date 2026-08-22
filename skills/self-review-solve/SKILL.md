---
name: self-review-solve
description: "Resolve the Boatstack self-review: fix open findings or run a fresh review, converge the loop, and seal the receipt."
---

Drive the supervisory-control self-review of the current branch to
convergence. The workflow decides from the committed control state what is
needed: open blocking findings (P0/P1) are fixed in code and committed, an
unreviewed tree gets a fresh review, an escalated loop asks before
reopening, and a converged loop is sealed — the minimal attestation
(reviewed tree + program fingerprint) is committed locally. Residual P2/P3
findings never block convergence and are never fixed by this loop; they are
listed in the completion payload for the user to decide about. This skill
never pushes: pushing the branch is the user's decision, and CI verifies
the attestation whenever the push happens.

Run from the repository root:

    .yield/bin/yskill run 'skills/self-review-solve'

If `.yield/bin/yskill` is missing, install the pinned runtime first:

    go install github.com/operatorstack/yield/cmd/yskill@v0.1.38 && yskill init skills/self-review-solve --language go

Follow each returned operation exactly. Answer it directly:

    .yield/bin/yskill respond <run-id> --value <answer> --skill 'skills/self-review-solve'

Do not skip an operation or invent a response. When the run asks you to fix
findings, fix only the blocking (P0/P1) ones, run the relevant tests, and
commit before responding; leave residual P2/P3 findings alone. When it asks for a review, read the prompt file it names, review
only the committed range it names, and return only the schema-valid JSON
object.
