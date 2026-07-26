# Host hook contracts

Verified against the published host contracts on 2026-07-22. Recheck these
sources before changing a generated adapter or making a stronger enforcement
claim.

| Host | Configuration and event | Blocking result | Activation boundary |
| --- | --- | --- | --- |
| Cursor | `.cursor/hooks.json`; synchronous `preToolUse`, `postToolUse`/`postToolUseFailure`, `beforeShellExecution`/`afterShellExecution`, and `beforeMCPExecution`/`afterMCPExecution` | JSON `permission: "deny"`; generated entries set `failClosed: true` | Reload and host enablement are operator-visible. Native Write/Edit tools, shell, and MCP mutations cross the guard and completion observer. |
| Claude Code | `.claude/settings.json`; `PreToolUse`, `PostToolUse`, and `PostToolUseFailure` | Exit 0 with `hookSpecificOutput.permissionDecision: "deny"`, or exit 2 with a secret-free error | The generated command explicitly uses Bash and `${CLAUDE_PROJECT_DIR}`. Reload and confirm with `/hooks`. |
| Codex | `.codex/hooks.json`; `PreToolUse` and `PostToolUse` | Exit 0 with `hookSpecificOutput.permissionDecision: "deny"`, or exit 2 with a secret-free error | The project path and exact hook hash must be reviewed and trusted. A linked worktree is a distinct project path. Start a new task after trust changes. |
| Gemini CLI | `.gemini/settings.json`; `BeforeTool` and `AfterTool` | JSON `decision: "deny"` with a secret-free reason | The generated sequential hooks supervise requests and observe results through the same repository guard. Reload after installation. |

Sources:

- Cursor: https://cursor.com/docs/hooks
- Claude Code: https://code.claude.com/docs/en/hooks
- Codex: https://learn.chatgpt.com/docs/hooks
- Gemini CLI: https://geminicli.com/docs/hooks/reference/

## Compatibility policy

The shared classifier accepts only the normalized tool name and input produced
by a host adapter. Current event names are authoritative. Missing event names
receive bounded legacy support only when the payload is unambiguous; unknown,
ambiguous, or malformed events deny without echoing tool arguments.

Deterministic schema, payload, decision, exit-code, and hydration fixtures block
release. Live host checks are opt-in through `BOATSTACK_LIVE_HOST_TESTS=1` and
report host availability separately from deterministic conformance.

During an update, the committed fragment from the installed release is the
ownership witness. Entries that match it exactly may move to new events or be
removed when an event retires, even when the target release no longer accepts
that old event. A marker-only or modified entry requires a fingerprinted
`--repair`; unrelated host entries are preserved, and malformed or mixed state
remains blocking.

Publication denials carry only secret-free recovery context: blocking feature
and slice, branch relation, parent delivery, and the read-only next operation.
Every host receives the same instruction to preserve edits and enter managed
recovery. A host must never translate denial into a request that the user repeat
the push or PR mutation manually.

Pre-activation denials use `workflow-phase-bypass` and may add only the feature,
observed workflow stage, attempted repository path, and deterministic next
operation. No task notification, conversation turn, or async completion changes
the authorization decision.

Managed mutations add one single-use operation lease before the host tool runs.
Post-tool events may complete only the matching kind, target, and argument
fingerprint. A delayed or duplicated completion cannot initiate work. Missing or
uncertain completion becomes `RECONCILE_REQUIRED`, and safety output may expose
only operation identity, state, attempt number, and whether reconciliation is
required.

## Denial rendering

A denial is a guardrail, not a crash. Every human-facing denial is one structured
value (`denial.go`) rendered by the surface that shows it:

- Hook decision `reason` (all hosts): a plain, multi-line message — a badge line
  (`Blocked by Boatstack`), the guidance, a reassurance line, and the recovery
  hint. This is the safe default every host displays.
- CLI errors and guard-script stderr: the same message as an ANSI soft-coral badge
  when the stream is a real terminal, and the plain form (the literal `BLOCKED:`
  prefix for CLI errors) when redirected. Controlled by `BOATSTACK_COLOR`
  (`auto` default, `always`, `never`) and `NO_COLOR`.
- Structured object (opt-in): `BOATSTACK_DENIAL_RICH=1` adds a `boatstackDenial`
  object next to the reason for a host that adopts rich denial rendering.

### Unknown-key tolerance (rechecked 2026-07-26 against the sources above)

Whether a host rejects unknown keys in the decision JSON governs the structured
object. None of the four host docs state that extra keys are rejected, and each
already defines additional optional fields (Claude `additionalContext` /
`updatedInput`; Gemini `systemMessage` / `continue`), which implies permissive
parsing — but none guarantees tolerance either.

| Host | Documented extra fields | Rejects unknown keys? | Structured-object default |
| --- | --- | --- | --- |
| Claude Code | `additionalContext`, `updatedInput` (under `hookSpecificOutput`) | Not documented; lean tolerant | off (opt-in), nested in `hookSpecificOutput` |
| Cursor | — | Not documented; lean tolerant | off (opt-in) |
| Gemini CLI | `systemMessage`, `continue` | Not documented; lean tolerant | off (opt-in) |
| Codex | — | Not documented; treat as strict (portable-only host) | off (opt-in) |

Because tolerance is unverified, the structured object is off by default and the
flat `reason` string is always complete on its own. Enable it per host only after
the host is confirmed to ignore unknown keys, and never add fields to the
Claude/Codex empty-allow path.
