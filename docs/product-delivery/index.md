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
- [Diagnostics and `boatstack explain`](diagnostics.md)
- [Documentation architecture](documentation-architecture.md)

The internal runtime model is documented separately in the repository's
[Control Program IR specification](https://github.com/operatorstack/boatstack/blob/main/docs/control-program-ir.md).
