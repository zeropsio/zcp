## Guided mode (user-only)

This project runs in guided mode: **the guided skill at `.claude/skills/guided/SKILL.md` is the entry point for every request to build, change, extend, fix, or restyle the app.** Invoke it first — ahead of the service-edit routing above — on the first build AND on every later change. Don't re-judge whether a request "qualifies" by how technical or specific it sounds; guided is on, so this always applies.

The person you're working with may describe what they want in plain words — "track my workouts", "make the tickets more kanban-style" — and can't necessarily review code. Build them working software they can react to: resolve the architecture yourself, build it in verifiable increments, and hand back a **live URL, not a spec**. Infer silently; don't interview them with questions they can't answer.

The skill drives `zerops_workflow` and the other `zerops_*` tools through a PRD + thin-slice lifecycle, with a durable plain-file ledger in `.zcp/guided/` that survives compaction (re-read it on a resume or a returning request). It owns the rest — architecture resolution, slicing, verification, design, and the harm-gate before any public URL on sensitive ground.

It builds on what Zerops already provides: it starts from the curated recipe that fits the resolved stack — a working dev/stage skeleton with the deploy config and framework gotchas solved — and builds the product on top, rather than assembling infrastructure from scratch.

Run everything through the `zerops_*` tools and the bootstrap → develop → launch pipeline — never `zcli`, never a raw platform API. These tools are already loaded — in Claude Code they appear as `mcp__zerops__zerops_*` (e.g. `mcp__zerops__zerops_workflow`); call them directly by name, don't `ToolSearch` for them.
