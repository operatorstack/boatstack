# Repository Control Program IR

Boatstack separates authoring languages from executable semantics:

```text
TypeScript Flow -> raw Control Program IR -> Go canonicalizer -> committed artifact -> kernel
```

The `control-program/v1` IR is domain-neutral. It declares typed facets,
evidence relations, predicate ASTs, operators, capabilities, authority,
effects, verification, recovery, transitions, marked targets, and entries.
Software terms such as plans, tests, Git, and pull requests belong to
`@operatorstack/boatstack-software-delivery`, not the base SDK.

## Compile and check

Install the TypeScript frontend in the repository, then run:

```sh
boatstack flow compile --repo .
boatstack flow check --repo .
boatstack next --repo . --flow product-delivery --entry run
```

Compilation executes the TypeScript frontend and emits raw IR. Boatstack then
strictly validates, canonicalizes, fingerprints, and writes the committed
`.flow.ir.json` artifact and generated Codex and Claude skills. Runtime commands
never execute `flow.ts`.

The artifact binds the source hash, compiler version, dependency-lock hash,
trusted operator fingerprints, canonical program fingerprint, and generated
skill hashes. Unknown fields, duplicate declarations, invalid references,
undeclared inline effects, missing recovery, binding drift, and generated-file
drift fail closed.

Trusted software-delivery bindings fix capabilities, authority, effects,
verifiers, recovery, and state effects. A repository may select and order those
operators and add conjunctive guards. It cannot weaken or replace the trusted
contract. Durable software state continues to pass through the existing
schema-v4 declared-effect reducer and native-handler boundary.
