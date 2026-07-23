# Plan: tatami-welcome-autostart

## Run State
- `phase:` owner-retest
- `base:` 791ce1e3af9dce7c92187136310bbab7166cb348
- `integration:` 80c0e969 (S4 init-time `zeropsSubdomain` policy)
- `approved:` Rev-4, 2026-07-23 — owner retest showed Tatami has no linked-runtime GUI export and directed init-time detection from the platform-provided `zeropsSubdomain`
- `codex:` APPROVE WITH CONDITIONS, `/tmp/codex-review-tatami-welcome-autostart.md` — conditions incorporated
- `next:` owner reloads/opens the localflow/Tatami editor and executes `plans/tatami-welcome-autostart-2026-07-23.retest.md`
<!-- material edit to Frame or Slice Register after approval resets phase to awaiting-approval -->

## Frame
**Outcome**: A ZCP container linked to a custom Zerops GUI opens the current main welcome as its first startup surface and hides Explorer, while ordinary production containers retain the current launcher.
| obs | evidence |
|---|---|
| `run.initCommands` completes before the code-server start command. | [SELF-VERIFIED:../zerops-docs/apps/docs/content/guides/deployment-lifecycle.mdx:65] |
| Bootstrap activates on `onStartupFinished`; `showInitial()` currently selects the startup launcher. | [SELF-VERIFIED:internal/content/templates/vscode-bootstrap-package.json:9], [SELF-VERIFIED:internal/content/templates/vscode-bootstrap-extension.js:347] |
| Welcome is currently command-only/dark; the spec names env-gated autostart as an additive mode that was not yet implemented. | [SELF-VERIFIED:docs/spec-welcome-mode.md:3], [SELF-VERIFIED:docs/spec-welcome-mode.md:15] |
| The target `localflow/zcp` init environment contains `zeropsSubdomain=https://zcp-24cb-8080.prg1.zerops.app`. | `ssh zcp 'env | <safe exact-key filter>'` on 2026-07-23 |
| The pre-change remote `zcp-bootstrap-0.1.13` extension is byte-identical to current `origin/main` d0be6787. | local/remote SHA-256 comparison on 2026-07-23 |
| The deployed code-server is 4.129.0 / Code 1.129.0 and contains `workbench.action.closeSidebar`. | `ssh zerops@zcp 'code-server --version; rg -l workbench.action.closeSidebar ...'` on 2026-07-23 |
- AC1: With a valid HTTP(S) init-time `zeropsSubdomain` whose host is not `app.zerops.io`, init writes `autoOpenWelcome:true` and activation opens the singleton `zerops.welcome` surface instead of `ZCP Launcher`, including when restored editor tabs exist; later watched env changes do not reopen the launcher. — planned evidence: Go derivation/install tests + focused Node startup tests + live re-init/reload observation.
- AC2: The custom-GUI startup branch closes the primary sidebar/Explorer idempotently after opening welcome. — planned evidence: command-recorder assertion + live UI retest.
- AC3: With `zeropsSubdomain` absent/empty/invalid/non-HTTP(S), or hosted at `app.zerops.io`, init writes false and current startup behavior is unchanged; malformed/missing startup policy also fails closed, while `ZGUI_DATA_APP_URL` and `ZCP_WELCOME_BRIDGE_ORIGINS` never select onboarding mode. — planned evidence: Go negative table + Node fail-closed table.
- AC4: The bumped immutable bootstrap extension installs on `localflow/zcp` through the binary-copy dev loop, writes the derived startup policy, and the target index points at the new version. — planned evidence: parity/install tests + remote version/config/index inspection.
**Non-goals**: browser-parent origin detection; frontend/Tatami changes; mutating service env; production release/deploy; changing bridge trust/ACK semantics. · **Constraints**: derive mode during init from the existing system env only; unknown/standalone falls back to current behavior; preserve lazy welcome loading outside the gated branch; bump `BootstrapExtVersion`.
**Risk class**: medium — trigger: owner asks + live test-service mutation
**Assumptions**:
- [VERIFIED] Init is early enough to install the extension before code-server activation. — `../zerops-docs/apps/docs/content/guides/deployment-lifecycle.mdx:65`
- [VERIFIED] The target test service carries `zeropsSubdomain` in both init process env and the watched store without any user-set addition. — live safe-whitelist SSH observation, 2026-07-23
- [VERIFIED] `workbench.action.closeSidebar` exists in the deployed Code 1.129 bundle. — live read-only SSH grep, 2026-07-23
- [VERIFIED] `zeropsSubdomain` is the service URL, not proof of the current parent browser origin; owner explicitly selected it as init-time instance policy. — live env shape + extension-host boundary; owner direction, 2026-07-23

