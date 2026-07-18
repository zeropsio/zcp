# Run-33 analysis — fresh dogfood after pre-flight + iteration-cost fixes

**Run dir:** `/Users/fxck/www/zcp/docs/zcprecipator3/runs/33/`
**Date analyzed:** 2026-05-09
**Pre-flight commit:** `0fc6b27f` (Cluster A + Step 2 + Step 3 + derived_rules.md)
**Iteration-cost fixes commit:** `7d7397f7` (F-47, F-48, F-49, F-50, F-52, F-53, F-54)
**Method:** counter-first; mechanical greps/jq over published surfaces and
SESSION_LOGS. No LLM in metric loop.
**Validation:** every numerical/behavioral claim independently verified
by codex (`codex:codex-rescue`); 11/12 PASS, one PARTIAL surfaced a
composer-wiring gap noted in §F-52.

---

## Headline

**Pre-flight worked.** Six of eight counters land at or near the
aspirational target. Two structural-quality residuals remain (KB shape
depth, env-content first-call hostname trap). Recommended next intervention
is incremental brief edits, not another fresh dogfood and not sim experiments.

| Counter | Run-32 baseline | Aspirational target | Run-33 actual | Δ vs baseline | Δ vs aspirational |
|---|---|---|---|---|---|
| **#1** cross-codebase env-var coherence | 3 | 0 | **0** | −3 | **at target** |
| **#2** slug-leakage published (English-cased) | 8 | 0 | **0 on regex; 7 evolved-shape leaks** | −8 on tracked regex | **PARTIAL** (regex needs follow-up) |
| **#3** cross-framework verb count | 22 | 0 | **1*** | −21 | **at target†** |
| **#4** voice-leak (sharpened regex) | 3 | 0 | **0** | −3 | **at target** |
| **#5** fact contamination rate | 12% (17/142) | unchanged sim-side | **13.6% (14/103)** | −3 records / +1.6pp rate | within expected |
| **#6** tier_decision Why-fill | 0% | 100% | **100% (10/10)** | +100pp | **at target** |
| #7 refinement recall | LLM-judged (deferred) | — | not measured | — | — |
| **#8** KB-header consistency | 100% inconsistent | 100% consistent | **100% consistent‡** | full | partial |

\* Single hit: `apidev/README.md:244` — `"NestJS on the default Express adapter"`. NestJS uses Express under the hood; this is accurate stack-naming, the same shape the aspirational reference deliberately keeps (`CHANGES.md`: "2 remaining 'Express' mentions are accurate stack-naming since NestJS uses Express under the hood").
† Effectively at target — the one remaining `Express` is naming the runtime adapter, not enumerating cross-framework alternatives.
‡ All three KBs use the same flat-bullet shape (no `##`/`###` header inside the extract markers). Aspirational also expects sibling-consistency, but additionally favours the deeper `## Tips and Others` H2 + H3 sub-headings + CAUTION callout shape (KB1). Sibling divergence is closed; depth-shape is not.

Adapt-path framing instances: **0** (run-32: 8). Closed.

