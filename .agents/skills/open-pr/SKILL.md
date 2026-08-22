---
name: open-pr
description: "Open the pull request for the current branch with a Boatstack-structured description, gated on a verified self-review attestation."
---

<!-- generated-by: yskill; source: skills/open-pr; digest: sha256:49be3144bb174f090f3a88c64c20bce631722c3446b2e70fa6e75b076c817b02; version: 0.1.38 -->

This adapter exposes the canonical Yield workflow at `skills/open-pr`.
Read its SKILL.md, then run from the repository root:

    .yield/bin/yskill run 'skills/open-pr'

	Follow each returned operation exactly. Answer each operation directly:

	    .yield/bin/yskill respond <run-id> --value <answer> --skill 'skills/open-pr'

	For structured agent results, use --result-json instead of --value.

Do not skip an operation or invent its response.
