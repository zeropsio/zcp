# z3 UI foundations — run register

Transient. One row per slice, updated at every event. Programme: `z3-ui-foundations-2026-08-30.md`
(the plan), `z3-ui-foundations-orchestrator-2026-08-30.md` (the brief). Fork `main` starts the
programme at `3f58549a5` (W0 commits on top of the CI-green `089a0e007`).

## Slices

| id | wave | workspace | branch | codex out-file(s) | launched | finished | gate (L1) | level | verdict | merged sha | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|
| W0-CI | 0 | — | main | — | — | 2026-08-30 | CI `089a0e007` success (public API) | — | green | — | clean-checkout diagnostic folded into W1-R3 (own commit) |
| W0-DS | 0 | — | main | — | — | 2026-08-30 | — | — | done | `3f58549a5` | `docs/internals/zerops/design-system.md` |
| W0-FORK | 0 | — | main | — | — | 2026-08-30 | — | — | done | `8a5b27aab` | fork.md §3 + CLAUDE.md zone row + README file table + AGENTS.md §3/§4 + connection-modes row |
| W0-REG | 0 | — | — | — | — | 2026-08-30 | — | — | done | — | this file; `.orchestrator/` in `.git/info/exclude`; `../z3-wt` exists |
| W0-OWNER | 0 | — | — | — | — | 2026-08-30 | — | — | sent | — | four §6 questions with defaults; defaults proceed |
| W1-EXC | 1 | `z3-wt/W1-EXC` | `wt/W1-EXC` | `/tmp/codex-out-1788089675-63778-27621.md` (78 execs) | 13:52 | 15:07 | L1 green (frozen install, 29/29, typechecks, lint, fmt, driver exit 0) | L2 | L2 running | | marker `T3CODE_GUARD_FINDING:{json}`; driver `scripts/check-guard-exceptions.ts --rule <r>`; plugin gains `exports: ./exceptions`; scripts depends on the plugin |
| W1-R2 | 1 | retired | `wt/W1-R2`, `wt/W1-R2-fix` (deleted) | impl `…-1788088807-58030-11004.md`; fu1 `…-1788090601-80697-9159.md`; L3 `…-1788089792…`, L3r2 `…-1788091245…`; fix `…-1788092089-30167-110.md` | 13:20 | 15:52 (fix) | L1 green; fix gated with CI's own `vpr typecheck` (0 errors) | L3 | L2 r2 MERGE; L3 r2 minor accepted; first merge `4f367e112` reverted (`17e011fdc`) after CI's Typecheck step; re-landed via `W1-R2-fix` | `beb3f683d` (re-land merge; the original 3 commits + one typing fix) | pushed 15:53; zone test 21 rows; Z3F-7 test name + `design-system.md` R2 status pending at wave end |
| W1-MAN | 1 | retired | `wt/W1-MAN` (deleted) | impl `…-1788089682-65494-27297.md`; fu1 `…-1788091055-96951-1370.md` | 13:52 | 15:22 (fu1) | L1 green ×2 | L2 | L2 r2 MERGE (three nits noted in the review) | `f5e811782` | not yet pushed (goes with the next push) |
| W1-D-LEGACYSB | 1 | retired | `wt/W1-D-LEGACYSB` (deleted) | impl; fu1; fu2 | 13:26 | 15:38 (fu2) | L1 green ×3 | L2 | L2 r3 MERGE | `4e0ab02b8` | 16 files, +30/−3,763; unpushed |
| W1-D-MOBLIST | 1 | retired | `wt/W1-D-MOBLIST` (deleted) | impl; fu1 | 13:26 | 15:28 (fu1) | L1 green ×2 | L2 | L2 r2 MERGE | `338fd68f7` | 25 files; unpushed |
| W1-D-WORKTREE | 1 | `z3-wt/W1-D-WORKTREE` | `wt/W1-D-WORKTREE` | impl `…-1788089679-64958-34.md`; L3 `…-1788090900…`; fu1 `…-1788091865-17441-9353.md`; L3 r2 running | 13:52 | 16:14 (fu1) | L1 green ×2 (fu1: 227 tests, 6 typechecks, lint, fmt) | L3 | L2 + L3: SEND BACK → fu1 (mobile clamp before validate/submit/enqueue; RN-free `workspaceMode.ts` used by provider + screen; every-member capability for grouped settings; effective-mode dirty checks; one shared clamp; real worktree path never hidden); L2 r2 + L3 r2 running | | 20 files |
| W1-D-MISC | 1 | `z3-wt/W1-D-MISC` | `wt/W1-D-MISC` | `/tmp/codex-out-1788089185-60786-16753.md` (45 execs) | 13:26 | 14:05 | L1 green (15/15, web typecheck, lint, release-smoke, lock; lockfile −73 lines = @vercel/config) | L1 | MERGE | `56000d5ce` (3 commits) | residue: `.vercel/project.json` git-test fixtures (unrelated), one T3 Connect sentence in `docs/operations/mobile-app-store-screenshots.md:13` (outside scope — docs pass later) |
| W1-D-STAGE | 1 | retired | `wt/W1-D-STAGE` (deleted) | impl; fu1; fu2 | 13:26 | 15:35 (fu2) | L1 green (fu2); re-gate after `merge main` 149 tests + build | L2 | L2 r2 fmt-only → MERGE | `3f6129018` | 21 files; two conflicts with LEGACYSB resolved (both sides deleted); unpushed |
| W1-D-PR | 1 | `z3-wt/W1-D-PR` | `wt/W1-D-PR` | impl `…-1788089677-64318-32425.md` (152 execs); L3 `…-1788091763-15771-32494.md`; fu1 running | 13:52 | 15:25 | L1 green | L3 | L2 + L3: SEND BACK — mobile static link fabricated `state:"open"` (feeds auto-settle); `stopPropagation` now fires for every markdown link; dead `_threadRef`/helpers; orphans `githubGraphQlBudget.ts`, most of `SourceControlRateLimit.ts`, `useLiveRefresh.ts`; `thread-sidebar.md:13–15` promises settle-on-merge for a linked PR; RED evidence not tool output — fu1 sent 16:27 | | `diffFileContents.ts` deviation confirmed right by L2; `server.test.ts` 147 green at L2 |
| W1-R3 | 1 | | | | | | | L2 | | | needs W1-EXC |
| W1-R4 | 1 | | | | | | | L2 | | | needs W1-EXC |
| W1-R6 | 1 | | | | | | | L2 | | | needs W1-EXC |
| W1-D-NAMING | 1 | | | | | | | L2 | | | needs W1-R4 |

