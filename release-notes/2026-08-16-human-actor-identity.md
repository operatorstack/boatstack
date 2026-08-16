### Make human actor identity explicit

Repositories now declare a literal or structured command descriptor for the
human actor that hosts present at authority boundaries. Boatstack fingerprints
and exposes the descriptor without executing it, while explicit approval and
external-provider authority remain separate requirements.

This is a breaking alpha boundary. Schema-2 project configurations and
schema-2 delegation records are intentionally unsupported. Regenerate the
project installation and start a new run; there is no in-place migration or
compatibility reader.
