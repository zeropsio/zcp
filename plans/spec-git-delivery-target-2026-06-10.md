# SPEC (DRAFT) — Git & Delivery: the target model (2026-06-10)

**Status:** intent specification for Karel's approval. On approval this becomes
`docs/spec-git-delivery.md` and the affected sections of `docs/spec-workflows.md` (§4.3 close-mode,
§GLC, §10) are rewritten against it. Until then it binds nothing.

**Provenance:** Karel's fundament statement (this session, 2026-06-10 evening) + intent archaeology
over the full design corpus (workflow `wf_bcfb4d57-191`: git-flow history, production lifecycle,
close semantics, credential feasibility — every claim file:line-cited) + the ground-up behavior
maps (`plans/git-delivery-foundations-2026-06-10.md`, workflow `wf_0dcb4135-15f`) + live
experiments on eval-zcp (credential helper auth without .netrc; GH_TOKEN precedence over hosts.yml;
env.json in-place rewrite without restart; **fresh SSH sessions see rotated env in seconds** —
verified on the alpine/bun runtime container, no jq present, none needed).

---

## §1 The Fundament

Six intent statements. Everything else in this spec derives from them.

1. **Once git-push is configured, the REPO is the single source of truth and PUSH is the terminal
   act of development.** ZCP never self-redeploys a git-configured service as the close act; the
   develop loop ends with commit + push. The repo never trails the container. *Karel's rationale
   (2026-06-10, confirms the rule):* the dev self-redeploy existed PRIMARILY for persistence —
   baking the code into the artifact so it survives container replacement; once all code persists
   in git, the baking is unnecessary. Self-redeploy of a git-configured service is reserved for an
   explicit FUNDAMENTAL reason (break-glass: integration outage, recovery) — and when it does
   happen, it MUST preserve everything on the dev service (committed-tree precheck + deployFiles
   `[.]` + `-g`), and ZCP flags "container now ahead of repo — push to reconcile".
2. **Builds flow from the repo via integrations.** Dev-only: the push (re)builds the service via
   its integration. Dev+stage: the push builds the STAGE half. Tag push builds PRODUCTION. ZCP
   configures/instructs the integrations and OBSERVES the builds; it does not replace them.
3. **Release is a user intent ZCP can execute end-to-end:** "chci release" → ZCP creates a version
   tag at the (clean, pushed) HEAD and pushes it; the production pipeline does the rest.
4. **Every credential has exactly ONE validated home, consumed LIVE.** GIT_TOKEN lives as a
   service-scope SECRET env on the push source — and nothing else: no `.netrc` files, no
   `gh auth login` copies, no restart-coupled shell snapshots.
5. **Pre-git states keep today's artifact flow.** Until git-push is configured, self-deploy is the
   durability act (the existing first-deploy invariant D2a generalizes to the whole L0 state).
6. **ZCP advises, never gates** (except the standing destructive/evidence gates); done-ness is
   DERIVED from live evidence, never stamped (the standing work-session doctrine — unchanged).

## §2 The Delivery Ladder — a DERIVED state, not a new field

`DeliveryState` is computed read-time from the existing meta axes + live facts (derive-never-stamp;
no new stored enum):

