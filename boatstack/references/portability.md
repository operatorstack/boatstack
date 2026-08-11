# Portability

The canonical hosts are CLI, Cursor, Codex, Claude Code, Gemini CLI, and MCP.
They share one request/response protocol and one prescription AST.

POSIX and Git Bash use the same quoting projection. PowerShell uses its own
quoting rules over identical argument values. Tests compare semantic arguments,
not presentation whitespace.

Local replacement is atomic on Unix and Windows. Workspace cleanup runs from a
neutral directory so Windows never has to delete a process's current directory.
Release CI compiles the runtime for Linux, macOS, and Windows.
