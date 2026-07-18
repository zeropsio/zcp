# ZCP — Architectural Root-Cause Audit (consolidated, 2026-05-29)

**Purpose.** Three independent audits ran in 48h with different lenses and converged on the same
handful of architectural defects. This document stops enumerating the ~60 symptoms and instead
names the **roots** they flow from, and for each root states the **target architecture** — the
clean foundation to build from, not a stack of symptom patches. Goal (Karel): *the best
architectural solution at the start.*

**Sources consolidated:**
- `plans/workflow-family-audit-2026-05-28.md` — drift-hunt, 30 confirmed. Thesis: "foundation lands,
  consumers not rewired, fallback never deleted."
- `plans/zcp-wholecodebase-audit-2026-05-29.md` — whole-repo bug-hunt, 13 confirmed. Thesis: silent
  `_ =` discard of load-bearing status at I/O boundaries (5 of 8 high bugs).
- `plans/zcp-workflow-deep-audit-2026-05-29.md` — logic/overcomplexity/reliability/design lens, 38
  confirmed + 7 partial, default-refute verified (5 refuted). Thesis: one concept answered by 2–4
  divergent code paths; defects cluster at the seams between islands.

**Why consolidate.** Each audit, read alone, reads as a long fix list. Read together, the same four
mechanisms generate almost every finding — and the wholecodebase bugs (which span sync/update/eval,
outside the workflow family) corroborate that the roots are *codebase-wide discipline gaps*, not
workflow-specific. Fixing the roots dissolves whole clusters of symptoms at once; fixing the
symptoms leaves the root free to regenerate the next batch (already observed: the keystone meta-dims
bug got a partial fix that made three branches *mutually* inconsistent where they were uniformly
wrong before).

> No edits proposed for action yet — this is the architectural map to validate (Codex pass next),
> then sequence. Recipe-generation engine internals are Aleš's scope; flagged, not actioned.

---

## 0. The whole picture in one frame

ZCP is, at core, **a state machine that derives agent guidance from persisted evidence.** Every
defect traces to one of four questions being answered without a single owner:

| # | The unowned question | What it should be | Symptom count |
|---|---|---|---|
| **R1** | *How is workflow state persisted and mutated?* | one locked, transactional, normalized store | ~14 |
| **R2** | *Who resolves a shared concept (setup-name, deployed?, idle-scenario, env-layers, http-ready…)?* | one pure resolver per concept, lint-enforced | ~13 |
| **R3** | *What happens to a load-bearing error / poll-timeout flag?* | non-ignorable; failure never reads as success | ~10 |
| **R4** | *From which signal is a domain truth derived?* | the authoritative signal, one predicate, every door | ~8 |

Two further roots are *coherence* and *capability*, not discipline:

| # | | |
|---|---|---|
| **R5** | Workflows bolted **beside** the spine (launch/export own state; local bootstrap unwired) | ~6 |
| **R6** | Genuine capability gaps (bundle env layers, local runtime cardinality, search query shape, git-init invariant) — feature work, not refactor | ~7 |
| **R7** | Contract drift (Codex 7th root): spec/comments/tests/runtime aren't one self-consistent contract — the spec *itself* contradicts (§5.3 vs §6.2). **Upstream of R1–R4** — fix first. | ~5 |

R1–R4 are facets of one meta-root: **state and the truths derived from it have no single-owner
discipline, and nothing in the build enforces single-ownership, so every new foundation
re-introduces divergence.** That meta-root is the thing to fix architecturally.

