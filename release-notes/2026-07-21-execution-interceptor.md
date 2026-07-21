### Intercept eager AI execution from native plan mode

Added a new "Execution Interceptor" boundary rule that is injected into repository global instructions (`GEMINI.md`, `CLAUDE.md`, `.cursorrules`, and `.cursor/rules/boatstack.mdc`) during Boatstack initialization and export. This instructs AI agents (like Gemini, Claude, and Cursor) to pause after the user approves a native plan, save it to `source-plan.md`, and suggest executing it through `/boatstack run` rather than blindly proceeding into Auto-Edit mode to mutate product files.
