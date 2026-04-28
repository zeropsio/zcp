# Phase 6 tracker — Prereq chain wiring + RemoteURL refresh

Started: 2026-04-29 (immediately after Phase 5 EXIT `8353186b`)
Closed: TBD (pending Codex POST-WORK round + user go for Phase 7)

> Phase contract per `plans/export-buildfromgit-2026-04-28.md` §6 Phase 6.
> EXIT: chain pin tested, RemoteURL refresh pin tested,
> Codex POST-WORK APPROVE on chain logic.
> Risk classification: MEDIUM.

## Plan reference

- Plan SHA at session start: `8353186b`
- Phase 5 amendment branch order (validation-failed > git-push-setup-required) already lands in handler.

## Coding deltas (Phase 6 EXIT)

- `internal/tools/workflow_export_probe.go`: added `refreshRemoteURLCache(stateDir, meta, liveURL)`. Compares `meta.RemoteURL` against live; writes live to meta when they differ. Empty cache → set is initialization (no warning); non-empty cache → live is drift (warning). Returns warnings + error.
- `internal/tools/workflow_export.go`: handler calls `refreshRemoteURLCache` after `readGitRemoteURL`. Drift warnings appended to `bundle.Warnings`. Cache-write failures surface as non-fatal warning (the bundle still uses live remote regardless).
- `internal/tools/workflow_export_test.go`: three new tests:
  - `TestHandleExport_RemoteURLDrift_SurfacesWarning` — cache differs from live → bundle.Warnings has drift entry; cache updated.
  - `TestHandleExport_RemoteURLAligned_NoWarning` — cache matches live → no warning, no write.
  - `TestHandleExport_FreshMetaCacheSeed` — empty cache + live URL → cache seeded without drift warning.

## Plan deviations

- Plan §6 Phase 6 step 2 specified `refreshRemoteURL(ctx, hostname) (string, error)` returning the live URL. Handler already SSH-reads via `readGitRemoteURL`; the new helper takes the live URL as input rather than re-reading. Same effect, simpler interface.
- Plan §6 Phase 6 step 1 ("Verify chain logic in handleExport correctly composes the setup-git-push response") — chain logic was already implemented in Phase 3 + amended in Phase 5 (branch order). No additional work in Phase 6 beyond reverifying with the new tests.

## Verify gate

| check | command | result |
|---|---|---|
| fast lint | `make lint-fast` | 0 issues |
| full lint + atom-tree gates | `make lint-local` | 0 issues |
| short suite | `go test ./... -short -count=1` | all packages PASS |
| race | `go test ./... -short -race -count=1` | all packages PASS |

## Codex rounds

| agent | scope | output target | status |
|---|---|---|---|
| POST-WORK | cache-update correctness + stateless-tools invariant + error severity + race/concurrency + plan §6 alignment + chain composition reverification | `codex-round-p6-postwork-remoteurl.md` | running |

### Round outcomes

| agent | scope | duration | verdict |
|---|---|---|---|
| POST-WORK | cache-update correctness + stateless-tools invariant + error severity + race/concurrency + plan §6 alignment + chain composition | ~131s | SHIP-WITH-NOTES (4 recommendations) |

**Effective verdict**: SHIP. No blockers. 2 of 4 recommendations folded; 2 deferred (out of Phase 6 scope).

### Amendments applied (2 of 4)

1. **Helper-level table test for `refreshRemoteURLCache`** (Codex recommendation 1) — `TestRefreshRemoteURLCache` covers 5 branches directly: nil meta no-op, empty liveURL no-op, aligned cache no-op, empty-cache seed (no warning, write occurs), stale-cache drift (warning + write). Pins each guard without MCP fixture overhead.

2. **Helper-level write-failure test** (Codex recommendation 2) — `TestRefreshRemoteURLCache_WriteFailure` forces `WriteServiceMeta` failure via read-only state directory; asserts the helper still surfaces drift warning AND returns the underlying error so the handler can append a non-fatal warning.

### Recommendations deferred (out of Phase 6 scope)

3. **Race/concurrency for `WriteServiceMeta`** — Codex flagged that `WriteServiceMeta` uses atomic temp-file rename but does not serialize concurrent same-hostname writers (last-write-wins). This is existing infrastructure, not Phase 6 work; documenting for follow-up if real concurrency surfaces. Per CLAUDE.md "Don't add features beyond what the task requires" — out of scope.

4. **Plan §6 retrospective signature note** — Plan §6 Phase 6 specified `refreshRemoteURL(ctx, hostname) (string, error)`; implementation split into `readGitRemoteURL` (live SSH read) + `refreshRemoteURLCache(stateDir, meta, liveURL)` (cache update). Codex confirms intent preserved; the signature drift is documented in the tracker's "Plan deviations" section above (no plan amendment needed).

## Phase 6 EXIT

- [x] `refreshRemoteURLCache` helper landed.
- [x] Handler integrates the helper; warnings flow through bundle.
- [x] Three tests cover drift / aligned / seed paths.
- [x] Verify gate green.
- [x] Codex POST-WORK SHIP-WITH-NOTES (4 recommendations; 2 folded, 2 out-of-scope).
- [x] Codex round transcript persisted (`codex-round-p6-postwork-remoteurl.md`).
- [x] `phase-6-tracker.md` finalized.
- [ ] Phase 6 EXIT commits (impl + tests + tracker — see hashes once made).
- [ ] User explicit go to enter Phase 7.

## Notes for Phase 7 entry

1. Phase 7 is LOW risk — test consolidation only. Most of the test work is already done across phases 2-5; Phase 7 adds:
   - Mock e2e integration test in `integration/export_test.go`.
   - Confirmation that export doesn't perturb other meta state.
2. No Codex POST-WORK round mandatory per plan §7 (Phase 7 not listed).
3. Session pause point: Phase 7 begins ONLY after explicit user go.