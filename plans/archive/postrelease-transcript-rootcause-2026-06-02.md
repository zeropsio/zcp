# Post-release transcript root-cause analysis (2026-06-02)

Source: two concurrent agent sessions against the SAME live project `zcp-eval`
(`2Biyb7d2TQeSum9HNtjLQQ`, org KRLS), both inside the `zcp` control-plane
container (binary `v9.107.0`, ≈ released):

- **c1** — Codex (`codex-tui`), Laravel WeTransfer clone via `laravel-showcase`
  recipe → appdev/appstage/workerstage + db/redis/storage/search. Succeeded
  after recovering from 4 friction points.
- **c2** — Claude Code (`claude-vscode`), Bun weather dashboard (classic) →
  `app`, then a 2nd turn added `mailpit` + `subdb`. Succeeded after a full
  bootstrap **reset** cycle.

Both finished with a working, verified app. The findings below are the friction
they fought through — every one is a *substance* (wrong owner / wrong invariant /
wrong abstraction) issue, not a surface bug.

Methodology: each finding confirmed against HEAD source + an adversarial pass;
F1/F5 additionally **live-verified** against `zcp-eval` via a HEAD MCP server
driven over JSON-RPC. Live data overturned two plausible-but-wrong fixes.

---

## Finding severity map

| ID | Title | Severity | Confirmed | Routes affected |
|----|-------|----------|-----------|-----------------|
| F1 | Provision type-check rejects platform-resolved version aliases; no in-band amend → forced reset | **CRITICAL** | code + LIVE | classic + recipe |
| F2 | Recipe-route plan is agent-re-authored; dropped recipe service → no meta → deploy blocked | **HIGH** | recipe |
| F3 | Adopt auto-derive sweeps ALL adoptable services (incl. other apps / control plane) | **HIGH** | adopt |
| F4 | Parallel cross-deploys from one source collide on `.git/config.lock`/`index.lock` | **HIGH** | deploy_batch cross |
| F5 | verify HTTP-probes a no-HTTP worker → false "degraded"; port DTO can't distinguish | **MEDIUM-HIGH** | verify |
| F6 | Auto-close fires on first green deploy+verify; "start a new session" note is false | **MEDIUM** | develop |
| F7 | knowledge search emits `zerops://` URIs that don't dereference | **MEDIUM** | knowledge |

---

## F1 — Provision type-check vs platform version resolution  **[CRITICAL, systemic]**

### What happened
c2 planned `bun@1.3` (a real catalog entry). Platform resolved import to
`ubuntu/bun@1.3.9`. `complete step=provision` returned:
`checkResult.app_type = fail "expected bun@1.3, got ubuntu/bun@1.3.9"`,
`message: "provisioning incomplete — fix issues and retry"`. But the *only*
broken thing — the frozen discover-plan type — **cannot be amended at provision**
(`complete step=discover` → "current step is provision"). The sole escape was a
destructive `action=reset` + full re-run. "Fix and retry" is an impossible
instruction.

### Live blast-radius (measured on zcp-eval, HEAD server)
Provisioned 8 runtimes with the version-family alias an agent picks from the catalog:

| authored | platform resolved | provision `_type` |
|---|---|---|
| `nodejs@22` | `ubuntu/nodejs@22` | ✅ pass |
| `dotnet@8` | `ubuntu/dotnet@8` | ✅ pass |
| `bun@1.3` | `ubuntu/bun@1.3.9` | ❌ FAIL |
| `elixir@1.16` | `ubuntu/elixir@1.16.2` | ❌ FAIL |
| `gleam@1.5` | `ubuntu/gleam@1.5.1` | ❌ FAIL |
| `deno@2` | `ubuntu/deno@2.0.0` | ❌ FAIL |
| `go@1` | `ubuntu/go@1.22` | ❌ FAIL |
| `rust@1` | `ubuntu/rust@stable` | ❌ FAIL |

**6 of 8 runtimes fail.** `go@1` and `rust@1` are first-listed catalog entries —
agents pick exactly the forms that break. `rust@1` → `stable` (numeric→tag).
Also extends to **recipe routes**: `checkProvision` is shared (container-mode
recipe runs the dev type-check); `bun-hello-world` declares `bun@1.2` →
`bun@1.2.2` → its own curated recipe would fail provision.

### Root cause (substance)
`topology.TypesAreEquivalent` (`type_equivalence.go:149`) canonicalizes FORM
(OS prefix + mode suffix stripped) but treats the **version as an opaque exact
token**. The catalog is a version *hierarchy* (`bun@1 ⊋ bun@1.3 ⊋ bun@1.3.9`),
and `bun@1.3` is a **selector** the platform resolves to a concrete patch. Two
class errors compose:
1. The equivalence primitive lacks the **family⊇patch containment axis** it
   already has for OS/mode.
