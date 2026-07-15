# Capture + MCP Inspector — audit and code-review plan

Date: 2026-07-15

Status: executed on 2026-07-15; final verdict **NO-GO**; no production-code changes were made

Review packet: `plans/capture-inspector-audit-2026-07-15/09-final-verdict.md`

Gitignored evidence: `tmp/capture-inspector-audit-2026-07-15/`

Review target:

```text
base:       8f1ef6f29e3b70809a43dbbe6363f0e8dafa03d3 (v9.126.0)
audit head: b5bd502ad8ab31d854bb938460612936e221887c (feat/capture-raw-prototype)
comparison: git diff 8f1ef6f2...b5bd502a
```

Primary handoff:
`plans/capture-inspector-technical-handoff-2026-07-14.md`

Authoritative feature contract:
`docs/spec-capture-inspector.md`

## 1. Goal and required verdicts

Audit the complete feature diff and answer four questions independently:

1. **Spec fidelity:** does the implementation satisfy every capture/inspector
   contract without silently implementing a weaker or different behavior?
2. **Isolation and non-interference:** when capture and the UI are not selected,
   are normal ZCP CLI, MCP, workflow, and eval behavior unchanged? When capture
   is selected, is every observer effect bounded to the declared opt-in side
   channel?
3. **Correctness and security:** are raw evidence, lifecycle state, derived
   claims, local HTTP security, plaintext handling, and failure recovery
   trustworthy under malformed input, concurrency, interruption, and hostile
   local files?
4. **Codebase design:** are the module interfaces and seams deep, local, and
   testable, or did the feature create coupling and shallow abstractions that
   will spread into the rest of ZCP?

The audit must produce a separate verdict for each question. A pass on one axis
must never hide a failure on another.

## 2. Critical scope distinction

The diff contains two related but architecturally different domains. They must
not be reviewed as one undifferentiated "Inspector" feature.

### 2.1 Cold browser-inspector domain

```text
cmd/zcp/capture_ui.go
  -> internal/captureinspector facade
    -> internal/captureinspector/internal/web
      -> internal/captureinspector/internal/projection
```

The intended contract is a compiler-fenced, CLI-only, read-only module. Only
`cmd/zcp/capture_ui.go` may compose it. Importing it must be cold.

### 2.2 Shared capture side channel

```text
internal/server   -> internal/capture (MCP byte recording)
internal/workflow -> internal/capture (composition provenance)
internal/eval     -> internal/capture (lifecycle, MCP config, artifacts)
cmd/zcp           -> internal/capture (manager, daemon, raw/inspect CLI)
```

`internal/capture` is intentionally reachable from normal ZCP packages, so the
facade/import checks alone do **not** prove whole-feature isolation. Its disabled
path, environment opt-in, failure behavior, transitive dependency graph, and
model/MCP byte transparency require their own runtime and differential audit.

This distinction is the central review constraint.

## 3. Fixed review set

The review is against the frozen hash `b5bd502a`, not a moving branch name.
Before any work:

```bash
cd /Users/macbook/Documents/Zerops-MCP/zcp-capture-raw
BASE=8f1ef6f29e3b70809a43dbbe6363f0e8dafa03d3
AUDIT_HEAD=b5bd502ad8ab31d854bb938460612936e221887c

git cat-file -e "$AUDIT_HEAD^{commit}"
test "$(git merge-base "$BASE" "$AUDIT_HEAD")" = "$BASE"
git log --oneline "$BASE..$AUDIT_HEAD"
git diff --stat "$BASE...$AUDIT_HEAD"
git diff --check "$BASE...$AUDIT_HEAD"
git status --short # record concurrent plan/report work; never delete it
```

Expected feature sequence:

```text
8a6314b3 feat(capture): add persistent raw capture inspector
13daa3b7 feat(capture): add isolated forensic inspector UI
e1eaff09 fix(capture): use page scrolling for flow map
c07fb09d fix(capture): show flow details in fixed sidebar
b5bd502a docs(capture): add technical inspector handoff
```

The aggregate review includes all 97 changed files, not only the final UI
commits or `internal/captureinspector/`.

### In scope

- every production, test, spec, lint, CLI, eval, workflow, and server change in
  `8f1ef6f2...b5bd502a`;
- canonical capture write/read paths and format validation;
- MCP and provider transparency;
- persistent manager and Claude-settings ownership/recovery;
- eval integration and disabled-path regression behavior;
- projection correctness and evidence basis;
- browser facade, HTTP server, assets, and optional Playwright harness;
- static and runtime isolation from the rest of ZCP;
- the four local real captures as read-only acceptance evidence;
- known unmeasured areas when deciding whether they are acceptable limitations
  or release blockers.

