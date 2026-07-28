# Slice brief: S6d — FE: wizard UI + drain rewire

Self-contained. Cite spec §s, never the plan.
Repo: /Users/macbook/Documents/Zerops-MCP/frontend-legacy, branch `feat/agent-first-onboarding`.
Depends: S6c approved. Contract home:
`/Users/macbook/Documents/Zerops-MCP/zcp/docs/spec-welcome-mode.md` — never copy contract
prose into this repo.

**FE-repo protocol (binding)**: ≤5 files; owner-present; `npx tsc --noEmit` +
`npx eslint . --quiet` + jest targets; re-read files before editing.

**Outcome** (observable): the wizard component renders the S6c service's states
(pick roster → auth-in-progress → launching → failure-with-Continue), proven via TestBed,
and the claim drain raises the wizard at `claiming` instead of the dumb cover. The live
MOUNT into `app.container` is deliberately S6e's (this phase's component is exercised by
its spec only).

**Allowed scope** — exactly 4 files:
- NEW `apps/zerops/src/modules/core/zcp-pool-claim-base/zcp-onboard-wizard.component.ts`
  (standalone component, inline template + styles — one file; name deliberately avoids the
  existing unrelated `zerops-onboarding` feature, which is the project-creation flow)
- NEW `apps/zerops/src/modules/core/zcp-pool-claim-base/zcp-onboard-wizard.component.spec.ts`
- `apps/zerops/src/modules/core/zcp-pool-claim-base/zcp-pool-claim-base.effect.ts`
- NEW `apps/zerops/src/modules/core/zcp-pool-claim-base/zcp-pool-claim-base.effect.spec.ts`
Excluded: `app.container.*` (the mount is S6e's); the old `zcp-claim-overlay.component.ts`
stays as-is unless dead — if it becomes fully unreferenced, note it for the owner; do NOT
delete it in this phase (file budget).

**Spec citations**: `spec-welcome-mode.md` §8 (layer over the claim skeleton; embed prewarms
+ announces BEHIND the layer; announce never blocks a wizard step), §8.1 (pick/auth/launch/
failure semantics; single Continue closing wizard layer AND code-server overlay, landing on
project detail; no retry button, no silent auto-reveal), §8.3 (roster renders immediately
from FE data — S6a parser; announce confirms/refreshes, never waited on).

**Facts**:
- Drain: `onFullyAuthedNavigateToZcp$` `zcp-pool-claim-base.effect.ts:64-119` — cookie
  `claimZcpPool` 600 s (util.ts:9-28), gate :66-67, cover :68, stack resolve
  (`isControlPlaneService`) :73-77, `-zagent` userData subscribe :87-98, arrival wait
  :99-105, cookie clear + `prewarm(stack.id)` :108-113, navigate :114. KEEP the tail; end
  raising the wizard at `claiming` instead of `show()`-ing the dumb cover. KEEP
  `ZCP_CLAIM_NAVIGATE_TIMEOUT` (:24) and its fallback path.
- The wizard component is presentation over the S6c service's signals — no own state
  machine, no bridge access (the service owns both). Failure state renders the single
  **Continue** (§8.1): closes wizard layer + the code-server overlay, lands on project
  detail.
- Copy voice: first-time-developer perspective; no internal vocabulary.

**RED test list**: effect spec — drain raises wizard at `claiming` (not the old cover),
cookie semantics unchanged, timeout fallback intact · component spec
(`zcp-onboard-wizard.component.spec.ts`, TestBed) — rendering per state: roster from parser
output, auth phase copy, launching state, failure + single-Continue wiring.

**Protocol**: RED → GREEN → REFACTOR → tsc + eslint → report → owner approval.

**BUILD addendum**: one named test at a time · independent oracle (spec literals) · assert
on public seams · lint/typecheck clean before done.

**Report contract**: RED + GREEN outputs with exit codes · files touched (exactly these 4) ·
tsc/eslint pass lines · independent-oracle note.

**Stop conditions**: scope drift (5th file) · material unknown · AC change · repeated
unexplained failure.

**Definition of Done**: RED replay valid · named specs pass · tsc + eslint clean · report
filled.
