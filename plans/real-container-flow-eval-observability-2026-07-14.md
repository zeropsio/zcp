# Real container flow-eval with Capture Inspector

Date: 2026-07-14

Status: execution plan, not yet run

Target branch at plan creation: `feat/capture-raw-prototype` / `b5bd502a`

## 1. Objective

Run one real behavioral flow-eval on the Zerops `zcp` service with the exact
candidate binary from this branch while preserving enough independent evidence
to answer all of the following:

1. What prompt and context did the model receive at each turn?
2. Which model decisions, tool proposals, built-in actions, MCP calls, results,
   retries, and phase transitions actually occurred?
3. What did ZCP return and which guidance/source component supplied it?
4. Did Claude Code preserve or rewrite the MCP result before the next provider
   request?
5. What was the authoritative Zerops platform state before eval cleanup?
6. Did the scenario reach its semantic intent, independently of process exit and
   the agent's self-report?
7. Is each observed problem attributable to the eval fixture/harness, ZCP
   guidance, ZCP handler/workflow behavior, Claude Code transport, the model's
   reasoning, the Zerops platform, or the Inspector projection?
8. Can the operator inspect the run in the browser during capture and inspect the
   complete causal story after finalization?

The execution must produce a loopback URL for the operator. The URL is served
through an SSH tunnel during the remote run and a local Inspector after the
finalized bundle is copied and validated.

## 2. Verified starting state

The following was read without mutation while preparing this plan:

- The MCP connection is scoped to project `eval-new`.
- That project is ACTIVE and currently contains only the ACTIVE `zcp` service.
- The SSH alias `zcp` enters `/var/www` as user `zerops` in that same project and
  service.
- The injected remote `projectId` and `serviceId` match the MCP-discovered
  project/service identities.
- No eval, capture, or headless Claude process was active at the probe time.
- The container had approximately 98 GiB free on both `$HOME` and
  `/var/www`; both currently resolve to the same 100 GiB filesystem.
- Remote Claude Code reported `2.1.207`.
- The canonical remote `zcp` resolved through `~/.local/bin/zcp` and reported
  `v9.125.3-3-g0754c501`; it does not contain the current capture implementation.
- The current branch contains the finalized Capture Inspector checkpoint through
  `c07fb09d`, followed only by the technical handoff commit `b5bd502a`.

The current `eval/behavioral/flow-eval.sh` cannot be used unchanged for this
run because it:

- calls `eval/scripts/build-deploy.sh`, which installs over
  `/usr/local/bin/zcp`;
- invokes the remote command as bare `zcp` even when a different
  `EVAL_REMOTE_BIN` was deployed;
- does not pass `--capture raw`;
- pulls eval results but not the canonical capture bundle;
- does not create or tunnel a live Inspector.

The existing Go runner itself already supports the required core operation:

```text
<current-executable> eval behavioral run ... --capture raw
```

The capture path is printed immediately, the runner's strict MCP config points
Claude at `os.Executable()` (therefore the same temporary candidate binary), and
the complete manifest is finalized when the eval process exits.

## 3. Scope decisions

### 3.1 First scenario

Use `greenfield-node-postgres-dev-stage` for the first observed run unless the
operator chooses another scenario before execution.

Reasons this is the default candidate:

- no one-shot launch delegation or external GitHub credential;
- no hard-coded stale project UUID in its prompt;
- broad path: recipe routing, bootstrap, Node + Postgres, dev first deploy,
  verify, stage promotion, and retrospective;
- it has a prior capture on this branch, allowing a factual before/after
  comparison without treating the old run as a golden verdict;
- expected duration from the prior run is approximately ten minutes.

Operational budget for this scenario is 10–20 minutes. The behavioral README's
historical provider estimate is approximately USD 0.65–1.45; actual cost depends
on model turns and current provider pricing. The prior canonical bundle was
about 12 MiB. These are estimates, not gates or outcome claims.

Do not use `behavioral all` for the first run. Do not use any launch-production,
one-shot delegation, or credential-bearing scenario without a separate explicit
selection and credential/cleanup review.

### 3.2 Candidate binary

Upload the candidate to a commit-addressed temporary path, for example:

```text
/tmp/zcp-observed-<12-char-commit>
```

Do not replace `/usr/local/bin/zcp`, do not alter the `~/.local/bin/zcp`
symlink, and do not run a release/install target. Running the temporary
executable still makes the spawned capture MCP server use that exact binary.

