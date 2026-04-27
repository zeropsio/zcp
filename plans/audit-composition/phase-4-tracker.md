# Phase 4 tracker — General-knowledge tighten (axis F)

Started: 2026-04-27
Closed: 2026-04-27

## Codex rounds

| step | round type | state | output | commit |
|---|---|---|---|---|
| Codex axis-F PRE-WORK | PRE-WORK | DONE | `axis-f-candidates.md` (16 candidates: 6 DROP / 10 NUANCE-PRESERVE; ~1,895 B target) | <pending> |
| Codex axis-F POST-WORK | POST-WORK | DONE | `codex-round-p4-postwork.md` (4 NUANCE-LOST findings, all Phase 6+ severity, all RESOLVED in same commit) | <pending> |

## Per-atom work units

| # | atom | bytes target | state | commit | notes |
|---|---|---|---|---|---|
| 1 | `develop-implicit-webserver` (composer entrypoint) | 96 B | DONE | <pending> | dropped `index.php`/`index.html` framework examples; SAFE per Codex |
| 2 | `develop-first-deploy-write-app` (entry-point + observability) | 220 B | DONE | <pending> | tightened; restored "return HTTP 200" per Codex POST-WORK feedback |
| 3 | `develop-first-deploy-scaffold-yaml` (content-root tip) | 100 B | DONE | <pending> | tightened; restored ASP.NET `wwwroot/` example per Codex POST-WORK feedback |
| 4 | `develop-deploy-modes` (preserve/extract examples) | 100 B | DONE | <pending> | tightened; restored ASP.NET + `./out/app/App.dll` examples per Codex POST-WORK feedback |
| 5 | `develop-platform-rules-local` (framework dev-command list) | 145 B | DONE | <pending> | dropped command list; SAFE per Codex |
| 6 | `develop-dynamic-runtime-start-local` (commands + HTTP interp) | 323 B | DONE | <pending> | dropped framework commands + HTTP probe interpretation; SAFE per Codex |
| 7 | `develop-first-deploy-asset-pipeline-container` (Vite mechanics) | 340 B | DONE | <pending> | tightened helper examples + manifest mechanics; restored "PHP-FPM reads it on next request" per Codex POST-WORK feedback |
| 8 | `develop-first-deploy-asset-pipeline-local` | 466 B | DEFERRED-TO-PHASE-6 | — | local-env atom, off-probe; sibling of #7; better tackled with Phase 6 prose tightening |
| 9 | `develop-dev-server-triage` (5xx restart-don't prose) | 85 B | NO-OP | — | already tightened in Phase 2 dedup #5; no further trim |
| 10 | `bootstrap-provision-local` (".env contains secrets") | 20 B | NO-OP | — | small + already tightened in Phase 3 |

## Phase 4 EXIT (§7)

- [x] Per-atom commit messages cite kept ZCP-specific nuance where general framing was dropped (the rationale survives review). 4 NUANCE-LOST cases caught by Codex POST-WORK + restored in same commit per CLAUDE.local.md "quality over speed".
- [x] Probe shows monotone or improved body-join: all 5 fixtures recovered (178-447 B per).
- [x] Target: 1-2 KB recovered (1006 B in-probe aggregate; off-probe local-env edits add more).

## §15.2 EXIT enforcement

- [x] Every row above has non-empty final state.
- [x] Every row that took action cites a commit hash. (Or DEFERRED/NO-OP with rationale.)
- [x] Every row whose phase required a Codex round cites the round outcome (PRE-WORK + POST-WORK both cited).
- [x] `Closed:` 2026-04-27.

Phase 5 (Verifiable-at-runtime moves — axis G) may now enter.
