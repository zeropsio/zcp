# Capture and Inspector

## 1. Purpose

ZCP capture records the two protocol boundaries needed to explain agent
behaviour:

1. the model client ↔ provider HTTP/SSE boundary; and
2. the model client ↔ ZCP MCP stdio boundary.

The raw bytes at those boundaries are canonical evidence. Inspection, session
indexing, eval grouping, context reconstruction, and source attribution are
derived views and must remain regenerable.

Capture is developer-only, local, opt-in, and plaintext. Provider authorization
and cookie header values are never recorded. Request and response bodies can
contain prompts, source code, tool inputs, and tool results and therefore live
under private user-only permissions.

## 2. Product invariants

1. **Raw is immutable.** Provider, MCP, and lifecycle JSONL files are append-only
   during capture and never rewritten by inspection.
2. **No observer effect.** The proxy does not change provider entity bytes,
   status, streaming order, MCP stdin/stdout bytes, guidance, tool results, or
   agent control flow.
3. **No silent loss.** Queue overflow, disk/write failure, unsupported framing or
   encoding, incomplete streams, daemon restart, and abrupt termination are
   visible before interpretation.
4. **Completeness and hash validity are distinct.** A partial or unclean capture
   can have valid hashes but can never be rendered as integrity `OK`.
5. **Every derived claim has evidence.** Views identify the raw file, sequence
   range, exchange/process, byte/decoded offset, timestamp, and equality basis.
6. **Credential headers are structurally absent.** Body-level identifiers and
   content remain plaintext evidence; inspection does not claim they were
   redacted.
7. **Client-specific signals are versioned observations.** If a supported client
   stops emitting an identity signal, records remain captured and are shown as
   unattributed rather than heuristically assigned.

## 3. Capture lifecycle

`zcp capture on`, `off`, and `status` form a serialized state machine. The
manager reconciles its journal against the real client configuration, daemon
control endpoint, process identity, and provider listener.

### 3.1 States

- **OFF** — no ZCP-owned provider override is installed, no owned capture daemon
  or listener exists, and no capture manifest is open.
- **ON** — the supported client configuration points at a health-checked,
  loopback-only ZCP listener owned by the recorded daemon; its capture window is
  open.
- **BROKEN** — configuration, journal, daemon, or listener disagree. `status`
  reports every disagreement and returns non-zero. `on`/`off` reconcile only
  changes demonstrably owned by ZCP.

The commands are idempotent and protected by an advisory lifecycle lock.

### 3.2 Enable transaction

1. Acquire the lifecycle lock, reconcile stale state, and durably journal the
   enabling transaction.
2. Create a private capture window and start the detached daemon.
3. Wait for a control-plane readiness response and verify daemon identity and
   the provider listener.
4. Prepare the exact compare-and-swap restore patch and journal it before any
   client setting changes.
5. Atomically install the client provider override and MCP capture coordinates.
6. Commit `ON` state.

The client configuration is never pointed at an endpoint that failed readiness.
Any pre-commit failure rolls back both configuration and daemon.

### 3.3 Disable transaction

1. Acquire the lifecycle lock.
2. Restore/remove only settings that still equal ZCP's installed values. Preserve
   unrelated and concurrently edited settings.
3. If configuration restoration fails while it still points at ZCP, keep the
   proxy alive and return an error.
4. Stop accepting new traffic, bounded-drain active exchanges, close lifecycle
   and provider recorders, and finalize the manifest.
5. Stop the owned daemon and verify its process and listener are gone.
6. Commit `OFF` state.

Clients started while capture was on must be stopped before `off`; hot attach
and clients surviving disable are outside the first compatibility contract.

### 3.4 Daemon failure

A dead daemon with an installed provider override is `BROKEN`, never `ON`.
Capture fails closed: ZCP does not silently bypass an enabled recorder. A future
supervisor may restart a daemon, but every process epoch and resulting evidence
gap must remain explicit.

## 4. Client adapters

The core owns daemon/storage lifecycle. An adapter owns only reversible client
configuration and compatibility extraction.

The first adapter is Claude Code:

- provider routing uses `ANTHROPIC_BASE_URL`;
- persistent interactive routing is installed through Claude's user settings,
  not by editing shell startup files;
- ZCP eval runners also inject the active endpoint into spawned Claude
  processes because evals may use an isolated `HOME`;
- exact prior settings are restored on disable;
- the compatibility report records the observed Claude version.