### 3.3 Browser exposure

Do not expose Inspector on a public Zerops subdomain. Keep its server bound to
remote `127.0.0.1` and forward the same port to local `127.0.0.1` over SSH. This
preserves the Inspector's loopback, Host, Origin, capability, reveal, and CSP
contracts.

### 3.4 Live versus finalized evidence

The current running-capture projection intentionally exposes only durable
structural prefixes. It polls every two seconds, but it does not construct the
full Session Story, context lineage, or causal tools view until the manifest is
finalized.

Therefore:

- **during the run:** show RUNNING integrity state, raw/provider/lifecycle/MCP
  prefix growth, plus the separately tailed eval process log;
- **after finalization and refresh:** show complete Cards/Flow/Split, context,
  tools, MCP, metrics, artifacts, and raw evidence.

The plan does not claim real-time full causal reconstruction.

## 4. Required evidence-harness closure before the run

The agent transcript and its own `zerops_verify` calls are not an independent
proof of final platform state. Behavioral cleanup deletes all non-`zcp`
services immediately after retrospective/verification, so an operator cannot
reliably query final state afterward.

Before the first run, add one eval-only, read-only artifact:

```text
platform-snapshot.json
```

It is captured after retrospective and before `CleanupProject`.

### 4.1 Snapshot contract

Use an explicit versioned DTO, not raw SDK structs. The artifact should contain:

```text
formatVersion
observedAt
project identity
scenario start time
services[]:
  service ID, hostname, type, status, subdomain-enabled flag
processes[] created during this scenario:
  process ID, action, status, service references, created/started/finished,
  app-version phase, failure reason when present
diagnostics[]:
  explicit query/parse failure; never an invented empty state
verification findings
```

Data sources:

- `ListServicesDirect` for lag-free authoritative service state;
- `GetProjectProcessesDirect` for direct process state/history;
- filter process history by scenario start for run-local findings;
- existing verification checks remain separate facts and may be included by
  reference or duplicated in a clearly named field.

Do not include environment values, API credentials, launch tokens, provider
credentials, arbitrary service DTO fields, or log bodies.

### 4.2 TDD and integration points

1. Add RED tests that prove:
   - successful checks still persist positive observed state;
   - direct service/process reads are used;
   - query failure is diagnostic/unknown rather than an empty project;
   - old/stale processes are distinguished from processes created during the
     scenario;
   - the JSON projection excludes env and credential fields;
   - `BundleEvalScenario` copies `platform-snapshot.json` unchanged;
   - snapshot happens before cleanup and is bundled on success, model spawn
     failure, session-ID extraction failure, retrospective failure, and
     seed/init/preseed failure;
   - cleanup still runs exactly once on every path where the existing runner is
     expected to clean.
2. Replace the existing verification path's `ListServices`/`SearchProcesses`
   evidence with `ListServicesDirect`/`GetProjectProcessesDirect`, filtered by
   run start. Prefer one collected direct observation from which both positive
   snapshot fields and platform verification findings are derived, so two reads
   cannot silently describe different moments. Retrospective phrase checks and
   any HTTP probe remain explicitly separate evidence. The current
   verification implementation is Elasticsearch-backed and cannot serve as
   immediate authoritative post-mutation evidence.
3. Introduce one centralized post-scenario finalizer that preserves this order
   on every return path:

```text
collect direct platform observation
→ derive and serialize verification findings
→ write platform snapshot including those findings
→ run post-scenario cleanup exactly once
→ deferred capture artifact bundling and scenario end
```

   Refactor the current explicit error-path cleanup calls rather than adding a
   success-only hook. A run that fails before retrospective still needs the
   last observable platform state before cleanup. Preserve the separate
   pre-scenario cleanup performed by seeding; “exactly once” here refers to the
   post-scenario cleanup boundary. Close the current seed/init/preseed early-exit
   cleanup gaps as part of the same tested finalizer.
4. Implement the eval-only snapshot writer in a cohesive `internal/eval` file.
5. Add `platform-snapshot.json` to
   `internal/capture/eval_bundle.go::evalArtifactNames`.
6. Update `eval/behavioral/README.md` artifact documentation.
7. Run focused eval/capture tests, then the repository short suite.

For a completed run, the read occurs after the evaluated model and retrospective
have finished. For an early failure, it occurs after the failure and before the
existing cleanup boundary. It must not change model prompt, model context, MCP
bytes, workflow state, or scenario verdict. Its only normal-path control-flow
effect is a bounded read-only evidence step before cleanup; failure is represented
in the artifact and must not erase or falsify the original run.

