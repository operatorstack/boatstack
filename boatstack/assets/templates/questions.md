# Question ledger: <feature>

| ID | Question | Why it matters | Options | Recommendation | Answer | Source | Status/expiry |
|---|---|---|---|---|---|---|---|

Use `ANSWERED` only for an explicit human answer or an authoritative existing contract. In an explicit goal-driven run, use `RESOLVED_BY_POLICY` only when the structured autonomy envelope proves the choice non-material, within-spec, reversible, free of protected impacts, evidence-backed, and independently verifiable. Repository inference is otherwise `PROPOSED`. Material unanswered questions remain `OPEN`, appear in `plan.md` as `blocking_questions`, and block activation.

When presenting finite questions, give every choice a compact inline-code key (`1a`, `1b`, `1c`, then `2a`, `2b`, and so on) and suffix exactly one choice per question with `(Recommended)`. End with one reply hint: name the keys for explicit selection, or use `r` to accept all displayed recommendations. A standalone `r` is `ANSWERED` human provenance only after the selected question-to-answer mapping is echoed; it is never an agent-selected default.
