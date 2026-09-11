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
   `<evaluator> eval behavioral run --candidate <candidate> --candidate-sha256 <sha> --project-id $projectId --ack-disposable-project yes --capture raw --id <scenario> … --capture-dir <local capture dir> --work-dir /var/www`
   (`spec-testing-architecture.md §10.4`'s binding, unmodified) — the local
   capture dir is the one uploaded as `runs/<runId>/capture/`, and the
   evaluator opens its own capture window under it, named `capture-<id>/`
   (its own session id, distinct from `runId`); a run project executes
   exactly one scenario, so exactly one such window exists (§5.2 relies on
   this). `--work-dir /var/www` (D21) puts the agent's `claude` — and the
   MCP `zcp serve` child it spawns — at the same cwd a real container uses
   (`internal/ops/mount.go` `mountBase`), so `zerops_mount`'s mount root and
   the deploy preflight's expected source root agree; the evaluator's own
   scratch paths (results dir, private candidate bin, private Claude home)
   stay under `$RUNDIR`, siblings of the results dir rather than the work
   dir (`cmd/zcp/eval_behavioral.go` `buildExecutionBinding`);
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
`CLAUDE_CODE_OAUTH_TOKEN` (§2.4) — no repo checkout. Off the farm host, a session that also sets
`ZCP_FARM_PROJECT_ID` (the farm project) needs only `ZCP_FARM_ACCOUNT_TOKEN`
and `CLAUDE_CODE_OAUTH_TOKEN`: every other `ZCP_FARM_*` key it lacks resolves
once from the `farm` service's env in that project, a `${os_<key>}` value
following to the `os` service; the environment always wins and no value is
ever printed. Both services are found by the project-level direct read — the
service-stack search is scoped to the first org in the token's
`clientUserList`, and the account-wide token belongs to more than one org,
the farm project's not being the first. `farm run` resolves everything it needs (the
`--set gate`/`--set all` scenario id list, each scenario's front matter, and
the evaluator pin absent `--evaluator`, the wrapper pin absent `--wrapper`)
from the bucket (`sets/<digest>/gate.txt`, `scenarios/<digest>/…`,
`evaluators/current`, `farm/wrapper/current`), never from a relative path on
disk; only `farm push`, which runs from a full checkout on a dev machine,
reads `eval/farm/gate-set.txt` off disk, to upload it. The eval subsystem's
isolation (only `cmd/zcp/eval*.go` may import `internal/eval/**`, which in
turn never reaches `internal/tools`/`internal/server`/`internal/authoring`)
is pinned by the `.golangci.yaml` depguard rules `core-not-eval` and
`eval-no-upper-layers`, and enforced independently of depguard by
`internal/eval/architecture_test.go`.

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

The manifest also records what a person needs to read the batch later:
`note` (`farm run --note <text>`, at most 200 chars: why the batch ran),
`runBudgetSec` (the run budget in seconds, so a reader can tell a stalled run
from a running one, §8.8), and `candidateInfo` — `{revision, modified, time,
goVersion}` read from the candidate binary's embedded Go build info
(`debug/buildinfo`) by `farm push --candidate`, which stores it next to the
binary as `candidates/<sha256>.info.json`; `farm run` copies it into the
manifest when present. `farm push` writes the info file only when the
binary carries `vcs.revision`; without it the batch has no `candidateInfo`
and readers show the sha. None of these fields reaches a
run project.

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
overwrites an existing observation key. The observer writes format
`zcp-farm-observation-2`; every reader also reads format 1 (below).

