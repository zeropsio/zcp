# ZCP — Headless Operation

> **Status**: Operator-facing reference. Describes the prerequisites
> for running `zcp serve` against an automated agent (Claude Code
> `--print`, unattended scripts, CI flows).
> **Date**: 2026-04-26

---

## Required setup: `zcp init`

`zcp serve` expects the agent's working directory to carry agent
context. Without it, the agent has only tool descriptions — workflow
doctrine, the canonical `status` recovery primitive, and SSHFS mount
semantics are all delivered through the agent-context files, **not**
through MCP init.

`zcp init` writes two files:

- **`AGENTS.md`** — the canonical doctrine body.
- **`CLAUDE.md`** — a thin `@AGENTS.md` include wrapper (Claude Code
  reads only `CLAUDE.md`, so it points at the canonical body).

```
cd /path/to/working/dir
zcp init
```

`zcp init` is idempotent — re-running re-stamps the managed section of
each file without overwriting user additions outside the
`<!-- ZCP:BEGIN -->` / `<!-- ZCP:END -->` markers.

Container env additionally writes SSH config and a global Claude Code
MCP entry (`~/.claude.json`, via the Claude adapter's `ContainerInit`).
It writes **no** git identity — git setup for mounted dev services
happens at bootstrap (`ops.InitServiceGit`), not at `zcp init`. Local
env writes a project-scoped `.mcp.json` carrying the per-project
`ZCP_API_KEY`.

## Failure posture: required vs best-effort steps

`zcp init` is routinely a `run.init` command in `zerops.yml`, so its exit
code gates the container start — a non-zero exit fails the whole service.
Each init step is therefore classified (`internal/init/init.go`,
`step.degraded`):

- **Required** — the agent context (`AGENTS.md` + `CLAUDE.md`), and the
  local `.mcp.json`. These are what `zcp init` delivers; a failure aborts
  init with a non-zero exit. `.mcp.json` is local-only, so failing loudly
  costs no container start.
- **Best-effort** — skill roots, permissions, Cursor project config, shell
  aliases, the guided skill, trusted domains, SSH config. A failure prints
  a warning naming the step, the underlying cause, and what stops working,
  then init carries on. Registering a best-effort step **requires** stating
  that cost in `step.degraded`.

Best-effort is not blanket tolerance: it exists because these steps write
into paths other tools own, where ZCP can meet an entry it did not create
and must not repair. Concretely, `.agents/` and `.claude/` may be symlinks
into persistent storage that mounts **after** `run.init` runs; `os.MkdirAll`
then reports a bare `mkdir .agents: file exists`, which used to abort the
start. ZCP now names the entry (`ensureDir` / `describeObstruction`: what it
is, and for a symlink its target), leaves it exactly as found — it may
become valid seconds later — and continues.

The same rule bounds what a warning may hide: a step that would otherwise
overwrite unexpected user content still refuses to write and still reports
the file by name (`.cursor/cli.json` wrong-shape or malformed). Only the
exit code softens; nothing is silently rewritten.

Per-agent adapter dispatch (`runContainerAdapters`) follows the same rule
at adapter granularity: a failing `Detect`, `Validate`, or `ContainerInit`
skips that adapter with a warning and never blocks the others or the boot.
Recovery is re-running `zcp init` once the path resolves.

## Verifying

`zcp serve` prints a stderr warning at startup only when **both**
`AGENTS.md` and `CLAUDE.md` are missing in cwd (either present →
silent):

```
WARNING: no AGENTS.md or CLAUDE.md in working directory; MCP-only mode
delivers no workflow doctrine. Run `zcp init` here first for full agent
guidance.
```

If the warning fires, run `zcp init` and restart the serve process.
The warning is silent on success — no warning means at least one of
the agent-context files was found and doctrine will be delivered
through auto-discovery.

## Why not auto-inject doctrine via MCP init

`internal/server/instructions_test.go::TestBuildInstructions_NoStaticRulesLeak`
forbids static doctrine in the MCP `Instructions` field. The reason is
duplication: the same prose lived in two places (template + MCP init)
and drifted. CLAUDE.md is the single source of truth for workflow
doctrine; `zcp init` is the deployment mechanism.

A long-lived install's agent context is also auto-refreshed when the
embedded template changes between releases (see
`internal/content/refresh_agents.go`, `RefreshAgentContext`), which
re-stamps the managed section of **both** AGENTS.md and CLAUDE.md and
runs at serve startup in both local and container envs; operators only
need to run `zcp init` once per working directory.
