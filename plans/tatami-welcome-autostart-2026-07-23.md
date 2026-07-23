# Plan: tatami-welcome-autostart

## Run State
- `phase:` owner-retest
- `base:` 791ce1e3af9dce7c92187136310bbab7166cb348
- `integration:` ca4ae50d (merges current `origin/main` d0be6787)
- `approved:` Rev-3, 2026-07-23 — owner rejected the v5 branch UI, required the exact pre-deploy/main 0.1.13 welcome content, and moved presentation detection from the bridge allowlist to an existing standard runtime URL env
- `codex:` APPROVE WITH CONDITIONS, `/tmp/codex-review-tatami-welcome-autostart.md` — conditions incorporated
- `next:` owner reloads/opens the localflow Tatami embed and executes `plans/tatami-welcome-autostart-2026-07-23.retest.md`
<!-- material edit to Frame or Slice Register after approval resets phase to awaiting-approval -->

## Frame
**Outcome**: A ZCP container linked to a custom Zerops GUI opens the current main welcome as its first startup surface and hides Explorer, while ordinary production containers retain the current launcher.
| obs | evidence |
|---|---|
| `run.initCommands` completes before the code-server start command. | [SELF-VERIFIED:../zerops-docs/apps/docs/content/guides/deployment-lifecycle.mdx:65] |
| Bootstrap activates on `onStartupFinished`; `showInitial()` currently selects the startup launcher. | [SELF-VERIFIED:internal/content/templates/vscode-bootstrap-package.json:9], [SELF-VERIFIED:internal/content/templates/vscode-bootstrap-extension.js:347] |
| Welcome is currently command-only/dark; the spec names env-gated autostart as an additive mode that was not yet implemented. | [SELF-VERIFIED:docs/spec-welcome-mode.md:3], [SELF-VERIFIED:docs/spec-welcome-mode.md:15] |
| The target `localflow/zcp` live env store already contains the direct linked-runtime export `febridge_ZGUI_DATA_APP_URL=https://febridge-24cb-80.prg1.zerops.app`. | `ssh zerops@zcp 'jq <safe whitelist> /etc/zerops-zembed/env.json'` on 2026-07-23 |
| The pre-change remote `zcp-bootstrap-0.1.13` extension is byte-identical to current `origin/main` d0be6787. | local/remote SHA-256 comparison on 2026-07-23 |
| The deployed code-server is 4.129.0 / Code 1.129.0 and contains `workbench.action.closeSidebar`. | `ssh zerops@zcp 'code-server --version; rg -l workbench.action.closeSidebar ...'` on 2026-07-23 |
- AC1: With a non-empty valid direct `ZGUI_DATA_APP_URL`/`<service>_ZGUI_DATA_APP_URL` runtime export for a non-production GUI, activation opens the singleton `zerops.welcome` surface instead of `ZCP Launcher`, including when restored editor tabs exist, and later watched env changes do not reopen the launcher over welcome. — planned evidence: focused Node startup tests + live re-init/reload observation.
- AC2: The custom-GUI startup branch closes the primary sidebar/Explorer idempotently after opening welcome. — planned evidence: command-recorder assertion + live UI retest.
- AC3: With the GUI runtime URL absent/empty/invalid/app-only/build-snapshot-only, current startup behavior is unchanged; the code-server's own `zeropsSubdomain` and `ZCP_WELCOME_BRIDGE_ORIGINS` alone never select onboarding mode. — planned evidence: negative table tests.
- AC4: The bumped immutable bootstrap extension installs on `localflow/zcp` through the binary-copy dev loop and the target index points at the new version. — planned evidence: parity/install tests + remote version/index inspection.
**Non-goals**: browser-parent origin detection; frontend/Tatami changes; mutating service env; production release/deploy; changing bridge trust/ACK semantics. · **Constraints**: derive mode only from the existing live zembed store; unknown/standalone falls back to current behavior; preserve lazy welcome loading outside the gated branch; bump `BootstrapExtVersion`.
**Risk class**: medium — trigger: owner asks + live test-service mutation
**Assumptions**:
- [VERIFIED] Init is early enough to install the extension before code-server activation. — `../zerops-docs/apps/docs/content/guides/deployment-lifecycle.mdx:65`
- [VERIFIED] The target test service already carries the chosen direct runtime URL marker in its watched env store without any user-set addition. — live safe-whitelist SSH observation, 2026-07-23
- [VERIFIED] `workbench.action.closeSidebar` exists in the deployed Code 1.129 bundle. — live read-only SSH grep, 2026-07-23
- [VERIFIED] `*_ZGUI_DATA_APP_URL` is still service-instance configuration, not proof of the current parent browser origin. — live env shape + extension-host boundary; owner accepted instance-level behavior on 2026-07-23

