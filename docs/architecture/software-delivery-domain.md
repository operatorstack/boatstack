# Software-delivery domain

Software delivery is a concrete domain over the general kernel. Its public
model is expressed in repository, plan, workspace, gate, evidence, delivery,
publication, authority, and recovery concepts—not internal package names.

Current implementation ownership is divided as follows:

| Area | Responsibility |
| --- | --- |
| `core` | trusted capability and operation declarations |
| `delivery` | compiled domain contracts and kernel adapter |
| `flow/standard` | first-party complete program declaration |
| `flow/softwaredelivery` | repository Flow binding and projection support |
| `internal/softwaredelivery` | observation, state, effects, admission, recovery, work, and surfaces |
| TypeScript software-delivery package | declarative authoring helpers and trusted binding references |

Repository authors select trusted lifecycle membership, priorities, targets,
entries, work contracts, additional mandatory authority, and diagnostics.
Trusted packages own operator effects, minimum capabilities, verification,
recovery, and canonical parameter contracts. Operation availability does not
force lifecycle membership.

The plant observes repository and external-provider facts. The supervisor
evaluates domain admissibility through the kernel relation. Effects own local
resource transactions and external settlement. Durable stores retain domain
state, journals, receipts, authorization, invocation input, and foreground
work. Surfaces project the resulting controller to configured hosts.

## Current implementation anchors

- [Software Program runtime](../../boatstack/delivery/program_runtime.go)
- [Domain engine](../../boatstack/internal/softwaredelivery/engine/engine.go)
- [Repository observer](../../boatstack/internal/softwaredelivery/plant/observer.go)
- [Runtime contract tests](../../boatstack/delivery/runtime_contract_test.go)
