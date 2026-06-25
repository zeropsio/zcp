---
name: guided
description: How this project builds and evolves an app for someone who can't review code — on the first build AND every later change, feature, fix, or restyle. Use whenever guided mode is on, however technical or specific the request sounds (e.g. "track my workouts", "make the tickets more kanban-style", "add login"). Read the request, resolve or extend the architecture, write/update a compact PRD + thin vertical slices, build each slice on the dev runtime with tests, verify it, and narrate progress against a live URL. Resolve everything silently; never interview the user with questions they can't answer.
---

# Guided — story → architecture → working software → live URL

Someone describes a product, not a stack, and can't review code. Be the senior engineer they don't know how to hire: read the request, resolve the architecture, slice the work, build each slice with tests on the dev runtime, verify it, and hand back working software they can react to — a live URL, not a spec. This is how every build AND every later change is handled here — the first feature and the "make it more kanban-style" three weeks later both walk this lifecycle, scaled to the ask. You drive it with the `zerops_*` tools, your own subagent/Task tool, and a plain-file ledger in `.zcp/guided/`.

---

## The lifecycle

A product request walks these phases in order. Detail lives in `phases/*.md` — open the phase file when you reach it (don't preload them).

| # | Phase | Read | What happens |
|---|---|---|---|
| 0 | **Entry / recovery** | this file | new build or returning? resume from `.zcp/guided/` + `zerops_workflow action="status"` |
| 1 | **Align** | `phases/align.md` | scan the repo, classify, resolve the decision set, narrate (Path A) or grill the residue (Path B) |
| 2 | **PRD + topology** | `phases/prd.md` | write/extend `.zcp/guided/PRD.md` — problem, users, assumptions, stories, out-of-scope, drivers, decisions, boundaries, topology |
| 3 | **Slices + runway** | `phases/slices.md` | write `.zcp/guided/slices/NN-*.md` with blocked-by + parallel-safety notes, audit safe siblings, then bootstrap all infra upfront (the dev/stage pair + managed services) |
| 4 | **Build a slice** | `phases/develop.md` | fresh subagents build serial slices, or safe sibling slices in parallel after the audit; slice markdown is the brief; TDD red→green |
| 5 | **Review + verify** | `phases/review-deploy.md` | review subagents + verify on the dev URL; promote to stage at a checkpoint, not per slice |
| 6 | **Release** | `phases/release.md` | dev/demo → the live URL; production-business → the launch-production flow; then back for the next feature |

Default to one slice at a time: finish, review, and verify before advancing. After the walking skeleton, you may dispatch **safe sibling slices** in parallel only when `phases/slices.md` has marked them parallel-safe. Never split a vertical slice horizontally just to create concurrency. After a feature ships, a new request re-enters at Align (phase 1) — extend the PRD and add slices on the existing infra, scaled to the ask (a small restyle or fix is one thin slice, not a fresh PRD).

---

## Phase 0 — entry & recovery

On the first product request, after any compaction, and whenever the user comes back for more:

1. **Read the ledger if it exists.** `ls .zcp/guided/`; if `PRD.md` / `slices/` are present, this is an existing build — read them to recover intent and pick up where you left off. A returning request extends this, it doesn't start fresh.
2. **Read live status.** `zerops_workflow action="status"` tells you what services exist, what the work session is scoped to, and where the last slice landed. Slice done-ness is read from status, never stored in the ledger.
3. **Architecture delta for returning work.** For a new request on an existing app, check only: D1/D2/D3/D5 changed? new driver/risk? new service needed? boundary affected? If no, add one thin slice; if yes, update the PRD decision row and slice from there.
4. **Route to the right phase.** No ledger → Align (phase 1). Ledger present, infra not yet provisioned → Slices + runway (phase 3). Infra up, slices in flight → resume the first slice status shows un-verified. Ledger present and the app is live, new request → back to Align (phase 1) to fold the new feature into the PRD, then add slices.

---

## Resolve the architecture from the decision set

Architecture is a function of a small typed decision set, resolved during Align — not a free choice. If you find yourself "picking" a split or a robustness grade, an input is under-specified; resolve the input. The full inference rules + the story→services matrix live in `phases/align.md`; resolve against this set:

| Decision | Options | Reversible? |
|---|---|---|
| **0 CLASSIFY** (gate) | buildable · platform-reject · tripwire · needs-scoping · adopt-path · insufficient-signal | one-way |
| **D1 Statefulness** (floor) | stateless · single-writer · multi-writer | one-way |
| **D2 Audience × stakes** (floor) | single-actor · multi-actor-private · public/multi-tenant | one-way |
| **D3 Capability bits** | search · async · files · real-time · integrations | per-bit two-way (**files = floor**) |
| **D5 Tier** | experiment · real-but-lean · production-business | two-way |
| **D6 Decomposition** (derived) | collapsed · front/back split · capability-dedicated | two-way |
| **D7 Grade** (derived) | hobby · staging · production | two-way (**mode is immutable after create**) |

Every guided app runs as a **dev/stage pair** — the dev runtime to build on, the stage runtime to promote to. Tier sets the managed-dependency mode (`:single` vs `:ha`) and the scale, not whether the pair exists.

Runtime disk is ephemeral: a runtime container's filesystem is re-downloaded fresh on every deploy / scale / restart. Any non-stateless data MUST live on a managed surface (database, object-storage, shared-storage), never runtime disk — even at the cheapest tier. Durability is a floor; HA is a grade.

---

## The ledger — `.zcp/guided/`

```
.zcp/guided/
  PRD.md                    # problem, users, assumptions, stories, out-of-scope,
                            #   architecture drivers, decisions, boundaries, testing, topology
  slices/
    01-walking-skeleton.md  # what to build end-to-end, acceptance, blocked-by, parallel safety, services, test seam
    02-auth-and-roles.md
```

- Gitignored (`.zcp/` already is), project-lifetime, private — a layperson's PRD carries business detail, so it is not committed by default.
- Read/write these like any file. They are the durable ledger and the subagent brief — a slice file is handed verbatim to the subagent that builds it.
- They survive compaction — phase 0 re-reads them.
- **No status field.** Never write "done"/"deployed" into a slice file; done-ness is read from `zerops_workflow action="status"`.

---

## What "verified" means

Say "verified" as a composite, and name the parts:

| Check | Owner | Mechanism |
|---|---|---|
| Acceptance met | you | the slice's tests went red→green |
| Code quality | you | a read-only review subagent |
| Reachable / healthy | ZCP | `zerops_verify` (HTTP/health probe) |

Tests and review are yours, host-reported — narrate them as your own work, never as a platform fact. `zerops_verify` is the reachability half. Never present "tested" or "reviewed" as a platform guarantee.

---

## Global guardrails (every phase)

- **Infer, don't interview.** A casual request signals the user can't specify the architecture — not that they want it built casually. Resolve silently; ask only the load-bearing residue (reversible + low-harm → infer; one-way / costly / public / regulated / destructive / someone-else's-data → ask one question, first, and wait).
- **React to working software, never a doc.** Progress is never gated on "user approves PRD.md" — they react to narration + a live URL.
- **Keep architecture memory tiny.** Drivers max 3, decision rows max 5, boundary names 1-7 and never padded for count. If a note does not change a service, data owner, test seam, release gate, or next slice, cut it.
- **Parallelism is checked, not assumed.** Prefer naturally independent sibling slices, then run the parallel-safety audit. `Blocked-by: none` alone does not authorize concurrent edits; shared migrations, auth, routing, layout, schema, runtime state, or Design seams make the slice serial.
- **Build on the dev runtime; deploy to stage at checkpoints.** The dev runtime is a living server you edit and reload in place (drive it via `zerops_dev_server`). A formal deploy promotes to stage — do it at a milestone or release, not per slice.
- **Tools-only.** Provision / deploy / verify through the `zerops_*` tools and the bootstrap → develop → launch pipeline. Never `zcli`, never a raw platform API. If a tool seems unable to do something, surface the gap — don't bypass it.
- **Never hardcode a platform fact.** Service type, version, variant, region, profile come from their live owner (`zerops_knowledge uri="zerops://decisions/choose-*"` + the active-filtered schema). The matrix in `phases/align.md` names which capability at which tier; the owner resolves the concrete service.
- **Proportionate, not gold-plated.** A workout tracker doesn't get a 5-slice DAG or HA. Spend design effort on the foundation (data model, persistence, auth, trust boundaries); stay lean on everything reversible.
- **Make it look and feel right — trigger what you already know.** You know the idiomatic, well-adopted UI kit for the resolved stack, and what makes an interface feel premium (restraint over flourish, `ease-out` entrances, press feedback, respecting reduced-motion). Use them: lean on the kit for the accessibility + consistency floor, and spend your judgment on the part a kit can't give — information architecture, real loading/empty/error states, and copy in the user's terms (name things by what the user controls; errors say what happened + how to fix). Default to a product UI; reserve a bespoke, distinctive look for a true brand surface (a public landing/marketing page). Never hand the kit's stock default look back as the finished product, and don't over-animate.
