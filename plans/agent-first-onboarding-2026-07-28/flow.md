# Plan: agent-first-onboarding (implementing /flow)

Entry brief: `handoff-flow.md` (same dir). Contract: `docs/spec-welcome-mode.md` +
`docs/spec-dataconsole.md` §4.4/inv-7 — spec-ahead-of-code; the specs are authoritative, this
plan is transient sequencing only.

## Run State
- `phase:` build
- `base:` a0e7d0595a1db93c80fa18dc628f215d495bd97d
- `fe-base:` 0d6423924 (`../frontend-legacy`, branch `feat/agent-first-onboarding`)
- `integration:` feat/agent-first-onboarding @ e85bba96 (Gate-1 e4994100 → S3 5cc4d6f8 →
  S4 68bc830d → S1 e85bba96; W1 complete → tracer fix 13b6cbee → S2 397848e8 → S5 d6b580fe → S7 2329e0bf; ALL zcp slices landed). S1 RED=1
  (assertion REDs on base e4994100), GREEN=0 (welcomejs 312/312 + parity + harness check).
  S2 RED=1 (JS assertion REDs on freshness/lifecycle at base e479d24a + Go missing-symbol on
  the renamed seam; slice transcript carries the assertion-level rename RED), GREEN=0
  (welcomejs 337/337, init+content ok). S5 RED=1 (71 assertion failures at base 397848e8),
  GREEN=0 (welcomejs 313/313, init+skillpacks+content ok; two real bugs caught by its own
  RED tests: expander edge case + focus-blur on unconditional DOM reorder). S2 §5.3
  interpretation accepted for LAND spec note:
  receiver closes only after an agent-ready outcome's relay-forwarded receipt — a
  pre-dispatch launch-failed never established a layout, so no auto-close. Replay evidence: S3 RED=1 (compile-RED skillpacks+cmd/zcp on exact new-seam
  symbols + assertion-RED internal/init; second skeleton-RED in slice transcript per additive-seam
  rule), GREEN=0. S4 RED=1 (dc-embed assertion; consolepanel new-seam TypeError; stub_view +
  discovery_contract module-load RED accepted — old extension.js eagerly requires 'vscode' and
  cannot load outside VS Code, so seam absence IS the load error; new DI seam createActivation is
  what makes them loadable), GREEN=0. Post-merge lint: only findings live in gitignored local
  tmp/captureseed/ (pre-existing junk, not slice code).
- `approved:` Rev-3, 2026-07-29 — owner approved "spusť W1"; GATE-1 spec writes executed
  (spec-skill-packs.md promoted; §11 button line; §5.4 trust-dialog note)
- `codex:` incorporated over 3 passes — Rev-1 changes-required (6 findings →
  `/tmp/codex-out-1785275004-26823-31764.md:6606`); Rev-2 4/6 resolved + 1 new
  (`/tmp/codex-out-1785275844-30481-29303.md:3638`); Rev-3 confirmed 1–2 resolved, its two
  residual line-items (S6d escape hatch removed → S6d=4/S6e=4 files exact;
  S4 transport MODIFY-or-DELETE alignment) applied verbatim
  (`/tmp/codex-out-*-rev3` via task b5ti5tsto)
- `rework (2026-07-29, owner live-drive verdicts → Codex-reviewed → landed complete):`
  root-caused the dismiss bug — `manualOpenResult.ok` only ever meant "the dialog OPENED"
  but was wired as auth completion, so the wizard launched the agent the moment the dialog
  appeared and a dismiss revealed vscode mid-launch. Auth completion is now the
  `markAuthorized` action (stack+agent matched); dismiss (X/ESC) bounces to `picking` with
  the pick retained. New `launch-ready` confirmation gate: post-auth the layer stays up,
  launch only on the explicit CTA (reverses the §8.1 no-CTA ruling — owner call); the
  secondary exit, Skip-for-now and failure-Continue all converge on close-overlay +
  project-detail (queued standard-directive machinery deleted). Roster is the static
  registry (the `ZCP_AGENTS` mirror parser, cross-repo fixture, skeleton tiles, announce
  reconciliation and `updateRoster` are deleted — fresh pool projects never set the var;
  container identity gate stays the backstop, restricted-pool invariant written into §8.1).
  Tiles redesigned: geometric stillness on hover, `--zcp-wizard-*` tokens, authorized
  badge, focus management. Codex verdicts folded: `successNavigation:'none'` (auth success
  no longer navigates to control-plane/openIde under the layer), `launch-ready` counted in
  `active()`, bounded bridge-context wait (15 s), busy-dialog gate, `setApiToken` declared
  out of wizard scope. FE S6k→S6m: e39192a62 → 567d00023 → b48bc9064 → bd5095f67
  (`fe-head`, 197/197, tsc app+spec clean, lint clean, seam greps clean). zcp: spec §8
  rewrite 531197e0, probe rework f1983954 (claiming/dismiss/launch-ready/hover-stillness/
  dark-picker shots + focus/aria-pressed JSON fields). Live probe run remains owner-gated
  (needs `ZE_EMAIL`/`ZE_PASS`).
