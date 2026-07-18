# S11 live audit — object (storage) + stream (events/queue)

Program: `plans/dataconsole-excellence-program-2026-07-16.md` §4 S11.
Scope: `provider/object` (object-storage/S3, full support) + `provider/stream` (kafka:single@3.9
"events", nats:single@2.12 "queue", both view-only). Live console, writes armed
(`allowWrites:true`, correct write token presented). All findings below are **live-verified**
against the running server unless explicitly marked `[UNVERIFIED]`/`not-run`.

Wire contract read from: `internal/dataconsole/console/server/server.go`,
`server_writetoken_test.go`, `provider/types.go`, `provider/actions.go`, `provider/errors.go`,
`provider/object/object.go`, `provider/stream/stream.go`, `webui/dist/app.js`,
`webui/dist/dc-format.js`, `cmd/dcseed/main.go`, `console/seed/{object,stream}.go`.

Fixture hygiene: all creates/deletes used an `audit_/` prefix on `storage`; every stream
mutation attempt was refused before touching state (view-only family, nothing to clean up).
Bucket verified back to exact pre-audit fixture state at the end (see §2).

---

## 1. Operation × verdict matrix

| Operation | object (`storage`) | kafka (`events`) | nats (`queue`) |
|---|---|---|---|
| Tree list, root | passed — 6 nodes (3 blob incl. pre-existing `AGENTS.md`/`vypis-1314812.pdf`, 3 container) | passed — 1 leaf (`orders`), no `meta` key at all | passed — 1 leaf (`EVENTS`), no `meta` key at all |
| Tree list, nested container | passed — `data/`, `images/`, `logs/` all walked correctly | n/a — `List` short-circuits to empty for any non-root path (by design, no deeper browse) | same as kafka |
| Tree pagination (25 objects, `limit=10`) | passed — 10/10/5, cursor = last-key-seen, 0 duplicates/gaps across the walk, clean re-empty after cleanup | n/a (single leaf, no pagination surface) | n/a |
| Stat, real leaf | passed — `Meta={size,contentType,etag}` | passed **but non-verifying** — see STR-AUD-01 | passed but non-verifying — see STR-AUD-01 |
| Stat, fabricated/nonexistent leaf | n/a | **200, identical shape to a real topic** — STR-AUD-01 | **200, identical shape to a real stream** — STR-AUD-01 |
| Stat, container/prefix path | failed clean — 404 `not_found` (S3 has no object literally named the prefix) | n/a | n/a |
| ReadBlob, text | passed — `text/plain` detected (seeder-set) | n/a | n/a |
| ReadBlob, image | passed — `image/png` detected (seeder-set) | n/a | n/a |
| ReadBlob, pre-existing binary (`vypis-1314812.pdf`) | passed, but `contentType` already degraded to `application/octet-stream` — see OBJ-AUD-01 | n/a | n/a |
| ReadBlob, summary | n/a | passed — JSON, see §4 | passed — JSON, see §4 |
| ReadBlob, truncation (17 MiB object, 16 MiB `MaxInlineBytes`) | passed — `Truncated:true`, body byte-exact 16777216-byte prefix of the original (`cmp` confirms no divergent bytes before EOF) | n/a | n/a |
| ReadBlob, nonexistent resource | n/a | **502 `upstream`** (misclassified) — STR-AUD-02 | 404 `not_found` (correct) — STR-AUD-02 |
| Download-hardening headers | passed — `Content-Disposition: attachment`, forced `Content-Type: application/octet-stream`, sanitized `X-DataConsole-ContentType`/`X-DataConsole-Truncated`, `X-Content-Type-Options: nosniff`, CSP `frame-ancestors 'none'` — uniform across every GET /api/blob tried | passed — identical header set | passed — identical header set |
| WriteBlob round-trip (`PUT /api/blob`) | passed (bytes exact) but **content-type not preserved** — OBJ-AUD-01 | refused — 403 `read_only` | refused — 403 `read_only` |
| Upload (multipart `POST /api/upload`) | passed (bytes exact, confirmed via `cmp` against source) but **content-type not preserved even though the multipart part declared `type=image/png`** — OBJ-AUD-01 | refused — 403 `read_only` | refused — 403 `read_only` |
| Overwrite existing key | passed — etag + size change confirmed across two writes to the same key | n/a | n/a |
| Confirm gate (`X-Write-Token` present, no `X-Confirm`) | passed — 409 `needs_confirm`, verified zero side effect (key absent afterward) | n/a (never reaches provider — gated earlier) | n/a |
| Rename, single object | passed — source 404 after, destination 200 present, byte-identical | refused — 422 `unsupported` (no `Rename` method on the type at all) | refused — 422 `unsupported` |
| Rename, prefix path (D-07) | failed clean — 404 `not_found` on both trailing-slash variants; **no data loss to the folder's real children** | n/a | n/a |
| Delete, real leaf | passed — 200 `{"ok":true}` | refused — 403 `read_only` (both `DELETE /api/blob` and `DELETE /api/node` routes, same handler) | refused — 403 `read_only` |
| Delete, already-deleted / fabricated key | **"passed" — 200 `{"ok":true}`, indistinguishable from a real deletion** — OBJ-AUD-02 | refused — 403 `read_only` | refused — 403 `read_only` |
| Delete, prefix path (D-07) | **"passed" — 200 `{"ok":true}` on both trailing-slash variants; no data loss to children, but false-positive success** — OBJ-AUD-02 | n/a | n/a |
| `setTTL` / `editKVEntry` (PUT+DELETE) / `insertRow` / `deleteRow` / `editCell` | refused — 422 `unsupported` (spot-checked on `storage`, shape mismatch) | refused — 422 `unsupported`, all 6 | refused — 422 `unsupported`, all 6 |
| Error: missing `service` param | failed clean — 400 `invalid` | (shared code path — not re-tested per engine) | |
| Error: malformed `segs` (bad JSON, or JSON non-array) | **"passed" — 200, silently falls back to a root listing** — OBJ-AUD-03 | (shared code path) | |
| Error: unknown service name | failed clean — 404 `not_found`, no internal detail leaked | | |
| Error: malformed multipart (missing `file` field) | failed clean — 400 `invalid` | | |
| Error: malformed JSON body | failed clean — 400 `invalid` | | |
| Cross-family GET (`/api/table`, `/api/query`) | failed clean — 422 `unsupported` | failed clean — 422 `unsupported` | n/a (not separately tested, same code path as kafka) |