## Exception ledgers

| rule | entries | `never` | as of |
|---|---|---|---|
| R3 | — | — | not landed |
| R4 | — | — | not landed |
| R6 | — | — | not landed |

## Wave status

- **Wave 0** — done 2026-08-30. CI green on `main` (`089a0e007`); W0 commits `8a5b27aab`, `3f58549a5` (not yet pushed — pushed at the end of wave 1 with the first merges, or earlier if wave 1 stalls).
- **Wave 1** — in flight. Merged so far: MISC `56000d5ce`, MAN `f5e811782`, R2 re-land `beb3f683d` (pushed 15:53; CI pending), LEGACYSB `4e0ab02b8`, MOBLIST `338fd68f7`, STAGE `3f6129018`. R2's first merge `4f367e112` was reverted (`17e011fdc`) after CI's `Typecheck` failed on `scripts/`. All six pushed at 16:12 (`3f6129018`, repo-wide `vpr typecheck` 0 errors first); **CI green on `3f6129018`** (16:32). Still open in wave 1: PR (fu1 running), WORKTREE (L2/L3 r2 running), EXC (fu1 running) → then R3/R4/R6 → NAMING. Research phase 13:04–13:25 (six read-only Claude researchers; fact sheets in the session scratchpad). Codex runs: R2 + LEGACYSB done and gated green (reviews running); MOBLIST, MISC, STAGE, EXC, PR, WORKTREE, MAN running (+ the R2 L3 review = 8 Codex slots). Then W1-R3/R4/R6 as W1-EXC merges; W1-D-NAMING as W1-R4 merges.

## Decisions taken by the orchestrator (schedule-level; design ones go to `design-system.md §6`)

