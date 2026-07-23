### Boatstack can now capture PR visual evidence for you

A new `capture-evidence` operation turns a repository's declared visual scenarios
into trusted PR evidence without manual screenshotting. It resolves the
repository-owned capability command, reads the scenarios recorded in the feature
plan, and runs each one as a supervised, fingerprinted operation — retrying a
flaky capture within a bounded budget and reusing a successful capture on the
same commit instead of re-running it.

Each screenshot the harness produces is conformance-checked before it is
ingested: the manifest is stamped to the current head commit and product diff, so
`pr-context` trusts it as PASS only while it still matches the change under
review. A harness that reports success but produces a non-conformant artifact
fails closed — capture never records evidence it cannot stand behind.

Boatstack ships the capture contract, not the harness. The repository owns a
`visual` (or `screenshot`/`e2e`) command that renders one PNG per scenario, and
Boatstack invokes it through a stable environment-variable contract
(`BOATSTACK_CAPTURE_SCENARIO_ID`, `_ENTRY`, `_STATE`, `_VIEWPORT`, `_OUTPUT`).
