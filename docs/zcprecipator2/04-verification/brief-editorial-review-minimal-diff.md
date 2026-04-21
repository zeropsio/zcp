# brief-editorial-review-minimal-diff.md — diff vs minimal v34 equivalent

**Role**: editorial-review
**Tier**: minimal

---

## 1. v34 predecessor for minimal: NONE

Same as showcase diff ([brief-editorial-review-showcase-diff.md §1](brief-editorial-review-showcase-diff.md)). Editorial-review is a genuinely new role; no v34 minimal dispatch existed. The `flow-minimal-spec-dispatches/` directory contains only `readme-with-fragments.md` (writer brief template) and `code-review-subagent.md` (code-review brief template) — no editorial-review predecessor.

**Additional consideration for minimal**: even the writer self-review content absorbed in v34 showcase ([brief-editorial-review-showcase-diff.md §2](brief-editorial-review-showcase-diff.md)) is thinner for minimal because minimal uses Path A main-inline writer — main's "self-review" is an inline check during deploy.readmes, not a dispatched writer sub-agent's self-review atom. So the v34 minimal "implicit editorial" surface is even smaller than v34 showcase's.

This makes editorial-review's additive value on minimal EVEN HIGHER than on showcase. Showcase gets fresh-context writer + editorial-review; minimal gets only editorial-review (writer is main-inline, non-fresh-context).

## 2. Content absorbed in v34 minimal → now editorial-review role

v34 minimal (reconstructed per RESUME decision #1 — no live minimal run audited) shows main-inline writing with implicit inline self-review. Content that v34 minimal main does implicitly and that now graduates to editorial-review dispatch:

| v34 minimal implicit check | New editorial-review atom |
|---|---|
| Main self-checks each fragment is non-empty | `single-question-tests.md` (applied by independent reviewer) |
| Main self-checks gotchas are Zerops-relevant | `classification-reclassify.md` (applied by independent reviewer) |
| Main self-checks no TODO tokens left | part of writer self-review still, not editorial |
| (Missing in v34 minimal) Cross-surface duplication check | `cross-surface-ledger.md` (NEW for minimal) |
| (Missing in v34 minimal) Citation audit | `citation-audit.md` (NEW for minimal) |
| (Missing in v34 minimal) v28 anti-pattern pattern-matching | `counter-example-reference.md` (NEW for minimal) |

**Disposition**: 3 of the 6 listed editorial-review functions are NEW for minimal (not just re-routed from writer self-review). Minimal's v34 editorial baseline was thinner than showcase's.

## 3. Size comparison (minimal)

| Content | v34 minimal | New architecture (minimal) |
|---|---|---|
| Editorial-review dispatch | — | ~8-9 KB transmitted prompt |
| Writer self-review | ~1-2 KB (inline, main's implicit check) | ~2-3 KB (now in briefs/writer/self-review-per-surface.md + main consumes inline if Path A) |
| Combined editorial-quality enforcement at close | implicit main-inline self-check (~1-2 KB) | explicit writer self-review (~2-3 KB) + editorial-review dispatch (~8-9 KB) = ~10-12 KB total |

**Net increase**: ~9-10 KB per minimal run. Trade: ~10 KB for an independent reviewer that restores the author/judge split that Path A main-inline writer collapsed.

## 4. Silent-drops audit

Zero content silently dropped. All v34 minimal implicit editorial checks are either:
- Retained in writer self-review (no removal; writer keeps its own pre-return self-check)
- Duplicated into editorial-review (intentional per the author/judge split)
- Enhanced in editorial-review (cross-surface ledger, citation audit, counter-example reference — 3 functions v34 minimal lacked entirely)

**Confirmed**: net-additive; no silent drops.

## 5. Differences from showcase diff

Minimal diff differs from showcase diff in 3 ways:

1. **v34 baseline thinner on minimal**: showcase had fresh-context writer sub-agent doing explicit self-review; minimal had main-inline implicit self-check. Editorial-review adds MORE net-value on minimal.
2. **Tier-branch in `surface-walk-task.md`**: fewer codebases + fewer env tiers; smaller walk surface.
3. **Path B default-on framing**: editorial-review on minimal is discretionary (ungated close) but default-on; dispatch fires unless explicitly opted out. On showcase editorial-review is gated substep.

## 6. Diff verdict

Net-additive on minimal with HIGHER relative value than showcase. Zero silent drops. Tier-branch differences are content-scope, not structural. The Path B default-on framing is the intended configuration; post-v35.5 minimal run evaluates whether default-on remains appropriate or if gating should be added.
