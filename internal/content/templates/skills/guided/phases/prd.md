# Phase 2 — PRD + topology

Goal: write `.zcp/guided/PRD.md` — the compact, durable record of what you're building and why, and the source the slice DAG (phase 3) is cut from. It is **yours to maintain**; on a returning feature request you **extend** it (new stories, assumptions, slices, updated out-of-scope), you don't rewrite it.

Never wait for PRD approval (`SKILL.md` guardrail): write it for yourself + recovery, narrate the shape, build. Its job is to capture your *inferred* assumptions explicitly, one line each, so a wrong guess is visible and cheap to flip.

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

## Design
<surface type (product/brand, the app default) · the one or two primary flows + what matters most on the main view · one brand touch — an accent + a type choice — that makes it THIS product, not the stock kit>

## Topology chapter
<the resolved service plan — see below>
```

## The topology chapter — the resolved decision record

This is the Zerops-native half — the plan Align resolved, written down (don't re-derive it). Record, for the whole app:

- **The decision set:** CLASSIFY result · D1 statefulness · D2 audience×stakes · D3 capability bits · D5 tier · the derived D6/D7. One line each.
- **Architecture drivers:** max 3 `trigger → action → failure mode` lines. If a driver does not change services, data ownership, test seam, release gate, or next slice, delete it.
- **Architecture decisions:** max 5 active rows. Record only decisions that affect multiple slices/services; when a later request changes one, supersede the row instead of appending a duplicate.
- **Boundaries:** 1-7 workflow/actor-action components, never padded for count and not entity buckets. Record owner/consumer/access path for each DB, bucket, job, or event; slices reference these boundaries instead of re-defining them.
- **The runtime pair:** the app's **dev/stage pair** (derivation: `phases/align.md` D7).
- **The concrete service set:** every service the app needs, each with *why it exists* and *what breaks without it* (so over-provisioning is visibly absent and each service is justified). Name the capability + mode/tier; the **concrete service + version is resolved live** at provision time from the owner — do not pin a version in the PRD.
- **The negative space:** services you deliberately did NOT add and why ("no search engine — DB search covers this until content grows"; "no cache — one small app").
- **Floor commitments:** which app-code obligations this app carries (catalog: `phases/align.md` floor clamps — auth/authorization, no runtime-disk persistence, files → object-storage).

This chapter IS the bootstrap input. The first slice provisions all of it upfront (infra-first; see `phases/slices.md`).

## The design chapter

Record (a few lines; vetoable; never gated): the **surface type** (product/brand default), the **primary flow + its IA intent** (what matters most on the main view), and **one brand touch** (an accent + a type pairing) that makes it this product, not the stock kit. Don't pin a library or version — the kit follows the stack (`SKILL.md` owns the kit-vs-judgment split).

## The test-seam decision

Name *where* acceptance tests sit (the red→green contract each slice inherits) in the PRD's Testing decisions. Depth scales by tier and is develop's job (`phases/develop.md`).

When this phase is done: `.zcp/guided/PRD.md` exists, you've narrated the shape to the user in plain words (without asking for approval), and you carry the topology chapter into the slice DAG.
