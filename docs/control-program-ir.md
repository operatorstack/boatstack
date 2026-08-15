# Repository Control Program IR

Boatstack separates authoring languages from executable semantics:

```text
TypeScript Flow -> raw Control Program IR -> Go canonicalizer -> committed artifact -> kernel
```

The `control-program` schema at revision `3` is domain-neutral. It declares
typed facets, evidence relations, predicate ASTs, operators, capabilities,
authority, effects, verification, recovery, bounded foreground work,
transitions, marked targets, and entries.
Software terms such as plans, tests, Git, and pull requests belong to
`@operatorstack/boatstack-software-delivery`, not the base SDK.

Trusted operator authority remains algebraic: `any_of` lists alternatives and
`all_of` lists mandatory classes. Repository transitions may add mandatory
authorities through `requires.authorities`; they cannot add alternatives or
grant authority. Entries may request a trusted delegation binding, but only the
runtime-owned authorization record can grant it for an exact run.

## Compile and check

Install the TypeScript frontend in the repository, resolve its absolute path,
then run:

```sh
boatstack flow compile --repo . \
  --frontend "$(pwd)/node_modules/.bin/boatstack-flow-frontend"
boatstack flow check --repo .
boatstack next --repo . --flow product-delivery --entry run
```

If the entry requests delegation, `next` returns `DELEGATION_REQUIRED` with an
exact run and request fingerprint before managed state changes. A human can
authorize that exact request and continue it:

```sh
boatstack flow authorize --repo . --flow product-delivery --entry run \
  --run-id <run-id> --request-fingerprint <fingerprint> --human <actor>
boatstack flow run --repo . --flow product-delivery --entry run --run-id <run-id>
boatstack flow revoke --repo . --run-id <run-id> --human <actor>
```

An entry may opt its generated Codex and Claude projections into factual
diagnosis when a run suspends:

```ts
entry({
  id: "run",
  target: "published-pr",
  diagnostics: { explain_on_suspend: true },
})
```

This setting changes generated-agent UX, not executable control semantics. It
is excluded from the executable program fingerprint while the source,
artifact, and generated-skill hashes still bind it. The generated skill calls
`boatstack explain` with the same run context and treats the explanation as
evidence, never as authority.

Compilation sends the exact source bytes to a restricted TypeScript frontend.
The frontend path is explicit authority: Boatstack never selects or executes a
repository `node_modules/.bin` program automatically.
The frontend accepts only literal data and calls to named exports from trusted
Boatstack SDKs. It rejects local imports and other repository code without
executing them. Boatstack then validates, canonicalizes, fingerprints, and
projects generated skills, retires obsolete skills, and publishes the committed
`.flow.ir.json` artifact last as one serialized update. Runtime commands never
execute `flow.ts`. The artifact filename comes from the declared program ID,
not the source filename.

The artifact binds the source hash, compiler version, dependency-lock hash,
foreground-work instruction and schema assets, trusted operator fingerprints,
canonical program fingerprint, and generated skill hashes. Unknown fields,
duplicate declarations, invalid references,
undeclared inline effects, missing recovery, binding drift, and generated-file
drift fail closed. A source, lock, instruction, or schema change during
compilation also fails closed.

## Foreground work

A Flow may require bounded human or agent work before a trusted transition can
be prescribed. The repository declares an instruction asset, exact entry
inputs, and an output manifest. Boatstack resolves the assets during compile,
creates a runtime-owned work request for the selected transition, and verifies
the staged outputs before it admits the trusted operator.

Foreground work cannot change Flow state, grant authority, or install an
effect handler. Its result is immutable evidence bound to one run, program,
transition, state revision, repository, and worktree. Questions suspend the
same run; answers are evidence rather than authority. A program or state change
invalidates the result before any trusted effect.

The foreground-work commands are explicit and foreground-only:

```sh
boatstack flow work show --repo . --flow <flow> --entry <entry> \
  --run-id <run-id> --work-id <work-id> --format json
boatstack flow work input-required ... --prompt "<question>"
boatstack flow work answer ... --question-id <question-id> --answer <json-file>
boatstack flow work complete ...
boatstack flow work block ... --reason "<reason>"
```

`complete` reads only the declared regular files below the request's staging
root. It checks paths, media types, size limits, JSON syntax and declared JSON
Schemas, then seals the exact bytes into the work result. The following
`next` call can prescribe the transition only with that exact result
fingerprint.

Trusted software-delivery bindings fix capabilities, authority, effects,
verifiers, recovery, and state effects. A repository may select and order those
operators and add conjunctive guards. It cannot weaken or replace the trusted
contract, alias a trusted transition identity, or leave required entry inputs
unresolved. Durable software state continues to pass through the existing
schema-v4 declared-effect reducer and native-handler boundary.
