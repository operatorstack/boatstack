# Software-delivery SDK

`@operatorstack/boatstack-software-delivery` binds repository-selected policy
to trusted software-delivery operations.

## Flow composition

`softwareDelivery` combines explicit lifecycle membership, priorities, work,
targets, entries, and the `humanIdentity` role with canonical domain facets,
evidence, operators, transitions, and producer declarations. It is a pure
composition helper: it adds no hidden lifecycle steps and grants no authority.

`trustedTransition`, `trustedSoftwareDeliveryTransitions`, and related helpers
bind operation IDs through the trusted registry. Repositories may strengthen
authority but cannot replace minimum capability, effect, verification, or
recovery semantics.

## Planning

`planningPackageAdmit`, `planningPackageApprove`, and
`planningPackagePromote` are optional trusted steps. A repository includes them
explicitly. `planningPackage: { work, planOutput }` binds foreground work and
one required canonical plan output only to admit. The plan-output ID becomes a
trusted compiled parameter binding; `plan` has no magic meaning.

## Authority and delegation

`humanIdentity` selects a named project role. `trustedDelegation("autonomy")`
requests a trusted delegation mechanism. Entry `requires.authorities` controls
activation separately. `inbox` declares the trusted planning-input resolver;
none of these helpers approves a run.

## Repository inputs and evidence

Trusted producer helpers read the default branch, managed workspace identity,
admitted planning manifest, current revision, gate evidence, publication body,
visual evidence, and recovery transaction through runtime-owned boundaries.

See [Product Delivery](../product-delivery/index.md) for lifecycle semantics and
the generated module page for exact signatures.