```json
{
  "formatVersion": "zcp-farm-observation-2",
  "runId": "…", "obsId": "…", "model": "claude-sonnet-5",
  "source": "worker|action|local",
  "createdAt": "<RFC3339>", "durationMs": 0, "costUsd": 0.0,
  "promptSha256": "…", "digestSha256": "…",
  "status": "ok|unparsed|error",
  "errorKind": "credential|timeout|killed|bundle|model|other",
  "error": "…", "raw": "…",
  "warnings": ["…"],
  "outcome": "ok|problem|inconclusive",
  "headline": "…",
  "story": {
    "task": "…", "expected": "…", "did": "…",
    "stuck": {"from": 12, "to": 54, "what": "…"},
    "ending": "finished|gave-up|session-limit|turn-limit|timeout|crashed"
  },
  "goal": {"reached": "yes|partly|no", "why": "…"},
  "checks": {
    "verdict": "passed|failed|blocked", "agree": true,
    "judged": [{"id": "<check id>", "correct": false, "why": "…"}]
  },
  "findings": [{
    "severity": "high|medium|low",
    "owner": "zcp-guidance|zcp-tool|platform|agent|scenario|evaluator",
    "surface": "tool:zerops_deploy",
    "anchor": "…",
    "title": "…", "what": "…",
    "evidence": [{"step": 18, "quote": "…", "verified": true}],
    "span": {"from": 12, "to": 54},
    "causedVerdict": true,
    "lookAt": "…", "fix": "…"
  }],
  "selfReview": {"accurate": "yes|partly|no", "note": "…"}
}
```

**Value, not volume.** An observation either says the run was fine or names
what is wrong, where, and what to change. A finding exists only when a
maintainer should change something because of it: at most **three**
findings, most important first, one fix direction each. A clean run has no
findings and a headline starting `OK —`. Friction that cost the run nothing
is reported only when the same text or behavior would mislead another run.

Model-authored fields:
- `headline` — one sentence, at most 25 words, leading with the problem and
  the ZCP surface it sits in (`OK — …` for a clean run).
- `story` — the session in five short fields: `task` (what the user asked),
  `expected` (what a good run does, from the scenario and checks), `did`
  (what the agent actually did), `stuck` (only when the run got stuck: the
  step range and what blocked it; `null` otherwise), `ending` (why the
  session ended).
- `goal`, `selfReview` — as in format 1.
- `checks.judged` — one entry per failed or blocked check of the run, saying
  whether that check judged the run correctly.
- A finding's `surface` names the part of ZCP (or of the farm) the problem
  sits in, as `<kind>:<name>`: `tool:<zerops tool>` or
  `tool:<zerops tool>/<action or step>`, `recipe:<slug>`, `check:<check id>`,
  or the bare kinds `scenario`, `platform`, `agent` (a mistake no ZCP change
  would have prevented). An `agent`-owned finding that ZCP could have
  prevented names that ZCP surface, not `agent`.
- A finding's `anchor` is the shortest verbatim ZCP text or error code a
  maintainer would search for (at most 160 chars), or empty when no ZCP text
  is involved. It is the part that would read the same on the next run: a
  run-specific path, hostname or id belongs outside it, and the console
  masks what slips through (§8.6).
- `span` — the steps the problem stretched over, when more than the cited
  ones; `causedVerdict` — true on the finding that explains a failed check
  or a missed goal.

**Derived, never model-authored:**
- `outcome` — `inconclusive` when `story.ending` is `session-limit`,
  `turn-limit`, `timeout` or `crashed` (the run could not show whether ZCP
  works); else `problem` when there is at least one finding; else `ok`.
- `checks.verdict` — the run's verdict as the farm reports it: the `result`
  of the run's row in `batches/<batch>/summary.json` when that summary
  exists, else `meta.json.task.result` (spec-testing-architecture §10.1).
  The local verb reads `<run-dir>/../summary.json` when present (a
  `farm pull --batch` layout), else `meta.json`. A run without `done.json`
  has no verdict and is never observed.
- `checks.agree` — false when any `judged` entry has `correct: false` or any
  finding is owned by `evaluator`; true otherwise.
- `source` — `worker` (§8.5 automatic), `action` (a console action) or
  `local` (`zcp eval farm observe`).
- `errorKind` — set with `status: "error"`: `credential` (OAuth token
  missing/rejected, or `ANTHROPIC_API_KEY` present), `timeout`, `killed`
  (the process died on a signal), `bundle` (a required bundle file is
  missing or unreadable), `model` (the call returned no usable answer),
  `other`.

