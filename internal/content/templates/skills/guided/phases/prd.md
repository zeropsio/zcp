# Phase 2 — PRD + topology

Goal: write `.zcp/guided/PRD.md` — the compact, durable record of what you're building and why. It is the ledger that survives compaction and the source the slice DAG (phase 3) is cut from. It is **yours to maintain**, not a gate the user approves. On a returning feature request, you **extend** this file (new stories, new assumptions, new slices, updated out-of-scope) — you don't rewrite it.

## The rule: never gate progress on PRD approval

A layperson cannot validate a PRD doc. So **do not** stop and wait for "user approves PRD.md". You write it for yourself (and for recovery), narrate the shape in plain words, and move on to build. The user corrects by reacting to working software, not by reading this file. The PRD captures your *inferred* assumptions explicitly so a wrong one is visible and cheap to flip later — that is its job, not sign-off.

## Write `.zcp/guided/PRD.md` with these sections

Keep it compact (one screen of substance, not an essay). Use the resolved decision set from Align.

```markdown
# PRD — <app name>

## Problem
<one paragraph: the real job this does, in the user's own framing>

## Users
<who acts: single-actor / a private team / public — and the roles, if any>

## Inferred assumptions (vetoable)
<each load-bearing thing you INFERRED rather than were told, one line each, so a
 wrong guess is visible — e.g. "assumed single-tenant; flip is an additive owner_id migration">

## Architecture drivers
<max 3 rows: trigger → action → failure mode; only things that change services, data owners, test seams, release gates, or the next slice>

## Architecture decisions
| Trigger | Decision | Rejected | Consequence | Check |
|---|---|---|---|---|
| <why this matters> | <what we chose> | <real alternative> | <trade-off accepted> | <test/review/verify proof> |

## Boundaries
| Component | Responsibility | Owns | Physical home |
|---|---|---|---|
| <workflow/actor-action name> | <what it does> | <data/fact owner> | <app module / service> |

## User stories
<the handful of "as a <role> I can <do X>" that define the product; mark the core wedge>

## Out of scope (for now)
<what you are deliberately NOT building yet — the visible roadmap, never a silent drop>

## Testing decisions
<the seam the acceptance tests sit at per slice — see the test-seam rule below>

## Topology chapter
<the resolved service plan — see below>
```

## The topology chapter — the resolved decision record

This is the Zerops-native half — the plan Align resolved, written down (don't re-derive it). Record, for the whole app:

- **The decision set:** CLASSIFY result · D1 statefulness · D2 audience×stakes · D3 capability bits · D5 tier · the derived D6/D7. One line each.
- **Architecture drivers:** max 3 `trigger → action → failure mode` lines. If a driver does not change services, data ownership, test seam, release gate, or next slice, delete it.
- **Architecture decisions:** max 5 active rows. Record only decisions that affect multiple slices/services; when a later request changes one, supersede the row instead of appending a duplicate.
- **Boundaries:** 1-7 workflow/actor-action components, never padded for count and not entity buckets. Record owner/consumer/access path for each DB, bucket, job, or event; slices reference these boundaries instead of re-defining them.
- **The runtime pair:** the app runtime is a **dev/stage pair** — build on the dev runtime, promote to the stage runtime. Record it as a pair; tier sets the scale + managed mode, not whether the pair exists.
- **The concrete service set:** every service the app needs, each with *why it exists* and *what breaks without it* (so over-provisioning is visibly absent and each service is justified). Name the capability + mode/tier; the **concrete service + version is resolved live** at provision time from the owner — do not pin a version in the PRD.
- **The negative space:** services you deliberately did NOT add and why ("no search engine — DB search covers this until content grows"; "no cache — one small app").
- **Floor commitments:** the app-code obligations no Zerops knob guarantees — auth + server-side authorization for multi-user, no runtime-disk persistence, files → object-storage.

Events/jobs are not second sources of truth: default to an ID plus the minimum context needed for a consumer to decide what to do. Copy before/after data only when the consumer cannot safely re-read the owner, and record that consistency trade-off in the decision row.

This chapter IS the bootstrap input. The first slice provisions all of it upfront (infra-first; see `phases/slices.md`).

## The test-seam decision (per the tier)

Decide now *where* acceptance tests sit, because it shapes how each slice is built (phase 4 TDD):

- **experiment** — a thin integration smoke test per slice (the endpoint/flow responds correctly). Don't over-test a throwaway.
- **real-but-lean** (default) — integration tests at the API/use-case seam for each slice's acceptance criteria; unit tests where logic is non-trivial.
- **production-business** — integration + unit at the seam, plus the floor invariants (authorization denies cross-tenant reads; no data on runtime disk).

The acceptance test for a slice is the contract the build subagent must satisfy (red→green). Name the seam in the PRD's Testing decisions so every slice file inherits it.

When this phase is done: `.zcp/guided/PRD.md` exists, you've narrated the shape to the user in plain words (without asking for approval), and you carry the topology chapter into the slice DAG.
