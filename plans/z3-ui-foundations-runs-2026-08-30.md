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
| W1-EXC | 1 | retired | `wt/W1-EXC` (deleted) | impl; fu1 | 13:52 | 16:31 (fu1) | L1 green ×2; post-merge incl. repo-wide typecheck 0 | L2 | L2 r2 MERGE (LOW: loader failure prints a cause chain; INFO: `GUARD_SCOPE_PATHS` export unread) | `b2a354f0a` (2 commits) | pushed 16:50; **CI green** |
| W1-R2 | 1 | retired | `wt/W1-R2`, `wt/W1-R2-fix` (deleted) | impl `…-1788088807-58030-11004.md`; fu1 `…-1788090601-80697-9159.md`; L3 `…-1788089792…`, L3r2 `…-1788091245…`; fix `…-1788092089-30167-110.md` | 13:20 | 15:52 (fix) | L1 green; fix gated with CI's own `vpr typecheck` (0 errors) | L3 | L2 r2 MERGE; L3 r2 minor accepted; first merge `4f367e112` reverted (`17e011fdc`) after CI's Typecheck step; re-landed via `W1-R2-fix` | `beb3f683d` (re-land merge; the original 3 commits + one typing fix) | pushed 15:53; zone test 21 rows; Z3F-7 test name + `design-system.md` R2 status pending at wave end |
| W1-MAN | 1 | retired | `wt/W1-MAN` (deleted) | impl `…-1788089682-65494-27297.md`; fu1 `…-1788091055-96951-1370.md` | 13:52 | 15:22 (fu1) | L1 green ×2 | L2 | L2 r2 MERGE (three nits noted in the review) | `f5e811782` | not yet pushed (goes with the next push) |
| W1-D-LEGACYSB | 1 | retired | `wt/W1-D-LEGACYSB` (deleted) | impl; fu1; fu2 | 13:26 | 15:38 (fu2) | L1 green ×3 | L2 | L2 r3 MERGE | `4e0ab02b8` | 16 files, +30/−3,763; unpushed |
| W1-D-MOBLIST | 1 | retired | `wt/W1-D-MOBLIST` (deleted) | impl; fu1 | 13:26 | 15:28 (fu1) | L1 green ×2 | L2 | L2 r2 MERGE | `338fd68f7` | 25 files; unpushed |
| W1-D-WORKTREE | 1 | retired | `wt/W1-D-WORKTREE` (deleted) | impl; L3; fu1; L3 r2 | 13:52 | 16:14 (fu1) | L1 green ×2; re-gate after `merge main` 147 tests | L3 | L2 r2 + L3 r2 MERGE | `0285a7011` (4 commits) | 20 files; unpushed |
| W1-D-MISC | 1 | `z3-wt/W1-D-MISC` | `wt/W1-D-MISC` | `/tmp/codex-out-1788089185-60786-16753.md` (45 execs) | 13:26 | 14:05 | L1 green (15/15, web typecheck, lint, release-smoke, lock; lockfile −73 lines = @vercel/config) | L1 | MERGE | `56000d5ce` (3 commits) | residue: `.vercel/project.json` git-test fixtures (unrelated), one T3 Connect sentence in `docs/operations/mobile-app-store-screenshots.md:13` (outside scope — docs pass later) |
| W1-D-STAGE | 1 | retired | `wt/W1-D-STAGE` (deleted) | impl; fu1; fu2 | 13:26 | 15:35 (fu2) | L1 green (fu2); re-gate after `merge main` 149 tests + build | L2 | L2 r2 fmt-only → MERGE | `3f6129018` | 21 files; two conflicts with LEGACYSB resolved (both sides deleted); unpushed |
| W1-D-PR | 1 | `z3-wt/W1-D-PR` | `wt/W1-D-PR` | impl `…-1788089677-64318-32425.md` (152 execs); L3 `…-1788091763-15771-32494.md`; fu1 `…-1788092724-56062-14518.md` (73 execs); L3 r2 running | 13:52 | 14:53 (fu1) | L1 r2: 7 typechecks 0, 34 files / 560 tests green, residue + keep greps clean, zone + lock OK; `server.test.ts` failed a different row on each of two runs under full machine load (`AuthenticationGetterError 404` bootstrap) — flake probe 3× ws / 2× main running | L3 | round 1: SEND BACK (see notes); fu1 report: all six rulings addressed, F8 (`ThreadRowLeadingStatus` non-interactive) + F11 (`PullRequestReviewPosition` stays in contracts barrel) declined with reasons; L2 r2 + L3 r2 running | | `diffFileContents.ts` deviation confirmed right by L2; RED rows now real tool output; `githubGraphQlBudget.ts`, `useLiveRefresh.ts` deleted; 134 paths |
| W1-R3 | 1 | `z3-wt/W1-R3` | `wt/W1-R3` | running | 16:52 | | | L2 | | | cut from `b2a354f0a`; edits `index.ts`, `vite.config.ts`, `ci.yml` (serial merge with R4/R6) |
| W1-R4 | 1 | `z3-wt/W1-R4` | `wt/W1-R4` | running | 16:52 | | | L2 | | | cut from `b2a354f0a` |
| W1-R6 | 1 | `z3-wt/W1-R6` | `wt/W1-R6` | impl (46 execs); L3 running | 16:52 | 14:53 | L1 green: plugin + scripts typecheck 0, 12 files / 155 tests, `check-css-motion` reconciled (5), lint/fmt/lock OK, zone 21; guard driver exit 1 with exactly 3 unlisted findings in `_chat.pull-requests.tsx` + `PullRequestListFilters.tsx` (deleted by W1-D-PR) | L2 | L2 + L3 running | | cut from `b2a354f0a`; **merge only after W1-D-PR** (else CI's new reconcile step is red on main); ledger 24 = 3 F5b (2 mobile `withRepeat(-1)` without reduced-motion guard, strip Spinner) + 21 never; kinds 2 Call / 1 JSX / 16 Literal / 5 css-declaration; zone test edit = one comment line |
| W1-D-NAMING | 1 | | | | | | | L2 | | | needs W1-R4 |

## Exception ledgers

| rule | entries | `never` | as of |
|---|---|---|---|
| R3 | — | — | not landed |
| R4 | — | — | not landed |
| R6 | — | — | not landed |

## Wave status

- **Wave 0** — done 2026-08-30. CI green on `main` (`089a0e007`); W0 commits `8a5b27aab`, `3f58549a5` (not yet pushed — pushed at the end of wave 1 with the first merges, or earlier if wave 1 stalls).
- **Wave 1** — in flight. Merged so far: MISC `56000d5ce`, MAN `f5e811782`, R2 re-land `beb3f683d` (pushed 15:53; CI pending), LEGACYSB `4e0ab02b8`, MOBLIST `338fd68f7`, STAGE `3f6129018`. R2's first merge `4f367e112` was reverted (`17e011fdc`) after CI's `Typecheck` failed on `scripts/`. All six pushed at 16:12 (`3f6129018`, repo-wide `vpr typecheck` 0 errors first); **CI green on `3f6129018`** (16:32). WORKTREE `0285a7011` merged 16:44, EXC `b2a354f0a` merged + pushed 16:50 (repo-wide typecheck 0). Second batch launched 16:52: R3, R4, R6 (three Codex slots) — then NAMING after R4. Still open from the first batch: PR (fu1 done 14:53, re-gating; R6 done 14:53, gated green, reviews running — R6 lands after PR). Research phase 13:04–13:25 (six read-only Claude researchers; fact sheets in the session scratchpad). Codex runs: R2 + LEGACYSB done and gated green (reviews running); MOBLIST, MISC, STAGE, EXC, PR, WORKTREE, MAN running (+ the R2 L3 review = 8 Codex slots). Then W1-R3/R4/R6 as W1-EXC merges; W1-D-NAMING as W1-R4 merges.

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
