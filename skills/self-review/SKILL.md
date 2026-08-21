---
name: self-review
description: "Run the Boatstack supervisory-control self-review for the current branch and report the verdict without changing code."
---

Run one review round for the current branch against `origin/main` and show
the recorded verdict. This skill never edits code: the review is performed
read-only, `boatstack-reviewer` admits or refuses the candidate, and the
round is recorded in the repository's local review store.

Run from the repository root:

    .yield/bin/yskill run 'skills/self-review'

If `.yield/bin/yskill` is missing, install the pinned runtime first:

    go install github.com/operatorstack/yield/cmd/yskill@v0.1.38 && yskill init skills/self-review --language go

Follow each returned operation exactly. Answer it directly:

    .yield/bin/yskill respond <run-id> --value <answer> --skill 'skills/self-review'

Do not skip an operation or invent a response. When the run asks for the
review (the `review` agent task), read the prompt file it names, review only
the committed range it names, and return only the schema-valid JSON object.