Legend: "passed" = the operation succeeded functionally; "failed clean" = a mutation/read was
correctly refused with a sanitized envelope; **bold** = a finding below.

---

## 2. D-07 verdict — observed folder semantics

Original register entry: *"object rename/delete are single-key; prefix (folder) semantics
unhandled — currently unexposed on containers"* (`object.go:210,223`), rated LOW/latent.

**Live verdict: the safety concern is smaller than "latent" implied, but a new, more general
defect sits underneath it.**

Setup: created `audit_/dir/a.txt` + `audit_/dir/b.txt` (so `audit_/dir` is a container/prefix,
never itself a real S3 key). Then, with the write token armed:

1. `DELETE /api/node {"path":{"segments":["audit_","dir"]}}` → **200 `{"ok":true}`**
2. `DELETE /api/node {"path":{"segments":["audit_","dir",""]}}` (trailing empty segment →
   `prefix()` joins to `"audit_/dir/"`) → **200 `{"ok":true}`**
3. `POST /api/rename {"from":{"segments":["audit_","dir"]},"to":{"segments":["audit_","dir2"]}}`
   → **404 `not_found`**
4. Same rename with the trailing-slash variant → **404 `not_found`**
5. Post-check: `GET /api/tree segs=["audit_","dir"]` still lists `a.txt` + `b.txt` with
   **unchanged `modified` timestamps** — zero data loss. `GET /api/tree segs=["audit_"]`
   shows only `dir` — no stray `dir2` was created by the failed renames.

Root cause: `Delete`/`Rename` in `object.go` both build a literal S3 key via
`prefix()` = `strings.Join(path.Segments, "/")` with no existence check first. `RemoveObject`
on a key that was never real is a no-op per S3's DELETE-is-idempotent semantics (200, no
error) — this is true for *any* nonexistent key, not just a prefix-shaped one (see OBJ-AUD-02).
`CopyObject`, unlike `RemoveObject`, does error on a missing source, so `Rename` correctly
surfaces 404.

