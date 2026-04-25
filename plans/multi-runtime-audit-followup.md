# Runtime Audit Follow-up — Shipping Plan

> **Authoritative for**: 7 forward fixes from the 11-runtime weather-dashboard audit (eval ran 2026-04-25 on eval-zcp). Earlier 7 fixes already shipped.
> **Self-contained**: every fix in §5 is written so a fresh session with zero memory of how this plan was authored can ship that fix without asking questions, without re-investigating the platform, and without re-litigating decisions.
> **Maintenance**: when a fix ships, update its `Status` line in §5 with the commit hash, leave the spec text intact (it documents intent forever). When all forward fixes ship, move this file to `plans/archive/`.

---

## §0. RESTART PROTOCOL — read FIRST in every session

**Always**:
1. `git log --grep='audit\|atoms(\|verify:\|workflow:\|deploy:' --oneline --max-count=20` — see recent shipping.
2. Read §2 "Shipping order + status" — find first unchecked `[ ]`.
3. Read that fix's full spec in §5. **Read it ALL** — every fix spec is self-contained on purpose; the temptation to skim and "just look at the diff" misses the locked decisions that prevent regressions.
4. Read §3 "Critical empirical facts" if you'll touch any of: dev_server, env vars, ServiceMeta, atom rendering. The facts there are the load-bearing reality those fixes assume.
5. Read §4 "Locked decisions" before considering any "wouldn't it be cleaner if..." refactor. They were locked because alternatives were investigated and rejected; re-litigation must be backed by new data.
6. Run `go test ./... -count=1 -short` — baseline green?
7. Execute the fix per its spec. One commit per fix. Commit message format: `<area>(<scope>): <one-line>` per repo convention.
8. After commit, update §2 status: tick `[x]` and paste commit hash. Update §5 fix's `Status:` line.

**Do NOT**:
- Re-litigate decisions in §4 without new empirical data.
- Bundle multiple fixes into one commit (the per-fix pinning test must verify in isolation).
- Drop pinning tests as "obvious" — they exist precisely because something obvious slipped past once.
- Skip §3 reading if your fix touches its topic.
- Add scope creep ("while I'm here, let me also fix..."). Each spec lists what's in scope.
- Treat audit data as advisory — it is the ground truth for these fixes; rejecting it requires re-running the audit.

---

## §1. CONTEXT

### Origin

Multi-runtime weather-dashboard eval sweep ran 2026-04-25 against eval-zcp project. 11 scenarios across runtimes: PHP-Laravel, Node.js, Python, Go, Next.js SSR, .NET, Ruby, Rust, Bun, Java, Deno. 9/11 pass. Each scenario produced a structured `## EVAL REPORT` with **Atom Bucket Classification** (A=load-bearing / B=useful awareness / C=pure noise).

### Empirical data

| Artifact | Path | Use |
|---|---|---|
| Final audit report | `eval/results/audit-multi-weather-20260425_012145.md` | 11-runtime bucket matrix + friction table |
| Per-scenario raw | `eval/results/scenario-*/2026-04-2*/weather-dashboard-*/` | result.json + log.jsonl + tool-calls.json + assessment |
| Aggregator | `eval/scripts/aggregate-weather-audit.py` | Re-run after edits to refresh report |
| Scenarios | `internal/eval/scenarios/weather-dashboard-{php-laravel,nodejs,python,go,nextjs-ssr,dotnet,ruby,rust,bun,java,deno}.md` | 11 scenario specs with `## EVAL REPORT` self-eval contract |

### Already shipped — DO NOT re-implement

Seven fixes plus one bonus already landed on `main` from this audit. Listed for context so a fresh session knows what's done and where to look for parallel patterns.

| Commit | Fix | Files |
|---|---|---|
| `d448325` | host→hostname + 13 managed types verified live | `bootstrap-env-var-discovery.md`, `develop-first-deploy-env-vars.md` |
| `7b8c3fd` | `develop-dev-server-triage` axes: `runtimes:[dynamic]` + `deployStates:[deployed]` | atom frontmatter |
| `c78182f` | new `serviceStatus` axis on AxisVector + ParseAtom + synthesize.go matcher | atom.go, synthesize.go, develop-ready-to-deploy.md, spec-knowledge-distribution.md |
| `bea7955` | `develop-dev-server-reason-codes` axis: `deployStates:[deployed]` | atom frontmatter |
| `46325f0` | `RecordExternalDeploy(stateDir, hostname)` + `zerops_workflow action="record-deploy"` | service_meta.go, workflow.go, workflow_record_deploy.go |
| `ef10d27` | gofmt fixup after struct-field reformat | atom.go |
| `8188b95` | DELETE `startup_detected` check (broken-by-design from c3493c1: `Search` is substring not OR) | verify_checks.go, verify.go, verify_test.go (×2), deploy_poll.go, spec-workflows.md |
| `8e72fc2` | v1 of this plan file | this file |

---

## §2. Shipping order + status

Sprint A first (structural bugs, smallest blast radius). Then B (env vars cluster). Then C (knowledge gap). Each row is one commit.

| # | Sprint | Fix | Status | Commit |
|---|---|---|---|---|
| 1 | A | Fix #14 — pair-keyed `setup=` resolution | [ ] | _pending_ |
| 2 | A | Fix #13 — env-prefix wrap in `zerops_dev_server` | [ ] | _pending_ |
| 3 | A | Fix #18 — atom-ID headers in `Synthesize` | [ ] | _pending_ |
| 4 | B | Fix #15 — `zerops_import` accepts `project.envVariables` | [ ] | _pending_ |
| 5 | B | Fix #7 — `develop-project-env-vars` atom + extend env-channels | [ ] | _pending_ |
| 6 | B | Fix #17 — `source` annotation in `zerops_discover` | [ ] | _pending_ |
| 7 | C | Fix #8-#12 — `develop-first-deploy-runtime-gotchas` atom | [ ] | _pending_ |

**After all 7 ship**: §8 validation re-run. If green, this file moves to `plans/archive/`.

---

## §3. Critical empirical facts (preserve verbatim)

These were established empirically in the source session and inform every fix. Treat them as load-bearing reality. If you suspect any is stale, verify on the live platform before acting on the suspicion — don't speculate.

### 3.1 Zerops runtime sh is busybox `ash` (NOT bash, NOT dash)

**Verified 2026-04-25 on eval-zcp:**
```
$ ssh probednode "uname -a; ls -la /bin/sh"
Linux ... x86_64 Linux
lrwxrwxrwx 1 root root 12 Sep 6 2024 /bin/sh -> /bin/busybox
```

**Implication**: BusyBox ash's `exec` builtin parses `exec VAR=val cmd` such that `VAR=val` is treated as the program name, not as an env-prefix assignment. POSIX 2.14 / 2.9.1 formally allow env-prefix on the special builtin `exec`, but busybox lands on the strict-parse side. Bash and dash work; busybox does not.

**Why this matters**: Fix #13. Any code that passes user-provided shell commands through `sh -c '... exec <CMD>'` MUST handle env-prefix in `<CMD>` itself, because the runtime container's `/bin/sh` will not.

### 3.2 Ruby eval log proving Fix #13 friction

From `eval/results/scenario-20260425_031112/2026-04-24-232027/weather-dashboard-ruby/log.jsonl`:

```json
{"action":"start","hostname":"appdev","running":false,"port":8080,
 "logTail":"sh: exec: line 0: BUNDLE_PATH=vendor/bundle: not found",
 "reason":"health_probe_connection_refused"}
```

The agent then succeeded with `command="sh -c 'BUNDLE_PATH=vendor/bundle bundle exec puma -p 8080 -b tcp://0.0.0.0'"` — wrapping in `sh -c` works (inner shell parses `VAR=val cmd` as standard simple-command env assignment, regardless of busybox `exec` builtin behavior). `env VAR=val cmd` (POSIX env utility) also works universally.

### 3.3 Python `pip install --target=./vendor` requires PYTHONPATH

**Verified 2026-04-25 on eval-zcp python@3.14.2:**
```
$ pip install --target=./vendor -r requirements.txt   # gunicorn==21.2.0
EXIT=0  ->  ./vendor/bin/gunicorn exists

$ which gunicorn
EXIT=1   (not on PATH)

$ ./vendor/bin/gunicorn --version
EXIT=1
ModuleNotFoundError: No module named 'gunicorn'
# Even the explicit binary path FAILS without PYTHONPATH

$ PYTHONPATH=./vendor python -m gunicorn --version
__main__.py (version 21.2.0)
EXIT=0   (works)

$ PYTHONPATH=./vendor ./vendor/bin/gunicorn --version
gunicorn (version 21.2.0)
EXIT=0   (binary path also works WITH PYTHONPATH)
```

**Implication**: Fix #10 (in original plan) said "use `start: /var/www/vendor/bin/gunicorn ...`" — this is **WRONG** standalone. The atom MUST teach BOTH:
- `run.envVariables.PYTHONPATH: /var/www/vendor` (mandatory)
- `start: python -m gunicorn ...` (no PATH dep) OR `start: /var/www/vendor/bin/gunicorn ...` (works once PYTHONPATH set)

Existing recipe `internal/knowledge/recipes/python-hello-world.md` already does this correctly — atom must align with recipe, not contradict.

### 3.4 Pair-keyed invariant (E8 from spec-workflows.md §8)

**Definition**: exactly one `ServiceMeta` JSON file represents a runtime service — as a dev/stage pair (container+standard, local+standard) or a single hostname (dev/simple/local-only). The file lives at `.zcp/state/services/{Hostname}.json` keyed on the **dev** hostname (not stage).

**Helpers**:
- `workflow.FindServiceMeta(stateDir, hostname)` — honors invariant. Returns the pair meta whether `hostname` is the dev or stage half.
- `workflow.ReadServiceMeta(stateDir, hostname)` — direct file read by `<hostname>.json`. Misses pair meta when `hostname` is the stage half.
- `meta.Hostnames()` — `[Hostname, StageHostname]` for pairs, `[Hostname]` otherwise.
- `meta.RoleFor(hostname)` — returns `DeployRoleStage` for stage hostname, `DeployRoleDev`/`DeployRoleSimple` for primary. Use this for hostname-specific role lookups.
- `meta.PrimaryRole()` — returns the dev/simple role only. Use only when you genuinely want the primary, not when you have a specific hostname in scope.

**Lint pinning**: `TestNoInlineManagedRuntimeIndex` enforces a related pattern (no inline `m.Hostname` keying). Fix #14 will add `TestReadServiceMetaUsageInTools` for the pair-keyed-violation pattern in tool handlers.

### 3.5 Atom ID hallucination examples (proves Fix #18)

Agents in self-eval reports invented atom IDs from semantic memory rather than reading the corpus:

