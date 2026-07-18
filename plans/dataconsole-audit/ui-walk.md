# S11 audit — UI walk (standalone tab, looks-right dimension)

Orchestrator's puppeteer walk of the live console (standalone read-only tab, all 11
engines seeded). Read-path + presentation only; mutation UX belongs to the embedded
path (write toggle is hidden by construction here — see below). Per-engine API rigor
is in the sibling `tabular.md` / `kv.md` / `document.md` / `object-stream.md`.
Evidence = screenshots captured 2026-07-16; observations tagged [VERIFIED] (seen) /
[INFERRED].

## Posture presentation — correct [VERIFIED]
- Standalone tab shows a `read-only` badge; the `Write mode` toggle is present in DOM
  but `visible:false` (hidden). Standalone-is-view-only is honestly surfaced, not
  faked. No mutation affordance leaks to the bearer-only caller.

## Per-family render (representative screenshots)

| Family / engine | Renders as | Verdict | Notes |
|---|---|---|---|
| tabular db (pg) | grid, `id 🔑` PK marker, dataType on header, "3 rows" | good | the reference view |
| tabular ch (view-only) | same grid, no edit | good | view badge in rail |
| kv hash (user:2) | field/value grid, `field 🔑`, "3 rows", `TTL: 0s` bar below | good | TTL bar is KV-only, detached at pane bottom (U-07) |
| kv tree | glyphs `▸` container / `▦` collection / `◇` blob | mixed | glyphs distinguish KIND, not FAMILY/type — a hash vs list vs set vs string is indistinguishable in the tree until opened (U-03) |
| object storage | file tree with size chips; inline `<img>` preview for png; `Download` for others | good | only family showing a meta chip; provider's modified/etag dropped (U-03) |
| document es (a1) | pretty JSON `<pre>`, `66 B · application/json`, Download | ok | no field structure, raw JSON (U-05) |
| document qdrant (point 1) | pretty JSON `{id,payload,vector[]}` | risk | vector array inline — fine at 4 dims, a real 384/768/1536-float embedding dumps a wall of numbers (new: UI-AUD-03) |
| stream nats (EVENTS) | JSON `<pre>` {bytes,firstSeq,lastSeq,messages:30,subjects} | **U-04 confirmed** | metadata rendered identically to a document blob; "messages:30" but zero messages viewable; nothing labels it "stream metadata, not content" |

## New UI findings (feed S11 rebaseline)

- **UI-AUD-01 [VERIFIED] — deep-link `#…&svc=<hostname>` does not select the service.**
  Navigating the fragment to `svc=vectors` left the view on the previously-selected
  service (breadcrumb `/ mariadb`); only a rail click switched it. The `svc=` param
  (spec §4.2 fragment) is parsed on first load at best, not on hashchange, and the
  observed first-load selection did not honor it either. Dead/again-dead deep link
  (relates to U-11 dead-auth-UI class: fragment features that don't work).
- **UI-AUD-02 [VERIFIED] — NATS `subjects` shows `events.>`.** The `>` wildcard is
  unicode-escaped in the rendered JSON (raw `json.Marshal` HTML-escaping), leaking an
  ugly `>` to a human reader. Cosmetic but visible; a symptom of dumping raw
  provider JSON with no presentation pass (U-04/U-05 root).
- **UI-AUD-03 [INFERRED] — vector payloads render as inline number walls.** Qdrant point
  JSON includes the full `vector[]`; at real embedding dimensionality this is
  unreadable. A vectors view should summarize (dim count + payload) and collapse the
  raw vector. Feeds DD-4 (document/vector presentation) + the qdrant half of S21.
- **UI-AUD-04 [VERIFIED] — families visually indistinguishable in the tree.** Rail
  shows type text (`postgresql`/`valkey`/…) + ready/view badge, but tree nodes use
  kind-only glyphs. Inside a KV service you cannot tell a hash from a list from a zset
  from a string before clicking. Feeds DD-4 meta-chip canon (U-03).

## Confirmed-healthy (no churn)
- Rail: consistent `ready` / `view` / (would-be `not yet`) badges; view-only engines
  (ch, queue, vectors, events) badged `view`, full engines `ready`. Honest labels.
- Object image inline preview + size chips + content-type label: the richest,
  best-realized family view — the presentation bar the others should meet.
- Tabular grid with PK marker + row count: clean, correct, the canonical grid.

## Epistemics
- not-run here (deferred to API agents / embedded path): every mutation UX (write
  toggle hidden in standalone), pagination "Load more" behavior under a real cursor,
  loading/empty/error states (only happy-path data seen), query console.
- The `svc=` deep-link finding (UI-AUD-01) is [VERIFIED] against this build but its
  ROOT (parsed-once vs never) needs a code read of app.js `bootAuth`/hashchange — not
  done here.
