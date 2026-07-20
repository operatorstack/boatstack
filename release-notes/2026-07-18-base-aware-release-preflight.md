### Catch release-note policy failures before CI

Maintainers can run one base-aware preflight before pushing an upstream Boatstack change. It fetches the live base and blocks both missing release messages and stale branches that would appear to delete append-only release history.
