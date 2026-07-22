### Block product mutation between auto-plan and activation

Boatstack now latches managed authority when `auto-plan` saves a feature plan.
Native edits, mutation-capable MCP calls, package installation, and shell commands
not proven read-only are denied until build activation creates a current lock.
Cursor native tools and Gemini `BeforeTool` now join the existing shell and host
guards. Plan approval also fingerprints any pre-existing product diff, preserves
it, and rejects drift before approval or activation. Human approval remains enabled
by default, unmanaged pre-auto-plan work is unchanged, and schema-v1 approval
receipts remain valid with a clean baseline.