### 4.3 Scenario verification

`greenfield-node-postgres-dev-stage` currently has no `verification:` block.
The current canonical `nestjs-minimal.import.yml` selected by the prior recipe
path pins `appdev`, `appstage`, and `db`. Re-confirm those bytes before editing
the scenario, then add direct-read post-run assertions for:

- `appdev`: ACTIVE Node runtime;
- `appstage`: ACTIVE Node runtime;
- `db`: ACTIVE PostgreSQL;
- no process created during the run remains FAILED without an explicit expected
  failure.

Keep the markdown prompt body byte-identical. Verification runs after model and
retrospective completion and is not injected into model context. If the current
recipe no longer pins these names, do not force them through the current
exact-hostname verification schema: either extend that schema with a tested
semantic selector or select an existing scenario whose expected hostnames are
explicit.

## 5. Execution phases

### Phase A — freeze and verify the candidate

1. Confirm the branch and clean worktree.
2. Record:
   - full commit SHA;
   - `git describe --always --dirty`;
   - scenario file SHA-256;
   - Go version;
   - candidate build metadata.
3. Run at minimum:

```bash
go test ./internal/eval/... ./internal/capture/... ./internal/captureinspector/... ./cmd/zcp -count=1
go test ./... -short -count=1
go vet ./...
make lint-fast
```

4. Confirm the prior local greenfield capture exists before listing it as a
   comparison baseline; if absent, run without that optional comparison.
5. If Inspector assets changed, also run JS syntax and the tagged browser gate.
6. Abort before remote mutation if the worktree is dirty, tests fail, or the
   candidate does not report the frozen commit.

### Phase B — verify the destructive target boundary

Immediately before execution, use two independent reads:

1. Zerops MCP `discover` confirms project name `eval-new` and that only `zcp`
   exists.
2. Remote injected `projectId`, `serviceId`, and `hostname` match the discovered
   project and `zcp` service.

Also assert:

- no existing `zcp eval`, capture, or headless Claude process;
- no active capture manager under the candidate binary;
- no inherited environment key beginning `ZCP_CAPTURE_` in the launch shell;
  otherwise `--capture raw` attaches to that ambient window and does not emit
  the scoped manifest path this runbook requires;
- no non-system service that belongs to another user/run;
- sufficient disk space on the filesystem containing
  `$HOME/.local/state/zcp/captures` as well as `/var/www`;
- Claude Code is present and authenticated;
- required scenario environment keys are present by key, without printing
  values.

This is a hard gate because the behavioral runner's seed and cleanup are
destructive to every non-`zcp` service in the selected project.

### Phase C — build and upload without installation

1. Build Linux amd64 from the frozen clean commit.
2. Compute local SHA-256.
3. SCP to the temporary commit-addressed path.
4. Set mode `0755` on that temporary file only.
5. Compare remote SHA-256 byte-for-byte.
6. Run `<temp-binary> version` remotely and compare commit/build metadata.
7. Verify the canonical `/usr/local/bin/zcp` hash/path was not changed.

The run dossier records both local and remote candidate hashes.

### Phase D — stage an isolated scenario input

Copy the selected scenario directory to a run-addressed remote directory, for
example:

```text
/tmp/zcp-observed-<run-id>/scenarios/
```

Copy the whole scenario tree so referenced `fixtures/` and `preseed/` paths
remain valid. Compute and compare the selected scenario SHA. Remove prior eval
result directories only after the target boundary gate and before the new run,
matching the existing anti-contamination behavior in `flow-eval.sh`.

Do not copy prior self-reviews, transcripts, capture bundles, or observations
into `/var/www`, where the evaluated model could read them.

### Phase E — start the captured eval as a supervised background job

Run from `/var/www` with the temporary binary:

```text
<temp-binary> eval behavioral run
  --scenarios-dir <remote-run-scenarios>
  --id greenfield-node-postgres-dev-stage
  --capture raw
```

Use a run-specific remote log, PID file, and exit-status file. Start it with
`nohup`/a detached shell so an SSH transport drop does not kill the eval.
Preserve stdout and stderr byte order in one operator log. The eval convenience
wrapper has no `--output-dir` passthrough; this scoped capture therefore writes
to the candidate user's default `$HOME/.local/state/zcp/captures` root.

Parse, but do not guess, these emitted values:

- capture session ID;
- manifest path / capture directory;
- eval suite ID;
- eventual child and capture statuses.

If no manifest path appears within the startup deadline, stop and diagnose the
launch rather than selecting the newest directory heuristically.

### Phase F — start live Inspector and SSH tunnel

Once the running manifest exists:

1. Choose a port free on both local and remote loopback.
2. Start remotely:

```text
<temp-binary> capture ui <exact-capture-dir>
  --listen 127.0.0.1:<port>
  --no-open
```

3. Parse the emitted launch URL from that process only.
4. Start an SSH local forward using the same port:

```text
127.0.0.1:<port> → remote 127.0.0.1:<port>
```

5. Confirm tunnel readiness with an unauthenticated request that does not use
   the launch path and receives the expected unauthorized/not-found response.
   Do **not** request the launch URL during this probe because doing so would
   consume its one-time capability in the probe client.
6. Send the untouched one-time launch URL to the operator in chat. The operator's
   browser performs the capability exchange; plaintext details remain forbidden
   there until `REVEAL`. Never write the capability token into a committed file
   or run dossier.

The remote UI and tunnel PIDs are recorded outside the capture bundle for
bounded cleanup. The user can open the URL while the eval is running. After the
manifest finalizes, refreshing the same authenticated browser session should
replace the running-prefix projection with the complete view.

### Phase G — observe without perturbing the scenario

Permitted live observation:

- Inspector running-prefix pages;
- tail of the remote eval operator log;
- read-only process liveness/status;
- read-only Zerops discover/events/log queries when needed.

Rules:

- no manual deploy, cancel, service mutation, file edit, workflow call, or user
  message during the scenario;
- timestamp every operator-side read because it is outside the eval capture;
- do not present an Elasticsearch search absence as authoritative immediately
  after mutation;
- use direct reads for current state;
- if an operator-side read contributes to a finding, preserve its result in the
  run dossier rather than relying on memory;
- never infer causal order from wall clock when provider/MCP/lifecycle sequence
  evidence exists.

### Phase H — finalize and preserve evidence

Wait for the detached eval process to terminate naturally. Do not stop Inspector
first.

Record independently:

- eval process exit code;
- lifecycle scenario/eval status;
- capture terminal status;
- manifest completeness/integrity;
- cleanup outcome.

A non-zero eval result and a complete capture are compatible facts. Do not
convert one into the other.

Before deleting any remote file:

1. SCP the exact capture directory to:

```text
tmp/container-capture-runs/<scenario>-<suite>/capture/
```

2. SCP the eval result directory to the sibling `eval/` directory.
3. Preserve the remote operator log and a token-free run metadata file.
4. Compare remote/local manifest SHA-256.
5. Run the local candidate binary's `capture inspect` over the copied bundle.
6. Require explicit output for complete, partial, unclean, gap, unsupported
   framing, or hash failure.
7. Re-run the token-sentinel pattern defined inside
   `eval/behavioral/flow-eval.sh` over self-review and retrospective artifacts.
   It is a shell function, not a standalone executable, so reproduce the exact
   reviewed patterns in this orchestration rather than sourcing the wrapper
   (sourcing it would execute the wrapper).
8. Keep the copied directory private and gitignored.

Do not delete the remote bundle or temporary candidate until the local copy and
inspection both pass.

### Phase I — provide the finalized browser URL

Start the local candidate binary over the exact copied capture directory or its
private containing root:

```text
<local-candidate> capture ui <local-capture-dir> --no-open
```

Send the newly emitted one-time URL to the operator. This local final URL is the
primary review address; it does not depend on the remote tunnel. Keep the
process alive until the operator confirms review is complete.

The launch URL is intentionally single-use. Restart the local Inspector to issue
a new one if the cookie/browser session is lost.

## 6. Evidence review protocol

Review in this order to avoid treating the retrospective or prior hypotheses as
ground truth.

### 6.1 Integrity first

Confirm:

- manifest format, hashes, record sequences, and terminal records;
- no unexplained `capture.gap`;
- provider request/response and MCP whole-stream hashes;
- lifecycle binding completeness;
- no unattributed exchange that should belong to the selected invocation;
- platform snapshot and eval artifacts are inventoried.

If integrity is partial/invalid, every downstream conclusion is scoped to the
persisted prefix and the missing evidence is listed explicitly.

### 6.2 Reconstruct behavior without the retrospective

Use Session Story and Flow Map first:

