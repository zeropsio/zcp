---
name: guided
description: How this project builds and evolves an app for someone who can't review code — on the first build AND every later change, feature, fix, or restyle. Use whenever guided mode is on, however technical or specific the request sounds (e.g. "track my workouts", "make the tickets more kanban-style", "add login"). Read the request, resolve or extend the architecture, write/update a compact PRD + thin vertical slices, build each slice on the dev runtime with tests, verify it, and narrate progress against a live URL. Resolve everything silently; never interview the user with questions they can't answer.
---

# Guided — story → architecture → working software → live URL

Someone describes a product, not a stack, and can't review code — so they never meet the engineering, only the product. Everything in this skill is your private workshop: you reason in it, you never speak it. To the user you give only what you're building, in their words, and a live URL to react to. Read the request, resolve the architecture, slice the work, build each slice with tests on the dev runtime, verify it, and hand back working software — a live URL, not a spec. Every build AND every later change walks this lifecycle, scaled to the ask. Drive it with the `zerops_*` tools, your own subagent/Task tool, and a plain-file ledger in `.zcp/guided/`.

**The `zerops_*` tools are already loaded — call them directly.** Claude Code exposes each as `mcp__zerops__zerops_*` (so `zerops_workflow` is the tool `mcp__zerops__zerops_workflow`). The short `zerops_*` names used throughout this skill are those tools; invoke them straight away by their `mcp__zerops__` name — don't `ToolSearch` or look them up first, they are pre-loaded, not deferred.

---

## The lifecycle

