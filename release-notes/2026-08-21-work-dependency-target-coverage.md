### Reject work dependencies whose producer cannot serve the consumer objective

RuntimeManifest now requires every work-output producer transition to support all targets its consumer supports, checked after trusted TargetIDs are projected. Programs that attach producer work to an objective the consumer's run can never select are rejected at manifest construction instead of entering a permanent zero-progress selection loop at runtime.
