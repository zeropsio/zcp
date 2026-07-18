# S11 audit — document family (es, search=meili, docs=typesense, vectors=qdrant)

Live API audit of the Managed Data Console's document-family wire contract against
`zcp-eval-clean` (write-armed, `allowWrites:true`). Scope: `/api/*` only — no SPA/UI
driving (that's `ui-walk.md` / the embedded-path walk). Code read first:
`internal/dataconsole/console/server/server.go`, `.../provider/{types,errors,actions,family}.go`,
`.../provider/document/{document,transport,elasticsearch,meilisearch,typesense,qdrant}.go`.
Evidence: live HTTP round trips captured 2026-07-16, request/response bodies and headers
inspected directly (not summarized from memory). All `audit_*`-prefixed fixtures created
during this run were deleted; all containers verified back to seeded baseline (es/products
1-3, search/products 1-3, docs/products 1-2, vectors/items 1-3) at close. No seeded doc was
modified. No code touched.

Static predictions this audit verifies: plan D-05 (`meilisearch.go:97-107`,
`typesense.go:86`), D-06 (`document.go:190-193`).

## 1. Operation × verdict matrix

| Operation | es | search (meili) | docs (typesense) | vectors (qdrant) |
|---|---|---|---|---|
| Tree: list containers (`GET /api/tree segs=[]`) | passed | passed | passed | passed |
| Tree: list docs, paginate (`limit=2`, walk cursor to exhaustion) | passed | passed | passed (see §6 phantom-next) | passed |
| Stat (`GET /api/stat`) | passed | not-run (same code path as es) | not-run | not-run |
| ReadBlob (`GET /api/blob`) — pretty JSON, headers | passed | passed | passed | passed |
| ReadBlob truncation (D-06, cap=16 MiB) | **blocked** (§4 — write ceiling below cap) | passed | passed | blocked (view-only, can't create an oversized point) |
| WriteBlob create (`PUT /api/blob`) | passed | passed (async, see §5) | passed | correctly refused (403 `read_only`) |
| WriteBlob update | passed | passed (async) | passed | correctly refused |
| Delete (`DELETE /api/blob`) | passed | passed (async) | passed | correctly refused, data unmodified (verified) |
| Upload (`POST /api/upload`, multipart) | passed (works; not UI-advertised — §6) | not-run | not-run | not-run |
| D-05 probe: id/pk mismatch in body vs path | **failed** (3 distinct per-engine behaviors — §3) | **failed** | **failed** | n/a (read-only) |
| Unsupported routes (table/query/cell/row/entry/ttl/rename) | passed (clean 422 `unsupported` on all 7) | not-run (same code path) | not-run | not-run |
| Error paths: nonexistent index/doc/service | passed (clean 404s) | not-run | not-run | not-run |
| Error paths: malformed write body | passed (clean 400) | not-run | not-run | not-run |
| Error paths: envelope over 64 MiB `maxWriteBody` | passed-with-finding (§4 DOC-AUD-02) | not-run | not-run | not-run |
| Write-token/confirm/bearer gates on document family | passed (403/409/403/401 all correct — §7) | not-run (object-family already pinned by `server_writetoken_test.go`; this run confirms document family specifically) | not-run | not-run |

"passed-with-finding" = behaves as coded, but the coded behavior is itself a gap worth
fixing. "failed" on the D-05 row = the probe's own point (D-05 is a known, code-confirmed
defect; this row records that live behavior matches/exceeds the code prediction).

## 2. D-06 verdict — confirmed exactly as coded

`document.go:190-193`: `out := out[:p.caps.MaxInlineBytes]` is a hard byte-slice of the
**pretty-printed** JSON, not a value-aware truncation.

Live: wrote a 17,000,040-byte document (`{"id":"audit_big1","title":"<17,000,000 × 'a'>","price":1}`)
to `search/products` and `docs/products` (MaxInlineBytes = 16 MiB = 16,777,216 B for every
document engine, `document.go:32`). `GET /api/blob` on both returned:

- `Content-Length` / body size: **exactly 16,777,216 bytes** — confirms the slice bound precisely.
- `X-Dataconsole-Truncated: true` — the only signal; HTTP status is **200 OK**, not an error.
- Body tail: `...aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`
  — cut mid-string, no closing quote or brace. **Genuinely invalid JSON.** A client that
  trusts `Content-Type: application/json` (or tries to parse the body because
  `X-Dataconsole-ContentType: application/json`) and only checks HTTP status will parse-fail
  on a 200 response.
- `Content-Disposition: attachment` + `Content-Type: application/octet-stream` are set
  regardless (this is the standard ReadBlob envelope, not D-06-specific — see §6).

Verdict: **CONFIRMED, precisely as the static read predicted.** No new mechanism found;
the only news is coverage — see §4 DOC-AUD-03 for the ES-specific reachability gap.

## 3. D-05 verdict — confirmed, and the failure mode is worse/more varied than "creates a duplicate"

The plan's one-line framing ("editing a document's id/pk field creates a duplicate instead
of updating") undersells it: live testing found **three distinct per-engine behaviors**,
one of which is silent **data loss**, not duplication.

**Elasticsearch** (`_doc/{id}` — id is path metadata, never a body field in modern ES):
- Body contains reserved `_id` field, differs from path id → **ES rejects the write outright**
  (mapper parsing exception, surfaces to the console as a flattened `502 upstream error` —
  see DOC-AUD-02). Correct-shaped refusal, but the console gives the caller no way to tell
  "you can't do that" from "the service is down."
- Body contains an ordinary (non-reserved) `id` field differing from the path id → **accepted**;
  document is stored at the **path** id; the body's `id` field is just inert user data
  alongside it. `GET products/totally-different-b` confirmed 404 — no phantom doc created.
  This is the one case that behaves the way a naive user would expect.

**Meilisearch** (`meilisearch.go:97-107` — `putDoc` PUTs `/indexes/{container}/documents`
with the body array-wrapped; the `id` **path parameter is never referenced in the function
body** — the request URL carries no id at all):
- Path id = `audit_d05c`, body primaryKey (`id`) = `audit_d05c_BODYID` → after the write
  settled (~1.35 s, see §5), `GET products/audit_d05c` = **404** (nothing was ever created
  there); `GET products/audit_d05c_BODYID` = **200**, full content present. The console
  returned `{"ok":true}` for a write that landed at a document the caller never named.
- Body **omits** the primaryKey field entirely (index already has a declared primaryKey
  `"id"`, from the seed fixture's first doc) → console still returns `{"ok":true}`
  immediately (enqueue succeeds). After a 2.5 s settle, the target path is 404 **and the
  full container listing shows no new/phantom entry anywhere** — the document was never
  created, full stop. Meilisearch's async task processor rejected it (missing primary key
  is a hard per-document validation failure in Meilisearch), but the console never queries
  task status, so **this failure is invisible at every layer the console exposes.** See
  DOC-AUD-01 — this is the standout finding of the audit.

**Typesense** (`typesense.go:86` — `POST .../documents?action=upsert`, body must carry `id`;
URL also carries no id):
- Path id = `audit_d05e`, body id = `audit_d05e_BODYID` → same shape as meili: target path
  404, body's id exists with full content. Console said `{"ok":true}`. Notably this
  reproduces with **zero async lag** (typesense upserts synchronously) — proving the
  misrouting is a **routing** defect, not a timing artifact of eventual consistency.
- Body **omits** `id` → **differs from meilisearch**: Typesense did not reject it. It
  auto-assigned its own sequential id (**`"5"`**, a plain small integer string — not a UUID)
  and created the document there. `GET products/audit_d05f` (the intended path) = 404;
  `GET products/5` = 200 with the submitted content. The console's `{"ok":true}` response
  gives the caller **zero indication the id changed** — no assigned-id echo anywhere in the
  response body.

**Net**: DD-6's target invariant ("path identity is immutable during Save; payload/path
identity mismatch is rejected") is violated by all three writable engines today, in three
different ways: ES rejects on the reserved-field collision but accepts+ignores on the
ordinary-field collision (closest to correct, still not "rejected on mismatch"); Meilisearch
silently misroutes or silently drops; Typesense silently misroutes or silently
auto-relocates. None of the three currently implement "refuse on mismatch." All test
artifacts cleaned up (`products/audit_d05b`, `products/audit_d05c_BODYID`,
`products/5`, `products/audit_d05e_BODYID` deleted; containers verified back to baseline).

## 4. New defects (DOC-AUD-nn)

**DOC-AUD-01 [HIGH] — Meilisearch writes can silently and permanently fail with no error
at any layer; the console's `{"ok":true}` does not mean applied, and nothing ever tells the
caller otherwise.**
Root cause: `meiliEngine.putDoc` (`meilisearch.go:97-107`) treats a `2xx` from
`PUT /indexes/{uid}/documents` as success and returns nil — but that endpoint only
**enqueues** a Meilisearch task; the console never polls `GET /tasks/{id}` to confirm the
task actually applied. Concretely demonstrated with a missing-primaryKey document (§3), but
the root cause is general: **any** per-document validation failure inside Meilisearch's async
indexer (type mismatch against a previously-inferred field type, a malformed geo field, etc.)
would fail the same invisible way. This is the same severity class as the KV corruption
hotfixes (D-01/D-02) — a write that silently does nothing while reporting success is a
correctness/trust violation, not a polish gap. Directly informs U-14's "mutation feedback
lies about state" and should be weighed for the same treatment.

**DOC-AUD-02 [MED] — Non-2xx responses are flattened to indistinguishable generic codes at
two separate layers, so the console cannot tell a client-shaped error from an outage.**
- *Upstream layer*: `transport.statusErr` (`transport.go:76-85`) maps 404→`ErrNotFound` and
  401/403→`ErrUpstream`, but **every other status** (400 validation errors, 409, 413, 500,
  etc.) also maps to the same `ErrUpstream` → **502 "upstream error."** Live-confirmed: ES's
  400 rejection of a reserved `_id` field in the body (§3) and ES's undocumented
  write-size ceiling (DOC-AUD-03) both surface as the identical `{"code":"upstream","status":502,
  "message":"upstream error"}` a real outage would produce. A caller/agent cannot
  distinguish "fix your payload" from "the service is down, back off and retry."
- *Console layer*: independently, exceeding the console's own `maxWriteBody` (64 MiB,
  `server.go:31`) is enforced via `io.LimitReader` inside `decode()` (`server.go:746-753`),
  which silently **truncates** the request body rather than rejecting it — the truncated
  stream then fails JSON parsing, surfacing as `400 invalid` (live-confirmed: an 80,000,110-byte
  envelope → `{"code":"invalid","status":400,"message":"invalid request"}`). The type system
  already defines a dedicated `ErrTooLarge` → 413 (`provider/errors.go:20`) for exactly this
  shape of problem, but the document family (and this global cap) never produce it — a
  legitimate large write and a client sending garbage both look like the same 400.

