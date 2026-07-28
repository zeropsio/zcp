# Slice brief: S6c — FE: wizard service + state machine + dismissal-machinery deletion

Self-contained. Cite spec §s, never the plan.
Repo: /Users/macbook/Documents/Zerops-MCP/frontend-legacy, branch `feat/agent-first-onboarding`.
Depends: S6b approved. Contract home:
`/Users/macbook/Documents/Zerops-MCP/zcp/docs/spec-welcome-mode.md` — never copy contract
prose into this repo.

**FE-repo protocol (binding)**: ≤5 files; owner-present; `npx tsc --noEmit` +
`npx eslint . --quiet` + jest targets; re-read files before editing.

**Outcome** (observable): the claim-overlay service is evolved into the wizard service
(`ZcpOnboardWizardService`) implementing the §8.1 state machine with the queued launch
intent + 30 s timer; the 3 s/45 s/`zcp-vscode-ready` dismissal machinery is deleted.

**Allowed scope** — exactly 3 files, under `apps/zerops/src/modules/core/zcp-pool-claim-base/`:
- `zcp-claim-overlay.service.ts` — evolves in place; class renamed
  `ZcpOnboardWizardService`. FILE rename is explicitly DEFERRED (it would cascade imports
  past the 5-file cap) — leave a one-line comment noting the pending rename; the owner
  decides at LAND.
- NEW `zcp-claim-overlay.service.spec.ts`
- `index.ts` (export the renamed class)
Excluded: the wizard's visual component (S6d), effects (S6d), bridge files (done in S6b).

**Spec citations**: `spec-welcome-mode.md` §8.1 (state machine, one-shot, failure), §4.3
(same-`eventId` retry + fresh `createdAt`; `set-mode "onboarding"` before launch on every
announce), §8.3 (signals service in root; `ev.source` retained here; Window refs out of the
store), §8 (deletions).

**Facts**:
- Current service (`zcp-claim-overlay.service.ts`): signals `#visible` :31-32,
  `#prewarmServiceStackId` :39-40; API `prewarm` :46, `show` :50, `hide` :57,
  `notifyIframeLoaded` :68. DELETE: `ZCP_CLAIM_IFRAME_FALLBACK_MS = 3000` (:17, armed :71),
  `ZCP_CLAIM_OVERLAY_MAX_MS = 45000` (:11, armed :53), `ZCP_VSCODE_READY_MESSAGE` (:7,
  listener :74-90 — it validates NOTHING; never carry the pattern forward). Prewarm/show
  survive (the wizard renders over the prewarming embed).
- State machine (§8.1): `claiming→picking→authorizing→launching→done|failed`. Single-select;
  no multi-auth queue; no roster editing. "Skip for now" from `picking` only → standard-mode
  directive (immediate if an embed address is retained, else on next `embed-ready`) → layer
  drops, embed stays. Auth = dispatch the existing dialog (`manualOpen`) over the layer;
  completion observed from the FE's own store; `ok:false` → `failed`. Launch: on auth
  completion mint intent (fresh UUIDv4 `eventId`), enter `launching`, start the 30 s intent
  timer; the intent is a QUEUED send (auth can complete before any announce); on EVERY
  announce while `launching`: `set-mode "onboarding"` first, then launch/retry with the SAME
  `eventId` + fresh `createdAt`. `done` on `agent-ready`. `launch-failed` (any reason) and
  timer expiry converge on ONE failure state (single Continue semantics — the UI wires it in
  S6d). Roster refresh removing the picked agent → back to `picking`.
- One-shot (§8.1): wizard raises only on explicit entry; never derived from
  authorized-agents state; abandonment deliberately unhandled; no prompted-launch state.
- `ev.source` + origin retained HERE, fed by S6b's callback surface (§8.3).

**RED test list** (new service spec): full state walk `claiming→…→done` · queued intent
fires on first announce when auth completed early · retry reuses eventId with fresh
createdAt · 30 s expiry → `failed` · `launch-failed` → `failed` · Skip-for-now → standard
directive + layer down · roster refresh removing picked agent → `picking` · no dismissal
timers remain (grep-level assertion in spec).

**Protocol**: RED → GREEN → REFACTOR → tsc + eslint → report → owner approval.

**BUILD addendum**: one named test at a time · independent oracle (spec literals) · assert
on public seams · lint/typecheck clean before done.

**Report contract**: RED + GREEN outputs with exit codes · files touched (exactly these 3) ·
tsc/eslint pass lines · independent-oracle note.

**Stop conditions**: scope drift (4th production file — e.g. an import cascade from the
class rename beyond index.ts) · material unknown · AC change · repeated unexplained failure.

**Definition of Done**: RED replay valid · named specs pass · grep
`zcp-vscode-ready|ZCP_CLAIM_IFRAME_FALLBACK|ZCP_CLAIM_OVERLAY_MAX` → zero hits · tsc +
eslint clean · report filled.
