# Phase 6 axis-B candidates — Codex PRE-WORK (2026-04-27)

Round type: PRE-WORK per §10.1 Phase 6 row 1
Reviewer: Codex

> **Artifact write protocol note (carries over).** Codex sandbox blocks artifact writes; reconstructed verbatim from text response.

## Per-atom audit (top 25 atoms with axis-B content)

(Full table-form summary; ranked descending by recoverable bytes.)

| # | atom | current B | target B | recoverable B | path | risk |
|---|---|---:|---:|---:|---|---|
| 1 | `develop-platform-rules-local` | 2659 | 1810 | 850 | TABLE | MEDIUM |
| 2 | `bootstrap-route-options` | 2713 | 1950 | 760 | TABLE | MEDIUM |
| 3 | `bootstrap-close` | 1897 | 1250 | 650 | TABLE | MEDIUM |
| 4 | `develop-ready-to-deploy` | 1901 | 1280 | 620 | DECISION-TREE-TRIPLET | HIGH |
| 5 | `bootstrap-provision-rules` | 2364 | 1800 | 560 | TABLE | MEDIUM |
| 6 | `develop-first-deploy-write-app` | 2465 | 1950 | 520 | TABLE | HIGH |
| 7 | `develop-verify-matrix` | 1715 | 1235 | 480 | TABLE | HIGH |
| 8 | `bootstrap-resume` | 1747 | 1290 | 460 | DECISION-TREE-TRIPLET | MEDIUM |
| 9 | `develop-first-deploy-asset-pipeline-local` | 1746 | 1320 | 430 | TIGHTEN-IN-PLACE | **LOW** |
| 10 | `develop-first-deploy-asset-pipeline-container` | 1950 | 1520 | 430 | TIGHTEN-IN-PLACE | **LOW** |
| 11 | `develop-api-error-meta` | 1912 | 1530 | 380 | TABLE | **LOW** |
| 12 | `develop-dynamic-runtime-start-local` | 1606 | 1250 | 360 | TABLE | **LOW** |
| 13 | `develop-dynamic-runtime-start-container` | 2398 | 2050 | 350 | TABLE | **LOW** |
| 14 | `bootstrap-env-var-discovery` | 2315 | 1995 | 320 | TABLE | MEDIUM |
| 15 | `develop-dev-server-triage` | 2491 | 2190 | 300 | DECISION-TREE-TRIPLET | **LOW** |
| 16 | `develop-deploy-files-self-deploy` | 1423 | 1130 | 290 | TIGHTEN-IN-PLACE | HIGH |
| 17 | `develop-deploy-modes` | 2105 | 1825 | 280 | TABLE | MEDIUM |
| 18 | `develop-first-deploy-scaffold-yaml` | 2124 | 1865 | 260 | TABLE | MEDIUM |
| 19 | `develop-implicit-webserver` | 1675 | 1435 | 240 | TABLE | **LOW** |
| 20 | `develop-http-diagnostic` | 1695 | 1465 | 230 | NUMBERED-LIST | MEDIUM |
| 21 | `bootstrap-recipe-import` | 1684 | 1465 | 220 | NUMBERED-LIST | MEDIUM |
| 22 | `develop-env-var-channels` | 1524 | 1345 | 180 | TABLE | **LOW** |
| 23 | `develop-platform-rules-common` | 1421 | 1260 | 160 | TIGHTEN-IN-PLACE | **LOW** |
| 24 | `bootstrap-provision-local` | 1280 | 1140 | 140 | TABLE | **LOW** |
| 25 | `develop-manual-deploy` | 1430 | 1290 | 140 | TABLE | **LOW** |

## Atoms already at LEAN (no Phase 6 work)

- `develop-platform-rules-container` (1698 B) — compact bullets; cited rule wording, not verbosity
- `develop-push-git-deploy` (1683 B) — procedure is compact + command-heavy
- `develop-mode-expansion` (1645 B) — narrow workflow with necessary JSON shape
- `develop-dev-server-reason-codes` (1572 B) — already a dispatch table
- `develop-first-deploy-intro` (1444 B) — compact flow

## Total recoverable

| Risk bucket | Atom count | Bytes |
|---|---:|---:|
| LOW (mechanical) | 11 | 3,110 B |
| MEDIUM (judgment) | 10 | 4,590 B |
| HIGH (fact-preservation hard) | 4 | 1,910 B |
| **Total Phase 6 recoverable** | 25 | **9,610 B** |

## Phase 6 work plan (priority by bytes × LOW-risk)

1. `develop-first-deploy-asset-pipeline-local` — 430 B (sibling of #2, similar shape)
2. `develop-first-deploy-asset-pipeline-container` — 430 B
3. `develop-api-error-meta` — 380 B (fires broadly)
4. `develop-dynamic-runtime-start-local` — 360 B
5. `develop-dynamic-runtime-start-container` — 350 B (fires broadly)
6. `develop-dev-server-triage` — 300 B (fires on develop-active dynamic)
7. `develop-implicit-webserver` — 240 B (implicit-webserver fixture)
8. `develop-env-var-channels` — 180 B (fires broadly)
9. `develop-platform-rules-common` — 160 B (fires broadly)
10. `bootstrap-provision-local` / `develop-manual-deploy` — small cleanup

## Risks + watch items

- Phase 6 is the riskiest phase. Per-atom Codex per-edit review is MANDATORY for HIGH-risk atoms.
- HIGH: `develop-ready-to-deploy`, `develop-first-deploy-write-app`, `develop-verify-matrix`, `develop-deploy-files-self-deploy` — carry ZCP-specific operational nuance where shortening can lose recovery conditions, verification semantics, or data-loss warnings.
- MEDIUM: `bootstrap-route-options` + `bootstrap-close` touch workflow state transitions.
- LOW: table conversions are mostly mechanical; preserve command syntax + guardrails.