- `next:` ASSEMBLE. UI iteration continues in fresh sessions via
  `handoff-ui.md` (entry point: map + operational knowledge + the traps). Post-lane fixes
  landed after live driving: dev-entry navigation + stack-emission race (FE c4f25af72), tile
  redesign (8f987fc23/e022777ec), z-index band + theme-aware tints + full names + single rank
  (fd500c8a7/f128b76cc), §5.3 onboarding layout in the container (zcp 6d1ad896, bundle 0.1.25).
  Live UI probe promoted to `tools/onboard-ui-probe/`. FE lane COMPLETE (S6a–S6h, 21 commits, 148/148 green, tsc clean;
  `fe-head: f6b36d21e`, 25 commits, 167/167). Four unwired seams were found by cross-layer inspection AFTER every
  phase reported green in isolation — three by the orchestrator (bridge events reached no
  subscriber; auth completion never reached the wizard; unconditional `set-mode "standard"`
  would have contradicted the wizard's own directive) and a fourth by S6g (the real cookie
  drain raised the wizard but never advanced it to `picking`, so only the dev param worked).
  Two more surfaced after that (an already-authorized pick never skipped to `launching`,
  breaking §8.2's dev loop; the announce's `agents` payload was dead on the wire while its
  carrier Subject had zero subscribers) — S6i/S6j closed both. SEVEN integration gaps total,
  none visible to any single phase's own green tests; all found by cross-layer greps between
  phases. Final wiring check passed on every hop: announce→wizard, outcomes→wizard,
  auth-result→wizard, both entries→picking, wizard mounted, launch-agent posted.
  Remaining: owner end-to-end click-through, then the fresh-session verifier battery +
  Verify Trace + retest pack. Previously: all 6 zcp slices landed + the full live battery is GREEN on the rig
  (bundle 0.1.24 @ 0578c126). Remaining: fresh-session verifier battery + Verify Trace fill +
  owner retest pack (GATE 2). FE lane S6a still waits for owner time.
- FE lane deferred-cleanup debt (each forced by the ≤5-file phase cap, none optional):
  the `ZcpClaimOverlayService` alias, the vestigial `notifyIframeLoaded()` no-op, and the
  service file rename — all collected into S6f. Also open for LAND: the bridge envelope
  builder is duplicated in `core/` rather than imported from `feature/` (S6c avoided a
  core→feature layering inversion; the honest fix is to move the builder INTO core and have
  the feature import it), and `crypto.randomUUID` is absent from this repo's jsdom env, so
  `feature.ts`'s existing randomUUID call is never unit-covered (real browsers have it).