| State | Predicate | Terminal act of development | What rebuilds the service(s) |
|---|---|---|---|
| **L0 artifact** | GitPushState ≠ configured | `zerops_deploy` self/cross (today's flow) | ZCP deploys |
| **L1 repo** | GitPushState = configured | **commit + push** (`zerops_deploy strategy=git-push`) | the integration (Actions / platform webhook), from the repo |
| **L2 released** | L1 + production launched (ProdLaunches non-empty) | L1 + **release act** (§7) for prod | tag-triggered prod pipeline |

Per topology at L1/L2:

| Topology | Push source | Build target of branch push | .git lives | Verify target |
|---|---|---|---|---|
| dev/stage pair | dev half | stage half (cross-build, no `-g`) | dev half only (persistent volume; never CI-replaced) | stage half |
| simple / single | the service | the service itself (self-build, **`-g` required** in CI template) | the service (artifact carries it) | the service |
| local-stage / local-only | user's machine | stage / none | user's local repo (GLC-6 untouched) | stage / local |

**Consequences (the load-bearing changes):**

- **CloseDeployMode shrinks to done-ness OWNERSHIP:** `auto` = ZCP derives done and may auto-close;
  `manual` = the user owns the loop; `unset` = decision pending. The `git-push` VALUE becomes
  redundant — the delivery MECHANISM is no longer chosen by close-mode, it is dictated by the
  ladder. One-way idempotent migration: `git-push` → `auto`.
- **In L1, the push source receives NO ZCP deploys** (extends the already-shipped "the dev half no
  longer receives deploys" from the 2026-05-19 redesign to ALL git-configured topologies, simple
  included). Iteration preview = dev-server; durability = commit (+ push). The never-pushed
  `deploy` commits — the reason `head-not-pushed` is today's EXPECTED launch state — stop existing.
- **L1 with BuildIntegration=none is an incomplete state, not a variant:** the push still ends
  development (code is safe in the repo — the fundament), but nothing rebuilds the service, so the
  done evidence (§6) cannot complete. ZCP surfaces the standing choice: wire the integration
  (recommended, prefilled call) / one-time explicit self-deploy to sync the service / close
  manually. Never silently self-deploys.

## §3 Deliberate supersessions (each was a recorded decision — flagged, not silently dropped)

| Dies | Was | Why it dies |
|---|---|---|
| S5 orthogonality CELL "GitPushState=configured + CloseDeployMode=auto ⇒ self-deploy at close" | pinned invariant + CLAUDE.md bullet | empirically the bug factory: repo trails container, deploy commits never pushed, launch gate friction, prod.txt session. The AXES stay (capability ≠ done-ness ownership); only the self-deploy meaning of that cell dies. |
| CloseDeployMode=`git-push` value | §4.3, S1 four values | mechanism now derived from GitPushState; value folds into `auto` (migration §10) |
| "configured push, no build = archive" cell | decomposition interaction matrix | becomes the L1-incomplete state with a surfaced choice (§2) — the "no build will fire" warning text survives |
| `.netrc` auth pattern (3 emit sites) | spec-workflows.md:60, probe/push/push-proof | replaced by the credential helper (§4); fail-open residue class deleted |
| XCUT-2 setup restart + poll | git-push-setup confirm | obsolete: fresh SSH sessions see rotated env in seconds (live-proven); replaced by a re-probe over a fresh session |
| `gh auth login` + hosts.yml | ghAuthSetupCommand | per-invocation `GH_TOKEN=$(ssh <push-source> 'printf %s "$GIT_TOKEN"')` prefix with empty-guard (live-proven precedence) |
| head-not-pushed as expected pre-launch state | foundations §2 | L1 makes pushed-HEAD the resting state; the blocker remains as genuine drift detection |

NOT superseded (verified against the rejected-alternatives ledger): the three meta axes as records;
no auto-commit of dirty trees (the commit stays the agent's explicit, atom-guided act); no auto-init
of user-local git (GLC-6); no PAT in URLs/buildFromGit; first-deploy exception D2a; P-LP-10/11 gate;
Path B (no platform PUT); declared-scope close denominator; derive-never-stamp; advise-never-gate;
per-PID sessions; one-call compaction recovery.

## §4 Credential model — one home, live consumption

**Home (unchanged, F5):** `GIT_TOKEN` = service-scope SECRET env on the push source. Probe-first
write discipline unchanged.

**Consumption (new):** a url-scoped git credential helper, written into the push source's
`.git/config` by the same single-owner re-assertion pattern as the deploy identity
(InitServiceGit + deploy safety-net + origin-sync + git-state reconstruction):

```
[credential "https://<host>"]
    helper = "!f() { test \"$1\" = get && { echo username=oauth2; echo password=$GIT_TOKEN; }; }; f"
```

- Reads the INVOKING SESSION's env. Every ZCP git operation runs in a fresh SSH session, which
  sees the current platform env within seconds of a rotation — live-verified on the runtime
  container (no restart, no jq, no file read). `/etc/zerops-zembed/env.json` stays the DIAGNOSTIC
  surface (sed-extract for error messages), never the auth path.
- Host-scoped (`credential.https://<host>`) — parity with today's `.netrc machine` line; the host
  derivation stays owned by `ops.parseGitHost` (the launch-gate inline duplicate folds into it —
  the consolidation the 2026-05-28 audit already ordered).
- First-time probe (token not yet written): one-shot inline `-c credential.helper=…` with the
  candidate token via env-assignment prefix — environ-only, no argv leak, no file. Probe-first
  invariant unchanged: no state mutation until the probe passes.
- Token never on disk in any ZCP-created copy. The platform's own env.json (644, plaintext — the
  unavoidable at-rest floor) is unchanged by us either way; ZCP-created copies drop from three
  (.netrc residue, hosts.yml, restart-coupled shell) to ZERO.

**Rotation (new):** git-push-setup with same remote + non-empty gitToken = rotation intent: probe
the new token → re-write the secret → done (~seconds; no restart, no container churn). The O3
short-circuit keeps firing only for token-less re-confirms. Raw `zerops_env set` of credential keys
redacts values in the response `stored[]`.

**gh:** every emitted gh command carries `GH_TOKEN=$(ssh … 'printf %s "$GIT_TOKEN"')` per
invocation (with the `test -n` empty-guard against the device-code hang). No login step, no
hosts.yml, no identity drift — the wrong-token diagnosis class from the prod.txt session dies.

**Remaining second homes, named and bounded:** ZEROPS_TOKEN / ZEROPS_TOKEN_PROD GitHub repo secrets
(CI must hold a Zerops token; validated by the first CI run; rotation pointer in guidance) and the
platform's per-clientUser OAuth grant (webhook path; platform-owned). Both documented as copies
with owners — not silent.

## §5 Git state model

- `.git` ownership unchanged: ZCP owns container-side git (GLC-1..5), user owns local git (GLC-6).
- The artifact law (foundations LAW 2) becomes spec: `.git` reaches a runtime only if the deploying
  path packs it. Single predicate `build target == push source` owns BOTH `includeGit` (ZCP
  deploys) and the emitted CI template's `-g` + `persist-credentials: false` (FP-1). In L1 this
  matters only for self-target topologies; pair dev halves stop being deployed at all.
- `git-state-missing` is a first-class distinct state (absent `.git` ≠ remote drift), with
  handler-side reconstruction in git-push-setup (init + remote + fetch + reset when tree matches
  remote; refuse with diff summary otherwise). Needed for: historic containers, container
  replacement on scale/failure, non-ZCP CI.
- GLC-2 spec text updated to the split init/config shape the code already has (B13).

## §6 Done-ness evidence (goal-contracts aligned)

Develop "done" stays a derived gate over DECLARED scope; the per-service evidence becomes
state-dependent:

| State | Evidence chain (all live-derived, OR-composed with recorded attempts per B4) |
|---|---|
| L0 | deploy succeeded (appVersion ACTIVE) + verify at-or-after it (today, unchanged) |
| L1 | **push receipt** (clean tree + local HEAD == authenticated remote HEAD — the P-LP-11 read, reused) + **build landed** (build-target appVersion ACTIVE, at-or-after the push; zerops_events watch + record-deploy bridge remain the agent's path, derivation the truth) + **verify at-or-after the build** |
| L2 | L1 + release act executed when the user asked for a release (tag visible on remote; prod pipeline observed via the existing read-only status checks) |

The guidance TEACHES the chain (push → watch events → verify) instead of letting launch blockers
teach it. Auto-close fires only on a complete chain; incomplete L1 (integration=none) surfaces the
§2 choice.

**§6.1 The build watch — L1's monitoring primitive (Karel's "domyslet monitoring" item).**
The asymmetry to fix: ZCP's own self-deploy is SYNCHRONOUS (pollBuild watches the build to
terminal, fetches build logs on failure, classifies via ops.ClassifyDeployFailure); the git-push
path returns `PUSHED` immediately and leaves watching to agent memory (zerops_events polling + a
manual record-deploy ack). In L1 the push IS the deploy, so it gets the same treatment:

- `zerops_deploy strategy=git-push` does not end at the push receipt. After PUSHED it DISCOVERS the
  integration-triggered build on the build target — polls for a new build process / appVersion
  created at-or-after the push timestamp (SearchProcesses + appVersion reads; same primitives
  LatestFailedAppVersionContext already uses) — then follows it to terminal exactly like the
  self-deploy path: progress notifications under the existing no-progress-before-response
  discipline, build-log fetch + failureClassification on FAILED, verify-ready handoff on ACTIVE.
- Discovery budget accounts for integration latency: platform webhook fires in seconds; GitHub
  Actions adds runner spin-up + zcli (~1–3 min typical). Default watch window ~10 min. If no build
  appears (integration missing/slow/broken): structured `push-landed-build-not-observed` response
  carrying the re-callable watch (and, when BuildIntegration=none is the recorded cause, the §2
  three-way choice) — never a silent PUSHED.
- The happy path records the deploy attempt ITSELF when the build reaches ACTIVE — the manual
  record-deploy bridge stays only as the recovery path (agent resumed after compaction, watch
  timed out), per derivation-over-stamp.
- Verify then targets the build target as today; the §6 evidence chain composes from these reads.

## §7 The release act (new, source-side — no prod trust-boundary change)

Trigger: user release intent at L2 (or L1 with launch upcoming). ZCP executes on the PUSH SOURCE:
verify clean tree + HEAD on remote (the P-LP-11 read) → derive next version from existing `v*` tags
(suggest, user confirms) → `git tag vX.Y.Z && git push origin vX.Y.Z` via the credential helper.
The tag fires whichever prod pipeline exists: the launch-emitted Actions tag workflow or the
dashboard TAG integration (both remain supported; recommendation follows the declared integration).
ZCP's prod access model is untouched — the tag push is a SOURCE-repo act, same trust surface as
every other push. Shape (action name vs `strategy` extension) = open decision #3.

## §8 Canonical journeys (the scenario catalog, normative)

