@AGENTS.md

## Claude Code only

- `/flow` is the Claude skill in `.claude/skills/flow/`; its router, DNA and phase files own the workflow and gates.
- Claude agent definitions live in `.claude/agents/`; hook wiring and tool allow/deny lists live in `.claude/settings.json`, with hook implementations in `.claude/hooks/`. Local overrides belong in `CLAUDE.local.md` and `.claude/settings.local.json`.
