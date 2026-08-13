# Boatstack failure moves

Use the failure class, not the latest symptom:

| Failure class | Boatstack move |
|---|---|
| stale snapshot or prescription | discard it; re-resolve; execute no effects |
| ambiguous identity | preserve resources; supply exact invocation |
| configuration drift | validate and request `configuration.mutate` |
| runtime absent or wrong | install a verified candidate; request runtime update |
| interrupted local transaction | resume or roll back the exact journal |
| unknown external settlement | observe/reconcile; never blind retry |
| closed-unmerged publication | return to frontier; preserve workspace |
| changed approved intent | `plan.amend`, validate, and approve the amendment |
| unproved cleanup | refuse until landing or abandonment is established |

Repeated denial without a changed snapshot is a stall. Do not try another host
or edit state to force progress.
