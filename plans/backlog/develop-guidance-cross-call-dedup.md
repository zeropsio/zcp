# Develop-guidance cross-call dedup

**Surfaced**: 2026-06-13, `nonauthoring-context-audit-2026-06-13` (F2 / §8b Ph2)
— non-authoring context-surface audit, dynamic-side measurement.

**Why deferred**: the audit shipped Phase 0 (safe cruft removal) and Batches
1-2 of Phase 1 (description trims on `zerops_deploy` setup + `zerops_workflow`
Plan examples), all proven safe via the 5-layer protocol (build+test, ratchet,
single-owner read, tree-wide consistency sweep, flow-eval on the real eval
container). F2 was explicitly named "Recommended next phase (NOT done)" and
"the biggest remaining win" in the source plan's own status line, but it is a
bigger unit of work than a description trim: it's an **explicit behavior
change** (guidance atoms stop re-appearing turn over turn), needs a design
pass before it's even a Phase-1-style trim, and — critically — must not break
compaction recovery, a load-bearing invariant (`action="status"` has to keep
re-emitting the full envelope even after dedup is wired in). The source plan
was banked at Batch 3 without executing this phase; it must not be silently
dropped when the plan archives.

## What F2 actually proposes

**The problem.** In a steady-state `develop` turn, one `status` call attaches
**16 atoms ≈ 21 KB / ~5,250 tokens** of guidance — and this is
**re-synthesized on every `status` call**, not just the first one in a
session. There is no cross-call dedup: the same atom bodies get dumped again
on turn 2, turn 3, etc., even though the agent already received and (per the
retro evidence in sibling backlog entries like
`develop-response-atom-proliferation.md`) is already flagging this as a "wall
of guidance" firehose.

**The mechanism that should fix it already exists but isn't wired up.**
`internal/ops/knowledge_tracker.go` (`KnowledgeTracker`) tracks what's already
been surfaced to a session — but it's currently wired only into bootstrap
(`internal/server/server.go`, `internal/tools/knowledge.go`), not into
`renderGuidance` (`internal/workflow/render.go` /
`internal/workflow/synthesize.go`), which is what composes the develop-phase
atom envelope on every `status` call.

**What gets deduplicated, across which calls.** The develop-phase atom
envelope, across successive `status` calls within the same session/work
session — i.e. surface each atom body once per session, not once per call.

**The explicit carve-out.** Compaction recovery must keep working: when a
session's context is compacted and the agent re-establishes state via
`action="status"`, it needs the FULL envelope again (dedup state itself may
not have survived the compaction, or the recovery path needs to be treated as
a fresh "first call" regardless of prior-turn dedup state). Getting this
carve-out wrong silently strands a recovering agent without guidance it
actually needs — this is why the source plan gated F2 as "Phase 2: design +
eval," not a mechanical wiring change.

**Why it's the biggest remaining win.** The source audit's own numbers: the
dynamic (per-call, re-dumped) cost of develop guidance (~5,250 tok/call,
repeating every `status` call across a session) **can exceed the entire
static `tools/list` schema cost** (~12,655 tok, paid once per session) over a
multi-turn session. By contrast, the safe static trims already harvested
(Batches 1-2) were small — deploy-setup ~625 B, Plan-examples ~266 B — because
the adversarial-vetting pass (§8a of the source plan) found most
broadly-exercised static descriptions are load-bearing and the remaining
static win (launch-only fields, ~500-525 tok) is compat-locked behind
`additionalProperties:false` and eval-unreachable in the clean loop. F2 has no
such ceiling: it targets a *recurring per-call* cost with an existing
mechanism (`KnowledgeTracker`) that just needs to be wired into the right
render path, and a documented target budget already exists from prior
bootstrap-restoration work (`plans/bootstrap-restore-four-goals-2026-06-02.md`):
**≤~12 KB first call, ≤~4 KB subsequent calls.**

## How to prove it (the eval angle)

The source plan's own verification protocol (§7b, "how every Phase-1 change
is PROVEN safe") applies directly and the source plan calls this dimension
out as "cleanly eval-provable":

1. **A1** — `go test ./... -short` stays green (build/contract).
2. **A2** — extend the existing byte-budget ratchet style (currently
   `TestInputSchemaByteBudget` for static schemas) with an equivalent
   *dynamic*-side measurement: assert turn-2+ `status` payload size drops
   toward the ≤~4 KB subsequent-call target while turn-1 stays ≥ the full
   envelope.
3. **A3** — confirm `KnowledgeTracker`'s per-session seen-set is the single
   owner of "what's already been surfaced," not a second parallel tracking
   mechanism.
4. **B1** — tree-wide sweep: every `renderGuidance`/envelope call site that
   can be the FIRST call in a recovery path (not just literal session start)
   is enumerated and explicitly exempted from dedup, so no recovery path goes
   dark.
5. **B2 — the decisive layer.** Run a multi-turn `develop` flow-eval scenario
   on the real eval container (`eval-zcp`) and diff the per-turn `status`
   payload sizes turn-over-turn: turn 1 should carry the full atom envelope,
   turn 2+ should shrink toward the target budget with the SAME atoms not
   re-appearing verbatim. Then run a **compaction-recovery scenario
   specifically** — force/simulate a compaction mid-session and confirm the
   next `status` call re-emits the FULL envelope (the carve-out working, not
   silently degraded). Both scenarios must succeed (`subtype:success`) with
   the agent's own retro not flagging "missing guidance" or "repeated
   firehose" — i.e. prove both (a) the dedup actually reduces the repeat dump
   and (b) it does not regress the one situation (recovery) that depends on
   the old always-full behavior.

## Sketch

- Wire `KnowledgeTracker` (or an equivalent per-session seen-set) into
  `internal/workflow/render.go`'s `renderGuidance`, keyed the same way
  bootstrap already keys it.
- Add the explicit recovery carve-out first, prove it via B2's
  compaction-recovery scenario, THEN land the dedup — carve-out-first order
  avoids ever shipping a version where recovery is silently broken.
- Reuse the existing atom corpus / synthesis filtering (`synthesize.go`) —
  this is a presentation-layer dedup on top of already-selected atoms, not a
  change to which atoms get selected.

## Risks

- Getting the "is this a recovery call" detection wrong strands an agent
  mid-recovery with no guidance — this is the one failure mode the source
  plan calls out by name as the reason F2 is Phase 2, not Phase 1.
- `KnowledgeTracker`'s current bootstrap-only wiring may carry assumptions
  (session lifecycle, key shape) that don't hold for the longer-running
  develop-phase session; needs its own read before reuse, not blind wiring.
- Golden/snapshot tests that currently assert full-envelope content on every
  `status` call will need deliberate updates distinguishing turn-1 vs
  turn-N expectations.

## Refs

- `internal/ops/knowledge_tracker.go`, `internal/ops/knowledge_tracker_test.go`
- `internal/workflow/render.go`, `internal/workflow/synthesize.go`
- `internal/server/server.go`, `internal/tools/knowledge.go` (current bootstrap-only wiring)
- `internal/tools/schema_byte_budget_test.go` (`TestInputSchemaByteBudget` — static-side sibling ratchet to model the dynamic-side one on)
- `plans/bootstrap-restore-four-goals-2026-06-02.md` (source of the ≤~12 KB / ≤~4 KB target budget)
- `plans/backlog/develop-response-atom-proliferation.md` (sibling backlog entry — same "firehose" symptom, different lever: atom *content* reduction vs. this entry's cross-*call* dedup; complementary, not duplicate)

**Source:** extracted 2026-07-17 from nonauthoring-context-audit-2026-06-13 before archival.