| Hallucinated ID | Real corpus ID | Frequency in audit |
|---|---|---|
| `develop-knowledge-on-demand` | `develop-knowledge-pointers` | 3/8 runtimes flagged it C |
| `develop-strategy-pick` | `develop-strategy-awareness` | 3/8 runtimes |
| `develop-apiMeta-errors` | `develop-api-error-meta` | 2/8 runtimes |
| `develop-apiMeta` | (no exact match; agent invented from "apiMeta" mention in body) | 2/11 runtimes |
| `develop-pick-strategy` | `develop-strategy-review` (semantic match) | mentioned in audit §C-buckets |

**Why**: Synthesize returns `[]string` (atom bodies, no IDs); render dumps bodies under "Guidance:" header. Agents see semantic content but never the canonical ID. Cross-runtime aggregator can't mechanically map noise across runtimes when 5 different agents fake-name the same atom.

### 3.6 Real managed service env keys (live-verified 2026-04-25, commit `d448325`)

Use this table when classifying envs in Fix #17 (`source: "platform"`). All 13 types provisionable today; RabbitMQ returns `serviceStackTypeVersionIsNotActive` (skip). MySQL/Redis/MongoDB **do not exist in platform schema** — earlier atoms mistakenly referenced them.

| Type prefix | Platform-injected keys |
|---|---|
| `postgresql@` | connectionString, connectionTlsString, hostname, port, portTls, user, password, superUser, superUserPassword, dbName |
| `mariadb@` | connectionString, hostname, port, user, password, dbName |
| `valkey@` | connectionString, connectionTlsString, hostname, port, portTls (no auth — private network) |
| `keydb@` | connectionString, hostname, port (no auth, no TLS) |
| `nats@` | connectionString, hostname, port, portManagement, user, password |
| `kafka@` | hostname, port, user, password (no connectionString — build broker URL from hostname:port) |
| `clickhouse@` | connectionString, hostname, port, portHttp, portMysql, portNative, portPostgresql, user, password, superUser, superUserPassword, dbName, clusterName |
| `elasticsearch@` | connectionString, hostname, port, user, password |
| `meilisearch@` | connectionString, hostname, port, masterKey, defaultAdminKey, defaultSearchKey, defaultReadOnlyKey, defaultChatKey |
| `typesense@` | connectionString, hostname, port, apiKey |
| `qdrant@` | connectionString, grpcConnectionString, hostname, port, grpcPort, apiKey, readOnlyApiKey |
| `object-storage` | apiUrl, apiHost, bucketName, accessKeyId, secretAccessKey, quotaGBytes, hostname |
| `shared-storage` | hostname (mounted via `mount:` in zerops.yaml, not a network service) |

