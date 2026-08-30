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
| W1-EXC | 1 | `z3-wt/W1-EXC` | `wt/W1-EXC` | | | | | L2 | | | |
| W1-R2 | 1 | `z3-wt/W1-R2` | `wt/W1-R2` | | | | | L3 | | | |
| W1-MAN | 1 | `z3-wt/W1-MAN` | `wt/W1-MAN` | | | | | L2 | | | |
| W1-D-LEGACYSB | 1 | `z3-wt/W1-D-LEGACYSB` | `wt/W1-D-LEGACYSB` | | | | | L2 | | | |
| W1-D-MOBLIST | 1 | `z3-wt/W1-D-MOBLIST` | `wt/W1-D-MOBLIST` | | | | | L2 | | | |
| W1-D-WORKTREE | 1 | `z3-wt/W1-D-WORKTREE` | `wt/W1-D-WORKTREE` | | | | | L3 | | | |
| W1-D-MISC | 1 | `z3-wt/W1-D-MISC` | `wt/W1-D-MISC` | | | | | L1 | | | |
| W1-D-PR | 1 | `z3-wt/W1-D-PR` | `wt/W1-D-PR` | | | | | L3 | | | full-stack (see decisions) |
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
- **Wave 1** — starting. First batch (needs W0 only, 8 slots): W1-EXC, W1-R2, W1-MAN, W1-D-LEGACYSB, W1-D-MOBLIST, W1-D-WORKTREE, W1-D-MISC, W1-D-PR. Then W1-R3/R4/R6 as W1-EXC merges; W1-D-NAMING as W1-R4 merges.

## Decisions taken by the orchestrator (schedule-level; design ones go to `design-system.md §6`)

- **W1-D-PR is one full-stack slice**, not a server half now and a client half in wave 2. The catalogue's split cannot keep every commit compiling: the server's RPC handler layer is typed against the contract's PR RPC group (handlers must be total), so the server half cannot drop handlers without dropping the contract definitions, and dropping those breaks `client-runtime/state/pull-requests` → web `components/pullRequest/**` → typecheck of web, which the server half's acceptance demands. Commit order inside the slice is top-down (web + mobile consumers → client-runtime state → contracts + server together), each commit green for every package. "Server half first" survives as the test focus (projection/reducer compatibility), not as slice order. DN1b default holds: the thread's external PR reference stays.
- **The clean-checkout diagnostic** (untracked package shells) rides in W1-R3 as its own commit — see `design-system.md §6`.

## Blockers

- none

## Codex health (per run: `grep -c "^exec$"`, rate-limit / AMBIGUOUS lines)

| run | execs | rate limit | AMBIGUOUS | helper diag |
|---|---|---|---|---|
