# Restore bootstrap's four design goals — remediation plan (2026-06-02)

**Status:** DRAFT — archaeology complete, restorations being finalized (workflow + Codex).
**Owner:** krls2020. **Trigger:** post-release manual test (transcripts c1/c2) + live expansion
campaign surfaced that ZCP is now doing the OPPOSITE of its job (misleading the agent instead
of giving it correct info). See `plans/postrelease-transcript-rootcause-2026-06-02.md` for the
F1–F11 evidence; THIS plan is the structural remediation.

---

## The four goals (the contract bootstrap was built to honor)

1. **Exact info about what changes.** Bootstrap tells the agent precisely which services
   will be created/changed — and the truth about them.
2. **Adoption as simple as possible.** Attaching to existing services is frictionless.
3. **No magic.** No silent/implicit behavior; the agent sees and controls what happens.
4. **Parallel work.** Multiple agent sessions in one project are EXPECTED and safe — only
   two concurrent *bootstraps* are blocked; everything else runs in parallel.

**Meta-principle (why ZCP exists):** ZCP is the agent's *helper* — it supplies correct
information, knowledge, and platform truth, and eases the work. Every finding below is a
place where ZCP instead **misleads** the agent. The remediation is not 8 patches; it is
re-asserting these four goals + the meta-principle at the seams where recent refactors
drifted.

---

## Layer 0 — The information substrate (the deepest fundament)

> Everything below this line (the machinery: checks, derivation, locks) is the SAFETY NET.
> This layer is the FRONT LINE: what ZCP actually tells the LLM. If the information is correct
> and curated, the agent makes the right choice and the machinery rarely fires. Today the
> information is *actively wrong* — it steers the agent INTO the traps the machinery then
> (badly) tries to catch.

**The one root insight (audit-converged, high confidence): ZCP conflates the VALIDATION SET
with the PRESENTATION SET.** Two artifacts, two owners:
- *Validation set* — the full schema enum (every OS/mode variant, every version-family alias,
  every rolling tag, every non-importable member). Its job: answer "does the platform accept
  X?" — silently, inside `HasServiceType`. This is correct and stays.
- *Presentation set* — what the response shows the agent. Its job: answer "what is the ONE
  correct next choice?"

ZCP **presents the validation set AS the presentation set.** Every bootstrap response dumps
`availableStacks` = the entire 1118-byte version matrix of all 24 technologies, and the
guidance literally says *"pick the highest available version from availableStacks."* So the
agent is handed `bun@{1,1.2,1.2.2,1.3,1.3.9,canary,latest,nightly,...} | go@{1,1.22,latest} |
deno@{1,...}` as a flat menu of equal peers and picks a version-family alias (→F1 freeze) or a
dead version (→F8 import death). **The information manufactures the bugs.** "Exact info" (Goal
1) was implemented as MAXIMAL info; the conflation is the disease.

