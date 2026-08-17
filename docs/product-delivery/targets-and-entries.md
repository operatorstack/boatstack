# Targets and entries

The three parts of a Flow answer different questions:

```text
transitions  what may happen
targets      what counts as done
entries      which target this invocation pursues
```

A Flow may contain several marked targets, such as `published-pr` and
`safely-abandoned`. Each entry names exactly one target. Boatstack carries that
program-scoped target through resolution, prescriptions, receipts, recovery,
and replay.

An entry may also declare typed inputs. The software-delivery `inbox` helper
requires exactly one eligible Markdown plan. Input resolution happens before a
managed run is created; zero or multiple plans stop with a typed blocker.

An entry may declare `requires.authorities`. Product Delivery's `run` entry
requires `human` activation and separately requests autonomy delegation. The
runtime combines their explicit consent prompt, binds it to the exact input
fingerprints, and preserves the two scopes; an unprotected entry adds neither a
prompt nor authority.

Targets are predicates over declared facets. They do not select a hard-coded
Boatstack mode. The supervisor chooses admissible, target-coreachable
transitions from the repository's declared relation.