## Evidence Ledger
| claim | gates | surface | command | observed | verdict | promote |
|---|---|---|---|---|---|---|
| platform service URL exists during init | AC1 | mcp | `ssh zcp 'env \| <safe exact-key filter>'` | `zeropsSubdomain=https://zcp-24cb-8080.prg1.zerops.app` | CONFIRMED | `TestBootstrapAutoOpenWelcome_DerivesFromInitZeropsSubdomain` + spec §1 |
| deployed Code supports an idempotent sidebar close command | AC2 | repo | `ssh zerops@zcp 'code-server --version; rg -l workbench.action.closeSidebar ...'` | `Code 1.129.0`; command found in workbench bundles | CONFIRMED | `TestBootstrapExtension_CustomGUIOrigin_ClosesSidebar` + spec §1 |

## Slice Register
| ID | Title | Depends | Files | Layers | Gate | State |
|---|---|---|---|---|---|---|
| S1 | Custom-GUI welcome autostart | — | `docs/spec-welcome-mode.md`; `internal/content/templates/vscode-bootstrap-extension.js`; `internal/content/templates/vscode-bootstrap-package.json`; `internal/content/welcomejs/welcome_dark.test.js`; `internal/content/welcomejs/diagnostics.test.js`; `internal/init/adapters/claude.go`; `internal/init/adapters/launcher_test.go`; `plans/tatami-welcome-autostart-2026-07-23.md`; `plans/tatami-welcome-autostart-2026-07-23.retest.md` | unit | review | landed |
| S2 | Port autostart onto welcome UX v5 | S1 | `docs/spec-welcome-mode.md`; `internal/content/templates/vscode-bootstrap-extension.js`; `internal/content/templates/vscode-bootstrap-package.json`; `internal/content/welcomejs/welcome_dark.test.js`; `internal/content/welcomejs/diagnostics.test.js`; `internal/init/adapters/claude.go`; `internal/init/adapters/launcher_test.go` on `feat/welcome-ux-v2` | unit | review | landed |
| S3 | Restore current-main welcome and derive mode from runtime GUI URL | S1 | same bootstrap/spec/test surface, merged with `origin/main` d0be6787 | unit | owner | landed |
| S4 | Derive startup policy during init from `zeropsSubdomain` | S3 | `docs/spec-welcome-mode.md`; bootstrap extension/package; welcome startup/diagnostic tests; init adapter/install tests; retest pack | unit + live | owner | landed |

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

S4 replay: RED=1 (missing Go derivation seam; three Node startup-policy
failures against the released extension); GREEN=0 (init/adapters + full
welcome suite). `localflow/zcp` installed 0.1.15 and generated
`{"autoOpenWelcome":true}` from its existing `zeropsSubdomain`.

## Verify Trace
| ACx | check | result | evidence |
|---|---|---|---|
| AC1 | Go init derivation/install tests + Node startup-policy cases | pass | custom service URL writes true; activation opens welcome and watcher cannot reopen launcher |
| AC2 | same suite asserts `workbench.action.closeSidebar` after welcome open | pass | command-recorder assertion; deployed extension contains the command |
| AC3 | Go table covers missing/invalid/non-HTTP/app; Node covers missing/malformed/non-boolean + legacy env inputs | pass | all default-mode cases remain lazy |
| AC4 | version parity/install tests + dev copy/init + remote policy/index inspection | pass | localflow index points to `zcp-bootstrap-0.1.15`; startup policy is true |
| — | current-main welcome content restored | pass | deployed 0.1.14 `welcome.html` and `welcome.js` are byte-identical to pre-change remote 0.1.13 |
| — | whole-repository race/lint/vet | pass | `make test-race`; `make lint-local`; `make vet-tags` |

Remote deployment observation, 2026-07-23: existing env values remained
unchanged; no env was created or mutated. Init read the platform-provided
`zeropsSubdomain` and wrote only the derived boolean startup policy.
`ZGUI_DATA_APP_URL` and `ZCP_WELCOME_BRIDGE_ORIGINS` no longer control
presentation. Visual acceptance remains owner-observed because the newly
installed extension activates on a fresh/reloaded code-server window.

## Promotion
- Contracts → `docs/spec-welcome-mode.md` §1
- Invariants → `welcome_dark.test.js` custom-GUI startup/production-fallback tests + `launcher_test.go` source/version pins + `docs/spec-welcome-mode.md` §1
- CLAUDE.md trap line (≤1): none
- This plan → `plans/archive/` on LAND close
