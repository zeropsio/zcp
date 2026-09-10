# ZCP Eval Farm Specification

> **Scope**: the farm — parallel disposable run projects, one bucket sink, one
> pinned evaluator, one deterministic report/coverage oracle. It extends
> `docs/spec-testing-architecture.md §10` (single-run required results,
> candidate binding, report) to many concurrent runs with no VPN or SSH on the
> critical path. It does not restate §10.1–§10.5; a farm bundle's per-run
> evidence is exactly that contract, produced inside one run project. This
> spec owns what changes above the single run: the bucket layout, the run
> project shape, the controller's safety rules, the `verification:` fields
> the farm adds, and the report/coverage verdict vocabulary over many bundles.

Pointer: `docs/spec-testing-architecture.md §4` links here for the farm's
scale-out of the single-run contract.

---

## 1. Sink — bucket layout, bundle contract, evaluator ≠ candidate

### 1.1 Layout

One bucket, one key, in the persistent `zcp-farm` project:

```
evaluators/<sha256>/zcp            # pinned evaluator binary, uploaded once per farm
evaluators/current                 # plain-text pointer: body is the evaluator sha256 last pushed
candidates/<sha256>/zcp            # candidate under test, pushed per batch
scenarios/<tree-digest>/…          # scenario tree, pushed per batch
sets/<tree-digest>/gate.txt        # gate scenario id list, keyed to the scenario tree it names
farm/wrapper.sh                    # the run-project wrapper script

runs/<runId>/started.json
runs/<runId>/results/…             # the runner's own results dir (spec-testing-architecture §10.1)
runs/<runId>/capture/…             # the run's capture window (capture spec §6/§8.5)
runs/<runId>/done.json             # written last; FM-3

batches/<batch>/manifest.json      # written at farm-run start
batches/<batch>/summary.json       # written at farm-run end
```

**FM-1.** Every object under `runs/<runId>/` and every project the controller
creates for that run is named from the same `runId`; nothing under `runs/` is
addressed by any other key.

**FM-2. Evaluator ≠ candidate.** The evaluator is a farm-wide pin: one digest
in the farm config, uploaded once, unrelated to the object under test. The
object under test is only ever named through `--candidate` (`candidates/<sha>/zcp`,
never the evaluator). A bundle records both digests (`meta.json.binding` plus
the farm envelope, FM-9). A candidate change to `internal/eval` (the verifier
itself) never regrades the run that measures it — the evaluator that graded a
bundle is fixed at upload time, not read back from the candidate.

### 1.2 `done.json` — the bundle-complete contract

**FM-3.** `done.json` is the wrapper's last write for a run, after
`runs/<runId>/results/` and `runs/<runId>/capture/` are fully uploaded. Its
absence — for any reason, including a `kill -9` of the wrapper's supervisor
process — means the bundle is incomplete; a run without `done.json` is graded
`blocked: no bundle` by `farm status` and `farm report`, never `not-run` and
never silently skipped.

**FM-4.** `done.json` carries, for every part it lists, that part's own
digest (not just its presence):

```json
{
  "runId": "…", "scenarioId": "…",
  "runnerDimensions": {"execution": "…", "task": "…", "taskEnd": "…"},
  "parts": {
    "results": {"treeDigest": "<sha256>"},
    "capture": {"treeDigest": "<sha256>"}
  },
  "evaluatorSha256": "<sha256>", "candidateSha256": "<sha256>"
}
```

**FM-5. Write order is never trusted.** `done.json` is read only after both
parts have finished uploading (per FM-3), and the controller/report never
infer completeness from write order or object listing order alone — they
recompute each part's tree digest from what actually landed in the bucket and
compare it against the digest `done.json` claims for that part. A part whose
recomputed digest disagrees with `done.json` is `blocked`, not `failed`
(the run itself may have passed; the evidence for it did not survive upload
intact).

