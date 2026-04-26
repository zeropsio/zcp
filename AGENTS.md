# Codex Project Instructions

This repository is shared with Claude Code. The authoritative project
instructions live in:

1. `CLAUDE.md`
2. `CLAUDE.local.md` when present

Before doing repository work, read both files and follow them as active
instructions. Treat `CLAUDE.local.md` as local/private operator policy; it is
gitignored and may contain machine-specific access, release, and verification
rules.

## Codex Adapter

- Apply all project architecture, testing, release, Zerops, and engineering
  rules from `CLAUDE.md` and `CLAUDE.local.md`.
- Where those files mention Claude Code-specific hooks, commands, agents, or
  allowlists, translate the intent to the available Codex tools instead of
  ignoring the rule.
- If a rule references a Claude-only command such as `/audit`, use the closest
  Codex workflow manually and preserve the same constraints and approval gates.
- Do not edit `CLAUDE.local.md` unless the user explicitly asks for local
  machine policy changes.
- Keep this file as a bridge only. Durable project invariants belong in
  `CLAUDE.md`; machine-local policy belongs in `CLAUDE.local.md`.
