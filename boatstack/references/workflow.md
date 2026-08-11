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

The supervisor returns one of `CANDIDATE`, `PRESCRIBED`, `TERMINAL`, `FRONTIER`,
`BLOCKED`, `REFUSED`, or `UNRESOLVED`. `CANDIDATE` identifies the deterministic
next transition while required parameters remain unbound. Only `PRESCRIBED` can produce an
admission. Only an independently verified postcondition can produce a receipt.
Untargeted resolution excludes transitions whose target is already established
and transitions that encode separate maintenance, repair, abandonment, or
caller-defined marker intent. Requested resolution can still admit those
transitions when the caller explicitly selected them.

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

`delivery.slice.advance` records an explicit opaque marker only. It does not
parse a plan, persist a slice cursor, define PR cardinality, or advance a
publication sequence.

Recovery outranks ordinary progress. Goal reconfiguration is explicit and may
change an active delivery only with human or autonomy authority.