> **Codex review pass (2026-05-29) — folded in below.** A full adversarial review against the
> code sharpened the architecture in five ways and corrected three claims; all verified by hand:
> 1. **R1 is broader than "transactional ServiceMeta access" — it is a typed state + lifecycle
>    *contract*.** Lifecycle status **cannot be a scalar `Phase`** (work + infra sessions are
>    explicitly concurrent); a scalar `ResolvePhase` bakes the ambiguity back in → use a composite
>    `ResolveLifecycle`.
> 2. **A package mutex is not enough — cross-process is real.** `engine.go:19` admits multiple ZCP
>    subprocesses serialized by the registry flock (`registry.go:120/141`); ServiceMeta has the same
>    cross-process lost-update hole and **no flock** → per-record flock + in-process mutex-map.
>    (That same comment wrongly calls the within-process race benign — which is *why* no lock was
>    ever added. The comment is load-bearing and wrong; XCUT-1 refutes it.)
> 3. **R2/R3 want structure, not just lints** — decode raw state into resolved *value objects* and
>    return *typed mutation outcomes* that must be surfaced; lints then enforce the boundary.
> 4. **New 7th root — contract drift (R7).** Specs, comments, tests, and runtime do not enforce the
>    same semantic contract. The proof: spec §5.3/§9.3 say bootstrap-primary while **§6.2 says
>    work-first** — a self-contradiction that is the actual cause of SPINE-1. *Fix the spec first.*
> 5. **One correction to R4** — the HTTP-readiness item is NOT "one predicate everywhere": `<500`
>    (reachability) and `<400` (serving-real) are two legitimate truths that must be **named and kept
>    separate** (the bug is `statusDevServer` using the reachability threshold for a readiness claim,
>    plus a stale comment). And "export variant inert" is imprecise — the sharper bug is stage-half
>    source-mode resolution (EXPORT-1).
> Full Codex report: `/tmp/codex-out-1780031584-20717-4756.md` (tail).

---

## R1 — No single owner for persisted state (the keystone)

**Root.** Workflow state lives in four uncoordinated stores — `ServiceMeta`
(`.zcp/state/services/{host}.json`), the per-PID work session (`work-{pid}.json`), the infra
session (registry + per-session file), and launch-state (`launch-production/`) — each
read-whole→mutate→write-whole, with **no transaction, no shared constructor, no read-time
normalization, and no single phase/precedence resolver.** ServiceMeta is the central evidence
object yet is the least protected.

**Symptoms that collapse into R1:**

| Finding | What it is |
|---|---|
| **XCUT-1** (deep) | `WriteServiceMeta` is atomic-rename only (torn-write safe, **RMW-unsafe**); the SDK dispatches tool_use concurrently (`go-sdk server.go:1441 jsonrpc2.Async`); close-mode + git-push-setup + build-integration as parallel tool_use → last writer **silently discards** the others' orthogonal, action-confirmed mutations. *Data-corruption hole.* (Hand-verified.) |
| **TOPO-1 / WF-2 / DELIV-2** (deep) · F2/F3/T2 (wf-fam) | No `NewServiceMeta` constructor → local-adopt writers omit `GitPushState`/`BuildIntegration`; no `parseMeta`-on-read normalization → empty leaks to the matcher; only `CloseDeployMode` is normalized at snapshot → the git-push→build-integration **atom chain never fires for local-stage**, the mode it targets. (Keystone; still open, partial fix made it *worse*.) |
| **SPINE-1** (deep) | "What workflow is active" answered twice with **opposite** precedence — dispatcher infra-first (`detectActiveWorkflow`), envelope work-first (`derivePhase`); `action=status` (the post-compaction recovery primitive) hides in-flight develop work in a concurrent session. |
| **SPINE-2** (deep) | `reset` clears only the infra session, leaving the work session dangling — "clean slate" isn't. |
| **SPINE-3** (deep) | `claimSession` writes the state-file PID then `_ =`-discards the registry PID update → the two stores disagree on ownership (a self-inflicted SPINE-1 divergence under I/O failure). |
| **SPINE-4 / TOPO-2** (deep) · #1 (whole) | `checkHostnameLocks` resolves via non-pair-aware `ReadServiceMeta` → stage-half hostname returns nil → concurrent bootstraps proceed against the same stage hostname. |
| **TOPO-3** (deep) | The pair-keying pin (`TestNoInlineManagedRuntimeIndex`) matches only one AST shape — can't catch read-by-stage-hostname, which is exactly how TOPO-2 slipped. |
| **ADOPT-2, GAP3-1** (deep) | `FirstDeployedAt` is set/copied but **never cleared** (override leaves a stamp pointing at a destroyed deploy); local eager-stamps it frozen while container re-derives live → two durability models. (Also R4.) |
| pattern 3 (whole) | Mutex held across blocking I/O (`zerops.go getClientID`, `work_session` Record*/Touch) — the *one* store that has a lock holds it wrong. |

