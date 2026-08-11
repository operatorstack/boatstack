<!-- Generated from catalog.Default by surfaces.RenderCatalogMarkdown. Do not edit. -->
# Boatstack V2 executable transition catalog

Registry size: **61** transitions. Event classes: authority 9; owned-local 30; owned-external 2; recovery 7; observed-external 13.

Controlling facets: `phase`, `topology`, `engagement`, `delivery`, `workspace`, `plan`, `configuration`, `configuration-policy`, `runtime`, `publication`, `verification`, `recovery`, `transaction`, `recovery-info`, `transaction-info`, `terminal`, `goal`.

| Transition | Class | Source phases | Target phases | Authority | Parameters | Owned resources | Recovery |
|---|---|---|---|---|---|---|---|
| `configuration.initialize` | owned-local | OBSERVED | OBSERVED / TERMINAL | human/repository-policy | `config_path*`, `config_sha256*` | `configuration` | `configuration.reconcile` |
| `configuration.mutate` | owned-local | OBSERVED / ACTIVE / FRONTIER / UNRESOLVED | OBSERVED / ACTIVE / TERMINAL | human/autonomy | `config_path*`, `config_sha256*` | `configuration` | `configuration.reconcile` |
| `configuration.reconcile` | recovery | RECOVERY / UNRESOLVED | OBSERVED / FRONTIER / TERMINAL | human/repository-policy | `transaction_id*` | `configuration` | `recovery.escalate` |
| `delivery.slice.advance` | owned-local | ACTIVE | ACTIVE / TERMINAL | human/autonomy | `slice_id*`, `source_revision*` | `delivery-state` | `recovery.resume` |
| `engagement.begin` | authority | DORMANT / OBSERVED | OBSERVED / ACTIVE | repository-policy | - | `engagement` | `recovery.resume` |
| `engagement.release` | authority | ACTIVE / FRONTIER | DORMANT | repository-policy | - | `engagement` | `recovery.resume` |
| `engagement.renew` | authority | ACTIVE | ACTIVE | repository-policy/autonomy | - | `engagement` | `recovery.resume` |
| `evidence.approval.revoke` | authority | ACTIVE / FRONTIER | FRONTIER | human | - | `approval` | `recovery.resume` |
| `evidence.visual.attach` | owned-local | ACTIVE | ACTIVE / TERMINAL | human/repository-policy | `manifest_path*`, `privacy_receipt*`, `source_revision*` | `evidence` | `recovery.resume` |
| `external.branch-changed` | observed-external | DORMANT / OBSERVED / ACTIVE / RECOVERY / FRONTIER / UNRESOLVED | OBSERVED | none | - | - | `-` |
| `external.ci-completed` | observed-external | DORMANT / OBSERVED / ACTIVE / RECOVERY / FRONTIER / UNRESOLVED | OBSERVED / ACTIVE / TERMINAL | none | - | - | `-` |
| `external.configuration-drifted` | observed-external | DORMANT / OBSERVED / ACTIVE / RECOVERY / FRONTIER / UNRESOLVED | OBSERVED / UNRESOLVED | none | - | - | `-` |
| `external.files-changed` | observed-external | DORMANT / OBSERVED / ACTIVE / RECOVERY / FRONTIER / UNRESOLVED | OBSERVED | none | - | - | `-` |
| `external.head-changed` | observed-external | DORMANT / OBSERVED / ACTIVE / RECOVERY / FRONTIER / UNRESOLVED | OBSERVED | none | - | - | `-` |
| `external.host-interrupted` | observed-external | DORMANT / OBSERVED / ACTIVE / RECOVERY / FRONTIER / UNRESOLVED | RECOVERY | none | - | - | `-` |
| `external.lease-expired` | observed-external | DORMANT / OBSERVED / ACTIVE / RECOVERY / FRONTIER / UNRESOLVED | DORMANT / FRONTIER | none | - | - | `-` |
| `external.pr-closed` | observed-external | DORMANT / OBSERVED / ACTIVE / RECOVERY / FRONTIER / UNRESOLVED | OBSERVED / ACTIVE / FRONTIER | none | - | - | `-` |
| `external.pr-merged` | observed-external | DORMANT / OBSERVED / ACTIVE / RECOVERY / FRONTIER / UNRESOLVED | OBSERVED / ACTIVE / TERMINAL | none | - | - | `-` |
| `external.pr-opened` | observed-external | DORMANT / OBSERVED / ACTIVE / RECOVERY / FRONTIER / UNRESOLVED | OBSERVED / ACTIVE / TERMINAL | none | - | - | `-` |
| `external.pr-updated` | observed-external | DORMANT / OBSERVED / ACTIVE / RECOVERY / FRONTIER / UNRESOLVED | OBSERVED / ACTIVE / TERMINAL | none | - | - | `-` |
| `external.provider-unavailable` | observed-external | DORMANT / OBSERVED / ACTIVE / RECOVERY / FRONTIER / UNRESOLVED | UNRESOLVED / RECOVERY | none | - | - | `-` |
| `external.runtime-disappeared` | observed-external | DORMANT / OBSERVED / ACTIVE / RECOVERY / FRONTIER / UNRESOLVED | OBSERVED / RECOVERY | none | - | - | `-` |
| `gate.build.record` | owned-local | ACTIVE | ACTIVE | repository-policy | `source_revision*`, `evidence_path*`, `evidence_fingerprint*` | `gate-evidence` | `recovery.resume` |
| `gate.change.record` | owned-local | ACTIVE | ACTIVE | repository-policy | `source_revision*`, `evidence_path*`, `evidence_fingerprint*` | `gate-evidence` | `recovery.resume` |
| `gate.journey.record` | owned-local | ACTIVE | ACTIVE | repository-policy | `source_revision*`, `evidence_path*`, `evidence_fingerprint*` | `gate-evidence` | `recovery.resume` |
| `gate.review.record` | owned-local | ACTIVE | ACTIVE / TERMINAL | human/repository-policy | `source_revision*`, `evidence_path*`, `evidence_fingerprint*` | `gate-evidence` | `recovery.resume` |
| `gate.test.record` | owned-local | ACTIVE | ACTIVE / TERMINAL | repository-policy | `source_revision*`, `evidence_path*`, `evidence_fingerprint*` | `gate-evidence` | `recovery.resume` |
| `goal.configure` | authority | OBSERVED / ACTIVE / FRONTIER / TERMINAL / ABANDONED | OBSERVED / ACTIVE / FRONTIER | human/autonomy | `goal_kind*`, `delivery_id*` | `goal` | `recovery.resume` |
| `installation.initialize` | owned-local | DORMANT / OBSERVED | OBSERVED | human | `source_revision*`, `runtime_path*`, `runtime_sha256*`, `config_path*`, `config_sha256*` | `installation` | `runtime.reconcile` |
| `installation.update` | owned-local | OBSERVED / ACTIVE | OBSERVED / ACTIVE / TERMINAL | human/autonomy | `source_revision*`, `runtime_path*`, `runtime_sha256*` | `installation` | `runtime.reconcile` |
| `invocation.rebind` | owned-local | OBSERVED / UNRESOLVED | OBSERVED | repository-policy | - | `identity-binding` | `recovery.resume` |
| `plan.abandon` | authority | OBSERVED / ACTIVE / FRONTIER | ABANDONED | human | - | `plan` | `recovery.resume` |
| `plan.activate` | owned-local | OBSERVED / ACTIVE | ACTIVE | human/autonomy | - | `delivery-state` | `recovery.resume` |
| `plan.amend` | owned-local | ACTIVE / FRONTIER | ACTIVE | human/autonomy | `source_path*`, `delivery_id*` | `plan` | `recovery.resume` |
| `plan.approve` | authority | ACTIVE / FRONTIER | ACTIVE / TERMINAL | human/autonomy | `plan_fingerprint*`, `actor*` | `approval` | `recovery.resume` |
| `plan.approve-amendment` | authority | ACTIVE / FRONTIER | ACTIVE | human/autonomy | `plan_fingerprint*`, `actor*` | `approval` | `recovery.resume` |
| `plan.create` | owned-local | OBSERVED / ACTIVE | ACTIVE | human/autonomy | `source_path*`, `delivery_id*` | `plan` | `recovery.resume` |
| `plan.invalidate` | owned-local | ACTIVE / OBSERVED | FRONTIER | repository-policy | - | `plan-evidence` | `recovery.resume` |
| `plan.validate` | owned-local | OBSERVED / ACTIVE | ACTIVE / FRONTIER | repository-policy | - | `plan-evidence` | `recovery.resume` |
| `publication.abandon` | authority | ACTIVE / FRONTIER | ABANDONED | human | - | `publication` | `recovery.resume` |
| `publication.correct` | owned-external | OBSERVED / ACTIVE / TERMINAL | ACTIVE / RECOVERY | human/autonomy AND external-provider | `publication_id*`, `body_path*`, `body_sha256*` | `publication` | `publication.reconcile` |
| `publication.execute` | owned-external | ACTIVE | ACTIVE / RECOVERY | human/autonomy AND external-provider | `preview_fingerprint*` | `publication` | `publication.reconcile` |
| `publication.observe` | owned-local | OBSERVED / ACTIVE / RECOVERY / UNRESOLVED | ACTIVE / TERMINAL / FRONTIER / UNRESOLVED | repository-policy | `publication_id*` | `publication-evidence` | `recovery.resume` |
| `publication.preview` | owned-local | ACTIVE | ACTIVE | repository-policy | `base_ref*`, `head_ref*`, `body_path*` | `publication-preview` | `recovery.resume` |
| `publication.reconcile` | recovery | RECOVERY / UNRESOLVED | ACTIVE / TERMINAL / FRONTIER / UNRESOLVED | human/external-provider | `publication_id*`, `transaction_id*` | `publication` | `recovery.escalate` |
| `recovery.escalate` | recovery | RECOVERY / UNRESOLVED | FRONTIER | repository-policy | `transaction_id*` | `recovery-journal` | `recovery.escalate` |
| `recovery.resume` | recovery | RECOVERY | DORMANT / OBSERVED / ACTIVE / FRONTIER / TERMINAL / ABANDONED | human/autonomy/repository-policy | `transaction_id*` | `recovery-journal` | `recovery.escalate` |
| `recovery.rollback` | recovery | RECOVERY | DORMANT / OBSERVED / ACTIVE / FRONTIER / TERMINAL / ABANDONED | human/repository-policy | `transaction_id*` | `recovery-journal` | `recovery.escalate` |
| `repository.attach` | owned-local | DORMANT / OBSERVED | OBSERVED | human | `topology*`, `config_authority*` | `repository-binding` | `recovery.resume` |
| `repository.detach` | owned-local | DORMANT / OBSERVED / FRONTIER | DORMANT | human | - | `repository-binding` | `recovery.resume` |
| `runtime.hydrate` | owned-local | OBSERVED / RECOVERY / UNRESOLVED | OBSERVED / ACTIVE / TERMINAL | repository-policy | `source_revision*`, `runtime_path*`, `runtime_sha256*` | `runtime` | `runtime.reconcile` |
| `runtime.reconcile` | recovery | RECOVERY / UNRESOLVED | OBSERVED / FRONTIER / TERMINAL | repository-policy | `source_revision*`, `runtime_path*`, `runtime_sha256*`, `transaction_id*` | `runtime` | `recovery.escalate` |
| `runtime.replace` | owned-local | OBSERVED / RECOVERY | OBSERVED / TERMINAL | human/repository-policy | `source_revision*`, `runtime_path*`, `runtime_sha256*` | `runtime` | `runtime.reconcile` |
| `workspace.abandon` | owned-local | ACTIVE / FRONTIER | ABANDONED | human | `branch*` | `workspace` | `recovery.resume` |
| `workspace.activate` | owned-local | OBSERVED / ACTIVE | ACTIVE | repository-policy | `branch*` | `workspace` | `recovery.resume` |
| `workspace.cleanup` | owned-local | OBSERVED / ACTIVE / TERMINAL / ABANDONED | OBSERVED / TERMINAL / ABANDONED | human/autonomy | `branch*` | `workspace` | `recovery.escalate` |
| `workspace.cut` | owned-local | OBSERVED / ACTIVE | ACTIVE | human/autonomy | `branch*`, `base_ref*`, `destination*` | `workspace` | `workspace.reconcile` |
| `workspace.publish` | owned-local | ACTIVE | ACTIVE | repository-policy | `branch*` | `workspace-state` | `recovery.resume` |
| `workspace.reap` | owned-local | OBSERVED / TERMINAL / ABANDONED | OBSERVED / TERMINAL / ABANDONED | human | `branch*` | `workspace` | `recovery.escalate` |
| `workspace.reconcile` | recovery | RECOVERY / UNRESOLVED | DORMANT / OBSERVED / ACTIVE / FRONTIER / TERMINAL / ABANDONED | human/repository-policy | `transaction_id*` | `workspace` | `recovery.escalate` |
| `workspace.sync` | owned-local | ACTIVE | ACTIVE / FRONTIER | human/autonomy | `branch*` | `workspace` | `recovery.resume` |

`*` marks a required parameter. OR authority is shown with `/`; mandatory authority clauses are shown with `AND`. Source and target facet predicates remain in the canonical JSON returned by `boatstack catalog --format json`.
