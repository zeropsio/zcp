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
sets/<scenariosDigest>/gate.txt    # gate scenario id list, keyed to the scenario tree it names
farm/wrapper/<sha256>.sh           # the run-project wrapper script, content-addressed
farm/wrapper/current               # plain-text pointer: body is the wrapper sha256 last pushed

runs/<runId>/started.json
runs/<runId>/results/…             # the runner's own results dir (spec-testing-architecture §10.1)
runs/<runId>/capture/…             # the run's capture window (capture spec §6/§8.5)
runs/<runId>/done.json             # written last; FM-3
runs/<runId>/observer/<obsId>.json # advisory observations (§7) — never listed in done.json, never graded

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
  "evaluatorSha256": "<sha256>", "candidateSha256": "<sha256>",
  "credentialMode": "oauth-token", "redacted": ["<path>", "…"]
}
```

`credentialMode` is always `"oauth-token"` (FM-16) — the field exists so a
future second mode has somewhere to be recorded, not because the wrapper
chooses between modes today. `redacted` lists every path (relative to the
run's own root) FM-7's redaction pass rewrote in place; `[]` when nothing
was redacted.

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

The run-project wrapper itself is content-addressed and verified the same way: the init line fetches `farm/wrapper/<sha256>.sh` and `sha256sum`-checks it before ever executing it (§1.1, R5).

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
per-run acceptance (`passed`/`failed`/`blocked`/`not-run`) and `endedBy`:
`settled` when every run reached a verdict on its own, `budget` when the
batch's own budget ended it, or `interrupt` when the operator's Ctrl-C did.
Neither file is the
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
  "observer": "claude-sonnet-5|off",
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
`manifest.json.observer` is written by `farm run` from `--observer` (§3.3,
§7.7); a manifest without the field (batches older than §7) is never observed
automatically.
`summary.json.runs[].projectId` is empty once that run's project has been
deleted (the common case for a settled run); `launchTokenId` is the id
(never the token value, §3.4) of a launch scenario's token, present only
while it has not yet been revoked — the FM-21 no-bundle exemption records it
here so `gc` can finish the revoke once the project is finally gone. An
interrupted batch (`endedBy: interrupt` — Ctrl-C/SIGTERM) exempts every run
RunBatch was still waiting on the same way: its row reads `blocked` with
`detail: "interrupted"`, and `projectId` and `launchTokenId` are both kept,
exactly as FM-21's own no-bundle exemption keeps them. `error` is set only
when the run's project itself could not be created, minted or imported — the
run never reached `waitForDone`. For a settled `blocked` or `failed` run,
`detail` names the blocking/failing check ids: sorted ids of every
`verification.json` row (spec-testing-architecture.md §10.1), sibling to
`meta.json` in the same results directory, whose `result` equals the run's
own result, joined with `", "` and capped at 5 with a `"+N more"` suffix; a
bundle with no `verification.json` sibling gets the literal
`"no verification.json in bundle"`. Two more `detail` shapes come from
`waitForDone` itself, before any bundle exists: `"no bundle"` when the run's
budget elapsed with no `done.json` (FM-3, FM-21's exemption — that run's
project is not deleted), and `"platform: <actionName> FAILED: <reason>"`
when the controller finds a FAILED creation-phase process
(`stack.create`/`stack.import` whose refs name the control service `zcp`, or
a ref-less project-level action) on the run's project before the run's
`started.json` exists — that project is still deleted; once `started.json`
exists the controller stops polling processes, so a failure of the agent's
own import never ends the run (§3.3). A run whose project was never created
is reported `blocked` with its `error`, never dropped (§5.1).

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
| `ZCP_FARM_WRAPPER_SHA` | the wrapper's sha256; the init line fetches `farm/wrapper/<sha>.sh` and `sha256sum -c`s it before exec (§1.1) |

Plus, not part of the descriptor but delivered the same way: the run's own
`ZCP_API_KEY` — the project-scoped token the controller mints at import
(§2.1 FM-10 step 2), not a platform-injected one — the batch credential
(§2.4), the sink key, and — launch scenarios only — a per-run
`ZCP_E2E_LAUNCH_KEY` (§2.4). The wrapper reads all of these from its own
environment; it never receives them any other way (no file drop, no second
RPC).

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
   `<evaluator> eval behavioral run --candidate <candidate> --candidate-sha256 <sha> --project-id $projectId --ack-disposable-project yes --capture raw --id <scenario> … --capture-dir <local capture dir>`
   (`spec-testing-architecture.md §10.4`'s binding, unmodified) — the local
   capture dir is the one uploaded as `runs/<runId>/capture/`, and the
   evaluator opens its own capture window under it, named `capture-<id>/`
   (its own session id, distinct from `runId`); a run project executes
   exactly one scenario, so exactly one such window exists (§5.2 relies on
   this);
4. at exit — success, failure, max-turns, or signal, via a trap that fires
   regardless of exit path — redact (§1.3) and upload
   `runs/<runId>/results/` then `runs/<runId>/capture/` (the `capture-<id>/`
   window inside it), then write `done.json` last (FM-3, FM-4).

**FM-14.** Seeding is the runner's, unchanged: `SeedEmpty/Imported/Deployed/
Settled/Building` run exactly as in a single supervised run, using the
run project's own `ZCP_API_KEY` — the token the controller minted at import
(§2.1 FM-10 step 2). The farm changes nothing about seed code or seed
semantics — it only changes who kicks the runner off and where the result
goes.

The retrospective `--resume` call is text-only (no MCP config, tools
disabled) and optional; its failure is recorded in `meta.error` but never
changes the `Execution:` line the wrapper reads off `child.log` for
`runnerDimensions.execution`.

### 2.4 Credential table

| Credential | Source | Scope | Never |
|---|---|---|---|
| `ZCP_API_KEY` | minted by the controller (§2.1 FM-10 step 2), delivered as a sensitive env on `zcp@1` at import | this run project only: seed, preflight, verify | leaves the project |
| agent credential — `CLAUDE_CODE_OAUTH_TOKEN` (the farm's long-lived `claude setup-token`; the ONLY supported mode, no API-key fallback — owner decision 2026-09-10) | farm config on the farm host | model requests for this run's agent invocations | any `ANTHROPIC_API_KEY` in a run project: Claude Code lets an API key shadow the OAuth profile, so the wrapper refuses to start when one is present |
| sink key | farm config on the farm host | the bucket only | the run's task prompt, transcript, or any uploaded object (FM-7) |
| `ZCP_E2E_LAUNCH_KEY` | minted by the controller per run (NO_ACCESS + `canCreateProjects`), launch scenarios only | creating this run's prod project | reused across runs; revoked after the run's projects are deleted (FM-24) |
| `ZCP_FARM_CONSOLE_TOKEN` | generated by `eval/farm/console/deploy.sh`; sensitive env on the `console` service; the operator's copy in `~/.zerops-dev/agent-creds/farm-console.env` (mode 0600) | console login and agent API (§8.2) | printed, logged, rendered into a page, or passed to the observer process |
| agent credential on the console — the same `CLAUDE_CODE_OAUTH_TOKEN` | sensitive env on `console` | the observer's model calls only (§7.4) | any env of the console's other children; `ANTHROPIC_API_KEY` |
| sink key on the console | `${os_accessKeyId}`/`${os_secretAccessKey}` references on `console` | reading the bucket; writing only under `runs/<runId>/observer/` — enforced by code (FM-47), not by the key, which is bucket-wide | passed to the observer process |

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
(presence recorded as `done.json.credentialMode: "oauth-token"`, FM-4, never
the value); a bundle produced with an `ANTHROPIC_API_KEY` present is
`blocked` (FM-7 redaction still applies to it).

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
binary and its service envs — `ZCP_AUTHORING=1` (this gate),
`ZCP_FARM_S3_URL`/`ZCP_FARM_S3_BUCKET`/`ZCP_FARM_S3_KEY`/`ZCP_FARM_S3_SECRET`
(the sink), `ZCP_FARM_ACCOUNT_TOKEN`/`ZCP_FARM_CLIENT_ID` (§2.4 FM-15), and
`CLAUDE_CODE_OAUTH_TOKEN` (§2.4) — no repo checkout. `farm run` resolves everything it needs (the
`--set gate`/`--set all` scenario id list, each scenario's front matter, and
the evaluator pin absent `--evaluator`, the wrapper pin absent `--wrapper`)
from the bucket (`sets/<digest>/gate.txt`, `scenarios/<digest>/…`,
`evaluators/current`, `farm/wrapper/current`), never from a relative path on
disk; only `farm push`, which runs from a full checkout on a dev machine,
reads `eval/farm/gate-set.txt` off disk, to upload it.

The gate set is currently 10 scenarios. The two launch scenarios (O6) are
deferred out of it until a source-control fixture exists for them to build
from.

**FM-18.** No daemon and no HTTP surface on the farm host. `farm run` starts
detached (`systemd-run --user` or `nohup`) so its kickoff SSH session may
drop; `farm status`/`pull`/`report`/`coverage` recompute from the bucket
(public S3 endpoint) and the project-list API — never from a listening
service on the farm host. The only network path onto the farm host itself is
one SSH session, over one VPN, for kickoff; nothing on the run projects'
critical path touches the farm host at all (§2.1). The console (§8) is the
farm's only HTTP surface. It is a separate service and never holds
`ZCP_FARM_ACCOUNT_TOKEN` or `ZCP_FARM_CLIENT_ID` (FM-49), so nothing reachable
over HTTP can create or delete a project.

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
deleted per this rule. This process poll runs only until
`runs/<runId>/started.json` appears in the bucket (the run's own wrapper is
underway) and only counts a process whose `serviceStacks[]` names the
control service `zcp` or carries no service ref at all, so a FAILED
`stack.import` the run's own agent triggers mid-run for one of ITS services
is never mistaken for the platform failing to create the run's own project.

**FM-22.** `farm run` writes `batches/<batch>/manifest.json` before creating
any project and `batches/<batch>/summary.json` after the last run in the
batch settles (done, budget-expired, or exempted). Both are evidence, never
the registry (§1.4).

`farm run --observer <model>|off` (default `claude-sonnet-5`; models
`claude-sonnet-5`, `claude-opus-5`, `claude-fable-5-1`) records the choice in
the manifest (§1.4, §7.7); it never reaches a run project. `--batch` must
match the batch-id grammar of FM-47; any other value is a flag error.

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
hand-maintained list (FM-33). A run's scenario is identified from its
bundle's own `done.json.scenarioId` (FM-4), never from a directory name or a
manifest lookup. A cell with no workflow-step marker is bucketed under the
literal `"(no step)"` rather than dropped, so a run that made tool calls
outside any phase still shows up in the table. A scenario removed from the
corpus drops every cell it was the sole source of; a re-run with fewer
scenarios visibly shows fewer cells, never stale ones held over from a prior
batch.

**FM-40.** Coverage is descriptive, not a gate: it never contributes a row to
any scenario's aggregated result. Its only effect on §4/§5's verdicts is
none — a scenario can be `passed` with thin coverage and `failed` with rich
coverage; the two dimensions are reported side by side, never merged.

---

## 6. Known gaps

These are current limitations of the eval lane, not roadmap items.

**Cross-deploy under the evaluator's work dir.** The agent's `zcp` MCP
server runs with the evaluator's `--work-dir` as its working directory, so
the deploy tool's cross-deploy preflight looks for the source service's
`zerops.yaml` under `<work-dir>/<sourceHostname>`, while `zerops_mount`
always mounts a service at `/var/www/<hostname>`. Outside the evaluator both
roots are `/var/www`; in a farm run they differ, so `zerops_deploy
sourceService=…` fails with `PREFLIGHT_FAILED … source mount
<work-dir>/<host> missing` and a cross-deploy scenario cannot promote.

---

## 7. Observer — an advisory evaluation of every finished run

### 7.1 Role

**FM-41.** The observer reads one finished run's bundle and writes a short
evaluation for ZCP maintainers: whether the agent reached the user's goal,
where ZCP helped or misled it, where the agent erred on its own, whether the
deterministic checks match what happened, and whether the agent's self-review
is truthful. It is advisory. It never produces or changes a verdict: `done.json`,
`summary.json`, `farm report` and `farm coverage` never read an observation.
It runs outside the run project, after that run's `done.json` exists, so it
cannot delay the run, alter its bundle, or keep its project alive.

### 7.2 Inputs and step numbering

**FM-42.** The observer reads only uploaded, already-redacted bundle files
(§1.3): from `results/<suite>/<scenario>/` — `task-prompt.txt`,
`transcript.jsonl`, `verification.json`, `meta.json`, `self-review.md`,
`platform-snapshot.json`; and `capture/<window>/eval/<suite>/<scenario>/scenario.md`.
A missing optional file (`self-review.md`, `platform-snapshot.json`,
`scenario.md`) is rendered as "(not recorded)"; a missing `transcript.jsonl` or
`meta.json` makes the observation `status: "error"`.

A run is a numbered list of **steps**, a pure function of those files (the
same bytes always yield the same numbers). Numbering starts at 1:

1. Step 1 is kind `user`: the text of `task-prompt.txt`.
2. `transcript.jsonl` (Claude Code stream-json) is read in file order. Each
   `system`/`init` event after the first starts a resumed segment: it
   contributes one `user` step whose text is `meta.json.userSim.turns[k].reply`
   for the k-th resumed segment (0-based), or `(user-sim turn; text not
   recorded)` when that entry is absent.
3. Each `assistant` event contributes one step per content block, in block
   order: `thinking` → kind `thinking`; `text` → kind `agent`; `tool_use` →
   kind `tool` (tool name, input JSON). A `tool` step's result is the
   `tool_result` block with the same `tool_use_id` (text content concatenated,
   `is_error` kept); a tool call with no result has result `(no result recorded)`.
4. No other event is a step.

The observer's digest, the console's step view and the agent API all use this
one numbering. `TestSteps_*` pins it.

### 7.3 Digest

**FM-43.** The observer sees a plain-text digest with fixed sections in this
order: `RUN` (run id, scenario id, deterministic verdict, duration, cost,
agent model), `TASK` (task prompt), `SCENARIO` (scenario.md, headed "the agent
never saw this file"), `CHECKS` (every `verification.json` row: id, result,
expected, observed, source), `STEPS`, `FINAL STATE` (services and their status
from `platform-snapshot.json`), `SELF-REVIEW` (headed "written by the agent
after the run, from its own memory"). A step renders as `#<n> <kind> …`; a
`tool` step as `#<n> tool <name> <input JSON>` followed by `  → <result>` or
`  → ERROR <result>`.