**DOC-AUD-03 [LOW] — Elasticsearch's own write-size ceiling sits below the 16 MiB
read-truncation cap, making D-06 unreachable via the console's write path for ES
specifically.** Binary-searched live: a raw document body of 10,600,000 bytes writes
successfully; 12,500,000 bytes and above fail (flattened 502, per DOC-AUD-02 — the ceiling's
exact value and source, e.g. ES's `http.max_content_length` on this managed instance, isn't
directly introspectable through the console and wasn't pinned further). Since `MaxInlineBytes`
is a family-wide constant (16 MiB, `document.go:32`) applied uniformly across es/meili/typesense/qdrant,
but only meili and typesense can actually produce a document large enough to trigger it
through the console's own write path, ES's D-06 behavior can only be observed on a document
that arrived via some other route (bulk import, direct API, pre-existing data) — never one
the console itself wrote. Worth deciding explicitly whether `MaxInlineBytes` should be
per-engine-aware, or whether this asymmetry is accepted as-is.

**DOC-AUD-04 [LOW] — Cursor pagination has a reproducible "phantom next page" at exact page
boundaries, shared by es/meili/typesense's identical `len(hits) == limit` cursor heuristic.**
When a container's total document count is an exact multiple of the requested `limit`, the
server has no way to peek ahead and returns a non-empty `nextCursor`; following that cursor
then returns a legitimately empty page (`{"nextCursor":"","nodes":[]}`). Live-reproduced
twice: typesense's `products` (2 docs, `limit=2` → phantom next) and elasticsearch's
`products` (3 docs, `limit=3` → phantom next, `nextCursor:"3"` → empty). Not unsafe and
arguably a defensible "client must tolerate an empty terminal page" contract — but it is not
documented anywhere as intentional, and a naive "Load more" affordance would fire once for
nothing. Feeds U-09 (pagination UX) if the SPA doesn't already handle an empty non-final
response gracefully — that check belongs to the UI-walk pass, not verified here.

## 5. Visibility-delay table (the accepted/applied/visible contract, U-14)

Methodology note: the first pass measured ReadBlob-visibility and List-visibility
**sequentially** (poll one to completion, then start the other), which biases the second
measurement's baseline whenever the first is slow. Caught on the first meilisearch run
(ReadBlob 3.9 s, List then falsely read "0 ms" because polling only started after the 3.9 s
had already elapsed) and fixed: all numbers below are from **interleaved polling from a
shared t0** (both endpoints checked every 150 ms in the same loop, starting immediately
after the write's HTTP response). 150 ms poll granularity; 1-2 live samples per cell (see §7
epistemics) — treat exact figures as order-of-magnitude, not precise SLAs.

| Engine | Write model | Console 200/`ok:true` means | Create → ReadBlob visible | Create → List visible | Update → ReadBlob visible | Delete → ReadBlob 404 | Delete → List absent |
|---|---|---|---|---|---|---|---|
| es | Sync index + near-real-time (NRT) search | Applied to the primary (translog-durable) | ~0 ms | 0-750 ms (refresh-cycle-bound; 2 samples: 0 ms, 0 ms create / 600 ms, 750 ms delete) | ~0 ms | ~0 ms | 600-750 ms |
| search (meili) | Async task queue | **Enqueued only** — not applied, not visible | ~1350 ms (1 sample) | ~1350 ms (same task, tracks together) | ~1200 ms | ~900-1200 ms | ~900 ms (tracks with ReadBlob-404) |
| docs (typesense) | Fully synchronous upsert | Applied | ~0 ms | ~0 ms | ~0 ms | ~0 ms | ~0 ms |
| vectors (qdrant) | n/a (read-only) | n/a | n/a | n/a | n/a | n/a | n/a |

Key asymmetry, **es**: `getDoc` (`GET /_doc/{id}`, a real-time translog-backed read) is
consistently instant; `docs()`/List (`POST /_search`, refresh-cycle-bound) lags up to
~1 refresh interval. A user who writes/deletes a document and immediately checks the **open
blob view** sees the truth instantly; the same user glancing at the **tree** can see stale
state for the better part of a second. This is invisible in the API response either way —
`{"ok":true}` looks identical in both cases.

Key finding, **meilisearch**: create/update/delete are *all* async with observed latency in
the ~0.9-1.35 s band on an idle single-node instance under no other load — this is a floor,
not a ceiling; a busier queue would be slower. The console's `{"ok":true}` is returned the
instant the task is *enqueued*, not when it's *applied*. This is the direct evidence behind
DOC-AUD-01 and the strongest live data point for U-14's document-family half.

## 6. UX payload observations (U-13 context; qdrant rendering)

- **No search/filter/query route exists for the document family at the wire level at all** —
  confirmed, not inferred. `/api/table` and `/api/query` both require provider interfaces
  (`ReadTable`/`Query`) that `document.Provider` never implements; every call returns a clean
  `422 unsupported`. The *only* browse primitives are id-cursor `List` and id-keyed
  `ReadBlob`. For es/meili/typesense — engines whose entire purpose is search — the console
  currently offers none. This is the wire-level confirmation behind U-13; DD-2's proposed
  design (bounded server-built query for es/meili/typesense, qdrant payload-filter deferred)
  would need to reconcile with the **already-divergent per-engine cursor mechanics** this
  audit pinned precisely: es uses a `from` offset, meili an `offset`, typesense a 1-based
  `page` number, qdrant an opaque engine-minted scroll token. A search endpoint's pagination
  contract can't assume any one of these shapes.
- **Qdrant's single-point `ReadBlob` always includes the full `vector[]` array**, even though
  the `List`/tree-scroll call explicitly requests `with_payload:true, with_vector:false`
  (`qdrant.go:47-58` vs `getDoc` at `qdrant.go:85-97`, which sets neither flag — Qdrant
  defaults a single-point GET to including the vector). Live-observed shape for a 4-dim toy
  vector: `{"id":1,"payload":{"name":"alpha"},"vector":[0.1,0.2,0.3,0.4]}`. This directly
  confirms `ui-walk.md`'s **UI-AUD-03**, previously tagged `[INFERRED]` — promote it to
  `[VERIFIED]`: at real embedding dimensionality (384/768/1536 floats) this is a wall of
  numbers dumped inline with no summarization, and it's not a UI rendering choice, it's what
  the API itself returns.
- **JSON field order in `ReadBlob` is whatever the upstream engine emits**, unmodified —
  `prettyJSON` (`document.go:240-246`) is `json.Indent` over the raw bytes, never a
  parse/remarshal, so key order is engine-authentic, not Go-map-alphabetized. Observed:
  typesense returns fields alphabetically (`id, price, title`) — a Typesense-internal
  convention, not a console artifact; ES/meili roughly preserve document-shape/insertion
  order. Purely cosmetic (JSON key order is semantically inert) but a human comparing the
  same logical document shape across two engines side-by-side would see inconsistent
  ordering with no explanation.
- **Upload works for the document family even though the UI never offers it**: `POST
  /api/upload` (multipart) hits the same `WriteBlob` type-assertion as `PUT /api/blob`
  (`server.go:592-608`), and `document.Provider` implements `WriteBlob` — so a raw multipart
  upload against `es`/`search`/`docs` succeeds today, confirmed live. `ServiceActions`
  (`actions.go:130-131`) only grants `FamilyDocument` the `writeBlob`/`deleteNode` mutating
  affordances, never `uploadObject` — so the SPA correctly never shows an upload button for
  these services, but the server-side capability is live and reachable by anyone holding a
  write token (e.g. via a hand-crafted request, or a future UI change that forgets this is
  family-gated only client-side). Not a security issue (same auth gates apply), just worth
  an explicit decision: is "upload replaces a JSON document" a real feature, or dead-but-live
  surface that should be excluded at the `Caps()`/provider level too, not just the UI layer?

## 7. Epistemics

- **[VERIFIED]** unless stated otherwise — every finding above was produced by a live HTTP
  round trip against the running console in this session and, where content mattered, by
  reading the response body back, not inferred from the code alone (though the code was read
  first per the brief, to know what to probe and to cite exact line numbers).
- **[UNVERIFIED]/not pinned precisely**: the *exact* byte value of Elasticsearch's write
  ceiling (DOC-AUD-03) — bounded live to (10,600,000B succeeds, 12,500,000B fails) but not
  binary-searched to the exact boundary; the mechanism is presumed to be an
  `http.max_content_length`-shaped upstream cap based on the range, not confirmed by reading
  the managed ES instance's own config (no route to that from the console/this audit's
  access).
