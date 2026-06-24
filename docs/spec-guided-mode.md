# Spec: Guided mode (`zcp init --guided`)

Guided mode is a **user-only** opt-in that makes the coding agent drive a genuinely good
application-development **lifecycle** for a **non-technical user** — someone who describes a product
in plain words ("make me a project-management app for our real-estate firm") and cannot articulate an
architecture or review code. With guided off, a casual request yields the model's match-down default
(a static toy, data on disk). With guided on, the agent is steered to land on a real, right-sized
Zerops-native service set — *which services exist and how robustly they run* — AND to build the app
*well*: a compact PRD, thin vertical slices, per-slice TDD in fresh subagents, review, scoped deploy,
and verification, all without interrogating the user. The user reacts to **working software at a live
URL**, never to a spec.

Guided is **content-only**: there is no `zerops_guided` MCP tool, no Go state machine, no phase enum,
and no `.zcp/state/guided/*.json` ledger types. The whole lifecycle ships as expanded guided *content*
(the AGENTS.md block + a guided skill subtree) wired to the **existing** `zerops_*` tools, the existing
work-session/`action="status"`, and the unchanged bootstrap → develop → launch pipeline (§6).

This spec is the durable contract: the signal, the seams, the lifecycle, the invariants, and how it is
used. The design rationale it captures — the decision-set engine, the inference rules, the story→services
infra matrix, the two-path interaction, and the content-only lifecycle wrapping them — is owned here and
in the guided content surfaces (the AGENTS.md block + the skill subtree), pinned by the tests named below.

---

## 1. How it is used

- **Enable:** `zcp init --guided` in the project. This records the preference and materializes the
  guided surfaces. Re-running plain `zcp init` turns it **off** (a toggle).
- **Then:** the user describes an app in plain language. The agent reads the always-on guided block in
  `AGENTS.md`, which directs it to apply the guided skill (`.claude/skills/guided/SKILL.md`, a lifecycle
  *router* that points at per-phase files in `.claude/skills/guided/phases/`) before planning or
  building. Every guided session walks the lifecycle (§6) to working software at a live URL.
- **Scope:** guided is a **local, per-checkout preference** (not committed, not shared). It is
  **user-only** and is never combined with maintainer authoring mode (§4).
