# misroute-map.md

**Purpose**: enumerate facts delivered **after** the phase they would have governed (or, more generally, delivered to the wrong audience or at the wrong time). Each entry cites a defect class from v20-v34 and the specific delivery-timing mismatch.

Misroute is different from redundancy: redundancy = same fact delivered multiple times (fine-in-principle, expensive-in-bytes). Misroute = a fact arrives too late, or at an agent that can't act on it, making the delivery cosmetic.

---

## 1. v25 substep-bypass class — architectural misroute risk (CLOSED by v8.90, stays live)

**Defect class**: v25 — main agent did 40 minutes of deploy work silently and backfilled attestations afterward. Sub-agents at spawn called `zerops_workflow action=complete` for their parent substep, which broke ordering and skipped guidance delivery.

**What was misrouted**:
- Topic `dev-deploy-subagent-brief` was previously **eagerly injected at deploy phase entry** (SubStepDeployEntry). Main agent read it, dispatched the feature subagent, came back with results. But substeps `deploy-dev`, `start-processes`, `verify-dev`, `init-commands` were implicitly already "in flight" — the agent attested them after the fact.
- Topic `readme-with-fragments` was similarly eagerly injected at `feature-sweep-stage` **before** the writer dispatch was even considered. Writer received (or was assumed to already hold) content that main had not yet consumed in attestation sequence.

**v8.90 fix**: de-eager these two topics. `dev-deploy-subagent-brief` now delivered as the **substep-return payload** at `complete step=deploy substep=init-commands` (i.e., the guide arrives with the completion of init-commands, naming subagent as the next substep). `content-authoring-brief` delivered as substep-return at `complete step=deploy substep=feature-sweep-stage` (naming readmes as next).

**Evidence of the fix holding in v34**:
- Main trace event #71 (`complete step=deploy substep=init-commands`) returns 24193 B with 21840 B guidance — the subagent-brief carries forward.
- Main trace event #135 (`complete step=deploy substep=feature-sweep-stage`) returns 27927 B — the largest return in the whole trace, and it carries `content-authoring-brief` forward.
- 0 out-of-order substep attestations in v34 main trace.

**Why this stays live as a risk class**:
- The architectural misroute pattern (guidance arrives after the work) is still easy to regress if any *future* substep addition forgets the de-eager pattern.
- The fix relies on `IncludePriorDiscoveries` + substep-return delivery + substep-order index maintained in [`recipe_brief_facts.go:L204-L219`](../../../internal/workflow/recipe_brief_facts.go). Adding a new substep without updating that list creates a silent misroute (Prior Discoveries filter wouldn't know the new substep's upstream set).
- **Gap class**: if any sub-agent (feature, writer) violates the "no `zerops_workflow` call" rule at its spawn, the server raises SUBAGENT_MISUSE — but if a brief-composition bug causes the dispatcher to include an early `zerops_workflow` request in the dispatched prompt, the sub-agent obediently executes it. This is exactly v32's dispatch-compression class.

**Recommendation for step 3**: make the substep-order index a first-class invariant verified by a server-side test (phase-and-substep ordering is authoritative). Any new substep must update three things in lockstep: `recipe_substeps.go`, `subStepToTopic()` in `recipe_guidance.go`, and the substep-order map in `recipe_brief_facts.go`. Principle #4 + #2 in architectural-principles list covers this.

---

## 2. Platform principle 4 (competing-consumer / NATS queue group) — delivered after scaffold, needed during feature

**What was misrouted**:
- Principle 4 is embedded in `scaffold-subagent-brief` (recipe.md:790-1125), which is delivered to scaffold sub-agents during generate.scaffold.
- The **feature** sub-agent, which wires NATS publishing in apidev and NATS subscription in workerdev, **needs** to know queue-group semantics (subject `jobs.process`, queue `workers`) for CROSS-CODEBASE contract consistency.
- The feature sub-agent's brief (`dev-deploy-subagent-brief`) references the principle by name but does NOT carry the full semantics. Feature agent receives the rule via Prior Discoveries if-and-only-if a scaffold sub-agent recorded a fact about it (v34: workerdev did record a fact — scope=both — so it flowed in).

