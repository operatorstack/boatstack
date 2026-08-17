# Authority, identity, and delegation

## Definitions

Authority, capability, identity, and delegation are separate:

- an **authority class** names a kind of trusted grant;
- a **capability** is permission enforced at a boundary;
- an **authority receipt** proves exact grants for a subject and time;
- an **identity role** is a Flow-selected functional name;
- an **identity descriptor** tells a host how to resolve that role;
- an **actor** is the concrete human recorded at approval;
- **provider authority** proves capability at an external provider;
- **entry activation authority** permits engagement of one exact run;
- **delegation** produces separately scoped run authority.

## Control boundary

Project configuration defines how roles resolve. The Control Program selects a
role. The host records the actor and exact approval provenance. An external
provider independently proves its own capability.

## Invariants

- Identity resolution is not approval; a role is not a person.
- An actor string is not authority.
- Human authority and provider authority do not substitute for each other.
- Entry activation does not become general later human authority.
- Only a trusted mechanism may create run-scoped delegated authority.
- Candidate configuration or program bytes cannot select the identity that
  approves their own admission.

## Lifecycle

When activation or delegation is required, the runtime suspends with an exact
run-bound request. The host resolves and presents the configured identity,
captures explicit approval, and records the two authority scopes separately.
Drift, expiry, or revocation requires a fresh request.

## Current implementation anchors

- [Identity binding](../../boatstack/internal/softwaredelivery/humanidentitybinding/binding.go)
- [Delegation record](../../boatstack/internal/softwaredelivery/delegation/record.go)
- [Entry activation conformance](../../boatstack/cmd/boatstack-helper/product_delivery_flow_e2e_test.go)
