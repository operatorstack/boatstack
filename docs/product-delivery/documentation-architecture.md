# Documentation architecture

Product Delivery documentation has three present-tense owners:

| Information | Authority |
| --- | --- |
| Flow semantics and authoring decisions | Product Delivery guides |
| Exact TypeScript signatures and comments | generated TypeDoc from public declarations |
| Exact compiled entries, transitions, authority, and bindings | canonical Control Program artifact and generated projections |

The authoring SDK produces declarative IR. Its helpers do not execute effects,
grant authority, or define a second runtime. Guides explain how repositories
compose those helpers; TypeDoc records exact API contracts; the compiler and
runtime remain executable authority.

Repository-specific Flow documentation must derive from the checked canonical
artifact rather than interpreting TypeScript source. Generated host files are
presentation surfaces and are owned per exact path. They do not become an
independent lifecycle or authority source.

Use the [TypeScript documentation map](../typescript/index.md) for API
navigation, [Writing a Flow](writing-a-flow.md) for composition, and
[Generated files](../generated-files.md) for artifact ownership.
