### Publishing toolkit moved to a standalone package

The `labkit` toolkit that projects Boatstack into this repository moved from a
submodule of the monorepo's Python package to a standalone top-level `labkit/`
project, invoked as `python -m labkit`. This only changes where the publishing
tooling lives and how the sync workflow installs it; the projected files are
byte-for-byte identical (asserted by the golden reproduction test), with no
effect on Boatstack's runtime, CLI, skill, or public contract.
