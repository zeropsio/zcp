# v23 post-mortem — content recovered, convergence broke

**Status**: post-mortem analysis only. The v8.86 implementation plan that was originally drafted in §7 of this doc was superseded after dialogue surfaced that those fixes addressed symptoms (finding truncation, brief framing, embedded-yaml docs) not root cause (external gate + dispatch subagent is anti-convergent by construction). **The current v8.86 plan lives at [implementation-v8.86-plan.md](implementation-v8.86-plan.md)**. Treat §7 below as a record of the initial (rejected) approach.

v23 is the first run after v8.81–v8.85 shipped (post-writer content-fix gate, scaffold preambles, dev-start contract check, env-var auto-inject teaching, env_self_shadow check, response-body hygiene, eager-scope shift). This doc audits which gates held, which silently failed to fire, which produced the right deliverable at the wrong cost.

**Headline**: v23 grades **C overall**. Content is the silver lining (38% pure-invariant gotchas vs v22's 0%, full architecture section, all CLAUDE.md pass) but the run wall-clocked **119 min** — second-worst since v8 (only v21's 129 min was longer of complete runs) — driven by a single load-bearing structural failure in the v8.81 post-writer content-fix gate that turned what should have been one round of fixes into FIVE rounds of churn. Plus three platform-mental-model defects (one of which, `zsc execOnce` "burn trap", got enshrined in the published `apidev/CLAUDE.md` as fictional Zerops folk-doctrine).

---

## Table of contents

