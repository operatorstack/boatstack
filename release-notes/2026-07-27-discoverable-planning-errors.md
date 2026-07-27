### Rejections name the values you can use

Two errors told you what was wrong but not how to fix it, so you had to discover the answer out of
band. When the bounded planning writer rejected an artifact name, it echoed the name you gave but not
the names it accepts. The accepted names are filenames that end in `.md`, so the natural guess, such as
`plan`, is always wrong. When an approval no longer matched the plan, the error said the fingerprint did
not match but did not say what the current fingerprint is.

Both now carry the answer. The planning writer lists the accepted artifact names and notes the `.md`
suffix, so `plan.md`, `source-plan.md`, and the rest are visible at the point of rejection. The approval
error reports the plan's current fingerprint and points at check-plan to confirm it, so you can
re-approve against the right value without a separate lookup.