**Verdict: no destructive folder-wide deletion is possible today — confirmed safe.** The
residual issue is honesty, not safety: a `Delete` on a folder-shaped (or merely mistyped) path
reports success without having done anything, and disagrees with `Rename`'s correct refusal on
the identical condition. Folded into OBJ-AUD-02 below since it's the general case, not a
folder-specific one. Matches the plan's "currently unexposed on containers" — confirmed via
`app.js:379-389` (`renderNode`): a `container`-kind row's `onclick` is wired to
`expandContainer`, never to `openBlob` (the only place Delete/Rename buttons are wired), so
today's SPA cannot reach this path at all.

---

## 3. Meta-availability for object tree nodes (U-03 evidence)

| Source | Fields present on the wire | Consumed by the SPA? |
|---|---|---|
| `List` (`GET /api/tree`) `Node.Meta` — `object.go:135` | `size`, `modified` | Only `size`. `metaChip()` (`app.js:407-414`) reads `meta.size` / `meta.type` / `meta.ttlSeconds`; object never populates a `type` key (that's KV's redis-type field), so that branch is always dead for object. `modified` is sent on every list response and never read anywhere. |
| `Stat` (`GET /api/stat` or `GET /api/node`) `Node.Meta` — `object.go:160` | `size`, `contentType`, `etag` | **None, for the object family.** `/api/stat` has exactly one call site in the whole SPA — `maybeTTL` (`app.js:753-757`) — gated on `hasAction(service, ACTION.setTTL)`, and `setTTL` is never in an object service's action list (`actions.go:122-137`, KV-only). So this endpoint's payload — including `etag`, the only stable identity/versioning hint the provider exposes — is live, correct, and completely unreachable through today's UI for object services. |
| `ReadBlob` (`GET /api/blob`) headers — `object.go:172`, `server.go:341-344` | `X-DataConsole-ContentType`, `X-DataConsole-Truncated` | Both consumed, but only inside the blob viewer (`openBlob`, `app.js:417-470`) after a node is already opened: `contentType` drives the image/text/binary branch and the "· type" label; `truncated` drives the "head slice (view-only)" badge. |