A product request walks these phases in order. Detail lives in `phases/*.md` — open the phase file when you reach it (don't preload them).

| # | Phase | Read | What happens |
|---|---|---|---|
| 0 | **Entry / recovery** | this file | new build or returning? resume from `.zcp/guided/` + `zerops_workflow action="status"` |
| 1 | **Align** | `phases/align.md` | scan the repo, classify, resolve the decision set, anchor on the best-fit curated recipe, narrate (Path A) or grill the residue (Path B) |
| 2 | **PRD + topology** | `phases/prd.md` | write/extend `.zcp/guided/PRD.md` — problem, users, assumptions, stories, out-of-scope, drivers, decisions, boundaries, topology |
| 3 | **Slices + runway** | `phases/slices.md` | write `.zcp/guided/slices/NN-*.md` with blocked-by + parallel-safety notes, audit safe siblings, then bootstrap from the anchored recipe (`route=recipe`: its import = the runway, `buildFromGit` = the seeded skeleton) and add the delta |
| 4 | **Build a slice** | `phases/develop.md` | fresh subagents build serial slices, or safe sibling slices in parallel after the audit; slice markdown is the brief; TDD red→green |
| 5 | **Review + verify** | `phases/review-deploy.md` | review subagents + verify on the dev URL; promote to stage at a checkpoint, not per slice |
| 6 | **Release** | `phases/release.md` | dev/demo → the live URL; production-business → the launch-production flow; then back for the next feature |

Default to one slice at a time — finish, review, verify before advancing; dispatch **safe sibling slices** in parallel only where `phases/slices.md` marked them safe. After a feature ships, a new request re-enters at Align — extend the PRD, add slices on the existing infra, scaled to the ask.

---

## Phase 0 — entry & recovery

On the first product request, after any compaction, and whenever the user comes back for more:

1. **Read the ledger if it exists.** `ls .zcp/guided/`; if `PRD.md` / `slices/` are present, this is an existing build — read them to recover intent and pick up where you left off. A returning request extends this, it doesn't start fresh.
2. **Read live status.** `zerops_workflow action="status"` tells you what services exist, what the work session is scoped to, and where the last slice landed. Slice done-ness is read from status, never stored in the ledger.
3. **Architecture delta for returning work.** For a new request on an existing app, check only: D1/D2/D3/D5 changed? new driver/risk? new service needed? boundary affected? If no, add one thin slice; if yes, update the PRD decision row and slice from there.
4. **Route to the right phase.** No ledger → Align (phase 1). Ledger present, infra not yet provisioned → Slices + runway (phase 3). Infra up, slices in flight → resume the first slice status shows un-verified. Ledger present and the app is live, new request → back to Align (phase 1) to fold the new feature into the PRD, then add slices.

---

## Resolve the architecture from the decision set

Architecture is a function of a small typed decision set, resolved during Align — not a free choice. If you find yourself "picking" a split or a robustness grade, an input is under-specified; resolve the input. The resolved set then selects the best-fit curated recipe to start from (smallest-covering, then add the delta) — the recipe is the starting material, the decision set still drives the architecture. The full inference rules + the story→services matrix live in `phases/align.md`; resolve against this set:

| Decision | Options | Reversible? |
|---|---|---|
| **0 CLASSIFY** (gate) | buildable · platform-reject · tripwire · needs-scoping · adopt-path · insufficient-signal | one-way |
| **D1 Statefulness** (floor) | stateless · single-writer · multi-writer | one-way |
| **D2 Audience × stakes** (floor) | single-actor · multi-actor-private · public/multi-tenant | one-way |
| **D3 Capability bits** | search · async · files · real-time · integrations | per-bit two-way (**files = floor**) |
| **D5 Tier** | experiment · real-but-lean · production-business | two-way |
| **D6 Decomposition** (derived) | collapsed · front/back split · capability-dedicated | two-way |
| **D7 Grade** (derived) | hobby · staging · production | two-way (**mode is immutable after create**) |

Every guided app runs as a **dev/stage pair** — tier sets scale + managed mode (`:single`/`:ha`), not whether the pair exists. `phases/align.md` owns the derivation (D7), the floor clamps (non-stateless data → managed surface, never ephemeral runtime disk), and the story→services matrix; this table is the at-a-glance index Phase 0 reads.

---

## The ledger — `.zcp/guided/`

```
.zcp/guided/
  PRD.md                    # problem, users, assumptions, stories, out-of-scope,
                            #   architecture drivers, decisions, boundaries, testing, topology
  slices/
    01-walking-skeleton.md  # what to build end-to-end, acceptance, blocked-by, parallel safety, services, design seam
    02-auth-and-roles.md
```

- Gitignored (`.zcp/` already is), project-lifetime, private — a layperson's PRD carries business detail, so it is not committed by default.
- Read/write these like any file. They are the durable ledger and the subagent brief — a slice file is handed verbatim to the subagent that builds it.
- They survive compaction — phase 0 re-reads them.
- **No status field.** Never write "done"/"deployed" into a slice file; done-ness is read from `zerops_workflow action="status"`.

---

## What "verified" means

"Verified" is a **composite** — name the parts, never collapse them to a bare "verified": acceptance tests went red→green (yours), review is clean (yours), and `zerops_verify` reports reachable/healthy (ZCP). Tests and review are host-reported — your own work, never a platform fact; `zerops_verify` proves reachability only. The per-phase table (with the looks-&-feels-right part) lives in `phases/review-deploy.md`.

---

## Global guardrails (every phase)

- **Infer, don't interview; react to software, never a doc.** A casual request means the user can't specify the architecture, not that they want it casual. Resolve silently, then present the plan in plain words and **confirm direction once** — wait for a go or a change before you build (it's the one thing they can judge before any code exists). After that one check, never gate on the PRD doc or per-slice sign-off — they react to working software. Ask only the load-bearing residue: reversible + low-harm → infer; one-way / costly / public / regulated / destructive / someone-else's-data → ask one question, first, and wait. (Path A/B mechanics: `phases/align.md`.)
- **Keep architecture memory tiny.** Drivers ≤ 3, decision rows ≤ 5, boundary names 1-7, never padded. If a note doesn't change a service, data owner, seam, release gate, or next slice, cut it.
- **Tools-only.** Provision / deploy / verify through the `zerops_*` tools and the bootstrap → develop → launch pipeline. Never `zcli`, never a raw platform API. If a tool seems unable to do something, surface the gap — don't bypass it.
- **Never hardcode a platform fact.** Service type, version, variant, region, profile come from their live owner (`zerops_knowledge uri="zerops://decisions/choose-*"` + the active-filtered schema). The matrix in `phases/align.md` names which capability at which tier; the owner resolves the concrete service.
- **Make it look and feel right.** Lean on the resolved stack's idiomatic UI kit for the accessibility + consistency floor; spend judgment on what a kit can't give — information architecture, real loading/empty/error states, copy in the user's terms (name things by what the user controls; errors say what happened + how to fix). The craft floor is yours: restraint over flourish, `ease-out` entrances under 300ms, press feedback, respect reduced-motion, don't over-animate. Default to a product UI; reserve bespoke for a true brand surface. Never ship the kit's stock default look as the finished product.
- **Parallelism is checked, not assumed** — the audit + rule live in `phases/slices.md`; `Blocked-by: none` alone never authorizes concurrent edits.
- **Build on the dev runtime; promote to stage at checkpoints** — cadence in `phases/develop.md` (build/reload) + `phases/review-deploy.md` (promote), not per slice.
- **Proportionate, not gold-plated.** Scale ceremony to the ask (DAG depth by tier: `phases/slices.md`). Spend design effort on the foundation (data model, persistence, auth, trust boundaries); stay lean on everything reversible.
