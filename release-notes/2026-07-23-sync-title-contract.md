### Sync PR title now satisfies the public repo's upstream-sync contract

The generated Boatstack `sync-upstream.yml` now titles its sync commit and PR
"Sync Boatstack from Intelligence Flow Labs @ <short>", matching the
"Verify upstream sync contract" check in the public repo's `ci.yml`. The source
name is config-driven (`sync.source_label`) rather than hardcoded, defaulting to
"Intelligence Flow" for other labs. This is a control-plane wording fix only; the
projected Boatstack surface is unchanged.