Plus runtime services have: `zeropsSubdomain` (when subdomain enabled), `zeropsHost` and similar — these are ZCP-related, not user-set. The classification module (Fix #17) needs to maintain this table.

### 3.7 Atom corpus reference (real IDs, as of plan authoring)

When writing test fixtures or referencing atoms, use these exact IDs. Do NOT invent or paraphrase. Generate fresh list with `ls /Users/macbook/Documents/Zerops-MCP/zcp/internal/content/atoms/` if you need to verify.

```
bootstrap-adopt-discover, bootstrap-classic-plan-dynamic, bootstrap-classic-plan-static,
bootstrap-close, bootstrap-discover-local, bootstrap-env-var-discovery, bootstrap-intro,
bootstrap-mode-prompt, bootstrap-provision-local, bootstrap-provision-rules,
bootstrap-recipe-close, bootstrap-recipe-import, bootstrap-recipe-match, bootstrap-resume,
bootstrap-route-options, bootstrap-runtime-classes, bootstrap-verify, bootstrap-wait-active,
develop-api-error-meta, develop-auto-close-semantics, develop-change-drives-deploy,
develop-checklist-dev-mode, develop-checklist-simple-mode, develop-close-manual,
develop-close-push-dev-dev, develop-close-push-dev-local, develop-close-push-dev-simple,
develop-close-push-dev-standard, develop-close-push-git-container, develop-close-push-git-local,
develop-closed-auto, develop-deploy-files-self-deploy, develop-deploy-modes,
develop-dev-server-reason-codes, develop-dev-server-triage,
develop-dynamic-runtime-start-container, develop-dynamic-runtime-start-local,
develop-env-var-channels, develop-first-deploy-asset-pipeline-container,
develop-first-deploy-asset-pipeline-local, develop-first-deploy-env-vars,
develop-first-deploy-execute, develop-first-deploy-intro, develop-first-deploy-promote-stage,
develop-first-deploy-scaffold-yaml, develop-first-deploy-verify, develop-first-deploy-write-app,
develop-http-diagnostic, develop-implicit-webserver, develop-intro, develop-knowledge-pointers,
develop-local-workflow, develop-manual-deploy, develop-mode-expansion,
develop-platform-rules-common, develop-platform-rules-container, develop-platform-rules-local,
develop-push-dev-deploy-container, develop-push-dev-deploy-local,
develop-push-dev-workflow-dev, develop-push-dev-workflow-simple, develop-push-git-deploy,
develop-ready-to-deploy, develop-static-workflow, develop-strategy-awareness,
develop-strategy-review, develop-verify-matrix, export, idle-adopt-entry,
idle-bootstrap-entry, idle-develop-entry, strategy-push-git-intro,
strategy-push-git-push-container, strategy-push-git-push-local,
strategy-push-git-trigger-actions, strategy-push-git-trigger-webhook
```

---

## §4. Locked decisions (do NOT re-litigate without new data)

These were locked after investigation in the source session. Re-opening requires fresh empirical evidence, not "wouldn't it be cleaner if".

### D1 — One consolidated atom for runtime-specific gotchas (Fix #8-#12)

**Decided**: Single atom `develop-first-deploy-runtime-gotchas.md` with a 5-row table covering Node, Next.js, Python, Go/Rust/.NET/Java, Ruby.

**Rejected alternative**: Per-runtime atoms with a new `runtimeBase` axis on `AxisVector`. Cost: ~100 LOC Go (atom.go, synthesize.go, compute_envelope.go) + 5 atoms + 4 axis tests, all to filter 5 short notes that fire only at scaffold-phase. Over-engineering for one-shot concern. If per-runtime tips ever exceed ~20 lines combined, reopen this decision.

**DO NOT** add a `runtimeBase` axis as part of Fix #8-#12. If you find yourself wanting one, the table grew too large — write down what you wanted to add, count rows, and re-evaluate.

### D2 — Annotation, not restructure, for Fix #17

**Decided**: Add `source: "platform" | "user-project" | "user-service"` field to existing flat env entries in `DiscoverResult.Services[].Envs`.

**Rejected alternative**: Restructure `Envs []map[string]any` into `Envs { platformInjected, userServiceLevel, userProjectLevel }` buckets. Breaks `references-fields` AST tests + every atom that pins DiscoverResult shape. Annotation gives the same agent-side benefit (filter by source) backward-compatibly.

**DO NOT** change `Envs` shape. Add fields to entries; do not nest.

### D3 — Uniform atom-ID prefix (Fix #18)

**Decided**: Every atom rendered through `Synthesize` gets `=== <id> ===\n` prefix. No opt-out, no per-atom flag, no phase-based exclusion.

**Rejected alternative**: Opt-out for idle entry atoms (`idle-bootstrap-entry`, etc.) on the theory that user-facing intro shouldn't have machine-style headers. Counter: idle entries are still authored content, not raw user prose; agent benefits from consistency, and per-atom flags create authoring decisions that drift.

**DO NOT** add a frontmatter `renderID: false` flag.

### D4 — Phase 1 only for Fix #14 (no `ZeropsSetup` field on ServiceMeta)

**Decided**: Phase 1 fix (FindServiceMeta + RoleFor) ships. Phase 2 (persist `zeropsSetup` value from import YAML to a new ServiceMeta field) deferred.

**Rationale**: All audit-observed recipe friction uses convention `zeropsSetup: dev` ↔ `setup: dev` and `zeropsSetup: prod` ↔ `setup: prod`. Phase 1 covers 100% of these via existing role→setup fallback in `resolveSetupEntry`. Phase 2 adds value only for nonconventional recipes (`zeropsSetup: foo` with `setup: foo`); none observed.

**DO NOT** add `ZeropsSetup` field to ServiceMeta as part of Fix #14. If a real nonconventional recipe surfaces later, reopen.

### D5 — Drop Fix #16 (env set 0-services message)

**Decided**: Drop. Stávající message at `internal/tools/env.go:178` ("No ACTIVE services needed restart. The new env value will be injected when a service starts or deploys.") is technically correct, not a bug.

**Rationale**: Audit attribution was "confusing" but verification shows the message accurately states what happened (env stored, no consumer to restart) and what will happen (inject on next start). Not friction.

**DO NOT** reword this message. If you want to, document new evidence first.

### D6 — Drop Fix #6 (per-scenario eval `timeoutMinutes`)

**Decided**: Drop. User instruction.

**Rationale**: Eval infra fix; not relevant to current shipping scope.

**DO NOT** add this fix back without explicit user re-authorization.

### D7 — Empirical verification IS the bar for Zerops platform claims

Per `CLAUDE.local.md`: "Verify against real Zerops". Atoms and tool descriptions making behavioral claims about the Zerops platform must be verified against the live platform (live API, SSH probe, eval). Don't infer from training data, recipe docs alone, or prior atom content. eval-zcp project (`i6HLVWoiQeeLv8tV0ZZ0EQ`) is the authorized playground; SSH access via `ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null <hostname>`; zcli on `zcp` container scoped to eval-zcp.

**Apply when**: any fix here has a "do X on Zerops" claim. If unverified, verify before shipping.

### D8 — One commit per fix; pinning test mandatory

**Decided**: Each of the 7 fixes ships as exactly one commit including its pinning test. No bundling. No "I'll add the test later".

**Rationale**: Pinning tests document intent — without them, the fix is a string of bytes that can be reverted accidentally. The test makes the rationale survive future refactors.

**DO NOT** ship a fix without its test. The "wide-and-long change" doctrine in CLAUDE.local.md applies: if a fix needs more files for full coherence, touch them — but each commit includes its own verification.

---

## §5. Fix specifications

Each subsection below is **self-contained**: a fresh session can ship that fix reading only that subsection (plus §3 facts when referenced and §4 decisions when referenced). The order in this list matches the shipping order in §2.

---

### A1 — Fix #14 Phase 1: pair-keyed `setup=` resolution

**Status**: [ ] not shipped — _commit pending_

#### Goal

Cross-deploy `zerops_deploy targetService=appstage` (no explicit `setup=`) must resolve to the right zerops.yaml setup block (typically `prod` for stage hostname) without the agent needing to know the recipe convention.

#### Friction (what an agent observes today)

Agent runs `zerops_deploy targetService=appstage` for a recipe with two setups (`setup: dev`, `setup: prod`). Today this fails — zcli is invoked with `--setup appstage`, recipe has no `appstage` block, error: `Cannot find corresponding setup in zerops.yaml`. Agent must know to pass `setup=prod` explicitly.

#### Root cause (verified)

In `internal/tools/deploy_preflight.go`, function `deployPreFlight`:

1. Line ~22 reads meta with `workflow.ReadServiceMeta(stateDir, targetHostname)`. For `targetHostname="appstage"`, this looks for `appstage.json` which does not exist — meta is at `appdev.json` per pair-keyed invariant E8 (see §3.4). Returns `meta == nil` → permissive skip of the whole pre-flight at line ~28-30.

2. Line ~57 derives role with `meta.PrimaryRole()`. Even if meta were correct, `PrimaryRole()` returns the dev role for the pair. For `targetHostname=appstage` we want `DeployRoleStage` so the existing `resolveSetupEntry` fallback (lines ~115-118: `role == Stage || role == Simple → "prod"`) fires.

The same pattern of bugs exists in `internal/tools/deploy_strategy_gate.go` around line 50.

#### Files to change

| File | Why |
|---|---|
| `internal/tools/deploy_preflight.go` | Two-line fix in `deployPreFlight` |
| `internal/tools/deploy_strategy_gate.go` | One-line parallel fix |
| `internal/tools/deploy_preflight_test.go` (likely new) | Pinning test |
| `internal/workflow/lint_test.go` (or wherever `TestNoInlineManagedRuntimeIndex` lives) | New lint test for pair-keyed invariant in tools layer |

#### Required change

In `internal/tools/deploy_preflight.go`, locate the `deployPreFlight` function. Find these two anchors (line numbers may have drifted; grep for the exact strings):

**Anchor 1** — meta read:
```go
meta, err := workflow.ReadServiceMeta(stateDir, targetHostname)
```
Replace with:
```go
meta, err := workflow.FindServiceMeta(stateDir, targetHostname)
```

**Anchor 2** — role derivation:
```go
role := meta.PrimaryRole()
```
Replace with:
```go
role := meta.RoleFor(targetHostname)
```

In `internal/tools/deploy_strategy_gate.go`, locate the analogous `ReadServiceMeta` call near line 50 and replace with `FindServiceMeta` (review the surrounding context — if the function has scope-limited semantics that DO want primary-only, leave as-is and document why; otherwise migrate).

#### Pinning test (full Go code)

Add to `internal/tools/deploy_preflight_test.go` (create if absent):

```go
package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestDeployPreFlight_CrossDeploy_StageHostname_ResolvesToProd pins the
// pair-keyed invariant fix: when pre-flight receives targetHostname=appstage
// (stage half of container+standard pair), it must resolve to prod setup,
// not silently skip via nil meta. See plans/multi-runtime-audit-followup.md
// §5 Fix A1 for the bug history.
func TestDeployPreFlight_CrossDeploy_StageHostname_ResolvesToProd(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	yaml := `zerops:
  - setup: dev
    build:
      base: nodejs@22
      deployFiles: [.]
    run:
      start: node dist/main.js
  - setup: prod
    build:
      base: nodejs@22
      deployFiles: [.]
    run:
      start: node dist/main.js
`
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	// Pair-keyed meta: ONE file for the pair, StageHostname set.
	meta := &workflow.ServiceMeta{
		Hostname:         "apidev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "apistage",
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-04-01T00:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatal(err)
	}

	mock := platform.NewMock()

	resolved, result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "apistage", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result; nil indicates silent skip via missing meta (pair-keyed invariant violation)")
	}
	if !result.Passed {
		t.Fatalf("expected pre-flight pass for stage cross-deploy; got: %+v", result)
	}
	if resolved != "prod" {
		t.Errorf("expected resolvedSetup=\"prod\" for targetHostname=apistage (role=stage→prod fallback); got %q", resolved)
	}
}
```

If `platform.NewMock()` doesn't exist or the mock interface differs, adapt — the assertion shape is what matters: pair-keyed meta + stage targetHostname + empty setup → resolved "prod".

#### Lint test (full Go code)

Add to the file containing `TestNoInlineManagedRuntimeIndex` (find via `grep -rn 'TestNoInlineManagedRuntimeIndex' internal/`):

```go
// TestReadServiceMetaUsageInTools enforces pair-keyed invariant E8 for
// tool-layer handlers. ReadServiceMeta is direct-file lookup; for any
// hostname that may be a stage half of a pair, FindServiceMeta is
// mandatory. Tool handlers receive hostnames from agent input — those
// can always be stage hostnames.
//
// Allowed: ReadServiceMeta in workflow/, ops/ where the hostname is
// guaranteed to be the dev primary by construction (e.g. iterating
// ListServiceMetas results).
//
// Disallowed: ReadServiceMeta in internal/tools/*.go where the input
// hostname comes from a tool argument.
func TestReadServiceMetaUsageInTools(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob("../tools/*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		// Look for the literal pattern; reflection-based AST would be
		// stricter but this catches the regression vector that hit Fix #14.
		if strings.Contains(string(data), "workflow.ReadServiceMeta(") {
			t.Errorf("%s uses workflow.ReadServiceMeta — tool-layer handlers must use FindServiceMeta to honor pair-keyed invariant E8 (stage hostnames must resolve to dev-keyed pair meta). See plans/multi-runtime-audit-followup.md §3.4.", f)
		}
	}
}
```

#### Verification

After patching:
1. `go test ./internal/tools -run TestDeployPreFlight_CrossDeploy_StageHostname_ResolvesToProd -count=1` → pass.
2. `go test ./internal/workflow -run TestReadServiceMetaUsageInTools -count=1` → pass (no `ReadServiceMeta` call in tools after fix).
3. Full suite: `go test ./... -count=1 -short` → green.
4. Lint: `make lint-local` → green.

#### Done criteria

- [ ] `deployPreFlight` uses `FindServiceMeta` and `RoleFor`
- [ ] `deploy_strategy_gate.go` parallel call migrated (or documented why exempt)
- [ ] Pinning test passes
- [ ] Lint test added and passing
- [ ] One commit, message format `deploy(preflight): use pair-keyed FindServiceMeta + RoleFor`

#### Locked decisions for THIS fix

- **Phase 1 only**. See §4 D4. Do NOT add `ZeropsSetup` field to `ServiceMeta`.
- **Don't touch `deploy_local_git.go` parallelism**. Audit observed friction was push-dev path; push-git lacks the role-based fallback but is not in scope. Document in a follow-up plan if it becomes friction.
- **Don't refactor `resolveSetupEntry`**. The existing fallback chain (explicit → role → stage→prod → hostname) is correct; only the inputs were wrong.

#### DO NOT

- Add a `ZeropsSetup` field to ServiceMeta as part of this commit.
- Bundle the parallel `deploy_strategy_gate.go` fix with the preflight fix into separate commits — they're one logical change (pair-keyed invariant in tools layer); ship together.
- Drop the lint test as "obvious" — it's the durable gate against the regression class.

---

### A2 — Fix #13: env-prefix wrap in `zerops_dev_server`

**Status**: [ ] not shipped — _commit pending_

#### Goal

`zerops_dev_server action=start command="VAR=val cmd"` works regardless of which `/bin/sh` the runtime container ships (busybox ash, bash, dash). Today it silently fails on busybox.

#### Friction (what an agent observes today)

Agent calls `zerops_dev_server action=start command="BUNDLE_PATH=vendor/bundle bundle exec puma" port=8080`. Tool returns `running: false, reason: health_probe_connection_refused, logTail: "sh: exec: line 0: BUNDLE_PATH=vendor/bundle: not found"`. Universal pattern affecting Ruby (BUNDLE_PATH), Node (NODE_ENV), Python (PYTHONPATH), any framework needing env-prefix in start command.

#### Root cause (verified — see §3.1 and §3.2)

`/bin/sh` on Zerops runtime containers is busybox `ash`. BusyBox ash's `exec` builtin parses `exec VAR=val cmd` such that `VAR=val` is treated as the program name. POSIX 2.14 formally allows env-prefix on `exec`, but busybox is non-conformant on this point. See §3.1.

The tool description in `internal/tools/dev_server.go` historically advertised "Env assignments and pipes are supported. Example: 'PORT=3000 npm run dev'." — promise the tool could not keep on busybox runtimes.

POSIX `env` utility (`env VAR=val cmd`) works universally. So does `sh -c 'VAR=val cmd'` (inner shell parses simple-command env-prefix correctly regardless of outer `exec` builtin behavior). The transparent fix: detect leading env-prefix in the user's command and wrap with `env `.

#### Files to change

| File | Why |
|---|---|
| `internal/ops/dev_server_start.go` | Add `wrapEnvPrefix`; call it in `spawnDevProcess` |
| `internal/ops/dev_server_test.go` (or new file) | Pinning test for `wrapEnvPrefix` |

#### Required change

In `internal/ops/dev_server_start.go`, locate the `spawnDevProcess` function. The current `inner` script construction looks like:

```go
inner := fmt.Sprintf("echo $$ > %s; exec %s", shellQuote(pidFile), command)
```

Change `command` to `wrapEnvPrefix(command)`:

```go
inner := fmt.Sprintf("echo $$ > %s; exec %s", shellQuote(pidFile), wrapEnvPrefix(command))
```

Add the helper at file scope (near other helpers in the same file):

```go
// envPrefixRe matches one or more leading POSIX env assignments —
// `[A-Z_][A-Z0-9_]*=<no-whitespace>` followed by whitespace.
var envPrefixRe = regexp.MustCompile(`^([A-Z_][A-Z0-9_]*=\S+\s+)+`)

// wrapEnvPrefix prefixes `env ` to a command that starts with one or
// more `VAR=val` env assignments, so the POSIX env utility sets the
// variables before exec'ing the command. This is needed because
// Zerops runtime containers ship busybox `ash` whose `exec` builtin
// does NOT support env-prefix assignments (POSIX 2.14 leaves this
// implementation-defined; busybox lands on the strict-parse side).
// Bash and dash work; busybox does not. See plans/multi-runtime-
// audit-followup.md §3.1 for the ground-truth verification.
//
// Commands without leading env-prefix are returned unchanged.
func wrapEnvPrefix(command string) string {
	if envPrefixRe.MatchString(command) {
		return "env " + command
	}
	return command
}
```

Add `"regexp"` to the imports if not already present.

Also update the tool description in `internal/tools/dev_server.go` for the `command` parameter — the existing text claims env-prefix is supported; with the wrap that's now true, but worth aligning the wording:

```
Description: "Shell command that starts the dev server. Required for start and restart. Example: 'npm run start:dev', 'vite --host 0.0.0.0', 'PORT=3000 npm run dev'. Leading env-prefix (`VAR=val cmd`) is automatically wrapped to use POSIX env utility (works on busybox sh). Pipes, redirections, and complex shell forms are supported through the underlying `sh -c` invocation. Unused by stop/status/logs.",
```

#### Pinning test (full Go code)

Add to `internal/ops/dev_server_test.go` (or create `dev_server_envwrap_test.go`):

```go
package ops

import "testing"

func TestWrapEnvPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
		want    string
	}{
		{
			name:    "single env prefix gets wrapped",
			command: "PORT=3000 node server.js",
			want:    "env PORT=3000 node server.js",
		},
		{
			name:    "no env prefix unchanged",
			command: "node -e 'require(\"http\").createServer(...)'",
			want:    "node -e 'require(\"http\").createServer(...)'",
		},
		{
			name:    "ruby BUNDLE_PATH wrapped",
			command: "BUNDLE_PATH=vendor/bundle bundle exec puma -p 3000",
			want:    "env BUNDLE_PATH=vendor/bundle bundle exec puma -p 3000",
		},
		{
			name:    "multi prefix wrapped (regex matches all)",
			command: "MY_VAR=val OTHER=other cmd args",
			want:    "env MY_VAR=val OTHER=other cmd args",
		},
		{
			name:    "lowercase var name not matched (POSIX env vars are uppercase)",
			command: "myvar=val cmd",
			want:    "myvar=val cmd",
		},
		{
			name:    "command starting with flag unchanged",
			command: "--port 3000 server",
			want:    "--port 3000 server",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := wrapEnvPrefix(tt.command)
			if got != tt.want {
				t.Errorf("wrapEnvPrefix(%q) = %q, want %q", tt.command, got, tt.want)
			}
		})
	}
}
```

#### Verification

1. `go test ./internal/ops -run TestWrapEnvPrefix -count=1` → pass.
2. Full suite: `go test ./... -count=1 -short` → green.
3. (Optional) Live verify on eval-zcp by provisioning a probe nodejs@22, calling `zerops_dev_server action=start command="PORT=3000 node -e 'require(\"http\").createServer((q,s)=>s.end(\"ok\")).listen(parseInt(process.env.PORT))'" port=3000`, then curl. Cleanup via `zcli service delete` afterwards.

#### Done criteria

- [ ] `wrapEnvPrefix` added with comment explaining busybox rationale
- [ ] `spawnDevProcess` calls `wrapEnvPrefix(command)`
- [ ] Pinning test passes covering 6 cases (single, none, ruby, multi, lowercase-skip, flag-skip)
- [ ] Tool description updated in `dev_server.go`
- [ ] One commit, message format `dev_server: wrap env-prefix commands for busybox ash compatibility`

#### Locked decisions for THIS fix

- **Tool fix, not docs-only**. See §4 D7 (verified empirically); per CLAUDE.local.md "single canonical path over multiple variants" — make the tool always-correct rather than documenting a workaround the agent must remember.
- **Wrap only when prefix is detected**. Avoid wrapping every command — would change behavior for users explicitly passing `env ...` already, and adds overhead for the common no-prefix case.
- **Uppercase-only regex** (`[A-Z_][A-Z0-9_]*`). POSIX convention is uppercase env vars. Lowercase matches add false positives for command flags or variable names.

#### DO NOT

- Add a tool flag like `useEnvWrap: true` to make the wrap optional. The wrap is transparent and correctness-preserving — no opt-out.
- Replace the wrap with a `sh -c` re-wrap. `env` is simpler and avoids nested quoting.
- Drop the comment explaining busybox. The reasoning is non-obvious to anyone reading the regex without context.

---

### A3 — Fix #18: atom-ID headers in `Synthesize` output

**Status**: [ ] not shipped — _commit pending_

#### Goal

Each atom rendered into the dispatch brief carries its canonical ID as a visible header, so agents reference atoms by real name in self-eval reports instead of hallucinating IDs from semantic memory.

#### Friction (what the audit observed — see §3.5)

Agents wrote atom IDs like `develop-strategy-pick`, `develop-knowledge-on-demand`, `develop-apiMeta-errors` in self-eval reports. None exist in the corpus (real names: `develop-strategy-awareness`, `develop-knowledge-pointers`, `develop-api-error-meta`). Cross-runtime aggregator can't mechanically map "atom X is C in 5/11 runtimes" when 5 different agents fake-name the same atom 5 different ways. Audit data has noise; signal blurred.

#### Root cause

`internal/workflow/synthesize.go::Synthesize` returns `[]string` — atom bodies, no IDs. `internal/workflow/render.go::renderGuidance` dumps each body indented under a "Guidance:" header. Agents see body content but never the canonical atom ID, so they reconstruct IDs from semantic memory of what the body said.

#### Files to change

| File | Why |
|---|---|
| `internal/workflow/synthesize.go` | Prepend `=== <id> ===\n` to each output body |
| `internal/workflow/synthesize_test.go` | New pinning tests |
| `docs/spec-knowledge-distribution.md` | Document the render convention (one paragraph) |

#### Required change

In `internal/workflow/synthesize.go`, locate the `Synthesize` function. Find the loop at the end:

```go
out := make([]string, 0, len(matched))
for _, atom := range matched {
    body := replacer.Replace(atom.Body)
    if leak := findUnknownPlaceholder(body); leak != "" {
        return nil, fmt.Errorf("atom %s: unknown placeholder %q in atom body", atom.ID, leak)
    }
    out = append(out, body)
}
return out, nil
```

Replace the `out = append(...)` line with:

```go
    // Prepend `=== <id> ===` header so agents see canonical atom IDs in
    // dispatch briefs. Without this, agents in self-eval reports
    // hallucinate IDs from semantic body content (verified in audit
    // 2026-04-25 — see plans/multi-runtime-audit-followup.md §3.5).
    out = append(out, fmt.Sprintf("=== %s ===\n%s", atom.ID, body))
```

Update `docs/spec-knowledge-distribution.md` §5.2 (or the rendering section — find the analogous one) by adding a paragraph:

> **ID headers in render output**: `Synthesize` prepends `=== <atom-id> ===\n` to each body before joining. Agents thus see canonical IDs alongside content, eliminating the fake-ID hallucination class. The header format is `=== ` + ID + ` ===` (markdown-friendly, grep-friendly, distinct from atom body markdown). All atoms get the prefix uniformly — no opt-out, including idle entries.

#### Pinning tests (full Go code)

Add to `internal/workflow/synthesize_test.go`:

```go
func TestSynthesize_PrependsAtomIDHeader(t *testing.T) {
	t.Parallel()

	corpus := []KnowledgeAtom{
		{
			ID:       "test-atom-one",
			Priority: 1,
			Axes:     AxisVector{Phases: []Phase{PhaseIdle}},
			Body:     "First atom body.",
		},
		{
			ID:       "test-atom-two",
			Priority: 2,
			Axes:     AxisVector{Phases: []Phase{PhaseIdle}},
			Body:     "Second atom body.",
		},
	}

	env := StateEnvelope{Phase: PhaseIdle}
	got, err := Synthesize(env, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 atoms, got %d", len(got))
	}
	wantHeaders := []string{"=== test-atom-one ===", "=== test-atom-two ==="}
	for i, want := range wantHeaders {
		if !strings.HasPrefix(got[i], want) {
			t.Errorf("atom %d: expected prefix %q, got start: %q", i, want, got[i][:min(50, len(got[i]))])
		}
	}
	if !strings.Contains(got[0], "First atom body.") {
		t.Errorf("atom 0: expected body content after header, got: %s", got[0])
	}
	if !strings.Contains(got[1], "Second atom body.") {
		t.Errorf("atom 1: expected body content after header, got: %s", got[1])
	}
}

func TestSynthesize_EmptyBodyStillGetsHeader(t *testing.T) {
	t.Parallel()
	corpus := []KnowledgeAtom{
		{
			ID:       "empty-atom",
			Priority: 1,
			Axes:     AxisVector{Phases: []Phase{PhaseIdle}},
			Body:     "",
		},
	}
	env := StateEnvelope{Phase: PhaseIdle}
	got, err := Synthesize(env, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 atom, got %d", len(got))
	}
	if !strings.HasPrefix(got[0], "=== empty-atom ===") {
		t.Errorf("empty body: expected header, got: %q", got[0])
	}
}

func TestSynthesize_MultilineBodyPreservesFormatting(t *testing.T) {
	t.Parallel()
	corpus := []KnowledgeAtom{
		{
			ID:       "multiline-atom",
			Priority: 1,
			Axes:     AxisVector{Phases: []Phase{PhaseIdle}},
			Body:     "Line 1\nLine 2\n\nLine 3 after blank.",
		},
	}
	env := StateEnvelope{Phase: PhaseIdle}
	got, err := Synthesize(env, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if !strings.HasPrefix(got[0], "=== multiline-atom ===\n") {
		t.Errorf("multiline: expected header, got start: %q", got[0][:min(40, len(got[0]))])
	}
	if !strings.Contains(got[0], "Line 1\nLine 2") {
		t.Errorf("multiline: internal formatting lost: %s", got[0])
	}
}
```

If `min` isn't defined yet (it's a Go 1.21+ builtin or a local helper), add `min(a, b int) int { if a < b { return a }; return b }` at file scope or use the builtin.

