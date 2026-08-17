# Runtime, persistence, and control bundles

The runtime keeps several durability domains separate. “State” without an
owner is insufficient to describe these stores.

| Durable boundary | Owner | Purpose |
| --- | --- | --- |
| Supervisory control state | general kernel store | instance/program identity, objective binding, mode, revision, recovery |
| Software-delivery state | domain durable store | repository, plan, workspace, gate, evidence, delivery, publication facts |
| Transaction journal | software-delivery effects | unresolved attempts, staged mutations, settlement, rollback, recovery |
| Transition receipts | kernel/domain commit boundary | immutable facts for verified transitions |
| Authorization and delegation | delegation store | exact activation request and run-scoped delegated grants |
| Invocation requests and receipts | invocation store | typed missing input, answer, and supersession lineage |
| Foreground-work records | foreground-work manager | request, bounded outputs, validation, and resumption |
| Repository control bundle | repository artifact | program, configuration, runtime, bindings, and projection identities |
| Runtime pin | repository configuration | exact verified runtime selected for this repository |

Repository control bundles are committed inputs. Machine-local controller
state, journals, locks, and runtime installations are not product artifacts.
Generated host projection files are committed only where the configured
projection selection requires them.

The runtime supports embedded, detached, and linked-worktree controller
topologies. Identity is derived from the Git common directory and exact
repository/worktree context rather than a path string alone. Transfers and
runtime selection preserve the control bundle and lock ownership across the
selected topology.

Resource replacement is staged and atomic where the host permits it. A failed
local mutation rolls back; an external effect with uncertain settlement enters
recovery. Restart loads journals and durable records before prescribing any new
effect.

## Current implementation anchors

- [Control bundle](../../boatstack/internal/runtime/control_bundle.go)
- [Runtime flow files](../../boatstack/internal/runtime/flow_files.go)
- [Effect journal](../../boatstack/internal/softwaredelivery/effects/journal.go)
- [Runtime store tests](../../boatstack/internal/softwaredelivery/effects/runtime_store_test.go)