A tool step's input is the transcript's raw `input` JSON compacted with
`json.Compact` — never decoded and re-encoded — in the digest, the step view
and the quote check alike, so a quote copied from the digest matches the step.

Truncation keeps the digest bounded and says what it dropped: a tool result
over 6,000 characters keeps its first 4,000 and last 2,000 (an error result:
first 8,000 and last 4,000 of anything over 12,000); a thinking block over
2,000 and a tool input over 2,000 keep their first 2,000; each cut is marked
`[… <n> chars omitted …]`. A digest over 400,000 characters drops whole steps
from the middle of `STEPS`, marked `[… steps #<a>–#<b> omitted …]`. The
digest's sha256 is recorded in the observation.

### 7.4 Invocation

**FM-44.** One call per observation:
`claude -p <observer prompt> --model <model> --tools "" --max-turns 1 --output-format json`,
digest on stdin, working directory a fresh empty temp dir, and an environment
of exactly `PATH`, `HOME` (a fresh private 0700 temp dir, removed afterwards)
and `CLAUDE_CODE_OAUTH_TOKEN` — no sink key, no console token, no `ZCP_*`
value passes. The observer refuses to start when `CLAUDE_CODE_OAUTH_TOKEN` is
empty or `ANTHROPIC_API_KEY` is set in its own environment (§2.4). Timeout: 5
minutes. The `claude` path is made absolute (`exec.LookPath`, then
`filepath.Abs`) before the child starts, because the child's working
directory is the empty temp dir. The prompt is an embedded file; its sha256 is recorded as
`promptSha256`. Default model: `claude-sonnet-5`.