- **Sample sizes are small** (1-2 live samples per visibility-delay cell, §5) — meilisearch's
  async task latency in particular is queue-depth-dependent and will vary under real load;
  treat the ~0.9-1.35 s figures as "this floor exists and is not negligible," not as a
  precise SLA. A rigorous version of §5 would run each cell N≥10 times and report a
  distribution; that's a natural fit for S10a/S10b's conformance-suite promotion, not a
  single manual audit pass.
- **Blocked, not failed**: D-06 on qdrant (can't create an oversized point — provider is
  view-only by design, `document.go:81` forces `readOnly=true` unconditionally for qdrant
  regardless of the session's `--allow-writes` posture) and D-06 on es (§4 DOC-AUD-03 — blocked
  by ES's own write ceiling, not a console defect in itself).
- **Not-run, out of scope for this pass**: any SPA/DOM rendering (owned by the UI-walk pass —
  `ui-walk.md`), the embedded (VS Code webview / mocked-broker) mutation-UX path (this audit
  only exercised the raw API with the given bearer + write token, not through the extension
  broker), `Stat`/error-path repetition across all four services once the code path was
  confirmed identical on the first (object-family write-token gate tests already cover the
  cross-cutting auth machinery in `server_writetoken_test.go`; this pass's §1 last row is the
  document-family-specific live confirmation those unit tests don't reach).
- **Fixture hygiene**: every `audit_*`-prefixed id created during this run was deleted; the
  one accidental non-`audit_`-prefixed artifact (typesense's auto-assigned id `"5"` from the
  D-05f probe, §3) was identified, confirmed, and deleted. No seeded fixture doc (ids `1`/`2`/`3`
  in any container) was modified. Final sweep (§ close of this run) confirmed all four
  containers (`es/products`, `search/products`, `docs/products`, `vectors/items`) match their
  pre-audit baseline exactly.
