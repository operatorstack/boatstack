### Keep detached configuration projections independent
Detached and hybrid installations now preserve the declared repository or controller configuration authority during updates and explicit configuration changes. Repository configuration divergence no longer blocks ordinary tools; Boatstack reports a fingerprinted `config-rebind` repair only when an invoked workflow needs reconciliation.