Net: of 5 distinct metadata fields the object provider can produce (`size`, `modified`,
`contentType`, `etag`, `truncated`), only 3 ever reach a user, and never in the same view
(`size` in the tree row; `contentType`+`truncated` only after opening the blob). `modified`
and `etag` are both live and correct server-side and both fully invisible client-side. This is
precise, field-level confirmation of U-03 ("tree meta chips only for object … provider's
modified/etag dropped").

---

## 4. Stream summary-blob content assessment (U-04 evidence)

Full JSON bodies, `GET /api/blob`, `Content-Type: application/octet-stream` (forced),
`X-DataConsole-ContentType: application/json` on both:

```
kafka "orders"  (87 bytes):  {"partitionIds":[2,1,0],"partitions":3,"topic":"orders"}
nats  "EVENTS" (134 bytes):  {"bytes":1920,"firstSeq":1,"lastSeq":30,"messages":30,
                               "stream":"EVENTS","subjects":["events.>"]}
```

(`seed/stream.go` declares the kafka topic with `NumPartitions:1` and publishes 5 nats
messages per run; live values of 3 partitions / 30 messages are accumulated history from
repeated seed runs against a persistent cluster — an environment artifact, not a console bug;
the console is reporting true upstream state correctly either way.)

Tree list nodes for stream carry **no `meta` key at all**
(`{"name":"orders","kind":"blob","path":{...},"hasChildren":false}`, confirmed live) —
`stream.go:79-86`'s `List` never sets `Node.Meta`. So `metaChip()` renders nothing on a
topic/stream row; there is zero hint of size, partition count, or message count before opening
it — the barest node in the whole console.

Once opened, `openBlob()` classifies `application/json` as `textual=true`, `image=false`, and
renders it through the **exact same `<pre class="blob">` branch** used for a real JSON object
(verified identical to the `data/config.json` object fixture's own render path: same toolbar,
same "· `application/json`" label, same "Download" button). The only distinguishing signal is
an *absence* — no "Save" button, because `writeBlob` is never in a stream service's action list
— not an explicit label. There is no banner or text anywhere stating "this is a generated
summary, not stored data." **Confirmed exactly as U-04 describes**: partition/consumer/message
metadata masquerades as document content, with zero labelling to tell them apart. This is
direct, live audit evidence for DD-3's already-planned fix ("honest labelled metadata card
now").

---

## 5. New defects

### OBJ-AUD-01 — HIGH — console-originated writes lose content-type, breaking preview/edit on next open
**[VERIFIED live.]** Neither write path ever sets an S3 Content-Type:
- `object.go:197-207` `WriteBlob` calls `PutObject(..., minio.PutObjectOptions{})` — empty
  options, no `ContentType`.
- `server.go:575-609` `handleUpload` reads the multipart file via
  `file, _, err := r.FormFile("file")` — the discarded second return is the `*multipart.FileHeader`
  carrying the browser-declared MIME type; it is never consulted, and `WriteBlob`'s interface
  signature (`provider/types.go:147`, `WriteBlob(ctx, p Path, data []byte) error`) has no
  parameter to carry a content-type even if it were read.

Live reproduction: fetched the real `images/red.png` fixture, re-uploaded it via
`POST /api/upload` with the multipart part explicitly declared `type=image/png` — bytes came
back byte-identical (`cmp` clean) but `Stat`/`ReadBlob` both reported
`contentType:"application/octet-stream"`. Same result writing plain text via
`PUT /api/blob`. The pre-existing `vypis-1314812.pdf` fixture (not seeder-created — some
earlier manual upload) shows the same degraded `application/octet-stream`, consistent with
having gone through this same path historically.

Impact, traced through the SPA's actual classifiers (`dc-format.js:26-32`):
`isTextual()`/`isImage()` both test the *stored* content-type via regex
(`^text\/`, `^application\/(json|xml|...)`, `^image\/`) — `application/octet-stream` matches
neither. So on the next open of anything written through the console:
- an uploaded image never gets its inline preview or tree thumbnail (`app.js:386`, `:441`) —
  falls to "Binary content — use Download" even though it's a perfectly good image;
- a text file that was opened, edited, and saved once loses `textual`, so `editable`
  (`app.js:435`) becomes false on the *next* open — no Save button, no textarea, "Binary
  content — use Download" only. **Editing a file through the console breaks the console's own
  ability to edit or even preview that same file again.**

Fix shape (not implemented — audit only): either sniff `http.DetectContentType` over the first
bytes inside `WriteBlob` when no type is supplied, or thread the multipart part's declared MIME
type through a new `WriteBlob` parameter / extension-based guess. Either needs a family-contract
decision (P2), not a point patch, since `KVProvider.WriteBlob` shares the same signature.

### OBJ-AUD-02 — MEDIUM — Delete is unconditionally idempotent-success; disagrees with Rename on the same nonexistent path (generalizes + resolves D-07's safety question)
**[VERIFIED live.]** `DELETE /api/node` (and identically `DELETE /api/blob`, same handler)
returns `200 {"ok":true}` for:
- a leaf that was already deleted one request earlier,
- a key that was never real (`audit_/never-existed-xyz-123.txt`),
- a folder-shaped prefix path, with or without a synthetic trailing slash (D-07's case).

All four produce byte-identical `{"ok":true}` responses — a caller cannot tell "something was
removed" from "nothing was there." `Rename` on the exact same nonexistent/prefix paths
correctly 404s (`object.go:223-237`; `CopyObject` errors on a missing source, unlike
`RemoveObject`). Root cause: S3 DELETE is spec-idempotent (no error on a missing key), and
`object.go:210-219` `Delete` does no existence pre-check, so that idempotency passes straight
through to the API's JSON envelope with no "did I actually do anything" signal.

Confirmed **not** destructive (§2) — the residual risk is a misleading success response (a
user could click Delete on a stale tree row after someone else already removed the key, or on
a mistyped path, and see "Deleted." with nothing having happened), and an inconsistency between
Delete and Rename's handling of "this path doesn't name a real object." Not reachable via
today's SPA for the folder case (§2); the leaf case (double-delete / already-gone key) *is*
reachable — e.g., a double-click on the Delete button before the tree refreshes.

### OBJ-AUD-03 — LOW — malformed `segs` silently degrades to a root listing instead of erroring
**[VERIFIED live.]** `GET /api/tree` (also `/api/stat`, `/api/node` GET, `/api/blob` GET —
all share `parsePath`) with `segs=not-json-at-all` or `segs={"a":1}` (valid JSON, wrong shape)
both return **200** with the full root listing, not a 400. Root cause:
`server.go:730-736` `parsePath` does `_ = json.Unmarshal([]byte(segs), &p.Segments)` — the
error is discarded, leaving `p.Segments` at its nil zero value, which is indistinguishable from
"no segs param supplied at all" (i.e., "list the root"). No security impact (a malformed
`segs` can only narrow the effective path to root, never expand or redirect it elsewhere), but
it silently masks a client-side bug as ordinary-looking root-listing output instead of
surfacing a clear error.

### STR-AUD-01 — MEDIUM — Stat never verifies existence for the stream family
**[VERIFIED live, both engines.]** `GET /api/stat`/`GET /api/node` for `events` (kafka) and
`queue` (nats) returns **200** with `{name, kind:"blob", path}` for a fabricated topic/stream
name exactly as readily as for a real one (`does-not-exist-topic` and `DOES_NOT_EXIST` both
verified live) — same shape, no way to tell them apart. Root cause: `stream.go:90-95` `Stat`
only checks `len(path.Segments) == 1` and echoes the name back; it never calls upstream.
`ReadBlob` is the only operation that actually verifies existence for this family (and see
STR-AUD-02 for how inconsistently it does so). Not currently reachable through the SPA — the
sole `/api/stat` call site (`maybeTTL`) is gated on the `setTTL` action, which stream never
advertises — so this is latent on the raw API surface, same shape as D-07 was for object.

### STR-AUD-02 — MEDIUM — "topic/stream not found" is misclassified for kafka (502) vs nats (404)
**[VERIFIED live.]** Reading a genuinely nonexistent resource via `GET /api/blob`:
- kafka (`events`, topic `does-not-exist-topic`) → **502 `upstream`**
  (`{"code":"upstream","status":502,"message":"upstream error",...}`)
- nats (`queue`, stream `DOES_NOT_EXIST`) → **404 `not_found`** (correct)

Root cause asymmetry: `stream.go:225-251` `natsStreamInfo` explicitly maps a missing-stream
error to `provider.ErrNotFound` (line 237). `stream.go:169-184` `kafkaTopicInfo` wraps *every*
`ReadPartitions(topic)` failure as `provider.ErrUpstream` unconditionally, with no branch for
"this topic doesn't exist." The seeder's own comment (`seed/stream.go:26`) notes the broker has
topic auto-create disabled, so a nonexistent-topic read is a common, permanent, client-caused
condition — not a transient upstream fault — and today it's indistinguishable from one. A
caller (future retry/error-recovery logic, or just a human reading the error) gets "try again
later" framing for a condition that will never resolve by retrying.

### Confirmed-uniform, not a defect — the stream mutation refusal matrix
**[VERIFIED live, 20/20 combinations — 10 mutating routes × {kafka, nats}, all refused.]**
Every mutating route refuses for both engines, with zero successes and zero crashes. The
refusal is not one single code — it splits cleanly by whether `stream.Provider` implements the
Go method at all:
- `writeBlob`, `deleteNode` (both `/api/blob` and `/api/node` DELETE routes), `uploadObject` →
  **403 `read_only`** — the type does implement `WriteBlob`/`Delete`, hardcoded to return
  `provider.ErrReadOnly` (`stream.go:112-115`).
- `renameObject`, `setTTL`, `editKVEntry` (PUT+DELETE), `insertRow`, `deleteRow`, `editCell` →
  **422 `unsupported`** — the type never implements `Rename`/`SetTTL`/`SetEntry`/`DeleteEntry`/
  `InsertRow`/`DeleteRow`/`EditCell`/`Query`/`ReadTable` at all, so the handler's interface
  type-assertion fails.

Both are sanitized, non-crashing, and safe. The split is invisible to the SPA today —
`ServiceActions()` (`actions.go:132`, `familyMutatingActionIDs(FamilyStream) = nil`) never
advertises *any* mutation for the stream family in the first place, so the SPA never issues
these requests — the two-code split only matters to a raw API caller. Worth normalizing to one
code in the P2 family contract if a single "this family is view-only" sentinel is wanted, but
it is not a safety gap: confirmed with the write token both present (reaching the provider) and
absent (gated earlier, also 403 `read_only` — "wrong token" and "genuinely read-only provider"
are deliberately indistinguishable, per the existing "no oracle" design pinned by
`TestWriteToken_CorrectTokenWrites`). One cosmetic nuance: `DELETE /api/entry`'s error envelope
reports `"action":"editKVEntry"`, not a distinct delete-entry id — there isn't one; `PUT` and
`DELETE /api/entry` share a single route table entry and hence a single static `action` field
(`server.go:100`) — so logs/telemetry can't distinguish a failed *set* from a failed *delete*
by action id alone. Behavior is unaffected (the handler's method switch still calls the correct
`DeleteEntry`); this only blurs observability.

### Minor wire-shape notes (not elevated to defects)
- Empty tree listings marshal as `"nodes":null`, not `[]` (Go nil-slice → JSON `null`,
  `provider.List` returns an unassigned `[]provider.Node`). Harmless — `app.js:331` already
  guards with `data.nodes || []` — noted only for the wire-contract record.
- Go's canonical header casing puts `X-DataConsole-ContentType` on the wire as
  `X-Dataconsole-Contenttype`. Functionally inert (`Headers.get()` is case-insensitive, and the
  SPA reads it that way), worth recording only because the task asked for exact observed header
  names.

---

## 6. Epistemics

**[VERIFIED] — driven live against the running server, response observed directly:**
everything in §1's matrix, the D-07 probe (§2), the meta-field wire shapes (§3), the stream
summary bodies (§4), and all defects OBJ-AUD-01..03 / STR-AUD-01..02. Every write/delete/rename
this agent issued is accounted for — bucket confirmed back to exact pre-audit state
(`AGENTS.md`, `readme.txt`, `vypis-1314812.pdf`, `data/`, `images/`, `logs/`, no `audit_/`
residue); stream engines were never actually mutated (100% of mutation attempts were refused
before touching state, so there was nothing to clean up there).

**[UNVERIFIED] / not-run — explicitly out of scope for this pass:**
- Embedded-path (webview/broker) mutation UX for object — this agent drove the raw HTTP API
  directly with the write token, not through a broker or the VS Code webview; the *mechanism*
  (caller-bound `X-Write-Token`) is verified, but the embedded UI's actual click-through flow
  is the `explore-spa`/UI-walk agent's territory (see `plans/dataconsole-audit/ui-walk.md`).
- VPN-down / unreachable-engine behavior for kafka/nats (`ErrUnreachable` / `showVPNGate`) —
  both engines were reachable throughout this session; the gate itself was not forced.
- `maxWriteBody` (64 MiB) boundary and any object above it — only tested the 16 MiB
  `MaxInlineBytes` read-truncation boundary (17 MiB object), not the write-side hard cap.
- Deep path-traversal / injection fuzzing of `segments` (e.g., `..`, control characters,
  extremely long keys) — object storage is a flat S3 key namespace with no filesystem
  backing, so a traversal-style key is just an unusual literal string, not a directory escape;
  treated as low-value given the architecture and not exhaustively fuzzed.
- rabbitmq / shared-storage — correctly unprovisioned per the plan's DD-8 (rabbitmq
  platform-deprecated, not-yet families) — nothing to test.
- Exact minio-go / S3-server-side reason `PutObject` with empty `ContentType` lands on
  `application/octet-stream` specifically (vs. some other default) was not traced into the
  minio-go/vendor source — treated as an empirically observed, reproducible fact (verified
  twice, two different write paths) rather than a root-caused library behavior; irrelevant to
  the fix shape either way since the real gap is "no content-type is ever supplied," not which
  default fills the gap.