#### Verification

1. `go test ./internal/workflow -run TestSynthesize_Prepends -count=1` → pass.
2. `go test ./internal/workflow -count=1` → all green (no existing test breaks; they all use `strings.Contains`).
3. `go test ./... -count=1 -short` → green.

#### Done criteria

- [ ] `Synthesize` prepends `=== <id> ===\n`
- [ ] Comment explains rationale + cites this plan section
- [ ] Three new tests pass (basic, empty body, multiline)
- [ ] Spec doc updated with the render convention
- [ ] One commit, message format `synthesize: prepend atom-ID header to each rendered body`

#### Locked decisions for THIS fix

- **Uniform prefix, no opt-out**. See §4 D3.
- **Delimiter `=== id ===`**. Markdown-friendly, grep-friendly. Don't change to `## id`, `<!-- id: ... -->`, `[atom: id]` — the chosen form was investigated; alternatives drift token usage and visual hierarchy.
- **Prepend in `Synthesize`, not in `render.go`**. Keeping the logic in Synthesize means every consumer of synthesized output (status render, immediate workflow, strategy push-git) gets the prefix uniformly. Render layer stays simple.

#### DO NOT

- Add per-atom or per-phase opt-out. See §4 D3.
- Change the delimiter style as part of "polish" — it's locked.
- Move the prepend into `render.go::renderGuidance` only. Then `SynthesizeImmediateWorkflow` callers (export, strategy push-git) wouldn't get prefixes, breaking aggregator consistency.

---

### B1 — Fix #15: `zerops_import` accepts `project.envVariables`

**Status**: [ ] not shipped — _commit pending_

#### Goal

Single `zerops_import` call handles import YAML containing `project: { envVariables: {...} }`. Tool extracts project envs, sets them via the existing project-env path, then proceeds with the services-only import. Other `project.*` fields (preprocessor, scaling, name, etc.) remain rejected with a specific error pointing at the right tool.

#### Friction (what an agent observes today)

Agent submits a YAML with both `project.envVariables` and `services:`. `zerops_import` rejects with `IMPORT_HAS_PROJECT: import YAML must not contain a 'project:' section`. Agent must split: call `zerops_env action=set project=true variables=[...]` first, then re-submit YAML with just `services:`. Two-step workflow, two failure modes (forget step 1 → services deploy with missing envs → app crashes at startup).

