# redundancy-map.md

**Purpose**: enumerate facts delivered to the same agent at the same phase via **3+ independent paths**. Each redundancy has evidence pointers (file:line or trace timestamp).

Redundancy ≠ reinforcement. Reinforcement = same rule stated in sub-agent brief and enforced by a check = good. Redundancy = the main agent or a sub-agent reads the *same content* from multiple sources at the same point in time = paid-for duplication that should collapse in the rewrite.

---

## 1. SSH-only executable rule (main + all scaffold sub-agents)

**Evidence** — same semantic rule delivered via ≥3 paths:

| Path | Source | Delivery point |
|---|---|---|
| A | `where-commands-run` block body | recipe.md:1830-1872 — **eager at deploy.deploy-dev** for main-agent entry guide |
| B | `scaffold-subagent-brief` permit/forbid section | recipe.md:790-1125 — **eager at generate.scaffold** for main, AND embedded in every scaffold dispatch brief (recipe.md:1000+ zone — "SSH-only executables" MANDATORY sentinel) |
| C | Explicit `Permitted tools / Forbidden` lists in every scaffold dispatch brief | e.g. `flow-showcase-v34-dispatches/scaffold-apidev.md` lines 20-32 (verbatim: "Bash ONLY via `ssh apidev \"...\"`") |
| D | `zerops_workflow action=complete` error response embeds SSH reminder in `SUBAGENT_MISUSE` class | server-side |

**Consuming agent**: scaffold-{api,app,worker}dev — each reads the SSH rule **4 times** (A embedded in their brief copy of scaffold-subagent-brief, B repeated as explicit permit/forbid list, C repeated again in dispatch preamble, D delivered if the agent tries to break it). Main agent reads A + B directly.

**Defect class closed by this redundancy**: v17 (SSHFS write-surface-as-execution-surface). Historically kept because the rule was load-bearing across a v17 regression.

**v34 trace evidence**: 0 zcp-side executions observed — rule holds. Redundancy did not cost correctness; it costs context bytes (~1.5 KB repeated 4× per scaffold sub-agent dispatch).

**Recommendation for step 3**: collapse into one atom (`principles/where-commands-run.md`) referenced by every brief. Transmitted copy stays — it's the leaf artifact. The source duplication across blocks goes.

---

## 2. File-op sequencing (Read-before-Edit)

**Evidence** — ≥3 delivery paths:

| Path | Source | Delivery point |
|---|---|---|
| A | `scaffold-subagent-brief` "File-op sequencing" MANDATORY sentinel | recipe.md:790-1125 inside block |
| B | Explicit dispatch-brief repetition | `flow-showcase-v34-dispatches/scaffold-apidev.md` lines 20-32 (verbatim: "every Edit must be preceded by a Read of the same file in this session") |
| C | Server-side Read-before-Edit sentinel enforced by Edit tool (v8.97 Fix 3) | raises error if Edit precedes Read |
| D | `dev-deploy-subagent-brief` carries it again for feature sub-agent | recipe.md:1675-1828 |
| E | `content-authoring-brief` carries it again for writer | recipe.md:2390-2736 |

**Consuming agent**: every sub-agent reads the rule 2-3× in-brief. v34 flagged 0 "File has not been read yet" errors → rule holds.

**Defect class closed**: v32 dispatch-compression dropped this rule; v8.97 fixed via enforcement + MANDATORY repetition.

**Redundancy cost**: same sentinel present in scaffold + feature + writer + code-review briefs. Roughly 200 bytes × 5 briefs = 1 KB of repeated sentinel text per run.

**Recommendation**: single `principles/file-op-sequencing.md` referenced by briefs. Because enforcement is server-side (v8.97), the brief sentinel can collapse to one short line ("Read before Edit — enforced") rather than the current paragraph.

---

## 3. Forbidden-tools list (every sub-agent)

**Evidence** — structurally identical list in every dispatch:

| Dispatch | Forbidden list | Line(s) |
|---|---|---|
| scaffold-apidev | `zerops_workflow`, `zerops_import`, `zerops_env`, `zerops_deploy`, `zerops_subdomain`, `zerops_mount`, `zerops_verify` | dispatch lines 22-28 |
| scaffold-appdev | same 7 tools | lines 22-31 |
| scaffold-workerdev | same 7 tools | lines 22-30 |
| feature | same 7 tools | lines 35-37 |
| writer | same 7 tools | lines 31-33 |
| code-review | same 7 tools + `zerops_browser`, `agent-browser` (2 extras) | lines 20-22 |

**Consuming agent**: every sub-agent reads the same 7-element list. Total duplication: 6 dispatches × ~200 bytes = 1.2 KB per run.

**Defect class closed**: v25 substep-bypass (sub-agent called `zerops_workflow` at spawn, broke attestation ordering). v8.90 added server-side SUBAGENT_MISUSE — this block is now **server-enforced**; the brief list is redundant except code-review's extra 2 (`zerops_browser` / `agent-browser`).

**Recommendation**: server declares the denylist; brief mentions it only where rules differ (code-review's 9-item list is the exception, worth stating). Collapse 6 identical lists to a single atom + one-line exception for code-review.

---

## 4. NATS separate user/pass (structured credentials principle)

**Evidence** — delivered 4× to scaffold-apidev + scaffold-workerdev:

| Path | Source | Delivery |
|---|---|---|
| A | `scaffold-subagent-brief` "Principle 5 — structured credentials" | recipe.md:790-1125 |
| B | Prior Discoveries block in apidev dispatch (7 entries include "NATS URL-embed restriction") | dispatch top |
| C | Prior Discoveries block in workerdev dispatch (1 entry: NATS queue group — adjacent to separate-options principle) | dispatch top |
| D | `worker-setup-block` topic (delivered as on-demand `zerops_guidance` @ 10:21:56) | recipe.md:727-736 |
| E | `zerops_record_fact` confirmation after apidev records NATS separate options (scope=both) → flows into feature sub-agent's Prior Discoveries | fact-scope=both → substep order index 0 (scaffold recorded) feeds substep order index 4 (subagent) |

**Consuming agents**: apidev (reads 2×: A+B), workerdev (reads 2×: A+C), feature (reads via Prior Discoveries from scaffolds), code-review (reads via Prior Discoveries).

**Defect class closed**: v22 (NATS URL-embedded creds recurrence despite gotcha README). v8.86-era hardening. Still firing in v34 (3× record_fact from sub-agents).

**Redundancy cost**: ~400 bytes × 4 paths × 3 consuming agents ≈ 5 KB over the run.

**Recommendation**: a single `principles/structured-credentials.md` atom; scaffold briefs reference it; facts log flows the "here's what we actually did" confirmation. The principle doesn't need to be stated in both (A) the principle block and (B) a Prior Discovery entry stating the principle was followed.

---

## 5. 0.0.0.0 bind / platform principle 2

**Evidence** — ≥4 paths per scaffold agent:

| Path | Source | Delivery |
|---|---|---|
| A | `scaffold-subagent-brief` principle 2 | recipe.md:790-1125 |
| B | Dispatch eager-inlined principle restatement | dispatch header |
| C | Prior Discoveries entry for apidev (1 entry) and appdev (1 entry) | dispatch top |
| D | `dev-server-host-check` eager topic | recipe.md:711-715 — eager at SubStepScaffold |
| E | `zerops_record_fact` scope=both from scaffold agents' runs (apidev 7 facts include it; appdev 1 fact is exactly this) | fact log |

**Consuming agents**: all 3 scaffolds + feature (reads from Prior Discoveries indirectly) + code-review.

**Recommendation**: same collapse pattern — one principle atom, one check, no Prior Discovery restatement of a thing the brief already stated.

---

## 6. TodoWrite vs `zerops_workflow` — parallel workflow model

**This is the marquee redundancy of v34.** The server maintains authoritative substep state via `zerops_workflow`; the main agent maintains a parallel TodoWrite list that re-renders **12 times** during the run.

**Evidence** — v34 main trace, 12 TodoWrite events ([flow-showcase-v34-main.md:L12-L29](../01-flow/flow-showcase-v34-main.md) flagged-events header):

| # | Timestamp | Type | Items | Classification |
|---|---|---|---|---|
| 1 | 10:19:15 | initial write | 5 | n=5 completed=0 pending=4 in_progress=1 |
| 2 | 10:21:47 | full-rewrite | 9 | different content |
| 3 | 10:36:34 | full-rewrite | 7 | different content |
| 4 | 10:41:08 | full-rewrite | 11 | different content |
| 5 | 10:41:51 | check-off | 11 | same content, status updated |
| 6 | 10:55:43 | full-rewrite | 6 | different content |
| 7 | 11:02:48 | full-rewrite | 5 | different content |
| 8 | 11:12:01 | full-rewrite | 5 | different content |
| 9 | 11:15:34 | full-rewrite | 4 | different content |
| 10 | 11:19:37 | full-rewrite | 4 | different content |
| 11 | 11:30:09 | full-rewrite | 4 | different content |
| 12 | 11:31:00 | full-rewrite | 4 | different content |

10 of 12 = full-rewrite. 1 = initial write. Only 1 = pure check-off.

**Parallel authoritative source**: at every `zerops_workflow action=complete step=X substep=Y` (26 such calls in the v34 main trace), the server returns the authoritative substep list with status per substep. The main trace shows the agent re-fires TodoWrite **immediately after** most complete calls — example: 10:41:08 TodoWrite rewrite fires 10 seconds after `complete step=deploy substep=deploy-dev` returns guidance at 10:40:58.

**Why the agent does this**: the step-entry guidance reads like fresh planning context. Each substep's `detailedGuide` says "your tasks for this phase are X/Y/Z" — the agent interprets this as "rewrite the TodoWrite to match the current phase's task list" rather than "check off the substep you just completed."

**Redundancy cost**:
- Context: each TodoWrite full-rewrite consumes ~160 bytes response + ~500 bytes request → ~8 KB over the run purely for TodoWrite traffic
- Cognitive: the agent maintains two workflow models, which must be reconciled at every turn (reconciliation is mostly successful in v34 but is a constant tax)
- Substep attestation is not affected (v8.90 held: 0 out-of-order substep attestations)

**Defect class**: v34 12-TodoWrite-rewrite class, named in plan §0 and RESUME decision #3. Per decision #3, the rewrite keeps TodoWrite but **as a pure check-off mirror** — the full-rewrite pattern at step-entries is the thing to eliminate.

**Recommendation for step 3**: (a) rewrite step-entry guidance so it does NOT read as fresh planning context ("your tasks for deploy-dev are" → "deploy-dev substep: attest when `zerops_deploy targetService=X setup=dev` returns DEPLOYED for all targets"); (b) explicit sentinel "TodoWrite mirrors substep state; do NOT rewrite at step-entries"; (c) consider dropping TodoWrite entirely (open decision).

---

## 7. Fact-recording mandate (deploy phase)

**Evidence** — same rule delivered to main agent 3×:

| Path | Source | Delivery |
|---|---|---|
| A | `fact-recording-mandatory` eager at SubStepDeployDev | recipe.md:1423-1466 |
| B | `dev-deploy-subagent-brief` has its own fact-recording section | recipe.md:1675-1828 |
| C | `content-authoring-brief` tells the writer to consume facts | recipe.md:2390-2736 — redundant-to-the-mandate since writer doesn't record, only consumes |

For the feature sub-agent: additional 4th path — the brief's own body + classification taxonomy (`framework-quirk`, `library-meta`, etc.) is effectively a 2-part statement of the same rule (record + classify at the same moment).

**Consuming agent**: main agent (A+B delivered), feature sub-agent (inherits A via main, receives B in its dispatch, C is for writer only).

**Recommendation**: collapse to one `principles/fact-recording.md` + a classification-taxonomy atom consumed by writer; main brief references the principle; feature brief references via 1-line pointer.

---

## 8. Dispatcher-vs-transmitted-brief audience mixing

**Evidence** — every sub-agent brief block contains dispatcher-facing instructions alongside sub-agent-facing content. Example from `scaffold-subagent-brief` (recipe.md:790-1125):

| Line-range class | Audience | Example |
|---|---|---|
| "Main agent: dispatch 3 parallel scaffold sub-agents..." | dispatcher (main) | instruction to compose the dispatch |
| "Sub-agent: your task is to scaffold..." | sub-agent (scaffold-X) | task context |
| "Compress everything except the MANDATORY sentinels..." | dispatcher | brief-composition rule |
| "You MUST record_fact for every platform observation..." | sub-agent | in-task behavior |

**Consuming agent**: the *same block* is read by main (as dispatcher) and by sub-agent (as task-brief via the dispatched Agent prompt). v32's dispatch-compression class occurred precisely here: main compressed the block and dropped Read-before-Edit **because it couldn't distinguish dispatcher-facing from sub-agent-facing lines**.

**This is redundancy of audience, not content** — but it drives misroute too (see `misroute-map.md` item about dispatch-compression).

**Recommendation**: physical separation. `briefs/scaffold/_shared-mandatory.md` = what the sub-agent receives. `DISPATCH.md` (outside `content/workflows/recipe/`) = instructions to the main agent on how to compose it. This is architectural principle #2 in README.md §5.

---

## 9. Platform principles 1-6 (scaffold-subagent-brief)

**Evidence** — the 6 principles are named in the scaffold brief AND checked for adherence AND some are delivered via separate on-demand `zerops_guidance` topics:

| Principle | In scaffold brief? | Separate topic? | Check covers? |
|---|---|---|---|
| 1. Graceful shutdown | yes (recipe.md:790-1125) | — | worker_shutdown_gotcha (readmes) |
| 2. 0.0.0.0 bind | yes | `dev-server-host-check` (eager) | — |
| 3. Trust proxy | yes | — | — |
| 4. Competing consumer (NATS queue group) | yes | `worker-setup-block` on-demand | worker_queue_group_gotcha (readmes) |
| 5. Structured credentials (NATS separate user/pass) | yes | `worker-setup-block` | — |
| 6. Stripped build root | yes | — | — |

For principle 2, the rule is in (a) scaffold brief, (b) dev-server-host-check eager topic, (c) Prior Discoveries for scaffolds that haven't-yet-but-will-need-it, (d) the agent's own record_fact output.

**Recommendation**: one atom per principle; scaffold brief references the atom; check guards the atom's requirement. Separating principles from brief body lets the writer consume them as reference instead of as instruction.

---

## 10. Size summary — redundancy bytes per run

Approximate total redundant byte cost per showcase run (double-counts excluded):

| Redundancy class | ~Bytes per run | Fix mechanism |
|---|---:|---|
| SSH-only executable rule across 4 paths | ~6 KB | one principle atom |
| File-op sequencing across 5 paths | ~1 KB | enforcement + 1-line reference |
| Forbidden-tools list × 6 dispatches | ~1.2 KB | server denylist + exception atom |
| NATS separate user/pass × 4 paths × 3 consumers | ~5 KB | principle + facts without restatement |
| 0.0.0.0 bind × 4+ paths | ~3 KB | principle + check |
| Platform principles 1-6 in multiple locations | ~4 KB | principle atoms |
| Fact-recording mandate × 3 paths | ~2 KB | one principle |
| TodoWrite re-renders × 12 | ~8 KB | eliminate full-rewrites |
| Dispatcher/transmitted mixing | structural (not measurable as bytes) | physical separation |
| **Approximate redundant context per run** | **~30 KB** | |

Total main + sub-agent context per v34 run is order ~400 KB; redundant ~30 KB ≈ 7.5%. Not huge as a raw percentage, but the cognitive cost (agent reconciling multiple statements of the same rule, TodoWrite full-rewrites) is what drove v34's 12-rewrite cost class and the 3-4 convergence rounds.
