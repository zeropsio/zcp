# Plan: tatami-welcome-autostart

## Run State
- `phase:` assemble
- `base:` 791ce1e3af9dce7c92187136310bbab7166cb348
- `integration:` 8b9f9324; landed range c87934ab..8b9f9324
- `approved:` Rev-1, 2026-07-23 — owner requested implementation and approved deriving mode from existing env without setting a new value
- `codex:` APPROVE WITH CONDITIONS, `/tmp/codex-review-tatami-welcome-autostart.md` — conditions incorporated
- `next:` run ASSEMBLE battery, build/deploy to localflow zcp, and inspect the remote immutable extension install
<!-- material edit to Frame or Slice Register after approval resets phase to awaiting-approval -->

## Frame
**Outcome**: A ZCP container already configured with a custom GUI origin opens the new Zerops welcome as its first startup surface and hides Explorer, while ordinary production containers retain the current launcher.
| obs | evidence |
|---|---|
| `run.initCommands` completes before the code-server start command. | [SELF-VERIFIED:../zerops-docs/apps/docs/content/guides/deployment-lifecycle.mdx:65] |
| Bootstrap activates on `onStartupFinished`; `showInitial()` currently selects the startup launcher. | [SELF-VERIFIED:internal/content/templates/vscode-bootstrap-package.json:9], [SELF-VERIFIED:internal/content/templates/vscode-bootstrap-extension.js:347] |
| Welcome is currently command-only/dark; the spec names env-gated autostart as an additive mode that was not yet implemented. | [SELF-VERIFIED:docs/spec-welcome-mode.md:3], [SELF-VERIFIED:docs/spec-welcome-mode.md:15] |
| The target `localflow/zcp` live env store already contains `ZCP_WELCOME_BRIDGE_ORIGINS=https://febridge-24cb.prg1.zerops.app`. | `ssh zerops@zcp 'jq <safe whitelist> /etc/zerops-zembed/env.json'` on 2026-07-23 |
| The deployed code-server is 4.129.0 / Code 1.129.0 and contains `workbench.action.closeSidebar`. | `ssh zerops@zcp 'code-server --version; rg -l workbench.action.closeSidebar ...'` on 2026-07-23 |
- AC1: With a non-empty valid `ZCP_WELCOME_BRIDGE_ORIGINS`, activation opens the singleton `zerops.welcome` surface instead of `ZCP Launcher`, including when restored editor tabs exist, and later watched env changes do not reopen the launcher over welcome. — planned evidence: focused Node startup tests + live re-init/reload observation.
- AC2: The custom-GUI startup branch closes the primary sidebar/Explorer idempotently after opening welcome. — planned evidence: command-recorder assertion + live UI retest.
- AC3: With the custom-origin env absent/empty/invalid, current startup behavior is unchanged: empty workbench opens `ZCP Launcher`; restored editors skip initial open; Explorer is not closed. — planned evidence: negative table tests.
- AC4: The bumped immutable bootstrap extension installs on `localflow/zcp` through the binary-copy dev loop and the target index points at the new version. — planned evidence: parity/install tests + remote version/index inspection.
**Non-goals**: browser-parent origin detection; frontend/Tatami changes; mutating service env; production release/deploy; changing bridge trust/ACK semantics. · **Constraints**: derive mode only from the existing live zembed store; unknown/standalone falls back to current behavior; preserve lazy welcome loading outside the gated branch; bump `BootstrapExtVersion`.
**Risk class**: medium — trigger: owner asks + live test-service mutation
**Assumptions**:
- [VERIFIED] Init is early enough to install the extension before code-server activation. — `../zerops-docs/apps/docs/content/guides/deployment-lifecycle.mdx:65`
- [VERIFIED] The target test service already carries the chosen instance marker in its watched env store. — live safe-whitelist SSH observation, 2026-07-23
- [VERIFIED] `workbench.action.closeSidebar` exists in the deployed Code 1.129 bundle. — live read-only SSH grep, 2026-07-23
- [VERIFIED] `ZCP_WELCOME_BRIDGE_ORIGINS` is service-instance configuration, not proof of the current parent browser origin. — `internal/content/templates/vscode-bootstrap-welcome.js:149`; owner accepted instance-level behavior on 2026-07-23

## Evidence Ledger
| claim | gates | surface | command | observed | verdict | promote |
|---|---|---|---|---|---|---|
| custom GUI marker exists before activation | AC1 | mcp | `ssh zerops@zcp 'jq <safe whitelist> /etc/zerops-zembed/env.json'` | `"ZCP_WELCOME_BRIDGE_ORIGINS": "https://febridge-24cb.prg1.zerops.app"` | CONFIRMED | `TestBootstrapExtension_CustomGUIOrigin_AutoOpensWelcome` + spec §1 |
| deployed Code supports an idempotent sidebar close command | AC2 | repo | `ssh zerops@zcp 'code-server --version; rg -l workbench.action.closeSidebar ...'` | `Code 1.129.0`; command found in workbench bundles | CONFIRMED | `TestBootstrapExtension_CustomGUIOrigin_ClosesSidebar` + spec §1 |

## Slice Register
| ID | Title | Depends | Files | Layers | Gate | State |
|---|---|---|---|---|---|---|
| S1 | Custom-GUI welcome autostart | — | `docs/spec-welcome-mode.md`; `internal/content/templates/vscode-bootstrap-extension.js`; `internal/content/templates/vscode-bootstrap-package.json`; `internal/content/welcomejs/welcome_dark.test.js`; `internal/content/welcomejs/diagnostics.test.js`; `internal/init/adapters/claude.go`; `internal/init/adapters/launcher_test.go`; `plans/tatami-welcome-autostart-2026-07-23.md`; `plans/tatami-welcome-autostart-2026-07-23.retest.md` | unit | review | landed |

Gate ∈ autonomous|review|owner · State ∈ pending|building|landed|blocked. Overlapping `Files` never share a wave.

S1 replay: RED=1 (custom-GUI startup assertions failed against `c87934ab`);
GREEN=0 (7/7 Node tests passed against `41885504`). Integrated focused
checks: Node 7/7, Go init packages `ok`, `make lint-fast` 0 issues.

## Verify Trace
| ACx | check | result | evidence |
|---|---|---|---|
| AC1 | `node --test internal/content/welcomejs/welcome_dark.test.js` custom-origin startup cases | not-run | pending |
| AC2 | same suite asserts `workbench.action.closeSidebar` after welcome open | not-run | pending |
| AC3 | same suite asserts absent/empty/invalid env preserves launcher/restored-tab behavior | not-run | pending |
| AC4 | Go version parity/install tests + `make zcp-dev-deploy` + remote extension-index inspection | not-run | pending |
| — | negative/regression: no custom origin keeps command-only lazy welcome and current launcher policy | not-run | pending |

## Promotion
- Contracts → `docs/spec-welcome-mode.md` §1
- Invariants → `welcome_dark.test.js` custom-GUI startup/production-fallback tests + `launcher_test.go` source/version pins + `docs/spec-welcome-mode.md` §1
- CLAUDE.md trap line (≤1): none
- This plan → `plans/archive/` on LAND close
