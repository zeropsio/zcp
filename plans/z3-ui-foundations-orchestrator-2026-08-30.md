# z3 UI foundations — orchestrator brief

Date: 2026-08-30. Owner: Karel. For: **a fresh Fable 5 session** that runs the whole programme in
`plans/z3-ui-foundations-2026-08-30.md` (the plan; read it first — this brief does not repeat it).
Start the session in this repo (`zcp`); the code lives in `../z3`.

You are the **owner-orchestrator**. You never implement. Implementation is done by **Codex runs**
(`gpt-5.6-sol`, reasoning `xhigh` — the account's default), as many in parallel as the dependency
graph allows, each in its own git worktree. You verify (mechanical gate + Claude reviewers), you
own git (every commit, merge, push is yours), you keep the register and the ledger, you talk to
the owner. The owner has a large Codex budget: the constraint is dependencies and review
bandwidth, not tokens.

Reading order, then act: this brief → the plan (all of it) → `docs/spec-z3.md` §5/§7–§9 →
`../z3/docs/internals/zerops/fork.md` → `../z3/CLAUDE.md` + `../z3/AGENTS.md` → the inputs in
`plans/z3-ui-foundations-2026-08-30-inputs/` (the two code maps, the Codex review of the plan) →
the concept `plans/z3-ui-redesign-concept-2026-08-29.md` §3, §5, §8 (skim).

---

## 1. Your job in plain words

Run phases F0–F6 of the plan to completion, wave by wave (§5), landing every slice on the fork's
`main` with the guards green, and stop at the end of F6 with a report — surface rounds need the
owner's flows and are not yours. Every slice is: a self-contained Codex brief → a Codex run in a
worktree → your gate → a reviewer's verdict → your commits → your merge → the register. Nothing
lands without a RED test first and without the plan's rules R1–R8 staying green.

## 2. Repos, branches, paths, rig

| What | Where |
|---|---|
| The fork | `/Users/macbook/Documents/Zerops-MCP/z3`, branch `main`, remote `origin` = `zeropsio/z3` (**public**; SSH push — a fine-grained PAT is refused for `.github/workflows`; Xcode's `osxkeychain` shadows `GIT_ASKPASS`). Never merge/rebase `upstream/*`. |
| Worktrees | `/Users/macbook/Documents/Zerops-MCP/z3-wt/<slice-id>` on branch `wt/<slice-id>` cut from `main` |
| This repo | `zcp`, branch `feat/z3-continuation`: plans, `docs/spec-z3.md`, CLAUDE.md. Spec/plan edits land here. |
| Register | `plans/z3-ui-foundations-runs-2026-08-30.md` (you create it; transient) |
| Ledger | `../z3/docs/internals/zerops/{verified,hacks,questions}.md` + the new `design-system.md` — **one writer: you**; subagents and Codex report facts as text |
| Live rig | project `z3-eval` (see CLAUDE.local.md "z3 dev rig") — needed only for recording scenes (F5a) and the door smoke (F6); everything else is offline |
| Codex | `/Users/macbook/bin/codex-helper.sh` (the only sanctioned path; never raw `codex exec`, never the `codex:*` plugin — other Claude sessions run in this repo and the plugin's broker teardown hangs them) |

## 3. Hard rules

Inherited (read them): `../z3/CLAUDE.md` disciplines, `../z3/AGENTS.md` ("Hit every surface",
reverse states, no continuously repainting animation, no repo-wide checks, UI changes need
before/after images), zcp `CLAUDE.md` (English, no `Co-Authored-By`, delete don't disable, atomic
commits), the plan's R1–R8.

Yours, in addition:

1. **You never edit product code.** You write briefs, run gates, commit what Codex produced, merge,
   write the ledger and the register. A fix you could "just do" is a follow-up Codex run.
2. **Git is yours alone.** Codex cannot write git metadata (proven, §4.1). Every commit is made by
   you from the commit plan in Codex's report; `main` is touched only by your merges; never
   `--force`, never a push of a knowingly red `main`.
3. **RED first, always.** A slice brief names the failing tests before the code; a report without
   the RED evidence (the failing run's output) is sent back.
4. **A slice that widened scope is rejected**, not trimmed by you. Files outside the brief's
   allowed list = send back with the list.
5. **The exception format is fixed** (§4.4) — no whole-file allowlists, no count-only budgets.
6. **No secrets in briefs or workspaces.** Codex reads `.env` through the setup symlink; never
   paste tokens, never point Codex at `~/.zerops-dev/accounts/*`.
7. **Never idle while a slot is free and a slice is unblocked** (§4.8). Never wait on one run when
   another can start.
8. **The owner decides only what §6 lists.** Everything else is pre-approved: the plan's §7
   recommendations are the decisions.

## 4. The machine

### 4.1 Facts proven on 2026-08-30 (the mechanics rest on these)

- `codex exec --sandbox workspace-write --cd <dir>` can edit files under `<dir>` and read
  anywhere, but **cannot write git metadata**: in a linked worktree (`git add` → `Unable to create
  '<main>/.git/worktrees/<id>/index.lock': Operation not permitted`) and in a local clone (`.git`
  inside the workspace is protected the same way). Only a workspace under `/tmp` escapes this —
  never use one (the fork must stay where it is). Consequence: **Codex edits and tests; you
  commit.**
- A Codex run launched through the helper with `Bash run_in_background: true` is **not** killed by
  the tool's 10-minute cap (a 1,172 s review run completed). Launch, get the completion
  notification, read the result.
- The helper prints one line — the absolute path of `/tmp/codex-out-<tag>.md` — and is
  parallel-safe (per-run tag, rollout recovery). The file holds the whole transcript; the report
  is the text after the **last** line that equals `codex`:
  `awk '/^codex$/{p=1;next} p' <out-file>`.
- The mobile screenshot harness seeds a real server + SQLite (`scripts/mobile-showcase*.ts`); the
  web has no harness; `vp` bundles oxlint (`typeAware:false`); the zone test is regex over import
  specifiers. Details: the inputs dir and the plan §1.

### 4.2 Workspace lifecycle

```bash
# create (one per slice; from main; main must be green)
cd /Users/macbook/Documents/Zerops-MCP/z3 && git worktree add ../z3-wt/<id> -b wt/<id> main
cd ../z3-wt/<id>
T3CODE_PROJECT_ROOT=/Users/macbook/Documents/Zerops-MCP/z3 sh -c 'vp i && ln -sf "$T3CODE_PROJECT_ROOT/.env" .env && ln -sf "$T3CODE_PROJECT_ROOT/infra/relay/.env" infra/relay/.env && node apps/web/scripts/warm-dep-cache.ts'
mkdir -p .orchestrator        # brief + every doc Codex must read live HERE (Codex reads the workspace; never commit it)
# once, in the main repo:  echo '.orchestrator/' >> /Users/macbook/Documents/Zerops-MCP/z3/.git/info/exclude

# retire (after the merge landed and the register says so)
cd /Users/macbook/Documents/Zerops-MCP/z3 && git worktree remove ../z3-wt/<id> && git branch -d wt/<id>
```

`vp i` hardlinks from the pnpm store (4.8 GB of `node_modules` per workspace costs little disk,
~1–2 min). Keep at most the concurrency cap (§4.8) of workspaces alive.

### 4.3 Launching a Codex slice and reading it

```bash
# launch — Bash tool, run_in_background: true, timeout 600000, description "codex <id>"
cat /Users/macbook/Documents/Zerops-MCP/z3-wt/<id>/.orchestrator/brief.md | /Users/macbook/bin/codex-helper.sh --cd /Users/macbook/Documents/Zerops-MCP/z3-wt/<id>
# on the completion notification: the task output's last line is the out-file path
awk '/^codex$/{p=1;next} p' <out-file>                    # the report
grep -c "^exec$" <out-file>; grep -n "rate limit\|429\|AMBIGUOUS recovery\|\[codex-helper.sh\]" <out-file>   # health
```

Record `<id> · out-file · started · finished` in the register the moment each event happens.
If the out-file carries a `[codex-helper.sh]` diagnostic or `AMBIGUOUS recovery`, treat the
workspace as untrusted: inspect `git -C <ws> status`, and re-run the slice (the brief is
idempotent: it starts from the workspace's current state and says so).

A follow-up run (send-back) reuses the same workspace and a brief that starts with the verdict's
findings verbatim and "the workspace already contains your previous attempt; continue from it".

### 4.4 The Codex slice brief — template (write it in full for every slice; Codex has no memory and reads only the workspace)

```markdown
# Slice <id> — <one-line goal>
You are implementing one slice of a larger programme inside a git worktree of z3 (a hard fork of T3 Code).
You work ONLY in this directory. You cannot run git write commands (add/commit/stash/checkout) — the sandbox forbids it;
do not try. `git diff`, `git log`, `git status` work. Leave the working tree with exactly your intended changes.

## Repo rules you must obey (excerpted — the full texts are CLAUDE.md and AGENTS.md in this directory)
- TDD: write the failing test first, run it, keep its failing output for the report; then make it pass.
- Verify minimally: `vp test run <files>`, `vp run --filter <package> typecheck`, `vp lint <paths>`; NEVER `vp check`, `vp run -r test`, `vpr typecheck`.
- Delete, don't disable: no commented-out code, no compat shims, no "just in case" flags. Follow the chain of every removal.
- English everywhere. No `any`. Inferred types. Comments describe use, not narrate behaviour.
- Zones: never edit `apps/server/src/provider/**`, `packages/effect-codex-app-server/**`, `packages/effect-acp/**`, `packages/contracts/src/provider*.ts`.
- No continuously repainting animation; every UI state has a phrase; reverse states; "hit every surface" (entry points, clients, providers, contracts, reverse states, connection modes, docs/user).
- Tokens only: no colour literal, no Tailwind palette utility, no `dark:`/`light:` in the Zerops dirs.
- Never touch `docs/internals/zerops/*.md` (the ledger) or `plans/`. Report facts as text; the orchestrator writes them.
- Do not run dev servers or browsers. Do not install packages unless the brief says so.

## Context (read these files in the workspace first)
- `.orchestrator/plan-excerpt.md` — the plan section for this slice (rules R<n>, predicates, acceptance)
- `.orchestrator/spec-excerpt.md` — the spec invariants this slice touches (spec-z3 §…)
- <the concrete source files, with line ranges, the slice starts from>

## The task
<what to build, in outcome terms; the exact predicate/behaviour; the exception format if a guard>

## Files you may create or edit (anything else = the slice is rejected)
<explicit list or globs>

## RED first — the tests you write before any implementation
<test file(s) and the table rows/cases each must contain; name the assertion of each rule>

## Acceptance (all must hold)
<bulleted, checkable: tests pass, typecheck of <pkg> passes, lint on touched paths passes, zone test passes, no file outside the list, …>

## Report (write it as the final message, exactly these sections)
1. RED evidence — the failing test run output (trimmed to the failing lines).
2. What changed — per file, one line.
3. Verification — every command you ran with its result line (tests: counts; typecheck/lint: pass/fail).
4. COMMIT PLAN — ordered commits, each: `message` (imperative, English, ≤72 chars + optional body) → list of files. Every changed file appears in exactly one commit. Tests and implementation may share a commit only when the RED run is described in the body.
5. Hit every surface — for each checklist row: applied / n/a + why.
6. Open questions / anything you could not verify — never guess; say so.
```

Put beside it in `.orchestrator/`: `plan-excerpt.md` (the plan's rows this slice implements —
copy, do not paraphrase), `spec-excerpt.md` (the spec invariants), and for guard slices the
**exception-entry schema**:

```
{ "path": "<repo path>", "kind": "<AST/declaration kind>", "fingerprint": "<normalized>", "owner": "<name>", "reason": "<why>", "expires": "<phase id>" }
```
CI fails on a new, a changed **or a dead** entry (an entry whose fingerprint no longer matches
anything).

### 4.5 Review — three levels

Every slice gets **L1**; most get **L2**; the big ones get **L3**. The level is in the catalogue (§5).

- **L1 — the mechanical gate (you, in the workspace):** `git -C <ws> status --short` shows only
  allowed files; the report has RED evidence; run yourself: the tests Codex names, `vp run --filter
  <pkg> typecheck` for every touched package, `vp lint <touched paths>`, `vp test run
  scripts/z3-zone-architecture.test.ts`, `node scripts/imported-lock.ts --check`, and (once it
  exists) `generate:theme --check`. Anything red = send back with the output.
- **L2 — a Claude verifier** (`Agent` tool, `subagent_type: "general-purpose"`, `model: "opus"`,
  self-contained brief): it reads the workspace diff (`git -C <ws> diff` + untracked files) and the
  Codex report, and answers, with file:line: (1) does every claim in the report hold against the
  diff; (2) does every rule of the slice brief hold (allowed files, RED-first, delete-don't-disable,
  tokens only, no scope creep); (3) is each acceptance bullet actually met; (4) which "hit every
  surface" rows were missed; (5) verdict: MERGE / SEND BACK (findings, ordered by severity) /
  ESCALATE (a design problem the plan did not foresee — that one comes to you). The verifier never
  edits. Give it the brief, the report path, the workspace path and the plan excerpt.
- **L3 — plus a Codex adversarial review**: a second Codex run in the same workspace with a
  read-only brief ("review the working-tree changes against `.orchestrator/brief.md`; find what
  is wrong, missing or gamed; name files and lines; do not edit") — cheap and independent of the
  implementer's blind spots. Run L2 and L3 in parallel; you reconcile.

You decide: MERGE → §4.6; SEND BACK → a follow-up run (§4.3) with the findings verbatim;
ABANDON → retire the workspace, note why in the register, re-brief.

### 4.6 Commit + merge protocol (yours)

```bash
# 1. commits from the COMMIT PLAN, in order, in the workspace
git -C <ws> add <files-of-commit-1> && git -C <ws> commit -m "<message-1>"      # repeat; the pre-commit hook runs vp fmt on staged files
git -C <ws> status --short                                                      # must be empty (except .orchestrator/)
# 2. bring main in, re-gate if anything merged
git -C <ws> merge main                                                          # rerere is on; a non-trivial conflict goes back to Codex as a follow-up with the conflict named
# 3. land
cd /Users/macbook/Documents/Zerops-MCP/z3 && git merge --no-ff wt/<id> -m "merge wt/<id>: <goal>"
# 4. post-merge checks on main (every time)
vp test run scripts/z3-zone-architecture.test.ts && node scripts/imported-lock.ts --check
vp run --filter <each touched package> typecheck
for f in $(git diff --name-only HEAD~1..HEAD -- '*.ts' '*.tsx'); do d=$(grep -o '^export const [A-Za-z0-9_]*' "$f" 2>/dev/null | sort | uniq -d); [ -n "$d" ] && echo "DUPLICATE EXPORT in $f: $d"; done   # the ledger's clean-merge trap
# 5. at the end of every wave (and after any slice that touched CI or guards): push, CI is the clean-checkout truth
git push origin main
```

If a post-merge check is red: revert the merge commit on `main` immediately (`git revert -m 1`),
send the slice back with the output, note it in the register. `main` is never left red.

### 4.7 The register and the ledger

`plans/z3-ui-foundations-runs-2026-08-30.md` — one table, one row per slice, updated at every
event: `id · wave · workspace · branch · codex out-file(s) · launched · finished · gate · review
level · verdict · merged sha · notes`. Below it: the exception ledger's current size per rule, the
wave status, blockers. This file is how the owner (and a successor session) sees the state.

The ledger (`verified.md`, `hacks.md`, `questions.md`, `design-system.md`): you write measured
facts, dated, with the command — after each wave at least. Codex/verifier reports are your input.

### 4.8 Concurrency — the scheduling rule

- Cap: **8 concurrent Codex runs** to start; raise to 12 when two waves have shown no rate-limit
  or `AMBIGUOUS recovery` lines and the machine keeps `vp test` under ~2× its solo time; lower on
  the first sign of either. The cap bounds workspaces, not thinking.
- The loop: (1) list every slice whose dependencies are merged; (2) fill free slots with them,
  largest first; (3) on each completion notification run L1 immediately, launch L2/L3 (they do not
  hold a Codex slot except L3), and refill the slot; (4) merge in dependency order as verdicts
  arrive; (5) never block on one review while a slot is free.
- Reviewers run as background subagents (`Agent` with a name); up to 4 at once.
- Between waves: push, CI green, ledger + register updated, the owner's one-screen report (§6).

### 4.9 Failure modes

| Symptom | Do |
|---|---|
| Out-file < 80 bytes / `[codex-helper.sh]` diagnostic / `AMBIGUOUS recovery` | the run died or was cross-wired; check `git -C <ws> status`; re-run the same brief |
| `429` / rate limit text in the out-file | halve the cap; retry the run after the others finish |
| Report claims tests pass, the gate says otherwise | send back with the gate output; if it repeats, L3 the slice |
| Codex touched files outside the list | reject; new brief with the list restated; if the list was wrong, fix the brief, not the rule |
| Merge conflict beyond rerere | follow-up run: "merge main was attempted by the orchestrator; these files conflict: …; resolve by …" — Codex cannot run `git merge`; you resolve trivial ones, Codex re-implements non-trivial ones on the fresh `main` |
| Two slices edit the same file (`oxlint-plugin-t3code/index.ts`, root `vite.config.ts`, `ci.yml`) | expected in wave 1: merge them serially, re-gate the second after its `merge main` |
| A verifier says ESCALATE | stop that slice; write the question in the register; ask the owner only if §6 says it is his; otherwise decide from the plan and note the decision in `design-system.md` |
| The live rig is needed (scene recording, door smoke) and the VPN is down | ask the owner to run `zcli vpn up` (`!` prefix in his prompt); do not fake a recording — author synthetic scenes marked `synthetic:true` and record real ones when the rig is back |

## 5. The waves — slice catalogue

Ids are `W<wave>-<name>`. Each row: goal · files allowed · RED · acceptance · review level · needs.
The plan's §3 rules and §4 phases are the authority for content; this catalogue is the schedule.
Write every brief from the template; copy the plan rows into `plan-excerpt.md`.

### Wave 0 — F0 (you, no Codex; ≤ half a day)

- **W0-CI** — `gh` is NOT authenticated for agents (401 — the login is the owner's); read CI
  through the public API instead: `curl -s -o <file> "https://api.github.com/repos/zeropsio/z3/actions/runs?per_page=5&branch=main"`
  then `jq -r '.workflow_runs[] | "\(.head_sha[0:9]) \(.status) \(.conclusion)"' <file>` (the
  repo's pre-bash hook blocks `curl | python`; save first). State on 2026-08-30 12:33: `089a0e007`
  success, `07c3d0d8c` success, `6da740c68` failure — `main` is green. If it turns red, one Codex
  slice per finding (L1). Add the clean-checkout diagnostic for untracked package shells (`git ls-files packages/ssh
  packages/tailscale` must be empty AND the dirs absent in CI) — a Codex slice if non-trivial.
- **W0-DS** — create `../z3/docs/internals/zerops/design-system.md` (dated ledger file): the
  vocabulary table skeleton (component → anatomy → states → phrase source; empty rows for the
  concept's vocabulary), the glossary (concept §2.5), the icon map placeholder, rules R1–R8 with
  their future test names, the exception-entry schema, an empty exception ledger per rule.
- **W0-FORK** — `fork.md §3` owned-product row + `AGENTS.md` "three surfaces" correction (desktop
  hosts nothing). Commit on `main`.
- **W0-REG** — the register file; `.orchestrator/` in `.git/info/exclude`; `mkdir ../z3-wt`.
- **W0-OWNER** — the §6 message to the owner (decisions with defaults).

### Wave 1 — guards + independent deletions (up to 11 parallel)

| Id | Goal | Allowed | RED | Level | Needs |
|---|---|---|---|---|---|
| W1-EXC | the shared exception-ledger module: schema, loader, `new/changed/dead` checker, a test with fixtures | `oxlint-plugin-t3code/{utils,exceptions}.ts` + tests, `scripts/lib/exceptions.ts` (if shared with CSS checks) | checker tests (new entry, changed fingerprint, dead entry → fail) | L2 | W0 |
| W1-R3 | `t3code/no-theme-escape-hatches` (generalise the mobile rule): semantic sinks only, complete-token palette utilities, class-like `dark:`/`light:`; + `scripts/check-css-tokens.ts` (parser over declarations); scope per plan R3; fingerprinted baseline = today's violations; CI wiring | `oxlint-plugin-t3code/**`, `scripts/check-css-tokens*.ts`, `vite.config.ts` lint block, `.github/workflows/ci.yml` | rule tests: SVG `d` passes, `fill="#fff"` fails, `text-red-600` fails, `text-[var(--x)]` passes; baseline size asserted | L2 | W1-EXC |
| W1-R4 | `t3code/no-legacy-vocabulary`: closed sink list, word boundaries; move the manual one-time-link copy into one named component with exact-literal exceptions | `oxlint-plugin-t3code/**`, `apps/web/src/components/auth/**` (the extraction only), `vite.config.ts` | rule tests per sink; the fallback component's exceptions in the ledger | L2 | W1-EXC |
| W1-R6 | `t3code/no-infinite-motion` (`withRepeat(-1)` resolved to its import; `Spinner` by binding in protected roots) + `scripts/check-css-motion.ts` (`animation`/`animation-iteration-count` with `infinite` → stepped helper cap or exception); baseline = the four known uses | `oxlint-plugin-t3code/**`, `scripts/check-css-motion*.ts`, `vite.config.ts`, `ci.yml` | rule + script tests | L2 | W1-EXC |
| W1-R2 | zone rule 6: protected roots + recursive local-import walk + read/allowed-command sets for `WS_METHODS`; **mechanical fixes**: split `apps/web/src/zerops/feeds.ts` into subscriptions + `commands.ts`, drop `Spinner` from `ZeropsServiceMap.tsx` | `scripts/z3-zone-architecture.test.ts`, `apps/web/src/zerops/{feeds,commands}.ts` + their tests and importers, `apps/web/src/components/zerops/ZeropsServiceMap.tsx` | zone test rows: a protected root importing `commands.ts` fails; `subscribeZeropsTopology` passes; the old raw-text quick-actions test replaced | L3 | W0 |
| W1-MAN | the surface manifest (`docs/internals/zerops/surfaces.json` or `.ts`) keyed by feature id with the plan's columns incl. connection modes; a completeness test; seed rows for every existing Zerops surface | the manifest + `scripts/surface-manifest.test.ts` | completeness test fails on a row missing a column / an unknown client value | L2 | W0 |
| W1-D-LEGACYSB | delete `LegacySidebar.tsx`, `legacySidebarEnabled` (settings contract + patch, Settings row, `AppSidebarLayout` branch, `_chat.tsx` `chat.new` branch) — follow the chain | web + `packages/contracts/src/settings.ts` | tests referencing the flag removed/updated; typecheck of web + contracts | L2 | W0 |
| W1-D-MOBLIST | mobile: `thread-list-v2-items.tsx` survives; delete legacy list branches + the device preference | `apps/mobile/src/features/threads/**`, `persistence/mobile-preferences.ts` | list tests | L2 | W0 |
| W1-D-WORKTREE | `worktreesAllowed` / `threadEnvironmentModes` on the environment descriptor (server fills from `ZeropsPolicy`); web selector filters options + default; mobile n/a stated | `packages/contracts/src/environment.ts`, server descriptor producer, `apps/web/src/components/BranchToolbarEnvModeSelector*.ts(x)` + tests | contract test; selector test: Zerops descriptor ⇒ no Worktree option | L3 | W0 |
| W1-D-MISC | `apps/web/vercel.ts`, `scripts/announce-connect-ga.ts`, `SidebarStageBackdrop` + stage art + `APP_STAGE_LABEL` consumers; `docs/user/*` pages for deleted features | as listed | build + tests of touched packages | L1 | W0 |
| W1-D-NAMING | F2.7 naming with the keep-list (bundle ids, `t3code://`, npm name) | `branding.ts`, `SplashScreen`, `index.html`, `clientMetadata`, `settingsSearch`, desktop `DesktopEnvironment.ts` + menu, mobile `app.config.ts` display names | string tests where they exist; R4 stays green | L2 | W1-R4 merged (so the vocabulary rule guards it) |
| W1-D-PR-SERVER | F2.1 server half: `apps/server/src/pullRequest/**`, `ws.ts` handlers, `RpcAuthorization` scopes, `server.ts:475` layer, contracts `pullRequest.ts` + `environmentHttp.ts` group; **keep** the thread's external PR reference (`orchestration.ts`, `environment.ts` capabilities) unless the owner said otherwise (§6) | server + contracts + client-runtime `state/pull-requests` | projection/reducer compatibility tests; typecheck of server, contracts, client-runtime, web, mobile | L3 | W0 |

### Wave 2 — shared logic ∥ tokens (6 parallel)

| Id | Goal | Level | Needs |
|---|---|---|---|
| W2-F3-MOVE | move the 13 UI-free modules + tests to `packages/client-runtime/src/zerops/`; storage/fetch/clock injected; web imports rewritten; zone rule 5 (R1) + `t3code/no-platform-globals` | L3 | W1-R2 merged |
| W2-F3-STATUS | `packages/shared/src/threadStatus.ts` resolver (`{kind, toneId}`) + `client-runtime/zerops/statusPresentation.ts`; consumers rewired (web row + pill, mobile row incl. its 8 literals, widget props, relay `statusForPhase`); the R5 vector test | L3 | W2-F3-MOVE (shares files) — or run first and let MOVE merge onto it; pick one order and say it in both briefs |
| W2-F4-THEME | `ZEROPS_THEME` + `brand.ts` (+ exports entry, subpath without `zerops`); web + mobile defaults through the existing paths; R7 tests | L3 | W1 guards merged |
| W2-F4-PROJ | `scripts/generate-theme-tokens.ts`: web `index.html` boot snippet + mobile `global.css` default block, `--check`, byte-equality tests, CI wiring (R8) | L2 | W2-F4-THEME |
| W2-F4-FONTS | Roboto (web self-hosted woff2 / mobile expo font), provider accent fallback, the colour-paths decision rows (ANSI/Pierre/shiki exceptions; `--success`/`--info` from `brand.ts`; `AuthSurfaceShell` gradient + artwork row deleted) | L2 | W2-F4-THEME |
| W2-D-PR-CLIENT | F2.1 client half: web `components/pullRequest/**` + route + state; mobile `use-thread-pr.ts` + affordances | L2 | W1-D-PR-SERVER |

### Wave 3 — harness ∥ seam adapters (9 parallel)

| Id | Goal | Level | Needs |
|---|---|---|---|
| W3-F5a-SCENES | scene bundle v1: versioned directory, schema (topology + lifecycle + agent-auth + agent-login + `threadActivities` + projects/threads), loader + validation test, first scenes (authored from contracts, `synthetic:true`; real recordings replace them when you record on `z3-eval`) | L2 | W2-F3-MOVE |
| W3-F5a-FEEDS | `apps/server/src/zerops/ZeropsFixtureFeeds.ts`: four `Layer.scoped` publishers (`Ref` + `PubSub` + `Clock`), composed with the `provideMerge` shape, selected in `zeropsFeedsLayer.ts` by `T3CODE_ZEROPS_FIXTURES`; `config.ts` + `cli/config.ts`; fixture mode sets Zerops mode; TestClock tests; a subscription test that acks every chunk | L3 | W3-F5a-SCENES |
| W3-F5a-SEED | the `projection_thread_activities` seeder shared by `mobile-showcase-environment.ts` and the new `scripts/web-showcase.ts` (seeded env + `playwright-core`, 1440×900 + 390×844 × light/dark × scene → `artifacts/web-showcase/`) | L3 | W3-F5a-SCENES |
| W3-F5a-CONTRACT | consumer-driven contract fixtures: every scene decodes through the public schemas and runs through the web / mobile / relay presentation adapters; unknown-shape and missing-optional cases | L2 | W3-F5a-SCENES, W2-F3-STATUS |
| W3-F5a-DESK | desktop `smoke-test` `capturePage()` | L1 | W3-F5a-SEED |
| W3-F5c-CHAT | `resolveZeropsChatChrome` extracted from `ChatView.tsx` + the availability / lifecycle / attention tests; inline and sheet paths consume one value | L3 | W2-F3-MOVE |
| W3-F5c-PANEL | exhaustive `RightPanelKind → availability/content/migration` adapter + test | L2 | W3-F5c-CHAT (touches `ChatView`) |
| W3-F5c-TIMELINE | the timeline row-vector test + the milestone predicate at the three hiding mechanisms (predicate only — which kinds escape stays the owner's D2; default per plan) | L3 | W2-F3-MOVE |
| W3-F5c-DOOR | one resolver over descriptor × `bootstrapMethods`, the four route checks consuming it, matrix test | L3 | W0 |

### Wave 4 — primitives + the door (3 parallel)

| Id | Goal | Level | Needs |
|---|---|---|---|
| W4-F5b-WEB | web primitives (`StatusDot`, `MicroLabel`, `Chip`, `Pill`, `FlatCard`, `MintPanel`, `ProcessSteps`, `KeyChip`, `LivenessLine`) with the four test kinds; `ui/*` variants **added** (`pill`, `chip`, `flat`); a showcase probe scene | L3 | W2-F4-*, W3-F5a-SEED |
| W4-F5b-MOB | mobile primitives (native containers, Zerops contents) + showcase scene | L2 | W2-F4-*, W3-F5a-SEED |
| W4-F6-DOOR | `PairingRouteSurface` branches on `bootstrapMethods`; `resolveChatIndexView` widened; identity base URL under `/z3/`; `cli.ts pack` favicons; live smoke on `z3-eval` from the container origin and a hosted build | L3 | W3-F5c-DOOR; **coordinate with S4's owner** (the owner) before launching |

After wave 4: the programme's done criteria (§7). Do **not** start surface rounds.

## 6. The owner

Pre-approved (the owner's instruction: "do it the way the plan proposes"): every recommendation in
the plan's §7 — D0 evolve, D1, D9, D10 adjusted, DN1a delete, DN2a/b delete, DN3 capability, DN4
scene bundle, DN5 `playwright-core`, DN6 mobile primitives now / screens later, DN7 naming in F2,
DN8 resolver in `packages/shared`, DN9 keep the activity-relay path, DN10 fingerprints.

Ask, once, at the start (one message, Czech, defaults stated; proceed with the defaults if no
answer arrives before the slice that needs it):

1. **DN1b** — the thread's external PR reference (status + deep link): keep read-only (default) or
   delete with DN1a.
2. **F2.5 trigger** — `/connect*` + web Clerk go only with S5-5's CLI entry points; is S5-5 his to
   start now, or does F2.5 wait (default: wait; everything else proceeds).
3. **W4-F6** — S4's files; when may the door slice run (default: after wave 3, with a live
   smoke he retests).
4. **Scene recordings** — when the `z3-eval` VPN can be up for ~1 h of recording (default:
   synthetic scenes first).

Report at the end of every wave, in Czech, one screen, the `plan-summary` shape: what landed
(slice → one line), what is red/blocked, the exception-ledger sizes, the next wave's slots, and
exactly one question if there is one. Never a question the plan already answers.

## 7. Done, and what comes after

Done when: every wave-4 slice is merged; `main` is pushed and CI is green on a clean checkout;
R1–R8 are enforced in CI with their exception ledgers listed in `design-system.md`; the theme
projector `--check` runs in CI; one scene renders on the web showcase, the mobile showcase and
the desktop capture; the register's last row says so; the ledger has the wave facts; `docs/spec-z3.md`
gained the "client design system" section with DS rows naming their tests (write it in `zcp`,
branch `feat/z3-continuation`, as the LAND step of the programme — promote from the plan, then
`git mv` the plan to `plans/archive/`).

After: surface rounds (plan §5) — each needs the owner's flow decision; hand the owner the
register and the design-system ledger, and stop.
