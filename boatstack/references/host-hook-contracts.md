# Host hook contracts

Verified against the published host contracts on 2026-07-22. Recheck these
sources before changing a generated adapter or making a stronger enforcement
claim.

| Host | Configuration and event | Blocking result | Activation boundary |
| --- | --- | --- | --- |
| Cursor | `.cursor/hooks.json`; synchronous `preToolUse` plus `beforeShellExecution` and `beforeMCPExecution` | JSON `permission: "deny"`; generated entries set `failClosed: true` | Reload and host enablement are operator-visible. Native Write/Edit tools, shell, and MCP mutations all cross the same guard. |
| Claude Code | `.claude/settings.json`; `PreToolUse` | Exit 0 with `hookSpecificOutput.permissionDecision: "deny"`, or exit 2 with a secret-free error | The generated command explicitly uses Bash and `${CLAUDE_PROJECT_DIR}`. Reload and confirm with `/hooks`. |
| Codex | `.codex/hooks.json`; `PreToolUse` | Exit 0 with `hookSpecificOutput.permissionDecision: "deny"`, or exit 2 with a secret-free error | The project path and exact hook hash must be reviewed and trusted. A linked worktree is a distinct project path. Start a new task after trust changes. |
| Gemini CLI | `.gemini/settings.json`; `BeforeTool` | JSON `decision: "deny"` with a secret-free reason | The generated sequential hook supervises every tool and uses the same repository guard. Reload after installation. |

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

Publication denials carry only secret-free recovery context: blocking feature
and slice, branch relation, parent delivery, and the read-only next operation.
Every host receives the same instruction to preserve edits and enter managed
recovery. A host must never translate denial into a request that the user repeat
the push or PR mutation manually.

Pre-activation denials use `workflow-phase-bypass` and may add only the feature,
observed workflow stage, attempted repository path, and deterministic next
operation. No task notification, conversation turn, or async completion changes
the authorization decision.