**FM-6. Legacy bundle, no digest.** A bundle produced before FM-4 existed
(no `evaluatorSha256`/`candidateSha256`/`parts[*].treeDigest` fields) is
distinct from one that carries a value: `report` prints `unpinned` for it,
never `blocked` and never a fabricated match. `unpinned` means "cannot be
checked," not "checked and wrong." §5 pins this into the verdict vocabulary.

### 1.3 Redaction before upload

**FM-7.** Every value the wrapper holds that is a credential — the agent
credential (`CLAUDE_CODE_OAUTH_TOKEN`), `ZCP_API_KEY`,
the sink key, and a minted `ZCP_E2E_LAUNCH_KEY` when the scenario uses one —
is redacted from `runs/<runId>/results/` and `runs/<runId>/capture/` before
either is uploaded, not after. The redaction pass runs inside the wrapper, in
the run container, over the same values the wrapper was given at boot; it is
not a post-hoc grep on the sink. `TestBundle_NoKnownSecretValue` proves no
known secret value appears in an uploaded bundle; a live grep over an S1 run's
uploaded objects is the acceptance-side check (A2/A5 in the plan's Verify
Trace — not restated here).

**FM-8.** The bucket key is a per-service object-storage user with full read/write on exactly its own bucket and nothing narrower (verified live: `ListAllMyBuckets` returns one bucket, cross-bucket PUT and CreateBucket → 403; `objectStoragePolicy` governs anonymous access only). Access is path-style (`<apiUrl>/<bucketName>/<key>`); the virtual-host form does not resolve.
Because of FM-8, "private by ACL" is never assumed: a run container can read
another run's evidence, so FM-7 is the only thing standing between the sink
and a leaked credential. Evidence itself (transcripts, tool calls, verifier
rows) is not secret and is not redacted beyond FM-7's credential values.

### 1.4 Batch manifests

**FM-9.** `batches/<batch>/manifest.json`, written at `farm run` start,
records: batch id, the scenario set (`--set gate|all|<ids>`), the candidate
digest, the evaluator digest, and the run ids the
batch created. `batches/<batch>/summary.json`, written at the end, records
per-run acceptance (`passed`/`failed`/`blocked`/`not-run`) and whether the
batch's own budget or the operator's Ctrl-C ended it. Neither file is the
registry of truth — `farm status` always recomputes from the bucket listing
and the live project list (§3), never from a cached manifest — but every
report cites which manifest it read.

Shape, as implemented by the controller (S4):

```json
// batches/<batch>/manifest.json
{
  "batch": "…", "createdAt": "<RFC3339>", "startedAt": "<RFC3339>",
  "set": "gate|all|<ids>",
  "candidateSha256": "…", "evaluatorSha256": "…", "scenariosDigest": "…",
  "runs": [{"runId": "…", "scenario": "…", "projectName": "zcp-farm-<runId>"}]
}
```

```json
// batches/<batch>/summary.json
{
  "batch": "…", "finishedAt": "<RFC3339>", "endedBy": "settled|budget|interrupt",
  "runs": [{
    "runId": "…", "scenario": "…", "projectId": "…",
    "result": "passed|failed|blocked|not-run", "detail": "…",
    "error": "…",            // set when the run's project could not be created, minted or imported
    "launchTokenId": "…"
  }]
}
```

`manifest.json.startedAt` is set once, at the same moment as `createdAt` —
`farm coverage --since` (§5.3) reads it to bound a batch's window.
`summary.json.runs[].projectId` is empty once that run's project has been
deleted (the common case for a settled run); `launchTokenId` is the id
(never the token value, §3.4) of a launch scenario's token, present only
while it has not yet been revoked — the FM-21 no-bundle exemption records it
here so `gc` can finish the revoke once the project is finally gone.

---

## 2. Run project — one self-driving disposable project per run

### 2.1 Shape

