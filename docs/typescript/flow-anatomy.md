# Flow anatomy

A complete Flow makes repository policy explicit: program identity, human
identity role, lifecycle membership, targets, entries, activation authority,
and delegation.

The example below is the checked source used by the documentation tests. It is
compiled with TypeScript and lowered by the restricted Boatstack frontend.

{@includeCode ./examples/product-delivery.flow.ts}

The source contains only trusted named imports, static declarations, and one
default export. The frontend produces raw IR; Go compilation later resolves
trusted operation bindings, assets, invocation completeness, and the canonical
program fingerprint.

Changing the entry name does not change Boatstack's semantics. Changing a
target, lifecycle step, priority, authority requirement, or producer changes
executable program semantics and therefore the program identity.
