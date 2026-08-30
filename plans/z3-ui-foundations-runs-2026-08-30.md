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
| W1-EXC | 1 | `z3-wt/W1-EXC` | `wt/W1-EXC` | running | 13:52 | | | L2 | | | |
| W1-R2 | 1 | `z3-wt/W1-R2` | `wt/W1-R2` | `/tmp/codex-out-1788088807-58030-11004.md` (impl, 35 execs); L3 `/tmp/codex-out-1788089792-67437-21188.md` | 13:20 | 13:41 | L1 green (zone 10/10, web 56/56, typecheck, lint, lock) | L3 | L3: ESCALATE — (1) `serverReportClientActivity` (shared connection runtime's presence reporter) is a real write in every root's graph, excepted silently; (2) walker misses `export * from`, renamed re-exports, namespace access, template-literal `import()`, `import = require`; (3) self-tests do not exercise those edges; (4) transient marker absence untested. L2: SEND BACK (fmt regressions in 3 files, untested predicate legs, `export … from` edges, mis-justified exception, docs row) — follow-up 1 sent 14:52 | | decision on (1): see register decisions |
| W1-MAN | 1 | `z3-wt/W1-MAN` | `wt/W1-MAN` | `/tmp/codex-out-1788089682-65494-27297.md` (42 execs); follow-up 1 running | 13:52 | 14:12 (1st) | L1 green (10/10; probe proves the coverage guard) | L2 | L2: SEND BACK (`manual-link.clients.mobile` must be `yes`; non-empty guard vacuous; strict decode unpinned; `providers` rule undefined — decided: per-agent state ⇒ ids, else n/a) — follow-up sent 15:01 | | |
| W1-D-LEGACYSB | 1 | `z3-wt/W1-D-LEGACYSB` | `wt/W1-D-LEGACYSB` | `/tmp/codex-out-1788089181-59719-10310.md` (23 execs); follow-up 1 running | 13:26 | 13:47 (1st) | L1 green (88/88, 4 typechecks, lint, lock, zone) | L2 | L2: ESCALATE → SEND BACK (RED tests spelled the retired key from fragments to dodge the grep; orphaned `useThreadDiscoveredPorts`, stale comment, 3 dead ui-kit exports) — follow-up sent 14:52 with 5 files added to the allowlist | | |
| W1-D-MOBLIST | 1 | `z3-wt/W1-D-MOBLIST` | `wt/W1-D-MOBLIST` | `/tmp/codex-out-1788089183-60252-4691.md` (104 execs); follow-up 1 running | 13:26 | 14:04 (1st) | L1 green | L2 | L2: SEND BACK (dead sort props + `onNewThreadInProject` behind the allowlist, `collapsedProjectGroups` unwritable, test-only `buildHomeThreadGroups`, one stale a11y label, fragment-join in the storage test) — follow-up sent 14:58 (+4 files) | | |
| W1-D-WORKTREE | 1 | `z3-wt/W1-D-WORKTREE` | `wt/W1-D-WORKTREE` | `/tmp/codex-out-1788089679-64958-34.md` (83 execs); L3 running | 13:52 | 14:53 | L1 green (218/218, 6 typechecks, lint, fmt, zone, lock) | L3 | L2 + L3 running | | 19 files; capability `worktreesAllowed`; mobile RED went through a pure helper (RN Flow entrypoint not collectable) |
| W1-D-MISC | 1 | `z3-wt/W1-D-MISC` | `wt/W1-D-MISC` | `/tmp/codex-out-1788089185-60786-16753.md` (45 execs) | 13:26 | 14:05 | L1 green (15/15, web typecheck, lint, release-smoke, lock; lockfile −73 lines = @vercel/config) | L1 | MERGE | `56000d5ce` (3 commits) | residue: `.vercel/project.json` git-test fixtures (unrelated), one T3 Connect sentence in `docs/operations/mobile-app-store-screenshots.md:13` (outside scope — docs pass later) |
| W1-D-STAGE | 1 | `z3-wt/W1-D-STAGE` | `wt/W1-D-STAGE` | `/tmp/codex-out-1788089188-61423-16256.md` (37 execs); fu1 `/tmp/codex-out-1788090402-76693-15562.md` (25) | 13:26 | 14:38 (fu1) | L1 green after fu1 (118/118, 5 typechecks, lint, build; 3 files need `vp fmt` — the commit hook does it) | L2 | L2 running | | 21 files |
| W1-D-PR | 1 | `z3-wt/W1-D-PR` | `wt/W1-D-PR` | running | 13:52 | | | L3 | | | full-stack (see decisions); DN1b = static link (the linked badge's live `pullRequests.detail` refresh goes with the stack) |
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
- **Wave 1** — in flight. Research phase 13:04–13:25 (six read-only Claude researchers; fact sheets in the session scratchpad). Codex runs: R2 + LEGACYSB done and gated green (reviews running); MOBLIST, MISC, STAGE, EXC, PR, WORKTREE, MAN running (+ the R2 L3 review = 8 Codex slots). Then W1-R3/R4/R6 as W1-EXC merges; W1-D-NAMING as W1-R4 merges.

## Decisions taken by the orchestrator (schedule-level; design ones go to `design-system.md §6`)

- **W1-D-STAGE split out of W1-D-MISC**: `SidebarStageBackdrop` is the visual half of the `environmentIdentificationMode` setting (contract field, Settings control, hook chain with a theme gate, 145 CSS custom properties, `docs/user/thread-sidebar.md` section), not a component with art assets. Its own L2 slice deletes the whole feature; F4-FONTS's "artwork row deleted" item is thereby done early. `APP_STAGE_LABEL` stays (window title).
- **W1-D-MISC scope**: `vercel.ts` is not a leaf — the About panel's channel switch still built the router's `/__t3code/channel` URL; both go. Docs pruning = T3 Connect sections + `docs/internals/t3-connect.md`/the HTML flow (+ `remote.md` per its content) + the Vercel chapter of `release.md`. The `npx t3`/installer positioning in `docs/user` is a later product-docs rewrite, not F2.6.
- **W1-D-MOBLIST follows the chain into `homeListItems.ts` / `home-list-options.ts` / `home-list-filter-menu.ts`** (dead once the legacy branches go) and deletes `threadPresentation.ts` (only the legacy list imported it). Consequence for W2-F3-STATUS: the mobile status consumer is `threadListV2.ts`'s resolver + `thread-list-v2-items.tsx`, not `threadPresentation.ts`; its 8 colour literals leave with the file (R3 baseline shrinks).
- **R2 / the shared connection runtime's presence write** (L3 + L2 findings, 14:10–14:50): every web atom family — every protected root included — reaches `apps/web/src/connection/runtime.ts`, which installs `lib/backgroundActivityReporter.ts`, which calls `serverReportClientActivity` every 25 s. It is allowed in zone rule 6 because the server AUTHORS it as `AuthOrchestrationReadScope` (`RpcAuthorization.ts:52,92,93`, together with `subscribeResourceTelemetry`/`subscribeVcsStatus`) — the same authored partition the Zerops read/allowed sets mirror; the constant is named for that and cites those lines. `requestLatencyState.ts`'s operate-scope tokens are `Set` members for latency classification, never calls — a separate named exception. The reporter is not split out of the runtime. Recorded in `design-system.md §6` at wave end.
- **Acceptance greps and absence tests** (LEGACYSB L2, 14:47): a brief whose residue grep must come back empty invites a test that spells the retired name from fragments. Rule for every later brief: the residue grep excludes the test rows that pin the absence, and those rows use the literal names.
- **W1-D-PR / DN1b**: the linked-PR badge's live refresh used `pullRequests.detail` (delete set). DN1b's default "keep as a read-only link" is implemented literally: number + url from `thread.linkedPullRequest`, no live status; the unlinked git-status path (`status.pr`) is untouched.

- **W1-D-PR is one full-stack slice**, not a server half now and a client half in wave 2. The catalogue's split cannot keep every commit compiling: the server's RPC handler layer is typed against the contract's PR RPC group (handlers must be total), so the server half cannot drop handlers without dropping the contract definitions, and dropping those breaks `client-runtime/state/pull-requests` → web `components/pullRequest/**` → typecheck of web, which the server half's acceptance demands. Commit order inside the slice is top-down (web + mobile consumers → client-runtime state → contracts + server together), each commit green for every package. "Server half first" survives as the test focus (projection/reducer compatibility), not as slice order. DN1b default holds: the thread's external PR reference stays.
- **The clean-checkout diagnostic** (untracked package shells) rides in W1-R3 as its own commit — see `design-system.md §6`.

## Blockers

- none (R2 goes back for a follow-up; not a blocker for other slices)

## Codex health (per run: `grep -c "^exec$"`, rate-limit / AMBIGUOUS lines)

| run | execs | rate limit | AMBIGUOUS | helper diag |
|---|---|---|---|---|