**Parse, validate, repair.** The model's answer is the first top-level JSON
object in its final text. `status` is `unparsed` (`raw` keeps the answer,
capped at 20,000 chars) when it does not parse or breaks the structure:
unknown enum values (except a finding's `owner`, repaired below), a finding
without title or evidence, `goal.reached` missing, `story` or
`story.ending` missing. Everything else is repaired deterministically and each repair
appends one plain sentence to `warnings` (shown wherever the observation is
shown): a finding whose `owner` is a surface kind takes the owner that kind
implies (`recipe` → `zcp-guidance`, `tool` → `zcp-tool`, `check` →
`evaluator`) and a finding with any other unknown owner is dropped; findings beyond three are dropped; a `surface` not of the form
above, naming a `tool:` the run never called, or a `check:` id not in the
run's checks, is cleared; an `anchor` that does not occur (FM-46
normalization) in any step the finding cites is cleared; a `span` outside
the run or with `from > to` is dropped; a `story.stuck` written as a sentence
keeps its text as `what`, a leading `Steps a-b` becoming the range, and a
stuck range outside the run is dropped; a headline starting `OK` on any
outcome that is not `ok` has that prefix stripped (warn); a `judged` id that is not a failed
or blocked check of the run is dropped; a headline over 30 words is kept
and warned about; a failed run where no finding has `causedVerdict` and
every `judged` entry is `correct: true` is warned about.

**Format 1** (`zcp-farm-observation-1`) documents stay valid and are read as
they are: no `story`, `surface`, `anchor`, `span`, `causedVerdict`,
`judged`; `outcome` derived by the same rule with `ending` unknown (so `ok`
or `problem`); `checks.agree` and `checks.why` as stored.

**FM-46. Quote check.** An evidence entry is `verified` iff its `quote` is a
substring of the cited step's full, untruncated text (a `tool` step's text is
its input JSON plus its result, §7.3) once both are normalized: JSON string
escape sequences (`\"`, `\\`, `\/`, `\b`, `\f`, `\n`, `\r`, `\t`, `\uXXXX`) are
decoded — ZCP's tool results are JSON text, so the agent saw `\"x\"` and
`\u003c` where the model quotes `"x"` and `<` — and every whitespace run is
collapsed to one space; the plain collapsed comparison counts as well. As a
third normalization, tried after both of the above, every backtick and
asterisk is dropped from both sides: a step can carry Markdown source (a
guidance sentence quoting `` Do **NOT** `override` ``) while the model
quotes its rendered plain reading (`Do NOT override`). An empty or
whitespace-only quote is never verified — it is trivially a substring of
any text. Step 0 names the digest's `CHECKS` section: the prompt tells the
model to cite a deterministic check as step 0, and a step-0 quote is
verified against the rendered `CHECKS` text. Any other step number outside
the run is unverified. Every surface that shows an observation shows its
unverified-quote count.

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
alone: a run without `done.json` is `running` until its batch's `summary.json`
settles it, and from then on shows that summary result (e.g. `blocked` for a
run that never produced a bundle); an unsettled run reads `stalled` once
`manifest.createdAt + runBudgetSec + 30 min` has passed (a manifest without
`runBudgetSec` never shows `stalled`). `deploy.sh` is
idempotent: it imports the service when missing (generating the console token
and writing the operator copy, §2.4) — when the service exists but the
operator copy has no token it exits 1 naming that file, never writing a
tokenless copy — pushes with `ZEROPS_TOKEN` and a private
`ZEROPS_CLI_DATA_FILE_PATH` in its own temp dir outside the pushed directory
(never the operator's own zcli login, never uploaded), enables
subdomain access explicitly after the deploy (the import flag alone does not
route a service imported without code), pins the service's scaling on
every deploy — exactly one container, since the observation queue, worker
and caches live in one process (§8.5), and a 2 GB RAM floor with 1 GB kept
free, since three concurrent observer processes outrun vertical autoscaling
from the platform's 0.125 GB default floor (live: an observer's `claude` was
OOM-killed) — and prints the URL. No secret value is ever printed.

### 8.2 Authentication and headers

**FM-50.** Every route except `GET /login`, `POST /login`, `GET /healthz` and
`GET /static/app.css` (the one stylesheet) requires `Authorization: Bearer <console token>` or a session
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
`Referrer-Policy: same-origin`, `Cache-Control: no-store`. `no-referrer`
would suppress the Origin header too on a navigate-mode form POST in some
browsers (Origin: null), which the strict Origin rule above then refuses —
`same-origin` keeps the console's own referrer private from every other
origin without breaking its own state-changing forms. Pages use no
script, no inline style and no external asset; they render in light and dark
(`prefers-color-scheme`) and at 400 px.