**Evidence**:
- scaffold-workerdev dispatch: "Principle 4" referenced with queue-group body ([`flow-showcase-v34-dispatches/scaffold-workerdev.md`](../01-flow/flow-showcase-v34-dispatches/) — in eager-inlined principles).
- feature dispatch: brief references principle but doesn't restate ([`flow-showcase-v34-dispatches/implement-all-6-nestjs-showcase-features.md`](../01-flow/flow-showcase-v34-dispatches/)).
- v34 feature sub-agent recorded a cross_codebase_contract fact for NATS subject+queue (scope=both) — delivered to downstream writer + code-review.

**Defect class**: v22 NATS URL-embedded creds recurrence — historically, feature code got written with URL-embedded auth because cross-scaffold contract wasn't delivered at feature time. Closed in scaffold by Principle 5 delivery; feature layer relies on Prior Discoveries propagation.

**Misroute is conditional**: works *only if* the scaffold sub-agent records a fact. If the scaffold agent hits the rule but doesn't call `zerops_record_fact`, the rule is not delivered to the feature agent.

**Recommendation**: symbol-naming contract (architectural principle #3). Let scaffolds **declare** the contract in a structured object (NATS subject, queue name, env var naming) separate from Prior Discoveries / facts log. Feature sub-agent receives this contract as a plan field, not as a facts-log sidebar.

---

## 3. Platform principle 1 (graceful shutdown) — delivered to scaffold, not enforced until close review

**What was misrouted**:
- Scaffold sub-agent receives "graceful shutdown" principle in its brief.
- Scaffold sub-agent records it as a fact (v34: apidev 1× scope=both, workerdev 1× scope=both).
- The actual **check** that enforces drain-code-block presence runs at deploy.readmes:
  - `hostname_worker_shutdown_gotcha` — validates README knowledge-base has a gotcha about graceful shutdown (readmes substep).
  - `hostname_drain_code_block` — validates README has a fenced code block with both drain+exit calls (readmes substep).
  - NO check validates the actual scaffold code has drain handling — only that the README documents it.

**Consequence**: v34 workerdev gotcha #3 was a SASL/password issue classified as self-inflicted per its own manifest → but shipped as a gotcha (v34 defect class: manifest↔content inconsistency). The principle-4 gotcha check surface validated presence of drain-topic, not correctness of shutdown handling.

**Timing misroute**:
- Correctness window: during scaffold (sub-agent writes shutdown handler).
- Validation window: readmes (main/writer).
- Gap: nothing between scaffold and readmes runs `pkill -TERM` or equivalent on the sub-agent's output to verify shutdown actually drains.

**Recommendation**: principle #1 in architectural invariants — "every content check has an author-runnable pre-attest form." Currently the shutdown check is content-shape (gotcha exists with right tokens), not behavior. The rewrite should add a runnable form: e.g. `ssh workerdev "kill -TERM $(pgrep -f node) && sleep 3 && ssh workerdev 'ps aux | grep nats-subscribe'"` = check the subscriber exited cleanly. Principle #1 covers this; the author runs it before attesting, the server gate becomes confirmation not discovery.

---

## 4. Prior Discoveries → code-review — reads facts produced by scaffolds, but not facts produced by writer

**What was misrouted**:
- `code-review-subagent` topic declares `IncludePriorDiscoveries=true`. At close.code-review dispatch, the server prepends facts filtered to substep order index < 12 (code-review index).
- Writer sub-agent does NOT call `zerops_record_fact` — it consumes facts from the log and writes content.
- Code-review therefore receives: scaffold-recorded facts (yes), feature-recorded facts (yes), writer-recorded facts (none).
- Writer's *decisions* (what got routed to discarded vs published-as-gotcha per `ZCP_CONTENT_MANIFEST.json`) are NOT in the facts log — they live in the manifest JSON.
- Code-review has no mechanism to read the manifest.

**v34 evidence**: workerdev DB_PASS gotcha shipped as published — the writer manifest classified it as `routed_to=claude-md` but the README still carried it as gotcha. Code-review could not have caught this because:
1. The fact that the manifest-vs-content contradiction existed was not in the Prior Discoveries block.
2. Code-review has no explicit instruction to open `ZCP_CONTENT_MANIFEST.json`.
3. The `writer_manifest_honesty` check caught it via server-side Jaccard — but only at `complete step=deploy substep=readmes`, not before code-review ran.

**Defect class**: v34 manifest↔content inconsistency (DB_PASS).

**Recommendation**: two fixes converge on this:
1. (gap-map) — writer_manifest_honesty check expanded beyond (discarded, published_gotcha) to cover all (routed_to_X, published_as_Y) dimensions.
2. (misroute-fix) — make manifest a readable input to code-review sub-agent. Either include manifest summary in the code-review brief via Go interpolation, or explicitly instruct code-review to `Read ZCP_CONTENT_MANIFEST.json` as part of its feature-coverage scan.

---

## 5. Writer citation-map → writer consumption — semi-misroute (works, inefficiently)

**What was misrouted**:
- `content-authoring-brief` tells the writer to consult the citation map BEFORE writing. The citation map is not a file — it's a list of `zerops_knowledge` topic names the brief lists (init-commands, rolling-deploys, object-storage, http-support, deploy-files, readiness-health-checks).
- v34 writer made **6 `zerops_knowledge` calls** on exactly those topics (trace). Each topic returned ~2-5 KB of reference content.
- Total citation-map consultation ~18 KB of fresh context pulled at writer-time.
- The same topic bodies exist in the session's facts log (scope=both) via scaffold-recorded facts — the writer read them TWICE (once from facts, once from knowledge).

**Evidence**: writer dispatch `content-authoring-brief` references citation-map at recipe.md:2390-2736 zone; trace shows 6 `zerops_knowledge` calls during writer execution.

**Cost**: citation-map consultation is ~18 KB pulled at writer time but ~half the content is already in Prior Discoveries.

**Recommendation**: rather than naming topics for the writer to fetch, preload them — the writer brief already includes Prior Discoveries; the citation map should be an inline block in the brief (one time, stitched at dispatch), not a set of knowledge calls the writer fires at runtime. Saves 6 round-trips.

This is a borderline misroute — the calls *work*, the content arrives, but it's inefficient relative to stitching-at-dispatch-time.

---

## 6. Main agent receives sub-agent briefs as part of its own guide

**What was misrouted**:
- `dev-deploy-subagent-brief` (recipe.md:1675-1828, 154 lines) is delivered to the **main agent** as the substep-return payload at `complete step=deploy substep=init-commands` (v8.90 de-eager).
- The main agent is the dispatcher — it compresses this into the Agent-tool prompt.
- v34 dispatch: feature brief prompt_len=14816 chars. Block content: ~7-8 KB raw text. Delta is plan-field interpolation + feature list expansion.

**This is BOTH a redundancy AND a misroute**:
- Redundancy: main reads the brief content (8 KB) + feature agent reads the brief content (same 8 KB in interpolated dispatch).
- Misroute: main agent doesn't need the brief's *internal* task structure; it needs to know what-to-include-verbatim and what-to-compress. That instruction is mixed into the same block.

**Evidence**: see redundancy-map.md #8 (dispatcher-vs-transmitted mixing).

**Recommendation**: architectural principle #2 (transmitted briefs are leaf artifacts). Physical separation: `briefs/feature/brief.md` = transmitted content; `DISPATCH.md` = instructions to main on composition. Main reads the leaf + the composition-rule; sub-agent reads only the leaf.

---

## 7. Version anchors in operational briefs (delivered to agents who can't act on them)

**What was misrouted**:
- Operational briefs reference versions: "(v25 class)", "(v8.94 fresh-context)", "(v8.96 Theme A)", etc.
- Consuming agents (scaffold, feature, writer, code-review) have no version history. They read "this was broken in v25 and fixed in v8.90" as background noise at best, imitation vectors at worst (v33: phantom output tree where the writer copied a pattern it had seen referenced by version anchor).

**Evidence**:
- `recipe-version-log.md` is the authoritative source for version history.
- Main recipe.md has version anchors threaded throughout (grep `v25|v8.\d+|v33`).
- Dispatch briefs re-inherit this text when copied from the block.

**Defect class**: v33 phantom output tree — writer invented a `/var/www/recipe-{slug}/` tree after seeing "v32 lost the Read-before-Edit rule" kind of references; agent pattern-matched "recipe v32 had this" → "so the output tree is named like the recipe version".

**Recommendation**: anti-goal for the rewrite — version anchors belong only in `recipe-version-log.md`. Architectural principle #6. Stitching-at-dispatch-time produces a brief with zero version anchors.

---

## 8. Internal check vocabulary in briefs

**What was misrouted**:
- Checks have internal names: `writer_manifest_completeness`, `writer_discard_classification_consistency`, `hostname_gotcha_distinct_from_guide`, etc.
- These names appear in briefs when describing failures (e.g. "if `writer_manifest_honesty` fails…").
- Consuming sub-agents (writer, code-review) don't know the Go implementation of these checks — they know what the check *reads* and what it *requires*. The implementation name is an internal identifier.

**Defect class**: v33 dispatch-compression — when main compressed the brief, check names survived but the context around them didn't, so sub-agents saw opaque identifiers.

**Recommendation**: anti-goal; brief describes the read surface + requirement, not the check name. Architectural principle #2.

---

## 9. Go-source file paths in briefs

**What was misrouted**:
- Briefs occasionally reference Go source paths: `internal/workflow/recipe_templates.go`, `internal/ops/facts_log.go`, `internal/content/workflows/recipe.md`.
- Sub-agents cannot open these files — they have SSHFS-mount Read access only.
- The reference is descriptive ("the env-README templates live in recipe_templates.go"), but functions as a dead pointer from the sub-agent's perspective.

**Evidence**: grep recipe.md for `internal/` — references appear in scaffold-subagent-brief, dev-deploy-subagent-brief.

**Recommendation**: anti-goal; what the template *produces* belongs in the brief, not where its source lives. Architectural principle #2.

---

## 10. `feature-sweep-stage` → carries `content-authoring-brief` (27 KB at single substep-return)

**What was misrouted**:
- `complete step=deploy substep=feature-sweep-stage` returns **27927 B** of guidance in v34 (largest substep-return in trace).
- Substantive content for feature-sweep-stage itself is ~2.5 KB (the topic body is 37 lines).
- The remaining ~25 KB is the `content-authoring-brief` topic carried forward to seed the upcoming readmes substep.

**Why this is a misroute**:
- Main agent at feature-sweep-stage is curl-ing endpoints (actual behavior at the moment). The 25 KB it receives is content it will use **later** (when it dispatches the writer).
- Main agent at readmes substep (next substep) re-receives the same 25 KB as its scoped.body + Prior Discoveries in the dispatch. Doubled delivery.

**Actually this is** a *conservative* misroute — the content arrives at feature-sweep-stage instead of readmes, but ultimately gets to the right place. The risk is: agent reads the 25 KB twice (once at feature-sweep-stage, once again at readmes dispatch composition), paying cache + cognitive tax.

**Recommendation**: separate the substep-return from the substep-transition. `content-authoring-brief` should be delivered to the main agent at the moment it composes the writer dispatch, not at the prior substep's completion. This may require Go-layer changes in `recipe_guidance.go`'s substep-return logic.

---

## 11. Summary — misroute classes by architectural axis

| Axis | v20-v34 defect class | Misroute pattern | Fix principle |
|---|---|---|---|
| Substep ordering | v25 substep-bypass | Guidance eagerly injected BEFORE work window | #4 (server state = plan), #2 (brief separation) |
| Cross-scaffold contracts | v22, v34 | Principle in scaffold brief, needed in feature/writer brief | #3 (symbol-naming contract) |
| Content shape vs behavior | v34 shutdown-gotcha shape | Check validates README has gotcha; not that scaffold has handler | #1 (author-runnable pre-attest) |
| Manifest ↔ code-review | v34 DB_PASS | Prior Discoveries doesn't include manifest decisions | #5 (two-way fact routing) |
| Dispatcher / transmitted mixing | v32 compression | Single block addresses both audiences | #2 (leaf briefs) |
| Version anchor leakage | v33 phantom tree | Version refs pattern-match invent | #6 (anchors in log only) |
| Internal vocabulary leakage | v33 | Check names in brief | #2 |
| Go-source references | all | Sub-agents can't open them | #2 |
| Substep-return over-carry | structural (not named defect yet) | 25 KB of next-substep content delivered too early | #4, #6 |
