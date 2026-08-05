### Detached attachment accepts an external project configuration

Detached Supervision can now validate and copy a normal Boatstack project configuration from an explicit `--config` path. Its exact SHA-256 is bound to detached status and generated provenance, and any later drift blocks resume without writing configuration into the repository or `.git`.
