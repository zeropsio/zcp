# S11 live audit — KV family (Valkey)

Program: `plans/dataconsole-excellence-program-2026-07-16.md` §4 S11. Live console,
writes armed, engine `cache` = `valkey:single@7.2` (full support), project
`zcp-eval-clean`. Method: direct HTTP against the running console
(`http://127.0.0.1:54020`) with curl, cited against the wire contract in
`internal/dataconsole/console/server/server.go`, `provider/types.go`,
`provider/kv/kv.go`, and the SPA's KV-entry logic in
`console/webui/dist/dc-rows.js`. Every check below was executed live this
session unless explicitly marked otherwise; tag `[VERIFIED]` = observed live,
`[CODE]` = established by reading source only.

**Fixture note (resolved).** `leaderboard` was driven from a zset to the string
`"oops"` while live-reproducing KV-AUD-01 below, and the console API has no path
to recreate a destroyed collection (KV-AUD-03) — flagged to team-lead mid-audit
(msg `01122183-6695-4b74-9bee-609f5de2f74b`). Team-lead applied the S10b-fix
(delete-before-write seed convergence) and re-ran `dcseed`; `leaderboard` is
confirmed restored as of this revision (`GET /api/table` → zset,
`alice:100, carol:175, bob:250`, matching the original seed exactly). Left as
historical evidence of the exact defect below; not re-broken.

---

## 1. Per-operation verdict matrix

| Area | Operation | Verdict | Evidence |
|---|---|---|---|
| Tree | root list, virtual `:` grouping | passed | `queue`/`user`/`session` containers + `leaderboard`/`tags`/`greeting` leaves, matches seed shape |
| Tree | cursor pagination (30 extra keys, `limit=5`) | passed | 8-page walk, 37/37 unique nodes, zero drops/dupes |
| Tree | malformed cursor (tree + table, both codec families) | passed (graceful) | garbage cursor → fresh start, no 500 |
| Stat | type + TTL for string/hash | passed | `user:1`→string, `user:2`→hash, `session:abc123`→ttlSeconds 3155 |
| Stat | missing key | passed | `404 not_found` |
| ReadBlob | string value + headers | passed | content correct; `ttlSeconds`/`size` from `BlobMeta` **never surface** (KV-AUD-05) |
| ReadBlob | missing key | passed | `404 not_found` |
| ReadBlob | binary round-trip (256-byte all-values incl. NUL) | passed | byte-identical |
| ReadBlob | oversize value (1.5 MiB vs 1 MiB `MaxInlineBytes`) | passed (truncates) | `Truncated:true`, exactly 1,048,576 bytes, byte-identical head-slice; true size never reported (KV-AUD-05) |
| WriteBlob | create/update string, round-trip | passed | |
| WriteBlob | **on an existing collection key** | **FAILED (destructive)** | KV-AUD-01 — silently replaces the collection with a string |
| Delete | whole key via `DELETE /api/node` | passed | |
| Delete | whole key via `DELETE /api/blob` | passed | identical to `/api/node` — same handler, intentional aliasing |
| TTL | set → stat → clear → stat (string key) | passed | 120s → 0 (persisted) |
| TTL | set → clear on a **hash** key | passed | EXPIRE/PERSIST are type-agnostic; no kv-only restriction |
| TTL | "no expiry" representation | **defect** | Stat always reports `ttlSeconds:0`, never a negative/null sentinel (KV-AUD-02) |
| ReadTable | hash (`field`,`value`, RowKeyCols=`[field]`) | passed | |
| ReadTable | list (`index`,`value`, RowKeyCols **empty**) | passed (confirms D-03) | |
| ReadTable | set (`member`, RowKeyCols=`[member]`) | passed | |
| ReadTable | zset (`member`,`score`, RowKeyCols=`[member]`) | passed | |
| ReadTable | missing key | inconsistent | `422 unsupported`, not `404` — Stat special-cases `"none"`, ReadTable doesn't (KV-AUD-09) |
| ReadTable | on a string-typed key | passed (honest) | `422 unsupported` |
| SetEntry | hash field value edit (`field`+`value`) | passed | matches SPA's post-fix wire shape |
| SetEntry | zset score edit, explicit score (`field`+`score`) | passed | matches SPA's post-fix wire shape |
| SetEntry | zset, **value-only, no score field** | **defect** | silently `ZADD`s score **0** — KV-AUD-10 |
| SetEntry | zset, both value+score | passed (value ignored) | score wins, as documented |
| SetEntry | set member rename (SREM+SADD, `field`=old,`value`=new) | passed | |
| SetEntry | list index edit (LSET, `field`="14") | passed (confirms D-03) | server-level edit works; UI never exposes it |
| SetEntry | **on a nonexistent key** | confirms gap | `422 unsupported` — no collection-creation path (KV-AUD-03) |
| SetEntry | on a string-typed key | passed (honest) | `422 unsupported`, key untouched |
| DeleteEntry | hash field / set member / zset member, round-trip | passed | |
| DeleteEntry | list (any index) | passed (honest) | `422 unsupported`, list untouched — D-03 delete-side confirmed |
| DeleteEntry | on a string-typed key | passed (honest) | `422 unsupported`, key untouched |
| DeleteEntry | nonexistent field within an existing hash | inconsistent | `200 {"affected":1}` though 0 fields were removed (KV-AUD-06) |
| Write auth | no write token, bearer+confirm | passed | `403 read_only` |
| Write auth | write token, no `X-Confirm` | passed | `409 needs_confirm` |
| Write auth | write token+confirm, cross-origin | passed | `403 read_only` (uniform, no distinct origin error) |
| Write auth | no bearer at all | passed | `401`, before write-token check |
| Write auth | wrong write token | passed | `403 read_only`, same as no-token (no oracle) |
| Write auth | full correct (token+confirm, no Origin = broker) | passed | `200`, mutation applied |
| Write auth | 401 response shape | inconsistent | plain `text/plain`, not the JSON error envelope (KV-AUD-08) |
| Error paths | missing `service` param | passed | `400 invalid` |
| Error paths | unknown service name | passed | `404 not_found`, `service` populated (GET/query path) |
| Error paths | malformed JSON body | passed | `400 invalid` |
| Error paths | malformed `segs` (tree) | passed (graceful) | silently treated as root, no error |
| Error paths | error envelope on mutating (body-addressed) routes | inconsistent | `service`/`family` always empty, only `action` populated (KV-AUD-04) |