### 7.5 Observation document

**FM-45.** `runs/<runId>/observer/<obsId>.json`, `obsId` =
`<UTC YYYYMMDDTHHMMSSmmmZ>-<model>` (milliseconds); the newest `obsId` is the
run's current observation, older ones stay listed. The store never
overwrites an existing observation key.

```json
{
  "formatVersion": "zcp-farm-observation-1",
  "runId": "…", "obsId": "…", "model": "claude-sonnet-5",
  "createdAt": "<RFC3339>", "durationMs": 0, "costUsd": 0.0,
  "promptSha256": "…", "digestSha256": "…",
  "status": "ok|unparsed|error", "error": "…", "raw": "…",
  "headline": "…",
  "goal": {"reached": "yes|partly|no", "why": "…"},
  "checks": {"verdict": "passed|failed|blocked", "agree": true, "why": "…"},
  "findings": [{
    "severity": "high|medium|low",
    "owner": "zcp-guidance|zcp-tool|platform|agent|scenario|evaluator",
    "title": "…", "what": "…",
    "evidence": [{"step": 18, "quote": "…", "verified": true}],
    "lookAt": "…", "fix": "…"
  }],
  "selfReview": {"accurate": "yes|partly|no", "note": "…"}
}
```

The model's answer is the first top-level JSON object in its final text.
`status` is `ok` when it parses and validates (enums as above, at most 5
findings, each with at least one evidence entry), `unparsed` when it does not
(`raw` keeps the answer, capped at 20,000 chars), `error` when the call failed
(`error` says why). `checks.verdict` is the run's verdict as the farm
reports it, never the model's: the `result` of the run's row in
`batches/<batch>/summary.json` when that summary exists, else
`meta.json.task.result` (spec-testing-architecture §10.1). The local verb
reads `<run-dir>/../summary.json` when present (a `farm pull --batch` layout),
else `meta.json`. The console and the observer use this one rule. A run
without `done.json` has no verdict and is never observed.

