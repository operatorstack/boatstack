### Let repository Flows require bounded foreground work

Flows can now declare exact instruction assets, entry inputs, and staged output
contracts for human or agent work. Boatstack suspends and resumes the same run,
validates the result as evidence, and still admits effects only through trusted
operators. Input fingerprints and request-specific staging prevent changed
inputs from reusing stale work. The software-delivery adapter also provides
optional planning-package admission, approval, and promotion operations.

The runtime performs a one-step, transactional schema-4 state upgrade and
verifies every declared planning-package output before approval. Trusted
software-delivery lowering also rejects work inputs that any reachable entry
cannot bind.

Fresh delegated runs now bootstrap verified runtime and configuration state
before deriving repository-policy authority. An exact autonomy delegation may
perform that local initialization, while transitions requiring repository
authority remain unavailable until verified configuration evidence exists.

Published-PR entries now stop until intended delivery changes are committed,
bind the preview and push to that exact commit, and derive short-lived GitHub
provider capability through each trusted transition's declared fingerprint
binding, including correction and recovery. Caller-provided provider receipts
are rejected.