## Evidence Ledger
| claim | gates | surface | command | observed | verdict | promote |
|---|---|---|---|---|---|---|
| custom GUI runtime URL exists before activation | AC1 | mcp | `ssh zerops@zcp 'jq <safe whitelist> /etc/zerops-zembed/env.json'` | `"febridge_ZGUI_DATA_APP_URL": "https://febridge-24cb-80.prg1.zerops.app"` | CONFIRMED | `welcome_dark.test.js` + spec §1 |
| deployed Code supports an idempotent sidebar close command | AC2 | repo | `ssh zerops@zcp 'code-server --version; rg -l workbench.action.closeSidebar ...'` | `Code 1.129.0`; command found in workbench bundles | CONFIRMED | `TestBootstrapExtension_CustomGUIOrigin_ClosesSidebar` + spec §1 |

## Slice Register
| ID | Title | Depends | Files | Layers | Gate | State |
|---|---|---|---|---|---|---|
| S1 | Custom-GUI welcome autostart | — | `docs/spec-welcome-mode.md`; `internal/content/templates/vscode-bootstrap-extension.js`; `internal/content/templates/vscode-bootstrap-package.json`; `internal/content/welcomejs/welcome_dark.test.js`; `internal/content/welcomejs/diagnostics.test.js`; `internal/init/adapters/claude.go`; `internal/init/adapters/launcher_test.go`; `plans/tatami-welcome-autostart-2026-07-23.md`; `plans/tatami-welcome-autostart-2026-07-23.retest.md` | unit | review | landed |
| S2 | Port autostart onto welcome UX v5 | S1 | `docs/spec-welcome-mode.md`; `internal/content/templates/vscode-bootstrap-extension.js`; `internal/content/templates/vscode-bootstrap-package.json`; `internal/content/welcomejs/welcome_dark.test.js`; `internal/content/welcomejs/diagnostics.test.js`; `internal/init/adapters/claude.go`; `internal/init/adapters/launcher_test.go` on `feat/welcome-ux-v2` | unit | review | landed |
| S3 | Restore current-main welcome and derive mode from runtime GUI URL | S1 | same bootstrap/spec/test surface, merged with `origin/main` d0be6787 | unit | owner | landed |

Gate ∈ autonomous|review|owner · State ∈ pending|building|landed|blocked. Overlapping `Files` never share a wave.

S1 replay: RED=1 (custom-GUI startup assertions failed against `c87934ab`);
GREEN=0 (7/7 Node tests passed against `41885504`). Integrated focused
checks: Node 7/7, Go init packages `ok`, `make lint-fast` 0 issues.

ASSEMBLE pivot evidence: the first localflow deploy installed 0.1.9 from
`main`, while the remote immutable dirs and repository showed the requested
new welcome is UX v5 on clean `feat/welcome-ux-v2` (`4b0ace4e`, manifest
0.5.0). This is a target-branch correction, not an acceptance change.

S2 replay: RED=1 against `4b0ace4e` with the new startup assertions; GREEN=0
against `3e8c54b8` (7/7 Node tests). The branch implementation bumps the
bootstrap extension from 0.5.0 to 0.5.1 and preserves the UX v5 welcome
templates.

Owner rejected S2's visual lineage after live retest. S3 uses the exact
0.1.13 welcome HTML/JS that was installed before this task and is present on
current `origin/main`; both files remain byte-identical in deployed 0.1.14.
S3 replay: RED=1 (three startup-policy failures with the new env contract);
GREEN=0 (7/7 Node tests) in `ca4ae50d`.

## Verify Trace
| ACx | check | result | evidence |
|---|---|---|---|
| AC1 | `node --test internal/content/welcomejs/welcome_dark.test.js` runtime-GUI startup cases | pass | current-main commit `ca4ae50d`, 7/7; watcher regression explicitly covered |
| AC2 | same suite asserts `workbench.action.closeSidebar` after welcome open | pass | command-recorder assertion; deployed extension contains the command |
| AC3 | table covers missing/invalid/app-only/build-only/bridge-only/own-subdomain fallback | pass | all default-mode cases remain lazy |
| AC4 | version parity/install tests + dev copy/init + remote extension-index inspection | pass | localflow runs `v9.133.0-6-gca4ae50d`; index points to `zcp-bootstrap-0.1.14` |
| — | current-main welcome content restored | pass | deployed 0.1.14 `welcome.html` and `welcome.js` are byte-identical to pre-change remote 0.1.13 |
| — | whole-repository race/lint/vet | pass | `make test-race`; `make lint-local`; `make vet-tags` |

Remote deployment observation, 2026-07-23: existing env values remained
unchanged; no env was created or mutated. Startup detection reads
`febridge_ZGUI_DATA_APP_URL`; `ZCP_WELCOME_BRIDGE_ORIGINS` remains only an
auth-bridge trust input. Visual acceptance remains owner-observed because
the newly installed extension activates on a fresh/reloaded code-server
window.

## Promotion
- Contracts → `docs/spec-welcome-mode.md` §1
- Invariants → `welcome_dark.test.js` custom-GUI startup/production-fallback tests + `launcher_test.go` source/version pins + `docs/spec-welcome-mode.md` §1
- CLAUDE.md trap line (≤1): none
- This plan → `plans/archive/` on LAND close
