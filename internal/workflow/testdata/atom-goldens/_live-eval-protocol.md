# Live-eval protocol — atom corpus goldens

**Plan**: `plans/atom-corpus-verification-2026-05-02.md` §5.5.
**Owner**: `@krls2020` (initial assignment; transferable on agreement
between current and incoming owner). Named in `_live-eval-runs.md`
header. CODEOWNERS entry at `.github/CODEOWNERS` enforces review
responsibility — every commit touching `_live-eval-runs.md`
auto-requests review from the owner.
**Cadence**: at minimum once after merge of this plan; quarterly
thereafter via the reminder mechanism (GitHub issue auto-closed +
reopened per quarter, OR equivalent calendar mechanism agreed with
owner). Ad-hoc when production friction surfaces.

## Why live-eval

Goldens (Phase 1-4 of the plan) pin rendered atom output against
typed-Go envelope fixtures. The fixtures derive cleanly from
production envelope-construction code — but the fixtures are not the
production envelopes. Live-eval crosses that gap: drive a real eval-zcp
project through one of the canonical scenarios, dump the actual
envelope from `zerops_workflow action="status"`, diff against the
fixture envelope, and capture any drift in the evidence ledger.

**Scope**: cross-check, not gate. Phase 5 explicitly defers the first
run to post-merge follow-up. Substantive divergence is a follow-up
plan, not a merge-blocker.

## The 5 spot-check scenarios

Per plan §5.5: pick scenarios spanning the 5 phase categories. The
eval-zcp project (`i6HLVWoiQeeLv8tV0ZZ0EQ`, org `Muad`) is the
authorized playground.

| # | Scenario id | Phase | What to provision |
|---|---|---|---|
| 1 | `bootstrap/recipe/provision` | bootstrap-active | Recipe-route bootstrap on a `nodejs@22` runtime; observe at provision step (services ACTIVE, awaiting first deploy). |
| 2 | `develop/first-deploy-dev-dynamic-container` | develop-active | Dev mode `nodejs@22` with `startWithoutCode: true`; never deployed; in-container ZCP. |
| 3 | `develop/standard-auto-pair` | develop-active | Standard pair (`appdev`+`appstage`), close-mode auto, both deployed. |
| 4 | `strategy-setup/configured-build-integration` | strategy-setup | Single runtime, GitPushState=configured, BuildIntegration=none — agent picks webhook vs actions. |
| 5 | `export/publish-ready` | export-active | Standard pair, full export workflow driven to publish-ready. |

## Procedure (per scenario)

1. **Provision matching shape on eval-zcp** — use `ssh zcp "zcli ..."`
   per `CLAUDE.local.md` (zcli authenticated, scope pinned).
2. **Drive into target state** — for first-deploy / standard-auto-pair
   / publish-ready that means actually running the deploy / iteration
   loop / export.
3. **Capture rendered guidance** — call `zerops_workflow action="status"`
   (and `workflow="export"` direct call for the export scenario);
   record the response's `guidance` field verbatim.
4. **Capture envelope shape** — same response carries the envelope
   under `state` / status fields; dump it.
5. **Diff against fixture** — open the corresponding golden + fixture
   (in `scenarios_fixtures_test.go`); field-by-field compare envelope
   + body.
6. **Record evidence** — append to `_live-eval-runs.md`: date,
   scenario, service hostname(s) used, divergence summary, disposition
   ("clean", "fixture lag — file follow-up plan", "production drift
   — file <path>").
7. **Cleanup** — delete the eval services. `zcli` provisioning via
   the zcp container is fast (<2 min for a node service); leaving
   stray services accumulates eval-zcp clutter.

## Evidence format (for `_live-eval-runs.md`)

Append-only log. One block per run:

```
## YYYY-MM-DD — Q<N> <year> live-eval

**Owner**: <name>
**Scope**: <which of the 5 scenarios>
**Services provisioned**: <hostnames>
**Disposition**: clean / fixture-lag / production-drift / other

### Findings
<per-scenario notes — divergences if any>

### Follow-ups
<plan files filed, atom edits proposed, or "none" if clean>

### Cleanup
<services deleted: yes/no>
```

## Reminder mechanism

The owner sets up ONE of:

- **GitHub issue** with quarterly auto-close + reopen labels:
  `area:atom-corpus`, `kind:eval`. Title: "Live eval verification —
  atom corpus goldens — Q<N> <year>". Auto-reopens the next quarter.
- **Calendar reminder** in the owner's preferred system (Google,
  iCal, Linear, etc.). Same cadence.

Either path is acceptable; the issue path keeps history visible to
the team. The owner records the choice in their entry on this file
under the **Owner** line.

## When live-eval surfaces a defect

Three classes of follow-up:

1. **Fixture lag** — the golden's envelope shape predates a production
   envelope-construction change. Fix: update fixture, regenerate
   golden, commit. Same-PR atom edits if the new shape exposes
   atom-prose drift.
2. **Production drift** — production envelope shape changed in a way
   that exposes a bug (atom prose lies for the new shape). Fix: file
   a plan; treat as a regular atom-corpus defect; address via the
   normal Cycle 1/2/3 process if substantial, or a one-shot edit if
   small.
3. **Live-eval reveals a new lie-class** — atom asserts something
   true in fixture but false in production. Fix: add to the master
   defect ledger (`_review-ledger.md`) with verbatim quotes from
   both, then schedule the rewrite.

## Out of scope for live-eval

- Comparing `Generated time.Time` field — production uses `time.Now()`,
  fixtures use a fixed 2026-05-02 09:00 UTC seed. Field is metadata
  (compaction key); no atom body references it. Diff this field but
  don't flag.
- Running every test in `internal/...` against eval-zcp — that's
  E2E test coverage (`e2e/ -tags e2e`), separate concern.
- Re-running the 6-agent review pass — Phase 2's review is a one-shot
  curation. Recurring reviews are not in scope; live-eval is the
  ongoing cross-check.

## Status

**First run pending**. Owner assignment + first quarterly run land
post-merge of the atom-corpus-verification plan. Subsequent runs
append to `_live-eval-runs.md`.
