# Plan — Production welcome screen (ship-dark, command-invoked)

**Date:** 2026-07-16 · **Owner:** Karel (approved: "kompletně dokončit") · **Branch:** `feat/vscode-welcome-production`
**Inputs:** PRD v6 + functional prototype on `mock/vscode-welcome-onboarding` · sendMessage auth-bridge prototype in `frontend-legacy` (`prototype/zcp-claude-auth-bridge`) · Codex gpt-5.6-sol adversarial review (5 blockers folded in).
**Durable contract:** `docs/spec-welcome-mode.md` (this plan is transient; the spec wins on conflict).

## Pattern

The welcome screen ships as a **dormant lazy module of the existing `zcp-bootstrap` code-server extension**, opened ONLY by the command `zerops.welcome` ("Zerops: Get Started"). Every mutating action delegates to an existing owner: agent login → the Zerops GUI's existing auth dialog (triggered via a credential-free `postMessage` bridge with ACK), guided → `zcp init --guided`, platform writes → Go (`zcp agent mark-oauth`), skills → embedded curated templates. The extension never holds secrets, never parses TUIs, never calls the platform from JS.

## Deltas vs PRD v6 (mock branch)

| PRD v6 | Now | Why |
|---|---|---|
| `ZCP_WELCOME` env gate, welcome auto-opens as primary onboarding | Command-only, ship-dark; auto-open is a future switch | Karel 2026-07-16: deployable but never shown |
| Tier B: node-pty AuthRunner + login-regex registry in the extension | Bridge-first: postMessage trigger → existing frontend dialog; Tier-A terminal fallback | sendMessage discovery (frontend-legacy prototype); keeps the login registry single-owner in the frontend |
| No guided step (guided didn't exist yet) | Featured "Zerops Guided" toggle → `zcp init --guided` + static explainer | guided shipped meanwhile (`docs/spec-guided-mode.md`) |
| Community skill packs (whole GitHub repos) | ~5 embedded curated skills; community packs deferred to v2 | supply-chain / prompt-injection; no trust/update story |
| Boolean auth union (`flag || cred`) | 5-state matrix incl. `Reconnect` (flag without cred) | union cannot represent orphaned flags (Codex blocker) |

## Phases (Codex-reordered; live checkpoints inline, not deferred to the end)

| # | Scope | Gate |
|---|---|---|
| P0 | Spec + version parity 0.1.3 + versioned immutable install dirs + atomic `extensions.json` switch + same-version no-op + reload notice | parity pin; complete-tree install; no-op; upgrade-from-old-dir keeps old dir intact; live bump→reload proof (P7 confirms) |
| F1 | frontend-legacy: commit the (currently uncommitted!) bridge receiver; add ACK replies (accepted / unsupported-agent / silent-on-invalid); pin dev origin | receiver validation preserved; ACK provable in the local harness |
| P1 | `contributes.commands` + lazy `welcome.js` (dependency-injected: registry/state-reader/launcher — no second copy of pinned commands) + singleton persistent panel + static bento shell + JS test harness + CI node pin | no welcome code/watchers/panel at activation or env change; 2nd invocation reveals; load failure leaves launcher healthy |
| P2 | state matrix + ready handshake + watchers (missing-dir tolerant, rename-aware, debounced, panel-scoped) | full matrix incl. Reconnect; no secrets in state/DOM |
| P3 | bridge sender (payload pin, build-time origin allowlist, ACK timeout → fallback hint, direct-code-server first-class) + Tier-A (claude, codex) + `zcp agent mark-oauth` (enum-only, via ops) | one login in flight; unsupported agents route to the Zerops panel |
| P4 | guided toggle (fixed argv spawn, selected workspace folder, multi-root ask, authoring/no-workspace disable, one-flight lock, dirty-file check, exit-code + marker re-read, honest partial-failure) + static explainer | spawn/cwd pins; failure honesty |
| P5 | curated skills: content (no placeholders, provenance) + installer (slug allowlist, `guided` reserved, containment/symlink guards, atomic create, absent/current/modified via hash, modal Replace) | installer safety table |
| P6 | CTA (explicit agent selection, clipboard-first kickoff) + copy + a11y + diagnostics tile + spec finalization + CLAUDE.md map line | unknown-message reject; keyboard/focus |
| P7 | live gate on a FRESH isolated service in `zcp-eval-clean` (never the shared `zcp` service — parallel data-console work + possibly the 2026-06-24 hand-patch): upgrade, window reload, restart/rebuild survival, bridge E2E + fallback, guided, skills, non-Zerops invocation | all green → release-ready |

## Coexistence

Zero mandatory-file overlap with `feat/managed-data-console` (it owns `internal/dataconsole/**`, `cmd/zcp/studio*.go`, `internal/init/{vscode.go,adapters/studio*.go}`); the only shared touch is `cmd/zcp/main.go` for `zcp agent` dispatch — trivial add-a-case conflict, resolve on whichever branch merges second.

## v1 cuts (deliberate)

Bridge for `claude-code` only (receiver allowlist) · Tier-A only for live-verified agents (claude, codex; antigravity/grok/cursor tiles say "authorize in the Zerops panel") · static guided explainer, no configurable-looking axes · embedded reviewed skills only · external video link only (no embed) · clipboard-first CTA (no blind delayed prompt send) · no webview serializer (dark contract) · no auto-open path at all.