**Target architecture (Codex-sharpened).** A single `internal/workflow/state` package that owns
decoding, normalization, migration, atomic writes, and locks — a typed **state + lifecycle
contract**, not just a ServiceMeta mutex. Keep the flat-file layout (`services/*.json`,
`work/*.json`, registry, `launch-production/*.json`) for backward-compat; do **not** collapse to one
file (raises write contention + migration blast radius) and do **not** event-source (too expensive
for this surface).
- **One transactional mutator** — `Update[T](stateDir, key, mutate func(*T) error)` that
  locks → reads fresh → applies the closure → writes atomically → unlocks. Lock = **per-record
  flock + an in-process mutex-map**: the mutex-map serializes the common same-process goroutine
  race cheaply, the flock covers the real cross-process case (multiple Claude instances sharing a
  `.zcp/state`, matching the registry's existing flock). ServiceMeta updates lock the **canonical**
  meta file — resolving a stage hostname to its primary meta *before* mutating (ties into pair-keying,
  closes TOPO-2). Global ordered lock only for multi-record transactions. **Every** RMW site routes
  through it; direct `WriteServiceMeta`-after-read becomes a lint error. Kills XCUT-1 + the
  ADOPT-2/GAP3-1 lost-update class. *(Also delete the wrong `engine.go:19` "worst case is a stale
  read that reset clears" comment that justified the missing lock.)*
- **Typed constructors, not one bucket** — `New{BootstrapPrimary,StandardPair,LocalOnly,LocalStage,
  AdoptedRuntime}Meta(...)` + shared `Normalize`/`Validate`; a single `NewServiceMeta(hostname,mode)`
  would degenerate into an argument bucket. Each stamps its invariant sentinels; no caller
  hand-builds the struct. Kills the divergent-fresh-meta class.
- **Read-normalize + write-canonical + idempotent boot migration** — on read, empty→semantic default
  in memory (close-mode→`unset`, git-push→`unconfigured`, build-integration→`none`); on write, emit
  canonical; a boot migration rewrites only **lossless** defaults. Kills TOPO-1/WF-2/DELIV-2 for
  every writer at once. **Do NOT default empty setup-names** — empty = "unknown, resolve or block"
  (feeds R2). (Backward-compat seam: pin with a legacy-empty-value fixture.)
- **Composite lifecycle resolver, NOT a scalar phase** — `ResolveLifecycle(stateDir, pid)` returns
  `{primary infra, work-session state, launch state, presentation priority}`, consumed by *both* the
  dispatcher and the envelope. A scalar `Phase` cannot represent the explicitly-concurrent
  work+infra case (§5.3) and would re-bake the SPINE-1 ambiguity. **Blocked on R7**: the spec
  contradicts itself (§5.3/§9.3 bootstrap-primary vs §6.2 work-first) — resolve the contract first,
  then both consumers read one resolver. Kills SPINE-1/2/3.
- **One lifecycle owner** — reset/close/claim operate on the *set* of a PID's sessions, not one
  store. The four stores become records behind one registry with one delete path.

**Why this is the foundation.** R1 is where the only true data-corruption bug lives, where the
keystone atom-misfire lives, and where the recovery primitive (`status`) is unreliable. It is a
*prerequisite* for R4 (the deployed-truth predicate needs the normalize-on-read seam), removes the
surface R2's fragmentation exploits, and provides the shared store + lifecycle rendering R5 needs.
On-disk format unchanged (only access discipline + lossless normalization); no public-surface
change. **Highest leverage — but its lifecycle half is gated on the R7 spec reconciliation.**

---

## R2 — "One concept, one resolver" is a convention, not an enforced invariant

**Root.** Each shared concept has a declared-canonical resolver wired at 1–2 callers while 2–4
parallel implementations (or hardcoded constants) live beside it, because foundations ship without
rewiring all consumers **and nothing in the build forbids re-derivation.** This is the dominant
disease across all three audits. The display layer (`render.go`) normalizes for show, masking the
matcher/validate disagreement — which is why these survive.

**Symptoms that collapse into R2:**

| Concept | Canonical thing | Divergent / wrong consumers | Finding |
|---|---|---|---|
| setup-name | `ResolveCanonicalSetup` / `meta.SetupNameFor` | `DeployIntent.Resolve` hardcodes `prod`/`dev` (ignores the fields it was built for) → bakes `--setup prod` into committed CI; 3 launch fallback sites | **WF-1** (deep), F1/F4/T1 (wf-fam), **LAUNCH-5** |
| idle-scenario | `deriveIdleScenario` (3-way: resume/bootstrap/adopt) | `build_plan.go countIdleServices` (2-way, no resume) → Plan says "adopt" while priority-1 atom says "resume" | **DELIV-1** (deep) |
| deploy-dimension normalization | (snapshot, for `CloseDeployMode` only) | 1-of-3 normalized; render.go normalizes 3 for display | **DELIV-2** (deep) (also R1) |
| env-layer assembly | `EffectiveServiceEnv` (typed RC2/RC3 availability) | `discover.go attachEnvs` re-implements inline → transient masquerades as live on the read path | **ENV-1** (deep) |
| candidate-overlay env-ref | preflight E3 overlay | `expandRefs` (generate-dotenv) resolves against stale platform app-version | **ENV-3** (deep) |
| env-ref preflight | `preflightEnvRefs` | container git-push runs it, local git-push doesn't | **DEPLOY-3** (deep) |
| env precedence | (documented: yaml-baked highest) | `env_generate.go findEnvValue` first-match + service-first order → service var wins | **#4** (whole) |
| HTTP-readiness | (none) | `<400` vs `<500` across `statusDevServer` / `checkHTTPRoot` / probe — no shared predicate | **#6** + pattern 2 (whole) |
| adoptability | `AdoptionState` enum | 4 classifiers (isSelf-by-ID / type-string / prefix), 1 consumer | F11/T7 (wf-fam) |
| git-auth ls-remote probe | `ops.BuildGitAuthProbeCommand` | launch re-implements inline; export uses neither | F9/T5 (wf-fam) |
| bundle composition | single `Compose` (never built) | export `variant` is inert end-to-end — extra round-trip + gate that changes nothing | **WF-1 export** (deep), F10/F26 (wf-fam) |
| subdomain URL | `ResolveSubdomainURL` (scheme-aware) | dead on cold enable (pre-enable snapshot) → wrong port reported/probed | **VERIFY-1** (deep) (also R4) |

**Target architecture.** Make single-ownership *structurally enforced*, not aspirational:
- **One exported pure resolver per concept**, in its owning package, returning the canonical value
  (and `empty → structured blocker` where empty is meaningful, never a silent default like `prod`).
- **A lint family that fails the build on bypass** — generalize the two patterns that already work
  (`TestNoDirectClientCallsInToolsEvalCmd` AST-forbids `client.ListServices` outside ops;
  `TestNoInlineManagedRuntimeIndex` AST-forbids inline pair-indexing) into a reusable
  "this concept resolves only here" guard for setup-name, idle-partition, env-assembly,
  http-readiness, adoptability, deploy-dim normalization. **The lint is the architecture** — without
  it R2 regenerates on the next foundation.
- **Delete the parallels** (CLAUDE.md Clean Code) — including the inert export `variant` dimension
  and `legacyDefaultSetupName`. Defensive retention against an explicit delete is itself the bug.

**Why.** R2 is the highest *count* of findings and the most insidious (display masks it). The fix is
cheap per-concept once the lint family exists, and it is the only thing that stops the disease
recurring. Several R2 items (DELIV-2, VERIFY-1) also need R1/R4, so R2 lands incrementally behind
those seams.

---

## R3 — Load-bearing status is discardable at I/O boundaries

**Root.** `_ =` on a meaningful error or a poll-timeout flag turns failure into silent success or
partial persisted state. The wholecodebase audit named this the dominant bug class (5 of 8 high
bugs); the deep audit located fresh instances on the workflow seams. The tell is always a sibling
call in the same function that *does* check the same return.

**Symptoms:**

| Finding | What it is |
|---|---|
| **XCUT-2** (deep) · #2 (whole) | git-push-setup discards `pollManageProcess`'s timeout flag → stamps `GitPushState=configured` + claims "GIT_TOKEN live in shell" on timeout → next git-push deploy fails confusingly. *Timeout-as-success.* |
| **XCUT-3** (deep) | env set/delete + auto-restart discard the same flag → "env values are live" on a timed-out restart → agent verifies against env that isn't applied. |
| **DEV-1** (deep) | all in-tree deploy/verify paths `_ =`-discard `RecordDeployAttempt`/`RecordVerifyAttempt`; only `record-deploy` checks it → mid-task deploys silently untracked, save-failures invisible. |
| **SPINE-3** (deep) | `claimSession` discards the registry-PID error (also R1). |
| #3 (whole) | `sync push` swallows `zerops.yaml` commit failure → incomplete PR ships as `Created`. |
| #5 (whole) | recipe phantom-tree `WalkDir` error discarded twice → stale content publishes on an unreadable path. |
| #8 (whole) | binary auto-update `copyFile` cross-FS fallback never chmods → non-executable binary shipped. |
| eval-analyze unmarshal, parseSemver Atoi (whole) | malformed input silently zero-valued. |

**Target architecture.**
- **Make the poll-timeout flag non-ignorable** — `pollManageProcess` (and siblings) return a typed
  result whose failure can't be silently dropped, or a single `awaitProcess(...) error` helper that
  converts timeout→`ErrAPITimeout` so call sites *must* handle it. Every "stamp configured / report
  live" decision sits behind that error.
- **Propagate Record/Save errors** — bring the in-tree deploy/verify paths to parity with
  `record-deploy` (ignore only `ErrHostnameOutOfScope`, surface the rest).
- **One sweep + a lint** — `_ =` on the enumerated load-bearing returns (`*Process` poll flags,
  `Record*`, `UpdateFile`, `WalkDir`, `Unmarshal` of wire input) is build-forbidden unless
  accompanied by a `// best-effort: <why>` nolint. Closes the class permanently.

**Why.** Cheap, mechanical, and it removes the "false success" failure mode — the most dangerous
reliability class (an agent told a thing worked when it didn't). Pairs naturally with R1 (same
discipline, applied to writes vs polls).

---

## R4 — Domain truths derived from the wrong (convenient) signal

**Root.** A domain truth is computed from a proxy that's easy to read instead of the authoritative
signal — *even though the codebase usually knows the right signal elsewhere.* The proxy is correct
in the common case and wrong at the boundary.

**Symptoms:**

| Truth | Wrong signal | Right signal | Finding |
|---|---|---|---|
| "service is deployed" | container `Status==ACTIVE` | latest non-NONE appVersion **succeeded** (`failed_context.go` filter already exists) | **ADOPT-1** (deep, partial), ADOPT-2, GAP3-1 |
| stage-half mode | `meta.Mode` | `meta.ModeFor(hostname)` | **EXPORT-1** (deep) |
| pipeline runtime identity | source dev/stage hostname | prod hostname (imported `ServiceStacks[].Name`) | **LAUNCH-1** (deep) |
| subdomain URL / readiness | pre-enable snapshot | post-enable re-fetch, HTTP-scheme port | **VERIFY-1** (deep) (also R2) |
| HTTP status semantics | `statusDevServer` uses `<500` (reachability) to claim *ready* | two **named** truths: `httpReachable` (`<500`, `http_ready.go:38`) vs `httpServingReal` (`<400`, `verify_checks.go:149`) | **#6** (whole, **corrected** — see note) |
| auto-close gate state on `status` | not reconciled (read-only) | `MaybeFireAutoClose` before envelope | **DEV-2** (deep, partial) |

> **Codex correction:** `<500` and `<400` are NOT a single drifted predicate — they are two
> legitimate concepts. `WaitHTTPReady`/L7-reachability is *correctly* `<500` ("is the container
> answering at all"); root-verification is *correctly* `<400` ("is it serving a real response, not a
> 4xx/5xx"). The fix is **named predicates per truth**, not collapse-to-one. The real bug is narrow:
> `statusDevServer` reports `Running=true` on `<500` (so a 4xx-rejecting dev server reads healthy),
> and `http_ready.go`'s comment falsely claims `checkHTTPRoot` is `<500` when it is `<400` — a
> stale-comment drift (R7).

**Target architecture.** One predicate per domain truth, computed from the authoritative signal,
used at *every* door — but where two truths genuinely exist, two *named* predicates:
- `IsReallyDeployed(meta, appVersionStatus)` gated on a successful non-NONE appVersion — called by
  adopt, override-reconcile, and `DeriveDeployed`; the durable stamp is set/cleared by it, never
  frozen. (Compat: keep reading legacy ACTIVE-stamped metas; only tighten *future* writes.)
- `httpReachable(<500)` vs `httpServingReal(<400)` — two named predicates; each caller routes through
  the one matching its truth (`statusDevServer` → `httpServingReal`); fix the stale comment.
- prod-hostname translation at the import boundary (one place maps source→prod identity).
- `ModeFor`/scheme-aware URL selection used wherever the pair/port matters.

**Why.** These are quiet correctness bugs that surface as "looks fine, fails at the seam" — the kind
that erode agent trust. They ride on R1's read-normalize seam (the deployed predicate needs it) and
fold into R2's resolver discipline (each predicate is a single-owner resolver).

---

## R5 — Workflows bolted beside the spine, not on it

**Root.** The envelope→plan→atoms spine is a real single source of state for
bootstrap/develop/deploy/verify, but launch and export run **parallel** state/recovery/audit models,
and local-mode bootstrap's *creation* path was never wired to the local-stage topology the rest of
the system models.

**Symptoms:** WF-5 (launch parallel state + export `nextSteps[]`), LAUNCH-2 (existing-project path
diverges from the new-project poll+pipeline tail → reports success on failure), LAUNCH-4 (the whole
multi-runtime path has zero tests — the enabler that lets LAUNCH-1/3 survive), BOOT-1/BOOT-2
(local+standard classic bootstrap broken end-to-end; writes a phantom-dev meta shape diverging from
`LocalAutoAdopt`), "launch source.RepoURL dropped" (whole).

**Target architecture (owner decision).** Either (A) fold launch into `WorkflowState.Launch` +
StateEnvelope + a launch Plan branch (versioned one-way migration), or (B) formalize export+launch
as stateless-narrowing workflows and extract **one** shared stateless-narrowing dispatch/recovery
helper both consume. Either way delete the second mechanism. Wire local-mode bootstrap to produce
the `LocalAutoAdopt` local-stage shape (gate `skipDevCheck` on env, not route). Share the launch
new/existing mutation tail. **This depends on R1** (the spine is the state owner).

---

## R6 — Genuine capability gaps (feature work, not refactor)

These are not discipline failures — the model is structurally short of platform reality and needs
feature work. Decide the model *before* coding.

- **GAP0** — export/launch bundle carries 2 env layers; platform has 3 → service-level env
  (`zerops_env set serviceHostname=X`) **silently lost** on export→launch; classify-prompt can't
  even surface it. *Add a per-runtime service-env layer through the 4-bucket classification.*
- **GAP2** — local-stage meta can't represent a second runtime; the only recovery path
  (`adopt-local`) hard-refuses → gate↔handler contradiction loop. *Decide: local is single-stage
  (fix the recovery hints) or N-runtime (relink/add sub-mode + N-target meta).*
- **PLAT-1** (partial) — search applies the server `LIMIT` account-wide then post-filters by project
  in Go → per-project window can be 0 → build-poll false-timeout under concurrent multi-service
  activity. *Add a `projectId` predicate to the EsFilter — verify the endpoint accepts it first.*
- **PLAT-2** (partial) — env upsert is non-atomic delete-then-create → a failed re-create destroys a
  secret. *The SDK exposes atomic `PutUserData`/`PutProjectEnv` — wire the PUT (verify capability).*
- **GAP4-1** — bootstrap git-init is fire-and-forget (swallowed); git-push-setup crashes on a
  missing `.git` while the deploy path heals it. *Make git-init a recorded precondition + one shared
  "ensure /var/www/.git" primitive.*
- **BOOT-3** — no cross-target hostname uniqueness in plan validation. *One global hostname set.*

---

## R7 — Contract drift: spec, comments, tests, and runtime don't enforce one semantic contract *(Codex 7th root)*

**Root.** The audits assume the spec is the authoritative contract the code drifted from — but in
several places **the contract itself is not single-sourced or is internally contradictory**, so the
code *could not* be "correct against spec." This is upstream of R2 (you can't have one resolver when
the spec says two things) and is the real cause of the most-confusing findings.

**Symptoms:**
- **The keystone:** spec §5.3/§9.3 make bootstrap PRIMARY in a concurrent work+bootstrap session;
  spec **§6.2 routing precedence says work-FIRST** (verified: §6.2 step 1 "If work session exists →
  return Work Session status"). The dispatcher followed §6.2's spirit one way, the envelope the
  other — SPINE-1 is a *spec* contradiction surfacing as a code split, not a code bug. My SPINE-1
  finding and its own verifier each cited a different contradictory section.
- **Load-bearing wrong comments that suppress fixes:** `engine.go:19` asserts the concurrent-write
  "worst outcome is a superseded state file that reset clears" — false (XCUT-1), and it is *why* no
  lock exists. `http_ready.go` claims `checkHTTPRoot` is `<500` (it's `<400`). `ops/deploy_batch.go:66`
  claims batch records attempts "in the workflow layer" (no such call). `setup_resolver.go:84` claims
  5 write-back callers (2). Comments are read as contract and are wrong.
- **Tests that pin the wrong shape:** `TestNoInlineManagedRuntimeIndex` pins one AST shape and misses
  the live violation (TOPO-3); the S13/local-stage fixtures hardcode the exact sentinel the writers
  omit, giving green-on-a-broken-writer (DELIV-2/WF-2).

**Target architecture.** Make the contract single-sourced and self-consistent *before* writing the
resolver that implements it:
- Reconcile the spec's self-contradictions (§5.3/§9.3 vs §6.2 lifecycle precedence) — **decide
  bootstrap-primary-with-background-work**, strike the contradicting §6.2 routing, and only then
  build `ResolveLifecycle`.
- Treat a comment that states a contract as test-pinnable: where a comment asserts behavior
  (`engine.go:19`, batch, http-ready), either make it true or delete it — a wrong comment that
  suppresses a fix is worse than none (CLAUDE.md "comment-vs-code mismatch is a load-bearing signal").
- Broaden the invariant pins to catch the *violation form*, not one syntactic shape (TOPO-3), and
  build atom-firing goldens from *real* writers, not hand-set fixtures (DELIV-2/WF-2).

**Why it's a root, not housekeeping.** R2's "one resolver" is unachievable while the spec says two
things; R1's `ResolveLifecycle` can't be written until §6.2 is reconciled. R7 is the *first* step of
the build order, not the last.

---

## 7. Root dependency graph + foundational build order

```
R7 (contract reconciliation) ──> R1 lifecycle half (ResolveLifecycle needs the §6.2 decision)
R1 (state+lifecycle store) ──┬──> R4 (deployed-truth predicate needs the normalize-on-read seam)
                             ├──> R5 (shared store + lifecycle rendering)
                             └──> unblocks R2 normalization items (DELIV-2, etc.)
R3 (typed mutation outcomes) ── independent, cheap, do alongside R1
R2 (value objects + lint family) ── the regression guard for everything
R6 ── feature work, model-decision-gated, last
```

**Order (Codex-revised — contract first, so the foundation is right at the start):**

1. **R7 — reconcile the contract + define the public state contract.** Resolve the spec
   self-contradiction (§5.3/§9.3 vs §6.2: choose bootstrap-primary-with-background-work);
   write down concurrent work+infra semantics, authoritative *deployed* evidence, reachability-vs-
   verification HTTP truths, and the migration rules. Strike the wrong load-bearing comments. *No
   code resolver can be correct until this is single-sourced.*
2. **R1 store (behavior-preserving first)** — introduce `internal/workflow/state` with per-record
   flock + mutex-map, typed decode/normalize/write, typed constructors, and legacy-fixture
   compatibility. Initially preserve behavior.
3. **R1 mutations behind the store** — route close-mode, git-push-setup, build-integration,
   setup-cache writeback, first-deploy stamping, and local-adopt through transactional `Update`.
   Dissolves XCUT-1, ADOPT-2/GAP3-1 durability, TOPO-1/WF-2/DELIV-2, SPINE-4/TOPO-2.
4. **R1 lifecycle** — replace scalar phase derivation with `ResolveLifecycle`, consumed by both
   `ComputeEnvelope` and `zerops_workflow status`. Dissolves SPINE-1/2/3.
5. **R3** — typed mutation outcomes + a lint forbidding discarded state/process/work-session errors;
   `BestEffort` wrapper for genuine best-effort.
6. **R2** — move each concept into a structural resolver / value object (setup, deploy-intent,
   idle/adoptability, export-target, env-assembly, named HTTP predicates) + a lint beside each;
   delete the parallels (export variant, `legacyDefaultSetupName`).
7. **R4** — implement the truths on those resolvers: `IsReallyDeployed` from appVersion evidence,
   export `ModeFor`, prod-hostname translation, setup-name → blocker (not `prod`).
8. **R5** — bring export/launch status + narrowing onto the shared store + plan/render machinery
   (shared access + rendering, NOT necessarily identical session storage — launch keeps its
   cross-project identity + one-shot launch-key constraints).
9. **R6** — capability gaps last (bundle service-env layer, local cardinality model, search
   `projectId` predicate, env PUT, git-init invariant).

Every new invariant lands with a pinning test — several findings explicitly note *the missing test
is how the bug slipped* (XCUT-1, SPINE-1, LAUNCH-1/3/4, TOPO-2/3, DELIV-1/2, BOOT-1/2). Build atom-
firing goldens from *real* writers, not hand-set fixtures.

---

## 8. Not roots — recorded so they're not re-litigated

**Refuted by the default-refute verify pass:** DM-2 "fails open on absent mount" (×2); "two snapshot
builders disagree on SetupName" (the real SetupName bug is WF-1, confirmed); "git-push deploy records
under wrong host key" (×2); one redundant framing of the git-push timeout stamp (the real one, XCUT-2,
is confirmed).

**Partials needing a spot-check before planning** (location real, headline severity suspect):
ADOPT-1 (fix only the startWithoutCode half; diagnose-before-destruct pillar refuted), DEV-2 (narrow
trigger), PLAT-1/PLAT-2 (verify the SDK/endpoint capability first), LAUNCH-5 (downgrade to cleanup),
GAP4-3 (real residue is the multi-mount delete race), SPINE-1 (fix is bootstrap-primary-merge, NOT
work-first — confirm spec §5.3 reading).

**Backward-compat seams (published product — must stay transparent):** on-disk `.zcp/state` files
(R1 normalize-on-read must heal legacy empties, not reject them; any state Version bump in R5 needs a
one-way idempotent tested migration), user `CLAUDE.md`/`.mcp.json`/permission allowlists, the
`zerops_export` tool surface (R2 dedup decision). Internal refactors (R1–R4) reshape freely.

---

## 9. Open architectural decisions (owner — Karel)

1. **R1 state package shape** — extract a dedicated `internal/workflow/state` owner (Codex
   recommends this), or add the primitives in place? Extraction is cleaner long-term + gives one
   home for the migrator; in-place is lower blast radius. *Leaning: extract.*
2. ~~**R1 lock granularity**~~ **(resolved by Codex pass):** cross-process ZCP is real
   (`engine.go:19` + the registry's existing flock) → **per-record flock + in-process mutex-map**,
   not a bare package mutex. ServiceMeta locks the canonical (primary) meta file, resolving stage
   hostnames first.
3. **R2 empty-setup policy** — flip every resolver to `empty → structured blocker`
   (`requiresSetupInput`) vs keep a default somewhere. Recommend blocker everywhere (kills the
   `prod`-invention class), gated on seeding fixtures first.
4. **R5 launch/export** — fold into the spine (Version migration) vs one shared stateless-narrowing
   primitive. (Prior plan's open decision #4.)
5. **R6 local runtime cardinality** — is a local checkout deploying to N runtimes a supported
   topology (then build the N-target model) or single-stage by design (then fix the recovery hints
   that promise a path the handler refuses)?
6. **R6 bundle env scope** — carry the service-env layer through classification, or accept project-
   only + warn on dropped service keys?

---

*Per-finding intended/actual/problem/root/fix detail lives in the three source audit docs; this
document is the architectural map over them.*
