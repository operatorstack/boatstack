# Boatstack TypeScript SDK

Boatstack exposes two authoring packages over one Control Program model:

- [`@operatorstack/boatstack`](base-sdk.md) provides domain-neutral program,
  predicate, transition, target, entry, invocation, authority, asset, and
  foreground-work declarations.
- [`@operatorstack/boatstack-software-delivery`](software-delivery-sdk.md)
  provides trusted software-delivery bindings and a composition helper.

```text
TypeScript declarations
  → restricted trusted frontend
  → raw Control Program IR
  → canonicalization, binding, completeness, assets
  → checked executable artifact
  → runtime controller
```

These APIs produce data. They do not execute effects, create handlers, grant
authority, approve a transition, or replace runtime verification.

Use the base package to define a domain-neutral or custom-domain Control
Program. Use the software-delivery package when composing a repository Flow
from Boatstack's trusted delivery operations. Start with
[Flow anatomy](flow-anatomy.md), then use the generated package modules for
exact signatures.

System concepts live in the repository [concept documentation](../concepts/index.md);
Product Delivery behavior lives in the [domain guides](../product-delivery/index.md).