- live battery (rig, 2026-07-29, bundle 0.1.24): reload · set-mode standard/onboarding ·
  launch · launch-failed · launch-idempotent · launch-eventid-reuse · no-directive · ack ·
  silent — ALL PASS (ack/silent need an unauthorized target: cursor's platform flag was
  temporarily removed and restored; rig state verified back to all-five-authorized).
  Three defects the battery found, each fixed RED-first: (1) the webview relay never copied
  `createdAt`, so the S2 freshness gate dropped EVERY live command — relay now forwards
  createdAt + stamps browser-clock `relayedAt`, host validates against relayedAt (4ebb561c);
  (2) manual `zerops.panel` on an existing dark receiver never revealed content, violating
  §1.4 (7f3dfbd6); (3) harness scenario 5 asserted a post-teardown re-ack the spec doesn't
  promise, and ack/silent didn't know about the §6 collapsed list (7f3dfbd6, 0578c126). Tracer live-proof PASSED 2026-07-29 on the rig (run.mjs
  MODE=launch: announce → set-mode → launch-agent → terminal with seeded claude argv →
  agent-ready correlated by eventId; exit 0) — after one tracer-found fix: a duplicated
  `handleBridgeOutcome` in welcome.html shadowed the working copy and threw on an undefined
  helper, so no outcome ever reached the top window; fixed + source-pinned
  (single-definition + no-stray-helper + createdAt re-stamp) in 13b6cbee, bump 0.1.20.
  S2 carries the addendum: dedup-store ≥2-min completed-retention floor + inbound createdAt
  freshness, both with an injectable clock (S1's recorded exclusions). FE lane S6a starts
  when the owner has time. Rig cleanup done (launched claude killed).
- owner ruling (2026-07-28, FRAME checkpoint): the legacy Agents-view title button is **dropped**
  with the `zerops.welcome` command identity (delete the `view/title` menu entry,
  vscode-bootstrap-package.json:26); the §11 line naming the button deletion is a GATE 1
  spec write.

## Frame

**Outcome**: A first-run Zerops container embedded in a non-`app.zerops.io` GUI is onboarded
entirely by the FE wizard — agent pick + authorization in an overlay, then a bridge
`launch-agent` the container executes terminal-only with the fixed prompt, answered by
`agent-ready` (S2) — while subsequent visits reduce to the agent panel (launcher + skill packs +
guided + single-tab Data Studio entry), per `spec-welcome-mode.md` W1–W15 +
`spec-dataconsole.md` §4.4/inv-7.

| obs | evidence |
|---|---|
| Bridge today: outbound `open-agent-auth` only; inbound only `open-agent-auth-ack`; no announce/set-mode/launch-agent/agent-ready anywhere | vscode-bootstrap-welcome.js:915-923, :980-983; grep zero |
| Real inbound blocker for a command channel is the live-authFlow guard, not the type check | vscode-bootstrap-welcome.js:967-970 |
| Inbound relay narrows to 6 primitive fields + 1024 B + 20/s; outbound `bridge-send` is unconstrained and re-stamps `createdAt` browser-side | vscode-bootstrap-welcome.html:1533-1544, :1499-1512 |
| §4.3 touches FIVE pipeline stages: webview pre-filter, host shape gate, host dispatch switch, flow gate, host→webview 6-case chain | welcome.html:1523-1557; welcome.js:945-958, :966-1015, :1713-1758, welcome.html:1550-1557 |
| Announce must be emitted from the host `ready` case — `sendBridgeMessage` silently no-ops on null panel; webview listener not yet installed at `open()` | welcome.js:858, :1726 |
| `startup.json` bool = `autoOpenWelcome`; rename touches claude.go:359-373, bootstrap_install_test.go:197-203, launcher_test.go:224, extension.js:99 | explore-zcp + adversary §5 |
| `hasEditors` read only on the legacy path; custom-GUI branch opens welcome unconditionally — `hadRestoredEditors` must hoist above extension.js:480 | vscode-bootstrap-extension.js:367-373, :481-486 |
| `zerops.welcome`: 1 registration, 1 execution, 2 manifest sites — incl. the legacy Agents-view title button (§11 self-contradiction → owner ruling below) | extension.js:466-478, :482; vscode-bootstrap-package.json:12, :26 |
| Registry: 5 agents; claude `opens[0]` = extension; `handleOnboard` launches `opens[0]`; seed helpers live at :1590-1632 | vscode-bootstrap-extension.js:44-73; welcome.js:1679 |
| Kickoff-wrapper deletion sites = research §4 list, verified exact; `onboard.test.js:87-89` marker pins die with it | claude.go:264, :520-567; welcome.js:1683-1685 |
| Skillpack port = bounded archive diff: +`set.go`/`set_test.go`, ~10 modified; axes/`fetchCommit` archive-only; `pack_install.test.js` already on branch; `pack_picker.test.js` + `spec-skill-packs.md` archive-only | `git diff HEAD archived/welcome-ux-redesign -- internal/skillpacks/` |
| Skill roots not created by init today (guided skill only) | internal/init/init.go:407-432 |
| Data Console panel is ALREADY a workspace-keyed singleton; conversion = sidebar deletion + rail flip + NEW command contributions (manifest has none) + stub view | consolePanel.js:184-210; consoleSession.js:22-24; vscode-studio/package.json |
| DC SPA: `dist/` IS the source (framework-free, no build step, `go:embed`) — edit `dist/dc-embed.js:17-26` directly | webui/embed.go:1-16 |
| FE branch = devel tip 022d0af03 + 4 bridge commits, clean; FE receiver hard-rejects non-`open-agent-auth` types → validator fork, not additive field | code-server-overlay.bridge.ts:153 |
| FE roster today = hardcoded `SUPPORTED_AGENT_TYPES`; `ZCP_AGENT_TYPES` was deliberately deleted for deploy-time-snapshot drift — `embed-ready` roster is the FIX for that drift (state it in FE-facing material) | zerops-services.model.ts:35-41; zerops-services.utils.ts:41-43 |
| FE deletion targets located (3 s, 45 s, `zcp-vscode-ready` — the latter validates nothing) | zcp-claim-overlay.service.ts:7, :11, :17, :71-90 |
| `?zcpOnboard=1` is greenfield (project-detail reads no query params); `?openIde=1` pattern exists on the service-stack page | service-stack-detail-control-plane.page.ts:209-229 |
| Env change → running container ~5–10 s, and a running process keeps boot-time environ (why extras read the live zembed store) | [KB: zerops-docs environment-variables.mdx:201]; welcome.js:166-179 |
| Embedding is an iframe; `frame-ancestors` is ZCP-nginx-owned incl. `localhost:*` — local rig needs no env change | nginx.conf.tmpl:122-123 |
| welcomejs = 19 suites / 284 cases: 1 dies, 9 survive, 9 update; `ui_structure` + `onboard` must be SPLIT, not deleted — sole pins of the W5 browser-stamp and the §5.1 seed helpers | adversary §6 table |
| Harness covers only the auth pair today | run.mjs:49; gui-harness.html:222-272 |

- **AC1** — §4.3 command channel: five types, one outcome per `eventId` (in-flight recorded
  before first side effect, duplicates coalesced, mismatched agentId rejected, idempotent
  re-ack, §4.3 bounds), `relay-forwarded` teardown receipt. Evidence: new welcomejs
  command-channel suite (W11/W12) + extended harness E2E announce→set-mode→launch-agent→
  agent-ready run on the localflow rig.
- **AC2** — §1.3 receiver lifecycle: boot-always under agentFirst+embedded, `awaiting-mode` +
  10 s window, self-close only on standard+`hadRestoredEditors`, never mid-intent,
  `zerops.panel` exemption; standalone always-panel. Evidence: receiver-lifecycle suite (W13)
  + P2 live proof pinned.
- **AC3** — §5 terminal-only launch: explicit `mode:"terminal"` selection, per-agent seeded
  argv, no probe gates (W10), post-dispatch silence (W14). Evidence: launch suite +
  `open_agent.test.js` gate inversions + P1 proofs pinned per agent.
- **AC4** — §1.1/§1.2 startup policy: `agentFirst` rename fail-closed, suppress→legacy
  fallback, legacy surfaces context-key-hidden while agent-first active (W3). Evidence:
  renamed Go test + welcome_dark/suppress updates.
- **AC5** — §6 agent panel: row-state table incl. Reconnect, collapsed list + expander,
  Data Studio box, a11y (W15). Evidence: panel behavior suite replaying the clickthrough's
  structure; owner retest pack.
- **AC6** — §7 skill packs + promoted `spec-skill-packs.md` (edited against main's actual
  package): axes, revision-gated `pack-set`, `fetchCommit`, unconditional skill-roots init
  step, picker behavior (W7); fresh Matt whole-repo→subset detach-migration test on THIS
  branch. Evidence: archived Go tests replayed RED→GREEN, `TestPackSet_*`, re-implemented
  picker suite, init-step test.
- **AC7** — §4.4 Data Studio single-tab: singleton reveal+switch, embedded rail always
  visible, repeated icon entry via stub view; sidebar subsystem deleted; `zcpStudio.open*`
  contributed. Evidence: the §4.4 "pinned during the conversion" list as tests + studiojs
  updates + `studioExtVersion` bump.
- **AC8** — §8 FE wizard: state machine (`claiming→picking→authorizing→launching→done|failed`),
  queued launch intent + 30 s timer, `set-mode` on every announce, single-select roster with
  the shared `ZCP_AGENTS` parser fixture, `?zcpOnboard=1` self-stripping dev entry,
  Continue-on-failure landing; 3 s/45 s/`zcp-vscode-ready` deleted. Evidence: FE suites
  (bridge spec fork + wizard service spec) + live rig walkthrough.
- **AC9** — §11 deletion inventory executed clean (incl. Go wrapper installer, `closeSidebar`,
  `autoOpenWelcome`, CTA, `anyRunnable`), no orphans; welcomejs split discipline honored.
  Evidence: grep sweeps per deleted symbol + full battery green.

**Non-goals**: launcher→panel convergence · `app.zerops.io` production onboarding · agent VS
Code extensions as onboarding vehicle · new packs beyond the catalog · Data Console reload
serializer · pixel-level visual design from the mock · `opencode-ai` UI support.

**Constraints**: every `vscode-bootstrap-*`/studio template edit ships its version-const bump
in the same commit (W1) · no backward compat — never shipped; delete freely · §9 security
floor (credential-free bridge, text-free `launch-agent`, CSP/nonce, allowlists) · archive is a
parts donor read via `git show` only · two-repo delivery; FE pins its own wizard tests ·
copy voice (first-time-developer) · English artifacts.

**Risk class**: **high** — trigger: public wire contract (§4.3 command channel) + security
surface (origin-gated remote command execution into `--dangerously-*` agent CLIs). §4/§5
slices gate at `review` minimum.

**Assumptions**:
- [VERIFIED] P1 — all five agents' argv auto-submit live-proven (ledger row P1; grok +
  cursor-agent after owner authorized them on the rig). No fallback needed; the §5.1 argv
  ships as-is. Bonus finding: the seeded prompt survives the first-run workspace-trust
  dialog on every CLI that shows one.
