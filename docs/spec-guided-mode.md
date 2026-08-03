# Spec: Guided mode (`zcp init --guided`)

Guided mode is a **user-only** opt-in that makes the coding agent drive a genuinely good
application-development **lifecycle** for a **non-technical user** — someone who describes a product
in plain words ("make me a project-management app for our real-estate firm") and cannot articulate an
architecture or review code. With guided off, a casual request yields the model's match-down default
(a static toy, data on disk). With guided on, the agent is steered to land on a real, right-sized
Zerops-native service set — *which services exist and how robustly they run* — AND to build the app
*well*: a compact PRD, thin vertical slices built on a living dev runtime with per-slice TDD in fresh
subagents, review, and verification (promoting to a stage runtime at checkpoints), all without
interrogating the user. The user reacts to **working software at a live URL**, never to a spec. The
lifecycle is a **repeatable loop** — a returning feature request re-enters it, it does not dead-end at
the first release.

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
  `AGENTS.md`, which directs it to apply the guided skill (`.agents/skills/guided/SKILL.md`, a lifecycle
  *router* that points at per-phase files in `.agents/skills/guided/phases/`) before planning or
  building. Every guided session walks the lifecycle (§6) to working software at a live URL.
- **Scope of agents:** guided is **agent-neutral**. The subtree is materialized into EVERY
  agent-discovery root (`.agents/skills/guided/` and `.claude/skills/guided/` —
  `content.GuidedSkillDirsRel`), so an agent that discovers `.agents/skills/` finds exactly what
  Claude Code finds natively under `.claude/skills/`. Nothing about enabling guided requires a
  particular agent to be installed, authorized, or running.
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
| Materialize / remove the skill | `internal/init/init.go` `generateGuidedSkill` | guided on (and not authoring) → reset EVERY dir in `content.GuidedSkillDirsRel` then write the WHOLE embedded subtree (router + `phases/*.md`) via `content.ReadGuidedSkillTree` into each; guided off → `RemoveAll` on each; authoring → no-op. Each dir is reset before every write so a re-init after an upgrade drops phase files removed from the embed (no orphans). |
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
| 0 | Entry / recovery | host + ZCP | read `.zcp/guided/` if present → `zerops_workflow action="status"`; routes new-build vs returning-feature vs compaction-resume |
| 1 | Align | host | scan repo → CLASSIFY → infer the decision set → anchor on the best-fit curated recipe for the resolved stack → narrate (Path A) or grill the load-bearing residue (Path B: wedge, slice 1, out-of-scope) |
| 2 | PRD + topology | host | write/extend `.zcp/guided/PRD.md` (problem, users, inferred assumptions, stories, out-of-scope, testing decisions, compact architecture drivers/decisions/boundaries, the topology chapter = the resolved service plan, incl. the dev/stage pair) |
| 3 | Slice DAG + bootstrap = runway | host + ZCP | write `.zcp/guided/slices/NN-*.md` (DAG depth scales with tier; design the one-way seams; each slice names one Preserve invariant, one Design seam, and parallel-safety status), run a read-only parallelization audit for safe siblings, then bootstrap provisions ALL PRD infra upfront via `route=recipe` from the anchored recipe (its import = the runway, `buildFromGit` = the seeded skeleton; `route=classic` only when no framework-matching recipe fit) — the dev/stage pair + managed services (infra-first; never a product slice) |
| 4 | Build a slice | host subagent | a fresh subagent per slice builds on the living dev runtime (edit + test + reload, no per-slice deploy); audited safe sibling slices may build in parallel, otherwise serial; the slice markdown IS its brief; TDD red→green; returns a compact receipt |
| 5 | Review + verify | host + ZCP | read-only review subagents + `zerops_verify` on the dev URL; promote to stage (a formal deploy) at a checkpoint, not per slice; "verified" is a composite |
| 6 | Release / live URL | ZCP | dev/demo → the dev (or promoted stage) URL; production-business → the launch-production flow + user-owned launch token; then the loop reopens for the next feature |

