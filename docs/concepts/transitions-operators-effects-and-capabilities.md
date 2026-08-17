# Transitions, operators, effects, and capabilities

## Definitions

A **transition** is a candidate relation from current and observed state toward
an accepted next state. An **operator** realizes one admitted operation. An
**effect** is the bounded set of resulting facts or mutations. A **capability**
is permission exposed by trusted authority at an enforceable boundary.

Owned facets and resources state which parts of domain or supervisory state an
effect may change.

## Control boundary

The relation selects a transition; admission supplies its exact effective
capabilities; the operator executes; verification checks the result. A command,
tool call, API, human action, or deterministic function can be an operator
mechanism, but it is not automatically a Control Program transition.

## Invariants

- Program declarations may narrow requirements but never create authority.
- Trusted capability classification supplies the minimum for a concrete
  operation.
- Effects must stay within declared owned facets.
- Transition identity, operator execution, and effect facts remain distinct.
- Receipts report committed effects; they do not grant future capability.

## Host-process boundary

The kernel mediates its registered effects. An arbitrary subprocess can use
ambient filesystem, credential, Git, and network permissions outside those
handlers. Without an external sandbox or broker, Boatstack does not claim that
`command.execute` isolates those host-process effects.

## Current implementation anchors

- [Transition and capability model](../../boatstack/kernel/program.go)
- [Operator boundary](../../boatstack/kernel/runtime.go)
- [Capability conformance tests](../../boatstack/kernel/program_test.go)