**FM-10.** A run project is created in two REST steps, never one — a
REST-created `zcp@1` service gets no first-class injected `ZCP_API_KEY` the
way the GUI's own import route does, and the mint below needs a project id
that only exists after step 1 (live-verified 2026-09-10):

1. `CreateAndImportProject` (`POST /client/{clientId}/project/import`) from
   a generated project-only import YAML (`ProjectImportYAML`, §2.3): project
   settings (`sshIsolation: "vpn project"`) and `services: []` — nothing
   else exists in the project yet.
2. `MintProjectScopedToken` (`POST /client/{clientId}/integration-token`,
   SDK `PostClientIntegrationToken`) mints a `NO_ACCESS` account-level token
   scoped `ADMIN` on exactly the new project id — the run's own
   `ZCP_API_KEY` (§2.4). Foreign-project calls with this token 403, project
   creation 403s, service import 200s — verified live. Deleting the project
   deletes the token automatically (a `GET` by id afterward answers
   `clientUserConnectionNotFound`), so run tokens carry no separate revoke
   step (unlike the launch token, §3.4 FM-23).
3. `ImportServiceStack` (`POST /project/{id}/service-stack/import`) from a
   generated service-only import YAML (`ServiceImportYAML`, §2.3) imports
   the one `zcp@1` service named `zcp`, carrying the run descriptor and
   credentials — including the minted token as `ZCP_API_KEY` — as sensitive
   envs (§2.2), plus an inline `zeropsYaml` whose `run.initCommands` list is
   the image's own boot sequence (`install.sh` → `zcp init` → `sudo -E zcp
   init nginx`) followed by one farm command that fetches `farm/wrapper.sh`
   and starts it detached (`nohup … &`). Init commands run after `zcp init`
   and BEFORE the `run.startCommands` units (nginx, code-server), and a
   detached child does not delay `ALL RUN.INIT COMMANDS FINISHED` or ACTIVE
   (verified live: import → ACTIVE 40 s with the child still running). The
   wrapper therefore never assumes code-server or nginx is up.

A mint (step 2) that 403s means the minting credential itself cannot mint
(§2.4 FM-15) — every other run would fail identically, so the controller
rolls back the just-created project shell and aborts the whole batch rather
than skipping one run at a time. Once all three steps land, the existing
fresh-target preflight (`spec-testing-architecture.md §10.4`) passes by
construction — a run project is never adopted, always fresh.

**FM-11.** Nobody reaches into a run container on the critical path: no SSH,
no VPN, no `scp`. The wrapper does everything — download, seed via the
runner, upload, and mark `done.json`. Multi-VPN into a specific run project is
a Mac-side debugging tool for a run that failed to upload; it is never part
of `farm run`/`status`/`pull`/`report`.

### 2.2 Run-descriptor environment

**FM-12.** The run descriptor is delivered as sensitive service envs on the
run project's `zcp` service, set once at import:

| Env | Carries |
|---|---|
| `ZCP_FARM_BATCH` | the batch id this run belongs to |
| `ZCP_FARM_RUN` | this run's id (`runId`) |
| `ZCP_FARM_SCENARIO` | the scenario id this run executes |
| `ZCP_FARM_EVALUATOR_SHA` | the pinned evaluator's SHA-256 (FM-2) |
| `ZCP_FARM_CANDIDATE_SHA` | the candidate's SHA-256 under test |
| `ZCP_FARM_SCENARIOS_DIGEST` | the scenario tree digest the wrapper downloads (`scenarios/<digest>/`, FM-1) |

<!-- PROVE: confirm these five names against the S1/S2 implementation; the plan names them without a final source citation -->

Plus, not part of the descriptor but delivered the same way: the platform-
injected `ZCP_API_KEY` (this project only), the batch credential (§2.4), the
sink key, and — launch scenarios only — a per-run `ZCP_E2E_LAUNCH_KEY`
(§2.4). The wrapper reads all of these from its own environment; it never
receives them any other way (no file drop, no second RPC).

### 2.3 The wrapper — supervisor + child

**FM-13.** `farm/wrapper.sh` runs as a supervisor process that forks a child;
the split exists so that a `kill -9` of the child still produces a bundle
(the supervisor's trap runs the upload) while a `kill -9` of the supervisor
itself is the one case with no bundle at all — that case is the explicit
`blocked: no bundle` outcome (FM-3), not a crash to paper over.

Wrapper steps, in order:

1. download `evaluators/<sha>/zcp`, `candidates/<sha>/zcp`, and the scenario
   tree from the bucket; verify both binary digests against
   `ZCP_FARM_EVALUATOR_SHA`/`ZCP_FARM_CANDIDATE_SHA` before running either;
2. write the private Claude home carrying `CLAUDE_CODE_OAUTH_TOKEN` (§2.4);
   refuse to start if `ANTHROPIC_API_KEY` is set in the environment;
3. run the evaluator's existing single-run binding unchanged:
   `<evaluator> eval behavioral run --candidate <candidate> --candidate-sha256 <sha> --project-id $projectId --ack-disposable-project yes --capture raw --id <scenario> …`
   (`spec-testing-architecture.md §10.4`'s binding, unmodified);
4. at exit — success, failure, max-turns, or signal, via a trap that fires
   regardless of exit path — redact (§1.3) and upload
   `runs/<runId>/results/` then `runs/<runId>/capture/`, then write
   `done.json` last (FM-3, FM-4).

**FM-14.** Seeding is the runner's, unchanged: `SeedEmpty/Imported/Deployed/
Settled/Building` run exactly as in a single supervised run, using the
project-scoped `ZCP_API_KEY` the platform injected. The farm changes nothing
about seed code or seed semantics — it only changes who kicks the runner off
and where the result goes.

The retrospective `--resume` call is text-only (no MCP config, tools
disabled) and optional; its failure is recorded in `meta.error` but never
changes the `Execution:` line the wrapper reads off `child.log` for
`runnerDimensions.execution`.

### 2.4 Credential table

| Credential | Source | Scope | Never |
|---|---|---|---|
| `ZCP_API_KEY` | platform-injected into `zcp@1` at import | this run project only: seed, preflight, verify | leaves the project |
| agent credential — `CLAUDE_CODE_OAUTH_TOKEN` (the farm's long-lived `claude setup-token`; the ONLY supported mode, no API-key fallback — owner decision 2026-09-10) | farm config on the farm host | model requests for this run's agent invocations | any `ANTHROPIC_API_KEY` in a run project: Claude Code lets an API key shadow the OAuth profile, so the wrapper refuses to start when one is present |
| sink key | farm config on the farm host | the bucket only | the run's task prompt, transcript, or any uploaded object (FM-7) |
| `ZCP_E2E_LAUNCH_KEY` | minted by the controller per run (NO_ACCESS + `canCreateProjects`), launch scenarios only | creating this run's prod project | reused across runs; revoked after the run's projects are deleted (FM-24) |

**FM-15.** The account-wide API key that the controller uses to create and
delete projects — `ZCP_FARM_ACCOUNT_TOKEN`, with its client (org) id
`ZCP_FARM_CLIENT_ID`, both sensitive envs on the farm host only — never enters
a run project. A run project's own `ZCP_API_KEY` (minted by the controller,
§2.1 FM-10 step 2) is scoped to that project and cannot create or delete
projects.

`ZCP_FARM_ACCOUNT_TOKEN` MUST be a personal access token — minting a
project-scoped run token (§2.1 step 2) is itself a mint call, and the
platform 403s any mint attempt made by an integration token without
delegation, live-verified with both apiCodes:
`notAllowedForIntegrationTokenWithoutDelegation` and the legacy
`notAllowedForIntegrationToken`. On either code the controller rolls back
the run's already-created project shell and aborts the whole batch — every
other run would fail identically — and `zcp eval farm run` prints one line
naming the fix ("ZCP_FARM_ACCOUNT_TOKEN must be a personal access token:
integration tokens cannot mint run tokens") and exits nonzero.

**FM-16.** Every bundle records the model observed on the wire and the
provider-reported usage. The agent credential is always the farm OAuth token
(presence recorded as `credential: oauth-token`, never the value); a bundle
produced with an `ANTHROPIC_API_KEY` present is `blocked` (FM-7 redaction
still applies to it).

---

## 3. Controller safety — `zcp eval farm` on the farm host

### 3.1 Placement and gate

**FM-17.** `zcp eval farm` lives in the same binary as `zcp eval behavioral`,
on the farm host, in the persistent `zcp-farm` project. It is gated
maintainer-only, the same discipline as `runtime.Info.Authoring` /
`ZCP_AUTHORING` (`docs/spec-authoring-boundary.md`): the farm host holds an
account-wide key and deletes projects, so the same reasoning that gates
authoring applies here — a route reachable without the gate is a defect the
same way an ungated authoring route is. The farm host needs only the
binary — no repo checkout. `farm run` resolves everything it needs (the
`--set gate`/`--set all` scenario id list, each scenario's front matter, and
the evaluator pin absent `--evaluator`) from the bucket (`sets/<digest>/gate.txt`,
`scenarios/<digest>/…`, `evaluators/current`), never from a relative path on
disk; only `farm push`, which runs from a full checkout on a dev machine,
reads `eval/farm/gate-set.txt` off disk, to upload it.

**FM-18.** No daemon and no HTTP surface on the farm host. `farm run` starts
detached (`systemd-run --user` or `nohup`) so its kickoff SSH session may
drop; `farm status`/`pull`/`report`/`coverage` recompute from the bucket
(public S3 endpoint) and the project-list API — never from a listening
service on the farm host. The only network path onto the farm host itself is
one SSH session, over one VPN, for kickoff; nothing on the run projects'
critical path touches the farm host at all (§2.1).

### 3.2 The prefix rule

**FM-19.** Every project the controller creates is named `zcp-farm-<runId>`
(run projects) or `zcp-farm-<runId>-prod` (a launch scenario's target,
FM-23). Every project the controller deletes is matched against that prefix
at the platform-call site — the guard lives where the `DeleteProject` call is
made, never only in a caller one layer up. `TestFarmGC_ForeignPrefix_NeverListed`
pins this: `farm gc`'s candidate list is built from a fake project listing
that includes non-`zcp-farm-` projects, and none of them is ever proposed for
deletion, let alone deleted.

**FM-20.** A project outside the `zcp-farm-` prefix is never touched by any
`farm` verb — not listed as a candidate, not included in a summary, not
mutated. This is a stronger promise than "the controller doesn't delete it":
the prefix check gates every write, including the ones a bug elsewhere might
otherwise reach (mint, revoke, import).

### 3.3 `farm run`

**FM-21.** `farm run --candidate <sha> --scenarios <digest> --set gate|all|<ids>`
creates one project per selected run (§2.1) over REST, then watches the
bucket for each run's `done.json`. It deletes every `zcp-farm-<runId>*`
project belonging to a run once that run reads `done` (FM-3) or once the
run's budget elapses — whichever comes first. A run that never wrote
`done.json` is the sole exemption: its project is **not** deleted by `farm
run` and stays for inspection (FM-3, FM-25). While waiting, the controller
also polls the run project's processes directly (D19): a FAILED
creation-phase process (`stack.create`, `stack.import`) settles the run
`blocked` immediately instead of waiting out the full run budget for a
`done.json` the dead project will never write, and its project is still
deleted per this rule.

**FM-22.** `farm run` writes `batches/<batch>/manifest.json` before creating
any project and `batches/<batch>/summary.json` after the last run in the
batch settles (done, budget-expired, or exempted). Both are evidence, never
the registry (§1.4).

### 3.4 Launch-token lifecycle

**FM-23.** For a launch scenario, the controller mints one
`ZCP_E2E_LAUNCH_KEY` (NO_ACCESS + `canCreateProjects`) per run before
creating that run's project, injects it as a sensitive env (§2.2), and
revokes it after that run's projects (the run project and its
`zcp-farm-<runId>-prod` target) are deleted. A launch token is never reused
across runs and never survives past its run's project deletion. Mint is
`POST /client/{clientId}/integration-token` (the body `MintDelegatedLaunchToken`
already sends); revoke is `DELETE /client/{clientId}/integration-token/{tokenId}`
(SDK `DeleteClientIntegrationToken`), after which the token answers 401 within
seconds (verified live). The controller stores the token id, never the token,
for the revoke step.

### 3.5 No rerun, no expected route

**FM-24.** Phase 1 has no automatic rerun. A failed, blocked, or flaky run is
reported as such; the controller never re-executes a scenario on its own
initiative to try for a better result. A scenario's flake rate is itself a
finding (per-scenario pass rate across the baseline's three repeats), not
something the controller papers over.

**FM-25.** There is no expected-route field and the controller never compares
an observed workflow-step sequence against a golden trajectory. `never` and
`askWhen` (§4.2) are the only decision-shaped gates; everything else about
"how" a run got to its outcome is coverage (§4.3), not a pass/fail input.

### 3.6 `farm gc`

**FM-26.** `farm gc [--older-than <duration>]` deletes `zcp-farm-*` projects
that no currently-running batch references, subject to FM-19/FM-20. A run
whose project was exempted under FM-21 (no `done.json`) stays exempt from
`gc` too, until an operator has looked at it — `gc` never deletes a
no-bundle run's project on a timer alone; `--older-than` only applies to
projects a batch has already finished with. `gc` deletes only projects a
batch manifest names; any other `zcp-farm-*` project is listed as
`exempt: unknown run` and left for the operator.

---

## 4. `verification:` fields, oracle families, decision rows

This section extends `spec-testing-architecture.md §10.1`'s row model and
§10.3's application-oracle pattern; it does not restate the row schema
(`id`/`check`/`scope`/`result`/`expected`/`observed`/`observedAt`/`source`/
`message`) or the aggregation rule (any `failed` → `failed`; else any
`blocked` → `blocked`; else all `passed` → `passed`) — both apply unchanged
to every row this section adds.

### 4.1 New `verification:` fields

```yaml
verification:
  mode: required
  spec: spec-workflows.md §4.3            # pointer only, never a copy
  expectedServices: [...]                  # O3 — unchanged from §10.1
  noFailedProcesses: true                  # O5 — unchanged from §10.1
  allowFailed: [api]                       # O5 — services whose FAILED is the seeded starting point
  liveness: {service: appdev, marker: "team-notes"}   # O2
  unchanged: [appstage]                    # O4 — standalone form of the nodePostgresRecord unrelated-artifact row
  never: [zerops_import{override=true}, zerops_delete]  # decision rows, gate
  askWhen: [GIT_TOKEN_MISSING]             # decision rows, advisory
