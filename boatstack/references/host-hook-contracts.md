# Host hook contracts

Verified against the published host contracts on 2026-07-19. Recheck these
sources before changing a generated adapter or making a stronger enforcement
claim.

| Host | Configuration and event | Blocking result | Activation boundary |
| --- | --- | --- | --- |
| Cursor | `.cursor/hooks.json`; `beforeShellExecution` and `beforeMCPExecution` | JSON `permission: "deny"`; generated entries set `failClosed: true` | Reload and host enablement are operator-visible. A current fast-exit output race is mitigated with a 50 ms settle delay, but Cursor remains defense in depth. |
| Claude Code | `.claude/settings.json`; `PreToolUse` | Exit 0 with `hookSpecificOutput.permissionDecision: "deny"`, or exit 2 with a secret-free error | The generated command explicitly uses Bash and `${CLAUDE_PROJECT_DIR}`. Reload and confirm with `/hooks`. |
| Codex | `.codex/hooks.json`; `PreToolUse` | Exit 0 with `hookSpecificOutput.permissionDecision: "deny"`, or exit 2 with a secret-free error | The project path and exact hook hash must be reviewed and trusted. A linked worktree is a distinct project path. Start a new task after trust changes. |

Sources:

- Cursor: https://cursor.com/docs/hooks
- Claude Code: https://code.claude.com/docs/en/hooks
- Codex: https://learn.chatgpt.com/docs/hooks

## Compatibility policy

The shared classifier accepts only the normalized tool name and input produced
by a host adapter. Current event names are authoritative. Missing event names
receive bounded legacy support only when the payload is unambiguous; unknown,
ambiguous, or malformed events deny without echoing tool arguments.

Deterministic schema, payload, decision, exit-code, and hydration fixtures block
release. Live host checks are opt-in through `BOATSTACK_LIVE_HOST_TESTS=1` and
report host availability separately from deterministic conformance.