### 8.3 Pages

**FM-51.** The console answers three questions, in this order: *what is
broken in ZCP right now*, *what did this batch find*, *what happened in this
run*. Every page shows the value first and the record last, speaks the
vocabulary of §8.8, and never shows a raw enum, a Go error chain or an empty
placeholder.

Every page carries the top navigation (Overview · Problems · Findings ·
Terms, the current page marked with `aria-current`), a sign-out control, and
the **observer status line**: `Automatic assessment: on · <model> · <n>
queued/running` / `off on this console (ZCP_FARM_OBSERVER=off) — Assess
buttons still work` / `unavailable — <reason>` (credential missing,
`ANTHROPIC_API_KEY` set, `claude` not found), in which last case every
Assess form is hidden. A `?notice=<code>` from an action (§8.5) renders as
one callout above the content.

- `/` — **Overview**, top to bottom:
  1. **Latest evaluation**: the newest batch whose set is `gate` or `all`
     with at least one finished run (else the newest batch with a finished
     run): id, time, ZCP build (§8.8), verdict counts with disputed verdicts
     counted (`2 failed (1 disputed)`), and **vs <previous batch of the same
     set>**: newly failing, fixed, still failing — by scenario name. Below,
     one line per failed or blocked run: scenario, headline, and
     `observer disputes: <why>` when the checks were judged wrong.
  2. **Top problems now**: the first five live problems (§8.6), each one
     line (severity, cause, surface, title, how often), linking to
     `/problems`.
  3. **Batches**: a sortable, filterable table (§8.7): batch, started, ZCP
     build, set, one dot per run ordered as on the batch page (tooltip
     `scenario — verdict`), verdict counts, ZCP findings high/medium, agent
     cost (`—` when unknown, `$2.19 + 7 unknown`), assessed n/m. Batches
     where no run finished (empty batches) are listed only under `kind=empty` or `kind=all`.
- `/problems` — §8.6, as a table (§8.7). A row expands (`<details>`) to its
  member findings: run, batch, build, severity, cause, title, first found
  quote with its step link.
