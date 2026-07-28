# Slice brief: S6e — FE: wizard mount + dev entry `?zcpOnboard=1`

Self-contained. Cite spec §s, never the plan.
Repo: /Users/macbook/Documents/Zerops-MCP/frontend-legacy, branch `feat/agent-first-onboarding`.
Depends: S6d approved. Contract home:
`/Users/macbook/Documents/Zerops-MCP/zcp/docs/spec-welcome-mode.md` — never copy contract
prose into this repo.

**FE-repo protocol (binding)**: ≤5 files; owner-present; `npx tsc --noEmit` +
`npx eslint . --quiet` + jest targets; re-read files before editing.

**Outcome** (observable): the wizard component (built + TestBed-proven in S6d) is MOUNTED
live in `app.container` beside the existing overlay mounts, and
`/project/:projectId?zcpOnboard=1` raises it at `picking` for that project's ZCP stack,
strips the param immediately, and is a silent no-op on a project without a ZCP.

**Allowed scope** — exactly 4 files:
- `apps/zerops/src/modules/app/app.container.html` (mount beside the existing overlay
  mounts, :153-159)
- `apps/zerops/src/modules/app/app.container.ts` (standalone-component import)
- `apps/zerops/src/modules/pages/+project-detail/project-detail.effect.ts`
- NEW `apps/zerops/src/modules/pages/+project-detail/project-detail.effect.spec.ts`
Excluded: routes, page, cookie utils — the cookie machinery is never read, written, or
cleared by this path.

**Spec citations**: `spec-welcome-mode.md` §8.2 (whole contract), §8.1 (an
already-authorized picked agent skips straight to `launching`; the queued-send rule covers
auth completing before the fresh embed's announce).

**Facts**:
- The project-detail effect reads NO query params today (routes at
  `project-detail.routes.ts:19-29`). Pattern reference: the `?openIde=1` effect at
  `service-stack-detail-control-plane.page.ts:209-229` (defer + `replaceUrl` strip).
- §8.2 semantics, exactly: strip immediately via `replaceUrl` (a reload must NOT
  re-trigger); raise the wizard at `picking` first; then perform the drain's tail scoped to
  THE ROUTE'S project — `isControlPlaneService` + `projectId` filter, never first-match
  client-wide; subscribe the `-zagent` userData; prewarm (the overlay opens a fresh
  fullscreen embed behind the layer). Pure bypass: `claimZcpPool` cookie untouched. No ZCP
  stack in the project → silent no-op (param stripped, `console.warn` at most). NO
  `isDevMode` gate — identical behavior in local dev, the rig, and production; ships dark
  (no UI, no docs). A second terminal in a running container is an accepted dev consequence.

**RED test list** (new effect spec): param stripped via `replaceUrl` on entry · wizard
raised at `picking` · stack resolve scoped to the route's projectId · silent no-op without
a ZCP stack (param still stripped) · cookie never touched · reload after strip does not
re-trigger.

**Protocol**: RED → GREEN → REFACTOR → tsc + eslint → report → owner approval.

**BUILD addendum**: one named test at a time · independent oracle (spec literals) · assert
on public seams · lint/typecheck clean before done.

**Report contract**: RED + GREEN outputs with exit codes · files touched (exactly these 4) ·
tsc/eslint pass lines · independent-oracle note.

**Stop conditions**: scope drift (5th file) · material unknown · AC change · repeated
unexplained failure.

**Definition of Done**: RED replay valid · named specs pass · tsc + eslint clean · grep
confirms no cookie-util import in the effect's new code path · report filled.
