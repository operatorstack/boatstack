### A read-only `root-cause` operation diagnoses a bug before you plan the fix

Boatstack already carried the discipline of eliminating a failure *class* rather than
patching one instance — the failure taxonomy in `failure-moves.md` and the symptom-vs-
systemic-boundary decision `auto-plan` surfaces under `boundary_analysis` — but there was
no dedicated entry point for turning a raw bug into that kind of plan. Diagnosis and
planning were fused inside `auto-plan`, so a stack trace had no home of its own.

The new `root-cause` operation is that home. Invoke it as `/root-cause <symptom-or-log>`
(`$boatstack root-cause` in Codex) with a stack trace, error, alert, or failing signal.
It is strictly read-only: it never edits product code, writes artifacts, contacts GitHub,
or advances a gate. It locates the failure below its surface symptom, classifies it
against the classes in `.product-loop/failure-moves.md`, and produces a numbered
root-cause chain in which every step is cited to `file:line` and the crashing frame (the
victim) is distinguished from the true origin (the cause). It then maps the blast radius —
every other call site exposed to the same class — and proposes the minimal *structural*
elimination that makes the whole class unreachable, using the same tiered
`[1a] Symptom Patch` / `[1b] Programmatic Enforcement` framing as `auto-plan`, plus a
regression that reproduces the failure mode as the proof the class is gone.

Its output is formatted as a host Plan-mode source plan. `root-cause` ends by making the
one next action explicit: save that plan to a durable in-repo path and run
`auto-plan --plan <path>`. It is the diagnostic front door to the existing plan gate and
changes nothing about how planning, approval, gates, or publication work.