- [VERIFIED] P2 — embed-classification predicate live-measured in ALL THREE real shapes
  (ledger row P2): welcome webview shares the workbench origin; standalone `[cs,cs]`,
  localhost embed `[cs,cs,localhost:*]`, real app.zerops.io Embedded Editor
  `[cs,cs,app.zerops.io]` ⇒ predicate `ancestorOrigins.some(o => o !== self.origin)`;
  `window.top !== window` true everywhere (useless, as spec warns).
- [VERIFIED] P3 — `ZCP_AGENTS` FE-visibility settled at the repo rung (ledger row P3): the FE
  already reads recipe-set + env-op-written keys from the same store via `UserDataEntity`;
  design constraint recorded — §8.1 roster read must use the UserDataEntity path, never
  `stack.userData`.
- [VERIFIED] env-store change reaches a running container in ~5–10 s and a running process
  keeps its boot-time environ — [KB: environment-variables.mdx:201]; welcome.js:166-179.
- [VERIFIED] no deployed FE receiver to protect — bridge.ts absent from origin/devel +
  origin/main (git cat-file).
- [VERIFIED] Data Console panel already singleton — consolePanel.js:184-210.
- [VERIFIED] skillpack port surface = bounded archive diff (+2 files, ~10 modified).
- [ASSUMED] archived skillpack tests replay with mechanical adaptation (bounded diff; the
  build slice adapts).