### What the audit found (3 auditors, quantified)
| Surface | Now | Problem |
|---|---|---|
| **Catalog** (`availableStacks`) | 1118 B, 126 version tokens, re-sent 4–6×/session (start + every status + the COMPLETED response via the `Current==nil→true` leak + `zerops_knowledge`) | DUMP + TRAP (family aliases, rolling tags, non-importable `deno@1`) + WRONG-TIME (re-sent after the choice) + NOISE (`golang` dup of `go`, `zcp`, docker/alpine) |
| **Develop Guidance** | **22 atoms / 26–28 KB on first call, 97% generic**; 34 atoms have NO state-gate so the same ~12–18 KB re-fires on every develop + status call; close-mode decision buried at 36% depth | DUMP + NOISE (no cross-call suppression) + WRONG-TIME/buried (load-bearing rule drowned) |
| **Route-menu** | full 3.8 KB import.yml inlined ×3 candidates before the agent chooses one | NOISE (2 of 3 are dead weight at decision time) |
| **`scope=infrastructure`** | 37.6 KB monolith (core 28.7 + model 8.6 + catalog) | the "rule buried in a long response" complaint |
| **knowledge search** | snippet returned, no mode to dereference it (F7) | TRAP-adjacent (teaser you can't open) |

### The governing principle (operationalize it as a contract)
**Recommend, don't enumerate · resolve, don't dump · curate to the next decision · correct >
complete · surface once.** The full data is for VALIDATION, never PRESENTATION.

### The Layer-0 redesign
- **Catalog → present the ACTIVE CONCRETE versions per tech (owner refinement: show them, don't
  hide to one).** The agent SHOULD know exactly what it's using and what's available — so list the
  active concrete versions per base, latest marked recommended, e.g.
  `nodejs@24 (latest) · 22 · 20 | bun@1.3.9 (latest) · 1.2.2 | go@1.22 | postgresql@18 (latest) · 17 · 16`.
  The curation is: **active-only + concrete-only** — drop the *traps*, not the choice: no
  version-family roots (`go@1`/`deno@1`), no rolling tags (`latest`/`canary`/`nightly`/`stable` as
  literal tokens — the highest concrete is *labelled* "(latest)" instead), no non-active versions,
  no `golang`(dup)/`zcp` noise. **Error/inform contract (owner ask):** if the agent requests a
  version not in the active list → discover rejects with the active alternatives for that base
  ("`deno@1` is not active; active: deno@2.0.0"); if it requests a family alias that the platform
  resolves → import proceeds and provision RECORDS + REPORTS the concrete result
  ("`bun@1` → created `bun@1.3.9`"). Either way the agent is *informed*, never silently frozen.
  Attach **ONCE at discover**
  (`needsStacks` = `Current.Name==StepDiscover`; kill the `Current==nil→true` leak + the
  start-commit/skip attaches). The renderer change is shared: the same `FormatStackList` also
  feeds the `zerops_knowledge scope=infrastructure` reply (knowledge.go:156) — both get the
  active-concrete form (that path is on-demand, so it legitimately shows the catalog).
  Enumeration of a base's versions surfaces ONLY on a wrong choice
  via the existing `FormatVersionCheck` ("X not found, available: …", scoped to that one base).
  `RecommendedStacks` reads the **active-filtered cache** (see below), so it can never recommend
  a deactivated version, and it recommends CONCRETE versions so the agent never authors a family
  alias. **This makes F1/F8 unhittable from the front line.**

  **Source correctness (the root fix — NOT a masking heuristic).** The schema genuinely cannot
  answer "is this version ACTIVE" — that fact lived ONLY in the stack-types API `120e36f4`
  deleted (alongside its redundant type-existence, which overlapped the schema). So **re-add a
  NARROW `client.ActiveServiceTypeVersions()`** (the SDK `PostServiceStackTypeSearch` → keep
  `Versions[].Status=="ACTIVE"` — exactly the one unique field that was dropped, nothing else)
  and fold it into the **`schema.Cache` refresh** (host-derived = the user's instance) AND
  `make schema-sync` (canonical-pinned = embedded floor): filter the type enum to the active set
  AT INGESTION. The cache stays the SINGLE client-side source — now CORRECT at the source.
  Ownership is clean: **schema = structure/existence · API = active-status (its one unique fact)
  · cache = composed-correct.** Then `HasServiceType("deno@1")=false` at discover ("passed
  discover ⟹ importable" restored) and every downstream consumer is correct for free. This
  honors single-source-of-truth (one cache; the API consulted for the ONE fact it uniquely owns,
  folded in — not a competing query path) and removes the deno@1 trap at the root, not by
  guessing. F1's provision-reconcile stays as the principled backstop for an agent that authors
  a family alias from memory.
- **Develop Guidance → decision vs reference split:** DECISION atoms (gated to current
  deploy-state/status — what to do NOW) are inlined; REFERENCE atoms (platform mechanics:
  deploy-modes, env-channels, platform-rules, verify-matrix) surface once, pointer-style
  (agent pulls the body on demand). Cross-call suppression of already-delivered atom bodies.
  Hoist the load-bearing decision (close-mode) above the wall. Size budget: ≤~12 KB first call,
  ≤~4 KB subsequent (pinned by test).
- **Cross-response:** route-menu carries no inlined import.yml (deliver at provision for the
  CHOSEN recipe only); `scope=infrastructure` becomes a section index, not a 37 KB monolith;
  knowledge search results are dereferenceable (F7); discover service-list scoped to the
  session (F3-adjacent).

**Layer 0 reframes Goal 1:** "exact info" = *the one correct curated thing*, not *all things*.
The machinery restorations below (F1/F8) remain — but as the safety net behind a front line
that no longer hands the agent the traps.

---

## THE UNIFYING STRUCTURAL ROOT: tell/check divergence

> Layer 0 (catalog) is the sharpest INSTANCE of a deeper, system-wide structural flaw. A hunt
> across 5 surfaces found the same shape everywhere: **ZCP TELLS the agent to do X (atom /
> guidance / jsonschema tag / presented data / response message) but a SEPARATE code path
> CHECKS / validates / enforces Y, and X ≠ Y — because the two halves are authored separately
> against the same concept with no shared owner, so they drift.** The agent does what it was
> told and the check rejects it (friction/freeze), or the tell asserts something the behavior
> contradicts. This is the generalization of "the validation set is not the presentation set":
> *every* (tell, check) pair needs ONE owner.

### The divergence map (5 surfaces, code-grounded; F = already-known instance)
| Surface | Divergence | Tell | Check | F |
|---|---|---|---|---|
| catalog | family alias / `latest` presented + modeled as the `type` to author | jsonschema examples hardcode `type:"go@1"`; scaffold atom rows `go@1`; "pick highest from availableStacks" | `checkServiceType` requires resolved-concrete (`go@1`≠`go@1.22`) | F1/F8 |
| catalog | full schema enum presented AS the choose-from set | `availableStacks` = `ImportYml.ServiceTypes` verbatim (incl. `deno@1`, `latest`) | enum answers existence, not "what to author" | F1/F8 |
| catalog | managed "use LATEST concrete" vs runtime "alias OK" — contradictory *within one surface* | recipe rule forces `postgresql@18`; runtime examples model `go@1` | managed passes provision, runtime alias freezes | new |
| catalog | recipe-override matcher: plan-alias vs recipe-YAML-concrete | "pick highest" lets plan carry `go@1` | `findRuntimeSlot` byte-matches recipe's concrete via `TypesAreEquivalent` | new |
| develop | verify `http_root`→degraded on the deferred-start window the atom calls **benign** | `develop-first-deploy-verify`: "502 is expected, run dev_server" | `classifyRuntime` keys on live ports, zero mode/meta awareness | F5 |
| develop | note "start a new session" vs deploy gate that **accepts** continued deploys | `sessionAnnotations` note | no close-state gate on deploy | F6 |
| develop | manual atom "ZCP never initiates a deploy" vs `zerops_deploy` runs normally | `develop-strategy-review` | gate only checks the strategy *param*, not close-mode | new |
| deploy | git-push prereq is `GIT_TOKEN` (tell) vs `GitPushState=configured via git-push-setup` (gate) | `deploy_ssh.go:94` jsonschema | `gitPushMetaPreflight` hard-rejects; never names `git-push-setup` | new |
| deploy | `setup=` omit-rule = "name==hostname" (tell) vs **three** different resolvers | jsonschema setup desc | `resolveSetupEntry` 4-step fallback ≠ the stated rule | new |
| deploy | self-deploy DM-2 framed as a *service property* vs enforced on the call-time source string | atom "self-deploying services MUST…" | `ClassifyDeploy` keys on `source==target` string identity | new |
| adopt/recipe | recipe **worker** invisible to the plan-authoring tell, load-bearing in the rewrite | `bootstrap-recipe-match`: 2-role (dev/prod)+deps | `RewriteRecipeImportYAML` buckets `worker`→`stage` | F2 |
| adopt/recipe | recipe-mode enforcement silently abstains on 3+-runtime recipes | "recipe mode is enforced" | `InferRecipeShape`→`""` for 3+ setups → `ValidateBootstrapRecipeMode` no-ops | new |
| adopt/recipe | adopt tell = "the listed services"; derive recomputes adoptable from LIVE | `idle-adopt-entry`: "adopt the listed services" | `BootstrapCompleteAdoptPlan` re-derives (superset, incl. other sessions') | F3 |
| schema/SDK | flatten-recovery PROMISED by the tell is structurally **unreachable** | Plan tag: "error includes corrected JSON, paste-and-resend" | SDK `additionalProperties:false` rejects terse FIRST | F-friction |
| schema/SDK | nested plan fields have NO jsonschema descriptions | only the giant top-level Plan blob | SDK exposes bare property names | F-friction |
| schema/SDK | route vocabulary (`bootstrapMode:"adopt"`, `resolution:"EXISTING"`) | atoms use "adopt"/"existing" elsewhere | enum rejects the near-synonym | F-friction |
| schema/SDK | `RecipeTarget.Type` "pick highest" vs `validateManagedVersionLatest` (latest-or-reason) | Type tag says "must exist" | check enforces a stronger unstated rule | new |
| schema/SDK | `ResearchData` required fields hard-enforced but absent from the exposed schema | nested object, no tags | `validateResearchFields` rejects missing/`dbDriver` | new |
| schema/SDK | record-fact brief said `payloadShape`, struct tag is `payloadSchema` | recipe brief example | SDK schema from struct tag → 5 rejects | new |

### Single-owner remediation (one owner per concept → tell + check can't drift)
1. **The active-filtered `schema.Cache` is the single source for "what exists + is active"**
   (NO `ResolveConcrete` table — dropped as an unjustified layer). The catalog PRESENTS active
   concrete versions from it; the provision check READS it to tell a selector from a concrete-leaf;
   `HasServiceType` validates against it. One correct source, three read-only consumers. (Subsumes
   the catalog divergences; the recipe-match alias case is handled by the recipe path, not a shared
   resolver.)
2. **The Go input struct = single owner of tool I/O** — the SDK already derives the *check*
   (properties/required/`additionalProperties`/enums) from the struct; force every *tell* to
   derive from it too: jsonschema tags on every field, accepted-token sets as shared data +
   synonym tables, and a **lint that every field a brief/atom instructs the agent to send exists
   as a json tag** on the input struct. (Subsumes all 6 schema/SDK divergences + the record-fact
   class.)
3. **`RecipeShape` descriptor parsed once from the recipe YAML** — owns both the plan-authoring
   tell (rendered FROM it, so worker slots are shown) and the rewrite check. (Subsumes F2 +
   `InferRecipeShape` abstention.) **DEFERRED to backlog (owner decision):** this full single-owner
   redesign is the deep solution; the four-goals plan ships only the MINIMAL worker-slot fix now
   (worker = own dev/simple target so it isn't dropped). See Phase 6 + the worker backlog entry.
4. **One lifecycle/runtime-shape owner** — `topology.isDeferredStart(mode,class)` feeds BOTH the
   "benign 502" atom and the verify check; `DeriveCloseState` owns BOTH the note text and the
   deploy behavior. (Subsumes F5 + F6 + manual-mode copy.)
5. **Deploy-config gates render their own jsonschema** — one `PrerequisiteSpec` (git-push) +
   one `ResolveSetupName` the gate enforces and the description is generated from. (Subsumes the
   3 deploy divergences.)
6. **Persist-and-derive for adopt** — persist the route-menu's adoptable enumeration at commit;
   the derive uses THAT recorded set (re-validating, dropping vanished), never a fresh live
   superset. (Subsumes F3.)

### Structural prevention (so tell/check can't silently re-drift)
The reason these drifted + shipped green: nothing pins "tell == check." Add lint/contract tests:
every accepted-token enum is shared between the schema the agent sees and the validator; every
field referenced by an atom/brief/jsonschema-example exists on the owning struct; the catalog
presents only what `checkServiceType` accepts. **This is the meta-pattern's coverage hardening —
it makes the single-owner discipline enforceable, not just aspirational.**

---

## Drift map — which refactor broke which goal (forensic, git-grounded)

Each regressing refactor was *individually reasonable* and shipped "green" (tests + Codex +
flow-eval). They drifted because (a) each optimized one thing and didn't notice the
side-effect on another goal, and (b) **the eval matrix never exercised the failing
combination** (version-expanding runtime / non-ACTIVE version / concurrent session / worker /
multi-cross-deploy) — it used node/php happy-paths.

| Goal broken | Regressing commit(s) | What it did (legitimately) | How it drifted | Finding |
|---|---|---|---|---|
| **1 Exact info** | `a3314929` (5-18) | Replaced in-place plan rewrite with type-equivalence, to fix a recipe-match OS-prefix bug ("plan NEVER rewritten") | Equivalence covers OS/mode form but NOT version-family resolution; the removed in-place rewrite WAS the reconcile that hid this. Now bootstrap LIES: "type mismatch" on the platform's own resolution → forced reset | **F1** |
| **1 Exact info** | `120e36f4` (6-2) | Collapsed overlapping type sources to one schema (good); deleted `StackTypeCache` + its version `Status` | Dropped Status on the assumption "schema is already curated to ACTIVE + platform backstops" — but `deno@1` is non-ACTIVE yet in the enum → discover passes, import hard-fails with a non-attributing error | **F8** |
| **1 Exact info** | `85992f36` + `a3314929` | Recipe route rewrites import YAML from the agent's plan | Two artifacts (recipe YAML + agent plan) describe one topology; metas derive from the agent's lossy copy → dropped worker → no meta → deploy blocked | **F2** |
| **2 Simple adopt / 3 No magic** | `0384ce5d` (6-2) | Auto-derive the adopt plan from `adoptableServices(...)` to kill a 14-failure hand-authoring slog | Derives from ALL adoptable services project-wide, self-excluding only the control plane — silently sweeps a **concurrent session's** services + offers the control plane. Simple, but magic + parallel-hostile | **F3** |
| **3 No magic** | `cad1ed35` (6-1) | Derived auto-close (never stamped) for compaction determinism (good) | Renders the reversible "scope is green" state as a FALSE imperative "Start a new session for this work" — deploys into the closed session succeed; closeMode is a confusing separate gate (unset→hangs; auto→premature) | **F6** |
| **4 Parallel** | deploy_batch (`5946d557`, 4-22) | Server-side parallel deploy to dodge the MCP-STDIO serialization penalty | Fans out goroutines per target with NO per-source serialization → same-source cross-deploys collide on the source container's `.git/config.lock` | **F4** |
| **4 Parallel** | `0384ce5d` (cross-session half) | (as above) | Parallel sessions in one project — an EXPECTED mode — are unsafe because adopt-derive (+ unscoped discover/status) reach across sessions | **F3** |
| (verify) | long-standing + `13fa32fb` (5-28) | classify HTTP-vs-worker by base image type | A php-nginx worker (setup=worker, no HTTP) is HTTP-probed → false "degraded". Live-proven: ports DTO (incl. scheme) can't distinguish worker from web; only the deployed setup can | **F5** |

**The unifying disease:** ZCP keeps comparing/deriving against the agent's *copy* of
information instead of the *authority* that owns it (platform resolution / recipe YAML /
live services / deployed setup), and it expresses reversible state as imperative commands.
Goal-restoration = **derive/reconcile from the authority, transparently, and tell the truth.**

---

## The one new pattern that fixes most of it

Three small primitives + one discipline, reused across goals:
- **A provision-scoped acceptance rule (NO broad primitive — Codex-corrected).** At the provision
  check ONLY: if the plan version is a **concrete leaf** (per the cache version-tree, e.g.
  `nodejs@22`) require exact equivalence to live (`TypesAreEquivalent` already does this — and a
  diff IS a real anomaly to fail on); if the plan version is a **selector** (a family root like
  `go@1`/`rust@1` that has sub-versions in the catalog, or a rolling tag `latest`/`canary`/
  `nightly`/`stable`) accept any **same-base** live and RECORD the live resolved type. This keeps
  the genuine cross-base + concrete-anomaly guard, handles `rust@1→stable` (rust@1 is a selector →
  same-base accept), and needs NO `TypeResolves`/`ResolveConcrete` topology primitive — it's a
  local rule in `checkServiceType` using the (now active-filtered) cache. The shared
  `TypesAreEquivalent` is untouched (catalog existence still rejects hallucinations).
- **`topology.RuntimeWorker`** — a NEW enum value (topology has none today); the missing canonical vocabulary for "this runtime serves no HTTP".
- **Record-and-report the authority's truth** — when the platform/recipe/setup is the authority, ZCP records the resolved truth on the agent-visible surface that ALREADY EXISTS (`ServiceSnapshot.TypeVersion` for the resolved type; `ServiceMeta.ServesHTTP` for HTTP-ness) and TELLS the agent ("`go@1` resolved to `ubuntu/go@1.22`"; "classified worker because `setup=worker`"). This is the "no magic" form of reconcile.
- **Confirm-before-commit** — generalize the existing `ErrAdoptPairingChoice` round-trip: a derived plan (adopt or recipe) is shown to the agent and acked before it's persisted. Transparency = no magic, and it's strictly *more* help (no fuzzing), not friction.

## Restoration by goal

> Each restoration preserves the legitimate gain of the refactor that regressed the goal.
> The "preserves" line is load-bearing — verified by the archaeology pass (high confidence).

### G1 — Exact info (F1 + F8)
**F1 — reconcile-and-report at PROVISION (not discover); provision-scoped acceptance rule, NO
broad primitive.** Keep the submitted plan shape untouched through validate/recipe-match
(`validate_bc_test` pins this — a3314929's real win). At `checkServiceType` (where the live
service + its resolved type are observable), apply the provision-scoped rule (see "one new
pattern" above): concrete-leaf plan → require exact equivalence (the existing `TypesAreEquivalent`;
a diff is a real anomaly to fail); **selector** plan (`go@1`/`rust@1` family root, or rolling tag)
→ accept any same-base live and RECORD it. On accept, persist the live resolved type (mirror the
existing `DiscoveredStatuses` pattern, `engine.go:672`) so bootstrap-active stops replaying the
stale plan type (`bootstrap_guide_assembly.go:174`), populate `ServiceSnapshot.TypeVersion`
(envelope.go:104 — already written post-bootstrap at compute_envelope.go:241), and emit a *pass*
check `"go@1 resolved to ubuntu/go@1.22"`. This is the 6afee029 reconcile at the correct layer
(provision) — the version reconcile is genuinely new (even 6afee029 only did OS/mode form) and
transparent (reported, not a hidden rewrite). *Preserves:* recipe-match (plan un-rewritten
pre-provision); `TypesAreEquivalent` byte-identical → catalog existence still rejects
hallucinations + the same-base concrete-anomaly guard (Codex: pure base-match would wrongly accept
`nodejs@22` plan vs `nodejs@24` live — the concrete-leaf branch prevents that).

**F8 — make "passed discover ⟹ importable" true, at the SOURCE.** The schema can't know
"is this version ACTIVE" — that fact lived only in the stack-types API `120e36f4` deleted.
**Re-add a NARROW `client.ActiveServiceTypeVersions()`** (SDK `PostServiceStackTypeSearch`,
verified live = `ServiceStackType{Versions[]{Name, IsBuild, Status}}`; keep `Status=="ACTIVE"`
only) and fold it into BOTH the runtime `schema.Cache` refresh (host-derived) and
`make schema-sync` (canonical-pinned embedded floor): filter the type enum + `active_versions.json`
to the active set AT INGESTION. Then `HasServiceType("deno@1")=false` at discover. *Preserves:*
single-source-of-truth — the cache stays the ONE client-side source; the API is consulted for
the ONE fact it uniquely owns (active-status), folded into the cache, NOT a competing query
path. This is the inverse of `120e36f4`'s error: it deleted the API's *redundant* fact (type
existence) AND its *unique* fact (active-status) together; we restore only the unique fact.
**Codex-verified feasible** (`PostServiceStackTypeSearch` exists, returns `Status` incl. `ACTIVE`;
runtime has client+creds) **with three hard rules so it's not a masking fallback:** (1) the
**runtime** cache may degrade if the ACTIVE fetch fails — but VISIBLY (annotate "active status
unavailable; list may include inactive versions", don't claim active-correct); fits the existing
TTL/embedded-floor/poison-guard pattern. (2) `make schema-sync` (writes the COMMITTED embedded
floor) must **HARD-FAIL** if ACTIVE can't be fetched — never silently write known-wrong schemas
(needs a client/auth path added to `schemaSyncCore`, which has none today). (3) `zcp schema check`
must apply the **same** active filter (or self-skip on ACTIVE-fetch failure) — else it compares a
filtered committed copy against an unfiltered live fetch and reports **permanent false drift**.
**F8b — attribution:** service-level import errors ALREADY land in `serviceErrors[].meta`
(ops/import.go:190); the gap is a partial import is a non-error result — surface
`serviceErrors` prominently in the summary/nextActions so an atomic multi-service import names
the offending `services[].hostname`. (Codex: prominence, not new plumbing.)

### G2/G3 — Simple adopt + no magic (F3 + F2)
**Apply cb63bf32's lesson (explicit scope, no project-wide derive) + confirm-before-commit
to BOTH routes.**
- **Adopt:** add an adopt scope (`WorkflowInput.AdoptServices []string` / reuse `Scope`)
  naming the trigger hostname(s); derive from EXACTLY those (+ managed deps as EXISTS).
  Empty scope ≠ "all" — return a candidate-list diagnostic (mirror `validateDevelopScope`).
  Explicit `adoptAll:true` for the genuine whole-project case. Lift `isControlPlaneType` into
  `adoptableServices` (route.go:291) so the control plane is excluded from BOTH menu and
  derive. Two-phase confirm before `completePlanWithTargets` persists.
- **Recipe (MINIMAL "basically works" now; deep solution BACKLOGGED — owner decision, Codex-corrected):**
  the REAL hard failure is the deploy-block: a recipe worker is created by import but, because it
  wasn't a plan target, gets no `ServiceMeta` → later `SERVICE_NOT_FOUND` blocks deploy. **Codex
  refuted my first minimal idea** (making `recipeRuntimeRole` give worker its own role is
  insufficient — `buildRuntimeSlots` still emits only dev/stage slots, `findRuntimeSlot` requires
  role equality, `RuntimeTarget` has no worker field, and `InferRecipeShape`≠"" also forces a
  `ValidateBootstrapRecipeMode` change — that creeps into the deep redesign). **The truly minimal,
  single-place fix:** for `route=recipe`, `writeProvisionMetas` (bootstrap_outputs.go:125) writes a
  `ServiceMeta` for EVERY recipe-imported runtime (from `RecipeMatch.ImportYAML`), not just the
  agent's plan targets. The recipe IS the authority on what exists; track what was actually
  provisioned. This closes the deploy-block (the hard failure) even when the agent drops the worker
  from the plan (the current c1 workaround now succeeds end-to-end). The plan-authoring friction
  ("no recipe service matches" → agent drops worker) stays as annoyance, NOT a block.
  **This is bootstrap-core, NOT Aleš's scope** (route=recipe is one of the three bootstrap routes
  per the bootstrap spec; `internal/recipe/`/`zerops_recipe` is the unrelated *authoring* engine —
  verified it doesn't import these files). *Preserves:* `RewriteRecipeImportYAML` + recipe-match
  untouched; the agent still authors the plan for now (no derive yet).
  **DEEP solution → `plans/backlog/recipe-plan-validator-worker-blind-spot.md`:** kill the friction
  entirely — derive the WHOLE plan from the recipe YAML (agent authors nothing, mirroring
  `0384ce5d` for adopt) + a first-class `RecipeShape`/worker-role on the plan. Promote when
  worker/multi-runtime recipes proliferate.

### G3 — Lifecycle truth (F6)
**Pure presentation fix; the derive engine is byte-for-byte untouched.** (1) Delete the false
imperative at `deploy_local.go:261`; replace with the truthful reversible signal: *"All
declared services deployed + verified — scope is green. Keep deploying into this session
(nothing is lost); call `action=close` when the task is actually done."* (2) Split the
agent-facing status: `scope-green` (reversible, session open on disk) vs the genuinely
terminal `closed` (explicit close deletes the file; iteration-cap stamps) — make the
reversible/terminal distinction structural. (3) The unset→hangs-open direction: surface
`AutoCloseProgress.Reason` (work_session.go:528, already populated) — *"scope green but
auto-close OFF because `<svc>` has no close-mode."* *Preserves:* `DeriveCloseState` /
`EvaluateAutoClose` / `ClosedAt==""` invariant / `TestNoRawClosedAtReads` all stay →
compaction determinism (P5) + manual-mode gate (P6) survive. The fix lives only at the
presentation seam, where the magic lived.

### G4 — Parallel work (F4 + cross-session F3)
**F4 — per-source git serialization.** Add a per-source keyed mutex (`sync.Map` of
`source.Name → *sync.Mutex`) in ops; `DeploySSH` acquires it around the `ExecSSH` into
`source.Name`. **This is the EXACT pattern already in the codebase** —
`ops/browser.go::browserMu` serializes a shared per-container daemon. `DeployBatchSSH` keeps
its goroutine fan-out; distinct sources stay parallel, same-source serialize. Fix the
doc-comment (it currently asserts the wrong invariant — "safe across distinct hostnames" —
about the TARGET; the contended resource is the SOURCE). *Preserves:* the server-side
parallelism that dodges the MCP "Not connected" penalty; the load-bearing commit (untouched —
serialized, not removed).
**F3 cross-session — never silently claim another session's work.** Extend
`adoptableServices` (or a scoped variant on the auto-derive path) to EXCLUDE any runtime
whose `ServiceMeta.BootstrapSession` names a DIFFERENT *alive* session (reuse the
`isProcessAlive(PID, StartTime)` predicate from `checkHostnameLocks`). A bare unmanaged
runtime with NO meta is ambiguous → the derive must NOT silently claim it; return the
confirm-shaped response. *Preserves:* single-agent own-services still derive frictionlessly
(goal 2); only the cross-session/ambiguous case asks first (goals 3+4).

### Verify — setup-aware classification (F5)
**HTTP-expectation derives from the DEPLOYED SETUP, which ZCP already computes and discards.**
(1) Add `topology.RuntimeWorker` enum value. (2) At deploy, `ZeropsYmlEntry.HasPorts()`
(deploy_validate.go:218) is already computed on the setup block — persist its result on
`ServiceMeta.ServesHTTP *bool` (pair-keyed, pointer so absent≠false), through the existing
`UpdateServiceMeta` lock. (3) `verify` already reads ServiceMeta (verify.go:60,80) — thread
`ServesHTTP` into `classifyRuntime` as the AUTHORITATIVE signal (false → RuntimeWorker even
for php-nginx); base-image type stays as FALLBACK for services with no recorded setup
(adopted/external). Report provenance in the result ("classified worker from `setup=worker`").
*Preserves:* scheme-based port SELECTION (bef5ccd9 — gates *whether*, not *which*); no extra
network probe (13fa32fb — reads disk); composite matching (0696f646 — the fallback path);
`TestVerify_ImplicitWebserver_SkipsStartup` stays green (no recorded ServesHTTP → fallback
HTTP-probes as before). Live-proven necessity: worker and web both report `port 80,
scheme:http` — the ports DTO can NOT distinguish them; only the deployed setup can.

---

## Codex machinery-review verdicts (refinements to the restorations above)

Adversarial pass against the actual code. Verdicts:
- **F8 — Codex correctly refuted "prune at sync = pure subtraction"; owner-decided the real fix.**
  `120e36f4` deleted the ONLY ACTIVE-status source (stack-types API); the schema can't answer
  "is this ACTIVE." **Owner decision (no masking heuristic): re-add the narrow
  `ActiveServiceTypeVersions()` fetch and active-filter the cache AT INGESTION** (see Layer-0
  "Source correctness" + the G1/F8 restoration). The cache stays the single source, made correct
  at the source; this is the inverse of `120e36f4`'s mistake (it dropped the unique fact along
  with the redundant one — we restore only the unique fact). The earlier "highest-concrete
  heuristic" is rejected as a fallback that masks the missing signal. **Deepest root: the
  single-source refactor deleted the ability to know what's importable — we fix that ability,
  not route around it.**
- **F1 resolver — holds, but provision-SCOPED.** Do NOT expose a broad `topology.TypeResolves`
  that could leak into catalog existence; keep the family→resolved logic inside `checkServiceType`
  (or a helper explicitly documented "provision-only, never for existence"). Shared
  `TypesAreEquivalent` stays exact-version (tests pin it); `HasServiceType` still rejects
  hallucinations.
- **F4 lock — holds; make it KEYED + ctx-aware.** `browserMu` is analogous only as "serialize
  shared external state" — it's global; deploy needs a lock keyed by resolved `source.Name`
  (+ maybe `workingDir`), acquired inside `DeploySSH` around the git/push critical section and
  released BEFORE `pollBuild`. One lock per call, no nesting → no deadlock.
- **F5 — holds; the base-image FALLBACK is refuted for two paths.** Default/SSH/batch deploys go
  through `deployPreFlight` (parsed setup available → record `ServesHTTP`). But **git-push skips
  preflight** (deploy_ssh.go:168, deploy_local.go:107) and `record-deploy` only stamps deploy
  metadata — both must COMPUTE `HasPorts()` from the YAML they already have, NOT fall back to
  base-image type (which re-misclassifies a worker). Base-image fallback is correct ONLY for
  genuinely-never-ZCP-deployed (adopted/external) services.
- **Adopt — holds; don't blanket-confirm (preserve goal 2).** An EXPLICIT scoped adopt commits in
  ONE call (no friction); only an UNSCOPED or multi-service-inferred derive previews + confirms.
  `adoptableServices` excludes system/managed/self but NOT control-plane type generically — lift
  `isControlPlaneType` in (route.go:291).
- **Recipe derive — holds; use recipe-v3's first-class worker fields.** `recipeRuntimeRole` folds
  worker→stage, but recipe v3 already has first-class worker/shared-codebase fields (recipe.go:150);
  the derive should use them. The `workflow=recipe` authoring path is blocked (workflow.go:564) —
  F2 is the bootstrap `route=recipe` path (recipe_override.go), Aleš-adjacent via shared structs.
- **Lifecycle — note rewrite is presentation-only (safe); status taxonomy is a CONTRACT.**
  `WorkSessionState.Status` (`open|auto-closed|none`) is pinned by tests; splitting it is a
  contract change, do it deliberately (or keep the wire value, change only the human note +
  a derived display field). Also: explicit terminal close is today indistinguishable from `none`
  after file deletion — worth fixing alongside.
- **F8b — holds; data already exists.** Service-level import errors ALREADY become
  `serviceErrors[].meta` (ops/import.go:190); the gap is that a partial import is a NON-error tool
  result (import_test.go:121). Fix = PROMINENCE (surface `serviceErrors` in summary/nextActions),
  NOT new error plumbing.

## Coverage hardening — so a goal can never silently regress again

The reason all of these shipped green: the eval/test matrix tests the happy path on common
techs. Add, as part of this remediation:
- **Eval scenarios** exercising the failing axes: a version-expanding runtime
  (`go@1`/`rust@1`/`bun@1.3`), a non-ACTIVE schema version, two **concurrent** sessions in one
  project, a worker-bearing recipe through to deploy, a ≥2-target same-source cross-deploy.
- **Invariant tests** pinning each goal: provision reconciles family→resolved (G1);
  discover⟹importable (G1/F8); adopt-derive is scoped + never claims an unrelated service
  (G2/G4); the auto-close note never says "start a new session" (G3); per-source deploy
  serialization (G4); a worker is never HTTP-probed (F5).

---

## Sequencing — verifiable phases (RED→GREEN, ≤~5 files each, unit tests land WITH each phase)

| Ph | Scope | Goal | Files (approx) | Dep | Aleš? |
|----|-------|------|----------------|-----|-------|
| **0a** | **SOURCE CORRECTNESS (do first — everything depends on it).** Re-add narrow `client.ActiveServiceTypeVersions()` (SDK `PostServiceStackTypeSearch`, `Status==ACTIVE`); active-filter the type enum at `schema.Cache` refresh + `make schema-sync`. The cache becomes correct → `HasServiceType("deno@1")=false`, all downstream consumers correct for free | 1 | `platform/client.go`+`zerops_search.go` (narrow re-add), `schema/cache.go`+`sync.go`, `catalog/sync.go` | — | no |
| **0b** | **Layer 0 — PRESENTATION (FRONT LINE).** `RecommendedStacks` (one concrete version/tech from the now-correct cache) + fix `needsStacks` (discover-only, kill completed-leak) + drop start-commit/skip attaches + retire "pick the highest from availableStacks" guidance | 1 | `knowledge/versions_format.go`+`catalog_view.go`, `tools/workflow_bootstrap.go`+`workflow.go`, `content/workflows/*` | 0a | no (catalog); guidance partly Aleš |
| **0c** | Develop-Guidance decision/reference split + cross-call suppression + size budget; route-menu/scope/F7 de-bloat | 1 | `workflow/synthesize.go`, develop/status handlers, `route.go`, `knowledge.go` | 0a | no |
| **1** | G1 machinery BACKSTOP — provision-scoped acceptance rule (concrete-leaf→exact; selector→same-base accept) + record live type + F8b attribution (data already in `serviceErrors[].meta` — prominence only) | 1 | `tools/workflow_checks.go`, `workflow/engine.go`+`bootstrap.go` (persist discovered type), `ops/import.go`+summary | 0a | no |
| **2** | G4 parallel safety | 4 | `ops/deploy_batch.go`+`deploy_ssh.go` (per-source mutex+doc), `workflow/adopt.go`+`route.go` (exclude alive-foreign-session) | — | no |
| **3** | G2/G3 adopt scoping + transparency | 2,3 | `workflow/adopt.go`, `route.go` (isControlPlaneType lift), `tools/workflow_bootstrap.go` (scope input + confirm round-trip) | 2 | no |
| **4** | G3 lifecycle truth | 3 | `tools/deploy_local.go::sessionAnnotations` (note + status split + unset cue), `verify.go` note, atoms/spec | — | no |
| **5** | Verify setup-aware | (verify) | `topology/types.go` (RuntimeWorker), `workflow/service_meta.go` (ServesHTTP), deploy record site, `ops/verify_checks.go`+`verify.go` | 1 | no |
| **6** | F2 recipe worker — MINIMAL "basically works": `writeProvisionMetas` writes a meta for EVERY recipe-imported runtime (route=recipe), not just plan targets → closes the SERVICE_NOT_FOUND deploy-block even when the agent drops the worker. Deep derive-from-YAML BACKLOGGED | — | `workflow/bootstrap_outputs.go` (writeProvisionMetas, route=recipe branch) | — | no (bootstrap-core; Aleš FYI only) |
| **7** | Coverage hardening (eval) | all | new flow-eval scenarios + cross-cutting invariant tests | 1–6 | partial |

**Why this order:** Phase 1 is the critical unblock (every classic/recipe bootstrap with a
version-family alias or a non-active version is broken today) and is fully internal. Phase 2
makes concurrent eval/work safe (needed before running the new concurrent-session eval
scenarios). Phase 3–5 are correctness/UX, independent. Phase 6 (recipe) is the biggest and
needs Aleš — schedule deliberately; it depends on Phase 3's confirm-round-trip primitive.
Phase 7's eval scenarios are the regression backstop so no goal silently re-breaks.

**Unit-test invariants land per phase** (full list per area above). The headline pins:
`TestCheckServiceType_SelectorAcceptsResolved` (go@1↔go@1.22 pass + record; nodejs@22↔nodejs@24 still FAIL) + `TestSchemaCacheActiveFiltered` (deno@1 absent) (P0a/P1);
`TestDeployBatchSSH_SerializesPerSource` + `TestBootstrapCompleteAdoptPlan_ExcludesConcurrentSessionServices` (P2);
`TestBootstrapCompleteAdoptPlan_ScopedToNamed` + `_DerivedPlanReportedBeforeCommit` (P3);
`TestNoFalseTerminalImperativeInLifecycleNote` (P4);
`TestVerify_PhpNginxWorker_RecordedSetup_NoHTTPChecks` + `TestRuntimeWorker_PromotedToTopology` (P5);
`TestRewriteRecipeImportYAML_RejectsUnrepresentedRuntime` + `TestRecipeRuntimeRole_WorkerDistinctFromStage` (P6).

**Eval scenarios to add (P7):** classic `go@1`/`rust@1` (P1 — was the silent freeze);
a recipe with a non-active version (F8); **two concurrent sessions in one project** sharing
adoptable services (F3/G4); worker-bearing recipe through to deploy (F2 + F5); a ≥2-target
same-source cross-deploy (F4). These are exactly the axes the existing node/php matrix missed.

---

## P0c — Develop-guidance de-bloat: study findings + design (2026-06-03)

**Study method:** 5-reader workflow + throwaway `Synthesize` measurement against the real corpus
(deleted post-run). All numbers below are MEASURED, not estimated.

### Empirical reality (post the 2026-04-26 trim, which is already shipped)
The prior trim emptied `knownOverflowFixtures` — the tested shapes are under the 28 KB soft cap
(`TestCorpusCoverage_OutputUnderMCPCap`). So P0c is NOT "fix truncation" (done); it's "parked at
the cap + re-firing generic mechanics every turn."

| Surface | Atoms | Bytes | vs P0c target |
|---|---|---|---|
| First develop call (container, never-deployed) | 24 | ~27.5 KB | ≤12 KB → ~2.3× over |
| First develop call (local, never-deployed) | 24 | ~31 KB | inflated by a dup bug (below) |
| Status re-fire (deployed, closeMode=unset) | 16 | ~18 KB | ≤4 KB → ~4.5× over |
| Status re-fire (deployed, closeMode=auto) | 16 | ~20 KB | ≤4 KB → ~5× over |

- **TWO surfaces only** carry develop guidance, both re-run `Synthesize` byte-identically:
  `workflow_develop.go::renderDevelopBriefing` (start) + `workflow.go::handleLifecycleStatus`
  (status, L1057). `deploy`/`verify`/`env` are LEAF (no guidance). So "subsequent bloat" = the
  status re-dump, not spread across tools.
- Ordering = `Priority` asc then `ID` asc (`synthesize.go:89`). **Fragile:** `develop-strategy-review`
  (the close-mode decision) is buried at ordinal **7/18 = 37%** purely because `s` sorts after
  `c/d/e/h/p` in the pri=2 band. The "buried at 36%" claim is confirmed exactly.
- **13–14 reference atoms (~17 KB) appear byte-identically on BOTH first AND iterate calls.**
  Same envelope on both → static gating CANNOT reduce them; only pointer-render or suppression can.
- **22 develop-active atoms (~36.5 KB on disk) have NO state gate**; the 7 with `phases:[develop-active]`
  as their ONLY axis (deploy-modes, platform-rules-common, env-var-channels, verify-matrix,
  auto-close-semantics, http-diagnostic, knowledge-pointers = 10,982 B) re-fire on EVERY develop call.

### Taxonomy (all 61 develop-*.md classified)
40 decision (61 KB) · 16 reference (30.5 KB) · 4 cheatsheet (3 KB, already optimally gated — leave) ·
1 intro. Prime de-bloat targets = the 14 ungated reference-class atoms. Biggest reference atom:
`develop-strategy-awareness` (3429 B, ungated, fires every deployed call).

### The design decision: STATIC mechanisms only — DROP cross-call suppression
The plan's "cross-call suppression of already-delivered atom bodies" is the ONE stateful piece and
should be **cut**. Confirmed risks (R2): it needs a per-PID delivered-atom set persisted in
WorkSession, which (1) breaks `Synthesize`'s byte-determinism contract (`f(envelope)` only),
(2) breaks compaction recovery (`action=status` MUST rebuild from envelope alone — a suppressed
body becomes unrecoverable), (3) breaks `scenarios_test.go` (pins `f(envelope)→atoms`), (4) turns
a pure read into an RMW write under `workSessionMu`. **This is the exact desync bug class CLAUDE.md's
auto-close history warns about.** It is also unnecessary — the same KB reduction is achievable with
two stateless levers:

1. **Pointer-render the 16 reference atoms** (platform mechanics: deploy-modes, env-var-channels,
   platform-rules-*, verify-matrix, http-diagnostic, auto-close-semantics, strategy-awareness,
   reserved-env-names, knowledge-pointers, …). Emit `id + one-line title` instead of the body;
   agent dereferences on demand. **The dereference primitive ALREADY EXISTS** —
   `LookupAtomBody(corpus, id)` (atom.go:411) + the knowledge `uri=` Mode 5 (knowledge.go:202) +
   the `action=dispatch-brief-atom atomId=` precedent (workflow.go:759). A pointer stub is
   byte-deterministic (same stub for same envelope) and compaction-safe (status re-emits it; agent
   re-dereferences). Collapses the 17 KB shared wall on BOTH calls. NO new state.
2. **Static gating** — push `deployStates`/`envelopeDeployStates` onto the ~8 still-ungated DECISION
   atoms (dynamic-runtime-start-{container,local}, checklist-*, change-drives-deploy, local-workflow,
   close-mode-manual) so first-call shows build-up, deployed status drops them. Pure frontmatter edit.

**Budget feasibility:** ≤4 KB subsequent is cleanly reachable (reference→pointer + deployed-gate
drops most decision atoms; only the close-mode decision + a few iterate atoms remain). **≤12 KB
first-call is NOT reachable by pointer+gating alone** — the first-deploy DECISION atoms (the
load-bearing "how to get your first deploy working" guidance) are ~15 KB and MUST stay inline.
Forcing 12 KB means also condensing decision prose (Axis 2/3 of the 2026-04-26 trim taxonomy) =
content rewrite = the real regression risk. **Recommendation: soften first-call target to ~14–16 KB
(from 28) and keep decision guidance rich; reserve decision-prose condensing for a separate, eval-gated pass.**

### Close-mode hoist — render-layer, not atom-priority
Precedent exists: `render.go` already emits load-bearing lines ABOVE the Guidance wall
(`renderProgressAndBlockers` auto-close-blocked line L204, `renderProdSourceControlSignpost` L50),
gated on stable envelope fields, NOT as atoms. So hoist the close-mode decision as a **render-layer
line** above Guidance (stateless, no atom-priority change). Fallback if a render line is too heavy:
single-atom priority bump `develop-strategy-review` 2→1 (surgical — moves only that atom; bumping a
GROUP reshuffles the crowded pri=2 band of 24 atoms). Check `docs/spec-scenarios.md` /
`scenarios_test.go` for any fixed-neighbor close-mode ordering assertion before bumping.

### Free win — duplication bug
`develop-platform-rules-local` renders **2×** on standard dev/stage pairs (2400+2394 = 4.8 KB) — it
declares `runtimes:[dynamic]` (matches appdev+appstage) AND uses the real `{stage-hostname}`
placeholder, so the two post-substitution bodies differ by 6 bytes and defeat the dedup at
`synthesize.go:139/168`. Fix: `multiService:aggregate` or drop the per-service axis (it's env-wide,
not service-wide). ~2.4 KB free local-mode saving, atom-edit only.

### Cross-response sub-items (all stateless, no new machinery)
| Item | Dnes | Po opravě | Site |
|---|---|---|---|
| **Route-menu** | inlines full import.yml ×3 candidates = ~10 KB worst case; agent picks 1, discards ~6 KB | drop `ImportYAML` at menu time (keep slug+fit+why+collisions); already re-derived for the CHOSEN recipe at provision (`bootstrap_guide_assembly.go:72`). 10 KB→0.9 KB (91%) | `route.go:225` |
| **scope=infrastructure** | 37.4 KB monolith (core.md 28.8 + model.md 8.6) on every YAML-authoring call | return a SECTION INDEX (16 H2 titles, ~2 KB) + on-demand body via existing Mode 5 `uri=` | `knowledge.go:138` |
| **F7 dereference** | search returns 300-char snippet + a `uri`, but agent is never told the two compose | LARGELY DONE — `uri=` Mode 5 already fetches full body. 2 prose edits + 1 static `hint` field on the search response | `knowledge.go:34,177` |

**One primitive serves all:** the `uri=`/`LookupAtomBody` dereference already exists and covers
develop reference atoms + infra-scope sections + search dereference. No parallel machinery.

### Aggressiveness dial (the owner's decision)
The single choice that changes the result: **are platform-mechanics reference atoms inlined on the
FIRST develop call, or pointer-ized there too?** Pointer-izing on first call → smaller first response
but the agent must dereference (e.g.) `platform-rules` before its first deploy (risk: it skips the
read, makes a platform mistake the inline body would've prevented). Three settings:
- **Conservative:** reference inline on first (never-deployed) call, pointer on iterate. First ~16 KB,
  iterate ~5 KB. Lowest regression risk.
- **Moderate:** reference pointer always; decision inline always. First ~16 KB (decision-dominated),
  iterate ~4 KB.
- **Aggressive:** + condense first-deploy decision prose to force ≤12 KB first. Highest risk; needs eval.

All three need a **flow-eval gate** to prove no guidance regression before/after.

### Owner steer (2026-06-03): AGGRESSIVE, content-level, real-situations, iterative
Karel chose aggressive + a method: read ALL content fully; pull the EXACT MCP output for real
scenarios (these are LLM instructions, not human docs); have fresh LLMs judge what's dead weight;
trim in several rounds; flow-eval continuously. Infinite budget. Start by understanding what/how/why.

### Ground-truth capture (faithful, real render path)
Harness: `internal/workflow/zz_p0c_dump_test.go` (THROWAWAY — delete before commit). Runs the EXACT
handler render path (`Synthesize`→`BodiesOf`→`BuildPlan`→`RenderStatus`) over an 18-scenario matrix;
writes each full MCP response + all 61 atom bodies to `/tmp/p0c-dumps/`. Run:
`go test ./internal/workflow/ -run TestP0CDump`.

**Three fixture-fidelity traps caught + fixed (why "real situations" matters — none are product bugs):**
1. managed deps have NO ServiceMeta → `Bootstrapped=false` in the real envelope (not true); else
   `envelopeDeployStates:[never-deployed]` falsely keeps first-deploy atoms alive forever.
2. `buildOneSnapshot` calls `normalizeDeployDims` → empty closeMode heals to `"unset"`; without it the
   `unset`-gated close-mode picker (`strategy-review`) never fires in the dump.
3. `Next: Primary: Close` on never-deployed/unset is a fixture artifact: `planDevelopActive` only emits
   deploy/verify primary when `env.WorkSession != nil`; dumps don't attach one. Real sessions derive it right.

**Faithful numbers (post-fix):** first call (never-deployed) 21–24 atoms / 26–33 KB wire; iterate
(deployed-unset) 16–19 atoms / 19–25 KB; iterate (deployed-auto) 22 atoms / 22–29 KB. Targets: ≤12 KB
first / ≤4 KB iterate → ~2–6× over everywhere.

**Two REAL findings (envelope-independent):**
- **MCP-cap breach:** scenario 15 (node standard LOCAL never-deployed) = **32,759 wire bytes > 32 KB**
  → truncation risk. The `develop-platform-rules-local` ×2 dup (~4.8 KB) is a contributor.
- **Dup bug:** `develop-platform-rules-local` renders twice on standard pairs (it has `runtimes:[dynamic]`
  → matches appdev+appstage AND uses the `{stage-hostname}` placeholder → 6-byte body delta defeats the
  post-substitution dedup). Fix: `multiService:aggregate` or drop the per-service axis.

### Content audit (full read of all 61 develop atoms) — duplication clusters
The same fact is re-stated across atoms that assemble into ONE response:
- **dev-server lifecycle ~5–6×** (`dynamic-runtime-start-{container,local}`, `dev-server-triage`,
  `dev-server-reason-codes`, `close-mode-auto-workflow-dev`, `platform-rules-local`, `asset-pipeline`):
  "start via dev_server, don't `ssh cmd &`, re-start after redeploy."
- **env-vars ~5×** (`env-var-model`, `env-var-channels`, `first-deploy-env-vars`, `env-var-shell-usage`,
  `reserved-env-names`, `platform-rules-common`, `local-env-channels`): `${host_KEY}` cross-ref +
  wrong-spelling→literal-string failure.
- **auto-close ~4×** (`auto-close-semantics`, `close-mode-auto`, `strategy-review`, `strategy-awareness`,
  `first-deploy-intro`, `standard-unset-iterate`).
- **three deploy-config axes ~3×** (`strategy-awareness` 3 KB full + `strategy-review` + `close-mode-*`).
- **verify ~4×** (`http-diagnostic`, `verify-matrix`, `first-deploy-verify`, `dev-server-triage`).
- **deploy-classes/deployFiles ~3×** (`deploy-files-self-deploy`, `scaffold-yaml`, `platform-rules-common`).

**Pure-reference / pointer candidates:** `strategy-awareness` (3 KB, fires every deployed call,
re-explains axes already chosen), `reserved-env-names` (2.6 KB, 3-regime enumeration),
`mode-expansion` (1.4 KB, rare infra change), `dev-server-reason-codes` (1.3 KB diagnostic table),
`local-env-troubleshoot` (1.4 KB error recovery), `record-external-deploy` (2.1 KB CI bridge),
`build-observe` (1 KB git-push failure path).

**Structural:** 9 near-duplicate `close-mode-auto-*` variants (mode×env) each re-explain auto-close +
switch syntax; only one fires per call but the corpus carries all 9. **Ordering chaos:** alphabetical-by-ID
puts `env-var-model` deep-dive before the `first-deploy-intro` orientation on the first call.

### LLM-judge verdicts (8 fresh agents role-played each state) — strong convergence
Genuine signal estimated ≈ **5–7 KB first-call / 3–5 KB iterate** (the LLMs themselves — more aggressive
than the ≤12/≤4 plan targets). Consensus verdicts:
- **Spine (KEEP):** first-deploy-branch flow, Establish zerops.yaml, Write app code, npm-install-not-ci, Run first deploy, Verify, Promote-to-stage, Pick close-mode.
- **Unanimous CUT (7×):** "Why source tree doesn't have ./out" (derivable).
- **TRIM-everywhere (redundant, fire every call):** Work-session-auto-close (7×), Platform-rules (7×), Per-service-verify-matrix (6×), Knowledge-on-demand (6×), Self-deploy-destruction (5×, pre-flight enforces it structurally → prose redundant with the check).
- **MOVE (wrong-state on first call):** HTTP-diagnostics + "Every code change must reach durable" + "Dynamic-runtime dev server" (the last actively misleads on STANDARD mode — standard starts on deploy, no dev_server).
- **POINTER:** Reserved-env-keys (3-regime catalog = validation-set, show 1-line trap), Env-var-channels.

### THE TARGET SHAPES (from the ideal designers)
**First-call** (causal order, ~12 KB): (1) first-deploy orientation+flow, (2) Establish zerops.yaml,
(3) env-var wiring model + self-shadow 1-liner, (4) env catalog (fetch live keys), (5) write app +
checklist, (6) npm-install-not-ci, (7) run first deploy + failureClassification, (8) verify, (9) promote
to stage, (10) pick close-mode (gate 1-liner), (11) knowledge-on-demand pointer index (LAST = the safety
net for everything trimmed). Order is strictly CAUSAL (execution timeline), not topic-grouped.
**Iterate** (~4 KB): state snapshot → **close-mode decision FIRST, marked BLOCKING** → iteration loop
(3-call dev sequence) → promote dev→stage → one "recover deeper guidance" pointer → Next.

### Dedup ownership (single owner per fact; non-owner → 1-line pointer; verify co-assembly before delete)
auto-close gate → `auto-close-semantics`(9 atoms restate); `${host_KEY}` ref + self-shadow →
`env-var-model`(7+4); dev-server lifecycle → `dev-server-triage`/`-reason-codes`(5+5+2); promote template
→ `first-deploy-promote-stage`(3); platform invariants (sudo/deploy=new-container/build≠runtime/override)
→ `platform-rules-common`(2–4 each); deployFiles self-deploy rule → `deploy-modes`. ~11.2 KB raw dup
(lower per-response due to env/route gating — keep container/local + close-mode-route parallel families split).

### EXECUTION ROUNDS (each: edit → re-dump measure → flow-eval gate)
**Round 1a — structural/axis only (no content loss, low-risk):**
- [ ] Fix dup bug: `platform-rules-local` → `multiService:aggregate` (kills the 2× render, ~2.4 KB + the cap breach).
- [ ] Fix gating bug: `dynamic-runtime-start-{container,local}` → `modes:[dev]` only (drop `standard` — standard starts on deploy; firing on standard misleads).
- [ ] Gate first-call-learning reference atoms to `envelopeDeployStates:[never-deployed]`: deploy-modes,
  deploy-files-self-deploy, http-diagnostic, verify-matrix, reserved-env-names, platform-rules-common,
  platform-rules-container, platform-rules-local, implicit-webserver, static-workflow. (These re-fire on iterate today; learned-once → first-call only, pointer-recoverable on iterate.)
- [ ] Causal ordering via priority re-assignment (bands: 0 orientation · 1 blocking-decision · 2 yaml ·
  3 env · 4 code · 5 deploy · 6 verify · 7 promote · 8 close-mode · 9 pointer-index).
- [ ] Close-mode hoist: `strategy-review` priority → 1 (top, after orientation), marked BLOCKING in body.
**Round 1b — content (dedup + TRIM + delete):**
- [ ] Delete `develop-auto-close-semantics` standalone (1.6 KB every call); tight gate 1-liner lives in the state atoms (first-deploy-intro / strategy-review / close-mode-auto); full mechanics → on-demand.
- [ ] Strip restated facts from non-owner atoms per ownership map → 1-line pointer (verify owner co-fires).
- [ ] TRIM pedagogical prose per judge verdicts (CUT "why source tree lacks ./out"; collapse deploy-classes
  + deployFiles + self-deploy-destruction to the tables + [.] rule; reserved-keys → 1-line trap; etc.).
**Round 2 (if first-call still > target):** move pure-reference bodies to `zerops_knowledge` topics +
pointer-render (reuse the existing `uri=`/`LookupAtomBody`/`dispatch-brief-atom` dereference primitive).
Harness: re-run `TestP0CDump` after each round; measure /tmp/p0c-dumps manifest; flow-eval on
greenfield-node-postgres (+ a local + a php scenario) between rounds.

---

## P0c PROGRESS (2026-06-03) — rounds 1a + 1b shipped + validated

Branch `feat/p0c-develop-guidance-trim` (NOT pushed). All green, throwaway dump harness
`internal/workflow/zz_p0c_dump_test.go` (delete before merge).

| State | P0c start | after 1a+1b | Δ |
|---|---|---|---|
| First-call | 27.8 KB | 24.5 KB | −12% |
| Iterate-unset | 19.4 KB | 9.7 KB | **−50%** |
| Iterate-auto | 29.3 KB | 19.9 KB | −32% |
| php iterate | 17.2 KB | 8.8 KB | −49% |

Commits: 1a (gate 6 reference atoms to never-deployed + platform-rules-local dup/cap fix +
strategy-review hoist), 1b chunk1 (close-mode command prominence + auto-close-semantics trim +
deploy-modes cut), chunk2 (env-var dedup + orientation-first ordering + reserved-keys trim),
chunk3 (env-var-model self-shadow trim + sudo-in-build fix), chunk4 (knowledge-pointers trim).
Plus backlog: local-iterate-gating, php-nginx-recipe-friction, cross-deploy-setup (2nd signal),
bootstrap-provision-guide-all-routes-dump.

**Diverse flow-eval pass — 6/6 scenarios COMPLETE, neutral prompts, ZERO P0c regression:**
greenfield-node-postgres (recipe/std) · develop-loop (classic/express/simple-dev) ·
recipe-laravel (recipe/php-nginx/std) · classic-rust-postgres (classic/rust/std) ·
adopt-existing-standard-pair (adopt/node/std) · recipe-nextjs-ssr (classic/node-SSR/std).
THREE positive confirmations of the changes: (1) close-mode prominence used correctly (rust);
(2) dev-server-on-standard kept correctly — the judge-override held (greenfield/rust);
(3) the reserved-keys TRIM caught the HOSTNAME trap (nextjs: agent "nearly set HOSTNAME:0.0.0.0…
caught it because the kb flags HOSTNAME as causing silent BUILD_FAILED in 4-5s zero logs" — the
exact signal I kept while cutting the catalog). All friction was pre-existing/bootstrap, never the
trimmed develop guidance. Pre-existing findings backlogged: `php-nginx-recipe-first-deploy-friction`,
`cross-deploy-setup` (2nd signal + self-deploy), `bootstrap-provision-guide-all-routes-dump`,
`deploy-failure-recovery-hint-and-classifier-gaps`, `local-develop-guidance-iterate-gating`.

## ROUND 2 DESIGN (decided 2026-06-03: Karel "Round 2 teď") — first-call ≤~14 KB via pointer-render

**Goal:** remove ~10 KB of inline REFERENCE from first-call (spine ~12 KB stays), keep it
RECOVERABLE on demand. Also closes the round-1a latent gap: reference atoms gated OFF iterate
are currently absent with no working pointer — round 2 gives them a resolvable fetch.

**Mechanism decision — single-source, NO knowledge-store duplication, NO new stateful state.**
Reject "move bodies to `zerops_knowledge` topics" (duplicates content + fuzzy `query=` resolution).
Instead: a thin STATELESS retrieval that returns a develop-corpus atom body by ID, rendered with the
live envelope's `{hostname}`/`{stage-hostname}`/`{project-name}` (the Synthesize replacer — NOT Go
templates; corpus atoms use `{...}`, the manifest `LoadAtomBodyRendered` uses `{{.X}}` and only
resolves manifest atoms, confirmed). Two impl options (pick at build):
- (A) new `zerops_workflow action="develop-atom" atomId=<id>` handler: `LookupAtomBody(corpus,id)` +
  apply the `{hostname}` replacer w/ primary-hostname picker → `{atomId, body}`. Cleanest separation.
- (B) extend `handleDispatchBriefAtom` to fall back to corpus lookup when `id` isn't a manifest atom.
  One retrieval action, but widens its recipe-flavored contract.
Recommend (A) — keeps dispatch-brief recipe-scoped; ~40-line stateless handler. The atom stays the
SINGLE owner of the body; the pointer references it by exact ID so it ALWAYS resolves (no dead pointer).

**✅ INCREMENT 1 DONE (committed `3d170cfd`)** — chose option (A): `zerops_workflow
action="develop-atom" atomId=<id>` → `handleDevelopAtom` (tools/workflow.go) does
`LoadAtomCorpus()` + `LookupAtomBody(corpus,id)` → `{atomId, body}`, RAW body (the 5
pointer-render candidates are placeholder-free, so no substitution needed; http-diagnostic
HAS `{hostname}` so it is NOT a candidate). Stateless (dispatched before the engine guard).
Wired: dispatch + Action jsonschema + valid-actions list + `content.AcceptedWorkflowActions`
(AST lint). Tests: `TestWorkflowTool_DevelopAtom_ReturnsBody` / `_UnknownErrors`. Standalone
value: the round-1a reference gated OFF iterate is now recoverable on demand.

**INCREMENT 2 (NOT STARTED — the first-call shrink) — precise checklist for a fresh context:**
- [ ] **`reference` flag**: add `reference` to `validAtomFrontmatterKeys` (atom.go) + a `Reference bool`
  field on `KnowledgeAtom` + parse `fields["reference"]=="true"` in `ParseAtom`. (Attribute, NOT an axis.)
- [ ] **Synthesize stub render**: in `Synthesize` (synthesize.go), when `p.atom.Reference`, emit a
  one-line stub instead of the body: ``**<Title>** — pull on demand: `zerops_workflow action="develop-atom"
  atomId="<id>"` `` (Title from `atom.Title`). Keep the dedup `seen` logic. The full body stays in the
  corpus (single source) so the fetch returns it.
- [ ] **Flip the 5 PLACEHOLDER-FREE reference atoms** to `reference: true`: develop-deploy-modes,
  develop-env-var-channels, develop-platform-rules-common, develop-platform-rules-container,
  develop-reserved-env-names. (NOT http-diagnostic — it has `{hostname}`; keep it as-is/gated.
  NOT verify-matrix, dynamic-runtime-start, the spine — keep inline.)
- [ ] **Aggressive dial (Karel's choice):** these are gated `envelopeDeployStates:[never-deployed]`
  (round 1a) so they only fire first-call — flipping to `reference` pointer-renders them ON FIRST-CALL.
  FLOW-EVAL is the judge: if an agent fails to author yaml because it skipped fetching deploy-classes/
  platform-rules, back off (un-flip the spine-adjacent ones, keep them inline-trimmed).
- [ ] **Migrate goldens**: `ZCP_UPDATE_ATOM_GOLDENS=1 go test ./internal/workflow/ -run TestScenarios_GoldenComparison`
  then eyeball the diffs (bodies → stubs is expected).
- [ ] **Migrate coverage fixtures** (`corpus_coverage_test.go`): the 5 atoms' body phrases become
  FETCH-ONLY (not in any synthesized response) → `MustContain` pins on those phrases break. For each:
  re-home the pin to the surviving stub text (`pull on demand` / the title) OR drop it if the atom's
  presence is otherwise pinned. `PinCoverage_AllAtomsReachable` must stay green (the atoms still fire,
  as stubs).
- [ ] **Pinning test**: every pointer's `atomId` resolves via `LookupAtomBody` (no dead pointers) —
  iterate the corpus, assert each `Reference` atom is fetchable + its stub names its own id.
- [ ] **Size budget**: tighten the soft cap or add a first-call ≤~14 KB assertion.
- [ ] **Re-measure**: `go test ./internal/workflow/ -run TestP0CDump` → check `/tmp/p0c-dumps/00_MANIFEST.md`
  (first-call should drop ~24.5 → ~14-16 KB).
- [ ] **Run `./internal/tools/...` + `./internal/content/...` + `./internal/workflow/...` ALL** (the
  chunk-1 miss was from skipping `./internal/tools`).
- [ ] **flow-eval gate**: greenfield-node-postgres + a php (recipe-laravel) + the iterate
  (develop-loop-after-bootstrap) — confirm agents fetch what they need + no regression.

---

## HANDOFF STATE (2026-06-03, pre-compaction) — read this first to resume

- **Branch** `feat/p0c-develop-guidance-trim` (NOT pushed, NOT merged). 11 commits: 1a · backlog ·
  1b chunk1-4 · 3 backlogs · round-2 design · round-2 increment 1. All GREEN
  (`go test ./internal/tools/... ./internal/content/... ./internal/workflow/... -count=1`).
- **Throwaway** `internal/workflow/zz_p0c_dump_test.go` (uncommitted) is the measurement harness
  (`TestP0CDump` → `/tmp/p0c-dumps/`). DELETE before merge. (Untracked `plans/guide-*` are NOT mine.)
- **Done**: rounds 1a + 1b (iterate −50%: 19.4→9.7 KB; first-call −12%: 27.8→24.5 KB; MCP-cap breach
  fixed) + round-2 increment 1 (develop-atom fetch). Validated by 6/6 diverse flow-evals (zero regression,
  3 positive confirmations).
- **NEXT**: round-2 INCREMENT 2 checklist above (the first-call shrink via pointer-render). It's the
  remaining P0c work; it rewires `Synthesize` + migrates goldens/coverage + needs the flow-eval gate.
- **Local-mode gating**: deliberately conservative (reverted) — backlog `local-develop-guidance-iterate-gating`
  (can't verify: flow-eval-local needs `make install`, prohibited without explicit ask).
- **Hard constraints**: NO `make release`/`make install` without explicit ask; NO push/merge without ask;
  flow-eval is the behavioral judge (neutral prompts — never tune them); unit tests (goldens/coverage)
  are structural pins, not goodness proof.
