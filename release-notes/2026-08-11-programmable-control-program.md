### Separate Kernel mechanism from delivery policy

Boatstack now compiles one immutable CoreSystem, one explicit primary delivery flow, and optional conservative extensions into a fingerprinted ControlProgram before the Kernel resolves or applies any transition. The default distribution retains StandardFlow behavior, while public SDK contracts can supply another trusted flow and checksum-verified subprocess extensions without modifying Kernel mechanism code.