Two properties shape the lifecycle:

- **Every guided app runs as a dev/stage pair.** Slices are built on the living dev runtime and promoted to stage at checkpoints, so a formal deploy is a milestone act, not a per-slice step. Tier sets scale + managed-dependency mode (`:single`/`:ha`), not whether the pair exists. The platform already supports this (bootstrap standard mode, pair-keyed runtime meta, cross-deploy) — guided just always chooses it.
- **The lifecycle is a repeatable loop, not a one-shot.** Bootstrap (infra) is one-time, but a returning feature re-enters at Align, extends the PRD, and adds slices on the existing pair — the same develop machinery re-entered. Only a genuinely new capability needing a new service triggers a bootstrap side-trip, then back to building.
- **Parallelism is a checked optimization, not the slice model.** The walking skeleton is serial. Later sibling slices should be shaped for natural independence when that preserves vertical slicing, then a read-only audit marks `serial` vs `candidate with <slice>`. `Blocked-by: none` is necessary but not sufficient; shared migrations, auth/session, routing, layout, schema, runtime state, or Design seams serialize the work.

### 6.1 Plain-file ledger — `.zcp/guided/`

State lives in gitignored plain markdown the host maintains (`PRD.md` + `slices/NN-*.md`). It is the
durable ledger AND the per-slice subagent brief, survives compaction (recovery re-reads it), and is
private-safe (never committed by default — a layperson PRD carries business detail). **No persisted
status field**: slice done-ness is DERIVED from `zerops_workflow action="status"`, never written into a
file (intent in files; status read live — the same discipline as `IsOpen`). A guided project with no
`.zcp/guided/` behaves exactly like a plain ZCP project until a lifecycle run opens it.

The ledger carries only compact architecture memory: max 3 drivers, max 5 active decision rows, 1-7
boundaries (never padded for count), and one Preserve/Design-seam/parallel-safety note per slice. This is content discipline, not governance
machinery: it keeps later slices from re-deciding the same topology or leaking facts across modules,
while avoiding a full ADR/process layer.

### 6.2 "Verified" is a composite (the honesty boundary)

Per-slice "verified" combines three checks: the **host** owns *acceptance met* (the slice's TDD tests)
and *code quality* (a read-only review subagent); **ZCP** owns *reachable/healthy* (`zerops_verify` on
the live dev URL — the slice is already running on the dev runtime, so no deploy is needed to verify it).
The formal deploy + `zerops_verify` on **stage** is the production-shaped checkpoint proof, taken at a
milestone, not per slice. The content must label "verified" as a composite and never promise "automated
review" or "tested" as a ZCP guarantee — ZCP claims only reachability (+ deploy at the stage checkpoint);
host-reported results (acceptance tests, review) are narrated to the user as the host's own work, never
through `zerops_record_fact` (a bootstrap/recipe-only tool — unavailable in the stateless develop session
guided mode runs on). A fourth host-owned angle — *looks-right* — renders the dev URL and critiques it
against the design chapter + the quality/craft floor (kit applied, real loading/empty/error states,
responsive, keyboard focus, reduced-motion, restrained motion, copy in the user's terms; for a `brand`
slice, not interchangeable with the stock kit). It is host-reported like acceptance and review;
`zerops_verify` still claims reachability only (§6.5).

### 6.3 The one conscious trade-off — no code gate (§3a of the PRD)

"Slice N+1 waits until N is built + verified on the dev runtime, unless it is an audited safe sibling" is **skill prose, not a hard gate.** Without a Go state machine, reliability is a content-quality + flow-eval matter. This is accepted deliberately: ZCP does NOT pre-build a scheduler on speculation. If — and only if — flow-eval empirically shows the host collapses even a short DAG, the minimal nudge is a develop-phase **atom** (the existing axis machinery), never a new tool.

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

### 6.5 The design dimension is triggered, not taught (G6)

Guided drives a genuinely good *look + UX*, not just a good service set — but as a **dimension threaded
through the existing phases**, never a new phase file, never a tool, never a `choose-ui-kit` knowledge
owner. The guided content only **triggers the model's own design knowledge** (which idiomatic,
well-adopted UI kit fits the resolved stack; what makes an interface feel premium — `ease-out`, press
feedback, restraint, reduced-motion). ZCP carries **no kit table, no craft values, no per-stack
enumeration**: that knowledge is the model's, not a Zerops fact, and pinning it in content would only rot.
The existing no-`@version` lint (§6.4) keeps it that way for free.