- [ASSUMED] the localflow rig stays available for live proofs (stood up + verified
  2026-07-28, ticket 10).

**Carried for SHAPE** (adversary findings that are design inputs, not assumptions): the five
§4.3 pipeline stages; announce-from-`ready` constraint; `hadRestoredEditors` hoist above
extension.js:480; welcomejs split discipline (`ui_structure`/`onboard`/`welcome_source_pins`);
`welcome_dark.test.js:78` changes for two independent reasons; stale FE comment
bridge.ts:10-18 (container-clock claim, false since the browser re-stamp) dies in the FE
slice; `consoleRoutes.js` survives though unenumerated by §2A/§2B; `pack_install.test.js`
survives — spec §7 keeps all four CLI verbs, `pack-set` is additive (no owner ruling needed);
`embed-ready`-roster-as-drift-fix framing for FE-facing material; first-run workspace-trust
dialogs (claude/agy/codex/cursor) precede the seeded turn in an untrusted cwd and the turn
survives them — §5.4's terminal-is-the-answer posture covers it; the one-line §5.4 note is a
GATE 1 spec write; rig now has all five agents authorized (grok+cursor done 2026-07-28
during PROVE).

## Evidence Ledger
| claim | gates | surface | command | observed | verdict | promote |
|---|---|---|---|---|---|---|
| P2: webview ancestor chain distinguishes "host page itself framed" across shapes | AC2 / P2 | spike | throwaway `zzz-probe-ancestors.mjs` + `zzz-probe-appzerops.mjs` (puppeteer, harness machinery); standalone + localhost-embedded + REAL `app.zerops.io` Embedded Editor (owner-provided login) | welcome webview (`webview/browser/pre/fake.html`, same origin as workbench), measured in all THREE shapes: standalone `ancestors=[cs,cs]`; localhost embed `[cs,cs,http://localhost:50153]`; app.zerops.io embed `[cs,cs,https://app.zerops.io]` (workbench iframe itself `[app.zerops.io]` — direct embed, no intermediate frame). `isTop:false` in all (webview always framed, as spec warns). Predicate: `ancestorOrigins.some(o => o !== self.origin)` | CONFIRMED | W13 receiver-lifecycle tests use the three measured chains verbatim as fixtures |
| P1: every registry agent's terminal argv auto-submits the seeded prompt as the session's first turn | AC3 / P1 | verifier + spike | per-agent tmux capture on the rig container (`ssh zerops@zcp`), inert prompt `Reply with exactly OK and nothing else.`; grok+cursor re-run after the owner authorized them live | claude 2.1.220 ✓ submitted+answered; codex 0.145.0 ✓ (dispatch proven via immediate 401 on a stub key — an unsubmitted prompt sends nothing); agy 1.1.5 ✓ (`--prompt-interactive`) submitted+answered; grok 0.2.112 ✓ post-auth submitted+answered; cursor-agent 2026.07.20 ✓ post-auth — and the seeded prompt SURVIVES the workspace-trust dialog (queued through it; verifier saw the same for claude/agy/codex). Zero argv REFUTED | CONFIRMED | argv shapes already pinned by `TestBootstrapExtension_AgentCommandsPinned` (launcher_test.go:41); its doc-comment gains the auto-submit provenance + current CLI versions in the launch slice |
| P3: `ZCP_AGENTS` on the zcp service is FE-visible via the UserDataEntity read path | AC8 / P3 | repo | `zerops_discover service=zcp includeEnvs` + FE code cites | zcp service env store holds recipe-set `VSCODE_PASSWORD` (`source: zerops.yaml`) + env-op-written `ZCP_AGENT_OAUTH_*` — the exact keys the FE reads TODAY via `UserDataEntity` (`detectPublicAccess` reads VSCODE_PASSWORD, `extractServiceMetadataMulti` reads ZCP_AGENT_OAUTH_*; utils.ts:206-273). `getZcpEnv` does exact `===` match, no allowlist. A `ZCP_AGENTS` key set the same way lands in the same store. CONSTRAINT: §8.1 roster must read via the UserDataEntity path, NOT `stack.userData` (utils.ts:26-27 — secrets may be absent there) | CONFIRMED | FE wizard slice: roster read pinned to the UserDataEntity path in the wizard service spec; shared `ZCP_AGENTS` parser fixture per §8.1 |

## Slice Register

Design step: skipped — the spec pair (Codex-adversarially-reviewed) locks every material
trade-off; no new one surfaced in FRAME/PROVE.

