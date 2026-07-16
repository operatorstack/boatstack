# Harness-neutral portability

## Canonical package and adapters

The source of truth is `.product-loop/`:

- `project.json`: repo-specific facts and policy;
- `workflow.md`: state machine and gate semantics;
- `artifacts.md`: document contract;
- `failure-moves.md`: failure taxonomy and experimental rules;
- `templates/`: artifact templates;
- `generated.lock.json`: generator version, config hash, and generated file list.

Host-specific files are compiled adapters:

- Cursor: `.cursor/rules/product-engineering-loop.mdc` and `.cursor/commands/*.md`;
- Claude Code: `.claude/skills/product-engineering-loop/SKILL.md`;
- Codex: `.agents/skills/product-engineering-loop/SKILL.md`;
- GitHub: `.github/PULL_REQUEST_TEMPLATE/product-engineering-loop.md`.

Adapters point to the canonical package; they do not copy its full reasoning. This keeps behavior consistent while letting each host expose its native invocation surface.

## Repository ownership

The exporter must not replace:

- `AGENTS.md`;
- `CLAUDE.md`;
- existing Cursor rules or commands;
- CI or PR templates with the same path;
- any file without the generated marker.

If a collision exists, stop and show the conflict. A human may move durable content into `.product-loop/project.json`, choose another adapter path, or explicitly reconcile it in a PR.

## Export PR contract

An installation or update PR should show:

- canonical loop version and config hash;
- host adapters added or changed;
- project context paths and real verification commands;
- existing instructions left untouched;
- collisions or unsupported host features;
- dry-run/check output;
- rollout and removal steps.

Generated output is reviewable code. Do not auto-merge it simply because generation succeeded.

## Host notes

### Cursor

Use project rules in `.cursor/rules/*.mdc`; `.cursorrules` is legacy. Use `.cursor/commands/*.md` for the named workflow commands. Keep the rule short and point it to `.product-loop/` artifacts. Cursor CLI also reads `AGENTS.md` and `CLAUDE.md`, so avoid duplicating those files into the generated rule.

### Claude Code

Use a project skill under `.claude/skills/`. Keep `CLAUDE.md` as project-owned durable context. If using the Agent SDK in automation, explicitly enable project setting sources when repository instructions are required; do not assume the SDK loads filesystem settings by default.

### Codex

Use a repo skill under `.agents/skills/`. Keep `AGENTS.md` concise for persistent repo conventions and route task-specific workflow detail into the skill and `.product-loop/` references.

### GitHub

The generated PR template collects evidence; branch protection and CI remain the enforcement layer. A future exporter can generate opt-in CI, but it must use commands from `project.json` and never invent repository checks.
