### Deterministic enforcement of Boundary-Oracle Loop

Transformed the Boundary Failure Mode Analysis from a prompt suggestion into a strict, programmatic intelligence harness:
1. **Schema Contract:** Added `systemic_boundaries` array to the `plan.md` schema, requiring each boundary to define a `verification_oracle`.
2. **Structural Lock:** Updated `ValidatePlan` to physically reject plans that define a systemic boundary but fail to provide an oracle or isolate the boundary into its own delivery slice.
3. **Execution Lock:** Updated `/test-gate` instructions to require test evidence proving that the verification oracle (negative test) successfully blocks or normalizes a violation attempt.
4. **Compounding Memory:** Upon PR publication (`publish-pr`), Boatstack now extracts verified `systemic_boundaries` from the feature lock and appends them to `.product-loop/verified-boundaries.md`. Agent system prompts have been updated to read this file and natively respect established boundaries in future runs.