### Out of scope unless the audit proves a direct dependency

- fixing behavioral issues observed *inside* captured evals;
- semantic grading or a quality score;
- adding Codex/pi/provider-neutral adapters;
- unrelated changes on `feat/anthropic-proxy-capture` or the research branch;
- a separate binary, build tags, database, or scale redesign without measured
  evidence that the current seam cannot meet the contract;
- release, install, `/usr/local/bin/zcp` replacement, or live platform mutation.

The older branches may be consulted for history only; they are not part of the
review diff and must not be accidentally merged into findings.

## 4. Sources and review method

### 4.1 Authority order

Use the repository's authority order:

1. tests for behavior;
2. code for implementation;
3. `docs/spec-capture-inspector.md` and `docs/spec-architecture.md` for design;
4. plans/handoff only as transient intent, inventory, and claimed evidence.

The handoff is not proof that a command passed or a contract holds. Reproduce
claims independently.

Read before reviewing:

```text
AGENTS.md
CLAUDE.md
CLAUDE.local.md (machine policy; do not edit)
docs/spec-capture-inspector.md
docs/spec-architecture.md
docs/spec-testing-architecture.md
plans/capture-inspector-technical-handoff-2026-07-14.md
plans/capture-inspector-v1-isolation-2026-07-13.md
plans/capture-inspector-gui-2026-07-13.md
plans/capture-inspector-session-trace-2026-07-13.md
plans/research/capture-inspector-roadmap-2026-07-13.md
```

### 4.2 External review guidance

The plan adopts the useful parts of Matt Pocock's engineering skills, pinned to
repository commit `e9fcdf95b402d360f90f1db8d776d5dd450f9234`:

- [`skills/engineering/code-review`](https://github.com/mattpocock/skills/tree/e9fcdf95b402d360f90f1db8d776d5dd450f9234/skills/engineering/code-review): fixed comparison point, independent
  **Standards** and **Spec** reviews, reported separately;
- [`skills/engineering/codebase-design`](https://github.com/mattpocock/skills/tree/e9fcdf95b402d360f90f1db8d776d5dd450f9234/skills/engineering/codebase-design): module/interface/seam/adapter vocabulary,
  depth, leverage, locality, deletion test, and interface-as-test-surface;
- `DEEPENING.md`: dependency categories and replace-don't-layer test strategy;
- `DESIGN-IT-TWICE.md`: only if evidence justifies moving a seam.

The Standards review includes the smell baseline as judgment calls:
Mysterious Name, Duplicated Code, Feature Envy, Data Clumps, Primitive
Obsession, Repeated Switches, Shotgun Surgery, Divergent Change, Speculative
Generality, Message Chains, Middle Man, and Refused Bequest. Repository rules
win over a generic smell; tooling-enforced style is not re-reported manually.

### 4.3 Independent review lanes

Run these in separate reviewer contexts so conclusions do not anchor one
another:

1. **Standards/diff lane** — repository rules, correctness smells, scope creep;
2. **Spec lane** — requirement-by-requirement implementation fidelity;
3. **Isolation lane** — dependency, reachability, disabled-path, side effects;
4. **Security/evidence lane** — local threat model and forensic trust;
5. **Design lane** — module depth, seam placement, locality, test surface.

Preserve each raw report. Only after all lanes finish may a synthesis group
shared root causes or duplicate findings.

## 5. Audit evidence and artifacts

Execution should create a transient review packet under:

```text
plans/capture-inspector-audit-2026-07-15/
  00-baseline.md
  01-contract-matrix.md
  02-standards-review.md
  03-spec-review.md
  04-isolation-review.md
  05-security-evidence-review.md
  06-codebase-design-review.md
  07-test-coverage-review.md
  08-finding-ledger.md
  09-final-verdict.md
```

Large command logs, coverage profiles, screenshots, generated captures, tokens,
and plaintext evidence remain gitignored under
`tmp/capture-inspector-audit-2026-07-15/`. Do not copy capability URLs, reveal
cookies, provider credentials, prompt bodies, or raw captured content into a
tracked report.

Every finding must include:

```text
ID and severity
review lane
status: candidate | confirmed | refuted | accepted limitation
feature-introduced vs pre-existing
file:line / symbol / commit
quoted spec or repository rule when applicable
observable impact and isolation blast radius
minimal deterministic reproduction
existing test and why it does not catch the issue
root cause, not only symptom
smallest safe correction and required RED test
```

Severity:

- **Blocker:** can corrupt MCP stdout/control flow, route credentials unsafely,
  mutate unrelated user state, falsify evidence integrity, bypass reveal/read
  isolation, or activate from the normal disabled path.
- **Major:** wrong lifecycle/correlation result, silent loss, unrecoverable
  manager state, material security/availability problem, or broad coupling that
  makes normal ZCP behavior depend on Inspector internals.
- **Minor:** bounded correctness, operability, maintainability, or test gap with
  a safe workaround.
- **Note:** design observation or explicitly accepted/unmeasured limitation.

Default to **refute**: no candidate becomes a finding until a second pass reads
its callers, callees, sibling paths, tests, and contract and reproduces the
impact.

## 6. Phase A — freeze, baseline, and differential setup

### A1. Record environment

Record Go, Node/npm, OS/arch, browser harness version, dependency graph, branch
hashes, and whether the local corpus exists. Record only presence/versions for
credentials, never values.

### A2. Create clean detached controls

Use temporary detached worktrees for the base and audited head so baseline and
feature behavior can be run under identical environment without changing the
feature worktree:

```bash
git worktree add --detach /tmp/zcp-capture-audit-base "$BASE"
git worktree add --detach /tmp/zcp-capture-audit-head "$AUDIT_HEAD"
```

Build only temporary binaries:

```bash
(cd /tmp/zcp-capture-audit-base && go build -trimpath -o /tmp/zcp-audit-base ./cmd/zcp)
(cd /tmp/zcp-capture-audit-head && go build -trimpath -o /tmp/zcp-audit-head ./cmd/zcp)
```

Run both from isolated temporary `HOME`, `CLAUDE_CONFIG_DIR`, working directory,
state directory, and capture root. No probe may read or edit the operator's real
Claude settings.

### A3. Baseline both revisions

Run the same applicable build/test/lint commands on base and feature. A failure
that reproduces on base is classified separately; it cannot excuse a new
feature failure, but it is not attributed to this diff.

Capture:

- full command, exit code, duration, and concise failure;
- test count and skipped tests;
- binary size and linked non-stdlib package list;
- open known local prerequisites rather than disguising them as code failures.

### A4. Protect the real corpus

Before any real-capture inspection, hash every canonical file. Re-hash after
CLI inspection, every browser route traversal, and shutdown. Any byte, mode,
name, or mtime mutation is a Blocker candidate. Screenshots after reveal stay
in the gitignored audit directory.

**Gate A:** hashes are frozen, base/head controls build, worktrees are clean,
and baseline failures are classified before code reading begins.

## 7. Phase B — inventory and contract traceability

### B1. Build a change inventory

Classify every changed production file into one owner:

| Area | Primary paths |
|---|---|
| CLI/process composition | `cmd/zcp/capture*.go`, `cmd/zcp/main.go`, `cmd/zcp/eval*.go` |
| recorder/proxy/MCP | `internal/capture/{recorder,proxy,mcp}.go` |
| lifecycle/manager/settings | `internal/capture/{runtime,control,manager,claude_settings,manifest}.go` |
| inspection/correlation | `internal/capture/inspect*.go` |
| eval/workflow adapters | `internal/eval/**`, `internal/workflow/bootstrap_guide_*` |
| inspector facade | `internal/captureinspector/inspector.go` |
| private projection | `internal/captureinspector/internal/projection/**` |
| local HTTP/UI | `internal/captureinspector/internal/web/**` |
| enforcement | `.golangci.yaml`, architecture and transport tests |

For each area record exported interface, callers, I/O, goroutines, global state,
environment keys, files, sockets, process control, and error ownership.

### B2. Draw actual graphs, not intended graphs

Generate and inspect:

- direct and transitive Go import graphs for `internal/server`,
  `internal/capture`, and `internal/captureinspector`;
- all imports of both `internal/capture` and `internal/captureinspector`;
- package initialization declarations and embedded assets;
- runtime entrypoints from `main` command dispatch;
- all capture environment reads/writes;
- all stdout/stderr, listener, goroutine, `os/exec`, signal, settings, and file
  mutation sites;
- all canonical read and write call sites.

Do not equate a direct-import allowlist with a transitive-dependency guarantee.
In particular, explain the consequences of `internal/capture` importing other
ZCP packages while also being imported by `internal/server`.

### B3. Build a requirement matrix

Give every normative statement in `docs/spec-capture-inspector.md` a local audit
ID. Matrix columns:

```text
requirement | implementation owner | positive test | negative test |
failure-path test | runtime evidence | status | gap/finding ID
```

Include all seven product invariants and the lifecycle, adapter, eval, bundle,
inspection, projection, browser, running-capture, isolation, compatibility, and
non-goal sections. Handoff assertions get a separate "claimed verification"
column; they do not replace evidence.

**Gate B:** every changed file has one review owner, every new interface has all
callers mapped, and every normative spec statement appears exactly once in the
matrix.

## 8. Phase C — independent Standards and Spec code reviews

### C1. Standards review

Review `git diff "$BASE...$AUDIT_HEAD"` per file/hunk against:

- `CLAUDE.md` architecture, TDD, error wrapping, global state, I/O, stdout,
  concurrency, and English rules;
- `docs/spec-architecture.md` and `docs/spec-testing-architecture.md`;
- `.golangci.yaml` dependency policy;
- the smell baseline in §4.2;
- Go/HTTP/process conventions already used by neighboring ZCP modules.

Focus on behavior and design that tooling cannot prove: swallowed errors,
partial transactions, locks around I/O, shutdown order, duplicate truth,
unbounded state, shallow wrappers, misleading names/comments, and tests that
pin implementation rather than interface behavior.

Output only Standards findings; do not use spec completeness to soften them.

### C2. Spec review

Independently walk the contract matrix and diff. Report:

- missing or partial requirements;
- implementation that contradicts the requirement;
- behavior that appears implemented but is not actually testable/reachable;
- scope creep not requested by the spec;
- claims stronger than available evidence;
- non-goals that accidentally became implicit guarantees.

Quote the spec section for each candidate. Do not report style smells here.

### C3. Preserve separation

Publish `02-standards-review.md` and `03-spec-review.md` before synthesis. Give
each its own finding count and worst issue. Do not produce one blended score.

**Gate C:** two independently produced reports exist, and every candidate is
traceable to a diff hunk plus either a repository rule or a spec requirement.

## 9. Phase D — architecture isolation and module-depth review

### D1. Compiler/import enforcement

Verify, and adversarially test, all claimed fences:

- only `cmd/zcp/capture_ui.go` imports the facade outside its subtree;
- projection/web cannot be imported from `internal/server`, another command,
  or an external module due to Go's nested `internal` rule;
- inspector outgoing imports match both AST tests and depguard;
- reverse imports from core are rejected;
- build tags, generated files, tests, aliases, dot imports, nested packages, and
  future subdirectories cannot bypass scanner globs;
- architecture scanner self-tests fail for each forbidden class, not only one
  combined fixture.

Use a throwaway worktree for intentional violations. Never leave mutation
probes in the reviewed tree.

### D2. Cold-import proof

Static AST checks are necessary but insufficient. Build two tiny temporary
control programs, one blank-importing the facade and one not importing it. At
`main`, compare:

- environment, cwd, umask-visible behavior;
- goroutine count/stacks after stabilization;
- open file descriptors and listeners;
- files created beneath isolated HOME/tmp/cwd;
- stdout/stderr;
- child processes.

Then repeat with the real feature binary for harmless non-inspector commands
(`version`, help/error paths) and the normal MCP harness. Import/linking may
increase binary size; it must not activate behavior.

### D3. Runtime reachability

Trace command dispatch and prove that normal MCP startup cannot call
`captureinspector.Start`. Separately document the expected call to
`capture.NewMCPRecorderFromEnvironment` and prove its no-env path creates no
file, goroutine, listener, cache, or protocol change.

### D4. Module/interface review

Use the codebase-design vocabulary consistently. Assess at least these modules:

| Module | Interface/seam question |
|---|---|
| `captureinspector` facade | Does the tiny interface earn its apparent Middle Man shape by enforcing the compiler seam? |
| private `web` | Does it hide HTTP/security/cache behavior behind a small lifecycle interface? |
| private `projection` | Is its large internal type/query surface coherent and private enough? |
| `capture` | Is one package a deep canonical-evidence module, or a divergent cluster of recorder, daemon, manager, inspector, corpus matching, and eval bundling? |
| MCP recorder adapter | Does the reader/writer adapter preserve the exact transport interface and own capture errors correctly? |
| eval/workflow adapters | Are capture concerns local, nil-safe, and absent from ordinary caller knowledge? |

Apply:

- **deletion test:** where would complexity reappear if the module vanished?
- **interface test surface:** can behavior be tested through the same interface
  callers use, with private internal seams only where justified?
- **adapter reality:** every introduced port/interface needs at least production
  and test adapters or another real variation;
- **locality:** a format/security/lifecycle change should not require edits
  across unrelated ZCP domains;
- **transitive weight:** direct allowlists do not hide a dependency graph that
  makes the normal server carry unrelated core concepts.

Do not recommend splitting solely because files are long. Recommend a seam move
only with a demonstrated coupling, activation, testing, or change-locality
problem.

### D5. Design-it-twice trigger

If and only if D1–D4 confirm that the current seam is unsafe or materially
shallow, produce at least three alternatives before remediation:

1. minimal-interface same-binary design;
2. flexibility-oriented adapters for recorder/readers;
3. common-caller optimized design;
4. separate-binary/build-lifecycle design only if measured dependencies justify
   reconsidering the existing decision.

Compare depth, leverage, locality, dependency category, migration risk, format
skew, and test surface. No architecture rewrite starts without explicit user
approval.

**Gate D:** publish separate static-isolation, cold-import, runtime-reachability,
and module-depth verdicts. "Architecture tests pass" alone is not a pass.

## 10. Phase E — disabled-path and observer-effect audit

Build a state matrix and test every cell:

| State | Inspector UI | Capture core | Expected effect |
|---|---:|---:|---|
| ordinary ZCP/MCP, no capture env | off | disabled | exact base-equivalent behavior |
| partial/malformed capture env | off | fail loud | no half-capture and no stdout corruption |
| scoped/persistent capture | off | enabled | declared evidence only; protocol result unchanged |
| `zcp capture inspect` | off | reader only | canonical files unchanged |
| `zcp capture ui` | on | reader only | loopback server/cache only |
| `zcp capture on/off/status` | off | manager enabled | only owned settings/process/files change |

### E1. Differential normal-MCP replay

Replay the same deterministic initialize, tools/list, representative tools/call,
notification/cancellation, and clean EOF transcript through base and feature
binaries or equivalent server harnesses with all capture env removed. Compare:

- stdout byte-for-byte;
- JSON-RPC ordering/framing;
- stderr except documented version/path noise;
- exit code and shutdown status;
- files, listeners, goroutines, and child processes;
- MCP instructions/tool schemas/results.

Any model-visible or protocol-visible delta needs an explicit non-capture cause
or becomes a finding.

### E2. MCP adapter transparency

Exercise arbitrary chunk boundaries, `n>0` with error, short writes, EOF,
cancellation, concurrent writes where permitted, delegate close errors, queue
overflow, recorder write failure, and terminal-close races. Assert delegate
`(n, err)` and accepted byte prefixes are unchanged. Capture errors must be
visible in evidence/final status but must not replace protocol I/O results while
the stream is active.

Review whether synchronous hashing/base64 work is within the declared observer
budget; "non-blocking queue" does not by itself prove low observer effect.

### E3. Provider proxy transparency

Compare direct-upstream and proxied behavior for:

- status and entity bytes;
- request path/query/body and fixed origin;
- redirects not followed;
- content length/encoding and compression behavior;
- hop-by-hop and `Connection`-nominated headers;
- SSE chunk order and early flush;
- client cancellation, upstream error, short reads/writes, trailers, and
  shutdown during active exchanges;
- authorization/cookie forwarding to the selected upstream but structural
  absence from recorded headers.

Measure first-byte latency, streaming delay, CPU, allocations, and peak memory
for representative and oversized requests. Set an explicit acceptance budget
before interpreting results.

### E4. Workflow provenance non-interference

For every instrumented rendering surface, compare capture off/on output bytes
and hashes. Inject open/write/fsync/close failures and concurrent calls. Capture
failure may warn on stderr but may not change model-visible content, return
value, workflow state, or tool result.

### E5. Eval disabled path

With `RunnerConfig.Capture == nil`, compare pre-feature and feature:

- Claude argv/order and MCP config behavior;
- inherited environment and isolated HOME behavior;
- user-sim and resume invocation count/order;
- transcript/artifact names and result status;
- timeout/cancellation/cleanup;
- no control-socket probe or capture bundle write.

Then verify enabled capture adds exactly one strict same-name `zerops` server and
never duplicates tools.

### E6. Persistent manager isolation

Run entirely against sandbox settings and injected process adapters. Fault every
transaction boundary in `On`, `Off`, and `Status`:

```text
state mkdir/write/sync/rename
start readiness and identity check
prepare/apply/restore settings
final ON journal commit
control shutdown
TERM/KILL and PID identity
manifest recovery
socket/state removal and directory sync
```

After each fault assert the truthful reconciled state, whether routing still
points at the proxy, whether an owned daemon remains, whether user edits were
preserved, and what command can recover. Include concurrent `on/off/status`,
dead daemon, stale socket, PID reuse, malformed journal, loopback-upstream
recursion, crash after every durable step, and signal shutdown.

**Gate E:** the no-capture path is byte/side-effect equivalent to base, and every
enabled-path deviation is explicit, bounded, recoverable, and represented in
capture status.

## 11. Phase F — forensic correctness and durability

### F1. Canonical format and validation order

Test each manifest status/format and every declared file kind. Independently
corrupt size, hash, sequence, gap, terminal record, aggregate stream hash,
encoding, framing, lifecycle identity, and path. Prove no derived claim or
plaintext detail is produced before all prerequisite validation passes.

Distinguish in every output:

- hash-valid vs complete;
- partial vs unclean vs invalid vs running;
- missing vs zero;
- missing vs ambiguous vs different vs exact;
- execution success vs semantic outcome.

### F2. Recorder and shutdown concurrency

Review lock ownership, queue sequencing, gap emission, close idempotence,
active-request drain, late capture errors, and manifest finalization. Use
`-race`, deterministic barriers, and repeated stress runs for:

- record concurrent with close;
- overflow immediately before terminal close;
- proxy/control errors arising during shutdown;
- duplicate close/shutdown calls;
- parent cancellation during startup and finalization;
- disk errors after a running manifest exists.

The terminal manifest and every stream must agree on the strongest known
completeness state.

### F3. Correlation truth table

Create a sanitized deterministic fixture matrix covering:

- MCP requests, responses, errors, notifications, progress, cancellation,
  duplicate/out-of-order IDs, parallel calls, and arbitrary transport chunks;
- all supported provider SSE events/block types and malformed/unknown events;
- gzip identity/magic compatibility and unsupported encoding;
- exact/different/missing/ambiguous tool result propagation;
- provider usage absent vs observed zero;
- late/missing invocation binds and unrelated sessions;
- context prefix carry, reset, rewrite, tool-schema/system changes;
- static corpus match vs capture-time provenance match;
- finalized, partial, invalid, unclean, and running-prefix views.

For every projected edge/metric, follow its evidence reference back to canonical
bytes and independently recompute the value. Wall-clock order must never be used
as an undeclared causal join.

### F4. Running-to-final transition

Poll a live synthetic capture while records and a trailing partial line are
written, then finalize it. Prove:

- only complete durable JSON objects appear while running;
- no integrity/completeness claim appears early;
- running views are not stale-cached;
- finalized valid/invalid state replaces running state deterministically;
- routes that cannot work for running captures fail honestly rather than
  implying absent evidence.

### F5. Real corpus acceptance

Re-run CLI inspection and browser projection over all four local captures.
Recompute the documented counts and selected edge/metric examples from raw
files rather than accepting the handoff table. Preserve before/after canonical
hashes.

**Gate F:** every derived claim class has a negative test, evidence round-trip,
and honest incomplete/ambiguous behavior.

## 12. Phase G — local security, privacy, and read-only audit

Threat model:

1. a malicious or corrupted local capture bundle;
2. a hostile web origin/browser page targeting the loopback service;
3. another local process probing the port or racing files;
4. accidental disclosure through URL, history, logs, screenshots, metadata,
   headers, or permissive files;
5. oversized/decompression/path inputs causing local denial of service.

### G1. Network/capability controls

Verify loopback bind and Host checks for IPv4, IPv6, `localhost`, malformed
Host, alternate textual IP forms, redirects, and DNS-rebinding patterns.
Review constant-time comparison, one-use launch semantics, cookie attributes,
exact Origin enforcement, SameSite behavior, method routing, request limits,
and shutdown.

Explicitly inspect whether the launch capability can remain in browser history,
process arguments, logs, referrers, or error pages after redirect. A current-URL
assertion alone is not sufficient.

### G2. Reveal and pre-reveal data classification

Build a tainted fixture placing unique sentinels in every body/content field,
thinking block, tool argument/result, provider result, transcript line, artifact,
and malformed/unknown block. Before reveal, recursively inspect every JSON/HTML
response and browser state for sentinel leakage. After reveal, ensure only the
requested bounded content is returned.

Confirm summary/trace/flow titles, diagnostics, errors, IDs, filenames, and
format candidates cannot accidentally derive plaintext snippets.

### G3. Browser/XSS/CSP

Use hostile HTML, SVG, markdown, JSON-in-string, URLs, control characters,
unicode, very deep objects, and large strings in every rendered surface.
Verify escaping in initial render and all post-reveal drawers, no DOM execution,
no inline style/event-handler weakening, no external network/telemetry, strict
CSP, no nested/stacked detail surfaces, and no console/page errors.

### G4. File/path safety and read-only proof

Exercise absolute paths, `..`, mixed separators, symlink file/parent/root,
hard-link replacement, FIFO/device/socket, permission changes, duplicate IDs,
manifest path aliases, and file replacement between hash verification and read.
Classify the documented verify-then-reopen race explicitly: fix, narrow the
contract, or accept it in writing with threat rationale.

Trace filesystem syscalls or compare full tree metadata before/after every
read route. The HTTP API, projection, cache, CLI inspect, and browser must not
write canonical evidence.

### G5. Resource bounds

Attack JSON nesting/line size, SSE event count, gzip expansion, manifest file
count, capture-root breadth/depth, trace construction, repeated context reads,
cache cardinality, raw pagination, concurrent HTTP requests, and client aborts.
Known absence of a 250 MiB/1 GiB benchmark is not automatically a defect, but
unbounded attacker-controlled memory/CPU or cache growth needs an explicit
ship decision.

### G6. Local secret handling and permissions

Verify provider authorization/cookie headers are never recorded, all canonical
and manager/control files have private modes, settings preserve prior mode, Unix
socket ownership is safe, tokens never enter provider records or tracked logs,
and warnings accurately state that bodies remain plaintext.

**Gate G:** no pre-reveal plaintext leak, no unowned write/process/network
mutation, and no unresolved Blocker/Major local-security candidate.

## 13. Phase H — test architecture and adversarial verification

### H1. Requirement-to-test coverage

For every contract-matrix row classify coverage as:

```text
unit | interface/adapter | HTTP | CLI subprocess | differential |
race/stress | browser | real-corpus | untested
```

Measure package and changed-line coverage, but do not treat percentages as a
verdict. Read assertions for tautologies, fixture coupling, implementation-only
pins, missing negative paths, global-state parallelism, and tests that would
stay green if the load-bearing call were deleted.

### H2. Mutation checks

In a disposable worktree, make targeted reversible mutations and require the
claimed gate to fail. Examples:

- import the facade from `internal/server` and a nested command;
- add `init`, package-call initializer, stdout, env mutation, or process import;
- bypass capability/reveal/integrity checks;
- change exact `(n, err)` forwarding;
- suppress a capture gap or partial status;
- mutate canonical evidence from a detail route;
- turn missing metrics into zero;
- expose one plaintext sentinel in a summary field.

A green suite after a load-bearing mutation is a confirmed test-enforcement gap.

### H3. Fuzz/stress targets

Run existing fuzzers and add temporary audit fuzz harnesses where absent for:
manifest paths, JSONL framing, MCP chunk reconstruction, provider SSE/gzip,
opaque trace refs, HTTP query parsing, and client content renderers. Any durable
new invariant found during remediation belongs in committed tests, not only an
audit script.

### H4. Verification commands

At minimum, on the frozen audit head:

```bash
go test ./internal/capture/... -count=1
go test ./internal/captureinspector/... ./internal/server ./internal/eval ./internal/workflow ./cmd/zcp -count=1
go test ./... -short -count=1
go test -race ./internal/capture/... ./internal/captureinspector/... ./internal/server ./internal/eval ./internal/workflow -count=1
go vet ./...
make lint-fast
make lint-local
./bin/golangci-lint run ./internal/capture/... ./internal/captureinspector/...
node --check internal/captureinspector/internal/web/assets/app.js
node --check internal/captureinspector/browsertest/smoke.cjs
go test -tags=captureinspector_browser ./internal/captureinspector/internal/web -run TestBrowserSmoke -count=1 -timeout=2m
git diff --check "$BASE...$AUDIT_HEAD"
```

Run `go test ./... -race -count=1` before a final GO verdict unless a documented
external prerequisite makes it impossible. Compare failures with the base.
Browser dependency installation and output stay inside the optional test-only
subtree/tmp; verify no production `go.mod` dependency appears.

Credentialed real Claude/provider or Zerops runs require explicit operator
approval, isolated credentials, cost awareness, and a separate evidence log.
They supplement deterministic tests; they never replace them.

**Gate H:** every claimed architecture/security/non-interference gate has been
mutation-tested or has a documented reason why it cannot be, and mandatory
verification is green or honestly classified against base.

## 14. Phase I — finding verification and code-review packet

### I1. Adversarial confirmation

A reviewer who did not author the candidate must attempt to refute it by:

1. reading the whole function and all callers/callees;
2. checking neighboring/sibling implementations;
3. locating existing tests and spec exceptions;
4. reproducing on `b5bd502a` from a clean environment;
5. checking whether it also exists on `8f1ef6f2`;
6. minimizing the reproduction;
7. confirming severity and blast radius.

Record refuted candidates so the same false positive is not rediscovered.

### I2. Synthesis without losing review axes

First publish the Standards and Spec reports unchanged. Then group confirmed
items by root cause in the ledger while retaining their original lane IDs.
Likely root-cause categories to test, not assume:

- disabled-path coupling;
- transaction/recovery asymmetry;
- observer hot-path work;
- validation-before-derivation gaps;
- cache/detail time-of-check races;
- plaintext classification/rendering drift;
- duplicated correlation truth;
- broad/shallow module interface;
- test gate that proves only its happy path.

### I3. Review meeting packet

`09-final-verdict.md` contains:

- fixed hashes and exact commands;
- separate Standards, Spec, Isolation, Security/Evidence, Design, and Tests
  verdicts;
- confirmed/refuted/accepted counts by severity;
- requirement matrix summary;
- disabled-path differential result;
- canonical read-only hash result;
- known limitations split into measured/unmeasured;
- smallest safe remediation sequence;
- explicit `GO`, `GO WITH ACCEPTED FOLLOW-UPS`, or `NO-GO` recommendation.

No implementation change begins until this packet is reviewed and the user
approves the remediation scope.

## 15. Phase J — remediation workflow after approval

Only confirmed findings approved by the user enter implementation.

### J1. Ordering

1. isolation, MCP stdout/control-flow, credential, canonical-write, reveal, and
   integrity Blockers;
2. manager crash recovery, silent loss, causal falsification, and other Majors;
3. disabled eval/workflow behavior regressions;
4. focused module/seam change only when D5 selected a design;
5. Minor correctness/test gaps;
6. documentation and handoff truth sweep.

Do not mix unrelated observed eval behavior into these patches.

### J2. TDD and commits

For each finding:

1. add the smallest interface-level RED test and record its expected failure;
2. commit/preserve RED evidence before implementation as required by project
   TDD policy;
3. make the smallest GREEN correction;
4. refactor only after affected layers remain green;
5. update the authoritative spec if the accepted contract changes;
6. run focused race/lint and the requirement-specific mutation check;
7. code-review that phase against its immediate parent before continuing.

A capture-side failure fix must never broaden what normal MCP/workflow/eval
callers need to know. A UI fix must not introduce a new core import.

### J3. Re-review ranges

Keep the original audit head immutable. Review fixes as explicit ranges:

```text
b5bd502a..phase-1-head
phase-1-head..phase-2-head
...
8f1ef6f2...final-head  (final aggregate regression review)
```

Repeat the independent Standards/Spec check for every phase; run the full
isolation matrix after any package, environment, process, or transport change.

## 16. Final ship gate

A **GO** requires all of the following:

- every normative spec row is PASS or has an explicit user-approved contract
  change in the authoritative spec;
- no unresolved Blocker or Major finding;
- normal MCP/CLI/workflow/eval disabled paths are differential-equivalent to
  base except explicitly approved behavior;
- compiler, depguard, AST, cold-import, and runtime-reachability isolation pass;
- provider/MCP observer transparency and capture-failure semantics pass;
- manager fault matrix has a truthful recovery path at every durable step;
- invalid/partial/unclean evidence cannot authorize derived/plaintext claims;
- pre-reveal taint corpus is absent from all metadata responses and DOM;
- canonical corpus hashes and metadata are unchanged after inspection;
- targeted and full tests, race, vet, lint, JS syntax, browser smoke, and diff
  hygiene pass or any base-only issue is explicitly documented;
- final aggregate code review finds no new issue;
- no release/install/canonical binary mutation occurred without separate
  approval.

`GO WITH ACCEPTED FOLLOW-UPS` is allowed only for explicitly non-contractual,
non-security, non-integrity limitations such as measured future scale work. An
unmeasured area is not accepted implicitly; the final packet must name who
accepted the risk and why.

A **NO-GO** is mandatory for any unresolved normal-path activation, protocol
byte/control-flow change, unowned user-state mutation, credential exposure,
canonical evidence mutation, pre-reveal plaintext leak, integrity
misrepresentation, or unrecoverable manager transaction.

## 17. Completion and knowledge placement

After fixes and final review:

- durable behavior belongs in tests plus `docs/spec-capture-inspector.md`;
- implementation mechanisms belong in code/doc-comments;
- cross-package architecture belongs in `docs/spec-architecture.md`;
- transient command logs and plaintext evidence remain under `tmp/`;
- the completed plan and audit packet move to `plans/archive/`;
- do not add audit history, hashes, or plan references to `CLAUDE.md`.

The audit is complete only when the final verdict is evidence-backed. The
existence of the current handoff, passing focused tests, or the inspector's
package fence is not by itself proof that the implementation is safe to merge.
