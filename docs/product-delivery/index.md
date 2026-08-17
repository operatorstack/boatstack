# Product Delivery Flow authoring

Boatstack repositories own their delivery control law. The TypeScript SDK is an
authoring frontend that produces declarative Control Program IR; TypeScript is
not executed by the Boatstack runtime.

```text
Flow TypeScript
    ↓
canonical Control Program IR
    ↓
trusted software-delivery bindings
    ↓
Boatstack runtime
```

Use the API reference for exact signatures and these guides for the control
semantics behind those signatures:

- [Writing a Flow](writing-a-flow.md)
- [Targets and entries](targets-and-entries.md)
- [Authority and delegation](authority-and-delegation.md)
- [Planning and foreground work](planning-and-foreground-work.md)
- [Lifecycle, evidence, and publication](lifecycle-evidence-and-publication.md)
- [Diagnostics and `boatstack explain`](diagnostics.md)
- [Documentation architecture](documentation-architecture.md)

The internal runtime model is documented separately in the
[Control Program IR specification](../control-program-ir.md) and
[software-delivery architecture](../architecture/software-delivery-domain.md).