#### Root cause / current state

In `internal/ops/import.go`, near line 90:

```go
// Check for project: key — K12 in the validation-plumbing plan. The
// platform's projectImportInvalidParameter for this case is generic;
// the specific code IMPORT_HAS_PROJECT is clearer.
if _, ok := doc["project"]; ok {
    return nil, platform.NewPlatformError(
        platform.ErrImportHasProject,
        "import YAML must not contain a 'project:' section",
        "Remove the 'project:' section. Import works within an existing project.",
    )
}
```

Was deliberate (cleaner error than generic platform parse error), but the audit shows it imposes a real two-step burden when project envs are common (Laravel, Rails, Django, Next.js secrets).

#### Files to change

| File | Why |
|---|---|
| `internal/ops/import.go` | Replace blanket rejection with whitelist+extract logic |
| `internal/tools/import.go` | Update tool description (no longer "must not contain project:") |
| `internal/ops/import_test.go` | Convert old rejection test to acceptance; add unsupported-fields test |
| `internal/platform/errors.go` | (verify `ErrImportHasProject` is still used elsewhere; if not, can repurpose) |

#### Required change

In `internal/ops/import.go::Import` function, replace the `project:` rejection block (around line 90-99) with:

```go
// Process project: block if present. Whitelist policy: only
// `envVariables` is supported at import time — apply via internal
// EnvSet, then strip the project: key before passing to the API.
// Other project.* fields (preprocessor, scaling, name, ...) are
// rejected with a specific error: full project YAML belongs to
// project creation (zcli or web UI), not zerops_import which
// operates inside an existing project.
projectBlock, hasProject := doc["project"].(map[string]any)
if hasProject {
    // Reject unsupported keys first — fail fast before any side effects.
    for key := range projectBlock {
        if key != "envVariables" {
            return nil, platform.NewPlatformError(
                platform.ErrImportHasProject,
                fmt.Sprintf("project.%s is not supported at import time", key),
                "Only project.envVariables is supported in zerops_import. Remove project."+key+" — full project YAML belongs to project creation (zcli or web UI). For env vars, project.envVariables here OR zerops_env action=set project=true.",
            )
        }
    }

    // Apply project.envVariables via the existing EnvSet path.
    if envVarsRaw, ok := projectBlock["envVariables"].(map[string]any); ok && len(envVarsRaw) > 0 {
        pairs := make([]string, 0, len(envVarsRaw))
        for k, v := range envVarsRaw {
            pairs = append(pairs, fmt.Sprintf("%s=%v", k, v))
        }
        sort.Strings(pairs) // deterministic ordering for tests
        if _, err := EnvSet(ctx, client, projectID, "", true, pairs); err != nil {
            return nil, fmt.Errorf("apply project envVariables from import: %w", err)
        }
    }

    // Strip project: from doc before re-marshaling for API call.
    delete(doc, "project")

    // Re-marshal to apply the strip.
    remarshaled, err := yaml.Marshal(doc)
    if err != nil {
        return nil, platform.NewPlatformError(
            platform.ErrInvalidImportYml,
            fmt.Sprintf("re-marshal after project: strip: %v", err),
            "Report this as a zcp bug.",
        )
    }
    yamlContent = string(remarshaled)
}
```

Add `"sort"` to imports if not already present.

If yaml.Marshal returns differently-ordered keys than the input had, the existing override-injection logic (lower in the function) needs to operate on the same `doc`. Order it: do project handling FIRST, override injection SECOND, sole remarshaling at the end. Inspect the override block's current location — if it's after the rejection point, this rearrangement is straightforward; if it's before, restructure carefully so there's only one `yaml.Marshal` round-trip.

In `internal/tools/import.go`, update the tool's `Description` to reflect the new behavior:

> "REQUIRES active workflow (zerops_recipe for recipe authoring, or zerops_workflow bootstrap/develop). Import services from YAML into the project. Supports an optional `project.envVariables` block — those vars are applied at the project level before services are created. Other `project.*` fields (preprocessor, scaling, name) are rejected with a specific error. The Zerops API validates fields, modes, types, and hostnames server-side and returns structured apiMeta on the error response when anything is wrong. Blocks until all processes complete; returns final statuses (FINISHED/FAILED)."

Add a new field to `ImportResult` for caller visibility:

```go
type ImportResult struct {
    // ... existing fields ...
    ProjectEnvsSet []string `json:"projectEnvsSet,omitempty"` // keys applied from project.envVariables
}
```

Populate it after the EnvSet call (just track which keys were sent).

#### Pinning tests (full Go code)

Update `internal/ops/import_test.go`. The existing rejection test (find via `grep -n 'ErrImportHasProject\|project:' internal/ops/import_test.go`) needs to flip:

```go
// TestImport_ProjectEnvVariables_AppliedInline verifies the new
// whitelist behavior: project.envVariables in import YAML is applied
// via the project-env channel before service creation, eliminating
// the historical 2-step "set project envs, then import services"
// workflow. See plans/multi-runtime-audit-followup.md §5 Fix B1.
func TestImport_ProjectEnvVariables_AppliedInline(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock()
	// Configure mock to capture project env writes — implementation-
	// specific to your platform.Mock; adapt to whatever capture API
	// it provides. Conceptually: assert SetProjectEnv called with
	// the expected keys.

	yaml := `project:
  envVariables:
    APP_KEY: secret123
    DB_PASSWORD: hunter2
services:
  - hostname: db
    type: postgresql@17
    mode: NON_HA
`
	result, err := Import(context.Background(), mock, "proj-1", yaml, "", false)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	wantKeys := []string{"APP_KEY", "DB_PASSWORD"}
	if !reflect.DeepEqual(result.ProjectEnvsSet, wantKeys) {
		t.Errorf("ProjectEnvsSet = %v, want %v", result.ProjectEnvsSet, wantKeys)
	}
	// Assert the mock recorded the project env writes with both keys.
	// (Specific assertion depends on your mock's recording API.)
}

func TestImport_ProjectNonEnvFields_Rejected(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock()

	yaml := `project:
  envVariables:
    APP_KEY: secret
  preprocessor:
    enabled: true
services:
  - hostname: db
    type: postgresql@17
    mode: NON_HA
`
	_, err := Import(context.Background(), mock, "proj-1", yaml, "", false)
	if err == nil {
		t.Fatal("expected rejection for project.preprocessor, got success")
	}
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrImportHasProject {
		t.Errorf("expected ErrImportHasProject, got: %v", err)
	}
	if !strings.Contains(err.Error(), "project.preprocessor") {
		t.Errorf("error should name the offending key, got: %v", err)
	}
}
```

(Adapt mock interactions to whatever interface `platform.NewMock` exposes in the codebase; the Discoverable assertion is the key behavior.)

#### Verification

1. `go test ./internal/ops -run TestImport_Project -count=1` → both new tests pass.
2. Old rejection test fails after refactor — that's expected; rename/repurpose it.
3. `go test ./... -count=1 -short` → green.
4. (Optional) Live test on eval-zcp: submit a YAML with project.envVariables to `zerops_import`, then `zerops_env action=get project=true` to verify keys persisted.

#### Done criteria

- [ ] `Import` extracts `project.envVariables`, applies via `EnvSet`, strips `project:` before API call
- [ ] Other `project.*` fields rejected with `ErrImportHasProject` and key-naming detail
- [ ] `ImportResult.ProjectEnvsSet` populated
- [ ] Tool description updated
- [ ] Two new tests pass (acceptance + non-env rejection)
- [ ] Old blanket-rejection test removed or repurposed
- [ ] One commit, message format `import: accept project.envVariables, reject other project.* fields`

#### Locked decisions for THIS fix

- **Whitelist `envVariables` only**. Other project.* fields (preprocessor, scaling, name) belong to project creation flow. Don't expand the whitelist without new evidence.
- **EnvSet first, then strip, then services**. If EnvSet fails partially, abort the whole import — services should not deploy when their expected project envs didn't all land.
- **Sort env pair keys before EnvSet**. Map iteration order is nondeterministic; tests would flake.

#### DO NOT

- Accept other `project.*` fields by silently ignoring them. The reject-with-specific-message is the discoverable failure mode.
- Skip the deterministic-ordering sort. One flaky test wastes more time than the sort costs.
- Bundle this with Fix #7 (project-vars atom) into one commit. Two distinct concerns: tool behavior (B1) and dispatch-brief knowledge (B2).

---

### B2 — Fix #7: `develop-project-env-vars` atom + extend env-channels

**Status**: [ ] not shipped — _commit pending_

#### Goal

Document project-level env var auto-injection so agents stop redeclaring project secrets in `zerops.yaml::run.envVariables` with cross-service `${KEY}` syntax (which fails silently).

#### Friction (what an agent observes today)

Agent at scaffold-yaml step writes:
```yaml
run:
  envVariables:
    APP_KEY: ${APP_KEY}
```
expecting `${APP_KEY}` to resolve to the project-level `APP_KEY` they set via `zerops_env`. Reality: `${...}` is exclusively cross-service syntax (`${hostname_KEY}`). Resolver looks for service `APP` with key `KEY`, doesn't find it, leaves the literal `${APP_KEY}` in the env. Laravel crashes at startup with "Unsupported cipher or incorrect key length".

Universal pattern: every framework with project secrets (Rails SECRET_KEY_BASE, Django DJANGO_SECRET_KEY, Next.js NEXTAUTH_SECRET, Symfony APP_SECRET).

#### Root cause

Atom corpus has:
- `develop-env-var-channels.md` — channels (service-level / run.envVariables / build.envVariables) and when each goes live. Doesn't mention project-level.
- `develop-first-deploy-env-vars.md` — managed-service env keys for cross-service refs (`${hostname_KEY}` syntax explained).
- `bootstrap-env-var-discovery.md` — discovery during bootstrap.

**No atom documents that project-level env vars auto-inject into all containers without redeclaration**. Knowledge gap.

#### Files to change

| File | Why |
|---|---|
| `internal/content/atoms/develop-project-env-vars.md` | NEW atom — full content below |
| `internal/content/atoms/develop-env-var-channels.md` | EXTEND — add project-level row to the table |

#### Required change

**Create** `internal/content/atoms/develop-project-env-vars.md` with this exact content:

