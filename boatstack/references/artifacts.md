# Boatstack artifact ownership

Repository artifacts live below `.boatstack/` and are written only by
registered effects:

- `project.json`: strict schema-2 policy;
- `plans/<delivery>.source`: exact source plan bytes;
- `approvals/<delivery>.json`: current plan-fingerprint approval;
- `evidence/<delivery>/`: revision-bound gate and visual manifests;
- `publication/<delivery>.preview.json`: exact external-effect preview.

Machine-local state, journals, locks, receipts, and JSONL events are not product
evidence to commit. They are partitioned by repository, clone, and worktree
identity.

An artifact is data, not authority. The kernel checks its fingerprint, source
snapshot, objective, transition, and authority receipt before use.
