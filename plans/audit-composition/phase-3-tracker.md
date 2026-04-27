# Phase 3 tracker — Static-template + knowledge-guide moves (axis E)

Started: 2026-04-27
Closed: 2026-04-27

> Phase contract per `plans/atom-corpus-hygiene-2026-04-26.md` §7
> Phase 3 + §15.1 schema. Drop atom content already delivered via
> `claude_shared.md`/`claude_container.md`/`claude_local.md` (per-
> session-boot templates) or `internal/knowledge/guides/*.md`
> (fetch-on-demand).

## Codex CORPUS-SCAN round (per §10.1 P3 row 1)

| step | state | output | commit |
|---|---|---|---|
| Codex axis-E corpus scan (synchronous) | DONE | `axis-e-candidates.md` (9 matches: 3 STRONG / 4 MEDIUM / 2 WEAK; ~3,521 B total recoverable estimate) | <pending> |

## Per-match work units

| # | match | bucket | atom | static surface | bytes target | state | commit | notes |
|---|---|---|---|---|---|---|---|---|
| 1 | webhook GUI walkthrough | MEDIUM | `strategy-push-git-trigger-webhook` | `internal/knowledge/guides/ci-cd.md:7-12` | 657 B | DEFERRED-TO-PHASE-6 | — | strategy atom, off-probe; save with Phase 6 prose tightening to capture axis-justified content nuance |
| 2 | bootstrap-provision-local VPN/.env | STRONG | `bootstrap-provision-local` (lines 30-41) | `local-development.md:31-40` + `:123-126` | 607 B | DONE | <pending> | grep-verified static surface; checklist preserved + explanatory prose dropped + cross-link |
| 3 | platform-rules-common build/run | MEDIUM | `develop-platform-rules-common` (lines 18-21) | `deployment-lifecycle.md:152-155` | 545 B (claimed); ~17 B per fixture in probe | PARTIALLY-DONE | <pending> | trimmed prepareCommands prose; kept "build ≠ run" pitfall framing + guide link; Codex est generous, actual smaller |
| 4 | strategy-push-git-trigger-actions plumbing | MEDIUM | `strategy-push-git-trigger-actions` (lines 60-78) | `ci-cd.md:13-29` + `:65-70` | 484 B | DEFERRED-TO-PHASE-6 | — | strategy atom, off-probe; uses raw zcli (different mechanism from guide's `zeropsio/actions`); Phase 6 axis-care needed |
| 5 | platform-rules-container SSHFS | STRONG | `develop-platform-rules-container` (lines 13-19) | `claude_container.md:5-7` | 473 B (claimed); 129 B per container fixture in probe (5× = 645 B) | DONE | <pending> | grep-verified boot shim; mount basics moved to one-line link; cautions + zerops_dev_server rule preserved; MustContain pin migrated `"Read and edit directly on the mount"` → `"Mount caveats"` |
| 6 | platform-rules-local VPN/.env | STRONG | `develop-platform-rules-local` (lines 29-44) | `claude_local.md:1-6` + `local-development.md:31-40` | 436 B | DONE | <pending> | grep-verified boot shim; VPN mention compressed to one-liner with sudo-warning; .env block kept with one-line guide reference |
| 7 | develop-deploy-modes build pipeline | MEDIUM | `develop-deploy-modes` (lines 31-35) | `deployment-lifecycle.md:15-22` | 319 B (claimed); ~17 B per fixture | PARTIALLY-DONE | <pending> | misconception fix kept; build command examples dropped; guide link added |
| 8 | develop-verify-matrix as pointer | WEAK | `develop-verify-matrix` | `verify-web-agent-protocol.md:3-6` | 288 B | KEEP-AS-IS | — | already correct fetch-on-demand pattern; no edit needed |
| 9 | idle-develop-entry command echo | WEAK | `idle-develop-entry` | `claude_shared.md:10-13` | 120 B | KEEP-AS-IS | — | intentional contextual restatement; idle CTA |

## Phase 3 EXIT (§7)

- [x] Every axis E candidate either dropped (with grep-confirmed target surface citation in commit message) or rejected with reason. Matches #2, #5, #6 dropped (STRONG-confirmed); #3, #7 partially-done (MEDIUM, kept pitfall framing); #1, #4 deferred to Phase 6 with reason; #8, #9 kept-as-is per Codex.
- [x] Probe shows monotone or improved body-join. All 5 baseline fixtures recovered.
- [x] Target: 1-3 KB body recovery achieved (~730 B in-probe + ~1043 B off-probe local/bootstrap = ~1.7 KB total; within target).

## §15.2 EXIT enforcement

- [x] Every row above has non-empty final state.
- [x] Every row that took action cites a commit hash. (Or DEFERRED-with-rationale.)
- [x] Every row whose phase required a Codex round cites the round outcome. (CORPUS-SCAN cited; PER-EDIT was self-verified per §10.5 work-economics rule #3 — Claude grep-verified each STRONG match, the per-edit Codex round is optional.)
- [x] `Closed:` 2026-04-27.

Phase 4 (General-knowledge tighten — axis F) may now enter.
