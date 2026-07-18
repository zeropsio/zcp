# PROVE

Entry: `phase: prove` — reached from FRAME's exit, or `/flow resume` finds
`phase: prove`. Revalidate on resume: `## Frame`'s Assumptions list exists
and every claim is tagged. Reads only that list; no other input.

## 1. Skip rule

Count `[PROBE]` tags in Frame's Assumptions. Zero → append one line to `##
Evidence Ledger`: "PROVE skipped — no load-bearing uncertainty", set `phase:
shape`, exit. Do not fabricate a probe to fill the section.

## 2. Budget

≤3 falsifiable claims probed per plan. A single `[PROBE]` claim needing a
few calls to settle still counts as one. Over budget → the plan is too big
or too uncertain: set `phase: frame`, exit — FRAME splits it, this phase
does not decide the split itself.

## 3. Surface ladder — cheapest that proves the claim, in this order

1. **`repo`** (free) — code/test/spec answers it → reclassify the Assumption
   to `[VERIFIED]` in Frame, cite the path, no ledger row needed.
2. **`mcp`** — `zerops_discover` / `zerops_verify` / `zerops_env` against the
   adopted `zcp-eval-clean` project. Seconds, live REST truth. ES-lag
   caveat: a search/list read right after ANY mutation (yours or a prior
   probe's) is not ground truth — absence can mean lag, not absence.
   Freshen with a by-id GET before concluding REFUTED (CLAUDE.md's
   ES-search-lag trap; `ListServices`/`SearchProcesses`/`SearchAppVersions`
   are the lagging ones, by-id `GetService`/`GetProcess` are not).
3. **`verifier`** — the `platform-verifier` subagent, for a mutating or
   platform-behavior claim. Pass it the claims list verbatim, not a
   paraphrase; it writes+runs temp live E2E and returns per-claim
   CONFIRMED/REFUTED/PARTIAL/UNTESTABLE. Map **PARTIAL and UNTESTABLE →
   INCONCLUSIVE** here — the ledger has no third/fourth state. Give it a
   unique `tmpverify-<run>` service name per probe (never a bare
   `tmpverify1` reused across runs — parallel/resumed PROVE sessions would
   collide). Its own memory (`.claude/agent-memory/platform-verifier/`)
   already skips re-verifying facts confirmed <30 days ago — don't force a
   redundant re-probe of those.
4. **`spike`** — only when 1-3 can't settle it. Write a throwaway
   `<pkg>/zzz_probe_test.go` (`//go:build e2e`) directly in the target
   package, run `go test -tags e2e -run <TestName> ./<pkg>/...`, read the
   result, then delete the file regardless of verdict (CLAUDE.md: delete,
   don't disable — it never survives as a skipped/commented test). Its
   assertion shape goes into the ledger's `promote:` field for a BUILD slice
   to write permanently.

## 4. Ledger entry — one row per probe, appended to `## Evidence Ledger`

`| claim | gates | surface | command | observed | verdict | promote |` —
`surface` ∈ `repo|mcp|verifier|spike`; `gates` names the ACx/Assumption this
claim gates; `command` is the exact call run; `observed` is a verbatim
result snippet; `verdict` ∈ `CONFIRMED|REFUTED|INCONCLUSIVE`; `promote`
names the permanent test that will pin this in a BUILD slice.

**Redact before anything reaches `observed`** — any token/credential (e.g.
`platform-verifier`'s own `ZCP_API_KEY=$(...)` extraction pattern) must
never reach a tracked file. A ledger row with a live secret in it is a
defect, not a detail.

## 5. Verdict consequences

- **CONFIRMED** — the Assumption's tag becomes `[VERIFIED]` in Frame, citing
  the ledger row.
- **REFUTED** — set `phase: frame`, exit; the plan's shape was wrong, not
  just one fact.
- **INCONCLUSIVE** (infra failure / ambiguous evidence — not the same as
  REFUTED) — retry the SAME surface once. Still inconclusive → pick one:
  reframe to remove the dependency, split/defer that slice, or halt with a
  blocked handoff (`templates/handoff.md`, `Phase: prove`). Never advance
  past an unresolved INCONCLUSIVE.

## 6. Set state and exit

Set `phase: shape`, `next:` = SHAPE's first action.

Exit: every `[PROBE]` row is CONFIRMED (or §1's skip note is in the
ledger) — the hard precondition for SHAPE's codex-brief gate.