**FM-46. Quote check.** An evidence entry is `verified` iff its `quote` is a
substring of the cited step's full, untruncated text (a `tool` step's text is
its input JSON plus its result, §7.3) once both are normalized: JSON string
escape sequences (`\"`, `\\`, `\/`, `\b`, `\f`, `\n`, `\r`, `\t`, `\uXXXX`) are
decoded — ZCP's tool results are JSON text, so the agent saw `\"x\"` and
`\u003c` where the model quotes `"x"` and `<` — and every whitespace run is
collapsed to one space; the plain collapsed comparison counts as well. Step 0
names the digest's `CHECKS` section: the prompt tells the model to cite a
deterministic check as step 0, and a step-0 quote is verified against the
rendered `CHECKS` text. Any other step number outside the run is unverified.
Every surface that shows an observation shows its unverified-quote count.

### 7.6 Writers and immutability

**FM-47.** Observer code writes only keys under `runs/<runId>/observer/`; the
store refuses any other key. A batch id matches `^[a-z0-9][a-z0-9-]{0,62}$`; a
run id is `<batch>-<scenarioId>` and matches `^[a-z0-9][a-z0-9-]{0,127}$`. The
store, the bundle readers and every console route reject any other id before
building a key. Nothing under `runs/<runId>/{started.json,
done.json, results/, capture/}` is written after `done.json`. Observations
are not parts of `done.json` (§1.2) and carry no digest there.