What ZCP **owns** is the *structure + discipline* the model would not self-impose:

- **`surface-type` is a per-slice attribute with an app-level default** (`product` — dashboards/CRUD, the
  majority and the default; `brand` — a public landing/marketing page). Resolved in Align like D6/D7
  (derived, not asked), recorded on each slice's `## Design seam`, overridable per route. A `product`
  slice leans on the kit; a `brand` slice goes bespoke (the distinctiveness doctrine applies only here).
  Per-slice scope is what lets a bespoke landing slice sit beside a kit-default admin without the brand
  bleeding onto the CRUD.
- **UX-states are acceptance, not polish** — every data view a slice adds ships loading + empty + error
  states as part of red→green (`## Acceptance`), the seam a thin-brief build subagent would otherwise skip.
- **The looks-right review angle** (§6.2) is the verification seam: a screenshot critique, host-reported.
- **Anti-sameness is structural** — the brand touch (one accent + one type pairing) is a slice-1
  acceptance item, and "interchangeable with the stock kit" is an explicit looks-right failure; the kit
  gives the floor, the agent's judgment (IA, density, flow, copy) gives the product.

The Zerops-identity theme `zerops_knowledge uri="zerops://themes/design-system"` is **ZCP's own brand**
(recipe showcases) and is **never** applied to a user's guided app.

### 6.6 The seam is the test surface — why there is no test-author/implementer split (G7)

A guided slice's seam is **one interface**, not two. The boundary the build subagent works behind *is*
the surface the acceptance test crosses — a deep module tested through its own interface (the
codebase-design model). The slice carries a single `## Design seam` field naming that interface; there
is no separate `## Test seam`. Three consequences are load-bearing:

- **Test through the interface, never past it.** The acceptance test exercises user-observable behavior
  at the seam, not the functions behind it; a test that must reach past the interface signals a
  wrong-shaped module — fix the shape, don't weaken the test. Tautological and rubber-stamp tests are
  dissolved by the module's shape, not patched by a guard rule.
- **Floor invariants live in the interface, not in an assertion.** Tenant isolation / default-deny /
  no-runtime-disk are owned by the model (an owner-scoped, default-deny repository), so the test
  *demonstrates* them while the design *enforces* them — the TELL and the CHECK are the same interface.
- **No test-author/implementer two-agent split.** Splitting phase 4 into a blind test-author plus an
  implementer was considered and rejected: it patches a module-shape problem with a process split, and
  the contract is already an independent design artifact (the seam, fixed in phase 3 before the build
  subagent exists). One build subagent testing through the seam — red→green with authorial memory of
  why red is red — is correct; a handoff would only add execution coupling (author before implementer on
  one live working tree) and destroy the fail-for-the-right-reason signal. If flow-eval later shows the
  host rubber-stamps acceptance, the minimal nudge is a develop-phase atom (§6.3), never an agent split.

### 6.7 Recipe-first — the curated recipe is the primary substrate (G8)

The resolved decision set is a stack spec, and the **primary** way guided lands a service set is to
anchor on the curated recipe whose **app framework matches the resolved framework** and provision it
via the existing `route=recipe` path — its `import.yml` is already the dev/stage pair + managed deps,
and its `buildFromGit` seeds the runtimes with a runnable skeleton carrying a committed `zerops.yaml`
and pre-solved gotchas. The story→services matrix is demoted from blueprint to **fit-judge +
delta-resolver**. Load-bearing boundaries:

- **The decision set still governs.** The recipe is selected BY and adjusted TO the resolved floors,
  never the reverse. Framework match is a hard precondition (dependency-fit is framework-blind, so a
  wrong-framework recipe can score "exact"); a recipe below the resolved D7 grade floor is disqualified
  for in-place use (managed mode is immutable, so a `:single` recipe never satisfies a
  production-business `:ha` floor — that lands at launch-production); floor-required services the recipe
  lacks (durable-data → DB, files → object-storage) are mandatory adds.
- **Topology = recipe + delta.** Add a missing service via the post-provision "Add more services"
  step; **remove only by selecting a smaller recipe** — there is no in-place strip (a recipe's managed
  deps back its app's `${host_*}` wiring; dev-only narrowing would break the dev/stage pair). When the
  only framework-matching recipe over-provisions, or none matches, design from the matrix (`route=classic`)
  and carry the recipe's gotchas as knowledge — the documented fallback.
- **The recipe is a stack substrate, not a product.** Slice 1 adapts the cloned skeleton (strips the
  demo, keeps the wiring + `zerops.yaml` + gotchas) and re-skins its look; the showcase `design-system`
  identity (§6.5) is never shipped as the user's app.
- **No recipe slug or `@version` in guided content** (the corpus is the owner; §6.4 lint). Recipes are
  named by mechanism — "the recipe the catalog surfaces for the resolved stack".

---

## Invariants (pinned)

| # | Invariant | Test |
|---|---|---|
| G1 | Guided state is the local marker `.zcp/state/guided`; not committed, not on `runtime.Info` | `TestRun_GuidedSkill_*` |
| G2 | Authoring AGENTS.md never carries the guided block; `--guided` is a no-op under authoring | `TestBuildAgentsMD_AuthoringExcludesGuided`, `TestRun_GuidedSkill_NotWrittenUnderAuthoring` |
| G3 | Block renders iff `guided && !Authoring`; toggles off on plain init; survives `zcp serve` | `TestBuildAgentsMD_GuidedGate`, `TestRefreshAgentContext_GuidedParam` |
| G4 | The guided skill is a subtree (router + `phases/*.md`) materialized WHOLE on guided-on and removed whole on toggle-off; content is tools-only, version-free, router↔phases coherent | `TestReadGuidedSkillTree_RouterAndPhases`, `TestRun_GuidedSkillMaterialized`, `TestRun_GuidedSkill_ToggleOffRemovesSubtree`, `TestGuidedSkillContent_*` |
| G5 | Content-only: no `zerops_guided` tool, no Go state machine / phase enum / `.zcp/state/guided/*.json` types; the lifecycle is content + existing `zerops_*` tools + `action="status"` + `.zcp/guided/` plain files | (anti-scope — nothing to register; absence pinned by no new tool in `annotations_test.go`) |
| G6 | Design is a triggered dimension: surface-type per slice, UX-states as acceptance, the looks-right review angle; no kit/craft enumeration, no design tool/owner; Zerops's own `design-system` theme is never applied to user apps | `TestGuidedSkillContent_DesignDimension`, `TestGuidedSkillContent_Invariants` |
| G7 | The slice seam is one interface = build boundary + test surface (no separate `## Test seam` field); floor invariants live in the interface; the acceptance test crosses the seam (no test-author/implementer split) | `TestGuidedSkillContent_SeamIsTestSurface` |
| G8 | Recipe-first: the framework-matching curated recipe is the primary substrate, provisioned via `route=recipe` (`buildFromGit` skeleton + import = runway); the decision set still governs (framework + D7-grade + floor-service gates select/adjust the recipe); topology = recipe + add-delta, remove only by choosing a smaller recipe (no in-place strip); `route=classic` + gotchas is the fallback; no recipe slug/`@version` in content | `TestGuidedSkillContent_RecipeFirst`, `TestGuidedSkillContent_Invariants` |
