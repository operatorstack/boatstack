### Append generic execution-mode notice to all agent templates

Boatstack now generates a clear, vendor-agnostic execution mode notice at the end of all exported routers, rules, and commands. This notice guides agents (Gemini, Claude, Cursor, and Codex) when operating in restricted modes (like Plan Mode or Read-Only Mode) to immediately halt and request the user to exit that mode or grant full tool/shell execution permissions, significantly improving Developer Experience (DX) and preventing opaque failures.
