### Shadow transition registry for delivery-flow navigation (no runtime effect)

A new internal package declares Boatstack's delivery workflow as a typed registry of transitions —
each state-changing operation with its states, kind, cost class, and the function that performs it —
as a single source of truth for future flow-navigation analysis. It is shadow-only: nothing consumes
it at runtime, and it changes no command, gate, authority, verification, evidence, or recovery
behavior. A conformance test keeps the registry faithful to the real state machine, so the declaration
and the code it mirrors can never silently diverge.
