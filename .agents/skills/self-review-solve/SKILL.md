---
name: self-review-solve
description: "Resolve the Boatstack self-review: fix open findings or run a fresh review, converge the loop, and seal the receipt."
---

<!-- generated-by: yskill; source: skills/self-review-solve; digest: sha256:08304ccee8618eb8dbad3cc2185a13fb53c2732ec64252e6e963c0f9678ec5e0; version: 0.1.38 -->

This adapter exposes the canonical Yield workflow at `skills/self-review-solve`.
Read its SKILL.md, then run from the repository root:

    .yield/bin/yskill run 'skills/self-review-solve'

	Follow each returned operation exactly. Answer each operation directly:

	    .yield/bin/yskill respond <run-id> --value <answer> --skill 'skills/self-review-solve'

	For structured agent results, use --result-json instead of --value.

Do not skip an operation or invent its response.
