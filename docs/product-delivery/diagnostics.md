# Diagnostics and `boatstack explain`

An entry can request an explanation when execution suspends:

```ts
entry({
  id: "run",
  target: "published-pr",
  diagnostics: { explain_on_suspend: true },
});
```

This option changes generated-agent UX. It is bound into the generated artifact
and skills, but it is excluded from the executable Control Program fingerprint.
It cannot make a transition admissible or provide authority.

`boatstack explain` is read-only. It reports the current decision trace,
including candidates and rejection reasons. Treat that explanation as evidence
for deciding the next action, never as permission to perform an effect.