- [1. Where we are — v23 scoreboard](#1-where-we-are)
- [2. v8.81–v8.85 gate effectiveness audit](#2-gate-audit)
- [3. The content-fix-loop disaster (the 23-minute cost driver)](#3-content-fix-loop)
- [4. Platform-mental-model decay (3 instances)](#4-mental-model)
- [5. Step-by-step time decomposition](#5-time-decomposition)
- [6. The "senseless deploy" perception — refuted by data](#6-deploy-perception)
- [7. Fixes for v8.86, ordered by blast-radius-per-effort](#7-fixes)
  - [7.1 Content-fix gate convergence — surface ALL findings, no truncation](#71-fix-gate-convergence)
  - [7.2 Embedded-yaml awareness — comment_ratio reads README, not zerops.yaml](#72-embedded-yaml)
  - [7.3 Scaffold-brief: nodenext requires `nest start`, not raw `ts-node`](#73-nodenext)
  - [7.4 `execOnce` semantics topic — kill the "burn trap" folk-doctrine](#74-execonce)
  - [7.5 `zerops_deploy` MCP-channel docs — kill the "no parallel deploys" myth](#75-mcp-channel)
  - [7.6 `content_reality` finding-class grouping — fix at the report layer](#76-content-reality)
  - [7.7 Optional: snapshot-dev redundancy elimination](#77-snapshot-dev)
- [8. Expected v24 grade](#8-expected-v24)
- [9. Risk analysis + validation plan](#9-risk)
- [10. Non-goals](#10-non-goals)

---

## 1. Where we are

### v23 scoreboard

- **Overall grade: C** (v22 was B; v21 was D; v20 was A−)
- **Wall: 119 min** (12:10 → 14:09; +15% vs v22, +68% vs v20)
- **Assistant events: 384** (v22: 410, v21: 381, v20: 294 — third-highest in the log)
- **Tool calls: 233** (v22: 243, v21: 233 — same as v21)
- **Main bash: 38.7 s / 0 very-long / 2 errored** — best operational hygiene since v18
- **Subagents: 10** (writer ×1, scaffold ×3, feature ×1, **3 README/content fix subagents**, env-comment writer ×1, code review ×1) — matches v20's record; but the 3 fix subagents are net-new structural cost
- **Deploy calls: 17** (v22: 11, v21: 15) — explained by 5 deploy clusters × 3 services (initial dev, snapshot-dev, dev→stage cross-deploy, code-review redeploy, second cross-deploy)
- **Dev_server calls: 21** (v22: 15) — new record; 7 start/stop cluster batches
- **Workflow `complete deploy` calls: 20** — 12 substep completes + 8 retries on README content checks
- **Guidance topic fetches: 18** (v22: 17) — peak; all 18 distinct topics, no duplicates
- **Published tree clean** — `scaffold_hygiene` gate held (no v21-class node_modules leak)

### Step time decomposition

| Step | Duration | Notes |
|---|---:|---|
| Research | 1m 6s | clean |
| Provision | 3m 8s | 14 services imported in one batch, env vars set in one batch |
| Generate | 15m 47s | 3 parallel scaffold subagents (apidev 11m / appdev 1m44s / workerdev 2m43s) + main writes 3 zerops.yaml files |
| Deploy | **44m 19s** | 12 substeps (12:30→13:14, ~44 min); but full-step gate then took **23 min more** for README content fixes (13:18→13:41) |
| Finalize | 4m 39s | 1 retry on `recipe_architecture_narrative` |
| Close | 22m 54s | code review subagent (6m12s) + redeploy ×3 + cross-deploy ×3 + close-browser-walk |

**The 23-min README fix loop within deploy is the single biggest controllable waste in the run.** Without it, v23 would have wall-clocked at ~96 min — same range as v18 and v22.

### Content metrics

| Codebase | README | CLAUDE.md | Gotchas | IG items |
|---|---:|---:|---:|---:|
| apidev | ~250 lines (7,840 B) | 5,487 B | 6 | 4 |
| appdev | ~260 lines (8,134 B) | 4,446 B | 3 | 4 |
| workerdev | ~290 lines (8,952 B) | 5,886 B | 4 | 4 |
| Root | architecture section ✅ | n/a | n/a | n/a |

Compared to v22 (341/208/221 README lines, 7/6/6 gotchas, 8/6/6 IG): v23 compressed ~40-50% on README size, dropped to 6/3/4 on gotchas, dropped to 4/4/4 on IG — but **gotcha-origin ratio swung from v22's 0% pure-invariant to v23's 38% pure-invariant + 15% mixed**. Compression came WITH quality, not against it.

### Close review

**0 CRITICAL / 0 WRONG / 1 STYLE post-fix** — equivalent to v22 and v14. Code-review subagent caught:
- 3 CRITs (StatusPanel flat-map shape, workerdev entity divergence, missing NATS queue group)
- 1 WRONG (workerdev SIGTERM drain)
- 1 WRONG (apidev cache.drop double-RTT)
- 1 STYLE (extra `data-feature="status"` wrapper)

All fixed pre-publish. No CRITs reached the published artifact.

### Rating

**S=A** (all 6 steps, all features, all browser walks, code-review CRITs caught), **C=A−** (38% pure-invariant gotchas + 15% mixed, full architecture section, all CLAUDE.md pass — minor: 4 of 13 gotchas have IG-overlap or split-lesson defects), **O=C** (119 min wall vs v22's 103, v20's 71; main bash A; cost driver is the 23-min README fix loop), **W=B** (10 subagents incl. 3 README fix dispatches; 1 substep gate retry; both browsers fired; v8.81 dispatch gate fired but failed to converge in one round). **Overall: C** (limiter: O).

---

## 2. v8.81–v8.85 gate audit

| Version | Gate | Fired? | Outcome | Verdict |
|---|---|---|---|---|
| v8.81 | post-writer content-fix dispatch gate | ✅ 3× | Forced 3 fix-subagent dispatches when post-readmes checks failed | **Mechanism works; brief construction broken** (see §3) |
| v8.81 | NATS credentials scaffold preamble | ✅ silent | apidev `nats.client.ts` and workerdev `main.ts` both pass user/pass as separate ConnectionOptions fields. v21+v22 CRIT class did NOT recur | Held |
| v8.81 | S3 endpoint scaffold preamble | ✅ silent | apidev uses `process.env.storage_apiUrl` (https). v22 301-redirect CRIT did NOT recur | Held |
| v8.81 | `dev_start_contract` check | not observed in pass/fail stream | Either passed silently or didn't fire on v23's setup. workerdev uses `npm run start:prod` → `node dist/main.js` (prod) and `npm run start:dev` → `ts-node ...` (dev); contract is consistent | Inconclusive — needs targeted shadow test |
| v8.81 | `recipe_architecture_narrative` (finalize) | ✅ 1 fail / 1 pass | Caught the missing root README architecture section, agent wrote it, retry passed | Held |
| v8.82 | `zerops_yml_comment_depth` (generate) | 0 fail / 1 pass | Passed first try | Held |
| v8.82 | `content-quality-overview` eager topic | n/a (not in tool count) | Topic was loaded as part of generate substep; no session-level evidence of effect | Inconclusive |
| v8.82 | container-ops-in-README soft nudge | not observed in stream | Either passed silently or unfired | Inconclusive |
| v8.82 | IG causal-anchor parity | not observed in stream | Inconclusive |
| v8.83 | substep response-size fall-through fix | n/a | response sizes look healthy in tool_results (no >40KB substep responses) | Held |
| v8.84 | step-entry eager scope shift | n/a | deploy step-entry response was inline, not persisted-to-disk | Held |
| v8.85 | `env-var-model` eager topic | ✅ silent | No env_self_shadow events in the pass/fail stream; both `redis_hostname/redis_port` AND `db_hostname/db_dbName/db_user/db_password` referenced via `process.env` correctly without YAML-level rename | Held |
| v8.85 | `{hostname}_env_self_shadow` check | 0 fail / 0 pass observed | Either fired silently with pass, or didn't fire — no `${key}: ${key}` shadows in any zerops.yaml | Held |
| v8.85 | pre-flight resolved-setup propagation | ✅ silent | All 17 deploys passed `setup=dev` or `setup=prod` correctly; no "Cannot find setup" errors | Held |

**Pattern**: every v8.81–v8.85 gate that fired produced the right outcome. The flagship v8.81 post-writer content-fix dispatch gate fired correctly and dispatched the right kind of subagent — but the **brief construction inside the gate** was anti-convergent and produced 3 fix subagents in series instead of 1.

The v8.81 post-writer dispatch gate is structurally correct. The brief content shipped with it is not.

---

## 3. The content-fix-loop disaster

### What happened

After the readmes-substep writer subagent (`a180aa17`, 5m11s, 33 events) shipped 6 README+CLAUDE.md files at 13:14:02, main agent called `complete step=deploy`. The deploy-step gate ran 11 content checks across 3 codebases. Result event #496 (13:18:15): **23 fails / 47 passes**.

v8.81's `content_fix_dispatch_required` gate fired, blocked the `complete deploy` action, and dispatched `agent-afb4e132` ("Fix README + zerops.yaml quality issues for deploy checker") at 13:20:32.

That subagent ran 5m 50s (55 events, 13 Edits + 3 Writes + 3 commits), reported success at 13:26:22. Main called `complete deploy` again at 13:26:38. Event #515: **11 fails / 59 passes**. Better, but not done.

Second fix subagent `af7c2223` ("Round 2 README content fixes") dispatched 13:27:45 — 4m 54s, 43 events, 13 Edits + 3 commits. Reported success at 13:32:39. Main called `complete deploy` again at 13:32:48. Event #521: **5 fails / 65 passes**.

Third fix subagent `a637f5c4` ("Final README content-reality fixes") dispatched 13:34:48 — 2m 14s, 33 events, 6 Edits + 3 commits. Reported success at 13:37:03. Main called `complete deploy` again at 13:37:10. Event #551: **4 fails / 66 passes**.

Then main agent abandoned the dispatch pattern and ran 17 inline Bash/Edit/Read calls itself for 2 min 51s, made 2 more `complete deploy` attempts at 13:40:01 (2 fails) and 13:41:38 (**0 fails — pass**).

**Total: 5 rounds (1 writer + 3 fix subagents + 1 inline main pass) over 23m 36s.** Strictly decreasing fail count: 23 → 11 → 5 → 4 → 2 → 0. No whack-a-mole.

### Why it took 5 rounds

Three deficiencies in v8.81's brief construction, in order of root-cause weight:

#### (a) `content_reality` check truncates findings with `... and 1 more`

The check emits a `Findings:` array, but the wire format truncates to the first ~5 entries with `and N more` appended. Each fix-subagent brief listed only the visible findings. Each round therefore could only fix what the previous round had been told about — and `and 1 more` became `and 1 more` again on the next round, with a different specific finding revealed.

Concrete trace:
- After SA1 (writer): `app_content_reality` listed `import.meta.env`, `process.env`, `response.json` (×2), `res.json`, `... and 3 more`
- After SA2 (fix #1): `app_content_reality` listed `import.meta.env`, `process.env`, `response.json` (×2), `res.json` again — same visible findings, the previously-hidden 3 still hidden
- After SA3 (fix #2): list shrank to 5 — SA3 found and fixed 3 of the visible 5; still `and 1 more` on `res.json` parens-vs-no-parens
- After SA4 (fix #3): main agent finally grep'd the whole tree for the offending tokens

#### (b) Brief-builder hardcodes "be surgical, only the 4 findings"

SA4's final assistant message literally says:

> "Unaddressed / intentionally skipped: apidev/README.md line 21 (IG step 1 prose still names dist/main.js, dist/migrate.js, dist/seed.js) — not flagged in the 4 failing findings."

The brief framing told the subagent to stay narrow. Combined with truncated findings, this guarantees the subagent leaves known-bad content in place because it wasn't on the visible list.

#### (c) Embedded-yaml semantics — SA2 fixed wrong file

`comment_ratio` reads the **embedded copy of `zerops.yaml`** inside the README's IG step 1 fenced ```yaml block — NOT the on-disk `zerops.yaml`. SA2 fixed every on-disk `zerops.yaml` (3 files, 9% → 57% comment ratio measured on disk). The next checker run still reported 7%/9%/11% on `comment_ratio` because the embedded README copies were untouched.

SA3's brief had to call this out explicitly: *"BUT the README's integration-guide step 1 contains a copy of zerops.yaml in a fenced yaml block — that copy is what the checker parses for `comment_ratio`."*

This is a hidden contract: the check operates on a derived surface (embedded copy), but the brief talks about the source surface (file). The brief-builder needs an explicit "if check X fails, the fix touches files A AND derived copies in B" map.

### Cost

- **23m 36s wall clock burned** — would have been 1 round (~5 min) with correct brief construction
- **3 fix subagents = ~600 KB of context burn** in subagent transcripts (148+178+163+127 KB across writer+3-fixers; without the failure pattern, only the writer's 148 KB needed)
- **5 redundant `complete deploy` calls** — each round-trips a multi-KB workflow response

### Comparison to v22

v22 also had this class of failure but it manifested differently — main agent absorbed the iteration via 11 Edits on `workerdev/README.md` after the writer subagent returned. v23 dispatched the iteration to subagents (the v8.81 dispatch gate works) — but the subagents also failed to converge because the gate's brief construction has the same issues.

**v22's failure: dispatch gate didn't exist, so iteration leaked to main.**
**v23's failure: dispatch gate exists but emits convergence-hostile briefs, so iteration loops across subagents.**

The v8.81 gate fixed the first half of the bug. v8.86 needs to fix the second half.

---

## 4. Platform-mental-model decay

Three instances. One is a real bug the agent invented; one is a transport-layer error the agent attributed to a non-existent platform rule; one is a bug-fix-without-understanding that landed sustainable code with a wrong narrative.

### 4.1 The "zsc execOnce burn trap" (apidev/CLAUDE.md as published)

**What happened**

At 12:31:36, the apidev container's first deploy ran:

```
🙏 zsc execOnce ${appVersionId} --retryUntilSuccessful -- npx ts-node src/seed.ts
✅ zsc execOnce ${appVersionId} --retryUntilSuccessful -- npx ts-node src/seed.ts
✅ ALL RUN.INIT COMMANDS FINISHED
```

Δ = 56 ms. Zero `[seed] upserted ...` lines between the 🙏 and ✅. `SELECT COUNT(*) FROM items` returned 0.

Manual rerun at 12:36:52 produced 5 `[seed] upserted ...` lines and exit 0. Subsequent deploys (12:53 snapshot-dev) ran the seed cleanly with new `appVersionId`.

**What the agent wrote** (deploy-dev attestation 13:14:47):
> "Seed initCommand finished in 56ms on first deploy with no output (suspected zsc execOnce burn from initial workspace deploy); ran `npx ts-node src/seed.ts` manually to populate the 5 seed rows."

And then **codified this in `apidev/CLAUDE.md`** under a section titled "Recovering `zsc execOnce` burn".

**Why it's wrong**

`zsc execOnce` keys on `${appVersionId}`, which is **per-deploy**, not per-workspace. There is no concept of a workspace-creation-time deploy that pre-burns the next user's first deploy's key. The platform doesn't expose any mechanism that would produce that behavior.

The actual cause of the 56ms / 0-rows event is undiagnosed but most likely:
- Script-side silent exit (e.g., a `catch` block that swallows + exits 0 with no stdout)
- Some race where `tsconfig-paths` failed to resolve `src/seed.ts` and ts-node returned without error
- A container-labeling artifact (`apidev-runtime-2-2e` suggests this is the *second* container — the first may have been an import-time placeholder)

The agent diagnosed by analogy to a similar-sounding mechanic ("burn") and persisted that fictional mechanic into the recipe deliverable. Future runs reading `apidev/CLAUDE.md` will inherit this folk-doctrine.

**Fix**: §7.4 below — explicit `execOnce` semantics topic at scaffold + deploy time, plus a content-check that fails if any README/CLAUDE.md text says "burn" near `execOnce`.

### 4.2 "Parallel cross-deploys are rejected" — `Not connected` misattribution

**What happened**

At 12:32:17–19 the agent issued 3 calls in parallel: `zerops_subdomain enable apidev`, `zerops_deploy targetService=appdev setup=dev`, `zerops_deploy targetService=workerdev setup=dev`. The subdomain call returned `FINISHED`. The two deploy calls returned `Not connected` (`is_error: true`).

Same pattern at 13:04:21–13:05:07 during cross-deploy.

**What the agent wrote** (TIMELINE.md):
> "Sequential cross-deploys (parallel called but tool returned 'Not connected' for simultaneous deploys, so serialized)"

**Why it's wrong**

`Not connected` is the standard MCP STDIO error when the server hasn't responded within the local timeout. `zerops_deploy` is a long-blocking call (1–2 min build duration) that holds the MCP STDIO channel; a second concurrent request on the same channel gets dropped at the transport layer. This is a **client-side limitation** in how the MCP server multiplexes requests, not a platform refusal.

The agent's narrative was then propagated to TIMELINE.md as if Zerops itself disallows parallel cross-deploys. It does not. The same call sequence, in a hypothetical async-MCP transport, would succeed.

**Fix**: §7.5 below — add to `zerops_deploy` tool description that the call blocks the MCP channel and other zerops_* calls in the same response will return `Not connected`. Or fix the MCP server's `zerops_deploy` to spawn the build async and return a deploy-id.

### 4.3 `module: nodenext` — fixed without understanding

**What happened**

Workerdev dev start failed at 12:35:26 with `Error: Cannot find module './app.module.js'`. Root cause: workerdev's tsconfig has `"module": "nodenext"` and its `start:dev` script is `ts-node -r tsconfig-paths/register src/main.ts` — raw ts-node under nodenext requires literal `.js` suffixes on relative imports, which weren't there.

Agent overwrote workerdev tsconfig with `"module": "commonjs"`, stripped `.js` suffixes, dev-start succeeded.

**Why it's wrong (or rather, incomplete understanding)**

apidev tsconfig has **identical** `"module": "nodenext"`. apidev's dev start works because its `start:dev` script is `nest start --watch`, which uses `@nestjs/cli`'s own bundler to handle nodenext.

The agent's TIMELINE narrative says: *"NestJS 11 scratch emitted module: nodenext... ts-node refused to resolve ./app.module.js under default CommonJS require stack"* — true, but the agent never asked: "why doesn't apidev hit this too?"

The fix (commonjs in workerdev) is sustainable. But the diagnosis is shallow. The correct fix is either keep nodenext + add `.js` suffixes everywhere, OR change workerdev's `start:dev` to use `nest start --watch` like apidev. Neither was attempted.

**Fix**: §7.3 below — scaffold-subagent brief should call out the ts-node + nodenext incompatibility and prescribe the choice (commonjs OR keep nest start as the dev runner).

---

## 5. Step-by-step time decomposition

| Time slot | Event | Cumulative wall |
|---|---|---:|
| 12:10:11 | session start | 0:00 |
| 12:11:17 | research complete | 0:01:06 |
| 12:14:25 | provision complete (14 services imported) | 0:04:14 |
| 12:15:32→12:27:17 | scaffold subagents (parallel, apidev took 11m, others 2-3m) | 0:17:06 |
| 12:30:12 | generate complete (zerops.yaml ×3 written, smoke tests passed) | 0:20:01 |
| 12:30:33→12:35:11 | deploy-dev ×3 (initial) + dev_server starts ×3 | 0:25:00 |
| 12:35:26 | workerdev nodenext crash → fix → restart → 12:35:59 success | 0:25:48 |
| 12:36:19 | logs check reveals seed 0-rows mystery | 0:26:08 |
| 12:36:52 | manual seed run successful | 0:26:41 |
| 12:37:40 | dev_server stops ×3 (pre-feature-subagent) | 0:27:29 |
| 12:39:28→12:53:10 | feature subagent (5 features, 14 min) | 0:43:00 |
| 12:53:37→12:56:30 | snapshot-dev redeploy ×3 (~3 min total) | 0:46:20 |
| 12:57:49→12:57:51 | dev_server starts ×3 (post-snapshot) | 0:47:42 |
| 12:58:54→13:01:00 | dev browser-walk + items-crud Svelte 5 fix | 0:50:50 |
| 13:02:04→13:02:06 | dev_server stops ×3 (pre-cross-deploy) | 0:51:55 |
| 13:02:16→13:05:06 | cross-deploy ×3 to stage | 0:54:56 |
| 13:05:06→13:14:31 | stage browser-walk + verify-stage + readmes substep | 1:04:21 |
| 13:14:31→13:18:15 | 12 substep `complete` calls (rapid) — full-step gate then **fails on README content checks** | 1:08:05 |
| **13:20:30→13:37:10** | **3 fix subagents** + main inline pass | **1:27:01** |
| 13:41:38 | full-step deploy gate finally passes | 1:31:28 |
| 13:45:33→13:46:17 | finalize (architecture-narrative fix) | 1:36:07 |
| 13:47:01→13:53:15 | code review subagent (6m12s) | 1:43:05 |
| 13:53:22→13:56:13 | redeploy ×3 (apply code review fixes) | 1:46:03 |
| 13:57:26→13:58:38 | dev_server start/stop cycle for E2E queue-group test | 1:48:28 |
| 13:58:41→14:01:50 | second cross-deploy ×3 | 1:51:40 |
| 14:03:24→14:03:48 | close-browser-walk + close complete | 1:53:38 |
| 14:09:11 | session end (TIMELINE generation + recipe export) | 1:59:01 |

**Aggregate**: 119 min total. The README fix loop was 23 min (13:18→13:41). Without it, v23 would have wall-clocked at ~96 min — competitive with v18 (65) / v22 (103) / v20 (71) — same range as the prior 3 successful runs.

---

## 6. The "senseless deploy" perception — refuted by data

User reported "senslessly kept repeating deploys even while it just did it in the prev step." Audit (full deploy-iteration analysis at `/private/tmp/.../a13f6534fef2e7718.output`):

- **17 total deploys**: 15 produced successful builds, 2 returned `Not connected` instantly (no build burned).
- **0 redundant deploys**: every same-target redeploy had at least one new commit between it and the prior deploy. The 5 deploy clusters of 3-each break down as:
  1. **Initial dev (12:30-12:35)**: first artifact creation. Necessary.
  2. **Snapshot-dev (12:53-12:56)**: bake feature subagent's commits (`afdfa1a`/`430bc06`/appdev-features) into deploy artifact. Necessary because subdomain traffic serves the deployed artifact, not the SSHFS mount. Without this, browser-walk on subdomain hits scaffold-only state.
  3. **First cross-deploy (13:02-13:05)**: dev → stage promotion. Necessary.
  4. **Code-review redeploy (13:53-13:56)**: apply 3 CRITs + 2 WRONG fixes from code review. Necessary.
  5. **Second cross-deploy (13:58-14:01)**: stage promotion of code-review fixes. Necessary.

**The user's perception is real** — 5 clusters of "we just deployed all 3 services" creates a strong rhythmic feeling of repetition. **But the data shows zero waste.** Each cluster has substantively different content from the prior.

**One possible structural simplification** (§7.7): snapshot-dev could be eliminated if cross-deploy were taught to read from the SSHFS mount directly without requiring a prior dev artifact. But that's a workflow architecture change, not a v8.86-scope fix.

---

## 7. Fixes for v8.86

Ordered by blast-radius-per-effort.

### 7.1 Content-fix gate convergence — surface ALL findings, no truncation

**Problem**: v8.81's `content_fix_dispatch_required` gate constructs a fix-subagent brief from the visible findings of failed checks. `content_reality` truncates with `... and N more`, so each round only sees a subset of failures. Combined with brief framing of "be surgical, only the listed findings", subagents leave known-bad content in place.

**Fix**:
1. Remove `... and N more` truncation from `content_reality`, `gotcha_causal_anchor`, `gotcha_distinct_from_guide`, and `claude_readme_consistency` check outputs. Emit ALL findings up to a hard cap of 50 per check.
2. Add a `tokenClass` field to each finding: `phantom_file_path`, `unsupported_symbol`, `embedded_yaml_drift`, `missing_anchor_token`, etc.
3. Modify the fix-subagent brief construction in `internal/workflow/recipe_content_fix_gate.go` to:
   - Group findings by `tokenClass`
   - For each token class, emit `EXHAUSTIVELY GREP across all 6 files for: <token_class_examples>; fix every occurrence, not just the listed lines.`
   - Add a closing instruction: `BEFORE returning, run a final grep for each token class — if any remain, fix them too.`

**Files to touch**:
- `internal/tools/workflow_checks_reality.go` — remove truncation, add token classification
- `internal/tools/workflow_checks_causal_anchor.go` — same
- `internal/workflow/recipe_content_fix_gate.go` — brief construction
- New tests: `recipe_content_fix_gate_test.go` with replay of v23 SA2/SA3/SA4 inputs verifying brief includes the token-class grep instruction.

**Expected impact**: 23 min wall reduced to ~5 min. One fix-subagent dispatch per failed deploy-step gate instead of three.

### 7.2 Embedded-yaml awareness — `comment_ratio` reads README, not zerops.yaml

**Problem**: `comment_ratio` and `comment_specificity` parse the YAML block embedded in the README's IG step 1 fenced ```yaml — NOT the on-disk `zerops.yaml`. SA2 wasted 5m 50s editing the wrong files.

**Fix**:
1. Document this in the check's `Detail` field: `"This check reads the YAML block embedded in <hostname>/README.md inside the IG step 1 \`\`\`yaml fence — NOT the on-disk zerops.yaml. Fix the embedded copy. Optionally also sync the on-disk file."`
2. Add a finalize-step `embedded_yaml_in_sync` check that fails if the README's embedded YAML differs from the on-disk `zerops.yaml`. This forces both to stay in sync.

**Files to touch**:
- `internal/tools/workflow_checks_comment_ratio.go` — update Detail message
- New: `internal/tools/workflow_checks_embedded_yaml_sync.go`
- New tests covering both behaviors

**Expected impact**: SA2 wouldn't fix the wrong file. Saves ~5 min per regression.

### 7.3 Scaffold-brief: nodenext requires `nest start`, not raw `ts-node`

**Problem**: workerdev scaffold subagent emitted `start:dev` as raw `ts-node -r tsconfig-paths/register src/main.ts` while keeping `nest new`-default `tsconfig.json` with `module: nodenext`. Result: `Cannot find module './app.module.js'` at first dev start. apidev escapes this because `nest start --watch` handles it.

**Fix**: add to scaffold-subagent-brief block in `internal/content/workflows/recipe.md`:

> **NestJS dev runner ↔ tsconfig contract**: if `tsconfig.json` has `"module": "nodenext"` (the default emitted by `nest new`), `package.json`'s `"start:dev"` MUST use `nest start --watch` (which proxies through `@nestjs/cli`'s bundler). If you replace `nest start` with raw `ts-node`, you MUST also flip tsconfig to `"module": "commonjs"` + `"moduleResolution": "node"` AND strip `.js` suffixes from relative imports — otherwise dev startup fails with `Cannot find module './X.js'`. Workers without a built-in dev runner: pick one and stick with it.

**Files to touch**:
- `internal/content/workflows/recipe.md` — scaffold-subagent-brief section
- `internal/content/workflows/recipe_topic_registry.go` — eager topic registration if appropriate
- Test: existing scaffold-brief shape tests should assert this paragraph's presence

**Expected impact**: workerdev nodenext class CRIT eliminated. Saves ~5 min for 1-fix scaffold cleanup + tsconfig replacement.

### 7.4 `execOnce` semantics topic — kill the "burn trap" folk-doctrine

**Problem**: agent invented "zsc execOnce burn" terminology and shipped it in `apidev/CLAUDE.md`. The mental model is wrong: `appVersionId` is per-deploy, not per-workspace.

**Fix**:
1. Add a new eager topic `execOnce-semantics` registered at `EagerAt: SubStepInitCommands`:
   ```
   `zsc execOnce ${appVersionId}` keys on the deploy version — each new deploy gets a fresh `appVersionId`,
   so the lock is NEVER pre-burned by a prior deploy from a different version. If your first-deploy initCommand
   silently no-ops (✅ in <100ms with no body output), the cause is your script — NOT a "burned key":
   - Check for early `process.exit(0)` or unhandled-rejection swallow
   - Check the runtime can resolve your script (ts-node + tsconfig-paths + module resolution)
   - Check stdout buffering — pipe through `node --enable-source-maps` if ts-node, or use `console.log + process.stdout.write` pairs
   The escape hatch (rotate appVersionId) requires editing a tracked file and redeploying — but it's a workaround,
   not a diagnosis. Do NOT codify "burn trap" terminology in CLAUDE.md or README — it's wrong.
   ```
2. Add a content check `claude_md_no_burn_trap_folk` that fails if any CLAUDE.md or README contains the substring `execOnce burn` or `burn trap` near `execOnce`.

**Files to touch**:
- `internal/content/workflows/recipe.md` — new topic block
- `internal/workflow/recipe_topic_registry.go` — register topic
- `internal/tools/workflow_checks_claude_md_folk.go` — new check (or extend existing claude_readme_consistency)
- Test: replay v23 apidev/CLAUDE.md → check fires

**Expected impact**: prevents this folk-doctrine class from shipping. Future runs that hit the silent-no-op pattern get accurate diagnostic guidance.

### 7.5 `zerops_deploy` MCP-channel docs — kill the "no parallel deploys" myth

**Problem**: agent attributed `Not connected` MCP transport errors to a non-existent platform rule and shipped that misattribution in TIMELINE.md.

**Fix**: amend `zerops_deploy` tool description in `internal/tools/deploy.go`:

> **Channel-blocking**: this call holds the MCP STDIO channel for the duration of the build (typically 60–120s). DO NOT issue other `zerops_*` calls in the same response — they will return `Not connected` (an MCP transport error, not a platform rejection). Serialize all deploys and verifications.

Also update `recipe.md` deploy step's "Constraints" section with the same note.

**Files to touch**:
- `internal/tools/deploy.go` — tool description
- `internal/content/workflows/recipe.md` — Deploy section Constraints

**Expected impact**: prevents "parallel deploys rejected" misattribution from recurring. Saves ~2 sec per accidental parallel call (low cost) + prevents downstream misinformation in published TIMELINE.

### 7.6 `content_reality` finding-class grouping — fix at the report layer

**Problem**: when `content_reality` finds 8 phantom paths across 3 files, the report shows them as 8 separate findings. The agent fixes them one at a time. With proper grouping ("phantom_file_path: 8 instances across {file glob}"), the agent could grep + fix in one pass.

**Fix**: modify `internal/tools/workflow_checks_reality.go` to:
- Compute findings, then group by `(tokenClass, tokenValue)` tuple
- Emit each group as a single finding with `instances: [{file, line, context}, ...]` array
- Brief construction picks up the grouped shape and instructs the subagent: `"phantom_file_path '${dist/migrate.js}' appears in 4 places: <list>. Fix all 4."`

**Files to touch**:
- `internal/tools/workflow_checks_reality.go`
- Test: ensure existing fail behavior preserved + new grouped output asserted

**Expected impact**: combined with §7.1, removes the "and N more" hide-and-seek pattern entirely.

### 7.7 Optional: snapshot-dev redundancy elimination

**Problem**: snapshot-dev redeploys all 3 services after the feature subagent finishes, taking ~3 minutes. Its purpose is to bake feature commits into the deployed artifact (subdomain serves the deployed artifact, not the SSHFS mount).

**Possible fix**: teach the workflow that cross-deploy can read directly from the SSHFS mount (since cross-deploy already runs build on the mount). Skipping snapshot-dev would save ~3 min per run, but breaks the dev-browser-walk because the browser would be hitting the previous deploy artifact.

**Recommendation**: leave as-is. The 3-min cost buys (a) verifies the prod build succeeds before stage promotion, (b) fresh dev artifact matches dev source. Not worth the architecture change.

---

## 8. Expected v24 grade

With §7.1 + §7.2 + §7.3 + §7.4 + §7.5 + §7.6 shipped:

- **S**: A (no change — code-review CRIT capture already at A)
- **C**: A (38% → ~50%+ pure-invariant gotchas if the "burn trap" pattern is purged + nodenext fix understood)
- **O**: B+ → A− (23 min README loop → ~5 min; total wall ~96 min → ~80 min)
- **W**: A− (post-writer fix-subagent dispatches converge in 1 round instead of 3)

**Overall: A−** — same as v20 (which was the prior peak), reached through structural improvement, not luck.

---

## 9. Risk analysis + validation plan

### Risk per fix

| Fix | Risk | Mitigation |
|---|---|---|
| §7.1 (truncation removal) | High-volume `content_reality` failures could blow up brief size | Hard cap at 50 findings + size-budget guard test |
| §7.2 (embedded-yaml) | Existing checks may break if Detail field is asserted in tests | Update test fixtures, run full test suite |
| §7.3 (nodenext) | Adds 1 paragraph to scaffold brief — within size budget | Brief size guard test |
| §7.4 (execOnce topic) | New eager topic adds ~600 B to init-commands substep response | Within v8.84 substep size budget |
| §7.5 (deploy MCP docs) | Tool description change — affects schema | Schema-stability test |
| §7.6 (finding grouping) | Output shape change — downstream parsers may break | Update workflow check tests |
| §7.7 (snapshot-dev) | Skipped — too risky for marginal gain | n/a |

### Validation plan

1. **Unit tests**: each new check + brief-construction change gets a RED test before implementation.
2. **Replay tests**: v23 session-log `tool_result` payloads for SA2/SA3/SA4 fed into the new brief-construction code; assert brief includes token-class grep instruction.
3. **Shadow tests**: run the new `claude_md_no_burn_trap_folk` check against v23 apidev/CLAUDE.md → assert fail. Run against v22 → assert pass (no false positive).
4. **Live validation**: v24 run on the same nestjs-showcase tier. Pass criteria:
   - Wall ≤100 min (was 119)
   - ≤1 fix-subagent dispatch (was 3)
   - 0 mentions of "burn trap" / "burn key" in shipped CLAUDE.md
   - 0 mentions of "parallel cross-deploys" / "Not connected for simultaneous deploys" in TIMELINE
   - Code-review close: 0 CRIT (preserve v22+v23 cleanliness)

---

## 10. Non-goals

1. **Don't change the deploy clustering shape.** §6 confirmed all 5 deploy clusters are necessary. The user's perception of "senseless deploys" is rhythmic, not substantive — fixing the rhythm would require an architecture change that's out of scope.
2. **Don't add per-framework hardcoding.** The nodenext fix in §7.3 is framework-aware (it names NestJS) but the rule is general (ts-node + nodenext = bad). The brief should describe the contract, not the framework.
3. **Don't compress content further.** v23 at 6/3/4 gotchas + 4/4/4 IG hit the right balance. The compression-with-quality story is what's working — don't push it harder.
4. **Don't add a v22-style gotcha-origin-diversity check.** v23 already shows 38% pure-invariant origin without that check. The compression toward authentic content emerged organically from v8.78–v8.85's quality pressure.
5. **Don't try to fix the `Not connected` MCP transport.** §7.5 documents the limitation; properly fixing it is an MCP server architecture change that's separate scope.

---

## Appendix — File inventory of v8.86 changes

```
internal/tools/workflow_checks_reality.go              # remove truncation, add tokenClass + grouping
internal/tools/workflow_checks_causal_anchor.go        # remove truncation
internal/tools/workflow_checks_comment_ratio.go        # update Detail field to mention embedded yaml
internal/tools/workflow_checks_embedded_yaml_sync.go   # NEW finalize-step check
internal/tools/workflow_checks_claude_md_folk.go       # NEW check (or extend claude_readme_consistency)
internal/tools/deploy.go                                # tool description: blocks MCP channel
internal/workflow/recipe_content_fix_gate.go           # brief construction: token-class grep instruction
internal/workflow/recipe_topic_registry.go             # register execOnce-semantics topic
internal/content/workflows/recipe.md                    # scaffold-brief nodenext callout, deploy constraints, execOnce topic
internal/content/workflows/recipe_topic_registry_test.go # eager-topic test for execOnce-semantics
```

Plus tests:

```
internal/tools/workflow_checks_reality_test.go         # truncation removed, grouping asserted
internal/tools/workflow_checks_causal_anchor_test.go   # same
internal/tools/workflow_checks_embedded_yaml_sync_test.go # NEW
internal/tools/workflow_checks_claude_md_folk_test.go  # NEW
internal/workflow/recipe_content_fix_gate_test.go      # v23 SA2/SA3/SA4 replay
internal/content/workflows/recipe_test.go              # nodenext paragraph + deploy constraints + execOnce topic presence
```

Total: ~10 source files + ~6 test files. Estimated effort: 1 day implementation + 0.5 day test buildout.
