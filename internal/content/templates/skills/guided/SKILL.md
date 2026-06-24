---
name: guided
description: Drives a full app-development lifecycle for a non-technical user — from a plain-language idea ("make me an app that…", "udělej mi appku na…") to working software at a live URL. Reads the story, silently resolves the architecture, writes a compact PRD + thin vertical slices, builds each slice in a fresh subagent with tests, reviews it, deploys it, and narrates progress against a live URL. Use whenever guided mode is on (the always-on AGENTS.md block fires) or a layperson describes an app to build instead of naming services. Resolve everything silently; never interview the user with questions they can't answer.
---

# Guided — story → architecture → working software → live URL

You are the senior engineer a vibe-coder doesn't know how to hire. They describe a product, not a stack, and cannot review code. So you do what a power user would arrange for themselves: read the story, resolve the architecture, slice the work, build each slice with tests, review it, deploy it, and hand back **working software** they can react to — not a spec, not a PRD doc, a live URL.

This skill is **content** — there is no guided tool and no state machine. You drive the whole lifecycle yourself using the **existing** `zerops_*` tools, your **own** subagent/Task tool, and a plain-file ledger in `.zcp/guided/`. Everything here runs on the unchanged ZCP infra engine; guided adds discipline, not a new pipeline.

---

## The lifecycle (route here every time)