1. **Greenfield simple → live:** bootstrap L0 → develop, self-deploys → git-push-setup (+ Actions
   with `-g`) → L1: iterate via dev-server, end with commit+push → CI rebuilds the service →
   verify → auto-close.
2. **Pair:** as (1); push builds stage; dev half never deployed again; verify targets stage.
3. **To production:** launch-production (gates naturally green in L1) → prod project + tag
   pipeline wired (FP-5) → L2.
4. **Day-2 / steady state:** edit → push (stage updates) → "release" → §7 act → prod updates. ZCP
   absent from prod runtime (unchanged trust model).
5. **Rotation:** new PAT → git-push-setup same remote + token → probed, written, live in seconds.
6. **Recovery:** container replaced / .git missing → git-state-missing → reconstruction; compaction
   → action=status (unchanged). (The FP-3 private-repo visibility blocker was retired 2026-06-11 by
   pipeline-first launch — see the program table below.)

## §9 Migration & compatibility (user-facing surfaces keep working)

- `CloseDeployMode=git-push` in existing metas: read as `auto` (one-way, idempotent, lazy on first
  read+write; no file rewrite sweep). The close-mode action keeps accepting the value with a
  deprecation note for one release window.
- Existing `.netrc`-era containers: helper asserted lazily by the next git-push-setup / deploy
  safety-net / reconstruction; stray `~/.netrc` deleted by the same single owner when asserting.
- Atoms: develop-close-mode-git-push folds into the L1 delivery atoms; axis values for
  closeDeployModes shrink; ~15 .netrc/restart tell-sites sweep (inventory in the mining map).
- Specs: spec-workflows §4.3/§GLC/§10 + spec-work-session evidence section rewritten; CLAUDE.md
  three-dimensions bullet updated (the S5 cell supersession named explicitly).
- Public docs (zerops-docs): tokens-and-project-access.mdx + delivery-handoff.mdx are stale on two
  counts already (project-scope, .netrc) — update with the helper model in the same pass.

## §10 Relationship to the standing fix programs

| Program | Fate under this spec |
|---|---|
| FP-1 artifact/-g parity, git-state-missing, reconstruction | SURVIVES unchanged (substrate; §5) |
| FP-2 credential truth | SUPERSEDED IN SHAPE: rotation survives; gh identity-assertion and netrc-reconciliation are replaced by elimination (§4) |
| FP-3 private-repo gate | RETIRED 2026-06-11: pipeline-first launch (plans/archive/launch-pipeline-first-2026-06-11.md) removed buildFromGit from the production import — nothing clones without a credential, so repo visibility is irrelevant to launch |
| FP-4 consent & scope (stage recommendation, production profile, HA) | SURVIVES unchanged (orthogonal consent layer) |
| FP-5 tag→prod Actions | SURVIVES + gains the §7 release act as its UX head |
| FP-6 eval round-trip | EXTENDED: + L1 close journey, + rotation, + release act scenarios |

## §11 Open decisions (Karel)

1. **Approve the S5-cell supersession + close-mode value fold** (§2/§3) — CONFIRMED IN SUBSTANCE
   by Karel 2026-06-10 ("když mám jenom dev a nastavím git push, tak už se pak nedělá redeploy…
   jakmile se zaručí, že veškerý kód je v gitu, není potřeba dělat redeploy té dev služby sama do
   sebe" + the pair variant). Formal promotion of this spec to docs/ is the remaining ack.
2. **L1 with integration=none** (§2): surface the 3-way choice (recommended) vs treat push-capable
   no-integration as L0 until an integration exists (simpler, but contradicts "push ends it").
3. **Release act shape** (§7): `zerops_workflow action="release"` (discoverable, atom-guided —
   recommended) vs `zerops_deploy strategy=git-push tag=…` (no new verb).
4. **Mid-iteration deploys to the push source in L1** — RESOLVED by Karel 2026-06-10: break-glass
   only ("pouze v případě, že je k tomu nějaký jiný fundamentální důvod"), always with full
   preservation ("zůstane v té dev službě všechno zachované") — §1.1 carries the rule.
5. Inherited, still open from the foundations doc: private-repo recommendation order; stage-first
   recommendation strength; consented 1-container; unreferenced-deps default; gh floor-version
   check.

## §12 What this kills, in user terms

The prod.txt session's six frictions against this spec: stage question → FP-4 consent (§10);
HA fait accompli → FP-4; foreign db in prod → FP-4 scope; git wiped by CI → §5 parity (+ L1 makes
pair dev halves immune by construction); token rotation dead-end + wrong-token diagnosis →
§4 (rotation path + no gh copy); manual dashboard CD → §7 + FP-5. Plus the launch gate stops
fighting the develop flow: in L1 its checks describe the RESTING state, so the transition to
production becomes a read, not a remediation loop.