---

## 2. Targeted verdicts (brief items a–e)

### (a) D-03 verdict — list editability: server works, UI correctly refuses

Server-level `SetEntry` on a list (`LSET`) **works**: live round-trip on
`queue:jobs` index 14 (`job-3` → `audit-lset-test` → restored to `job-3`),
`200 {"statement":"LSET","affected":1}` both ways
(`internal/dataconsole/console/provider/kv/kv.go:352-360`).

`ReadTable`'s list case never sets `RowKeyCols`
(`kv.go:244-261`, `TablePage{Columns: cols2("index","value"), Rows: rows}` — no
`RowKeyCols:` field), so `rowKeyCols` is `null` on the wire. The SPA's
`entryEditPlan` in `console/webui/dist/dc-rows.js:35-39` locks **every** cell
when `rowKeyCols.length === 0`, and `renderGrid`'s delete-button gate
(`app.js:586`, `keyCols.length > 0`) does the same — so no cell is clickable and
no delete button renders for a list, full stop. This is a deliberate, correct
UI choice (confirmed by the code comment at `dc-rows.js:27-29`: "A row with no
key column at all (redis list…) has no safe entry identity, so every cell is
locked"), not an oversight. `DeleteEntry` on a list returns an honest
`422 unsupported` and leaves the list untouched (verified live).

**Verdict: D-03 stands exactly as characterized — LSET is server-capable but
deliberately unreachable from the UI; delete is honestly unsupported both
server- and UI-side.** No UI dead-button (the delete affordance simply doesn't
render for lists), so there's no misleading control — worth noting as
correct-by-omission rather than a gap needing a fix.

### (b) D-01 / D-02 — server-side contract confirmation

The SPA fix (this branch) is entirely **client-side**, and the live server
contract behind it is unchanged and correctly matches what the fixed SPA now
sends:

- **D-01 (zset)**: `dc-rows.js:48-54` sends `{field: member, score: <number>}`
  for the `score` column, never `value`. Live: `SetEntry` with explicit
  `field`+`score` applies correctly (`ZADD`, verified round-trip on
  `leaderboard`/bob 250→260→250). This closes the old bug (SPA used to always
  resend the OLD score). The SPA also *never* sends a value-only payload
  against a zset (its `score` column is mandatory-numeric,
  `entryEditPlan`'s `member` column is locked, `dc-rows.js:39,48-54`) — but the
  server accepts one anyway, and it's silently destructive. New finding,
  same shape as KV-AUD-01: **KV-AUD-10**, written up below.
- **D-02 (hash)**: `dc-rows.js:41-43` locks the `field` (key) column whenever a
  sibling non-key column exists, so the SPA can no longer send
  `{field: oldField, value: newFieldNameText}`. Live: editing the `value`
  column (`{field:"role", value:"member-edited"}`) applies correctly
  (`HSET`, verified round-trip on `user:2`).
- **Residual, by design**: the server itself performs **zero** field/value
  discrimination — `kv.go:347-351` HSETs whatever `Field`/`Value` a caller
  sends, with no protection against the old D-02 shape. I confirmed this is
  intentional (not a leftover gap) per the `dc-rows.js:19-29` comment
  explaining the fix is a UI-side lock because "the /api/entry wire shape
  carries one Field + one Value/Score" — there's no server-side way to
  distinguish "this is a key-column edit" from "this is a value-column edit"
  without the column/RowKeyCols shape the SPA has and the server doesn't need.
  Any *other* caller of `/api/entry` (a different embed host, a script) that
  reproduces the old shape would still corrupt a hash field's value. Not
  re-flagging as a new defect — it's the documented shape of the existing fix
  — but worth the explicit confirmation the brief asked for.

**Verdict: both server contracts match what the corrected SPA sends; both
fixes are UI-side locks over an unchanged, permissive server API.**

### (c) New defects

**KV-AUD-01 [CRITICAL — worst finding this session]**
`WriteBlob` (`PUT /api/blob`, the *string*-value write path) performs an
unconditional Redis `SET` with **no type check**. Called against an
**existing hash/list/set/zset key**, it silently destroys the whole collection
and replaces it with a plain string. `200 {"ok":true}`, no warning, no distinct
confirmation beyond the normal write-token gate. Upgraded from HIGH to CRITICAL
per team-lead review: it is silent (no error, no partial-failure signal),
irreversible through any documented API (KV-AUD-03), and — as demonstrated —
took a real fixture down mid-audit with no recovery path short of a direct
database command.

**Exact reproduction:**

1. Precondition — `leaderboard` is a zset:
   `GET /api/table?service=cache&segs=["leaderboard"]` →
   `{"columns":[{"name":"member","pk":true},{"name":"score"}],"rows":[["alice",100],["bob",250],["carol",175]],"rowKeyCols":["member"]}`.
   `GET /api/stat?service=cache&segs=["leaderboard"]` →
   `{"kind":"tabular","meta":{"type":"zset","ttlSeconds":0}}`.
2. Action — `PUT /api/blob` with body
   `{"path":{"service":"cache","segments":["leaderboard"]},"data":"<base64 of "oops">"}`,
   headers `Authorization: Bearer <read-bearer>`, `X-Write-Token: <write-token>`,
   `X-Confirm: true` (the same auth shape every other successful mutation in
   this audit used — nothing distinguishes this call as more dangerous).
3. Result — `200 {"ok":true}`.
4. Postcondition —
   `GET /api/stat?service=cache&segs=["leaderboard"]` →
   `{"kind":"blob","meta":{"type":"string","ttlSeconds":0}}` (kind flipped
   `tabular`→`blob`, type flipped `zset`→`string`).
   `GET /api/blob?service=cache&segs=["leaderboard"]` → body `oops`.
   The 3-member zset (`alice`, `bob`, `carol` with their scores) is gone; no
   trace of it survives anywhere the API can read.
5. Recovery attempted — `PUT /api/entry` (`SetEntry`) against the same path
   with `{"field":"alice","score":100}` → `422 unsupported` (client-visible
   body is the generic envelope, `{"code":"unsupported",...}`; the specific
   cause — `kv: "string" not an editable collection` — is server-side only,
   from `kv.go:380-381` / stderr diagnostics, never returned to the caller,
   see KV-AUD-04). `DELETE` then `SetEntry` again → `422 unsupported` the same
   way (internal cause `kv: "none" not an editable collection`, KV-AUD-03).
   **No documented API call sequence recovers the original zset.** Team-lead
   restored it out-of-band via a `dcseed` re-run after landing the S10b-fix
   convergence work — i.e., recovery required a maintainer with direct
   database access re-running the seeder, not anything available through the
   console itself.

**Design analysis (requested by team-lead — where does the fix belong?):**

- **Guard site: none exists.** `WriteBlob`'s entire body
  (`internal/dataconsole/console/provider/kv/kv.go:298-309`) is:
  ```go
  func (p *Provider) WriteBlob(ctx context.Context, path provider.Path, val []byte) error {
      if p.caps.ReadOnly {
          return provider.ErrReadOnly
      }
      ctx, cancel := context.WithTimeout(ctx, opTimeout)
      defer cancel()
      if err := p.cli.Set(ctx, keyOf(path), val, 0).Err(); err != nil {
          return fmt.Errorf("kv: set: %w", provider.ErrUpstream)
      }
      return nil
  }
  ```
  The only precondition checked is the engine-level `ReadOnly` (arming) flag.
  There is no `TYPE` lookup, no comparison against the key's current shape, no
  rejection path of any kind — every other type-sensitive KV method
  (`SetEntry`, `DeleteEntry`, `ReadTable`) does a `TYPE` call first and
  dispatches/refuses accordingly; `WriteBlob` is the one KV write method that
  doesn't look before it overwrites.
- **SPA reachability: API-only, not reachable through normal navigation.**
  Traced the full dispatch chain in `console/webui/dist/app.js`:
  - A tree row's click handler dispatches purely on the node's server-supplied
    `kind` (`app.js:378-389`): `container` → `expandContainer`, `tabular` →
    `openTable` (grid view, backed by `/api/table` + `/api/entry`), else
    (`blob`) → `openBlob` (backed by `/api/blob`).
  - `saveBlob()` — the sole caller of `PUT /api/blob` in the entire SPA (the
    other 3 `/api/blob` call sites are all `GET`, for read/thumbnail/download,
    confirmed by grep) — is wired only inside `openBlob()` (`app.js:459`),
    which is itself only reachable when a node's `kind !== "tabular"` and
    `!== "container"`.
  - KV's own `List()`/`leaf()` (`kv.go:165-171`) sets `kind:"blob"` **only**
    when the key's live `TYPE` is `"string"` at listing time, and
    `kind:"tabular"` for any of hash/list/set/zset. A node the tree currently
    reports as a collection therefore routes exclusively to `openTable`; the
    SPA never even builds a blob editor for it, so there is no "Save" button
    to click.
  - There is also no free-text "create/open arbitrary key" affordance for KV
    (checked: the only `promptModal` asking for a key name is the object-family
    rename flow, `app.js:495`, and `renameObject` isn't in KV's action set at
    all — confirmed via `/api/services`). So a user can't typo their way into
    this either.
  - **Conclusion: the SPA structurally cannot trigger KV-AUD-01 in its own
    normal use.** I reached it only by calling `PUT /api/blob` directly against
    a path I already knew (from a prior read) was a zset — i.e., this is an
    API-only footgun, not a UI gap. Any *other* caller of the API (a script, a
    different or future embed host, a bug in some client the console doesn't
    control) hits it with zero protection.
- **Where the fix belongs:** server-side, not the UI. The SPA's kind-dispatch
  already avoids this path for its own flows, so a UI-side lock would be
  redundant there — the actual exposure is that the *server* accepts a
  cross-type overwrite from anyone holding a write token, regardless of
  client. This is the same "the UI is never a boundary" principle
  `server.go`'s own package doc states for write authorization
  (`server.go:1-8`) — it should extend to type-safety too, since a write token
  today authorizes silent, irreversible collection destruction as a side
  effect of what looks like an ordinary string write.
  **Recommended shape**, for whoever picks up the P0 slice: `WriteBlob` should
  `TYPE`-check first (it already talks to the same client every other KV
  method uses for exactly this); if the current type is an existing
  hash/list/set/zset, refuse with a typed, honest error rather than
  overwriting — consistent with the idiom this same file already uses for
  "this operation doesn't safely apply here" (`SetEntry`'s `default:` case,
  `DeleteEntry`'s list case). `ErrConflict` (409, already wired end-to-end for
  optimistic-concurrency mismatches) reads naturally for "the resource is not
  the shape you're overwriting it as"; a dedicated sentinel is the other
  option if 409/`conflict` is judged too overloaded. Exact error code is an
  implementation-slice decision, not an audit one.

**Compounding (KV-AUD-03):** once a collection is destroyed this way (or
simply deleted), the console cannot recreate it — see KV-AUD-03 below. The two
findings are separable fixes: KV-AUD-01 is "refuse the destructive write" (a
guard), KV-AUD-03 is "add a way to create a collection at all" (new
capability) — a server guard alone would have prevented the `leaderboard`
incident without requiring the create-collection affordance to exist first.

**KV-AUD-02 [HIGH, live + visually confirmed]** A key with **no TTL** reports
`ttlSeconds: 0` from `Stat`, not a negative/null "no expiry" sentinel.
Live-confirmed on a freshly created never-TTL'd key and on a TTL explicitly
cleared via `PERSIST` — both read back `ttlSeconds: 0`
(`kv.go:185,190-191`: `ttl, _ := p.cli.TTL(...)`  then
`"ttlSeconds": int64(ttl.Seconds())` with no guard for the "no expiry"
sentinel — contrast `ReadBlob` at `kv.go:217-220`, which correctly guards with
`ttl > 0` before setting `TTLSeconds`, but that field never reaches the wire
anyway, see KV-AUD-05). The SPA's only "no expiry" check is
`app.js:759`: `(ttl == null || ttl < 0) ? "no expiry" : ttl + "s"` — since the
server never sends a negative value, this branch **never fires** for a
persisted key. **Independently confirmed visually**: the sibling UI-walk audit
(`plans/dataconsole-audit/ui-walk.md:21`) observed "`kv hash (user:2) ...
TTL: 0s bar below`" on `user:2`, which has no TTL — the exact misdisplay this
finding predicts, caught by a completely different method (screenshot vs API
trace). **Every KV key without a TTL — the common case — shows a misleading
"TTL: 0s" instead of "no expiry".**

**KV-AUD-03 [HIGH]** There is no collection-creation path anywhere in the
console API. `SetEntry` TYPE-dispatches on the key's *current* Redis type
(`kv.go:342-346`); a nonexistent key reads `TYPE` `"none"`, which falls into
the same `default: return ..., ErrUnsupported` branch
(`kv.go:379-381`) as an actually-unsupported existing type — live-confirmed on
a fresh nonexistent key (`422 unsupported`). None of `HSET`/`RPUSH`/`SADD`/
`ZADD`-on-a-new-key is reachable through any route in `server.go`. This means
(1) a user can never intentionally create a new hash/list/set/zset through the
console — only string keys are creatable (`WriteBlob`) — and (2) a collection
destroyed via KV-AUD-01, or any deleted collection, cannot be recreated
in-console. Symmetric with the already-known D-08 (no whole-key *delete*
affordance for collections in the UI); this is the creation-side mirror, and
unlike D-08 it's a hard *server* gap, not a UI omission.

**KV-AUD-04 [MEDIUM, cross-cutting — not KV-specific, discovered via KV]**
Every mutating (body-addressed) route's error envelope omits `service` and
`family`, leaving only `action` populated. `withRouteContext`
(`server.go:175-183`) derives `service` **only** from
`r.URL.Query().Get("service")`. GET routes (`tree`/`stat`/`blob` read/`table`)
put the service in the query string, so their envelopes are fully populated
(verified: 404 on missing key shows `"service":"cache","family":"kv"`). Every
POST/PUT/DELETE route (`blob` write, `cell`, `row`, `entry`, `upload`,
`rename`, `ttl`, `node` delete) carries the service **inside the JSON body**
(`path.service`), which `withRouteContext` never reads — confirmed on 3
separate body-routed error responses (`422` from `SetEntry` on a nonexistent
key, `422` from list `DeleteEntry`, `400` from malformed TTL body): none carry
`service`/`family`, all carry `action`. This is a `server.go` architecture
issue affecting every family equally, not KV-specific — flagging here since
this is where I found it; likely relevant to the tabular/object/document audit
siblings too. Weakens error-log triage for every mutation failure across the
whole console.

**KV-AUD-05 [MEDIUM]** A truncated (or any) blob read never surfaces its true
byte size anywhere. `BlobMeta.Size` and `BlobMeta.TTLSeconds` are computed by
the KV provider's `ReadBlob` (`kv.go:203,217-220`) but `handleBlob`'s `GET`
case only turns `ContentType` and `Truncated` into headers
(`server.go:341-344`) — `Size`/`TTLSeconds` are silently dropped in transport.
KV's `Stat` meta doesn't carry size either (`kv.go:190-191`: only
`type`/`ttlSeconds`). Live-confirmed with a 1.5 MiB value against the 1 MiB
`MaxInlineBytes` cap: `X-Dataconsole-Truncated: true`, exactly 1,048,576 bytes
returned, but nothing in the response (headers or a follow-up `Stat` call)
tells the caller the value is actually 1.5 MiB rather than, say, 1,048,577
bytes. `WriteBlob` has no size cap of its own (`kv.go:298-309`) beyond the
transport's 64 MiB body limit (`server.go:31`), so the gap between "visible"
and "actual" size can be up to ~63 MiB with zero indication.

**KV-AUD-06 [LOW]** `Applied.Affected` is a hardcoded literal `1` on every
successful KV `SetEntry`/`DeleteEntry`, never the real Redis command reply
count (`kv.go:351,360,369,378` for `SetEntry`;
implicit `Applied{Statement:"DEL-ENTRY", Affected:1}` at `kv.go:414` for all
three `DeleteEntry` cases, regardless of `HDEL`/`SREM`/`ZREM`'s actual return).
Live-confirmed: `DeleteEntry` on a hash field that does **not exist** returns
`200 {"statement":"DEL-ENTRY","affected":1}` though `user:2` still has exactly
its original 3 fields — nothing was removed, "affected" lied.

**KV-AUD-07 [LOW, live + visually confirmed]** Tree nodes carry no type
metadata. `List`'s `leaf()` helper (`kv.go:165-171`) sets only `Kind`
(`blob` for string, `tabular` for any of hash/list/set/zset — collapsing all
four into one kind) and never populates `Node.Meta`. So `hash`/`list`/`set`/
`zset` are visually and structurally indistinguishable in `/api/tree` output
— confirmed live (`leaderboard` zset and `tags` set both render
`"kind":"tabular"` with no distinguishing field) — until the client opens the
node (`ReadTable`) or issues a separate `Stat` call, which *does* carry
`type` (`kv.go:186-189`). **Independently confirmed visually**: sibling
UI-walk audit `UI-AUD-04` (`plans/dataconsole-audit/ui-walk.md:44-47`):
"Inside a KV service you cannot tell a hash from a list from a zset from a
string before clicking" — same root cause, found by screenshot rather than by
reading `List()`.

**KV-AUD-08 [LOW]** The bearer-auth failure (`401`) is the only response on
the whole API that doesn't use the JSON error envelope: `auth()`
(`server.go:217-226`) calls `http.Error(w, "unauthorized", 401)` directly,
producing `Content-Type: text/plain; charset=utf-8` and a bare
`"unauthorized\n"` body — no `code`/`status`/`message`/`requestId`. Live-
confirmed via header dump. Every other error path (400/403/404/409/413/422/
502/503) uses the `errorEnvelope` JSON shape (`server.go:765-803`). A client
parsing errors uniformly needs a 401 special case.

**KV-AUD-09 [LOW]** `ReadTable` on a nonexistent key returns `422 unsupported`
(`"kv: \"none\" not a collection"`), not `404 not_found`. `ReadTable`'s type
switch (`kv.go:234-295`) has no `"none"` case, so a missing key falls into the
generic `default:` branch — unlike `Stat`, which explicitly special-cases
`t == "none"` → `provider.ErrNotFound` (`kv.go:182-184`). Live-confirmed on
both a genuinely nonexistent key and (incidentally) on `user:1`, whose type
mismatch made this easy to trigger against real data. A user who opens a tree
node that was deleted moments earlier (by another client, or their own prior
action) sees a confusing "unsupported" rather than "not found".

**KV-AUD-10 [HIGH — same class as KV-AUD-01, smaller blast radius]**
`SetEntry` against a **zset**, given a `value` but no `score`
(`Score *float64` absent/`null`), silently `ZADD`s the member at score **0**
instead of rejecting the request or leaving the score untouched — verified
live and reproducible: with `leaderboard` at `carol:175`, `PUT /api/entry`
`{"path":{...,"segments":["leaderboard"]},"field":"carol","value":"<base64
'ignored-text'>"}` (no `score` key at all) → `200 {"statement":"ZADD","affected":1}`;
`carol`'s score reads back as `0`, and the `value` text is discarded entirely
(zsets have no per-member payload beyond the score). Root cause:
`kv.go:370-378` —
```go
case typeZset:
    var score float64
    if e.Score != nil {
        score = *e.Score
    }
    if err := p.cli.ZAdd(ctx, key, redis.Z{Score: score, Member: e.Field}).Err(); err != nil {
```
`Score == nil` silently falls through to Go's zero value (`0.0`) rather than
being treated as "not provided, don't touch the score" or rejected as invalid.
Same design-analysis shape as KV-AUD-01: **no guard site** (the `nil` check
exists only to pick a default, not to refuse), and **API-only reachable** —
the SPA's `entryEditPlan` never omits `score` for a zset's score column (it's
validated as a mandatory finite number before the request is even built,
`dc-rows.js:48-54`) and locks the `member` column entirely (`dc-rows.js:39`),
so this shape can only arise from a caller other than the current SPA: a
different/future embed host, a hand-built request, or a copy-paste of the
hash-family `{field,value}` shape against a zset path by mistake. Recommended
fix shape mirrors KV-AUD-01's: require `Score` to be non-nil for a zset
`SetEntry` and return a typed `ErrInvalid`/`ErrConflict` when it's missing,
rather than defaulting to zero.

### (d) UX-relevant payload observations

- **TTL presentation is actively wrong for the common case** — see KV-AUD-02.
  Most keys have no TTL; most keys will show "TTL: 0s".
- **Type is invisible until you commit to opening a node** — see KV-AUD-07.
  The tree's `container`/`tabular`/`blob` triage tells you shape-of-navigation
  (does it have children, is it a grid or a value) but never redis type; a
  user cannot visually distinguish a `set` from a `zset` from a `hash` without
  opening each one or knowing to check `Stat` separately. `ReadTable`'s
  response *does* disambiguate immediately via column shape (`field`/`value`
  vs `member` vs `member`/`score` vs `index`/`value`) — the information exists,
  it's just one click later than the tree.
- **Truncation is a one-bit signal with no magnitude** — see KV-AUD-05. A user
  sees "truncated" with no way to know whether they're missing 1 byte or 63
  MiB.
- **`DELETE /api/blob` and `DELETE /api/node` are the same operation** —
  confirmed live (identical behavior, and `server.go:94,105` route to the same
  `handleNode`). Not a defect, just worth recording: the two-route surface for
  whole-key delete is intentional aliasing, not two different capabilities.
- **Malformed `segs`/`cursor` degrade silently to a fresh start** rather than
  erroring (`server.go:730-736` swallows the JSON-unmarshal error on `segs`;
  `kv.go`'s cursor decoders fall back to a zero-value state on any parse
  failure). Defensible (never crashes, never 500s) but means a client-side
  encoding bug would silently reset pagination instead of surfacing — verified
  across both cursor codec families (SCAN-state for tree/hash/set, integer
  offset for list/zset).

### (e) Epistemics

- **blocked**: none of the brief's checks were blocked by tooling or access —
  every item in §Ground rules/Protocol was executed.
- **not-run**: the engine-level `ReadOnly` gate (process started without
  `--allow-writes`) — this session's server is fixed at `allowWrites:true`;
  that boundary is covered by existing offline/unit tests (plan §1.5 T-06),
  not re-verified live here since there's no read-only instance to test
  against in this session.
- **not-run**: concurrent/race conditions (two write-token holders racing the
  same entry) — not practical to sequence deterministically via curl; no
  evidence either way.
- **not-run**: actual browser/DOM interaction — deferred to the sibling
  UI-walk audit; I cross-referenced its `user:2` TTL screenshot (KV-AUD-02)
  and tree-glyph finding (KV-AUD-07) where they independently corroborate a
  server-side finding, but did not myself drive the rendered UI.
- **not-investigated**: root cause of `user:1` holding a legacy string instead
  of the seeded hash — pre-existing before I began (per the brief's warning),
  left untouched, not chased further (out of scope for an API/contract audit).
- **not fully explored**: `WriteBlob`'s upper size bound — tested at 1.5×
  `MaxInlineBytes` (1.5 MiB); did not probe near the 64 MiB HTTP body cap
  (`server.go:31`). The truncation mechanism is a simple threshold compare
  already validated at its boundary, so low expected marginal value, but
  strictly unverified at the extreme.
- **resolved**: `leaderboard` fixture corruption (KV-AUD-01's live
  reproduction) — flagged to team-lead mid-audit; repaired via the S10b-fix
  (seed convergence) landing plus a `dcseed` re-run, outside this audit's
  file-write-only scope. Confirmed restored (see top of report).

---

## Fixture integrity

Verified restored to original seeded values after all testing:

- `user:1` — string `"alice"` (pre-existing drift, untouched by me, not
  restored to a hash since I never changed it)
- `user:2` — hash `{name:Bob, email:bob@example.io, role:member}`
- `tags` — set `{go, zerops, data, console}`
- `queue:jobs` — list `[job-1,job-2,job-3] × 5` (15 elements; note: `RPUSH` in
  `seedValkey` isn't idempotent, so this 5× duplication predates my testing
  and reflects repeated seed runs, not something I caused — corroborates the
  already-tracked seed-idempotence work, no new defect ID)
- `greeting` — string `"hello from seed"`
- `session:abc123` / `session:def456` — untouched, TTL still counting down
- `leaderboard` — zset `{alice:100, bob:250, carol:175}`; driven to a string
  while reproducing KV-AUD-01, restored out-of-band by team-lead (S10b-fix +
  `dcseed` re-run); confirmed matching the original seed exactly as of this
  revision
- All `audit_*` scratch keys created during testing were deleted; tree root
  confirmed back to exactly the original 6 entries
