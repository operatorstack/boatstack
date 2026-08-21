---
name: self-review
description: "Run the Boatstack supervisory-control self-review for the current branch and report the verdict without changing code."
---

<!-- generated-by: yskill; source: skills/self-review; digest: sha256:70ad765b0495fed0dc9fbe319eb01fdcbbcc63075ede2358028ff92182682b7f; version: 0.1.38 -->

This adapter exposes the canonical Yield workflow at `skills/self-review`.
Read its SKILL.md, then run from the repository root:

    .yield/bin/yskill run 'skills/self-review'

	Follow each returned operation exactly. Answer each operation directly:

	    .yield/bin/yskill respond <run-id> --value <answer> --skill 'skills/self-review'

	For structured agent results, use --result-json instead of --value.

Do not skip an operation or invent its response.
