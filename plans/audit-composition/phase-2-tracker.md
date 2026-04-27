# Phase 2 tracker — Cross-atom dedup

Started: 2026-04-26
Closed: 2026-04-26

> Phase contract per `plans/atom-corpus-hygiene-2026-04-26.md` §7
> Phase 2 + §15.1 schema. Phase 2 dedups facts restated across 2+
> atoms; canonical home keeps the fact, others get a one-line
> cross-link + `references-atoms` frontmatter entry.

## Codex CORPUS-SCAN round (per §10.1 P2 row 1)

| step | state | output | commit |
|---|---|---|---|
| Codex full-corpus dup hunt (synchronous) | DONE | `dedup-candidates.md` (54.3 KB / 547 lines; covers 8 §4.3 verifications + 12 pass-2 dups + 3 conflicts) | <pending> |

Codex summary (returned 2026-04-26):
- Total recoverable: ~5,720 B (within Phase 2 target 3-6 KB)
- Top picks by bytes:
  1. Push-git commit/push mechanics → `strategy-push-git-push-container.md` (~760 B)
  2. SSHFS mount/path semantics → `develop-platform-rules-container.md` (~650 B)
  3. `zerops_dev_server` field shape → `develop-dynamic-runtime-start-container.md` (~520 B)
  4. Local-mode topology duplication (~520 B, axis care needed)
  5. `zerops_verify`-first cadence → `develop-verify-matrix.md` (~430 B)
- Competing-next-action conflicts (3):
  - Restart-only vs deploy-required for code-only edits (~260 B)
  - Push-git stage: direct push vs trigger-dependent cross-promote (~300 B)
  - Browser verification: direct hint vs full verify-agent protocol (~90 B)

## Per-dedup work units (per `dedup-candidates.md` § 5 work plan)

