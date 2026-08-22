---
name: open-pr
description: "Open the pull request for the current branch with a Boatstack-structured description, gated on a verified self-review attestation."
---

Open the pull request for the current branch. This is a governed boundary:
the workflow refuses unless the committed review attestation verifies for
the exact head tree — the same deterministic check CI performs — so a pull
request only opens for a head whose self-review converged and was sealed.
Run `skills/self-review-solve` first when the gate refuses.

The description follows a fixed structure. The agent drafts the prose of
three sections — Boundary (the durable control boundary the change
crosses), Transition (refused/allowed behavior before versus after), and
Evidence (what was run and what it proves) — and the workflow appends the
facts it gathers itself: the commit list, the attestation binding (reviewed
tree and program fingerprint), and any residual P2/P3 findings the
converged review recorded. Running this skill is the explicit decision to
push the branch and create the pull request; nothing is pushed when any
gate refuses.

Run from the repository root:

    .yield/bin/yskill run 'skills/open-pr'

If `.yield/bin/yskill` is missing, install the pinned runtime first:

    go install github.com/operatorstack/yield/cmd/yskill@v0.1.38 && yskill init skills/open-pr --language go

Follow each returned operation exactly. Answer it directly:

    .yield/bin/yskill respond <run-id> --result-json <json> --skill 'skills/open-pr'

Do not skip an operation or invent a response. When the run asks for the
draft (the `draft` agent task), write the three sections for a reviewer who
has not followed the work, and return only the schema-valid JSON object.