- `/findings` — every finding in scope, one card each, as a list (§8.7).
- `/b/<batch>`, top to bottom:
  1. Header: id, time, set, ZCP build, note, and the **vs <previous batch of
     the same set>** line.
  2. Summary: verdict counts, disputed verdicts, goal reached yes/partly/no,
     assessment outcome ok/problem/inconclusive, findings high/medium per
     cause class, agent cost (with the count of runs whose cost is
     unknown), assessed n/m.
  3. **Problems in this batch**: §8.6 over this batch's runs only, each
     marked `also in <previous batch>` / `not in <previous batch>` (the
     previous batch of the same set; the status words stay §8.6's). Without
     any assessment it falls back to "checks that failed in two or more
     runs".
  4. Runs, as a table (§8.7), in groups by precedence — a run sits in the
     first group it fits: **Failed and blocked** (one line of why: the first
     failed check in plain words `id: expected …, got …`, or the blocked
     reason, then the headline) · **Not finished** (running, stalled, not
     started — with the reason) · **Not assessed** (no current `ok`
     observation) · **Problems in passed runs** (outcome `problem` or
     `inconclusive`) · **Clean** (collapsed to one line of names). `sort`
     orders runs within each group.
  5. An Assess callout only while some finished run needs an assessment
     (§8.5); "Re-assess all" only once at least one run is assessed; neither
     when no run finished.
- `/r/<runId>`, top to bottom:
  1. Scenario, verdict (with its reason when blocked, not started, running
     or stalled), times, agent cost, step count.
  2. **Why this verdict**: `Failed because: <check id> — expected …, got …`
     per failed or blocked check (each linking to the checks table), or
     `All checks passed.`
  3. **Assessment** card: outcome, headline; when the checks were judged
     wrong, a full-width line `The observer thinks a check is wrong — this
     verdict may not be fair: <why>` linking to the explaining finding (the
     first `evaluator` finding), or to the judged check's row when there is
     none; the
     story (task, expected, did, stuck with step links, ending); goal
     reached; verdict right (per judged check); a line of findings by
     severity and cause linking to `#f<n>`; model, time, quote count and
     `warnings`; the live status line of §8.5 while one is queued or running;
     the assess form (model preselected to the current observation's
     model); earlier assessments, each with its outcome, headline or
     failure and a link to `?obs=<obsId>`, which renders that version in
     the card. Without an observation the card says why (§8.8) and shows
     **How the run ended**: the last agent message (first 300 chars) and
     the tool errors with step links.
  4. Findings F1…Fn (`id="f<n>"`): severity, cause, surface, title, what,
     evidence (step link, `quote found`/`quote not found`), span, where to
     look (`anchor` in monospace first), fix.
  5. Failed and blocked checks: id, expected, observed, source, and the
     observer's judgement of that check.
  6. Steps, filterable (`steps=all|cited|errors`, default `all`): every step
     a finding cites renders open, with `Cited by F<n> — <title>: "<quote>"
     ↩ F<n>` at the top of its body; tool results with JSON escapes decoded;
     empty thinking blocks left out (numbering stays the record's, §7.2).
  7. Record, collapsed: self-review with the observer's note on it, task
     prompt, run metadata (run id, candidate and evaluator sha, forensic
     command `zcp eval farm pull <runId> --out <dir>` then
     `zcp capture ui <dir>/<runId>/capture`). A run without `done.json`
     shows no Record.
- `/terms` — the glossary of §8.8.
- `/login?next=<path>` — the login form (it says the token is in
  `farm-console.env`, written by `deploy.sh`); after login the browser goes
  to `next` when it is a path starting with `/` and not `//`, else `/`. An
  unauthenticated HTML request is sent to `/login?next=<its path>`.
  `POST /logout` clears the session cookie.

### 8.4 Agent API

**FM-52.** Same authentication. Markdown endpoints, each with a JSON twin at
the same path ending `.json` carrying the same content as fields. Every list
endpoint takes exactly the filter and sort parameters of its page (§8.7).

- `GET /api/digest.md?batch=<id>` or `?since=<window>` — one call, at most
  8 KB, for "what did the farm find": the scope header (batches, builds,
  verdict counts, cost), the problems of §8.6 ranked (severity, cause,
  surface, title, anchor, runs hit, status, where to look, fix, one run link
  per problem with its finding anchor), failed and blocked runs with their
  failed checks and headline, and the count of finished runs not yet
  assessed. Truncation is said, never silent.
- `GET /api/problems.md` — §8.6 with its members (each with its scenario).
- `GET /api/batches.md` — the Overview's batches table.
- `GET /api/runs.md?since=<window>` or `?batch=<id>` — newest first, grouped
  under batch headers: run id, scenario, verdict (+ reason), outcome,
  findings high/medium per cause class, failed check ids, headline.
- `GET /api/runs/<runId>.md` — header (with step count), the current
  observation in full (story, findings with surface/anchor/span, judged
  checks, warnings), failed and blocked checks, the steps its evidence cites,
  links to the task prompt and self-review.
- `GET /api/runs/<runId>/steps.md?from=<n>&to=<m>` or `?n=<step>` — those
  steps; a tool result over 2,000 chars is cut (said) unless `full=1`; empty
  thinking blocks are left out.
- `GET /api/runs/<runId>/self-review.md`
- `GET /api/findings.md` — every finding in scope: batch, scenario, build,
  run id, started, severity, cause, surface, anchor, title, what, steps,
  quotes found n/m, where to look, fix.
- `GET /api/runs/<runId>/files/<path>` — one bundle file under `results/`,
  served as `text/plain; charset=utf-8`; any other path is 404.
- `GET /api/runs/<runId>/observations/<obsId>.md` — one stored observation.

`digest.md` takes only `batch` or `since` and refuses anything else like a
list (§8.7). Findings and problem members carry `causeClass` — the value
`cause=` takes — beside the cause label, and a run's `outcome` reads `none`
when it has no current `ok` observation, in JSON as in markdown.

A window is a Go duration or `<n>d`, measured back from now against the
run's `meta.json.startedAt` (a run without `meta.json` uses its batch
manifest's `createdAt`). Step lists are sorted and de-duplicated. Hashes
appear once per run, never per line. Every markdown list starts with a one-
line legend of the terms it uses.

`observerState` (JSON) stays one of `observed`, `observing`, `not observed`,
`observer off`, `observer disabled` in the precedence of format 1, plus
`assessment failed` for a run whose current observation's status is `error`
or `unparsed`; `observerStateText` carries the §8.8 wording.

A batch or run whose manifest/observation/meta cannot be read is skipped
(logged to stderr) rather than failing the whole listing; only a genuine
store error on the batches/ listing itself fails the call. The caching rules
of format 1 stand: a finished run's row is cached once resolved (the current
observation re-read at most every 2 minutes, or at once when the console's
own queue finishes a job for that run); a run without `done.json` and a
missing `summary.json` are re-checked at most every 15 seconds; a cold fill
resolves at most 8 rows at a time in sequential order.

### 8.5 Worker and actions

**FM-53.** One queue, at most three observations at a time, feeds both the
worker and the actions. Every 60 s the worker lists batches whose manifest
`createdAt` is within 14 days plus a 2-hour slack and whose `observer` names
a model, and queues each of their runs that has `done.json` and no
observation, with the manifest's model (`source: worker`). With
`ZCP_FARM_OBSERVER=off` it queues nothing.

A run **needs an assessment** when it has `done.json`, is not queued or
running, and either has no observation or its current one failed (`error`
or `unparsed`). The batch callout counts exactly those runs, and
`POST /b/<batch>/observe` without `all=1` queues exactly those.

Actions (`source: action`): `POST /r/<runId>/observe` queues a new version
with `model` from the allowlist `claude-sonnet-5`, `claude-opus-5`,
`claude-fable-5-1` — absent `model`, the current observation's model, else
the manifest's, else `claude-sonnet-5`; a value off the allowlist is 400.
`POST /b/<batch>/observe` queues the runs that need an assessment, or every
finished run with `all=1`. Actions work regardless of the manifest field and
the kill switch. A run without `done.json` is never queued (`/r/` answers
409 `run not finished`; the batch action skips it). A run already queued or
running answers 409 on `/r/`, and `all=1` answers 409 while any run of the
batch is queued or running; without `all=1` such runs are skipped.

Answers: a bearer request gets 202 with a JSON body `{queued: [runIds],
skipped: [{runId, reason}]}` (409/400/503 as above, with a one-line text
body); a cookie request is always sent back to the page it came from with
`?notice=<code>` — `queued` (with `n`), `busy`, `not-finished`,
`bad-model`, `unavailable` — rendered per §8.3.

The queue exposes each job's model, source, enqueued and started time, and
keeps the last failure that happened before anything was stored, per run
(error and time), until the next job for that run starts. The run card and
the batch row show a queued or running job as `Assessing with <model> —
started <time>, usually 1–2 min; the result replaces the one below` (the
form hidden meanwhile), and a pre-store failure as `The attempt at <time>
failed before anything was stored: <error>. Re-assess to retry.` While any
job of the page is in flight, the page carries `<meta http-equiv="refresh"
content="20">`.

Without `CLAUDE_CODE_OAUTH_TOKEN`, with `ANTHROPIC_API_KEY` set, or with an
unresolvable `claude` path, the console serves every page, the worker stays
idle, actions answer 503, and the status line (§8.3) says why. A job runs for
the console's lifetime, not the enqueuing request's (bounded by a 10-minute
job timeout). Worker and queue state live in memory and are re-derived from
the bucket after a restart. Both the worker and `POST /b/<batch>/observe`
check a run's live queue state before listing its stored observations, and
skip the run (never enqueue) when that list call itself fails.

### 8.6 Problems — findings clustered across runs

**FM-54.** A **problem** is every finding, over the current observations
(status `ok`) of the runs in the `since` window, that shares one key:
- a format-2 finding with an `anchor`: its surface up to the first `/`
  (`tool:zerops_deploy/deploy` → `tool:zerops_deploy`) + `norm(anchor)`;
- a format-2 finding without an anchor whose surface is `tool:…`,
  `recipe:…`, `check:…` or `scenario`: its surface up to the first `/` +
  owner + scenario;
- a format-2 finding whose surface is `agent`, `platform` or empty, and
  every format-1 finding: a problem of its own.

`norm` applies the FM-46 normalization (`observer.NormalizeText`),
lowercases, then replaces, in this order and each as whole tokens only (a
token is bounded by a character outside `[a-z0-9]` or the text's edge):
the run's service hostnames — from its `platform-snapshot.json` when
present plus every hostname its `verification.json` rows name as scope —
with `<host>`; 22-character base62 tokens and hex tokens of 8 or more
characters with `#`; then every run of digits with `#`. Clustering is
deterministic and runs in the read path; no model is called.

**Builds and status.** A build is a candidate sha256 (§8.8). A scenario is
*assessed on a build* when one of its runs on that build has a current `ok`
observation. Status is computed once over the `since` window across all
batches; every other filter narrows rows and never changes a status. The
**newest build** is that of the newest `gate`/`all` batch with an assessed
run, else of the newest batch with one. For a problem:
- `recurring` — hit on the newest build and on an older one;
- `new` — hit on the newest build only, and one of its scenarios was
  assessed on an older build without hitting it (a regression);
- `first seen` — hit on the newest build only, and none of its scenarios was
  assessed on an older build;
- `gone` — not hit on the newest build although one of its scenarios was
  assessed there in the same observation format as the problem's members,
  and hit on an older build (fixed, or not reproduced);
- `unconfirmed` — not hit on the newest build and none of its scenarios was
  assessed there in that format.
`live` = `recurring`, `new` or `first seen`.

**Rank:** live before the rest; then highest member severity; then runs hit
on the newest build; then runs hit in total; then last seen, newest first;
then the key. A row shows: highest severity, the cause labels of its
members, surface, the title and fix of its most severe member (newest on a
tie), the anchor, `hit <a>/<b> runs on <newest build>` (`b` = runs of its
scenarios assessed on that build), `<n> runs · <m> batches · <k> builds`,
status, first and last seen. Members are ordered by severity, then newest.

### 8.7 Lists — sorting and filtering

**FM-55.** Every list (Overview batches, `/problems`, `/findings`, a batch's
runs, a run's steps) and its API twin take a closed set of query
parameters. The state lives in the URL; every link on the page keeps the
other parameters.
- A filter with a closed value set takes one or more comma-separated values
  (OR within, AND across filters) — except `severity`, which is one minimum
  everywhere, and `kind` and `status`, which are switches taking one value
  (their `all`/`live` options already combine the others), so an option's
  count is the number of rows its link shows. `cause` takes `zcp|test|agent|platform` everywhere and matches
  an item when any of its findings is in that class. `verdict` takes
  `passed|failed|blocked|not-started|running|stalled`. A filter over open
  values (batch, scenario, build, surface) is matched exactly;
  `surface=tool:*` style prefixes match a kind; an open value matching
  nothing yields an empty list, not an error.
- Each filter option shows its count under the other active filters; an
  option with count 0 is shown but not a link. Active filters show as chips,
  each removable, plus "clear all".
- Sorting: `sort=<key>` from the list's set and `dir=asc|desc`; each key's
  default direction and tie-break are in the table; an unknown value (cost
  not recorded, duration of a run without `done.json`) sorts last in both
  directions.
- A parameter the list does not take, or a closed-set value outside its
  set, is refused: the page renders a 400 that names the parameter and its
  allowed values with a link that drops it; the API answers 400
  `{error, allowed}`. Nothing is ignored silently. `notice` and `n` (§8.5)
  are accepted on every page and never carried into links; `/r/` also takes
  `obs` (§8.3).

| list | filters | sort keys — default direction; tie-break |
|---|---|---|
| Overview batches | `kind=evaluation\|empty\|all` (default `evaluation`), `since` | **`newest`** desc; batch id · `zcp` (ZCP high, then ZCP medium) desc; newest · `failed` (failed + blocked runs) desc; newest · `cost` desc; newest |
| `/problems` | `cause`, `severity`, `status=live\|recurring\|new\|first-seen\|gone\|unconfirmed\|all` (default `live`), `surface`, `scenario`, `batch`, `build`, `since` (default `30d`) | **`rank`** (§8.6); key · `severity` desc; rank · `runs` (runs hit in total) desc; rank · `last` desc; rank · `first` desc; rank |
| `/findings` | `cause`, `severity`, `surface`, `scenario`, `batch`, `build`, `since` (default `7d`) | **`severity`** desc; newest, then run id, then finding index · `newest` desc; severity · `cause` (ZCP-first order) asc; severity |
| batch runs | `verdict`, `outcome=ok\|problem\|inconclusive\|none`, `cause` | **`problem`** (verdict rank, disputed, highest severity, finding count) desc; scenario · `scenario` asc; — · `duration` desc; scenario · `cost` desc; scenario |
| `/api/runs.md` | `batch`, `since`, `verdict`, `outcome`, `cause` | **`newest`** desc; run id |
| run steps | `steps=all\|cited\|errors` (default `all`) | record order |

`TestLists_*` pins, for every list and parameter, that each value narrows the
result exactly as defined, that each sort key orders as defined (including
direction, tie-break and unknown values last), that counts match, and that an
unknown parameter or value is refused while `notice`/`n` (and `obs` on
`/r/`) are accepted.

### 8.8 Vocabulary

**FM-56.** One word per concept, the same on every page, in every markdown
endpoint's legend, on `/terms`, and in the farm-triage skill; each badge
carries its definition as a `title`.

- **Batch** — one farm run: a set of scenarios against one ZCP build.
  **Evaluation batch** — at least one run finished. **Empty batch** — no run
  finished (setup failures, aborted, stalled).
  **Run** — one scenario done once by an agent in a fresh project.
  **Scenario** — a scripted user task plus the automatic checks that grade it.
- **ZCP build** — the candidate binary, identified by its sha256; shown as
  its git commit (12 chars, `+ modified` when built from a dirty tree) when
  the manifest records it (§3.3), else `build <sha256[:12]>`. The label is
  display only; two binaries are two builds even at one commit, and the
  `build` filter takes `sha256[:12]`.
- **Verdict** — the automatic checks' result, never the observer's:
  passed (every check held) · failed (a check proved the run wrong) ·
  blocked (could not be graded — reason shown) · not started · running ·
  stalled (no result after the batch's deadline, §3.3 `runBudgetSec`, plus
  30 minutes).
- **Check** — one automatic test: expected, observed, where the observed
  value came from.
- **Observer** — an AI model that reads a finished run and writes an
  **assessment**; it never changes the verdict. Assessment outcome: OK ·
  Problem · Inconclusive · none (no current `ok` observation). Assessment state: assessed · assessing… · not
  assessed — run not finished / batch ran without observer / automatic
  assessment is off on this console / older than 14 days (assess by hand) ·
  assessment failed — <reason>.
- **Goal reached** — did the user get what they asked for, whatever the
  checks say. **Verdict right** — did the checks judge correctly.
  **Disputed** — the current observation has `checks.agree: false`: a check
  judged wrong, or a check the observer says is missing; counted under the
  verdict it disputes, passed included.
  **Self-review honest** — does the agent's after-run summary match the
  record.
- **Finding** — one problem in one run, with quotes, where to look and a fix.
  **Problem** — the same finding across runs (§8.6). **Problem status**:
  new · first seen · recurring · gone · unconfirmed; **live** = new, first
  seen or recurring.
- **Severity** — high: the goal was missed, something was destroyed, or (for
  a test cause) the verdict is wrong · medium: it cost many steps or much
  time · low: ZCP text or behavior that is wrong but cost this run nothing.
- **Cause** (the finding's owner): ZCP guidance · ZCP tool · Zerops
  platform · Agent mistake · Test scenario · Test check. **Cause class**:
  ZCP (guidance, tool) · Test (scenario, check) · Agent · Platform. Lists
  order causes ZCP first.
- **Surface** — the part of ZCP (or the farm) a finding sits in (§7.5).
  **Anchor** — the exact ZCP text or error code to search for.
- **Quote found** — the quoted words occur in the cited step; it does not
  prove the finding right.
- **Agent cost** — the run's model spend, without the observer; `—` when
  not recorded.