```markdown
---
id: develop-project-env-vars
priority: 2
phases: [develop-active]
deployStates: [never-deployed, deployed]
title: "Project-level env vars — set once, available everywhere"
references-fields: []
---

### Project-level env vars auto-inject into containers

When you set a project-level env var via `zerops_env action="set"
project=true KEY=value`, the platform stores it once and makes it
available in every container (runtime service) in the project —
automatically, without redeclaration.

**Do NOT redeclare project envs in `zerops.yaml::run.envVariables`.**
Service-level redeclaration shadows the project value, and future
project-env updates won't apply to the shadowed service. See the
shadow-loop pitfall in `develop-env-var-channels`.

**`${...}` syntax is for cross-service references only**, in the form
`${hostname_KEY}` (e.g. `${db_connectionString}`, `${redis_password}`).
Project-level vars are NOT accessed this way; they're auto-available
by their bare name. Writing `APP_KEY: ${APP_KEY}` in your zerops.yaml
makes the resolver hunt for a service named `APP` with key `KEY`,
fail, and leave the literal string `${APP_KEY}` in the env — your
app then crashes at startup with a malformed-secret error.

**Pattern (correct):**
```
zerops_env action="set" project=true APP_KEY=<@generateRandomString(<32>)>
```
Every runtime service automatically has `APP_KEY` available at
startup. No `envVariables:` entry needed in `zerops.yaml`.

**Verification:** `ssh {hostname} "printenv | grep YOUR_KEY"` —
auto-injected value appears even when `zerops.yaml` never mentions it.
```

**Edit** `internal/content/atoms/develop-env-var-channels.md`. Find the channels table:

```markdown
| Channel | Set with | When live |
|---|---|---|
| Service-level env | `zerops_env action="set"` | Response's `restartedServices` lists hostnames whose containers were cycled; `restartedProcesses` has platform Process details. |
| `run.envVariables` | Edit `zerops.yaml`, commit, deploy | Full redeploy. `zerops_manage action="reload"` does NOT pick them up. |
| `build.envVariables` | Edit `zerops.yaml`, commit, deploy | Next build uses them; not visible at runtime. |
```

Add a new row at the top (project-level is the broadest scope, top of table is appropriate):

```markdown
| Project-level env | `zerops_env action="set" project=true` | Stored immediately; auto-injected into every runtime container at start/restart. No redeclaration in `zerops.yaml` needed — see `develop-project-env-vars`. |
```

#### Pinning test

The existing `TestAtomAuthoringLint` at `internal/content/atoms_lint.go` (or the equivalent test runner) covers frontmatter validation, axis enum checks, references-fields integrity, and atom-references integrity. The new atom passes all of these (no `references-fields` declared, frontmatter is valid, axes are real values).

No additional pinning test required — the lint suite is the gate.

If you want stronger pinning, add to a workflow integration test:

```go
// In internal/workflow/synthesize_test.go or similar
func TestProjectEnvVarsAtom_FiresOnDevelopActive(t *testing.T) {
	t.Parallel()
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatal(err)
	}

	env := StateEnvelope{
		Phase: PhaseDevelopActive,
		Services: []ServiceSnapshot{{
			Hostname:     "app",
			Bootstrapped: true,
			Deployed:     false,
		}},
	}
	bodies, err := Synthesize(env, corpus)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range bodies {
		if strings.Contains(b, "develop-project-env-vars") {
			found = true
			break
		}
	}
	if !found {
		t.Error("develop-project-env-vars atom did not fire on develop-active first-deploy envelope")
	}
}
```

(After A3 ships and IDs are in headers, the assertion can simply be `strings.Contains(b, "=== develop-project-env-vars ===")`.)

#### Verification

1. `go test ./internal/content -count=1` (or wherever atom lint lives) → green.
2. `go test ./internal/workflow -count=1` → green (atom corpus loads cleanly).

#### Done criteria

- [ ] `develop-project-env-vars.md` created with the exact content above
- [ ] `develop-env-var-channels.md` table has the new project-level row
- [ ] Atom lint passes
- [ ] (Optional) Workflow integration test added
- [ ] One commit, message format `atoms(develop): document project-level env auto-injection`

#### Locked decisions for THIS fix

- **Two atom edits, not one consolidated**. Channels atom is the right place for the row (channel summary). Standalone atom is the right place for the deeper "do not redeclare in zerops.yaml" warning + verification command. Both fire when relevant — no duplication.
- **`deployStates: [never-deployed, deployed]`** on the new atom. Project envs are relevant after deploy too (rotating secrets, adding new ones). Not gated to first-deploy only.
- **`references-fields: []` (empty)**. Atom is purely conceptual; doesn't reference Go field shapes.

#### DO NOT

- Merge the new atom into `develop-env-var-channels.md`. The atom is dedicated content; merging dilutes both.
- Change the priority to 1 to surface earlier. The default surface ordering by phase + axis filtering already places it correctly.

---

### B3 — Fix #17: `source` annotation in `zerops_discover`

**Status**: [ ] not shipped — _commit pending_

#### Goal

`zerops_discover includeEnvs=true` returns each env var with a `source` field indicating where the value originates: `"platform"` (auto-injected by service type), `"user-project"` (user-set via `zerops_env project=true`), or `"user-service"` (user-set via service-level `zerops_env`). Backward compatible — existing consumers ignore unknown fields.

#### Friction (what an agent observes today)

Agent runs `zerops_discover service=db includeEnvs=true` and gets a flat list:
```
"envs": [
  {"key": "hostname", "value": "..."},
  {"key": "password", "value": "..."},
  {"key": "MY_CUSTOM_KEY", "value": "..."},
  {"key": "zeropsSubdomain", "value": "..."},
]
```

No way to mechanically distinguish platform-injected (`hostname`, `password`) from user-set (`MY_CUSTOM_KEY`). Agent must mentally categorize each — slow reading, wasted cognition, especially on services with many envs.

#### Files to change

| File | Why |
|---|---|
| `internal/ops/platform_envs.go` (NEW) | Hardcoded data table of platform-injected key sets per service-type-version |
| `internal/ops/discover.go` | `attachEnvs`, `attachProjectEnvs` thread `serviceType` and `projectEnvKeys` to env-map building |
| `internal/ops/helpers.go` (or wherever `envVarsToMaps` lives) | Add `source` annotation logic |
| `internal/ops/discover_test.go` | New tests covering the three source values |

#### Required change

**Create** `internal/ops/platform_envs.go`:

```go
package ops

// platformManagedKeys maps service-type-version (e.g. "postgresql@17")
// to the set of env var keys that the platform auto-injects for that
// type. Used by `envVarsToMaps` to annotate each entry with
// `source: "platform"` vs user-set sources.
//
// Truth table verified live on Zerops platform 2026-04-25 (commit
// d448325 ran a platform-verifier agent that provisioned each managed
// type and recorded the keys exposed). RabbitMQ omitted —
// `serviceStackTypeVersionIsNotActive` at the time of verification.
// MySQL/Redis/MongoDB intentionally absent — they don't exist in the
// Zerops platform schema; earlier atom guidance referenced them
// mistakenly.
//
// New service types or versions: extend this map; the live truth is
// the API's GetServiceEnv response for a freshly-provisioned service
// of that type with no user-set vars. See plans/multi-runtime-audit-
// followup.md §3.6 for the canonical truth table.
var platformManagedKeys = map[string]map[string]bool{
	// PostgreSQL — versions 13..17 share the same key set as of 2026-04-25.
	"postgresql@13": pgKeys, "postgresql@14": pgKeys, "postgresql@15": pgKeys,
	"postgresql@16": pgKeys, "postgresql@17": pgKeys,

	"mariadb@10": mariadbKeys,

	"valkey@7": valkeyKeys,
	"keydb@6":  keydbKeys,

	"nats@2": natsKeys,

	"kafka@3": kafkaKeys, // no connectionString — build from hostname:port

	"clickhouse@23": clickhouseKeys,

	"elasticsearch@8": elasticsearchKeys,

	"meilisearch@1": meilisearchKeys,

	"typesense@0": typesenseKeys,

	"qdrant@1": qdrantKeys,

	"object-storage": objectStorageKeys,
	"shared-storage": sharedStorageKeys,
}

// Reusable key sets — share across versions where the platform exposes
// identical envs.
var (
	pgKeys = map[string]bool{
		"connectionString": true, "connectionTlsString": true,
		"hostname": true, "port": true, "portTls": true,
		"user": true, "password": true,
		"superUser": true, "superUserPassword": true,
		"dbName": true,
	}
	mariadbKeys = map[string]bool{
		"connectionString": true, "hostname": true, "port": true,
		"user": true, "password": true, "dbName": true,
	}
	valkeyKeys = map[string]bool{
		"connectionString": true, "connectionTlsString": true,
		"hostname": true, "port": true, "portTls": true,
	}
	keydbKeys = map[string]bool{
		"connectionString": true, "hostname": true, "port": true,
	}
	natsKeys = map[string]bool{
		"connectionString": true, "hostname": true, "port": true,
		"portManagement": true, "user": true, "password": true,
	}
	kafkaKeys = map[string]bool{
		"hostname": true, "port": true, "user": true, "password": true,
	}
	clickhouseKeys = map[string]bool{
		"connectionString": true, "hostname": true,
		"port": true, "portHttp": true, "portMysql": true,
		"portNative": true, "portPostgresql": true,
		"user": true, "password": true,
		"superUser": true, "superUserPassword": true,
		"dbName": true, "clusterName": true,
	}
	elasticsearchKeys = map[string]bool{
		"connectionString": true, "hostname": true, "port": true,
		"user": true, "password": true,
	}
	meilisearchKeys = map[string]bool{
		"connectionString": true, "hostname": true, "port": true,
		"masterKey": true, "defaultAdminKey": true,
		"defaultSearchKey": true, "defaultReadOnlyKey": true,
		"defaultChatKey": true,
	}
	typesenseKeys = map[string]bool{
		"connectionString": true, "hostname": true, "port": true, "apiKey": true,
	}
	qdrantKeys = map[string]bool{
		"connectionString": true, "grpcConnectionString": true,
		"hostname": true, "port": true, "grpcPort": true,
		"apiKey": true, "readOnlyApiKey": true,
	}
	objectStorageKeys = map[string]bool{
		"apiUrl": true, "apiHost": true, "bucketName": true,
		"accessKeyId": true, "secretAccessKey": true,
		"quotaGBytes": true, "hostname": true,
	}
	sharedStorageKeys = map[string]bool{
		"hostname": true,
	}
	// Runtime services: the platform injects zeropsSubdomain (when subdomain
	// access enabled) and a few zcp-managed keys. We don't pre-list them
	// here — runtime envs are dominated by user-set vars; mis-classification
	// risk is low. If needed later, add a runtimeManagedKeys set keyed by
	// runtime base (nodejs, python, etc.).
	zeropsRuntimeManagedKeys = map[string]bool{
		"zeropsSubdomain": true,
	}
)

// classifyEnvSource returns the source classification for an env key
// on a service of the given type-version, in the context of known
// project-level keys. Returns "platform", "user-project", or
// "user-service".
func classifyEnvSource(serviceType, key string, projectKeys map[string]bool) string {
	if keys, ok := platformManagedKeys[serviceType]; ok && keys[key] {
		return "platform"
	}
	if zeropsRuntimeManagedKeys[key] {
		return "platform"
	}
	if projectKeys[key] {
		return "user-project"
	}
	return "user-service"
}
```

**Modify** `internal/ops/helpers.go` (or wherever `envVarsToMaps` is defined). Find the function — it currently looks roughly like:

```go
func envVarsToMaps(envs []platform.EnvVar, includeValues bool) []map[string]any {
    // ...
}
```