2. Deeper: ZCP compares the agent's **pre-resolution authored string** to the
   platform's **post-resolution stamped string at all**. The platform is the
   authority on version resolution; `checkServiceType` asserts byte/equivalence
   identity between an input the platform deliberately transforms and its output.
3. The discover plan is immutable once advanced (`engine` rejects re-completing
   discover), so a benign mismatch becomes session-fatal.

### Fundamental fix
**Stop comparing pre-resolution intent to post-resolution truth. Reconcile, don't
re-assert.** At provision, the service *exists* with the correct **base** (the only
thing the agent actually chose); adopt the platform's concrete resolved type into
the plan (`checkServiceType` becomes a base-match + self-heal of the stored
version), rather than failing on a version the platform itself produced.

Bandaid to avoid: "tell agents to always author concrete patch versions" — the
catalog advertises the family forms; this pushes platform-internal resolution onto
the user and still breaks `@latest`/`@stable`/recipes.

**ADJUDICATED FIX DESIGN (workflow adversary + Codex deep-dive, code-grounded, tests run):**

Do NOT touch `TypesAreEquivalent`. A symmetric dot-containment edit is *unsafe* —
`matchesAnyEquivalent` (catalog.go:61) calls it `(candidate, catalogValue)`, so
`HasServiceType("bun@1.5")` would false-accept against catalog `bun@1` (`["1"]` ⊆
`["1","5"]`), defeating "hallucination rejects"; also breaks `HasRunBase`/`HasBuildBase`.
Symmetry is pinned (`type_equivalence_test.go:99`).

Plan self-heal is right, but **"base-only match" is too weak** — `nodejs@22` vs
`ubuntu/nodejs@24` MUST still fail. Precise design:

1. New **provision-scoped** `matchProvisionRuntime(expected, actual)` in
   `internal/tools/workflow_checks.go` (NOT the shared primitive):
   ```
   if actual=="" → pass(no-update)
   if TypesAreEquivalent(expected,actual) → pass(update-if-different)
   if !both runtime-ish | canonical base differs | OS/mode decorations conflict → FAIL
   if expectedVersion=="latest" → pass(update)
   if dotComponents(expectedVer) is prefix of dotComponents(actualVer) → pass(update)   // 1.3 ⊆ 1.3.9
   if knownResolverAlias(expected,actual) → pass(update)                                 // rust@1 → rust@stable (dot-prefix misses this)
   else → FAIL                                                                           // nodejs@22 vs @24 stays a real mismatch
   ```
2. **Persist** the resolved live type into the plan via a new
   `Engine.ReconcileBootstrapRuntimeTypes(...)` from inside `checkProvision` (the checker
   already writes env/status state; engine.go:455 supports checker-side writes). Reconcile
   at **provision**, NOT discover — `validate_bc_test.go:39` pins that discover must not
   mutate plan types (recipe matching needs the submitted shape).
3. **Consumers stay correct:** catalog/recipe matchers + shared primitive unchanged →
   hallucinated versions still reject; recipe matching still exact (pre-resolution). Add
   negative guard tests (`TypesAreEquivalent("bun@1","bun@1.5")==false`, etc.).
4. **No-amend dead-end is MOOTED** by the self-heal (false mismatches no longer fail; plan
   reconciles). Genuine mismatches still need reset — separate policy, out of scope.

Bandaid to avoid: "tell agents to always author concrete patch versions" — pushes
platform-internal resolution onto the user, brittle on every patch bump, contradicts the
catalog advertising family forms, and the next agent using `go@1`/`@latest` reproduces it.

Anchors: `internal/topology/type_equivalence.go:149`,
`internal/tools/workflow_checks.go:241` (`checkServiceType`),
`internal/workflow/validate.go:310` (stores plan type verbatim), provision no-amend
in the bootstrap engine.

---

## F2 — Recipe-route plan is the agent's lossy re-statement  **[HIGH]**

### What happened
c1 (route=recipe, laravel-showcase) submitted a discover plan with appdev/appstage
(standard) + workerstage (simple). Rejected: `"no recipe service matches plan
target type \"php-nginx@8.4\" (role=dev)"` — unactionable; the agent guessed it
should drop workerstage. It did → provision passed → but **workerstage was created
by the recipe import yet never entered the plan**, so `writeProvisionMetas` (which
iterates only `Plan.Targets`) wrote no ServiceMeta for it. Mid-develop, the
cross-deploy to workerstage failed `SERVICE_NOT_FOUND "not adopted by ZCP — deploy
blocked"`, forcing a separate mid-task bootstrap-adopt cycle.

