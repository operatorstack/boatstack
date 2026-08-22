---
name: self-review
description: "Run the Boatstack supervisory-control self-review for the current branch and report the verdict without changing code."
---

<!-- generated-by: yskill; source: skills/self-review; digest: sha256:7be36f40fd15f0ada56cec3378fb2b9a17ced46b5ec256182879f1b9fa12cdda; version: 0.1.38 -->

This adapter exposes the canonical Yield workflow at `skills/self-review`.
Read its SKILL.md, then run from the repository root:

    .yield/bin/yskill run 'skills/self-review'

	Follow each returned operation exactly. Answer each operation directly:

	    .yield/bin/yskill respond <run-id> --value <answer> --skill 'skills/self-review'

	For structured agent results, use --result-json instead of --value.

Do not skip an operation or invent its response.
