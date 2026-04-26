# Phase 2 tracker — Cross-atom dedup

Started: 2026-04-26
Closed: open

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
| 1 | Push-git commit/push mechanics | `strategy-push-git-push-{container,local}` | `develop-push-git-deploy`, `develop-close-push-git-{container,local}`, `export` push task | 760 B | PENDING | — | conflict 2 (downstream trigger) folds in here |
| 2 | Push-git downstream trigger | `strategy-push-git-intro` (selection); `-trigger-{webhook,actions}` (behavior) | `develop-push-git-deploy` (trigger decision prose) | 300 B | PENDING | — | competing-action conflict #2 |
| 3 | SSHFS mount/path semantics | `develop-platform-rules-container` | `develop-first-deploy-write-app`, `-push-dev-workflow-{dev,simple}`, `-push-dev-deploy-container`, `-http-diagnostic`, `-first-deploy-intro` | 650 B | PENDING | — | §4.3 candidate; 7 restatements |
| 4 | `zerops_dev_server` action/response shape | `develop-dynamic-runtime-start-container` | `develop-platform-rules-container`, `-close-push-dev-dev`, `-dev-server-triage`, `-push-dev-workflow-dev` | 520 B | PENDING | — | §4.3 candidate |
| 5 | Restart-only vs deploy-required (conflict) | `develop-push-dev-workflow-dev` (dev-mode dynamic exception); `develop-push-dev-workflow-simple` (simple-mode rule) | `develop-change-drives-deploy` (narrow blanket wording) | 260 B | PENDING | — | competing-action conflict #1 |
| 6 | Local-mode topology + runtime loop | `develop-platform-rules-local` (develop), `bootstrap-discover-local` + `bootstrap-provision-local` (bootstrap) | `develop-local-workflow`, `-dynamic-runtime-start-local`, `-close-push-dev-local`, `-push-dev-deploy-local` | 520 B | PENDING | — | axis-care (cluster boundaries) |
| 7 | `zerops_verify`-first cadence | `develop-verify-matrix` | `develop-first-deploy-{intro,verify}`, `-http-diagnostic`, `-close-push-dev-{standard,simple,local}` | 430 B | PENDING | — | §4.3 candidate |
| 8 | Env-ref syntax `${hostname_KEY}` | `develop-first-deploy-env-vars` (wiring) + `bootstrap-env-var-discovery` (catalog) | `develop-first-deploy-{scaffold-yaml,verify,write-app}`, `bootstrap-recipe-import`, `bootstrap-provision-local` | 380 B | PENDING | — | §4.3 candidate |
| 9 | DeployFiles class semantics | `develop-deploy-modes` (table) + `develop-deploy-files-self-deploy` (failure mechanism) | `develop-push-dev-deploy-container`, `develop-first-deploy-scaffold-yaml`, `develop-change-drives-deploy` | 340 B | PENDING | — | — |
| 10 | Standard-mode pair residual | `develop-first-deploy-promote-stage` + `develop-auto-close-semantics` | `develop-close-push-dev-standard` | 360 B | PENDING | — | — |
| 11 | First-deploy outline (`bootstrap-close` redirect) | `develop-first-deploy-intro` + sub-atoms | `bootstrap-close` | 300 B | PENDING | — | bootstrap-close redirect to develop |
| 12 | Local git-push preflight | `strategy-push-git-push-local` | `develop-close-push-git-local`, `develop-platform-rules-local`, `develop-strategy-review` | 290 B | PENDING | — | — |
| 13 | Browser verification protocol (conflict) | `develop-verify-matrix` | `develop-platform-rules-container` | 90 B | PENDING | — | competing-action conflict #3 |
| 14 | `deploy = new container` + `deployFiles` persists | `develop-platform-rules-common` | `develop-change-drives-deploy`, `develop-dynamic-runtime-start-container`, `develop-close-push-dev-dev`, `develop-push-dev-workflow-dev` | 410 B | PENDING | — | §4.3 candidate |
| 15 | Manual strategy + ZCP-out-of-loop | `develop-close-manual` + `develop-manual-deploy` | `develop-strategy-review` | 70 B | PENDING | — | — |

## Phase 2 EXIT (§7)

- [ ] All §4.3 candidates re-verified and acted on (or documented as "duplication justified by axis").
- [ ] `plans/audit-composition/dedup-log.md` committed listing every fact deduped + canonical home + non-canonical atoms updated.
- [ ] Probe re-run shows body-join recovery on at least 2 of the 4 baseline fixtures.
- [ ] Target: 3-6 KB body recovery achieved.

## §15.2 EXIT enforcement

- [ ] Every row above has non-empty final state.
- [ ] Every row that took action cites a commit hash.
- [ ] Every row whose phase required a Codex round cites the round outcome.
- [ ] `Closed:` date filled in.
