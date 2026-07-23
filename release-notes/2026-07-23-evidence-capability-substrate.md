### Evidence capabilities are now a shared, extensible substrate

Boatstack's PR evidence detection is no longer hard-wired to a single evidence
type. A generic evidence-capability registry now backs the "can this repository
produce the evidence?" cut, and visual evidence is its first tenant. Nothing
changes in how visual evidence behaves today — the same repository commands,
statuses, and fingerprints — but the shared spine means future evidence types
(and the upcoming capture and provisioning flows) plug in through one registry
instead of a bespoke path each time.
