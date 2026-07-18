# User-test feedback (supplementary, 2026-04-26 follow-up)

User noted these findings during a live MCP test of the latest version.
User stated explicitly that this is supplementary context, not a request
to address now — saved here as durable input for Phase 7 (axis-tightness +
composition) and any post-Phase-8 follow-up.

## Findings

### F1 — Type drift after first deploy (handler bug, not corpus)

After first deploy of a Go application (`filedrop`) the service is
reported as `type: alpine@3.21` (because `run.base: alpine@3.21`), even
though it's a Go app at the build layer (`build.base: go@1.22`).

A subsequent bootstrap for a *different* service (`db`) failed with:

```
plan with isExisting: true expected go@1, got alpine@3.21
```

The user had to reset + resubmit with `alpine@3.21`. Decision points
the user proposes:

- `isExisting: true` should skip strict type check (lookup-by-hostname is sufficient), OR
- type tracking should hold the build base / "logical type" (`go@1`) and treat alpine as runtime image, OR
- at minimum, `develop-active` close should report `"service type narrowed to alpine@3.21 due to run.base — update plan if you bootstrap again"`.

**Scope verdict (for hygiene plan):** out of scope — this is a tool /
ops handler issue, not an atom-corpus content issue. Track separately;
do NOT pile onto Phase 7. The third option (one-line warning at close)
is the only atom-side touch and would belong in `develop-closed-auto`
or a new field on the close envelope — not Phase 7 scope.

### F2 — Duplicate "Checklist (simple-mode services)" across out-of-scope services

`zerops_workflow start develop scope=["filedrop"]` returned the
"Checklist (simple-mode services)" section twice — once for `filedrop`
and once for `weatherdash`, which was NOT in the scope filter.

User asks: filter guidance to only services in the active scope.

**Scope verdict (for hygiene plan):** PARTIALLY in scope. The atom that
renders the simple-mode checklist (`develop-checklist-simple-mode`) is
in the 68-atom unpinned list. If the duplication is in the atom's
content (e.g., the checklist body restates per-service info), Phase 7
axis-tightening should address. If the duplication is in the
synthesizer (renders one atom per matching service rather than once
per envelope), that's a `Synthesize` change — out of scope. Audit
during Phase 7 composition pass; flag a hand-off if it's a
synthesizer issue.

### F3 — `zerops_knowledge` BM25 weak on phrase + `zerops_workflow status` guidance redundant with start

Two sub-findings:

1. **`zerops_knowledge` returns 5 atoms with score 4–6**, but the
   interesting fact is in only one (e.g. "50 MB limit" mentioned only
   in `php-tuning`). BM25 might benefit from an exact-match-on-phrase
   boost.
2. **`zerops_workflow status` guidance is often redundant with what
   was shown at `start`.** Could be collapsed by default; expand only
   on `verbose=true`.

**Scope verdict (for hygiene plan):**
- F3.1 is out of scope (knowledge ranking, not corpus content).
- F3.2 is in scope IF the redundancy is the atom corpus rendering the
  same atoms at start AND status. Phase 7 composition pass should
  check whether `develop-intro` / `develop-knowledge-pointers` /
  similar fire on BOTH start and status envelopes; if yes, axis-
  tighten so they fire only on `start`. Add as a Phase 7 work item.

## Disposition

| # | Finding | Phase | Disposition |
|---|---|---|---|
| F1 | Type drift after deploy | — | Out of scope (tool/handler bug). Track in separate issue. |
| F2 | Duplicate checklist for out-of-scope services | 7 | Audit during Phase 7 composition pass; route to atom-fix or synthesizer-fix per finding. |
| F3.1 | BM25 phrase boost | — | Out of scope (knowledge ranking, not content). |
| F3.2 | status guidance redundant with start | 7 | Add Phase 7 work-item: axis-tighten atoms that fire on BOTH `start` and `status` envelopes when they should only render at `start`. |

This artifact was saved during the Phase 0 PRE-WORK round; not yet
applied to any phase tracker. Phase 7 entry checks should include
"have F2 + F3.2 been resolved or explicitly deferred?"