Change signature to accept context, and add `source`:

```go
func envVarsToMaps(envs []platform.EnvVar, includeValues bool, serviceType string, projectKeys map[string]bool) []map[string]any {
    result := make([]map[string]any, 0, len(envs))
    for _, e := range envs {
        m := map[string]any{
            "key":    e.Key,
            "source": classifyEnvSource(serviceType, e.Key, projectKeys),
        }
        if includeValues {
            m["value"] = e.Content
        }
        // Preserve existing isReference annotation if currently emitted.
        if isCrossServiceRef(e.Content) {
            m["isReference"] = true
        }
        result = append(result, m)
    }
    return result
}
```

(Adapt `isCrossServiceRef` to the existing helper; if there isn't one, the regex `^\$\{[a-z0-9_-]+_[A-Za-z0-9_]+\}$` matches the cross-service form.)

**Update** `internal/ops/discover.go::attachEnvs` and `attachProjectEnvs` to pass the new args:

```go
func attachEnvs(ctx context.Context, client platform.Client, info *ServiceInfo, serviceID string, result *DiscoverResult, includeValues bool) []platform.EnvVar {
    envs, err := client.GetServiceEnv(ctx, serviceID)
    if err != nil {
        result.Warnings = append(result.Warnings,
            fmt.Sprintf("Failed to fetch env vars for %s: %s", info.Hostname, err.Error()))
        return nil
    }

    // Build project-key set for source classification.
    projectKeys := make(map[string]bool, len(result.Project.Envs))
    for _, penv := range result.Project.Envs {
        if k, ok := penv["key"].(string); ok {
            projectKeys[k] = true
        }
    }

    info.Envs = envVarsToMaps(envs, includeValues, info.Type, projectKeys)
    return envs
}
```

For project envs (in `attachProjectEnvs`), the source is unconditionally `"user-project"`:
```go
func attachProjectEnvs(ctx context.Context, client platform.Client, info *ProjectInfo, projectID string, result *DiscoverResult, includeValues bool) {
    envs, err := client.GetProjectEnv(ctx, projectID)
    if err != nil {
        result.Warnings = append(result.Warnings,
            fmt.Sprintf("Failed to fetch project env vars: %s", err.Error()))
        return
    }
    // Project envs always have source: "user-project" — no platform-injected
    // ones at this scope, no service-level confusion.
    info.Envs = make([]map[string]any, 0, len(envs))
    for _, e := range envs {
        m := map[string]any{
            "key":    e.Key,
            "source": "user-project",
        }
        if includeValues {
            m["value"] = e.Content
        }
        info.Envs = append(info.Envs, m)
    }
}
```

**Order matters**: project envs must be attached BEFORE service envs in `Discover`, so `attachEnvs` can reference `result.Project.Envs` for projectKeys. Find the call sites in `Discover` — there's a single-service path and a list-all path. Restructure to populate project envs first:

```go
// In list-all path (around line 99-115):
// 1. Populate project envs FIRST (list of project keys for classification)
if includeEnvs {
    attachProjectEnvs(ctx, client, &result.Project, projectID, result, includeEnvValues)
}
// 2. Then iterate services with the project-key set available
result.Services = make([]ServiceInfo, 0, len(services))
for i := range services {
    if services[i].IsSystem() {
        continue
    }
    info := buildSummaryServiceInfo(&services[i])
    if includeEnvs {
        attachEnvs(ctx, client, &info, services[i].ID, result, includeEnvValues)
    }
    result.Services = append(result.Services, info)
}
```

For the single-service path (when hostname filter is active), still fetch project envs first to populate the projectKeys map.

#### Pinning tests (full Go code)

Add to `internal/ops/discover_test.go`:

```go
func TestEnvVarsToMaps_SourceAnnotation_PlatformInjected(t *testing.T) {
	t.Parallel()
	envs := []platform.EnvVar{
		{Key: "hostname", Content: "db.zerops.io"},
		{Key: "password", Content: "secret"},
	}
	got := envVarsToMaps(envs, true, "postgresql@17", map[string]bool{})
	for _, m := range got {
		if m["source"] != "platform" {
			t.Errorf("postgresql key %q should be source=platform, got %q", m["key"], m["source"])
		}
	}
}

func TestEnvVarsToMaps_SourceAnnotation_UserProject(t *testing.T) {
	t.Parallel()
	envs := []platform.EnvVar{
		{Key: "APP_KEY", Content: "secret"},
	}
	projectKeys := map[string]bool{"APP_KEY": true}
	got := envVarsToMaps(envs, true, "nodejs@22", projectKeys)
	if got[0]["source"] != "user-project" {
		t.Errorf("APP_KEY (project key) should be source=user-project, got %q", got[0]["source"])
	}
}

func TestEnvVarsToMaps_SourceAnnotation_UserService(t *testing.T) {
	t.Parallel()
	envs := []platform.EnvVar{
		{Key: "MY_CUSTOM", Content: "val"},
	}
	got := envVarsToMaps(envs, true, "nodejs@22", map[string]bool{})
	if got[0]["source"] != "user-service" {
		t.Errorf("MY_CUSTOM (no project key, no platform key) should be source=user-service, got %q", got[0]["source"])
	}
}

func TestClassifyEnvSource_AllManagedTypes(t *testing.T) {
	t.Parallel()
	// Sample one well-known key per managed type — sanity check that
	// the platformManagedKeys table is populated and classification works.
	cases := []struct {
		serviceType, key string
	}{
		{"postgresql@17", "hostname"},
		{"mariadb@10", "connectionString"},
		{"valkey@7", "port"},
		{"kafka@3", "user"},
		{"clickhouse@23", "clusterName"},
		{"meilisearch@1", "masterKey"},
		{"qdrant@1", "grpcPort"},
		{"object-storage", "bucketName"},
		{"shared-storage", "hostname"},
	}
	for _, c := range cases {
		got := classifyEnvSource(c.serviceType, c.key, map[string]bool{})
		if got != "platform" {
			t.Errorf("%s/%s: got source=%q, want platform", c.serviceType, c.key, got)
		}
	}
}
```

#### Verification

1. `go test ./internal/ops -run TestEnvVarsToMaps -count=1` → all pass.
2. `go test ./internal/ops -run TestClassifyEnvSource -count=1` → pass.
3. `go test ./... -count=1 -short` → green.
4. (Optional) Live: `zerops_discover service=<postgres-host> includeEnvs=true` and confirm `hostname` key has `source: "platform"` in response.

#### Done criteria

- [ ] `internal/ops/platform_envs.go` created with full truth table (matching §3.6)
- [ ] `envVarsToMaps` accepts `serviceType` and `projectKeys`; emits `source` field
- [ ] `attachEnvs` and `attachProjectEnvs` thread the new args
- [ ] `Discover` populates project envs before service envs (so projectKeys is available)
- [ ] Four pinning tests pass
- [ ] One commit, message format `discover: annotate env source (platform|user-project|user-service)`

#### Locked decisions for THIS fix

- **Annotation, not restructure**. See §4 D2.
- **Hardcoded truth table**. The data is stable per service-type-version; embedding live-derived data in code is fine. If platform adds a new managed type, table needs an entry — discoverable via failing test or by API observation.
- **Project envs always `user-project`**. No platform-injected envs at project scope; no ambiguity.

#### DO NOT

- Auto-derive `platformManagedKeys` from atom corpus markdown by parsing `develop-first-deploy-env-vars.md`. The atom is documentation, not data; coupling via parser is fragile. Two source-of-truth paths is acceptable here because both are verified against the same live data.
- Restructure `Envs []map[string]any` into `Envs { platformInjected, userServiceLevel, userProjectLevel }` buckets. Locked decision §4 D2.
- Skip the project-envs-first-then-service-envs ordering in `Discover`. Without it, projectKeys is empty and `source: "user-project"` never fires for service-level envs that match a project key.

---

### C1 — Fix #8-#12: `develop-first-deploy-runtime-gotchas` atom

**Status**: [ ] not shipped — _commit pending_

#### Goal

Cover the 5 runtime-specific first-deploy frictions found in the audit (Node fresh scaffold, Next.js TS deprecation, Python --target+PYTHONPATH, Go/Rust/.NET/Java heavy compile, Ruby BUNDLE_PATH) with one consolidated atom that fires at scaffold-yaml + write-app phase.

#### Friction (what the audit observed)

5 frictions, all reproduced live on eval-zcp 2026-04-25 (Agent 3 verification, see §3.3 for Python, others described below):

1. **Node fresh scaffold**: `npm ci` fails with `EUSAGE` because no `package-lock.json` exists yet (agent only wrote `package.json`).
2. **Next.js TS 6+**: `tsc --noEmit` fails with `error TS5107: Option 'moduleResolution=node10' is deprecated and will stop functioning in TypeScript 7.0`. Hard error, not warning.
3. **Python --target**: see §3.3 — `pip install --target=./vendor` puts wheels under `./vendor/` and scripts under `./vendor/bin/`, but neither is on `PATH` or `PYTHONPATH`. Even the binary at `./vendor/bin/gunicorn` fails with `ModuleNotFoundError` unless `PYTHONPATH=./vendor` is set.
4. **Go heavy compile in `initCommands`**: a realistic weather-dashboard with mux+uuid+logrus deps takes ~19s cold to build in the runtime container (verified). Not a 30-min timeout in normal cases, but moves serving latency on every cold start; build container has cache + faster cores so `buildCommands` is the right place. Pattern applies to Rust/Java/.NET (heavier compile, larger penalty).
5. **Ruby `bundle install`**: default writes to system gem dirs (`/usr/local/lib/ruby/gems/...`) where the `zerops` user has no write permission. Build-time bundling injects `BUNDLE_PATH=vendor/bundle` automatically; interactive SSH and dev-server start commands do not.

#### Files to change

| File | Why |
|---|---|
| `internal/content/atoms/develop-first-deploy-runtime-gotchas.md` | NEW atom — full content below |
| `internal/workflow/synthesize_test.go` (or atom test file) | Pinning test that the atom fires on first-deploy and not after |

#### Required change

**Create** `internal/content/atoms/develop-first-deploy-runtime-gotchas.md` with this exact content:

```markdown
---
id: develop-first-deploy-runtime-gotchas
priority: 3
phases: [develop-active]
deployStates: [never-deployed]
title: "First-deploy gotchas — known runtime-specific traps"
references-fields: []
---

### First-deploy gotchas

These hit on first scaffold every time. Skim the row matching the
runtime of the service you're configuring; ignore the others.

| Runtime | Trap | Fix in `zerops.yaml` |
|---|---|---|
| Node, Bun | `npm ci` / `bun install --frozen-lockfile` requires a checked-in lockfile. Fresh scaffolds have only `package.json`. | First build: `npm install` (writes `package-lock.json`) or `bun install` (no `--frozen-lockfile`). Switch to `npm ci` only after committing the lockfile. |
| Next.js (TypeScript) | `next` auto-generates `tsconfig.json` with `moduleResolution: "node10"`. TypeScript 6+ rejects this with `error TS5107`. | Write `tsconfig.json` manually before the first `next build` with `"moduleResolution": "bundler"` (preferred) or `"NodeNext"`. |
| Python | `pip install --target=./vendor` puts wheels under `./vendor/` and scripts under `./vendor/bin/`, but neither is on `PATH` or `PYTHONPATH`. The shim in `./vendor/bin/gunicorn` itself fails with `ModuleNotFoundError` unless `PYTHONPATH` is set. | Set `run.envVariables.PYTHONPATH: /var/www/vendor` AND start with `python -m gunicorn ...` (no PATH dependency) — or with the explicit binary `/var/www/vendor/bin/<tool>` after PYTHONPATH is set. |
| Go, Rust, .NET, Java | Heavy compile (`go build`, `cargo build`, `dotnet publish`, `mvn package`) in `run.initCommands` runs in the runtime container, with no build cache, on every cold start. Cold builds add 20–120 s to startup. | Put compile in `build.buildCommands` (cached, faster cores) and ship the binary via `deployFiles`. `initCommands` is for fast, idempotent setup like migrations. |
| Ruby | Default `bundle install` writes to system gem dirs (`/usr/local/lib/ruby/gems/...`) where the `zerops` user has no write permission. Build-time bundling already injects `BUNDLE_PATH=vendor/bundle`; interactive `ssh` sessions and dev-server `start` commands do NOT. | Set `run.envVariables.BUNDLE_PATH: vendor/bundle` (and `build.envVariables.BUNDLE_PATH: vendor/bundle` if your build chain needs it explicitly). For interactive `ssh`, prefix the command: `BUNDLE_PATH=vendor/bundle bundle install`. |

When in doubt, query `zerops_knowledge query="<runtime>"` for the
deeper runtime guide — that doc has full setup recipes for each base.
```

#### Pinning test (full Go code)

Add to `internal/workflow/synthesize_test.go` or a new dedicated test file:

```go
// TestRuntimeGotchasAtom_FiresOnFirstDeployOnly pins the atom's axes:
// it must appear in dispatch when phase=develop-active and the
// service is bootstrapped but never-deployed; must NOT appear after
// the first deploy lands.
func TestRuntimeGotchasAtom_FiresOnFirstDeployOnly(t *testing.T) {
	t.Parallel()

	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatal(err)
	}

	// Should fire: develop-active + never-deployed.
	envBefore := StateEnvelope{
		Phase: PhaseDevelopActive,
		Services: []ServiceSnapshot{{
			Hostname:     "app",
			Bootstrapped: true,
			Deployed:     false,
		}},
	}
	bodiesBefore, err := Synthesize(envBefore, corpus)
	if err != nil {
		t.Fatal(err)
	}
	hitBefore := false
	for _, b := range bodiesBefore {
		if strings.Contains(b, "First-deploy gotchas") {
			hitBefore = true
			break
		}
	}
	if !hitBefore {
		t.Error("expected runtime-gotchas atom to fire on develop-active first-deploy envelope, got none")
	}

	// Should NOT fire: same envelope but Deployed=true.
	envAfter := envBefore
	envAfter.Services[0].Deployed = true
	bodiesAfter, err := Synthesize(envAfter, corpus)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bodiesAfter {
		if strings.Contains(b, "First-deploy gotchas") {
			t.Error("runtime-gotchas atom should NOT fire after first deploy (deployStates: [never-deployed])")
			break
		}
	}
}
```

#### Verification

1. Atom lint: `go test ./internal/content -count=1` (or wherever `TestAtomAuthoringLint` lives) → green.
2. Pinning test: `go test ./internal/workflow -run TestRuntimeGotchasAtom -count=1` → pass.
3. `go test ./... -count=1 -short` → green.
4. (Optional) Inspect a develop-active first-deploy envelope's rendered guidance to eyeball the atom appears — do `zerops_workflow status` after entering develop with a freshly bootstrapped, never-deployed service.

#### Done criteria

- [ ] `develop-first-deploy-runtime-gotchas.md` created with content above
- [ ] Atom lint passes
- [ ] Pinning test passes (fires on never-deployed, not on deployed)
- [ ] One commit, message format `atoms(develop): runtime-specific first-deploy gotchas table`

#### Locked decisions for THIS fix

- **One consolidated atom**. See §4 D1.
- **Python row teaches BOTH PYTHONPATH AND `python -m`**. See §3.3.
- **Go/Rust/.NET/Java grouped**. Same root pattern (heavy compile in initCommands), same fix (move to buildCommands). One row, same advice.
- **`priority: 3`**. Same priority tier as `develop-first-deploy-write-app.md`. Sorts by ID after scaffold-yaml@2 and before write-app — agent reads scaffold instructions, then sees gotchas, then writes app. Right ordering.
- **`deployStates: [never-deployed]` only**. After first deploy, the gotchas no longer apply (the agent has working build chain). Don't include `deployed`.

#### DO NOT

- Add a `runtimeBase` axis to AxisVector. See §4 D1.
- Split into per-runtime atoms. See §4 D1.
- Drop the Python `python -m` advice in favor of "just use binary path". See §3.3 — binary path alone fails.
- Remove the closing pointer to `zerops_knowledge`. It directs agents to deeper runtime docs that this atom intentionally doesn't duplicate.

---

## §6. Dropped (do not implement)

### Fix #6 — per-scenario eval `timeoutMinutes`

**Status**: dropped per user instruction (eval infra; not in current shipping scope).

**Rationale**: User explicitly excluded eval changes from this round.

**If reopened**: would add `TimeoutMinutes int` to `internal/eval/scenario.go::Scenario`, propagate to `RunScenario` context timeout, allow per-scenario override. ~10 lines + 1 test. Save for future eval-infra sprint.

### Fix #16 — env set 0-services message

**Status**: dropped — verified not a real bug.

**Rationale**: Stávající message at `internal/tools/env.go:178` ("No ACTIVE services needed restart. The new env value will be injected when a service starts or deploys.") accurately describes both outcomes:
- The env var is stored (no data loss).
- It will inject when a consumer service starts/deploys.

Audit attribution as "confusing during bootstrap" was overstated; the message is technically correct. Reopening requires fresh evidence that an agent took wrong action because of the wording.

---

## §7. Deferred (ship later under conditions)

### Fix #14 Phase 2 — `ServiceMeta.ZeropsSetup` field

**Status**: deferred until nonconventional recipe surfaces.

**Why deferred**: Phase 1 (A1 in this plan) covers 100% of audit-observed friction because all observed recipes follow `zeropsSetup: dev` ↔ `setup: dev` and `zeropsSetup: prod` ↔ `setup: prod`. Phase 2 adds value only for nonconventional recipes (`zeropsSetup: foo` with `setup: foo`); none observed.

**Reopen when**: a recipe with nonconventional setup name produces a deploy failure that Phase 1 fix doesn't catch.

**Shape (when needed)**:
- Add `ZeropsSetup map[string]string` to `ServiceMeta` (hostname → setup name from import YAML).
- Bootstrap import flow (`internal/workflow/bootstrap_outputs.go`, `recipe_override.go`) writes the map.
- `resolveSetupEntry` priority: explicit setup → `meta.ZeropsSetup[targetHostname]` → role fallback → hostname fallback.

### Local git-push role-based fallback parity

**Status**: deferred until cross-deploy via push-git surfaces friction.

**Why deferred**: `internal/tools/deploy_local.go` and `deploy_local_git.go` use a simpler fallback (hostname only, no role-based) than SSH deploy's `resolveSetupEntry`. Audit didn't exercise push-git cross-deploy, so the asymmetry didn't manifest.

**Reopen when**: an agent's `zerops_deploy targetService=appstage` via local-git path fails because it didn't get role-based stage→prod fallback.

**Shape (when needed)**:
- Extract `resolveSetupEntry` into a shared helper (`internal/tools/setup_resolve.go`).
- Both SSH (`deploy_preflight.go`) and local-git (`deploy_local.go`, `deploy_local_git.go`) call it.
- Test parity via shared test fixture.

---

## §8. Post-shipping validation

After all 7 forward fixes ship, run a validation pass to confirm dispatch-brief friction reduced:

1. **Build + ship the binary to eval-zcp's zcp container:**
   ```
   ./eval/scripts/build-deploy.sh
   ```

2. **Re-run 4 representative scenarios** (subset of original 11):
   ```
   ./eval/scripts/run-scenario.sh internal/eval/scenarios/weather-dashboard-python.md
   ./eval/scripts/run-scenario.sh internal/eval/scenarios/weather-dashboard-nextjs-ssr.md
   ./eval/scripts/run-scenario.sh internal/eval/scenarios/weather-dashboard-php-laravel.md
   ./eval/scripts/run-scenario.sh internal/eval/scenarios/weather-dashboard-ruby.md
   ```

3. **Aggregate**:
   ```
   python3 eval/scripts/aggregate-weather-audit.py
   ```

4. **Diff** against pre-fix audit (`eval/results/audit-multi-weather-20260425_012145.md`).

**Expected outcomes**:
- C-bucket count drops on `develop-dev-server-triage`, `develop-dev-server-reason-codes`, `develop-ready-to-deploy` (already-shipped fixes #2-#4).
- New atoms `develop-project-env-vars` and `develop-first-deploy-runtime-gotchas` appear in dispatch with their canonical IDs (Fix #18 makes IDs visible).
- Ruby scenario completes first-try without dev-server env-prefix retry (Fix #13).
- Cross-deploy `apidev → apistage` test resolves to `prod` setup without explicit `setup=` (Fix #14).
- `zerops_import` accepts YAML with `project.envVariables` block (Fix #15).
- `zerops_discover` response shows `source` field per env entry (Fix #17).

**If validation passes**: move this file to `plans/archive/multi-runtime-audit-followup.md` with final commit.

**If anything regresses**: do NOT roll back fixes individually. Investigate the regression's root cause; if it's a fix-introduced bug, ship a follow-up commit. The pinning tests should catch most regressions before validation.

---

## §9. Updating this document

**When a fix ships**:
1. Update §2's status row: tick `[x]`, paste commit hash.
2. Update the fix's `Status:` line in §5.
3. If shipping revealed a new locked decision worth preserving, add to §4.
4. If shipping revealed an empirical fact future sessions need, add to §3.
5. If shipping caught a problem in this plan (wrong file path, drifted line number, missed test case), correct in place — the doc is authoritative for forward sessions.

**When all 7 ship**:
- Run §8 validation.
- If green, `git mv plans/multi-runtime-audit-followup.md plans/archive/`.
- Final commit message: `archive(plans): runtime-audit-followup complete (7 fixes shipped)`.

**When in doubt about scope**:
- §4 locked decisions are authoritative. They override new ideas unless backed by fresh data.
- §3 empirical facts are ground truth. If a fix conflicts with a fact, the fix is wrong, not the fact.
- §5 fix specs are the shipping plan. Don't depart from them without updating §5 first.
