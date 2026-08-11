### Reconcile program-changing updates atomically

Boatstack now binds explicit program-change acceptance to the exact prior program, candidate program, runtime, and launcher, then installs them through one recoverable transition. Updates that do not receive that acceptance preserve the healthy prior installation, and interrupted local updates can restore its exact launcher and managed state.