IG line counts (Defect #14 surrogate):

| Codebase | IG #2 / #3 / #4 / #5 — run-32 | Run-33 | Cap |
|---|---|---|---|
| apidev   | 19 / 24 / 39 / 51 | **12 / 12 / 17 / 17** | ≤30 |
| appdev   | 20 / 21 / 23 / —  | **15 / 29 / 19 / 22** | ≤30 |
| workerdev | 33 / 48 / 56 / 70 | **30 / 25 / 30 / 28** | ≤30 |

All IG items now within cap. workerdev's 70-line IG #5 is gone. Two
worker IG bodies are at-cap (30/30) — agent took two retries on those,
see Surprise #5 below.

---

## Per-residual-question verdict

### Residual #1 — UPPER_SNAKE `candidateSurface` canonical values?
**PASS.** `jq -r 'select(.candidateSurface != null) | .candidateSurface' facts.jsonl | sort -u` returns exactly four canonical UPPER_SNAKE values: `CODEBASE_IG`, `CODEBASE_KB`, `CODEBASE_ZEROPS_COMMENTS`, `ENV_IMPORT_COMMENTS`. Zero kebab-case, zero `CODEBASE_KNOWLEDGE_BASE`, zero drift. Run-32 had 9 distinct spellings; F-A enum lock + brief teaching landed clean.

### Residual #2 — Engine populates Why on tier_decision shells?
**PASS.** `jq -c 'select(.kind=="tier_decision")' facts.jsonl | wc -l` = 10. `jq -c 'select(.kind=="tier_decision") | select(.why != null and .why != "")' | wc -l` = 10. **100% Why-fill** (run-32: 0%). All 10 are `engineEmitted=true` shells; F-B's emit-time Why population landed.

Sample: `tier-5-cpumode` → `"Tier 5 (Highly-available Production) — CPUMode moves  → DEDICATED."`. Topology delta narrated in porter-grade prose for every tier_decision shell.

### Residual #3 — Cross-codebase coherence Notice surfaced + influenced agent behavior?
**PASS — and exemplary.** `TIMELINE.md` lines 30-32 record: scaffold close-phase returned `ok:true with one notice: cross-codebase env-key drift between api (DB_PASSWORD) and worker (DB_PASS) on the same ${db_password} source — flagged for the feature pass to fix`. Then in the feature pass: `Renamed worker DB_PASS → DB_PASSWORD to align with api (cross-codebase coherence notice from scaffold close-out)`.

The Notice surfaced, the agent carried the finding across phases, the feature backend applied the fix. Counter #1 reads 0 because of this loop, not because the agent independently picked the same key name — the validator detected the drift and the agent self-corrected. Step 2 detection-only is sufficient here; refusal-mode would be over-engineered for this class.

### Residual #4 — Scaffold-phase fact text dropped cross-framework alternatives?
**PASS effectively.** `jq -c '.. | strings | select(test("Express|Fastify|Webpack|Astro|SvelteKit|Next\\.js|Nuxt|amqplib|BullMQ|Symfony|Django|Rails|Flask"))' facts.jsonl | wc -l` = 1. The single hit is the porter_change record citing `Express` as the underlying NestJS adapter — same accurate-stack-naming exception as Counter #3. F-E (deferred upstream teaching) effectively closed without a brief edit; the brief-side iteration-cost fixes likely tightened scaffold voice enough.

### F-47..F-54 verification predicates (from iteration-cost-fixes spec)

| Fix | Predicate | Run-33 | Verdict |
|---|---|---|---|
| **F-47** KB-stem record-fragment thrash ≤2 retries per agent | cc-worker had **15** record-fragment calls, **6** is_error tool_results | cc-worker run-32: 10 calls / 8 errors before 1 OK on stem. Run-33: 15 calls / 6 errors. Retry-rate dropped from 80% to 40%. Two of six errors were IG body-cap overruns (got 33, 31; cap 30) — F-50 surface-caps callout doesn't list IG body cap. Other four are KB-citation-as-noun-phrase + V3 link-text. **PARTIAL.** |
| **F-48** scaffold close-phase round-trips ≤2 per codebase | scaffold-api: 3, scaffold-app: 3, scaffold-worker: 2 | run-32: 4-5 per codebase. Run-33: 2-3. **PARTIAL** (scaffold-worker at target; api+app one over). |
| **F-49** Zero `unknown codebase`/`unknown fragmentId` errors on first call | **6 unknown-fragmentId** errors from env-content agent in ONE parallel batch (apidev/apistage/appdev/appstage/workerdev/workerstage), all slot names; recovered immediately and remained clean for env/1. Zero `unknown codebase` errors anywhere. | **PARTIAL FAIL.** Principle file landed in 4 composers per spec, but env-content sub-agent still tried slot-named fragmentIds for `env/<N>/import-comments/<bare>` shape. After one error batch the agent fully self-corrected (env/1 was clean from the first call). Either (a) the principle didn't reach the env-content composer's effective slice, or (b) the principle file teaches the codebase-MCP-parameter form but not the fragmentId path form clearly enough. **Diagnose before next dogfood.** |
| **F-50** Zero one-shot env-content cap rejections | env-content: 8 is_error tool_results across 76 record-fragment calls (10.5%). Two of those are `import-comments` line-cap (≤8 got more) and intro 350-char overruns surfaced in run-32; need detail. | **MOSTLY PASS** — error rate vastly lower than F-50's run-32 evidence (4 single-shot rejections of 14/8/350 caps). |
| **F-52** Zero `nothing to commit` git-commit failures | features-backend + features-frontend: 1 each = **2 hits** | run-32: 1 hit (scaffold-worker). **REGRESSION + composer-wiring gap.** Codex verification surfaced that the F-52 pre-check is present on line 186 of both feature briefs at runtime but **MISSING from scaffold briefs at runtime** despite the iteration-cost-fixes spec naming `scaffold/content_authoring.md` + `phase_entry/scaffold.md` as edit sites. Furthermore: features-frontend issued naked `ssh appdev "git add ... && git commit -m '...'"` six times (no `git status --porcelain` guard) despite the brief carrying the pre-check on line 186 — agent saw the teaching, didn't internalize it. **Two separate gaps.** Diagnose before next dogfood. |
| **F-53** Zero `zerops_workflow workflow=develop` invocations | **0 actual tool_use blocks** invoking `mcp__zerops__zerops_workflow` from any sub-agent | **PASS.** Run-32 had 1 misroute. |
| **F-54** Zero wrong-path `ls` calls in refinement | refinement agent ran `ls /var/www/zcprecipator/nestjs-showcase/{api,app,worker} 2>/dev/null` — exit code 2 (the `2>/dev/null` swallowed the `No such file or directory`; mounts are at `/var/www/<slot>dev/` with no `zcprecipator/<slug>/` prefix). | **PARTIAL FAIL.** Same hostname-vs-slot trap as run-32 F-54, with an extra slug-prefix layer. The original PASS verdict here was wrong — see [run-33-defect-comparison.md §Part 5](run-33-defect-comparison.md). |

---

## Top 5 surprises

### 1. Cross-codebase coherence loop worked end-to-end on first dogfood
The pre-flight rolled out detection-only (Notice) on the assumption that
refusal-mode would land later if the agent ignored the Notice. Run-33
shows the agent NOT only saw the Notice but propagated the finding
across phase boundaries (scaffold close-out → feature backend) and
applied the fix at exactly the right surface (worker yaml `DB_PASS` →
`DB_PASSWORD`). This validates Step 2's design: detection + structured
recovery hint is enough; refusal would be unnecessary friction. Step 2
enforcement (refusal mode) is **demoted** in priority — keep deferred.

### 2. F-E (cross-framework drift in scaffold facts) closed without a brief edit
The pre-flight explicitly deferred F-E ("scaffold-phase teaching to prevent
cross-framework drift at source — defer until fresh-run evidence"). Run-33
fact text shows 1 cross-framework hit, all in accurate-stack-naming
context. The brief-side iteration-cost fixes (F-47 stem self-check
vocabulary, F-48 batch-fix discipline) tightened scaffold voice enough
to drop alternative-enumeration without explicit anti-pattern teaching.
F-E can be **closed without action**.

### 3. KB shape collapsed to consistent BUT shallow
Counter #8 collapsed sibling-divergence to zero (target hit), but the
shape converged on the SHALLOWEST form: no header inside extract markers,
flat bullets with full mechanism+effect+fix prose per item. The
aspirational reference uses `## Tips and Others` H2 + H3 sub-headings +
paragraph + CAUTION callout (KB1, KB7). Run-33 substance is at-bar
(7 items in apidev, 4 in appdev, 4 in workerdev — all framework × platform
intersection traps with mechanism+effect+fix narrated), but shape is
not. Refinement could in principle promote shape, but the rule-substrate
hypothesis was that principle-shaped rules drive ACTs; an `apply
KB1+KB7 shape uniformly` rule would have to compete with "first do no
harm" against agent-authored substance. Decide explicitly: do we want
KB1/KB7 promotion in next refinement substrate revision?

### 4. F-49 hostname principle missed env-content fragmentId path
The biggest specific gap. The principle file `principles/codebase-name-vs-slot-hostname.md`
explicitly lists `fragmentId=env/<N>/import-comments/<bare>` (run-32
iteration-cost-fixes spec, F-49 fix shape). It's wired into the
env-content composer per spec. Run-33's env-content agent issued one
parallel batch of 6 record-fragment calls all using slot names
(`env/0/import-comments/apidev` etc.). Six errors, one batch, then full
recovery. Either:
  - **Hypothesis A**: the principle wasn't loaded into the env-content
    *effective* prompt slice — verify by grepping the run's brief at
    `runs/33/environments/.briefs/env-content-phase/` for the principle's
    title.
  - **Hypothesis B**: the principle teaches the rule but the env-content
    agent's mental model of "I've been editing apidev/import.yaml,
    therefore the fragmentId is `import-comments/apidev`" beats the
    principle.
  - **Hypothesis C**: env/0 has both a `<bare codebase>` set AND a `<managed service>` set (the agent later issued `import-comments/{db,cache,broker,storage,search}` cleanly), and the slot-names trap is specifically the codebase-runtime axis where the agent has been editing slot-named yaml files. The principle teaches `<bare>` is required but the agent generalized "I'm working on apidev's tier 0 yaml so my fragmentId is .../apidev" without applying the principle to that path.
  
  Diagnose by reading the env-content brief content + the agent's
  reasoning trail before next intervention. Likely a one-line edit
  inside the env-content per_tier_authoring atom or the principle
  file itself.

### 5. cc-worker IG body-cap overrun is the residual F-47/F-50 case
codebase-content-worker had 5 complete-phase round-trips and 6
record-fragment errors. Two of the errors are IG body cap (`got 33` and
`got 31`; cap 30). The others are KB-citation-as-noun-phrase (V3) and
kb-citation-missing — refinement-layer-flagged classes that the
codebase-content agent should have caught at authoring time.

The F-47 stem self-check vocabulary delta improved cc-worker's record-fragment
retry rate from 80% to 40%, but the IG-body 30-line cap is not in the
F-50 surface-caps callout (F-50 currently lists `intro` 350-char +
`import-comments/<host>` 8-line). One-line addition: extend F-50 callout
to include IG body cap. Cheap.

---

## Recommended next intervention class

**Brief edits** — three targeted, low-cost edits before any re-dogfood:

1. **(Sharpen F-49 for env-content)** — investigate Hypothesis A/B/C above; either fix composer wiring or strengthen the principle's `fragmentId=env/<N>/import-comments/<bare>` example with a worked BAD/GOOD pair from run-33 evidence. ~30 min.
2. **(Extend F-50 surface-caps callout)** — add `codebase/<host>/integration-guide/<n>` body ≤30 lines to the F-50 top-of-file Surface caps section. Co-locate at the IG worked-example site if one exists in `codebase-content/synthesis_workflow.md`. ~20 min.
3. **(Diagnose F-52 two-gap regression)** — codex verification revealed two distinct gaps:
   - **Composer-wiring gap**: F-52 spec named `scaffold/content_authoring.md` + `phase_entry/scaffold.md` + `feature/decision_recording.md` as edit sites; feature briefs at runtime carry the pre-check (line 186) but **scaffold briefs at runtime do not**. Either the scaffold composer doesn't pull from those atoms, or the atom-edit didn't reach the assembled brief slice. Verify by grepping scaffold atom source files for the pre-check + tracing the composer's atom-include chain. ~20 min.
   - **Discipline gap**: features-frontend's brief carried the pre-check but the agent issued six naked `ssh appdev "git add ... && git commit -m '...'"` calls without the `git status --porcelain` guard. The brief teaching wasn't internalized. Tighten by either (a) co-locating the pre-check shell shape directly above the worked-example commit at the agent's typical reading-anchor, or (b) making the guard part of an `ssh-commit` shell wrapper teaching that's harder to skip. ~30 min.

**Decision points to surface to user:**

- **KB1/KB7 shape promotion** — do we want refinement to promote KB
  shape from flat-bullets to `## Tips and Others` + `### Per-trap` H3 +
  `> [!CAUTION]` for destructive ops? Substance is at-bar; this is a
  shape-depth call.
- **Step 2 enforcement (refusal)** — deferred indefinitely after this
  run's evidence. Worth dropping from the deferred-items list as
  resolved-by-evidence.
- **F-E (scaffold cross-framework drift)** — close as resolved-by-evidence.

---

## Sim experiment results (Job 2)

**Decision: skip Job 2 sim experiments.** Counters moved 80%+ to
aspirational on every measurable axis (1, 2, 3, 4, 6, 8). Per the
handoff doc:

> If counters move ≥80% of the way to aspirational on Job 1, the
> pre-flight worked and the next iteration is incremental brief edits.

The remaining gaps (KB shape depth, env-content fragmentId batch, IG
body cap overruns, F-52 regression) are all addressable by targeted
brief edits, not by varying the substrate or re-stitching with hand-edited
inputs. Sim experiment 1 (refinement-only replay) wouldn't move
the residual axes — KB shape depth requires NEW substrate rules
(KB1/KB7 promoted to scoring); env-content fragmentId is a scaffold-or-env-content-brief
edit, not a refinement reach.

If next dogfood (after the three brief edits above) regresses on any
counter, fall back to sim experiment 1 to isolate cause. For now,
sim is over-engineered for the residual gap.

---

## Appendix — counter-collection commands (reproducible)

```bash
RUN=docs/zcprecipator3/runs/33
# Published surface set
PUB=$(find "$RUN" \( -name "README.md" -o -name "import.yaml" -o -name "zerops.yaml" \) \
  -not -path "*/.briefs/*" -not -path "*/SESSION_LOGS/*" \
  -not -path "*/screenshots/*" -not -name "CLAUDE.md" | sort)

# C#1 — extract cross-service env mappings, compare across codebases
for f in apidev/zerops.yaml workerdev/zerops.yaml; do
  grep -nE '^\s*[A-Z][A-Z0-9_]+:\s*\$\{[a-z][a-zA-Z0-9_]*_' "$RUN/$f"
done

# C#2 — published slug-leakage (English-cased)
grep -nE '\[(managed (NATS|Postgres|Redis|...) service|Zerops object-storage service|Zerops env-var model|...)\]' $PUB

# C#3 — cross-framework verbs published
grep -nE '\b(Express|Fastify|Webpack|Astro|SvelteKit|Next\.js|Nuxt|...)\b' $PUB

# C#4 — voice-leak (sharpened)
grep -nE '\b(under|via|owned by|managed by) (zerops_dev_server|the agent)\b|...' $PUB

# C#5 — fact contamination
grep -cE 'zerops_dev_server|zsc noop|"the agent"|the agent[^=]|record-fact' "$RUN/environments/facts.jsonl"

# C#6 — tier_decision Why-fill
jq -c 'select(.kind=="tier_decision") | select(.why != null and .why != "")' "$RUN/environments/facts.jsonl" | wc -l

# C#8 — KB-header per codebase
for cb in apidev appdev workerdev; do
  START=$(grep -n 'ZEROPS_EXTRACT_START:knowledge-base' "$RUN/$cb/README.md" | head -1 | cut -d: -f1)
  sed -n "${START},$((START+15))p" "$RUN/$cb/README.md"
done
```
