# Managed Data Console

## 1. Purpose

The Data Console is a single-binary, code-isolated subsystem that browses — and,
under a caller-bound write posture, edits — a Zerops project's managed-service
data. It is a loopback HTTP server plus an embedded single-page app: the server
resolves each managed service to a typed data provider and exposes a same-origin
JSON API; the SPA renders tables, KV entries, objects, and documents over it.

It is reached two ways, from one process:

1. **Embedded** in the editor as a first-party VS Code `WebviewPanel` (the
   Zerops Studio extension's "Managed Data" surface; the panel is titled "Data
   Console"). The webview is host-brokered — it never talks to the console over
   the network.
2. **Standalone** in the user's real browser as a top-level tab, reached through
   code-server's authenticated `/proxy/<port>/` forward.

It is built INSIDE the `zcp` binary now (local-mode-first) but structured to lift
out to its own repo later; §3 is the boundary that makes that a `git mv`.

## 2. Product invariants

1. **The write token is the mutation boundary — not the bearer.** Every mutating
   route authorizes through one server-side choke point (`safety.Policy.
   AuthorizeWrite`) that requires the process's per-request write token. A caller
   holding only the read bearer can read everything and mutate nothing (§5).
2. **Write authority is caller-bound, not process-global.** One process serves
   both the trusted embed host and view-only standalone tabs; there is no runtime
   "armed" flag, so a successful embed write never upgrades a bearer-only caller
   (`TestWriteToken_DualClient_CallerBound`).
3. **Tokens never enter the browser.** The read bearer stays host-side in the
   embed path; the write token is held ONLY by the embed host broker. Neither is
   ever logged.
4. **Loopback only, never public.** The server binds `127.0.0.1`; it is never
   proxied to the project's public domain. Standalone reach rides code-server's
   existing gate, adding no console-specific nginx (§4.3).
5. **The engine imports zero zcp core.** The console engine lifts to a standalone
   repo unchanged; only `zcpadapter` bridges to core, through an allowlist (§3).
6. **Honest support labels.** A service type the console cannot fully drive is
   labelled view-only or not-yet, never mis-rendered as editable (§6).

## 3. Code-isolation boundary

The subsystem is the island `internal/dataconsole/{console,zcpadapter,watch,
extension}`. Two seams, pinned in both directions by an AST test AND a depguard
rule (the test covers the import set the lint enforces, so a config drift cannot
silently open the boundary).

### 3.1 Inner seam — engine ↔ adapter

- `console/` is the engine: provider contracts, family providers, safety, HTTP
  server, embedded SPA. It imports ZERO zcp core — only stdlib, its own subtree,
  and third-party drivers (minio-go, pgx, go-sql-driver/mysql, go-redis,
  clickhouse-go, kafka-go, nats.go). Pinned by `TestDataConsoleBoundary_
  CoreIsolated` + depguard `dataconsole-core-isolated`.
- `zcpadapter/` is the ONLY bridge to core, implementing the `console.Host` seam.
  It reaches core through an enumerated allowlist (auth, ops, platform, topology,
  and the console/provider packages) — adding an edge is a deliberate contract
  change. Pinned by `TestDataConsoleBoundary_AdapterImportsAllowlistedOnly` +
  depguard `dataconsole-adapter-allowlist`.

### 3.2 Outer island — core ↔ subsystem

No core package imports `internal/dataconsole/...` except a handful of enumerated
composition points: `cmd/zcp/studio.go`, `cmd/zcp/studio_console.go`, the
`cmd/dcseed` seed CLI, `internal/init/adapters/studio.go`, and
`internal/init/vscode.go`. So the whole subtree lifts out without unpicking core.
Pinned by `TestDataConsoleBoundary_CoreDoesNotImportSubsystem` + depguard
`core-not-dataconsole`.

### 3.3 Extraction contract

On extraction, only `zcpadapter/` is rewritten; `console/` moves byte-for-byte.
The inner rules pin the engine↔adapter seam INSIDE the subsystem; the outer rule
pins the core↔subsystem seam AROUND it.

## 4. Reach model

The console child is a long-lived local process spawned by `zcp studio console
serve`. It binds loopback, prints ONE private ready-line to stdout
(`{url, sessionToken, writeToken, pid, allowWrites}` — a secret channel the
parent reads over the spawn pipe, NEVER a log), then serves HTTP and logs only to
stderr. A single per-workspace process is reused across repeat opens (the session
manager never spawns a rival); if the parent dies without killing it, the stdin
pipe EOFs and the child shuts down (no orphan).

### 4.1 Embedded — WebviewPanel + host broker

The SPA IS the webview content (loaded as webview resources, not framed from a
URL). Its CSP is `default-src 'none'` with NO `connect-src`, so it physically
cannot fetch the console. All data flows webview → host broker → loopback
console. The broker (in the extension host) holds the bearer and dials the
console; the bearer therefore never enters the browser. Because the broker will
execute whatever the webview asks, it is the trust boundary, and it carries two
structural guards so an XSS in the SPA's rendering of untrusted data cannot turn
the host into a localhost SSRF: the destination is FIXED to the console's own
`127.0.0.1:<port>`, and only the exact method+`/api` path shapes the server
exposes are allowed (mirrors `server.go` routes).

### 4.2 Standalone — browser tab under the code-server gate

Embed is the only first-party surface — there is NO in-UI "open in browser"
button. But the console stays reachable as a plain browser tab (a developer
opening the loopback URL through code-server's own port proxy), and that path is
supported READ-ONLY by construction. A standalone tab holds ONLY the read bearer,
carried in the URL fragment (`#t=<bearer>`, never the query/log); it has no write
token, so every mutation is refused server-side. The SPA is prefix-safe: its asset
and `/api` URLs are document-relative, so it serves correctly under code-server's
`/proxy/<port>/` prefix (the trailing slash before the fragment is load-bearing
for that resolution). Pinned by `TestServerSmoke_StandaloneUnderProxyPrefix`.

### 4.3 No console-specific nginx

The console is either native webview content (embed) or a code-server-proxied tab
(standalone). There is NO console-specific nginx location: the standalone tab
rides code-server's own `/proxy/<port>/`, which lives behind the container's
existing nginx cookie gate (the same `location /` that gates code-server on
`:8081`). An earlier design added a gate-exempt `location /dcproxy/` tunnel that
rewrote to code-server's generic `/proxy/<port>/` — a live-proven bypass that let
an unauthenticated visitor reach ANY localhost port (including code-server, which
runs `--auth none`). That tunnel was the wrong approach; it is deleted and must
stay gone. Pinned by `TestNginxTemplate_NoDCProxyTunnel` (forbids `/dcproxy/`;
requires code-server reachable from exactly one cookie-gated location).

## 5. Caller-bound write posture

This is the security core. Writes are refused by default; authorizing one is
CALLER-BOUND, per request, and routes through the single server-side choke point
`safety.Policy.AuthorizeWrite`, called by the mutating-route middleware in
`server.go` (the sole non-test call site).

### 5.1 The write token is the boundary

The boundary is the per-request `X-Write-Token`, constant-time compared against
the process write token — an INDEPENDENT secret from the read bearer. It is
minted only under `--allow-writes`, emitted once on the private stdout ready-line,
and held ONLY by the trusted embed host broker. The broker attaches it to a
mutating request only after a native VS Code confirmation modal, and it is
fail-closed: an absent confirm callback is treated as NOT approved, never as
implicit consent. The standalone SPA receives only the bearer, never the write
token, so it is view-only BY CONSTRUCTION and the server enforces it. Because
authority is per request, not a process flag, a successful embed write never
lifts a bearer-only caller (`TestWriteToken_DualClient_CallerBound`). Every
capability failure returns the SAME `ErrReadOnly` — no oracle reveals which
condition failed (`TestWriteToken_ArmingNotPermitted`,
`TestWriteToken_StandaloneGap_ConfirmWithoutTokenIsRefused`).

### 5.2 Arming ceiling and engine-level read-only

`--allow-writes` is the arming CEILING, not "writes are live". Absent, the process
mints no write token (`AuthorizeWrite` always refuses) AND every provider is built
hard read-only at the engine level — SQL runs in a `READ ONLY` transaction, KV is
restricted by command allowlist, object/document providers are read-only, stream
is read-only by nature. Present, the ceiling is lifted and the per-request write
token becomes the live boundary. A read-only query is always available regardless
of posture.

### 5.3 Confirm and Origin are not the boundary

`X-Confirm` is a per-action INTENT signal (the client opts each mutation in), not
the boundary: a bearer holder can set it freely, so it can never be what
authorizes a write. The per-request Origin check on mutating routes is
defense-in-depth against CSRF — it passes the no-Origin broker and same-host or
loopback pages — never the boundary (`TestWriteToken_OriginGuard`).

## 6. Family taxonomy

Each managed service type is classified to a data-shape Family (`Classify`) and a
support tier (`SupportFor`); `DerivedCaps` folds family + tier + write posture
into the affordance profile. Pinned by `TestClassify`, `TestSupportFor`,
`TestServiceActions_ConformsToSupportAndPosture`.

| Family | Service types | Support | Notes |
|---|---|---|---|
| tabular | postgresql, mariadb, mysql | full | browse, arbitrary read-only SQL, cell/row edit |
| tabular | clickhouse | view-only | async mutations — browse/query only |
| kv | valkey (Redis protocol; keydb classified but removed from the platform) | full | string values + hash/list/set/zset entries via typed-command allowlist; TTL shown read-only, set/clear gated |
| object | object-storage (S3) | full | browse, read, edit, upload, rename, delete |
| document | elasticsearch, meilisearch, typesense | full | documents as JSON blobs by id (REST over stdlib `net/http`) |
| document | qdrant | view-only | vectors are not human-editable |
| stream | kafka, nats | view-only | a message log is not an editable dataset |
| file | shared-storage | not yet | mount-only, no network endpoint — deferred |
| unknown | anything unclassified/new | not yet | falls to `FamilyUnknown`, surfaced as not-yet, never mis-rendered |

Support tiers are `SupportFull` / `SupportViewOnly` / `SupportNotYet`. Writes
require the full tier AND the arming ceiling AND the per-request write token; a
view-only or not-yet family is read-only regardless of posture.

## 7. Excellence contracts

A live audit of every family against real engines (2026-07-16, distilled findings
in `plans/dataconsole-audit/`) found the dominant defect class was not UX
unevenness but **operations that reported success while destroying, losing, or
never applying data**. The excellence contracts fix what "excellent" means per
family, as four cross-family invariants + a shared error/state vocabulary; each
family states only its deltas. Every normative rule here is an acceptance
criterion the per-engine conformance suite
(`internal/dataconsole/console/provider/conformance/`) proves — a capability with
no green case is a claim, not a supported tier.

### 7.1 The four systemic invariants

- **I-1 — Success never lies (accepted / applied / visible).** A `200`/`{"ok":true}`
  MUST mean the write is *applied* (a subsequent authoritative read returns it) or
  MUST carry an explicit non-applied status. It is never returned on mere enqueue
  (meilisearch writes are task-polled to terminal — `ErrTimeout` (504) is the
  honest "accepted, not confirmed"), on an idempotent no-op that changed nothing
  (object delete of a missing key → `ErrNotFound`; `Applied.Affected` carries the
  real reply count), on a cross-type clobber (a KV `WriteBlob` over an existing
  collection → `ErrWrongType` (409), never a silent overwrite), or on a value the
  wire corrupted (§7.3).
- **I-2 — Errors are honest and structured.** Every error, the auth 401 included,
  is the JSON envelope `{code,status,message,service,family,action,requestId}` with
  `service`+`family` populated on *every* route (body-addressed mutations resolve
  them from the decoded path). Distinct conditions map to distinct sentinels
  (§7.2): a validation rejection is not an outage (a not-found relation/index →
  `ErrNotFound`, not a flat `502`), a missing resource is not "unsupported", an
  over-cap body is `413`, and the same condition across sibling engines maps to the
  same sentinel (kafka and nats both 404 a missing topic/stream). Raw driver causes
  never cross the wire (§9.1).
- **I-3 — Values keep their fidelity.** A value read, displayed, and written back
  round-trips exactly (§7.3).
- **I-4 — Create and identity are explicit and immutable.** A family that can
  create offers an explicit, collision-refusing create path (KV collection-create
  and document create both `ErrConflict` on a name/id collision, never a clobber);
  identity is immutable during an edit — a path/payload identity mismatch is
  refused (document id-immutability guard), never silently re-identifying or
  duplicating a record; a SQL primary-key column is non-editable in v1.

### 7.2 Shared sentinel taxonomy

The one typed error set (`provider/errors.go`), referenced by every family's error
contract — a family maps its conditions onto these, never a flat catch-all:
`ErrInvalid` (400) · `ErrReadOnly` (403, the sole capability-failure signal — no
oracle) · `ErrNotFound` (404) · `ErrNeedsConfirm`/`ErrConflict`/`ErrWrongType`
(409) · `ErrTooLarge` (413) · `ErrUnsupported` (422) · `ErrUpstream` (502, a
sanitized real outage) · `ErrUnreachable` (503, the VPN gate) · `ErrTimeout` (504,
accepted-not-confirmed).

### 7.3 I-3 — the value-fidelity wire contract

The wire could silently corrupt or discard a VALUE even on an otherwise-authorized,
successful mutation. DD-9 resolved this as one contract, not a point patch:

1. **bigint/int64 transports as exact decimal text end-to-end.** `server.go`'s
   `decode()` decodes every mutating body with `json.Decoder.UseNumber()`, so a
   bare JSON integer in a field typed `any` (`CellEdit.NewValue`/`ExpectedOld`/
   `RowKey`, `InsertRow`/`DeleteRow`'s row/key maps) arrives as `json.Number`
   (the exact source text) instead of a `float64` — plain `encoding/json`
   already loses precision above 2^53 at PARSE time, before any provider runs.
   The tabular provider then binds a `json.Number` to the SQL driver as a
   native Go string (`bindArg`), never letting it re-enter a float
   intermediate; pgx/mysql both parse a string parameter against a numeric
   column from its decimal text directly. A concretely-typed field (`int`,
   `*int64`, `*float64`, …) is unaffected — `UseNumber()` only changes the
   `interface{}` fallback.
2. **Timestamps serialize with full sub-second precision.** The tabular
   provider's `normalize()` formats a `time.Time` via `RFC3339Nano`, not
   `RFC3339` — a plain `RFC3339` read-back never matches a microsecond-
   precision `timestamptz`/`DATETIME` column, so using it as a later edit's
   `expectedOld` spuriously 409s.
3. **S3 content-type is carried on write.** `WriteBlob`'s signature is
   `WriteBlob(ctx, path, data, contentType string) error` on both
   `provider.ObjectProvider` and `provider.KVProvider` (KV ignores it — a
   redis string has no MIME concept; a document/stream provider ignores it
   too, for the same reason). `PUT /api/blob`'s body carries an optional
   `contentType` field (the SPA's read-edit-save round-trip echoes back what
   it read); `POST /api/upload` reads the multipart part's declared
   `Content-Type`. Either path falls back to `http.DetectContentType` when the
   caller supplies none — never an empty string, which minio-go/S3 itself
   default to `application/octet-stream`, degrading every later read's
   textual/image classification.
4. **A key's no-TTL state is a distinct sentinel, never the literal 0.** Redis
   `TTL` replies `-1` for "exists, no expiry"; the KV provider's `Stat` and
   `ReadBlob` both surface that as an absent/nil `ttlSeconds`, never `0` (which
   the SPA would otherwise render as "expires in 0s").
5. **`InsertRow` echoes the new row's key.** `Applied.Key` (`map[string]any`,
   omitted when unknown) carries the inserted row's primary key so a caller
   can address it for a follow-up edit/delete without a separate lookup: a
   caller-supplied key is echoed verbatim; a server-generated one is recovered
   via `RETURNING` (Postgres) or `LastInsertId` (MySQL/MariaDB, single-column
   PK only — a multi-column generated PK is left `nil` rather than guessed).

Pinned by `TestEditCell_BigintJSONNumber_BindsExactText`,
`TestNormalize_TimestampPreservesSubSecondPrecision`,
`TestWriteBlob_CarriesContentTypeToS3`, `TestStat_NoTTL_ReportsNilSentinel`,
`TestInsertRow_Postgres_ReturningEchoesGeneratedKey` and their siblings.

### 7.4 Presentation contract (server-declared, SPA-rendered)

The server DECLARES presentation metadata; the SPA renders it — it never guesses.
`Node.Meta` is a typed, discriminated `NodeMeta` (`size`/`modified`/`contentType`/
`etag`/`entryType`/`count`/`ttlSeconds`, each populated only where a provider knows
it); a KV key's `entryType` drives a per-redis-type tree glyph so a hash/list/set/
zset/string is visually distinct. `Column` carries per-column `editable`+`reason`
(a PK column is `editable:false reason:"primary key"`; a view-only tier's columns
all `false`), so the SPA's edit affordances follow server truth. The SPA is one
canon: ONE grid renderer (tables, KV collections, AND read-only SQL query results),
ONE state rendering per condition (loading / empty / error / unreachable-VPN /
read-only-posture / view-only-no-key — the three "cannot edit" conditions each get
a distinct visible reason, never a shared silent no-op), ONE meta-chip renderer,
one "Load more" pagination. A qdrant point read carries `BlobMeta.Vector` (+
`X-DataConsole-Vector`) so the SPA collapses the raw embedding behind a summary; a
stream summary carries `BlobMeta.StreamMetadata` (+ `X-DataConsole-StreamMetadata`)
so the SPA labels it a metadata card, never an editable-looking blob.

### 7.5 Per-family contracts (deltas)

- **tabular** (postgresql/mariadb/mysql full; clickhouse view-only) — browse,
  arbitrary read-only SQL (engine-enforced `READ ONLY` tx / `readonly=1`), cell/row
  edit + insert (with key echo) + delete; value fidelity per §7.3 (bigint,
  timestamp); PK columns non-editable; a missing table → `ErrNotFound`.
- **kv** (valkey full) — SCAN `:`-tree with per-type glyphs; string values +
  hash/list/set/zset entries via a typed-command allowlist; collection-create
  (collision-refusing); `WriteBlob` never clobbers a collection (`ErrWrongType`);
  no-TTL is the nil sentinel.
- **object** (S3 full) — prefix tree with size+modified chips; read/edit/upload/
  rename/delete; content-type carried on write; delete of a missing key →
  `ErrNotFound`; a folder (prefix) delete/rename is refused, not a false single-key op.
- **document** (elasticsearch/meilisearch/typesense full; qdrant view-only) —
  index/collection browse, per-id docs as JSON, bounded read-only search
  (`/api/search`, ids-only, no engine highlight HTML), create + edit with the
  id-immutability guard; meili writes are task-confirmed (I-1); qdrant points render
  as a collapsed-vector summary.
- **stream** (kafka/nats view-only) — a labelled metadata card (partitions/subjects,
  message counts, consumer info best-effort with an explicit "unavailable"), never
  message content; every mutation refuses with the one `ErrReadOnly` signal. Message
  peek is a deferred non-goal.
- **file/unknown** (shared-storage, unclassified) — honest not-yet: zero
  affordances, never mis-rendered as browsable.

The design decisions behind these (DD-1…DD-9, the recipe-first substrate, the
substrate/conformance split) are recorded in `plans/archive/dataconsole-excellence-
program-2026-07-16.md` and its contracts draft; this section is their durable home.

## 8. Install

The console SPA ships in the Zerops Studio VS Code extension. `zcp init`
materializes that extension, dispatching on the runtime host — the desktop and
container installers write the IDENTICAL extension tree and differ only in target
dir and the `extensions.json` entry shape each editor host expects.

- **In-container (code-server).** `InstallStudioExtensionContainer` writes to
  `~/.local/share/code-server/extensions/zerops.zcp-studio-<ver>/` and registers
  the code-server entry shape (`location.{$mid,fsPath,external,path,scheme}` +
  `metadata.installedTimestamp`). It is gated on `ZCP_VSCODE=true` and lands at
  container boot — a plain `zcp init` installs the cockpit alongside the bootstrap
  extension, no `--vscode` flag needed. Pinned by
  `TestContainerSteps_VSCode_InstallsManagedData`,
  `TestInstallStudioExtensionContainer_*`.
- **Desktop.** `InstallStudioExtension` writes to `~/.vscode/extensions/` with the
  leaner desktop entry shape. Pinned by `TestInstallStudioExtension_*`.

Both are idempotent and non-destructive: prior `zerops.zcp-studio-*` version dirs
are reset before the fresh write; exactly one manifest entry is kept (dedupe
across versions, `installedTimestamp` preserved); every other extension's entry
round-trips byte-for-byte; a pre-existing malformed index is refused, not
overwritten (the whole profile's blast radius is never ours). The folder write is
the success gate — registration is best-effort and surfaces a reload hint on
failure, never a fatal init error. The Go `studioExtVersion` const is parity-
pinned to the extension's `package.json` version
(`TestStudioExtVersion_ParityWithPackageJSON`).

## 9. Security posture and known inherited exposures

### 9.1 Console defense-in-depth

Beyond the write boundary (§5), the console layers:

- **Loopback bind** — `127.0.0.1` only; never public-domain-proxied.
- **Per-process read bearer** on every `/api/*`, constant-time compared (any web
  page can reach `127.0.0.1`, so every request must present it);
  `TestWriteToken_RequiresBearer`.
- **`frame-ancestors 'none'`** — the console is never iframed (embed = webview
  content, standalone = top-level tab), so clickjacking surface is zero;
  `TestServerSmoke_CSPFrameAncestorsNone`.
- **Blob download-hardening** — `GET /api/blob` forces
  `application/octet-stream` + `Content-Disposition: attachment` + a sanitized
  content-type header, so attacker-controlled bytes never render inline on the
  console origin (a stored-XSS → bearer-theft path).
- **SQL read-only transaction** for arbitrary queries; **KV allowlist-by-
  construction** (only typed commands are ever issued).
- **Sanitized provider errors** — responses carry public sentinel messages only;
  the raw cause goes to the stderr diagnostic sink, never the client.
- **Broker guards** (embed) — fixed loopback destination + method/path allowlist
  mirroring the server routes (§4.1).

### 9.2 Inherited platform exposures (deferred, not introduced here)

Two exposures pre-exist on `main` and are recorded honestly. The console rides
the existing gate correctly and introduces neither; both are platform/main issues
to fix separately.

- **(a) code-server binds `0.0.0.0:8081` with `--auth none`**
  (`internal/service/service.go`). Anything on the project's private network can
  reach code-server directly, bypassing the nginx cookie gate — which widens the
  console's blast radius (a network peer can reach the standalone tab's
  code-server host without the cookie). Recommendation: bind code-server to
  loopback so the nginx gate is the only ingress.
- **(b) `VSCODE_PASSWORD` is logged.** It is used verbatim as the path component
  of `location = /zcp-auth/{{.Password}}` in the nginx template, and nginx
  `access_log` is on, so the password lands in the access log. Recommendation:
  move the secret out of the URL path (or exclude that location from access
  logging).

## 10. Non-goals

The console is not exposed to the public domain and is not a multi-user service.
v1 does not edit clickhouse, qdrant, stream, or file families (§6). The
production-posture discriminator (`safety.Policy.environment`) is reserved and
empty in v1; a future production posture gates harder off it.
