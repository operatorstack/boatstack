# Failure taxonomy and move catalog

Select a move only after locating the failure below its surface symptom. “Timed out,” “tests failed,” and “the agent got confused” are starting observations, not diagnoses.

| Failure class | Evidence | Candidate moves | Main regression risk |
|---|---|---|---|
| Unknown requirement | Plausible implementations disagree on product behavior | Ask a targeted human question; record answer and expiry | Invented requirements or stalled delivery |
| Context miss | Relevant interface/invariant existed but was not loaded | Reload minimal relevant context; add routing reference | Blind truncation removes useful state |
| Protocol malformed | Invalid JSON/schema/tool call despite recoverable intent | Parse repair; schema validation; constrained retry | Retrying semantic errors as syntax |
| Tool/transport | API, shell, network, or environment failure | Classify retryability; bounded retry; fallback; resume | Duplicate side effects or retry storms |
| Step/budget exhaustion | Progress is still converging at cap | Continue from checkpoint; conditional budget increase | More time converts timeout into wrong answer or thrash |
| Thrashing | Repeated actions without new evidence | Stop after repeated tactic; re-diagnose; stronger planner | Spending more tokens on the same loop |
| Implementation correctness | Independent tests fail the contract | Local repair from failing evidence; narrower task | Rebuilding and losing near-correct work |
| Test fidelity | Tests pass wrong code or reject correct code | Contract fixtures; collect/load gate; mutation/differential/human oracle | Treating more model-authored tests as truth |
| Review miss | Defect found after same-agent review | Independent reviewer; risk checklist; mechanical enforcement | Expensive review everywhere |
| Scope drift | Diff no longer maps to approved outcomes | Re-scope; split PR; update spec with approval | Hiding product changes in implementation |
| Update self-lockout | An installed helper, stale hook event, or damaged owned receipt blocks its own updater | Let the verified target helper classify state; migrate exact provenance automatically or offer fingerprinted `--repair` | Reinstalling blindly, overwriting user settings, or treating `--repair` as downgrade authority |
| Ownership projection contradiction | Update admission classifies a path as Boatstack-owned, then final validation rejects the controller's own bounded mutation | Build one semantic ownership projection before execution; reuse it for admission, mutation, final verification, staging, and preview | Path-only allowlists accepting user content or independently maintained validators disagreeing after a side effect |
| Security/tenancy | Trust boundary or data scope violated | Specialist review; invariant test; deny-by-default guard | Generic prompt mistaken for enforcement |
| Integration/deploy | Local pass but runtime fails | Environment parity; canary; health checks; rollback | Treating staging as identical to production |
| Documentation drift | Durable behavior and docs disagree | Update source-of-truth artifact; drift check | Growing instructions with unverified rules |
| Irreversible recovery escalation | A failed external operation causes authority/target broadening or an invented reset | Immutable pre-execution deny; preserve state; read-only diagnosis; transactional retry or fix forward | False denial of legitimate isolated development operations |
| Worktree bootstrap deadlock | A linked worktree inherits fail-closed hooks but not the ignored runtime required to evaluate them | Versioned Git-common runtime; atomic first-use hydration; provenance check | Cross-version execution or weakened failure behavior |
| Post-publication correction routing | CI, review, or a denied push targets work already marked published | Resolve branch and recorded PR identity; append the observation; draft an independently approved corrective child | Treating PR creation as completion or asking the user to bypass the guard |
| Unobserved side-effect completion | The same visible state could mean not started, executing, succeeded with a lost response, or failed | Durable operation receipt; exact lease; observe completion; reconcile the expected postcondition before retry | Conversation-scoped retry loops, duplicate PRs, or phantom success |

## Lessons encoded from the benchmark campaign

- **Parse repair is a protocol move.** It can recover malformed completion without pretending to improve reasoning.
- **More steps are conditional.** Qwen experiments reduced step exhaustion but largely converted it into confident wrong answers. Increase budget only when trajectories show continuing progress.
- **Strict self-checking is not monotonic.** A stricter prompt caused collateral rework and regression. Preserve a known-good snapshot and require an oracle with fidelity to the real goal.
- **Self-authored tests are scaffolding before they are truth.** Spec-first helped a development slice but its frozen oracle agreed poorly with the hidden grader and did not transfer to the full board.
- **Development promotion is not product promotion.** A +7 point development result became a statistical wash on the full distribution. Representative evaluation and holdout remain mandatory.
- **Do not discard near-correct work.** Repair attempts can wash or regress, so retain prior evidence and compare states.
- **Model changes relocate the bottleneck.** The same harness exposed different binding modes on Gemini and Qwen. Route moves by measured failure population, not by a universal “best loop.”
- **Tool failure must not create recovery authority.** The sanitized database incident moved from a partial schema apply failure to an invented reset path. The irreversible-operation guard is `PROPOSED`, not promoted: evaluate its deny corpus, safe corpus, latency, and workflow regressions against the unguarded baseline.
- **Fail-closed controls need an available evaluator.** A linked worktree copied the safety hook but not its ignored helper, so the guard also denied its own repair command. Share only the verified runtime within the Git clone and hydrate local ignored state before judging the original event.
- **A retry needs a new observation.** Identical in-flight calls wait. Unknown non-idempotent calls enter `RECONCILE_REQUIRED`; Git, GitHub, filesystem, browser, and MCP boundaries must observe their exact postcondition before another attempt consumes the persistent budget.
- **Preconditions run before leases.** Wrong branch, stale base, or invalid diff state returns a recovery operation without creating a durable attempt. A rejected precondition cannot consume retry budget or leave an identity that collides with the corrected invocation.

## Move proposal schema

Before experimenting, record:

```yaml
id: stable-move-name
target_failure: one-class
population: observable predicate selecting affected runs
mechanism: why this intervention should change the outcome
change: one minimal behavioral delta
expected_effect: directional metric prediction
cost: latency, tokens, money, and human attention
risks: plausible regressions and affected populations
smoke: cheapest mechanism check
evaluation: paired sample, representative distribution, holdout
rollback: identity/default behavior
decision: PROPOSED | PROMOTE | REJECT | WASH
```

Never promote from an unpaired anecdote, a mid-run aggregate with mismatched coverage, or a metric produced solely by the model being evaluated.