1. Initial user prompt and phase boundary.
2. Model-visible system/guidance/tool-schema context.
3. Each model decision and proposed tool call.
4. Exact MCP arguments, result, error status, and propagation state.
5. Next provider context and whether the result was exact, different, missing,
   or ambiguous.
6. Built-in Bash/file/question actions from client transcript evidence.
7. Context growth, rewrite, reset, cache usage, compaction, and retries.
8. Final model claim.

For every apparent divergence, identify the earliest evidence-backed turn where
actual behavior departed from the expected contract. Later retries are symptoms
unless they establish a separate defect.

### 6.3 Compare against independent state

Compare the agent's claim and its own verify calls to:

- `platform-snapshot.json` direct service/process observations;
- scenario `verification.json`;
- captured platform logs or process failures where available;
- the exact scenario prompt/persona and seed/fixture declaration.

An empty finding list is not by itself proof unless the positive snapshot shows
what was observed.

### 6.4 Read retrospective last

Then read `self-review.md` and the retrospective stream. Treat them as evidence
of what the model reports remembering, not as authoritative history. Check every
material retrospective claim against provider/MCP/client/platform evidence.
Record compaction if it may have changed retrospective recall.

### 6.5 Compare to prior run

For `greenfield-node-postgres-dev-stage`, compare with the prior 2026-07-13
capture:

- tool set and call order;
- retries and error codes;
- context growth/reset/rewrite;
- result propagation;
- phase and wall-time differences;
- new or removed warnings;
- final platform snapshot/outcome.

The current Compare tab provides metric deltas only. Structural sequence
comparison is manual/evidence-referenced in this iteration. Do not synthesize a
single quality score.

## 7. Finding classification

Every finding receives exactly one primary current-owner hypothesis and any
secondary contributors. `unknown` remains valid when evidence cannot decide.

| Class | Required evidence |
|---|---|
| Capture/Inspector defect | Canonical raw evidence and derived projection disagree, or required evidence was lost/altered by capture. |
| Eval harness/fixture defect | Seeded platform state, copied scenario, user-sim behavior, cleanup, or verification differs from the scenario declaration. |
| ZCP guidance/content defect | The needed fact is absent, contradictory, mistimed, overly broad, or owned by the wrong atom/recipe/template in the exact model-visible context. |
| ZCP schema/handler defect | The model supplies contract-valid input but MCP returns wrong validation, mutation, state, result, or non-actionable error. |
| ZCP workflow/state defect | Envelope/session/phase/route state contradicts specs or live platform state. |
| Claude Code client/transport defect | Raw MCP output differs from provider-visible result or context due to client transformation, truncation, omission, or identity drift. |
| LLM decision defect | Correct, relevant guidance/result was present before the decision, but the model ignored or misapplied it and the tool contract itself behaved correctly. |
| Zerops platform defect | Direct API/process/log evidence shows platform behavior inconsistent with the platform contract, independent of ZCP presentation. |
| Expected stochastic variation | Different valid path/outcome with no violated scenario or system contract. |
| Unknown | Missing or ambiguous evidence prevents a unique attribution. |

Do not classify an issue as “the LLM had trouble” merely because the final call
was wrong. First prove that the correct information was present and usable in
that turn. Conversely, do not change ZCP guidance to compensate for a model
mistake until the existing guidance and tool result are shown in context.

## 8. Finding record

Each finding should use this shape:

```text
ID
Observed outcome
Expected contract and source
First divergence
Primary owner hypothesis
Secondary contributors
Confidence: proven / strong / tentative / unknown
Provider evidence references
MCP/client evidence references
Lifecycle/platform/artifact references
User-visible impact
Minimal reproduction
Candidate ZCP layer and source owner, if applicable
Proposed test before code change
Re-run condition
```

A file path or code change is proposed only after the evidence points to the
responsible owner. Guidance findings should use capture-time composition
provenance when available. Handler findings should trace the MCP method through
its handler/workflow/ops consumers before proposing a patch.

## 9. Decision loop after analysis

For each proven ZCP-owned finding:

1. Reconstruct why the current artifact/code exists from spec, tests, and git
   history.
2. Check parallel paths for the same invariant.
3. Write the smallest RED test at the owning layer.
4. Implement without changing unrelated capture/Inspector behavior.
5. Run affected unit/tool/integration layers and the Inspector non-interference
   gates.
6. Rebuild a temporary commit-addressed binary.
7. Re-run the same scenario with the same model, prompt, seed, and verification
   contract.