### Root cause (substance)
**Wrong owner of the recipe-route plan.** The recipe import YAML
(`RecipeMatch.ImportYAML`) already declares every service — it is the source of
truth — yet the architecture makes the agent re-author a parallel `ServicePlan`
that must match the recipe service-for-service, and **derives ZCP metadata from
that hand-authored plan, not from the imported services**. Two artifacts describe
one topology and the metadata-bearing one is the lossy restatement. This is the
exact wrong-owner shape `0384ce5d` fixed for route=adopt — never applied to recipe.
(The backlog `plan-schema-author-friction.md` tracks only the authoring *friction*,
not the more dangerous meta-divergence→deploy-block cascade.)

Mechanism: `recipe_override.go:41` `RewriteRecipeImportYAML` maps plan slots to
recipe services by (type, role); `recipeRuntimeRole` maps worker→role=stage, so a
3rd same-type runtime has no clean slot (`buildRuntimeSlots` only emits dev/stage).
`InferRecipeShape` only classifies 0/1/2 setups → a 3-runtime worker recipe early-
returns mode="" and the mode guard never fires. 3-runtime/worker recipe shape is
**untested** in the bootstrap route.

### Fundamental fix
**Derive the recipe-route plan from `RecipeMatch.ImportYAML`** when plan is
empty/omitted (mirror `0384ce5d`): every `zeropsSetup` runtime → a target (dev+prod
collapse to a standard pair; each worker/extra runtime → its own correctly-moded
target), every managed service → a CREATE dep (EXISTS on live collision). Agent
authors nothing; no recipe service can be dropped because the plan *is* the recipe;
metas derive from a complete plan, closing the SERVICE_NOT_FOUND cascade
structurally.

Adversarial caveat: under-specified as written — there is no `BootstrapMode` that
emits a stage-only/worker slot, and `ValidatePlanHostname` hard-requires a non-empty
DevHostname; needs `deriveRecipePlan(importYAML, liveServices)` that round-trips
through `RewriteRecipeImportYAML` for ALL runtimes incl. workers. **Touches
Aleš-scoped recipe code → flag + coordinate before implementing.**

### Live end-to-end reproduction (flow-eval `recipe-laravel-showcase-fullstack`, 2026-06-02)
A fresh post-release agent run reproduced F2 exactly and surfaced extra detail:
- The agent authored `type: php-nginx@8.4` (from the recipe YAML / the bare-type plan
  example) → rejected `no recipe service matches plan target type ... (role=dev)`. It
  tried `isExisting` both ways, same error. **Neither the error nor the guidance names
  the format the validator wants.**
- **Empty-plan on recipe route is a SILENT DEAD-END** (new, worse than documented):
  submitting no plan "silently succeeded but left the provision step without a plan,
  making it impossible to complete — required a full session reset." (Adopt's empty-plan
  auto-derives; recipe's empty-plan bricks the session.)
- **The agent's escape was to ABANDON recipe → switch to the adopt route**, which
  auto-derived with "no friction at all." Its verbatim lesson: *"The contrast in clarity
  between the recipe route and adopt route guidance was stark"* — i.e. it wished recipe
  behaved like adopt's auto-derive. That IS the fix.
- Worker-shape confusion confirmed: "no example of how to represent a standalone worker
  in the plan… I guessed `simple`."
- F2 was so blocking the agent **never reached deploy** → F4/F5 not exercised this run
  (separate scenarios needed to reach cross-deploy).

---

## F3 — Adopt auto-derive over-adopts  **[HIGH]**

### What happened
c1, mid-develop, needed to adopt its own `workerstage`. It started a bootstrap and
the adopt derive swept up **workerstage AND `mailpit`** — mailpit belongs to the
*concurrent c2 session*. Live re-confirmed: the adopt route-menu on zcp-eval offered
`['zcp','mailpit','app','appdev','workerstage','appstage']` — 6 services across three
unrelated apps **plus the control plane itself**.

