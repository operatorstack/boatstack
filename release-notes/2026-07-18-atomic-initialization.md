### Boatstack initialization failures are actionable and atomic

Boatstack now identifies the exact JSON source and parse operation when initialization encounters malformed configuration, embedded templates, generated assets, hooks, or install metadata. Initialization validates its JSON outputs before repository writes and restores the original repository state if any commit or verification stage fails, preventing partial installations while preserving fail-closed safety.