8. Compare evidence, not just exit code or self-review language.

One run proves one observed behavior, not its prevalence. For guidance/model
interactions where stochasticity matters, retain the first run and perform an
additional unchanged replay before or alongside the fix verification when the
operator approves the extra time/cost.

Behavioral ZCP fixes, Inspector fixes, and eval-fixture fixes remain separate
commits/findings so one category does not mask another.

## 10. Failure handling

### Eval launch fails before manifest path

Preserve the remote launch log and candidate hash. Do not select a capture by
mtime. Fix launch/auth/config and start a new run ID.

### SSH connection drops

The detached eval continues. Reconnect, read the PID/exit/log files, and restore
the tunnel. Do not start a second eval while the first PID is live.

### Capture is partial or unclean

Copy it unchanged and inspect the durable prefix. Report the exact gap/failure.
Do not re-label it complete. A repeat run gets a new capture ID.

### Live UI fails

Do not weaken loopback/Origin/CSP. Continue the run, copy the finalized bundle,
and start the local Inspector afterward.

### Scenario cleanup fails

Do not immediately rerun, because the next seed may destroy diagnostic state.
Preserve direct platform state/logs and classify the cleanup failure first.
Manual cleanup is a separate explicit operation.

### Scenario fixture differs from its description

Classify the run as harness/fixture-invalid for the intended semantic question.
The provider/MCP evidence is still valid evidence of how the agent handled the
actual state.

### Credential-shaped content appears

Keep raw evidence private, stop publication/copy beyond the local private
bundle, and follow the token containment/rotation policy. Do not redact the
canonical capture in place.

## 11. Run outputs

The local private run directory should contain:

```text
<scenario>-<suite>/
├── capture/                  canonical immutable capture bundle
├── eval/                     pulled runner artifacts
├── operator.log              remote command output, token-screened
├── run-meta.json             commit/hash/version/project/scenario/exit identities
├── inspection-summary.txt    deterministic local core inspection
├── observations.md           factual review and evidence-linked findings
└── operator-reads/           only read-only side observations used in findings
```

`run-meta.json` must not contain capability/reveal tokens, provider credentials,
API tokens, environment values, or captured body content.

Deliverables to the operator:

1. live loopback/tunneled one-time Inspector URL shortly after capture starts;
2. final local one-time Inspector URL after copy and integrity validation;
3. scenario/suite/capture IDs and binary/scenario hashes;
4. integrity and semantic-outcome summary kept separate;
5. evidence-linked finding table;
6. explicit unknowns and missing evidence;
7. proposed ZCP changes only for findings whose owner is supported by evidence.

## 12. Acceptance checklist

The run is operationally complete only when all applicable items are true:

- [ ] User confirmed the scenario (default: `greenfield-node-postgres-dev-stage`).
- [ ] Target is the isolated `eval-new` project and contains only `zcp` before seed.
- [ ] Candidate source is clean and all preflight gates pass.
- [ ] Temporary remote binary hash equals local candidate hash.
- [ ] Canonical remote `/usr/local/bin/zcp` is unchanged.
- [ ] Strict capture MCP config names the temporary current executable.
- [ ] Capture path and suite ID were parsed from this run's output.
- [ ] Live Inspector is loopback-only and reached through an exact-port SSH tunnel.
- [ ] Operator received and opened a live URL without a public listener.
- [ ] No operator mutation perturbed the scenario.
- [ ] Eval and capture statuses were recorded independently.
- [ ] `platform-snapshot.json` was written before cleanup and inventoried.
- [ ] Remote and local manifest hashes match.
- [ ] Local core inspection reports the real integrity state.
- [ ] Raw/eval artifacts are private, gitignored, and token-screened.
- [ ] Operator received a final local Inspector URL.
- [ ] Review followed integrity → raw causal path → platform state → retrospective.
- [ ] Every finding has evidence references and an explicit owner confidence.
- [ ] Temporary remote UI/tunnel/binary/evidence cleanup waits for operator confirmation.

## 13. Approval points

No remote upload, eval seed/cleanup, provider-cost-incurring Claude run, canonical
binary change, or temporary evidence cleanup is performed by this plan itself.

Execution requires one operator confirmation covering:

- selected scenario;
- temporary candidate upload;
- destructive use of the isolated eval project;
- expected Claude/provider cost;
- private plaintext capture and reveal.

Replacing the canonical remote binary or running a release remains a separate
operation and is not required by this plan.
