# ZCP — Headless Operation

> **Status**: Operator-facing reference. Describes the prerequisites
> for running `zcp serve` against an automated agent (Claude Code
> `--print`, unattended scripts, CI flows).
> **Date**: 2026-04-26

---

## Required setup: `zcp init`

`zcp serve` requires `CLAUDE.md` to be present in the working
directory the agent will operate from. Without it, the agent has only
tool descriptions — workflow doctrine, the canonical `status` recovery
primitive, and SSHFS mount semantics are all delivered through
CLAUDE.md, **not** through MCP init.

```
cd /path/to/working/dir
zcp init
```

`zcp init` is idempotent — re-running re-stamps the managed section of
CLAUDE.md without overwriting user additions outside the
`<!-- ZCP:BEGIN -->` / `<!-- ZCP:END -->` markers.

Container env additionally writes SSH config, git identity, and a
global Claude Code MCP entry. Local env writes a project-scoped
`.mcp.json` carrying the per-project `ZCP_API_KEY`.

## Verifying

`zcp serve` prints a stderr warning at startup if CLAUDE.md is missing
in cwd:

```
WARNING: no CLAUDE.md in working directory; MCP-only mode delivers no
workflow doctrine. Run `zcp init` here first for full agent guidance.
```

If the warning fires, run `zcp init` and restart the serve process.
The warning is silent on success — no warning means CLAUDE.md was
found and doctrine will be delivered through auto-discovery.

## Why not auto-inject doctrine via MCP init

`internal/server/instructions_test.go::TestBuildInstructions_NoStaticRulesLeak`
forbids static doctrine in the MCP `Instructions` field. The reason is
duplication: the same prose lived in two places (template + MCP init)
and drifted. CLAUDE.md is the single source of truth for workflow
doctrine; `zcp init` is the deployment mechanism.

A long-lived container's CLAUDE.md is also auto-refreshed when the
embedded template changes between releases (see
`internal/content/refresh_claude.go`); operators only need to run
`zcp init` once per working directory.
