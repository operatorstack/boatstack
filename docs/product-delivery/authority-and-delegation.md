# Authority and delegation

Trusted operators keep alternatives and mandatory requirements separate:

```text
any_of: human | autonomy
all_of: external-provider
```

Runtime admission requires one trusted alternative, every trusted mandatory
authority, and every repository-added authority. A repository may strengthen a
transition with `requires.authorities`, but it cannot weaken trusted authority,
replace provider authority, or grant authority.

```ts
trustedTransition(
  { id: "publication.execute", priority: 76 },
  { requires: { authorities: ["human"] } },
);
```

`trustedDelegation("autonomy")` requests a trusted delegation mechanism for an
entry. It does not authorize the run. Boatstack presents an exact run-bound
request at runtime, and only a trusted human/host boundary can authorize it.
Revocation, expiry, incompatible drift, or an unauthorized execution context
ends or suspends that delegation. External-provider authority remains separate.
For repository Flow continuation, the trusted GitHub boundary derives that
provider capability from the current repository identity and authenticated
write permission. This is capability evidence, not another human approval.
Repository files and `--authority-receipt` cannot create provider authority.

Publication is admitted only after the product worktree is clean and the
preview binds its exact committed HEAD. A changed HEAD or worktree invalidates
the preview before the external effect.