### 7.7 When an observation happens

**FM-48.** `farm run --observer <model>|off` (default `claude-sonnet-5`)
records the choice in the manifest. The console's worker (§8.5) observes each
run of a batch whose manifest names a model, once the run's `done.json`
exists and the run has no observation. `off` and a missing field are never
observed automatically. On demand: the console's actions (§8.5), and
`zcp eval farm observe <run-dir> [--model <m>]`, which works over a pulled
bundle, writes `<run-dir>/observer/<obsId>.json`, prints the rendering and
never touches the bucket. A failed observation is stored with `status:
"error"` and is not retried automatically.

---

## 8. Console — the hosted view over the bucket

### 8.1 Service

**FM-49.** Service `console` in `zcp-farm`, runtime `nodejs@22`, deployed by
`eval/farm/console/deploy.sh`: a prebuilt linux `zcp` binary plus the
`claude` CLI installed at build time at a pinned version, started as
`zcp eval farm console --listen :8080 --claude <path>`, public on its
zerops.app subdomain. Envs: `ZCP_AUTHORING=1`, `ZCP_FARM_S3_URL`/`BUCKET`/`KEY`/`SECRET`
as `${os_*}` references, sensitive `CLAUDE_CODE_OAUTH_TOKEN` and
`ZCP_FARM_CONSOLE_TOKEN`, optional `ZCP_FARM_OBSERVER=off`. It never holds
`ZCP_FARM_ACCOUNT_TOKEN`/`ZCP_FARM_CLIENT_ID`; run state comes from the bucket
alone (`started.json` without `done.json` = running). `deploy.sh` is
idempotent: it imports the service when missing (generating the console token
and writing the operator copy, §2.4) — when the service exists but the
operator copy has no token it exits 1 naming that file, never writing a
tokenless copy — pushes with `ZEROPS_TOKEN` and a private
`ZEROPS_CLI_DATA_FILE_PATH` in its own temp dir outside the pushed directory
(never the operator's own zcli login, never uploaded), enables
subdomain access explicitly after the deploy (the import flag alone does not
route a service imported without code), and prints the URL. No secret value
is ever printed.

### 8.2 Authentication and headers

**FM-50.** Every route except `GET /login`, `POST /login`, `GET /healthz` and
`GET /static/*` requires `Authorization: Bearer <console token>` or a session
cookie. `POST /login` compares in constant time and sets the cookie
`farm_session` = `<expiry>.<hex HMAC-SHA256(token, expiry)>`: HttpOnly, Secure,
SameSite=Strict, Path=/, 30 days — rotating the token ends every session.
Both the login form and the bearer header are compared in constant time, and
every failed login or bearer mismatch is answered no sooner than 1 s; there
is no global lockout (the token is 32 random bytes, so a lockout would only
let a stranger lock the owner out). A request without auth gets a 303 to
`/login` (HTML routes) or 401 (`/api/*`). State-changing routes are POST
only; a cookie-authenticated POST must carry `Origin: <scheme>://<host>`,
where host is `X-Forwarded-Host` when present, else `Host`, and scheme is
`X-Forwarded-Proto` when present, else `https` under TLS and `http` without;
a bearer-authenticated POST needs no Origin.
Every response carries `Content-Security-Policy: default-src 'none';
style-src 'self'; img-src 'self' data:; form-action 'self'; frame-ancestors
'none'; base-uri 'none'`, `X-Content-Type-Options: nosniff`,
`Referrer-Policy: no-referrer`, `Cache-Control: no-store`. Pages use no
script, no inline style and no external asset; they render in light and dark
(`prefers-color-scheme`) and at 400 px.

### 8.3 Pages

**FM-51.**
- `/` — batches, newest first: id, created, candidate sha (12 chars), set,
  count per verdict, total cost, observed runs n/m.
- `/b/<batch>` — one row per run: scenario, verdict, duration, cost, the
  current observation's headline (or `observing…`, `not observed`,
  `observer off`), failed/blocked check ids.