Codex and pi require separate base-URL/protocol adapters and are not represented
as working merely because the Claude transport works.

## 5. Capture windows, Claude sessions, and eval scopes

One `on → off` interval is a **capture window**. It can contain multiple
interactive sessions and eval runs.

For observed Claude Code Messages requests, `metadata.user_id` is a JSON-encoded
string containing `device_id`, `account_uuid`, and `session_id`. The derived
Claude adapter parses `metadata.user_id.session_id`. Provider responses inherit
the session through the proxy exchange identity. Account and device identifiers
are not needed for grouping and are not printed in normal views.

The session signal is adapter-specific. Missing or malformed metadata produces
an unattributed exchange and a compatibility warning.

### 5.1 Eval lifecycle side channel

The eval runner emits capture-only lifecycle records to the daemon. It never
adds a provider header or body field.

Identity hierarchy:

```text
eval_run_id
└── scenario_run_id
    └── invocation_id
        ├── phase
        └── claude_session_id
```

Required phases include `agent.initial`, `user-sim.<n>`,
`agent.resume.<n>`, and `retrospective`.

For a fresh invocation the runner emits `invocation.start`, starts Claude, reads
`system.init.session_id`, and then emits `invocation.bind`. Late binding is
intentional: inspection joins already-recorded exchanges after the run. Resume
and retrospective invocations can bind the known session before launch.
`invocation.end` bounds the phase. The same Claude session may therefore have
several non-overlapping invocation phases.

MCP process evidence carries the same invocation identity through a
capture-only launch side channel. Provider and MCP protocol bytes remain
unchanged.

Missing lifecycle/bind evidence does not invalidate raw provider bytes. It marks
the eval annotation incomplete. Unrelated sessions remain visibly
`unattributed`; they are never silently folded into an eval.

## 6. Eval operation

When global capture is `ON`, ZCP eval commands discover it automatically,
inject its endpoint into all Claude subprocesses (including user simulation),
and register lifecycle scopes.

`zcp eval behavioral ... --capture raw` is a convenience contract:

- if global capture is on, attach to that window;
- otherwise create a scoped window before the eval and close it afterward;
- never start a second competing proxy.

A suite has one eval-run identity, each scenario has one scenario-run identity,
and every Claude process invocation is marked independently.

The capture bundle copies and hashes the scenario source, task prompt,
transcript, retrospective, self-review, metadata, and available verification
artifacts under an eval-owned directory. The top-level manifest inventories raw
and eval files. Logical eval views reference canonical raw sequence ranges; they
do not rewrite or delete unrelated evidence.

## 7. Canonical bundle

```text
<capture-window>/
├── manifest.json
├── provider.jsonl
├── lifecycle.jsonl
├── mcp/
│   └── zcp-<pid>.jsonl
└── eval/
    └── <eval-run-id>/
        └── <scenario-run-id>/
            ├── scenario.md
            ├── task-prompt.txt
            ├── transcript.jsonl
            ├── retrospective.jsonl
            ├── self-review.md
            └── meta.json
```

Every canonical file is regular, relative to the capture directory, hashed in
the terminal manifest, and private to the user. Derived output is optional and
regenerable.

## 8. Inspection

Inspection validates, in order:

1. supported manifest format and lifecycle consistency;
2. every inventoried file size/hash and safe relative path;
3. record sequence continuity, explicit gaps, and terminal status;
4. request/response and MCP stream byte counts and hashes;
5. declared content encoding and protocol framing;
6. only then, provider/MCP/lifecycle parsing and correlation.

Primary views are:

- summary/completeness;
- Claude sessions and unattributed exchanges;
- eval run → scenario → invocation timeline;
- model tool-use → MCP call/result → provider tool-result causality;
- model-context composition and request-to-request delta;
- exact current-corpus source matches and capture-time composition provenance.

Unsupported encoding or framing is an explicit inspection error. Hidden thinking
content is not printed by default; views expose its type, size, and raw evidence
location.

## 9. Compatibility and non-goals

The first supported transport lane is the empirically verified Claude Code
HTTP/SSE Messages path. Provider message IDs identify one model response;
Claude session IDs identify the conversation; capture/eval IDs identify local
observation scopes. These identities must not be conflated.

The first version does not promise hot attachment, transparent survival of
`capture off` by already-running clients, packet/TLS-record capture, model
attention, semantic grading, a browser UI, or a stable provider-neutral derived
schema.