### Root cause (substance)
`InferServicePairing` (`adopt.go:44`) / `BootstrapCompleteAdoptPlan` turn **every**
adoptable runtime in the whole project into an `isExisting` target, with **no
scoping** to intent, to the service that triggered `ADOPT_REQUIRED`, or to the
develop scope. The derive-everything default is correct for the single-app project
(`0384ce5d`'s target case) but has no narrowing path for multi-service / multi-agent
projects. (`ErrAdoptPairingChoice` only guards the exactly-2-same-type case; mixed
types commit frictionlessly — including unrelated ones.)

### Fundamental fix
Adopt-derive needs a **scope**. The trigger carries one: an `ADOPT_REQUIRED` /
SERVICE_NOT_FOUND on service X should adopt **X + its managed deps only**; an
explicit `adoptServices:[...]` from the agent should narrow; the unscoped
"adopt everything" stays available but as an explicit opt-in, not the default a
deploy-block forces. Filter the control-plane (`isControlPlaneType`) out of the
*route-menu* candidate list too (it's filtered at derive but advertised in the menu).

Bandaid to avoid: telling agents "pass an explicit plan" — that re-introduces the
authoring friction `0384ce5d` removed. The fix is *scoped* derive, not *no* derive.

---

## F4 — Parallel cross-deploys collide on git lock  **[HIGH]**

### What happened
c1 batch cross-deploy from `appdev` → [appstage(prod), workerstage(worker)]:
workerstage succeeded, appstage failed `"could not lock config file .git/config:
File exists"`. Twice. Serial deploy worked.

### Root cause (substance)
`DeployBatchSSH` (`deploy_batch.go:49`) fans out one goroutine per target with **no
per-source serialization**. Each `DeploySSH` runs `cd /var/www && git init && git
config && git add -A && git commit && zcli push` **inside the SOURCE container**
(`deploy_ssh.go:158` execs into `source.Name`). Two cross-deploys sharing source
`appdev` concurrently mutate the one `/var/www/.git` → git's own
`.git/config.lock` / `index.lock` (O_CREAT|O_EXCL) race. The `deploy_batch`
concurrency doc-comment reasons over the WRONG invariant ("per-hostname subprocess
isolation") — the contended resource is the **source** hostname's shared git tree.

### Fundamental fix
**The git mutation is load-bearing — do NOT remove it.** (Adversarial live check of
`zcli push --help` on the container: cross-deploy uses the DEFAULT
`--workspace-state all`, which is git-based; the commit establishes the workspace the
upload reads. The investigator's "emit `--no-git`" fix was REFUTED — it would switch
the upload contract to working-dir-as-is, shipping untracked junk and breaking the
`.deployignore`/git-tracking discipline.)

The fix is **concurrency isolation of shared git state**, and **(a) mutex-per-source
is correct; (b) GIT_INDEX_FILE is NOT sufficient**: the observed error is
`.git/config: File exists` (config.lock), and `git config` writes `.git/config`
regardless of `GIT_INDEX_FILE` — two concurrent `git config` still race on config.lock
even writing identical values. So:
- **Minimal fix:** a `map[sourceService]*sync.Mutex` in `DeployBatchSSH`, held around
  the `DeploySSH` git-mutation. Distinct sources stay parallel; same-source targets
  serialize. Preserves the batch's reason-to-exist (server-side parallelism dodging
  the MCP-STDIO serialization penalty) while killing the race.
- **Cleaner variant:** group targets by source; run `git init/config/commit` ONCE per
  source (all same-source cross-deploys push the SAME committed tree), then fan out the
  N `zcli push` (read-only on the commit) in parallel.
- **Also fix the lie:** the `deploy_batch.go:62-66` doc-comment claims safety via
  "independent `ssh` subprocess per **hostname**" — it reasons over the TARGET
  hostname; the contended resource is the **source** container's git tree. Rewrite it.

Anchors: `internal/ops/deploy_batch.go:49-116` (no per-source mutex),
`internal/ops/deploy_ssh.go:158` (execs into `source.Name`), `:240` (the git cmd).

---

## F5 — verify HTTP-probes a worker  **[MEDIUM-HIGH; live data overturned the obvious fix]**

### What happened
c1 `workerstage` (php-nginx, `setup=worker`, runs `queue:work`, no HTTP) → verify
returned `status: degraded` with http_root probing it and logs full of
`connect() to unix:/var/run/php-fpm84/php-fpm.sock failed ... fastcgi`. The worker
was healthy (`service_running: pass`); the http_root failure is meaningless noise
that falsely downgrades it.

### Root cause (substance)
`classifyRuntime` (`verify_checks.go:49`) returns `RuntimeImplicit` (HTTP-serving)
for php-nginx/php-apache **by base image type, unconditionally** — the `!hasPorts →
RuntimeWorker` branch is unreachable for php. HTTP-expectation is treated as a
property of the **base image**, but the SAME php-nginx base serves both web and
worker roles depending on the **deployed setup**.

### Live finding that overturns the "obvious" fix
The investigator AND the adversary both proposed: classify by the live port signal
(`Port.Scheme==http`). **Live data refutes this.** On zcp-eval all three report
identical ports:

| service | role | port | httpSupport | scheme |
|---|---|---|---|---|
| workerstage | queue worker, **no HTTP** | 80 | false | **http** |
| appstage | HTTP web | 80 | false | http |
| app (bun) | HTTP web | 8080 | false | http |

The worker's port carries `scheme:http` identically to real web services (port
config is stack-level, independent of the deployed setup). `httpSupport` is `false`
for everyone (the documented-unreliable post-enable flag). **Neither field in the
ports DTO distinguishes a worker from a web service.** A port-driven fix would still
misclassify.

### Fundamental fix
The only authoritative "does this service serve HTTP" signal is the **deployed
setup** (`worker` setup = no documentRoot / no run.ports → no HTTP; `prod`/`dev` =
HTTP) — a zerops.yaml fact ZCP **knows at deploy time** (`deploy_batch` passed
`setup="worker"`) and **discards before verify**. Record the deployed setup's
HTTP-ness in ServiceMeta and have verify consult it; the base-image type is at best a
fallback. Promote a `RuntimeWorker` concept into `topology` (it has none — workers
are an ops-local invention) so verify/subdomain-auto-enable/deploy-poll agree, and
check the sibling bug the blast-radius flagged: does subdomain auto-enable wrongly
fire on a worker too?

Adversarial caveat: keep `TestVerify_ImplicitWebserver_SkipsStartup` green (a real
php web service must still be probed) — the setup-driven classifier satisfies it
where the scheme-driven one would not.

Anchors: `internal/ops/verify_checks.go:49-63`, `verify.go:134-192`,
`internal/platform/types.go:86-100` (Port.Scheme/HTTPSupport semantics),
`internal/topology/runtime_class.go` (parallel type-only classifier, no worker).

---

## F6 — Auto-close premature + misleading guidance  **[MEDIUM]**

### What happened
In BOTH sessions, once every scoped service had one DEPLOYED + one healthy verify,
`workSessionState` flipped to `auto-closed` with note *"Develop session auto-closed
… Start a new session for this work."* But the agents were NOT done — functional
smoke tests (real HTTP POST, mailpit inbox, queue log) then surfaced REAL bugs
(Carbon string→addDays 500; worker absolute-URL bug). Codex deployed into the
auto-closed session ~4 more times; every deploy still returned `DEPLOYED` (auto-close
does NOT block deploys) **and** re-emitted "start a new session". The agent correctly
ignored it.

### Root cause (substance)
Auto-close is derived purely from "every scoped service deployed + verified +
closeMode=auto" (`EvaluateAutoClose`/`DeriveCloseState`). That is **necessary but not
sufficient** for task completion — functional correctness (smoke tests) lives beyond
first-green-verify, which is exactly where the real bugs were. The note
(`sessionAnnotations`, `deploy_local.go:261`, shared by deploy_ssh + verify) issues
an **imperative** — "Start a new session for this work" — that is **false**:
continuing in the closed session works, and starting a new session would be the
friction. The lifecycle SIGNAL ("scope reached green") is conflated with a STOP
COMMAND.

### Fundamental fix
Separate "scope is green" (a true, useful signal) from "you must stop." Either: the
note states the truth — "scope green; further deploys are fine and stay tracked" (no
"start a new session"); or a post-close deploy **re-opens** the session transparently
(a new edit is new work). Do not instruct a restart the platform doesn't require.
Keep the derived-never-stamped invariant (compaction determinism) — this is a
guidance/semantics fix, not a trigger-stamp change.

Anchors: `internal/tools/deploy_local.go:248-270` (`sessionAnnotations`),
`internal/workflow/work_session.go` (`EvaluateAutoClose`/`DeriveCloseState`).

---

## F7 — knowledge URIs don't dereference  **[MEDIUM]**

### What happened
Both sessions: `zerops_knowledge` results carry `{uri:"zerops://recipes/mailpit",…}`.
Agents naturally tried `ReadMcpResourceTool uri="zerops://recipes/mailpit"` →
`-32002 Resource not found` (same for `zerops://guides/zerops-yaml-advanced`). They
fell back to re-running keyword searches, never getting the full doc.

### Root cause (substance)
Knowledge search emits dereferenceable-LOOKING URIs not backed by a readable
resource. A half-built MCP resource template is registered under a DIFFERENT,
mis-scheme'd path (`zerops://docs/{+path}`), `ListMcpResources` is empty, no
consumer references it. The search-result URI and any fetch handle are not the
same string.

### Fundamental fix
One owner of "the URI a search result carries == the handle you read it back with."
The tool-side fetch already exists: `zerops_knowledge` Mode 5 `uri=` calls
`store.Get(input.URI)` on the bare URI search emits. So: (1) re-frame Mode 5 `uri=`
as the canonical "fetch full doc by the exact uri a search result gave you" (drop
the sub-agent-only framing); (2) make Mode 1 query responses self-documenting
(carry a `fetchFullDoc` hint with the same URI) so agents stop reaching for
`ReadMcpResourceTool`; (3) delete the dead `zerops://docs/` resource template
("remove, don't disable" — no live consumer) OR re-scheme it to the bare form so
the resource handle is byte-identical to the search URI. Invariant to pin: every
search-result URI is dereferenceable by the same string through the single fetch
path.

Anchors: `internal/tools/knowledge.go:34,169-180`, `internal/server/resources.go`
(+ `resources_test.go`).

---

## F8 — Client schema advertises version tokens the platform rejects; atomic non-attributing import  **[HIGH, live-found]**

Found during the live F1 expansion (not in the transcripts). Two distinct defects:

### F8a — schema lists non-ACTIVE versions
Live on zcp-eval: a classic plan with `deno@1` **passes discover** (`HasServiceType`
validates it — `deno@1` is in the curated schema enum, catalog shows
`deno@{1,1.45,1.45.5,2,2.0,2.0.0,latest}`) but **`zerops_import` hard-fails**:
`API_ERROR serviceStackTypeVersionIsNotActive "Unable to use Service stack Type
version which is not in ACTIVE state."`. So the client-side schema (the documented
"single client-side source of truth") advertises a version the platform won't accept.
The `zcp schema check` drift sentinel doesn't catch it — `deno@1` is structurally
present in the enum. Disambiguated: `nodejs@latest`, `go@latest`, `bun@1` import fine
individually; **only `deno@1`** was the non-ACTIVE culprit in the batch.

Root cause (pinned locally): the `active_versions.json` derivation is a misnomer —
it **also lists `deno@1`** (`internal/knowledge/testdata/active_versions.json`:
`deno@{1,1.45,1.45.5,2,2.0,2.0.0,latest}`). So `make schema-sync` pulls the published
catalog versions but NEVER queries/filters by platform ACTIVE state. `HasServiceType`
(`internal/schema/catalog.go:24`) trusts the import-schema enum, so a published-but-
non-ACTIVE version passes discover and fails import. Fundamental fix: the schema sync /
`active_versions` derivation must query the platform's ACTIVE version set and prune the
enum to it (rename it to mean what it says), so "passed discover" ⟹ "importable"; and
`zcp schema check` should assert every advertised version is ACTIVE-importable.

### F8b — atomic batch import with a non-attributing error
`zerops_import` of a multi-service YAML is all-or-nothing: one bad type (`deno@1`)
failed the **entire** 4-service import with a generic `serviceStackTypeVersionIsNotActive`
and **no indication which service** was the culprit. An agent importing a real
multi-service stack can't localize the failure. Fundamental fix: surface the
offending `services[].hostname`/type in the error (the platform `apiMeta` carries
field context — thread it through), or validate each service-type against the ACTIVE
set pre-import and name the bad one.

### F8 ↔ F1 interaction (measured)
`nodejs@latest` → resolves `ubuntu/nodejs@24`; `bun@1` → `ubuntu/bun@1.3.9`. Both
import fine but their plan-stored token (`nodejs@latest` / `bun@1`) **also fails the
F1 provision check** against the resolved concrete version. So `@latest` — the single
most common alias an agent reaches for — is doubly cursed: importable but
provision-rejected. The ONLY forms that pass provision today are exact concrete
versions the platform doesn't expand (`nodejs@22`, `dotnet@8`, `php-nginx@8.4`).

---

## Cross-cutting themes

- **F1 + F2 + F3 are the same disease**: ZCP makes the agent author/restate a plan,
  then compares/derives against it, when a *live or curated source of truth already
  exists* (platform resolution / recipe YAML / live services). `0384ce5d` solved this
  for adopt by deriving from live state; F1 (reconcile to resolved type), F2 (derive
  plan from recipe), and F3-scope are the same "stop re-authoring; derive from the
  authority" move applied to the other two routes.
- **F4 + F5 are deploy/verify ignoring per-deploy reality**: F4 mutates shared source
  git state without isolating concurrent deploys; F5 classifies by static type
  instead of the deployed setup. Both discard a fact ZCP holds at the moment of
  deploy.
- **F6 + F7 are honest-signal bugs**: a true state ("scope green", "here's a doc")
  expressed as a false/dangling affordance ("start a new session", a non-resolving URI).

## Live campaign — additional behavioral findings (flow-eval, lower severity)

From `classic-rust-postgres-standard` (clean run — F1 not hit because the agent picked a
non-expanding concrete rust version; F1 is **input-dependent** on the authored alias) and
the two earlier runs:

- **F9 — dev-mode deploy → 502 → must `zerops_dev_server action=start` before verify.**
  Dev-mode runtimes idle on `zsc noop --silent`, so the subdomain 502s right after a
  successful deploy. The natural deploy→verify instinct fails and wastes a diagnose cycle.
  Guidance says it but it's "buried in a long response." Ordering rule: dev deploy →
  dev_server start → verify. (Adjacent backlog: `subdomain-auto-enable-dev-mode-zsc-noop.md`.)
- **F10 — close-mode gate is bidirectionally counterintuitive (same root as F6).** Unset →
  "session hangs open forever" (agent must manually `close-mode`); auto → premature "start a
  new session" (F6). Agent verbatim: "deploy + verify passing is necessary but not sufficient
  for auto-close. The close-mode is a separate gate." The close-mode mental model needs a
  rethink, not just a note tweak — F6 and F10 are the two faces of it.
- **F11 — ToolSearch deferred-tool confusion (recurs across c2 + rust run).** `select:mcp__zerops__*`
  returns "no match" once the schema is cached, but the tool is still callable — "looks
  unavailable, works anyway." Harness-level (Claude Code deferred tools), but ZCP can mitigate
  via MCP init-instructions preload-hint (backlog `preload-hint-in-mcp-init-instructions.md`
  — recurrence is its promotion trigger).
- **Minor:** `waitSeconds` on `zerops_dev_server` is an unguided guess (no default documented;
  a cold Rust compile could exceed it); cross-deploy `setup=` mapping is only shown by example,
  unclear for recipes whose setup blocks aren't named `dev`/`prod`
  (backlog `cross-deploy-setup-yaml-block-vs-hostname.md`).

**F4 not reproduced live:** both flow-evals reached at most one cross-deploy target (rust:
appdev→appstage; laravel: stuck at F2 pre-deploy). F4 needs ≥2 parallel cross-deploys from
one source (worker + stage) — code + the c1 transcript (the actual `.git/config.lock` error,
twice) remain the evidence.

**`recover-failed-buildfromgit-missing-dep` (failure path) — no new server-logic bugs; a
positive + 2 known confirmations.** The `READY_TO_DEPLOY → re-import override+startWithoutCode`
recovery worked well: the provision check's `expected one of [RUNNING, ACTIVE], got
READY_TO_DEPLOY` is a CLEAR, actionable signal the agent trusted. **Instructive contrast to
F1:** the SAME `checkProvision` produces an *actionable* status-mismatch message but a
*misleading* type-mismatch message — because F1 is a FALSE mismatch (the check mechanism is
fine; the type-equivalence false-negative is the bug). Confirmed two already-backlogged
items: CLAUDE.md historical/REFLOG block misled the agent about current state (trust
`zerops_discover`/`status`, not the snapshot — `claude-md-history-vs-service-meta-desync.md`,
`reflog-vs-live-discover-staleness.md`); and Python recipe-knowledge leans DB-backed, needs
mental-stripping for a no-DB adopt (`recipe-knowledge-context-bleed-adopt-scenarios.md`).
Minor: adopt close-step auto-skip surprised the agent (looked for a close step that isn't there).

## Campaign coverage + stopping point
Routes: classic (c2 bun ✓, rust ✓ clean), recipe (laravel — F2-blocked pre-deploy), adopt
(recover ✓). States: from-scratch, existing/adopt, failed-buildfromgit recovery, multi-iterate.
New-finding rate dropped to ~0 by the 3rd flow-eval (recover added only known-item
confirmations) → diminishing returns. Untested novel paths if deeper coverage wanted later:
`launch-production-*`, `export-buildfromgit-self-snapshot`, `resume-after-compaction`,
`greenfield-fullstack-multi-runtime` (the last would finally trigger F4's ≥2-cross-deploy shape).

## Backlog cross-reference (honesty: what's already tracked vs new)

| Finding | Status vs backlog | Note |
|---|---|---|
| **F1** | **Extends + refines** `plans/backlog/bootstrap-adopt-plan-type-mismatch.md` | That entry (2026-05-17) is ADOPT-scoped + a *wrong-type* mixed-pair case, and explicitly says "classic/recipe don't have a live runtime at plan time — fix would NOT apply there." My data shows the trap ALSO bites **classic + recipe at the PROVISION step** (live runtime exists post-import) and the root cause is **version-family→resolved-patch**, NOT agent error. Critically, that entry's proposed "fix 1: pre-seal cross-check plan type == live type" would **worsen F1** if naive (rejects `bun@1.3` vs `bun@1.3.9` even earlier). The Codex-adjudicated `matchProvisionRuntime`+self-heal is the corrected fix. |
| **F2** | **KNOWN — my flow-eval is the promotion trigger** | `plans/backlog/recipe-plan-validator-worker-blind-spot.md` (2026-05-19, SAME scenario) explicitly set trigger = "flow-eval catches it again" → MET (2026-06-02 run). Sibling: `bootstrap-cross-type-recipe-pair.md` (cross-type pairs). I add the **metadata-cascade depth** (dropped runtime → no meta → deploy block) and the **empty-plan silent dead-end**, which the backlog entries under-cover. The fundamental fix (derive plan from recipe YAML) subsumes all three. |
| **F3** | **NEW — gap from a recent landing** | Not backlogged. It is the unintended consequence of `plans/adopt-route-auto-derive-plan-2026-06-01.md` (`0384ce5d`): auto-derive added, no scoping. Regression-class. |
| **F4** | **NEW** | No backlog entry for parallel cross-deploy git-lock contention. |
| **F5** | **NEW** (sibling adjacent: `subdomain-auto-enable-dev-mode-zsc-noop.md`) | No entry for verify HTTP-probing a worker. |
| **F6** | **NEW** (adjacent: `work-session-state-enabled-false-framing.md`) | The misleading "start a new session" imperative is not tracked. |
| **F7** | **NEW** | No entry for non-dereferenceable knowledge URIs. |
| **F8** | **NEW** | No entry for schema advertising non-ACTIVE versions / atomic non-attributing import. |

Net: 6 of 8 are new; F1 refines (and partly contradicts the proposed fix of) an existing
entry; F2 is the live evidence that promotes a deferred entry. None should be filed as a
fresh duplicate — promote/extend the existing two, open new entries for F3–F8.

## Recommended fix order (decision matrix)

Ordered by (severity × how often it blocks a real bootstrap × independence). Effort is
relative (S/M/L), not LOC. "Aleš" = recipe-scope, flag + coordinate before implementing.

| # | Fix | Sev | Effort | Touches | Owner / coordination | Notes |
|---|-----|-----|--------|---------|----------------------|-------|
| 1 | **F1** `matchProvisionRuntime` + `ReconcileBootstrapRuntimeTypes` self-heal | 🔴 crit | M | `tools/workflow_checks.go`, new `engine` method | internal; Codex design ready, tests run | Unblocks classic+recipe for 6/8 runtimes + `@latest`; moots F1 no-amend dead-end. Leaves `TypesAreEquivalent` untouched (catalog stays safe). |
| 2 | **F8a** prune schema to platform-ACTIVE versions | 🔴 crit | M | `schema/sync.go`, `active_versions` derivation, `zcp schema check` | internal; needs platform ACTIVE-version query | "passed discover ⟹ importable". `deno@1` is the live witness. |
| 3 | **F8b** attribute the atomic-import failure to a service | 🟠 high | S | `tools/import.go` (thread `apiMeta` field ctx) | internal | Agent can localize which of N services has the bad type. |
| 4 | **F4** mutex-per-source in `DeployBatchSSH` (+ fix the doc-comment lie) | 🟠 high | S-M | `ops/deploy_batch.go` | internal | NOT `--no-git` (git commit is load-bearing). Same-source serialize; distinct-source stay parallel. |
| 5 | **F3** scope adopt-derive to the trigger service (+ filter control-plane from menu) | 🟠 high | M | `workflow/adopt.go`, route-menu, `tools/workflow_bootstrap.go` | internal; regression from `0384ce5d` | Stops sweeping unrelated/other-session services. Keep "adopt all" as explicit opt-in. |
| 6 | **F2** derive recipe-route plan from recipe YAML (agent authors nothing) | 🟠 high | L | `workflow/recipe_override.go`, `recipe_shape.go`, discover dispatch, metas-derive | **Aleš** | Subsumes `recipe-plan-validator-worker-blind-spot` + `bootstrap-cross-type-recipe-pair` backlog items. Needs a worker/stage-only slot representation. |
| 7 | **F5** setup-aware verify classification (record deployed-setup HTTP-ness) | 🟡 med-high | M | `ServiceMeta`, `ops/verify*`, promote `RuntimeWorker` to `topology` | internal | Live-proven: ports DTO (incl. `scheme`) can't distinguish worker from web — must use deployed setup. |
| 8 | **F6+F10** close-mode model + note rewrite | 🟡 med | S (note) + design (model) | `tools/deploy_local.go::sessionAnnotations`, close-mode | internal | Stop the false "start a new session"; clarify close-mode-as-separate-gate (both directions). |
| 9 | **F7** one knowledge fetch path; delete dead `zerops://docs/` resource template | 🟡 med | S-M | `tools/knowledge.go`, `server/resources.go` | internal | Search-result URI must dereference by the same string. |
| 10 | **F11** MCP init preload-hint for deferred zerops tools | ⚪ low | S | MCP init instructions | internal | Promote `preload-hint-in-mcp-init-instructions.md` (recurs). |

Sequencing logic: 1→2→3 are the bootstrap-blocking correctness fixes (do first, all
internal). 4→5 deploy/verify correctness. 6 (F2) is high-value but needs Aleš + is the
biggest — schedule deliberately. 7–10 are UX/correctness polish.

## Test-setup note
The two sessions sharing ONE project is unusual (it surfaced F3's cross-session
adoption and the cross-app noise in develop status). Real single-app projects won't
hit the cross-session part of F3 — but the *no-scoping* root cause is real for any
multi-service project. Recommend: future eval runs use one project per concurrent
agent, OR keep shared-project runs explicitly to test multi-tenant behavior.