- **W1-D-STAGE split out of W1-D-MISC**: `SidebarStageBackdrop` is the visual half of the `environmentIdentificationMode` setting (contract field, Settings control, hook chain with a theme gate, 145 CSS custom properties, `docs/user/thread-sidebar.md` section), not a component with art assets. Its own L2 slice deletes the whole feature; F4-FONTS's "artwork row deleted" item is thereby done early. `APP_STAGE_LABEL` stays (window title).
- **W1-D-MISC scope**: `vercel.ts` is not a leaf — the About panel's channel switch still built the router's `/__t3code/channel` URL; both go. Docs pruning = T3 Connect sections + `docs/internals/t3-connect.md`/the HTML flow (+ `remote.md` per its content) + the Vercel chapter of `release.md`. The `npx t3`/installer positioning in `docs/user` is a later product-docs rewrite, not F2.6.
- **W1-D-MOBLIST follows the chain into `homeListItems.ts` / `home-list-options.ts` / `home-list-filter-menu.ts`** (dead once the legacy branches go) and deletes `threadPresentation.ts` (only the legacy list imported it). Consequence for W2-F3-STATUS: the mobile status consumer is `threadListV2.ts`'s resolver + `thread-list-v2-items.tsx`, not `threadPresentation.ts`; its 8 colour literals leave with the file (R3 baseline shrinks).
- **R2 / the shared connection runtime's presence write** (L3 + L2 findings, 14:10–14:50): every web atom family — every protected root included — reaches `apps/web/src/connection/runtime.ts`, which installs `lib/backgroundActivityReporter.ts`, which calls `serverReportClientActivity` every 25 s. It is allowed in zone rule 6 because the server AUTHORS it as `AuthOrchestrationReadScope` (`RpcAuthorization.ts:52,92,93`, together with `subscribeResourceTelemetry`/`subscribeVcsStatus`) — the same authored partition the Zerops read/allowed sets mirror; the constant is named for that and cites those lines. `requestLatencyState.ts`'s operate-scope tokens are `Set` members for latency classification, never calls — a separate named exception. The reporter is not split out of the runtime. Recorded in `design-system.md §6` at wave end.
- **L1 typechecks every package that has a touched file** (CI red on R2, 15:38): the R2 gate ran only the web typecheck while the slice's main change lives in `scripts/`; `vp test run` never typechecks, so a test file with an `any`-inferring recursion passes vitest and fails `tsgo`. From now on the L1 gate derives the package set from `git status` and runs `vp run --filter <pkg> typecheck` for each; the post-merge check does the same.
- **Acceptance greps and absence tests** (LEGACYSB L2, 14:47): a brief whose residue grep must come back empty invites a test that spells the retired name from fragments. Rule for every later brief: the residue grep excludes the test rows that pin the absence, and those rows use the literal names.
- **W1-D-PR / DN1b**: the linked-PR badge's live refresh used `pullRequests.detail` (delete set). DN1b's default "keep as a read-only link" is implemented literally: number + url from `thread.linkedPullRequest`, no live status; the unlinked git-status path (`status.pr`) is untouched.

- **W1-D-PR is one full-stack slice**, not a server half now and a client half in wave 2. The catalogue's split cannot keep every commit compiling: the server's RPC handler layer is typed against the contract's PR RPC group (handlers must be total), so the server half cannot drop handlers without dropping the contract definitions, and dropping those breaks `client-runtime/state/pull-requests` → web `components/pullRequest/**` → typecheck of web, which the server half's acceptance demands. Commit order inside the slice is top-down (web + mobile consumers → client-runtime state → contracts + server together), each commit green for every package. "Server half first" survives as the test focus (projection/reducer compatibility), not as slice order. DN1b default holds: the thread's external PR reference stays.
- **The clean-checkout diagnostic** (untracked package shells) rides in W1-R3 as its own commit — see `design-system.md §6`.

## Blockers

- none (R2 goes back for a follow-up; not a blocker for other slices)

## Codex health (per run: `grep -c "^exec$"`, rate-limit / AMBIGUOUS lines)

| run | execs | rate limit | AMBIGUOUS | helper diag |
|---|---|---|---|---|
