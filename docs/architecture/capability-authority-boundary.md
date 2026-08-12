# Capability and authority boundary

A repository Control Program may narrow its executable surface. It cannot grant
itself authority.

```text
program capability surface
  + transition requirements
  + kernel effect minimum
  + external authority receipts
  -> exact admission or deterministic refusal
  -> effect-boundary recheck
```

The admission law is:

```text
transition required + kernel effect required
  subset of program declared
  subset of externally granted
```

The effective set is the complete required set. Boatstack never continues with
a partial intersection.

## Capability vocabulary

| Capability | Kernel-mediated effect |
| --- | --- |
| `repository.write` | Install or remove Boatstack-managed repository and controller resources. |
| `command.execute` | Invoke a configured command or component runtime. |
| `product.mutate` | Change objective, plan, workspace, gate, evidence, delivery, or publication state. |
| `publication.prepare` | Create a publication preview artifact. |
| `publication.publish` | Perform or correct an external publication. |
| `human.approve` | Cross an admission policy that requires human approval. |

`repository.read` is intentionally absent. Current observation uses ambient
host reads and cannot distinguish that capability at an enforceable boundary.
Capability names are exact identifiers with no aliases or implied hierarchy.

The kernel maps existing authority receipt classes to grants. Repository,
human, autonomy, and external-provider receipts remain the evidence sources;
program declarations are never evidence. A repository-policy receipt is
derived only from current verified configuration evidence. Human approval and
external publication remain separate grants.

## Exact bindings

A prescription binds transition, state, program fingerprint, authority-source
fingerprint, required capabilities, and effective capabilities. Apply must use
the same context; changed authority requires re-resolution. Admissions and
receipts also retain granted capabilities and non-secret authority provenance.
A transition receipt is evidence only and is not accepted as an authority
receipt.

Recovery uses a new admission. It does not inherit stronger capability from a
failed transition. Program capability-surface changes alter the program
fingerprint and invalidate old prescriptions.

## Command execution frontier

`command.execute` is a high-power capability. An arbitrary subprocess can use
filesystem permissions, Git credentials, environment credentials, and network
access to perform effects outside Boatstack's mediated handlers. Without an OS
sandbox or external broker, these finer guarantees have the following scope:

| Guarantee | Status |
| --- | --- |
| Program cannot self-grant a Boatstack capability | Supported |
| Kernel-mediated writes/publication require their exact capability | Supported |
| Helpers and component runtimes receive the admitted context | Supported |
| Arbitrary command cannot write files or publish through ambient tools | Not enforceable with arbitrary command execution |
| Complete inventory of host credential effects | Unknown |

Boatstack does not claim process isolation. The capability boundary governs
kernel-mediated effects; `command.execute` explicitly crosses into the host
process trust domain.