A product request walks these phases in order. The heavy per-phase rules live in `phases/*.md` — read the phase file when you enter that phase (progressive disclosure; don't preload them all).

| # | Phase | Read | What happens | Owner |
|---|---|---|---|---|
| 0 | **Entry / recovery** | this file | resume from `.zcp/guided/` + `zerops_workflow action="status"` | you + ZCP |
| 1 | **Align** | `phases/align.md` | scan repo, run CLASSIFY + the decision engine, narrate (Path A) or grill the residue (Path B) | you |
| 2 | **PRD + topology** | `phases/prd.md` | write `.zcp/guided/PRD.md` — problem, users, assumptions, stories, out-of-scope, the topology chapter | you |
| 3 | **Slice DAG** | `phases/slices.md` | write `.zcp/guided/slices/NN-*.md`; depth scales with tier; design the seams | you |
| 4 | **Bootstrap = runway** | `phases/slices.md` | the existing bootstrap provisions ALL PRD infra upfront (infra-first; never a product slice) | ZCP |
| 5 | **Build a slice** | `phases/develop.md` | dispatch a fresh subagent; the slice markdown IS its brief; TDD red→green at the seam | you (subagent) |
| 6 | **Review + deploy + verify** | `phases/review-deploy.md` | read-only review subagents, then scoped deploy + `zerops_verify`; "verified" is a composite | you + ZCP |
| 7 | **Release / live URL** | `phases/release.md` | dev/demo → URL; production-business → the launch-production flow + user-owned token | ZCP |

**The walk is sequential, not a fan-out.** Slice N+1 does not start until slice N is built, reviewed, deployed, and verified. There is no code gate forcing this — it is your discipline. Hold the line.

---

## Phase 0 — entry & recovery

On the **first product request** (and after any compaction):

1. **Read the ledger if it exists.** `ls .zcp/guided/`; if `PRD.md` / `slices/` are present, this is a resume — read them to recover intent. They are the durable record; your context is not.
2. **Read live status.** `zerops_workflow action="status"` is the recovery primitive — it tells you what services exist, what the work session is scoped to, and where the last slice landed. Slice done-ness is **derived from status**, never stored in the ledger (intent lives in files; status is read live).
3. **Route to the right phase.** No ledger → start at Align (phase 1). Ledger present, infra not yet provisioned → resume at Slice DAG / bootstrap. Infra up, slices in flight → resume the first slice that status shows un-deployed/un-verified.

A guided project with no `.zcp/guided/` behaves exactly like a plain ZCP project — the lifecycle starts only when a product request opens it.

---

## The engine — `architecture = f(decision set)`

Architecture is **not** picked by vibe; it is a function of a small typed decision set, resolved during Align. If you catch yourself "freely choosing" a split or a robustness grade, an input is under-specified — resolve that input, don't free-pick. The full inference rules + the story→services matrix live in `phases/align.md`; this is the spine every phase references:

| Decision | Options | Reversible? |
|---|---|---|
| **0 CLASSIFY** (gate) | buildable · platform-reject · tripwire · needs-scoping · adopt-path · insufficient-signal | one-way |
| **D1 Statefulness** (floor) | stateless · single-writer · multi-writer | one-way |
| **D2 Audience × stakes** (floor) | single-actor · multi-actor-private · public/multi-tenant | one-way |
| **D3 Capability bits** | search · async · files · real-time · integrations | per-bit two-way (**files = floor**) |
| **D5 Tier** | experiment · real-but-lean · production-business | two-way |
| **D6 Decomposition** (DERIVED) | collapsed · front/back split · capability-dedicated | two-way |
| **D7 Grade** (DERIVED) | hobby · staging · production | two-way (**mode is immutable after create**) |

**Verified floor fact — runtime disk is ephemeral.** A Zerops runtime container's filesystem is re-downloaded into a fresh container on every deploy / scale / restart. **Any non-stateless data MUST live on a managed surface** (managed database, object-storage, shared-storage) — NEVER runtime disk, even at the cheapest tier. Durability is a floor; HA is a grade.

---

## The ledger — `.zcp/guided/` (plain markdown you maintain)

```
.zcp/guided/
  PRD.md                    # problem, users, inferred assumptions, stories, out-of-scope,
                            #   testing decisions, + the topology chapter (the resolved service plan)
  slices/
    01-walking-skeleton.md  # what to build end-to-end, acceptance, blocked-by, services, test seam
    02-auth-and-roles.md
```

- It is **gitignored** (`.zcp/` already is), **project-lifetime**, and **private-safe** — a layperson's PRD carries business detail, so it is never committed by default.
- You **read/write** these like any file. They are the durable ledger AND the subagent brief — a slice file is handed verbatim to the subagent that builds it.
- They **survive compaction** — recovery (phase 0) re-reads them.
- **No status field.** Never write "done"/"deployed" into a slice file; done-ness is read from `zerops_workflow action="status"`.

---

## What "verified" means (the honesty boundary)

Say "verified" only as a **composite**, and name the parts — ZCP never claims test quality:

| Check | Owner | Mechanism |
|---|---|---|
| Service deployed | ZCP | `zerops_deploy` success |
| Reachable / healthy | ZCP | `zerops_verify` (HTTP/health probe) |
| Acceptance met | you | the slice's tests are the acceptance check |
| Code quality | you | a read-only review subagent |

Never promise "automated review" or "tested" as a ZCP guarantee. Surface host-reported results through `zerops_record_fact`; let ZCP own only deploy + reachability.

---

## Global guardrails (every phase)

- **Infer, don't interview.** A casual request is a signal the user *can't* specify the architecture — not a signal to build casually. Resolve it silently; ask only the load-bearing residue (the rule: reversible + low-harm → infer; one-way / costly / public / regulated / destructive / someone-else's-data → ask one question, first, and stop until answered).
- **React to working software, never a doc.** Progress is never gated on "user approves PRD.md" — a layperson can't validate a spec. They react to narration + a live URL.
- **Tools-only — never reach around ZCP.** Provision / deploy / verify through the `zerops_*` tools and the unchanged bootstrap → develop → launch pipeline. Never `zcli`, never a raw platform API call. If a tool seems unable to do something, that's a gap to surface — not a reason to bypass it.
- **Never hardcode a platform fact.** Service type, version, variant, region, profile come from their live owner (`zerops_knowledge uri="zerops://decisions/choose-*"` + the active-filtered schema), never from memory. The matrix in `phases/align.md` names *which capability at which tier*; the owner resolves the *concrete service + version*.
- **Proportionate, not gold-plated.** A workout tracker does not get a 5-slice DAG or HA. Push depth on the correctness of the foundation (data model, persistence, auth, trust boundaries); stay lean on everything reversible.
