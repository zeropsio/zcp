# Slice brief: S6b — FE: bridge validator fork + listener extension

Self-contained. Cite spec §s, never the plan.
Repo: /Users/macbook/Documents/Zerops-MCP/frontend-legacy, branch `feat/agent-first-onboarding`.
Depends: S6a approved. Contract home:
`/Users/macbook/Documents/Zerops-MCP/zcp/docs/spec-welcome-mode.md` — never copy contract
prose into this repo.

**FE-repo protocol (binding)**: ≤5 files; owner-present; `npx tsc --noEmit` +
`npx eslint . --quiet` + jest targets; re-read files before editing.

**Outcome** (observable): the shipped bridge receiver validates and routes the three new
embed→FE types (`embed-ready`, `agent-ready`, `launch-failed`) through the full existing
validation pipeline; every existing auth-ack behavior is unchanged; outside an active wizard
the FE answers every `embed-ready` with `set-mode "standard"`.

**Allowed scope** — exactly 4 files, all under
`apps/zerops/src/modules/feature/code-server-overlay/`:
- `code-server-overlay.bridge.ts`
- `code-server-overlay.bridge.spec.ts`
- `code-server-overlay.feature.ts`
- `code-server-overlay.feature.html`
Excluded: the auth-dialog feature's internals (consume its actions only); ngrx store shape
for Window refs.

**Spec citations**: `spec-welcome-mode.md` §4.1 (envelope; FE page stamps `createdAt` on its
own sends; skew check stays as defense in depth), §4.3 (types + correlation), §8.3 (listener
home; announce answered outside wizard).

**Facts**:
- The type gate at bridge.ts:153 hard-rejects anything but `open-agent-auth` — fork it into
  type-dispatch; KEEP the whole pipeline (envelope → context → origin → iframe-identity walk
  feature.ts:492-509 → skew bridge.ts:18-19 → TTL :9 → replay :39/:513-519) for every
  inbound type. `agent-ready`/`launch-failed` carry the COMMAND's `eventId` (correlation —
  outcomes have no identity of their own, §4.3).
- The stale comment bridge.ts:10-18 claims `createdAt` is minted on the CONTAINER clock —
  false since the webview re-stamp; delete the comment, keep the skew check.
- Listener stays in `CodeServerOverlayFeature`, outside the Angular zone (:196-203,
  :414-422). Route parsed embed→FE events to a small typed callback surface (the S6c wizard
  service will consume it); `ev.source` + origin are retained by the SERVICE, never in an
  ngrx action (§8.3).
- Announces arrive on every embed boot, wizard or not; the no-active-wizard answer is
  `set-mode "standard"` posted to the retained source with the FE page stamping `createdAt`
  at send time (§4.1/§8.3).

**RED test list** (extend `code-server-overlay.bridge.spec.ts`): `embed-ready` accepted +
routed with payload · `agent-ready` correlated by eventId · `launch-failed` reason surfaced ·
unknown type still ignored · stale/oversized/replayed messages still dropped for new types ·
all 23 existing auth-ack cases stay green.

**Protocol**: RED → GREEN → REFACTOR → tsc + eslint → report → owner approval.

**BUILD addendum**: one named test at a time · independent oracle (spec literals) · assert
on public seams · lint/typecheck clean before done.

**Report contract**: RED + GREEN outputs with exit codes · files touched (exactly these 4) ·
tsc/eslint pass lines · independent-oracle note.

**Stop conditions**: scope drift (5th file) · material unknown · AC change · repeated
unexplained failure.

**Definition of Done**: RED replay valid · named specs + existing 23 cases pass · tsc +
eslint clean · report filled.