- **End-user product documentation** (the vibe-coder's "what guided does for me") belongs in the public
  guide repo `../zcpd-guide`, not here.

---

## 2. The signal — a local marker (G1)

The single source of truth for "guided is on for this project" is a local marker file:

```
.zcp/state/guided        # content "on"; presence = enabled
```

It lives in the gitignored `.zcp/` project meta, so guided is a **local preference for this checkout**
— deliberately not committed and not shared with teammates. Owner: `internal/content/guided.go`
(`GuidedEnabled(root)` reads it, `SetGuided(root, on)` writes/removes it). There is **no** env var, **no**
`.mcp.json` persistence, and **no** `guided` field on `runtime.Info` (which stays env/container detection
only). Pinned by `TestRun_GuidedSkill_*`.

---

## 3. Mechanism — the seams

| Seam | File | Behavior |
|---|---|---|
| Record the preference | `cmd/zcp/main.go` | `zcp init`: `guided = args has "--guided" && !rt.Authoring`; `content.SetGuided(".", guided)` runs **before** `init.Run`, so every step reads a fresh marker. |
| Render the block | `internal/content/build_agents.go` | `BuildAgentsMD(rt, guided bool)` appends the guided block iff `guided && !rt.Authoring`. `guided` is an explicit parameter — never a `runtime.Info` field. |
| Materialize / remove the skill | `internal/init/init.go` `generateGuidedSkill` | guided on (and not authoring) → reset `.claude/skills/guided` then write the WHOLE embedded subtree (router + `phases/*.md`) via `content.ReadGuidedSkillTree`; guided off → `RemoveAll(.claude/skills/guided)`; authoring → no-op. The dir is reset before every write so a re-init after an upgrade drops phase files removed from the embed (no orphans). |
| Write AGENTS.md at init | `internal/init/init.go` `generateAgentContext` | passes `content.GuidedEnabled(baseDir)` into `BuildAgentsMD`. |
| Survive `zcp serve` | `internal/server/server.go` + `internal/content/refresh_agents.go` | the startup refresh passes `content.GuidedEnabled(projectRoot)` into `RefreshAgentContext(..., guided bool)`, so the guided block is preserved across the AGENTS.md re-render instead of being wiped. |

The guided **behavior** (what the agent is told to do) is owned by the content surfaces, not this Go
plumbing: `internal/content/templates/agents_guided.md` (the always-on block — trigger + lifecycle
pointer + `.zcp/guided/` recovery + the two paths + the tripwire) and the guided skill subtree under
`internal/content/templates/skills/guided/` (`SKILL.md` the router + `phases/*.md` the per-phase
disciplines). The plumbing only decides *whether* those render; the surfaces decide *what* they say. The
subtree is embedded recursively (`//go:embed all:templates`) and materialized whole by init (§3).

---

## 4. Mutual exclusion with authoring (G2 — hard)

An **authoring-context** `AGENTS.md` must **never** contain the guided block, and `zcp init --guided`
under `ZCP_AUTHORING=1` must materialize nothing guided. Authoring is the maintainer domain; guided is
user-only; they are mutually exclusive. Enforced in three places, all gating on `!rt.Authoring`:

1. the block append (`build_agents.go`),
2. the skill-gen step (`generateGuidedSkill` is a no-op under authoring),
3. the marker write (`main.go` ands `!rt.Authoring` into the flag).

Authoring code/domain, depguard rules, and `TestAuthoringBoundary_*` are not modified by guided.
Pinned by `TestBuildAgentsMD_AuthoringExcludesGuided` (authoring=true + guided=true → authoring block
present, guided block absent) and `TestRun_GuidedSkill_NotWrittenUnderAuthoring`.

---

## 5. Toggle + serve-survival (G3)

- **Toggle:** `init --guided` writes marker + block + skill; plain `init` clears the marker, removes the
  guided block from `AGENTS.md`, and removes the skill. `.mcp.json` is byte-identical to the template in
  both cases (guided never touches it).
- **Serve-survival:** `zcp serve` re-renders the managed `AGENTS.md` section on startup. Because the
  refresh reads the marker (§3), a `--guided` install keeps its block across serve; a plain install stays
  out. No env var or config persistence is needed.

Pinned by `TestRefreshAgentContext_GuidedParam` (block follows the `guided` param) and the
`TestRun_GuidedSkill*` tests (subtree write / toggle-off-removes-subtree / authoring-suppressed).

Note: `zcp serve` refreshes only the AGENTS.md/CLAUDE.md managed sections, **not** the skill subtree.
The subtree is materialized at `zcp init` time; the refresh path for the skill content is re-running
`zcp init --guided` after a binary upgrade. The block→skill link stays intact across an upgrade (the
block points at the skill by path; the subtree is internally consistent because init writes it as a set).

---

## 6. The lifecycle (content-only, host-orchestrated) — G4

The expanded guided content drives a full app-development lifecycle. The **host** (the coding agent)
reads the content and orchestrates the flow using its OWN subagent/Task tool plus the EXISTING
`zerops_*` tools, the EXISTING work-session/`action="status"`, and the UNCHANGED bootstrap → develop →
launch pipeline. ZCP adds **no** tool and **no** Go state machine for this — the only Go is the subtree
materialization (§3). The guided skill is a router; each phase's heavy rules live in a `phases/*.md`
file loaded on demand (progressive disclosure).

The phases (router `SKILL.md` → `phases/*.md`):

| # | Phase | Owner | Mechanism |
|---|---|---|---|
| 0 | Entry / recovery | host + ZCP | read `.zcp/guided/` if present → `zerops_workflow action="status"` (the compaction-recovery primitive) |
| 1 | Align | host | scan repo → CLASSIFY → infer the decision set → narrate (Path A) or grill the load-bearing residue (Path B, extended to product: wedge, slice 1, out-of-scope) |
| 2 | PRD + topology | host | write `.zcp/guided/PRD.md` (problem, users, inferred assumptions, stories, out-of-scope, testing decisions, the topology chapter = the resolved service plan) |
| 3 | Slice DAG | host | write `.zcp/guided/slices/NN-*.md`; DAG depth scales with tier; design the one-way seams |
| 4 | Bootstrap = runway | ZCP | the existing bootstrap provisions ALL PRD infra upfront (infra-first; never a product slice) |
| 5 | Build a slice | host subagent | a fresh subagent per slice; the slice markdown IS its brief; TDD red→green at the seam; returns a compact receipt |
| 6 | Review + deploy + verify | host + ZCP | read-only review subagents, then scoped deploy + `zerops_verify`; "verified" is a composite |
| 7 | Release / live URL | ZCP | dev/demo → deploy+URL; production-business → the existing launch-production flow + user-owned launch token |

### 6.1 Plain-file ledger — `.zcp/guided/`

State lives in gitignored plain markdown the host maintains (`PRD.md` + `slices/NN-*.md`). It is the
durable ledger AND the per-slice subagent brief, survives compaction (recovery re-reads it), and is
private-safe (never committed by default — a layperson PRD carries business detail). **No persisted
status field**: slice done-ness is DERIVED from `zerops_workflow action="status"`, never written into a
file (intent in files; status read live — the same discipline as `IsOpen`). A guided project with no
`.zcp/guided/` behaves exactly like a plain ZCP project until a lifecycle run opens it.

### 6.2 "Verified" is a composite (the honesty boundary)

"Verified" combines four checks with two owners: **ZCP** owns *deployed* (`zerops_deploy` success) and
*reachable/healthy* (`zerops_verify`); the **host** owns *acceptance met* (the slice's TDD tests) and
*code quality* (a read-only review subagent). The content must label it as a composite and never promise
"automated review" or "tested" as a ZCP guarantee — ZCP claims only deploy + reachability; host-reported
results surface through `zerops_record_fact`.

### 6.3 The one conscious trade-off — no code gate (§3a of the PRD)

"Slice N+1 waits until N deploys + verifies" is **skill prose, not a hard gate.** Without a Go state
machine, reliability is a content-quality + flow-eval matter. This is accepted deliberately: ZCP does
NOT pre-build a gate on speculation. If — and only if — flow-eval empirically shows the host collapses
even a short DAG, the minimal nudge is a develop-phase **atom** (the existing axis machinery), never a
new tool.

### 6.4 Content disciplines (the P1 skills-lint gate)

The guided skill subtree is held to the same content contract as the rest of the agent-facing corpus,
pinned by `TestGuidedSkillContent_*` over `content.ReadGuidedSkillTree()`:

- **Tools-only.** A `zerops://` reference appears only via the tool-call form
  `zerops_knowledge uri="zerops://..."`, never as a bare backticked URI (ZCP advertises no MCP resources
  protocol — same rule as the rest of the corpus).
- **No hardcoded platform facts.** No service `@version` token; every version/variant/profile routes to
  its live owner (`zerops_knowledge uri="zerops://decisions/choose-*"` + the active-filtered schema).
- **Router ↔ phases coherence.** Every `phases/*.md` the content points at exists in the subtree (no
  dangling pointer) and every phase file is referenced (no orphan).

---

## Invariants (pinned)

| # | Invariant | Test |
|---|---|---|
| G1 | Guided state is the local marker `.zcp/state/guided`; not committed, not on `runtime.Info` | `TestRun_GuidedSkill_*` |
| G2 | Authoring AGENTS.md never carries the guided block; `--guided` is a no-op under authoring | `TestBuildAgentsMD_AuthoringExcludesGuided`, `TestRun_GuidedSkill_NotWrittenUnderAuthoring` |
| G3 | Block renders iff `guided && !Authoring`; toggles off on plain init; survives `zcp serve` | `TestBuildAgentsMD_GuidedGate`, `TestRefreshAgentContext_GuidedParam` |
| G4 | The guided skill is a subtree (router + `phases/*.md`) materialized WHOLE on guided-on and removed whole on toggle-off; content is tools-only, version-free, router↔phases coherent | `TestReadGuidedSkillTree_RouterAndPhases`, `TestRun_GuidedSkillMaterialized`, `TestRun_GuidedSkill_ToggleOffRemovesSubtree`, `TestGuidedSkillContent_*` |
| G5 | Content-only: no `zerops_guided` tool, no Go state machine / phase enum / `.zcp/state/guided/*.json` types; the lifecycle is content + existing `zerops_*` tools + `action="status"` + `.zcp/guided/` plain files | (anti-scope — nothing to register; absence pinned by no new tool in `annotations_test.go`) |