- `/r/<runId>` — header (scenario, verdict, times, cost, candidate and
  evaluator sha); the current observation (headline, goal, agreement with the
  checks, findings with severity, owner, title, what, evidence linking to
  `#s<n>` with a verified mark, lookAt, fix; unverified-quote count); older
  observation versions; a re-observe form with a model picker; failed and
  blocked checks with expected, observed, source; the self-review; the task
  prompt; every step with anchor `s<n>`, collapsible, full input and result;
  and the local forensic command `zcp eval farm pull <runId> --out <dir>` then
  `zcp capture ui <dir>/<runId>/capture`.
- `/findings?since=<window>&owner=<owner>` — findings of every run whose
  `meta.json.startedAt` falls in the window (default 24h, same rule as §8.4), grouped by owner then
  severity, each linking to its run and step.

### 8.4 Agent API

**FM-52.** Same authentication. Markdown endpoints for agents, each with a
JSON twin at the same path ending `.json`:
- `GET /api/runs.md?since=<window>` or `?batch=<id>` — one line per run:
  run id, scenario, verdict, cost, headline, finding titles.
- `GET /api/runs/<runId>.md` — run header, current observation in full,
  failed/blocked checks, the step ranges its evidence cites.
- `GET /api/runs/<runId>/steps.md?from=<n>&to=<m>` — those steps, untruncated.
- `GET /api/runs/<runId>/self-review.md`
- `GET /api/findings.md?since=<window>` — every finding in the window,
  grouped by owner then severity, with run id and step numbers.