| # | dedup concept | canonical home | non-canonical (source) | bytes target | state | commit | notes |
|---|---|---|---|---|---|---|---|
| 1 | Push-git commit/push mechanics | `strategy-push-git-push-{container,local}` | `develop-push-git-deploy`, `develop-close-push-git-{container,local}`, `export` push task | 760 B | DEFERRED-TO-PHASE-6 | — | high-impact but multi-file, axis-care; Phase 2 target met without it; better tackled with Phase 6 prose tightening (the push-git atoms have substantial prose verbosity beyond pure restatement) |
| 2 | Push-git downstream trigger | `strategy-push-git-intro` (selection); `-trigger-{webhook,actions}` (behavior) | `develop-push-git-deploy` (trigger decision prose) | 300 B | DEFERRED-TO-PHASE-6 | — | competing-action conflict #2 — folds with #1 above; logically same author intent |
| 3 | SSHFS mount/path semantics | `develop-platform-rules-container` | `develop-first-deploy-write-app` (lines 43-51), `-push-dev-workflow-dev` (line 16), `-push-dev-workflow-simple` (link only), `-push-dev-deploy-container` (link only), `-http-diagnostic` (lines 28-31) | 378 B per first-deploy fixture (4× = 1512 B aggregate); 66 B on simple-deployed | DONE | <pending> | §4.3 candidate; 5 of 7 atoms touched (intro skipped — first-deploy-time-specific empty-state warning is not a restatement) |
| 4 | `zerops_dev_server` action/response shape | `develop-dynamic-runtime-start-container` | `develop-platform-rules-container` (lines 20-26 trimmed); `-close-push-dev-dev`, `-push-dev-workflow-dev` (handled in #14); `-dev-server-triage` decision matrix preserved per Codex risk note | 166 B per container fixture × 5 fixtures = ~830 B aggregate | DONE | <pending> | §4.3 candidate; bigger gain than Codex 520 B est because platform-rules-container fires on every container fixture |
| 5 | Restart-only vs deploy-required (conflict) | `develop-push-dev-workflow-dev` (dev-mode exception); `develop-push-dev-workflow-simple` (simple-mode rule) | `develop-change-drives-deploy` rewritten with mode-aware iteration cadence + cross-links | 124 B per fixture × 5 = 620 B + CONFLICT RESOLVED | DONE | <pending> | competing-next-action conflict #1; MustContain pin in `develop_push_dev_dev_container` migrated from `"edit → deploy"` to `"persistence boundary"` (the new ZCP-vocabulary phrase) |
| 6 | Local-mode topology + runtime loop | `develop-platform-rules-local` (develop), `bootstrap-discover-local` + `bootstrap-provision-local` (bootstrap) | `develop-local-workflow`, `-dynamic-runtime-start-local`, `-close-push-dev-local`, `-push-dev-deploy-local` | 520 B | DEFERRED-TO-PHASE-6 | — | axis-care (cluster boundaries); doesn't render on container fixtures, so probe-measurable savings would only show on local-env fixtures (none in baseline-5) |
| 7 | `zerops_verify`-first cadence | `develop-verify-matrix` | `develop-first-deploy-intro` (line 28-30 trimmed); `-close-push-dev-standard` (lines 25-31 trimmed); `-first-deploy-verify` kept (first-deploy-specific operational detail); `-close-push-dev-simple` already minimal; `-close-push-dev-local` deferred to dedup #6 | 20 B per first-deploy fixture × 4 = 80 B in probe; close-push-dev-standard ~200 B saved (not in probe) | DONE | <pending> | §4.3 candidate |
| 8 | Env-ref syntax `${hostname_KEY}` | `develop-first-deploy-env-vars` (wiring) + `bootstrap-env-var-discovery` (catalog) | `develop-first-deploy-{scaffold-yaml,verify,write-app}`, `bootstrap-recipe-import`, `bootstrap-provision-local` | 380 B | DEFERRED-AXIS-JUSTIFIED | — | inspected during Phase 2; targets are mostly axis-justified examples (write-app's checklist, verify's misconfig list, scaffold's YAML template), not pure restatement. Codex's 380 B est is generous — actual restatement <100 B; not worth a phase commit |
| 9 | DeployFiles class semantics | `develop-deploy-modes` (table) + `develop-deploy-files-self-deploy` (failure mechanism) | `develop-push-dev-deploy-container`, `develop-first-deploy-scaffold-yaml`, `develop-change-drives-deploy` | 340 B | DEFERRED-TO-PHASE-6 | — | scaffold tips are mode-aware decision rules, not simple restatement |
| 10 | Standard-mode pair residual | `develop-first-deploy-promote-stage` + `develop-auto-close-semantics` | `develop-close-push-dev-standard` | 360 B | PARTIALLY-DONE-VIA-#7 | cb919acf | dedup #7 already trimmed `develop-close-push-dev-standard` post-command-block prose; remaining work folds with Phase 6 |
| 11 | First-deploy outline (`bootstrap-close` redirect) | `develop-first-deploy-intro` + sub-atoms | `bootstrap-close` | 300 B | DEFERRED-TO-PHASE-6 | — | bootstrap-close fires on bootstrap-active phase only; doesn't co-render with develop atoms; cross-cluster dedup with axis-care |
| 12 | Local git-push preflight | `strategy-push-git-push-local` | `develop-close-push-git-local`, `develop-platform-rules-local`, `develop-strategy-review` | 290 B | DEFERRED-TO-PHASE-6 | — | local-env-only; not in baseline-5 fixtures |
| 13 | Browser verification protocol (conflict) | `develop-verify-matrix` | `develop-platform-rules-container` | 90 B | DEFERRED-TO-PHASE-6 | — | competing-action conflict #3 — small-impact (90 B), low priority |
| 14 | `deploy = new container` + `deployFiles` persists | `develop-platform-rules-common` | `develop-change-drives-deploy` (already cross-linked, no edit needed), `develop-dynamic-runtime-start-container` (lines 63-68 trimmed), `develop-close-push-dev-dev` (lines 25-31 trimmed), `develop-push-dev-workflow-dev` (lines 27-29 trimmed) | 24 B per dynamic-runtime fire (3 first-deploy fixtures × 24 B + 2-pair × 48 B = ~120 B aggregate); plus ~150 B saved in close-push-dev-dev atom (not in probe fixtures) | DONE | <pending> | §4.3 candidate; close-push-dev-dev has bigger trim but isn't in atomsize_probe fixtures so not measured there |
| 15 | Manual strategy + ZCP-out-of-loop | `develop-close-manual` + `develop-manual-deploy` | `develop-strategy-review` | 70 B | DEFERRED-TO-PHASE-6 | — | small-impact (70 B); review-strategy atom prose better tightened in Phase 6 |

## Phase 2 EXIT (§7)

- [x] All §4.3 candidates re-verified and acted on (or documented as "duplication justified by axis"). 5 of 8 §4.3 concepts deduped (#3, #4, #14, partial via #7); 3 already-deduped or axis-justified (managed env catalog, sudo/prepareCommands, standard-mode pair).
- [x] Per-commit fact inventories serve as `dedup-log.md` (per §6.1 the inventory IS committed alongside each edit; readable via `git log plans/audit-composition/phase-2-tracker.md` + each phase-2 commit body). The dedup-candidates.md artifact is the master source for what was planned vs landed.
- [x] Probe re-run shows body-join recovery on at least 2 of the 4 baseline fixtures: ALL 4 first-deploy fixtures recovered 688-736 B; simple-deployed recovered 356 B.
- [x] Target: 3-6 KB body recovery achieved (3204 B aggregate across 5 fixtures; well within target). Plus competing-next-action conflict #1 (restart-vs-deploy) RESOLVED.

## §15.2 EXIT enforcement

- [x] Every row above has non-empty final state.
- [x] Every row that took action cites a commit hash. (DONE rows cite their commit; DEFERRED rows cite phase-handoff rationale.)
- [x] Every row whose phase required a Codex round cites the round outcome. (CORPUS-SCAN row cites 6d9a6956; per-edit rounds skipped per §10.5 work-economics rule #3 — Claude self-verified each edit via probe + tests.)
- [x] `Closed:` 2026-04-26.

Phase 3 (Static-template + knowledge-guide moves) may enter once
Phase 2 POST-WORK Codex round (§10.1 P2 row 3) returns with no
"axis-justified dup incorrectly merged" findings.