```

**FM-27.** `spec` names the spec section this scenario proves — one pointer,
never a restatement of that section's prose. The drift lint (FM-33) checks
only that the named section exists; it does not check that the scenario's
assertions match the section's content (that judgment stays human).

**FM-28.** `allowFailed` lists services whose `FAILED` process state is the
scenario's seeded starting point, not a violation. `noFailedProcesses: true`
together with a non-empty `allowFailed` means: no FAILED process on any
service *except* those listed, created after scenario start.

**FM-29.** `unchanged` is the standalone form of the "unrelated artifact
unchanged" row that `nodePostgresRecord` already produces for its `unrelated`
field (`spec-testing-architecture.md §10.3`): each hostname listed gets its
own `unrelated_artifact/<hostname>/unchanged` row, graded the same way
(active app-version id at freeze == at baseline), independent of whether the
scenario also declares `nodePostgresRecord`.

### 4.2 Decision rows — `never` and `askWhen`

**FM-30.** A decision row is graded from captured MCP traffic (the same
records `mcpstream` and coverage read, §4.3), not from a platform read. Each
`never` entry is a call-shape expression (tool name plus argument
constraints, e.g. `zerops_import{override=true}`); one matching call anywhere
in the run's captured stream produces a `failed` row
`decision/<expr>` — `never` gates exactly like an outcome row.
`TestVerification_NeverRow_FailsOnMatchingCall` pins that one matching call is
sufficient and that a run with zero matching calls produces a `passed`
`decision/<expr>` row, never a `not-run` (the check is executable whenever
the run produced any captured stream).

**FM-31.** `askWhen` entries are error codes. In phase 1 the row they produce
is advisory only: it never contributes to the scenario's aggregated result
(§10.1's aggregation applies only to outcome rows and to `never` rows). The
row records whether a user-simulation turn occurred between the named error
code appearing in the captured stream and the next mutating call; this is a
finding, printed by `report`, not a gate. `askWhen` may be promoted to a
gating row for an individual scenario once the baseline shows it is stable
(operator decision per scenario, not a phase-1 default).

**FM-32.** There is no `reach:`/expected-route field anywhere in
`verification:`. An ordered list of workflow steps is not an assertion this
spec supports; the observed route is coverage data (§4.3), and a correct
outcome reached by an unexpected route is never a failure.

### 4.3 Vocabulary is derived, never listed

**FM-33.** Every name a `never`/`askWhen`/coverage cell can use — tool names,
`action` argument values, `workflow.Phase` values, `VerificationConfig` field
names — is derived from the server's own registries at test time, the same
discipline `eval_scenario_drift_test.go` already applies to scenario front
matter. A scenario naming something that no longer exists in those
registries fails the drift lint at build time, before any run.

### 4.4 Oracle families O6–O9

These extend the O1–O5 families already covered by §10.1/§10.3 of
`spec-testing-architecture.md` (O1 record round-trip, O2 HTTP liveness via
`liveness`/`subdomainProbe`, O3 topology shape via `expectedServices`, O4
unrelated-unchanged via `unchanged`, O5 no-unexpected-failure via
`noFailedProcesses`/`allowFailed`):

| Id | Observes | Rows |
|---|---|---|
| O6 launch shape | prod project exists; runtimes `startWithoutCode`; no `buildFromGit`; first release is the first build; launch token never appears in the transcript | `launch_shape/<field>` per checked field |
| O7 artifact promotion | after a dev→stage promote, the target's ACTIVE appVersion was `created` after run start with `source: "CLI"` and `publicGitSource: null` (zcp's cross-deploy is a `zcli push --service-id <target>` issued from the source container, so the target receives its own full build — `build` is populated; there is no platform field pointing at the source service or a parent appVersion, verified live), the target's earlier `startWithoutCode` stamp (`source: "NONE"`, `build: null`) is `BACKUP`, and the dev service's ACTIVE appVersion id is unchanged. A target appVersion with `source: "GIT"` (an independent `buildFromGit` build) fails the row. Whether the push was cross-service is a decision-side fact: the captured `zerops_deploy` call carrying `sourceService`/`targetService` (§4.2 vocabulary) | `artifact_promotion/<target>/{created_after_start,source_cli,no_git_source,dev_unchanged}` |
| O8 no fabricated secret | capture grep over tool inputs for token-shaped values the user never supplied in the scenario's own fixtures | `no_fabricated_secret/<runId>` |
| O9 cost and turns | provider tokens, MCP call counts, deployment counts, recovery-turn counts | advisory only; never gates (same status as S4-min's cost reporting) |

**FM-34.** O9 rows never contribute to a scenario's aggregated result,
regardless of mode. They are printed by `report` (§5) alongside gating rows
but are excluded from the aggregation §10.1 defines.

---

## 5. Report and coverage — verdict vocabulary over many bundles

This section extends `spec-testing-architecture.md §10.5`'s single-run report
to `farm report` (many bundles) and adds `farm coverage`; it reuses that
section's principle unchanged — the report has no verdict authority beyond
what is already frozen in the bundle's own files.

### 5.1 Verdict vocabulary

| Verdict | Meaning | Applies to |
|---|---|---|
| `passed` | every row for this dimension is proven true | a run, or a single row |
| `failed` | at least one row is proven false | a run, or a single row |
| `blocked` | evidence for at least one row could not be obtained, or the bundle itself is incomplete/unverifiable | a run, or a single row; a bundle with a digest mismatch (FM-5) or a foreign evaluator (FM-35) is `blocked`, never `failed` |
| `not-run` | no task work happened for this row/run | a row (§10.1) only — a run whose project was never created is reported `blocked` with its `error` (FM-9), never dropped |
| `unpinned` | the bundle predates FM-4's digest fields and cannot be checked against the evaluator/candidate pin | a bundle only, never a row; distinct from `blocked` — `unpinned` is "cannot check," `blocked` is "checked and failed" |

**FM-35.** A bundle whose `evaluatorSha256` differs from the farm's current
pin (FM-2) is `blocked` in `report`, never silently graded under the wrong
evaluator and never `failed` — the run's own outcome is not in question, the
evidence's comparability is. `TestFarmReport_ForeignEvaluator_Blocked` pins
this.

**FM-36.** A bundle missing a part `done.json` claims (FM-4/FM-5) is
`blocked`. A run whose project shows no `done.json` at all (FM-3) is
`blocked: no bundle`, distinguishable in `report`'s output from a `blocked`
row inside an otherwise-complete bundle.

A bundle whose `done.json` already names an `error:`-prefixed
`runnerDimensions.execution` (FM-13) — a signal-kill mid-run, an
execution-binding preflight failure, or any other execution error — grades
`failed` straight from that field, never `blocked`: the run's outcome is
already known, it is not evidence in question, even though the capture tree
never reached a window the report builder can open.
`TestFarmReport_SignalKilledBundle_FailedNotBlocked`,
`TestFarmReport_ExecutionBindingError_FailedNotBlocked`.

### 5.2 `farm report`

**FM-37.** `farm report <batch>|<run>` runs entirely over pulled bundles (no
network beyond the initial `farm pull`) and reproduces the single-run report
shape of `spec-testing-architecture.md §10.5` per run, plus a batch roll-up:
per-scenario verdict, cost, and the manifest/summary it read from (FM-9).
`TestFarmReport_Run5Golden_ByteIdentical` pins that `report` over the
preserved S5 run-5 bundle reproduces that run's known-good `report.txt`
byte-for-byte — the farm's report is not a new format, it is the single-run
report's contract applied per bundle in a batch.

**FM-38.** Deleting or corrupting any file `done.json` lists a digest for
turns that run's report `blocked` (FM-5/FM-36), never silently `passed`.

### 5.3 `farm coverage`

**FM-39.** `farm coverage [--since <batch>]` derives (scenario, workflow
step, tool decision) cells only from captured MCP traffic and envelope phase
markers already recorded in each bundle's capture window — never from a
hand-maintained list (FM-33). A scenario removed from the corpus drops every
cell it was the sole source of; a re-run with fewer scenarios visibly shows
fewer cells, never stale ones held over from a prior batch.

**FM-40.** Coverage is descriptive, not a gate: it never contributes a row to
any scenario's aggregated result. Its only effect on §4/§5's verdicts is
none — a scenario can be `passed` with thin coverage and `failed` with rich
coverage; the two dimensions are reported side by side, never merged.