- `GET /api/runs/<runId>/files/<path>` — one bundle file under `results/`,
  served as `text/plain; charset=utf-8`; any other path is 404.

A window is a Go duration or `<n>d`, measured back from now against the run's
`meta.json.startedAt` (a run without `meta.json` uses its batch manifest's
`createdAt`). A run that is still running shows as `running` and has no
verdict. JSON twins carry exactly the markdown's content as fields:
`runs` → `[{runId, batch, scenario, verdict, startedAt, durationSec, costUsd,
observerState, observation: {obsId, status, headline, findingTitles,
unverifiedQuotes}}]`; a run → `{runId, batch, scenario, verdict, startedAt,
durationSec, costUsd, candidateSha256, evaluatorSha256, observerState,
observation (the §7.5 document), olderObsIds, failedChecks: [{id, result,
expected, observed, source}], evidenceSteps}`; steps → `[{n, kind, tool,
input, result, isError, text}]`; findings → `[{owner, severity, title, what,
runId, steps, quotesVerified, quotesTotal, lookAt, fix}]`. `observerState` is
one of `observed`, `observing`, `not observed`, `observer off`, `observer
disabled`, decided in that precedence: a queued or running observation reads
`observing`; a run without `done.json` reads `not observed`; a run with an
observation reads `observed` whatever its manifest or the kill switch say;
only a finished run without one reads `observer disabled` or `observer off`.

### 8.5 Worker and actions

**FM-53.** One queue, at most three observations at a time, feeds both the
worker and the actions. Every 60 s the worker lists batches whose manifest
`createdAt` is within 14 days and whose `observer` names a model, and queues
each of their runs that has `done.json` and no observation, with the
manifest's model. With `ZCP_FARM_OBSERVER=off` it queues nothing and pages
and the API say `observer disabled`. Actions: `POST /r/<runId>/observe`
(model from the allowlist `claude-sonnet-5`, `claude-opus-5`,
`claude-fable-5-1`, else 400) queues a new version; `POST /b/<batch>/observe`
queues that batch's runs that have no observation, or all of them with
`all=1`. Actions work regardless of the manifest field and the kill switch.
A run already queued or running answers 409, and so does `all=1` while any
run of that batch is queued or running. An accepted action answers 303 back
to the page (cookie) or 202 (bearer). Without `CLAUDE_CODE_OAUTH_TOKEN` the
console still serves every page, the worker stays idle, and actions answer
503 `observer credential missing`; an unresolvable `claude` path is treated the
same way (503 `observer unavailable`). Without `all=1`, runs of the batch that
are already queued or running are skipped, not answered with 409. A job runs for the console's lifetime, not the
enqueuing request's (bounded by a 10-minute job timeout), and a job that fails
before an observation can be stored is logged to stderr. Worker and queue
state live in memory and are re-derived from the bucket after a restart.
