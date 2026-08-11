# V2 workflow reference

The executable catalog is the authority. Generate the full inventory with:

```sh
boatstack catalog --format markdown
```

Event families:

- invocation and engagement;
- installation, runtime, and configuration;
- goal and plan;
- workspace;
- delivery gates and evidence;
- publication;
- recovery;
- observed external plant changes.

The supervisor returns one of `PRESCRIBED`, `TERMINAL`, `FRONTIER`,
`BLOCKED`, `REFUSED`, or `UNRESOLVED`. Only `PRESCRIBED` can produce an
admission. Only an independently verified postcondition can produce a receipt.

A normal verified-delivery path is:

```text
installation.initialize
goal.configure
engagement.begin
plan.create -> plan.validate -> plan.approve -> plan.activate
gate.build.record -> gate.test.record -> gate.review.record
TERMINAL(verified-implementation)
```

PR delivery continues through `publication.preview`,
`publication.execute`, and `publication.observe`. Merged delivery also needs
the exact workspace to become `landed`.

Recovery outranks ordinary progress. Goal reconfiguration is explicit and may
change an active delivery only with human or autonomy authority.
