### Bind human approval to named repository roles

Boatstack project configuration schema 5 replaces `identity.human` with an
explicit default and named role descriptors. Software-delivery Flows select one
role in their Control Program, and Boatstack persists that admitted role so a
candidate program cannot choose its own approver. Role or descriptor drift now
invalidates pending inputs, delegation, and human authorization while remaining
strictly separate from GitHub provider capability.