| ID | Title | Depends | Files | Layers | Gate | State |
|---|---|---|---|---|---|---|
| S1 | Bridge command channel + terminal-only launch (tracer) | — | `vscode-bootstrap-welcome.{js,html}`, `vscode-bootstrap-package.json` (bump), `internal/init/adapters/claude.go` (bump only), welcomejs: NEW `command_channel.test.js` + `launch_gate.test.js` (incl. seed-helper coverage on the bridge path), UPDATE `message_allowlist.test.js`, minimal `tools/welcome-bridge-harness/{gui-harness.html,run.mjs}` type extension. `handleOnboard`/kickoff wrapper/old onboard sender stay ALIVE (deleted in S5 with the surface that renders them) | unit (JS+Go) | review | landed |
| S2 | Startup policy rename + receiver lifecycle + `zerops.panel` | S1 | `vscode-bootstrap-extension.js`, `vscode-bootstrap-welcome.{js,html}`, `vscode-bootstrap-package.json` (rename + button delete + bump), `internal/init/adapters/claude.go`, `bootstrap_install_test.go`, `launcher_test.go`, welcomejs: NEW `receiver_lifecycle.test.js`, UPDATE `welcome_dark.test.js`, `handshake.test.js`, `message_allowlist.test.js` + `welcome_panel.test.js` (both fetch `zerops.welcome` by name — :26/:12) | unit (JS+Go) | review | landed |
| S3 | Skill-pack port (code + tests only; spec promoted at GATE 1) | — | `internal/skillpacks/**` (per archive diff: +`set.go`,+`set_test.go`, ~10 modified), `cmd/zcp/skills.go`, `cmd/zcp/main.go`, `internal/init/init.go` (+init tests), NEW Matt detach-migration test | unit + tool | review | landed |
| S4 | Data Studio single-tab conversion | — | vscode-studio: `package.json`, `extension.js` (stub view inline), DELETE `lib/webviewSession.js`, `lib/cards.js`, `cards/managed.js`, `cards/refresh.js`, `handlers/refresh.js`, `handlers/console.js`, `lib/discoverToUIMap.js`, `lib/svc-icons.js`; MODIFY `lib/handlers.js`, `lib/consolePanel.js`; MODIFY-or-DELETE `lib/transport.js` (conditional — only if all remaining consumers are in the deleted set; either outcome reported explicitly); `webui/dist/dc-embed.js` + `webui/dist/app.js` (call site), `webui/spa/dc-embed.test.js`; studiojs: DELETE `webview_session/managed/refresh/console/discover_to_uimap/shell_render.test.js`, UPDATE `discovery_contract.test.js` + `consolepanel.test.js`, NEW `stub_view.test.js`, KEEP `browserdownload/consoleclient/transport.test.js`; `internal/init/adapters/studio.go` + `studio_test.go` (bump) | unit (JS+Go) | review | landed |
| S5 | Agent panel §6 + §7 UI + §11 deletions (walk-through/CTA/kickoff/onboard) | S2, S3, S4 | `vscode-bootstrap-welcome.{js,html}` (panel; DELETE `handleOnboard`, old onboard sender, `armKickoffMarker`/`kickoffMarkerPath`, CTA/`anyRunnable`), `vscode-bootstrap-package.json` (bump), `internal/init/adapters/claude.go` (DELETE `patchVSCodeClaudeWrapper`/`installKickoffWrapper` + call site :264; bump), DELETE `vscode-claude-kickoff-wrapper.py`, `claude_test.go`/`launcher_test.go` (wrapper assertions + argv provenance), welcomejs: NEW panel/a11y suites + ported `pack_picker.test.js`, DELETE `cta_flow.test.js` + `onboard.test.js` (seed pins live in `launch_gate.test.js` since S1), SPLIT `ui_structure.test.js`, UPDATE `open_agent.test.js` (promptless mode selection + W10 inversions), `state_matrix/welcome_panel/welcome_source_pins` | unit (JS+Go) | review | landed |
| S6a | FE: `ZCP_AGENTS` parser + shared fixture | — | FE repo (3): `core/zerops-services/zerops-services.utils.ts`, `zerops-services.model.ts`, NEW `zerops-services.utils.spec.ts` | unit | review | landed (2e583be12) |
| S6b | FE: bridge validator fork + listener extension | S6a | FE repo (4): `feature/code-server-overlay/code-server-overlay.bridge.ts`, `code-server-overlay.bridge.spec.ts`, `code-server-overlay.feature.ts`, `code-server-overlay.feature.html` | unit | review | landed (148599168) |
| S6c | FE: wizard service + state machine + dismissal-machinery deletion | S6b | FE repo (3): `core/zcp-pool-claim-base/zcp-claim-overlay.service.ts` (evolves; class → `ZcpOnboardWizardService`, file rename deferred — explicit cleanup note), NEW `zcp-claim-overlay.service.spec.ts`, `index.ts` (export) | unit | review | landed (0c7340de4) |
| S6d | FE: wizard UI + drain rewire (TestBed-tested; mount lands in S6e) | S6c | FE repo (4): NEW `core/zcp-pool-claim-base/zcp-onboard-wizard.component.ts` (standalone, inline template), NEW `zcp-onboard-wizard.component.spec.ts`, `zcp-pool-claim-base.effect.ts`, NEW `zcp-pool-claim-base.effect.spec.ts` | unit | review | landed (b5125394b) |
| S6e | FE: wizard mount + dev entry `?zcpOnboard=1` | S6d | FE repo (4): `app/app.container.html`, `app/app.container.ts` (standalone import), `pages/+project-detail/project-detail.effect.ts`, NEW `project-detail.effect.spec.ts` | unit | review | landed (ba5e6b975) |
| S6f | FE cleanup: retire the migration scaffolding | S6e | FE repo (≤5): delete the `ZcpClaimOverlayService` back-compat alias once every importer is migrated, delete the vestigial no-op `notifyIframeLoaded()` + its call site in `code-server-overlay.feature.ts`, rename the service FILE to `zcp-onboard-wizard.service.ts` | unit | review | landed (b2ca83de5) |
| S6g | FE: join the unwired seams (integration gap found by the orchestrator, not by any phase's own tests) | S6f | FE repo: subscribe `embedBridgeEvents$` → `wizard.onEmbedReady/onAgentReady/onLaunchFailed`; dispatch the auth dialog on pick and feed `manualOpenResult` → `wizard.authCompleted`; gate the unconditional `set-mode "standard"` auto-reply on wizard inactivity | unit | review | landed (c8c5027b9) |
| S6h | FE: real cookie-drain reaches `picking` (4th unwired seam, found by S6g) | S6g | FE repo (2): `zcp-pool-claim-base.effect.ts` + spec | unit | review | landed (5a16075a4) |
| S6i | FE: already-authorized pick skips to `launching` (§8.2 dev-loop rule; 5th seam) | S6h | FE repo (8): `parseAuthorizedAgents` + both entry effects + wizard service | unit | review | landed (e717076a3) |
| S6j | FE: announce confirms/refreshes the roster (§8.1) + delete the orphaned `embedBridgeEvents$` Subject | S6i | FE repo: `code-server-overlay.feature.ts`, wizard service + specs | unit | review | landed (f6b36d21e) |
| S6k | FE owner-rework core: `launch-ready` confirmation gate, real auth signals (`markAuthorized`, never `manualOpenResult.ok`), dismissal bounce to `picking`, static registry roster (`ZCP_AGENTS` mirror parser + fixture + skeleton + announce reconciliation deleted) | S6j | FE repo (13): wizard service+component, `code-server-overlay.feature.ts`, both entry effects, `zerops-services.utils/.model` + specs | unit | review | landed (e39192a62) |
| S6l | FE: caller-owned `successNavigation` on the auth dialog (`'none'` for wizard opens — auth success no longer re-docks the embed) + registry parity pin (every supported agent has handler + display name) | S6k | FE repo (9): `zcp-agent-auth-dialog` model/state/effect/feature + NEW handlers/effect/feature specs, `code-server-overlay.feature.ts` + spec | unit | review | landed (567d00023) |
| S6m | FE visual redesign: still-under-cursor card tiles (no transform, transition = background/border only, `@media (hover:hover)`), `--zcp-wizard-*` light/dark tokens, authorized corner-check badge, aria-pressed selected state, focus management (CTA on `launch-ready`, tile on dismissal bounce) | S6k | FE repo (5): wizard component+service + specs, `styles/base/_theme.scss` | unit | review | landed (b48bc9064; comment fix bd5095f67) |
| S7 | E2E harness full-conversation drivers (deterministic) | S1, S2, S5 | `tools/welcome-bridge-harness/**` (scenario matrix + contract tests failing at S7 base, README, Makefile target); LIVE battery runs at ASSEMBLE, needs S6e for the FE walkthrough | e2e | autonomous | landed |

Wave plan: W1 = S1, S3, S4 (disjoint write-sets) · W2 = S2 · W3 = S5 · W4 = S7.
**Worktree-base quirk (observed W1, 2026-07-29)**: `isolation: "worktree"` branches from
`main` (68f78120), NOT the checked-out integration branch — code-identical to e4994100
(branch diff = docs/ + plans/ only), but local `docs/` + `plans/` in a slice worktree are
STALE; every slice prompt must instruct reading specs + briefs via
`git show feat/agent-first-onboarding:<path>`, and RED-replay base = the slice's ACTUAL
worktree base. W1 bases: S1 = e4994100 (did a clean ff-only merge of the two docs commits),
S3/S4 = main@68f78120 (read specs via git show). W2+ slices depend on landed W1
code — verify each later wave's worktree base actually contains its Depends before letting
it run (a main-based worktree without S1's code is a hard stop → hand the agent the
integration SHA to cherry-pick or re-spawn after checking out the branch as main-tree HEAD). **The FE
lane (S6a–S6e) is governed by `frontend-legacy/CLAUDE.md` "PHASED EXECUTION" (≤5 files per
phase, explicit owner approval between phases, `npx tsc --noEmit` + `npx eslint . --quiet`
per phase)** — it runs owner-present, sequentially, NOT as AFK worktree agents; interleave
with zcp waves as owner time allows (no interop dependency until ASSEMBLE — no deployed
receiver exists). Tracer live-proof: after S1 lands, the ORCHESTRATOR (not the slice agent)
deploys to the rig (`eval/scripts/build-deploy.sh` + `zcp init` + reload) and drives the
minimal announce→set-mode→launch-agent→agent-ready conversation via the extended harness
before W2 spawns. Every template-touching slice (S1/S2/S5) carries its own
`BootstrapExtVersion` bump — sequential by Depends, so no bump conflicts. FE repo state is
tracked separately: `fe-base: 0d6423924` (branch `feat/agent-first-onboarding` =
devel tip `022d0af03` + 4 bridge commits).

## Verify Trace
| ACx | check | result | evidence |
|---|---|---|---|
| AC1 | `node --test internal/content/welcomejs/command_channel.test.js` (W11/W12: one-outcome-per-eventId, in-flight-before-side-effect, coalesced dupes, mismatched-agentId reject, re-ack, bounds, relay-forwarded teardown) | not-run | |
| AC1 | rig E2E: harness full conversation announce→set-mode→launch-agent→agent-ready (S7 mode) | not-run | |
| AC2 | `node --test internal/content/welcomejs/receiver_lifecycle.test.js` (W13: P2 chain fixtures, awaiting-mode + 10 s, self-close rules, mid-intent hold, `zerops.panel` exemption) | not-run | |
| AC2 | rig: reload-after-launch shows no panel beside restored terminal; standalone shows panel | not-run | |
| AC3 | `node --test internal/content/welcomejs/launch_gate.test.js` + updated `open_agent.test.js` (W10 inversions) + live rig launch lands prompt (P1 argv pinned) | not-run | |
| AC4 | `go test ./internal/init/... -run 'TestInstallBootstrap' -short` (renamed `agentFirst` policy test) + welcomejs suppress/`welcome_dark` suites | not-run | |
| AC5 | welcomejs panel + a11y suites (W15 focus retention/live-region) + Gate-2 owner visual pass | not-run | |
| AC6 | `go test ./internal/skillpacks/... ./cmd/... -short` incl. `TestPackSet_*` + fresh Matt detach-migration test + ported picker suite | not-run | |
| AC7 | `node --test internal/dataconsole/extension/studiojs/` + `spa/dc-embed.test.js` (rail-visible flip) + `go test ./internal/dataconsole/... -short`; icon-entry triple-click check in Gate-2 pack | not-run | |
| AC8 | FE: jest bridge spec fork + wizard service spec + registry parity pins (the shared `ZCP_AGENTS` fixture died with the mirror parser, S6k); rig walkthrough localhost:1111 (wizard + `?zcpOnboard=1` + auth-dismiss bounce + `launch-ready` CTA + Skip/failure converged exits) | not-run | |
| AC9 | grep sweeps: `kickoff|claudeProcessWrapper|autoOpenWelcome|anyRunnable|closeSidebar|zcp-vscode-ready|zerops\.welcome` → zero product-code hits; `go test ./... -short` + full welcomejs run green | not-run | |
| — | negative/regression: auth-bridge §4.2 flow unchanged (`bridge_flow.test.js` 28 cases stay green); legacy launcher survives under suppress (`launcher_flow.test.js`); Data Console write-token posture untouched (`TestWriteToken_DualClient_CallerBound`) | not-run | |

## Promotion
- Contracts → already promoted ahead of code: `docs/spec-welcome-mode.md` (rewrite),
  `docs/spec-dataconsole.md` §4.4 + invariant 7.
- GATE 1 spec writes (executed at approval, per Codex Rev-1 finding 3 — every governing
  contract exists before BUILD): (a) `docs/spec-skill-packs.md` promoted from the archive
  with the editing pass vs main's actual `internal/skillpacks/` (S3 then implements against
  it, code+tests only); (b) `spec-welcome-mode.md` §11 gains the Agents-view title-button
  deletion line (owner ruling); (c) §5.4 gains the workspace-trust-dialog note (P1 live
  finding: dialogs precede the seeded turn; the turn survives them).
- LAND spec amendments (queued): W10–W15 "Pinned by" column gets the landed suite names.
- Invariants → planned pins: W10 `launch_gate.test.js` · W11/W12 `command_channel.test.js` ·
  W13 `receiver_lifecycle.test.js` (P2 measured chains as fixtures) · W14 launch-suite
  post-dispatch cases · W15 `panel_a11y.test.js` · W3 renamed
  `TestInstallBootstrap_WritesStartupPolicyFromZeropsSubdomain` + welcomejs suppress suite ·
  W7 ported picker suite + `TestPackSet_*`.
- CLAUDE.md trap line (≤1): none — every candidate trap is spec-row + test-pinned;
  re-evaluate at LAND.
- This plan dir → `plans/archive/` on LAND close.
