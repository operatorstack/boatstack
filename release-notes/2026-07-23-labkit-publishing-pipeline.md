### Publishing now runs on the shared labkit toolkit

Boatstack is now projected into this repository by Intelligence Flow's shared `labkit` engine, driven by a declarative `publish.config.json`, instead of a Boatstack-specific build script. The projected files are byte-for-byte identical to before — a golden reproduction test asserts the new engine reproduces the previous output exactly — so this is an internal provenance and tooling change with no effect on Boatstack's runtime, CLI, skill, or public contract.
