"use strict";
// Zerops Data Console — framework-free SPA. One UI over a same-origin JSON API.
//
// Single owner of affordances: the SERVER tells the SPA what each service can do
// (the per-service `actions` profile in /api/services); the SPA renders edit/
// upload/delete/query/ttl affordances ONLY from that payload — it never guesses
// a write posture. Every mutation is gated again server-side (the UI is not a boundary)
// and goes through the confirm modal, which shows the exact action before commit.
//
// The bearer never enters the URL query/log: standalone reads it from
// location.hash (then scrubs); embedded receives it via postMessage.
//
// S15 — SPA unification. One canon, everywhere: ONE grid renderer drives tables,
// KV collections AND SQL query results (query = read-only via server column
// truth); ONE meta-chip renderer + per-type tree glyph reads the typed NodeMeta;
// ONE state canon (loading / empty / error / unreachable-VPN / read-only-posture /
// view-only-no-key), each with one honest rendering; ONE pagination "Load more"
// for tree + grid + query. Editability is SERVER truth (Column.editable), never a
// client guess. Mutation feedback is honest: no "Saved" before applied, and the
// `timeout` sentinel reads "accepted, still applying" — not success, not failure.

window.DC = window.DC || {};

// embedded: the console runs as content of a Studio WebviewPanel (vs its own
// top-level browser tab). Set true by the host's 'dataconsole-init' message —
// NOT by window.self!==window.top (that is false inside a webview). Drives the
// transport (postMessage RPC vs fetch) and chrome (hide the duplicate rail).
const state = { token: null, writeEnabled: false, project: null, services: [], active: null, reopen: null, embedded: false };
const CONTRACT = window.DataConsoleContract || { actionIDs: [] };
const ACTION = Object.freeze((CONTRACT.actionIDs || []).reduce((m, id) => { m[id] = id; return m; }, {}));
const DCFormat = window.DC.format;
const DCActions = window.DC.actions;
const DCRows = window.DC.rows;
const DCErrors = window.DC.errors;
const DCEmbed = window.DC.embed;
const { esc, fmt, human, baseType, isTextual, isImage, isImageName, b64 } = DCFormat;
const { rowKeyOf, entryEditPlan } = DCRows;
const { errorFromEnvelope, errorSummary, errorHTML } = DCErrors;

// editing reports whether edit affordances should render. It is true ONLY when the
// console is EMBEDDED (a VS Code WebviewPanel) AND the host has confirmed write mode
// — the embed host holds the per-request write token behind a native modal. The
// STANDALONE SPA (its own browser tab) receives only the read bearer, never a write
// token, so it is view-only by construction and never edits. Per-operation
// enablement still comes from service.actions.
function editing() { return state.embedded && state.writeEnabled; }

// Inline preview caps: blobs larger than DISPLAY_CAP are download-only (never
// dumped into the DOM); textual blobs up to EDIT_CAP are editable in a textarea.
const DISPLAY_CAP = 1 << 20; // 1 MiB — textual inline preview cap
const EDIT_CAP = 512 << 10; // 512 KiB — inline editor cap
const IMAGE_CAP = 8 << 20; // 8 MiB — inline image preview cap
const QUERY_CAP = 2000; // server-side query row ceiling (surfaced when a full page returns no cursor)

// ---------- transport ----------
// One chokepoint, two transports. STANDALONE (own tab): fetch with the fragment
// bearer. EMBEDDED (Studio WebviewPanel): a postMessage RPC to the extension
// host, which holds the bearer and proxies to the loopback console — the bearer
// never enters the webview and the webview CSP has no connect-src (no fetch).
const vscodeApi = (typeof acquireVsCodeApi === "function") ? acquireVsCodeApi() : null;
const rpcPending = {};
let rpcSeq = 0;

function b64ToBytes(s) {
  const bin = atob(s || "");
  const u = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) u[i] = bin.charCodeAt(i);
  return u;
}

// makeResponse wraps a brokered reply as a fetch-Response-shaped object so every
// call site (.ok/.status/.headers.get/.json/.arrayBuffer/.blob) is unchanged.
function makeResponse(d) {
  const bytes = b64ToBytes(d.b64);
  const headers = { get: (k) => { const v = d.headers ? d.headers[String(k).toLowerCase()] : null; return v == null ? null : v; } };
  return {
    ok: !!d.ok, status: d.status || 0, headers: headers,
    arrayBuffer: () => Promise.resolve(bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength)),
    blob: () => Promise.resolve(new Blob([bytes])),
    text: () => Promise.resolve(new TextDecoder().decode(bytes)),
    json: () => Promise.resolve(JSON.parse(new TextDecoder().decode(bytes))),
  };
}

function rpcFetch(path, opts) {
  return new Promise((resolve) => {
    const id = "r" + (++rpcSeq);
    rpcPending[id] = (d) => resolve(makeResponse(d));
    vscodeApi.postMessage({
      type: "dc-rpc", id: id,
      method: opts.method || "GET",
      path: path,
      body: typeof opts.body === "string" ? opts.body : null,
      confirm: !!(opts.headers && opts.headers["X-Confirm"] === "true"),
      upload: opts.upload || null,
    });
  });
}

function realFetch(path, opts) {
  const headers = Object.assign({ "Authorization": "Bearer " + state.token }, opts.headers || {});
  const u = path.replace(/^\//, ""); // relative so it works behind a path prefix
  return fetch(u, Object.assign({}, opts, { headers }));
}

// hostAction asks the extension host to perform a native operation the webview
// sandbox blocks (save/open dialogs). Embedded only; a no-op standalone.
function hostAction(msg) {
  if (vscodeApi) vscodeApi.postMessage(msg);
}

function api(path, opts = {}) {
  const send = state.embedded ? rpcFetch(path, opts) : realFetch(path, opts);
  return send.then(async (r) => {
    if (r.status === 401) {
      showAuth();
      const e = new Error("unauthorized");
      e.status = r.status; e.code = "unauthorized"; e.requestId = r.headers.get("X-Request-Id") || "";
      throw e;
    }
    if (!r.ok) {
      const ct = r.headers.get("Content-Type") || "";
      let payload = null;
      if (ct.includes("json")) { try { payload = await r.json(); } catch (_) {} }
      throw errorFromEnvelope(payload, r.status, r.headers.get("X-Request-Id") || "");
    }
    return r;
  });
}
const apiJSON = (p, o) => api(p, o).then((r) => r.json());

// ---------- auth bootstrap ----------
function bootAuth() {
  window.addEventListener("message", onHostMessage);
  const m = /[#&]t=([^&]+)/.exec(location.hash);
  const sm = /[#&]svc=([^&]+)/.exec(location.hash);
  if (sm) state.pendingService = decodeURIComponent(sm[1]); // deep-link target service
  if (m) {
    state.token = decodeURIComponent(m[1]);
    history.replaceState(null, "", location.pathname + location.search); // scrub token+svc
  }
  if (state.token) { start(); return; }              // standalone with a fragment bearer
  if (vscodeApi) { vscodeApi.postMessage({ type: "dc-ready" }); return; } // webview: host sends init
  showAuth();                                         // standalone, no token → paste gate
}

// onHostMessage routes every host->webview message. Embedded sessions receive
// their deep-link via 'dataconsole-init' (NO bearer — the host holds it), a
// service switch via 'dataconsole-switch-service', and brokered API replies via
// 'dc-rpc-result'.
function onHostMessage(ev) {
  const d = ev && ev.data;
  if (!d) return;
  if (d.type === "dc-rpc-result") {
    const fn = rpcPending[d.id];
    if (fn) { delete rpcPending[d.id]; fn(d); }
    return;
  }
  if (d.type === "dataconsole-init") {
    state.embedded = true;
    state.token = "embedded"; // sentinel; the real bearer is host-side only
    state.writeEnabled = !!d.writeEnabled;
    if (d.service) state.pendingService = d.service;
    hideAuth(); start();
    return;
  }
  if (d.type === "dataconsole-write-mode") {
    applyWriteMode(d.writeEnabled);
    return;
  }
  if (d.type === "dataconsole-switch-service") {
    if (d.service) {
      state.pendingService = d.service;
      if (state.services.length) { applyChrome(); openPendingService(); }
    }
    return;
  }
  if (d.type === "dataconsole-uploaded") {
    if (d.ok) { toast("Uploaded."); refreshTree(d.service); } else { toast("Upload failed.", true); }
    return;
  }
}

function openPendingService() {
  if (!state.pendingService) return;
  const svc = svcOf(state.pendingService);
  if (!svc) return; // not discovered yet — keep pending so a later refresh/switch can resolve it
  state.pendingService = null;
  if (supported(svc)) selectService(svc);
}
function showAuth() { document.getElementById("authgate").classList.remove("hidden"); }
function hideAuth() { document.getElementById("authgate").classList.add("hidden"); }

// ---------- services rail ----------
async function start() {
  hideAuth();
  try {
    const data = await apiJSON("/api/services");
    state.project = data.project;
    state.services = data.services || [];
    document.getElementById("project").textContent = state.project ? state.project.name : "";
    renderWriteMode();
    applyChrome();
    renderServices();
    openPendingService();
  } catch (e) { renderError(e); }
}

// applyChrome hides the duplicate left rail when the host (Studio) embeds the
// console AND deep-links a browsable service — the host's managed-service list is
// the selector then, so the console gives the full width to that service's data.
// Single owner of the decision: DC.embed.shouldHideServiceRail. Runs before
// openPendingService consumes state.pendingService (the deep-link target).
function applyChrome() {
  const hideRail = DCEmbed.shouldHideServiceRail({
    embedded: state.embedded,
    deepLinkedService: state.pendingService,
    services: state.services,
    isBrowsable: supported,
  });
  document.getElementById("rail").classList.toggle("hidden", hideRail);
}

function renderWriteMode() {
  const sw = document.getElementById("editswitch");
  const badge = document.getElementById("writemode");
  if (state.embedded) {
    // Embedded: the write toggle is available — enabling is host-confirmed, and the
    // per-request write token (held host-side) is what the server checks. Reflects writeEnabled.
    sw.classList.remove("hidden");
    badge.classList.add("hidden");
    document.getElementById("editchk").checked = state.writeEnabled;
    sw.classList.toggle("on", state.writeEnabled);
  } else {
    // Standalone (own browser tab): bearer-only, NO write token → view-only by
    // construction (every mutation 403s server-side). Never a write toggle; show a
    // persistent read-only indicator instead so the posture is unambiguous. This is
    // the state canon's read-only-posture rendering (P.4): one global signal, never
    // "cells silently don't respond".
    sw.classList.add("hidden");
    badge.classList.remove("hidden");
    badge.textContent = "read-only";
    badge.className = "badge view-only";
  }
}

// onEditToggle drives write mode. Only the EMBEDDED console renders the toggle, so
// this only runs embedded: defer to the host — enabling needs a native confirmation
// and the server checks the per-request write token, so post the intent and wait
// for the authoritative reply (keep the switch on its current state meanwhile). The
// standalone SPA is view-only and shows no toggle, so there is no standalone branch.
function onEditToggle(on) {
  document.getElementById("editchk").checked = state.writeEnabled;
  document.getElementById("editswitch").classList.toggle("on", state.writeEnabled);
  hostAction({ type: "dc-write-mode", enable: !!on });
}

// applyWriteMode lands the host's authoritative write-mode decision and re-renders
// the live view so affordances appear/disappear immediately.
function applyWriteMode(writeEnabled) {
  state.writeEnabled = !!writeEnabled;
  renderWriteMode();
  if (state.active) loadTree(state.active, [], document.getElementById("tree"), true);
  if (state.reopen) state.reopen();
}

function supported(s) { return s.support === "supported" || s.support === "view-only"; }

function renderServices() {
  const ul = document.getElementById("services");
  ul.innerHTML = "";
  for (const s of state.services) {
    const li = document.createElement("li");
    if (!supported(s)) li.classList.add("disabled");
    if (state.active === s.hostname) li.classList.add("active");
    li.innerHTML = `<span>${esc(s.hostname)}</span><span class="svc-type">${esc(baseType(s.type))}</span>`
      + `<span class="spacer"></span>${badge(s.support)}`;
    if (supported(s)) li.onclick = () => selectService(s);
    ul.appendChild(li);
  }
}
function badge(sup) {
  const cls = sup === "supported" ? "supported" : sup === "view-only" ? "view-only" : "notyet";
  const label = sup === "supported" ? "ready" : sup === "view-only" ? "view" : "not yet";
  return `<span class="badge ${cls}">${label}</span>`;
}

function svcOf(hostname) { return state.services.find((x) => x.hostname === hostname); }
function actionOf(hostname, id) {
  return DCActions.actionOf(svcOf(hostname), id);
}
function hasAction(hostname, id) { return DCActions.hasAction(svcOf(hostname), id); }
function actionEnabled(hostname, id) { return DCActions.actionEnabled(svcOf(hostname), id); }
function actionButton(id, label, cls, service, actionID) {
  const ctrl = DCActions.actionControl(svcOf(service), actionID, label);
  if (!ctrl.available) return "";
  const disabled = ctrl.enabled ? "" : ` disabled title="${esc(ctrl.reason || "Unavailable")}"`;
  const klass = cls ? ` class="${esc(cls)}"` : "";
  return `<button id="${esc(id)}"${klass}${disabled}>${esc(ctrl.label)}</button>`;
}
function wireAction(id, service, actionID, fn) {
  if (actionEnabled(service, actionID)) wire(id, fn);
}

function selectService(s) {
  state.active = s.hostname;
  state.reopen = null;
  // Keep the active hostname visible in the topbar — when the rail is hidden
  // (embedded under Studio) it is the only on-screen orientation cue.
  document.getElementById("activesvc").textContent = s.hostname ? "/ " + s.hostname : "";
  renderServices();
  const content = document.getElementById("content");
  if (s.support === "not yet") {
    content.innerHTML = `<div class="placeholder">${esc(s.hostname)} (${esc(baseType(s.type))}) is discovered but not yet browsable.</div>`;
    document.getElementById("tree").innerHTML = "";
    return;
  }
  let hint = `Browse <b>${esc(s.hostname)}</b> in the tree.`;
  if (actionEnabled(s.hostname, ACTION.querySQL)) hint += ` <button class="link" id="querylink">Run a query ▸</button>`;
  if (actionEnabled(s.hostname, ACTION.searchDocs)) hint += ` <button class="link" id="searchlink">Search ▸</button>`;
  if (editing() && actionEnabled(s.hostname, ACTION.createKey)) hint += ` <button class="link" id="createkeylink">Add key ▸</button>`;
  content.innerHTML = `<div class="placeholder">${hint}</div>`;
  const ql = document.getElementById("querylink");
  if (ql) ql.onclick = () => openQuery(s.hostname);
  const sl = document.getElementById("searchlink");
  if (sl) sl.onclick = () => openSearch(s.hostname);
  const ck = document.getElementById("createkeylink");
  if (ck) ck.onclick = () => createKeyForm(s.hostname);
  loadTree(s.hostname, [], document.getElementById("tree"), true);
}

// ---------- state canon (P.4) ----------
// One honest rendering per state, shared across tree / grid / blob / query.
// loading and empty here; error/unreachable-VPN via gate()/errorHTML; read-only-
// posture via the rail badge; view-only-no-key via the grid label (renderGrid).
function stateLoading(label) {
  const txt = label ? esc(label) : "Loading";
  return `<div class="state loading"><span class="spinner"></span>${txt}…</div>`;
}
function stateEmpty(label) {
  return `<div class="state empty">${esc(label || "Empty")}</div>`;
}

// ---------- tree ----------
// treeGen is a monotonically increasing tree-load epoch. Two root loads for the
// same #tree container can be in flight together (a sidebar re-browse racing
// applyWriteMode()'s own refresh loadTree()) — without a generation guard, the
// slower response's appendTreePage() re-appends onto the faster one's already-
// rendered nodes (its DOM-clear step only drops ".loadmore"/".state" placeholders,
// never previously appended node wrappers), duplicating every root entry. Every
// loadTree() call mints a new generation; appendTreePage() (and everything that
// continues its page — load-more, lone-container auto-expand) carries that
// generation forward and drops its result if a newer generation has superseded it.
let treeGen = 0;

async function loadTree(service, segs, container, root) {
  const gen = ++treeGen;
  if (root) container.innerHTML = stateLoading(""); // loading state — never a blank pane (U-10)
  await appendTreePage(service, segs, container, root, "", gen);
}

// appendTreePage loads one page of nodes and, when the server returns a cursor,
// adds a "load more" button so the whole level is reachable (no silent first-page).
async function appendTreePage(service, segs, container, root, cursor, gen) {
  const q = new URLSearchParams({ service, segs: JSON.stringify(segs) });
  if (cursor) q.set("cursor", cursor);
  let data;
  try { data = await apiJSON("/api/tree?" + q.toString()); }
  catch (e) { if (gen !== treeGen) return; container.innerHTML = gate(service, e); return; }
  if (gen !== treeGen) return; // a newer tree load superseded this one — drop the stale render
  // Drop the transient loading/empty placeholder + any prior "load more" before appending.
  container.querySelectorAll(":scope > .loadmore, :scope > .state").forEach((el) => el.remove());
  const nodes = data.nodes || [];
  if (root && nodes.length === 0 && !cursor) {
    container.innerHTML = stateEmpty("Empty");
    if (editing() && actionEnabled(service, ACTION.uploadObject)) addUploadBar(service, segs, container);
    return;
  }
  // Smart auto-expand: a lone container never costs a click — drill through.
  if (!cursor && nodes.length === 1 && nodes[0].kind === "container") {
    const el = renderNode(service, nodes[0]);
    container.appendChild(el);
    expandContainer(service, nodes[0], el, gen);
    return;
  }
  for (const n of nodes) container.appendChild(renderNode(service, n));
  if (data.nextCursor) {
    const more = document.createElement("button");
    more.className = "loadmore";
    more.textContent = "Load more…";
    more.onclick = () => appendTreePage(service, segs, container, false, data.nextCursor, gen);
    container.appendChild(more);
  }
  if (root && editing() && actionEnabled(service, ACTION.uploadObject)) addUploadBar(service, segs, container);
}

// expandContainer's gen defaults to the CURRENT epoch when absent — a manual
// user click (renderNode's onclick) is a fresh, independent action rather than a
// continuation of some earlier page load, so it belongs to whatever tree epoch is
// live at click time (and is correctly dropped if a root reload supersedes it
// before its fetch resolves).
function expandContainer(service, n, el, gen) {
  if (gen == null) gen = treeGen;
  const kindEl = el.querySelector(":scope > .node .kind");
  let kids = el.querySelector(":scope > .children");
  if (kids) {
    const hidden = kids.classList.toggle("hidden");
    if (kindEl) kindEl.textContent = hidden ? "▸" : "▾";
    return;
  }
  kids = document.createElement("div");
  kids.className = "children";
  kids.innerHTML = stateLoading(""); // loading state on expand (U-10) — cleared when the page lands
  el.appendChild(kids);
  if (kindEl) kindEl.textContent = "▾";
  appendTreePage(service, n.path.segments, kids, false, "", gen);
}

// KV_GLYPH maps a redis TYPE (NodeMeta.entryType) to a per-type tree glyph so a
// hash / list / set / zset / string are visually distinct in the tree instead of
// all collapsing to one kind-glyph (UI-AUD-04 / U-03). All entries are markup-safe
// (no <, >, &), so they inline into node HTML without escaping.
const KV_GLYPH = { string: "◇", hash: "#", list: "≡", set: "∈", zset: "⇅" };

// glyphFor picks a tree node's icon. A server-declared redis type wins (per-type
// glyph); otherwise the node KIND decides: container ▸, table ▦, blob ◇.
function glyphFor(n) {
  const et = n.meta && n.meta.entryType;
  if (et && KV_GLYPH[et]) return KV_GLYPH[et];
  if (n.kind === "container") return "▸";
  if (n.kind === "tabular") return "▦";
  return "◇";
}

function renderNode(service, n) {
  const el = document.createElement("div");
  el.className = "node-wrap";
  const row = document.createElement("div");
  row.className = "node";
  const et = n.meta && n.meta.entryType;
  const gtitle = et ? ` title="${esc(et)}"` : "";
  row.innerHTML = `<span class="kind"${gtitle}>${glyphFor(n)}</span><span class="nname">${esc(n.name || "(root)")}</span>`
    + metaChip(n.meta);
  el.appendChild(row);
  if (n.kind === "container") {
    row.onclick = () => expandContainer(service, n, el);
  } else if (n.kind === "tabular") {
    row.onclick = () => openTable(service, n);
  } else {
    row.onclick = () => openBlob(service, n);
    // Lazy thumbnail for a blob-like image leaf, size-capped.
    if (actionEnabled(service, ACTION.readBlob) && isImageName(n.name) && n.meta && n.meta.size && n.meta.size <= (2 << 20)) {
      lazyThumb(service, n, row);
    }
  }
  return el;
}

async function lazyThumb(service, n, row) {
  try {
    const r = await api("/api/blob?" + new URLSearchParams({ service, segs: JSON.stringify(n.path.segments) }));
    if (!isImage(r.headers.get("X-DataConsole-ContentType") || "")) return;
    const url = URL.createObjectURL(await r.blob());
    const img = document.createElement("img");
    img.className = "thumb";
    img.onload = img.onerror = () => setTimeout(() => URL.revokeObjectURL(url), 200);
    img.src = url;
    const kindEl = row.querySelector(".kind");
    if (kindEl) kindEl.replaceWith(img); else row.prepend(img);
  } catch (_) { /* thumbnail is best-effort */ }
}

// metaChip is the ONE meta-chip renderer. It reads only the typed NodeMeta the
// server vouches for (DD-4): object rows show size + modified, blobs carrying a
// contentType show it; the redis entryType rides the GLYPH, not the chip. A nil
// TTL is "no expiry" and produces NO chip — never the literal "ttl 0s" (KV-AUD-02;
// the S28 sentinel makes ttlSeconds absent for no-expiry).
function metaChip(meta) {
  if (!meta) return "";
  const bits = [];
  if (meta.size != null) bits.push(esc(human(meta.size)));
  if (meta.modified) bits.push(esc(fmtModified(meta.modified)));
  if (meta.contentType) bits.push(esc(meta.contentType));
  if (meta.ttlSeconds != null && meta.ttlSeconds > 0) bits.push("ttl " + esc(String(meta.ttlSeconds)) + "s");
  return bits.length ? `<span class="nmeta">${bits.join(" · ")}</span>` : "";
}

// fmtModified renders an RFC3339 timestamp as a compact "YYYY-MM-DD HH:MM";
// anything unexpected passes through verbatim.
function fmtModified(s) {
  const str = String(s);
  return str.length >= 16 && str[10] === "T" ? str.slice(0, 16).replace("T", " ") : str;
}

// ---------- blob preview/edit ----------
async function openBlob(service, n) {
  state.reopen = () => openBlob(service, n);
  const content = document.getElementById("content");
  content.innerHTML = stateLoading("Loading " + n.name);
  const q = new URLSearchParams({ service, segs: JSON.stringify(n.path.segments) });
  let r;
  try { r = await api("/api/blob?" + q.toString()); }
  catch (e) { content.innerHTML = gate(service, e); return; }
  const truncated = r.headers.get("X-DataConsole-Truncated") === "true";
  const isVector = r.headers.get("X-DataConsole-Vector") === "true";
  const isStreamMeta = r.headers.get("X-DataConsole-StreamMetadata") === "true";
  const ctype = r.headers.get("X-DataConsole-ContentType") || "";
  const trueSize = parseInt(r.headers.get("X-DataConsole-Size") || "", 10);
  const buf = await r.arrayBuffer();
  const size = buf.byteLength;
  const textual = isTextual(ctype);
  // Truncated reads carry the TRUE pre-slice size (KV-AUD-05): "showing X of Y".
  const sizeText = (truncated && Number.isFinite(trueSize) && trueSize > size)
    ? "showing " + human(size) + " of " + human(trueSize)
    : human(size);

  let html = `<div class="toolbar"><b>${esc(n.name)}</b>`
    + `<span class="meta">${esc(sizeText)}${truncated ? " · head slice (view-only)" : ""}${ctype ? " · " + esc(ctype) : ""}</span>`
    + `<span class="spacer"></span>`;
  // Affordances from edit-mode toggle AND service.actions + the blob's own state.
  // A vector-bearing point is never inline-editable (its raw floats are collapsed).
  const editable = editing() && actionEnabled(service, ACTION.writeBlob) && !truncated && textual && size <= EDIT_CAP && !isVector;
  if (editable) html += `<button id="saveblob">Save</button>`;
  if (editing() && actionEnabled(service, ACTION.renameObject)) html += actionButton("renameblob", "Rename", "ghost", service, ACTION.renameObject);
  if (editing() && actionEnabled(service, ACTION.deleteNode)) html += actionButton("delblob", "Delete", "danger", service, ACTION.deleteNode);
  html += `<button id="dlblob" class="ghost">Download</button></div>`;

  const image = isImage(ctype) && !truncated && size <= IMAGE_CAP;
  if (image) {
    // Render attacker-controlled bytes as an <img> via an object-URL — safe (an
    // image can't execute, and SVG-in-<img> has scripting disabled), and it never
    // navigates the console origin to the raw blob.
    const url = URL.createObjectURL(new Blob([buf], { type: ctype }));
    html += `<div class="imgwrap"><img class="imgpreview" alt="${esc(n.name)}"></div>`;
    content.innerHTML = html;
    const img = content.querySelector("img.imgpreview");
    img.onload = img.onerror = () => setTimeout(() => URL.revokeObjectURL(url), 200);
    img.src = url;
  } else if (isVector && textual && size <= DISPLAY_CAP) {
    // Qdrant point: collapse the raw embedding behind a toggle, show id/payload
    // as pretty JSON (UI-AUD-03) — never a wall of floats inline.
    content.innerHTML = html;
    renderVector(content, new TextDecoder().decode(buf));
  } else if (isStreamMeta && textual) {
    // Kafka/nats summary: a LABELLED metadata card, never an editable-looking JSON
    // <pre> indistinguishable from a document (U-04). Rendering PARSED values also
    // fixes the escaped-source artifact (UI-AUD-02: subjects like `events.>`).
    content.innerHTML = html;
    renderStreamCard(content, new TextDecoder().decode(buf));
  } else if (size > DISPLAY_CAP || !textual) {
    html += `<div class="placeholder">${!textual ? "Binary content" : "Large content"} — use Download.</div>`;
    content.innerHTML = html;
  } else if (editable) {
    html += `<textarea class="editor" id="blobedit"></textarea>`;
    content.innerHTML = html;
    document.getElementById("blobedit").value = new TextDecoder().decode(buf);
    // Carry the content-type we read back on Save so a text file stays text on the
    // next open (OBJ-AUD-01) — no silent degrade to application/octet-stream.
    document.getElementById("saveblob").onclick = () =>
      saveBlob(service, n, () => document.getElementById("blobedit").value, ctype);
  } else {
    html += `<pre class="blob"></pre>`;
    content.innerHTML = html;
    content.querySelector("pre.blob").textContent = new TextDecoder().decode(buf);
  }
  wireAction("delblob", service, ACTION.deleteNode, () => confirmAction("Delete " + n.name + "?", `DELETE ${n.name}`, () => deleteNode(service, n)));
  wireAction("renameblob", service, ACTION.renameObject, () => renameObject(service, n));
  wire("dlblob", () => downloadBlob(service, n));
  maybeTTL(service, n, () => openBlob(service, n));
}

// renderVector summarizes a qdrant point: the embedding's dimension count with the
// raw floats hidden behind a toggle, and the id/payload shown as pretty JSON.
// Falls back to plain JSON when the body is not the expected {..., vector:[...]}.
function renderVector(content, text) {
  let obj;
  try { obj = JSON.parse(text); } catch (_) { obj = null; }
  const vec = obj && Array.isArray(obj.vector) ? obj.vector : null;
  if (!vec) {
    const pre = document.createElement("pre");
    pre.className = "blob";
    pre.textContent = text;
    content.appendChild(pre);
    return;
  }
  const rest = {};
  for (const k of Object.keys(obj)) if (k !== "vector") rest[k] = obj[k];
  const box = document.createElement("div");
  box.className = "vectorbox";
  const doc = document.createElement("pre");
  doc.className = "blob";
  doc.textContent = Object.keys(rest).length ? JSON.stringify(rest, null, 2) : "(no payload)";
  const summary = document.createElement("div");
  summary.className = "vecsummary";
  summary.innerHTML = `<span class="badge view-only">vector · ${vec.length} dims</span> `;
  const toggle = document.createElement("button");
  toggle.className = "link";
  toggle.textContent = "Show raw vector ▾";
  const raw = document.createElement("pre");
  raw.className = "blob vecraw hidden";
  raw.textContent = "[" + vec.join(", ") + "]";
  toggle.onclick = () => {
    const hidden = raw.classList.toggle("hidden");
    toggle.textContent = hidden ? "Show raw vector ▾" : "Hide raw vector ▴";
  };
  summary.appendChild(toggle);
  box.appendChild(doc);
  box.appendChild(summary);
  box.appendChild(raw);
  content.appendChild(box);
}

// renderStreamCard renders a kafka/nats summary as an explicitly labelled metadata
// card — "not message content" — so it can never be mistaken for a document/object
// blob (U-04). It reads the PARSED summary (kafka {topic,partitions,partitionIds,
// consumerGroups}, nats {stream,subjects,messages,bytes,firstSeq,lastSeq,consumers}),
// so wildcard subjects like `events.>` render literally, not as escaped JSON source
// (UI-AUD-02). Consumer info absent-by-privilege surfaces as an explicit "unavailable".
function renderStreamCard(content, text) {
  let obj;
  try { obj = JSON.parse(text); } catch (_) { obj = null; }
  const box = document.createElement("div");
  box.className = "streamcard";
  if (!obj || typeof obj !== "object") {
    const pre = document.createElement("pre");
    pre.className = "blob";
    pre.textContent = text;
    box.appendChild(pre);
    content.appendChild(box);
    return;
  }
  const rows = [];
  const add = (label, value) => { if (value !== undefined && value !== null) rows.push([label, value]); };
  add("topic / stream", obj.topic != null ? obj.topic : obj.stream);
  if (Array.isArray(obj.subjects)) add("subjects", obj.subjects.join(", "));
  if (obj.partitions != null) add("partitions", String(obj.partitions));
  if (Array.isArray(obj.partitionIds) && obj.partitionIds.length) add("partition ids", obj.partitionIds.join(", "));
  if (obj.messages != null) add("messages", String(obj.messages));
  if (obj.bytes != null) add("bytes", human(obj.bytes));
  if (obj.firstSeq != null || obj.lastSeq != null) add("sequence", (obj.firstSeq != null ? obj.firstSeq : "?") + " → " + (obj.lastSeq != null ? obj.lastSeq : "?"));
  const cg = obj.consumerGroups;
  if (cg && typeof cg === "object") {
    add("consumer groups", cg.available === false
      ? "unavailable" + (cg.reason ? " (" + cg.reason + ")" : "")
      : String(cg.count != null ? cg.count : (Array.isArray(cg.groups) ? cg.groups.length : 0)) + (Array.isArray(cg.groups) && cg.groups.length ? " · " + cg.groups.join(", ") : ""));
  } else if (obj.consumers != null) {
    add("consumers", String(obj.consumers));
  }
  let dl = `<div class="streamlabel"><span class="badge view-only">stream metadata</span> not message content</div><dl class="streammeta">`;
  for (const [label, value] of rows) dl += `<dt>${esc(label)}</dt><dd>${esc(String(value))}</dd>`;
  dl += `</dl>`;
  box.innerHTML = dl;
  content.appendChild(box);
}

async function saveBlob(service, n, getVal, ctype) {
  const data = b64(new TextEncoder().encode(getVal()));
  confirmAction("Overwrite " + n.name + "?", `PUT ${n.name}`, async () => {
    const body = { path: n.path, data };
    if (ctype) body.contentType = ctype; // keep the type we read (OBJ-AUD-01)
    await api("/api/blob", {
      method: "PUT",
      headers: { "Content-Type": "application/json", "X-Confirm": "true" },
      body: JSON.stringify(body),
    });
    toast("Saved.");
  });
}

async function deleteNode(service, n) {
  await api("/api/node", {
    method: "DELETE",
    headers: { "Content-Type": "application/json", "X-Confirm": "true" },
    body: JSON.stringify({ path: n.path }),
  });
  toast("Deleted.");
  refreshTree(service);
}

function renameObject(service, n) {
  promptModal("Rename " + n.name, "New full key", n.name, (to) => {
    if (!to || to === n.name) return;
    const segs = n.path.segments.slice(0, -1).concat(to.split("/"));
    confirmAction(`Rename ${n.name} → ${to}?`, `COPY+DELETE`, async () => {
      await api("/api/rename", {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-Confirm": "true" },
        body: JSON.stringify({ from: n.path, to: { service, segments: segs } }),
      });
      toast("Renamed.");
      refreshTree(service);
    });
  });
}

async function downloadBlob(service, n) {
  // Embedded: <a download> is blocked in a webview — the host saves via a native
  // dialog (bytes never re-enter the webview). Standalone: object-URL download.
  if (state.embedded) {
    hostAction({ type: "dc-download", service: service, segs: n.path.segments, name: n.name || "object" });
    return;
  }
  try {
    const r = await api("/api/blob?" + new URLSearchParams({ service, segs: JSON.stringify(n.path.segments) }));
    const blob = await r.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url; a.download = n.name || "object";
    document.body.appendChild(a); a.click(); a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 4000);
  } catch (e) { toast("Download failed: " + errorSummary(e), true); }
}

// addUploadBar is only ever called once the caller has confirmed writeObject is
// enabled (a view-only service renders no upload bar at all — FIX 5) — so this
// always builds the live control, never a disabled placeholder.
function addUploadBar(service, segs, container) {
  const bar = document.createElement("div");
  bar.className = "uploadbar";
  if (state.embedded) {
    // A file <input> / FormData can't bridge a webview — the host picks the
    // file with a native dialog and uploads it (megabytes never postMessage'd).
    bar.innerHTML = `<button class="link" id="uploadbtn">⤒ Upload file</button>`;
    bar.querySelector("#uploadbtn").onclick = () => hostAction({ type: "dc-upload", service: service, segs: segs });
  } else {
    bar.innerHTML = `<label class="link">⤒ Upload file<input type="file" hidden></label>`;
    const input = bar.querySelector("input");
    input.onchange = () => { if (input.files[0]) uploadFile(service, segs, input.files[0]); };
  }
  container.appendChild(bar);
}

async function uploadFile(service, segs, file) {
  const fd = new FormData();
  fd.append("service", service);
  fd.append("segs", JSON.stringify(segs.concat(file.name)));
  fd.append("file", file);
  confirmAction(`Upload ${file.name}?`, `PUT ${file.name} (${human(file.size)})`, async () => {
    await api("/api/upload", { method: "POST", headers: { "X-Confirm": "true" }, body: fd });
    toast("Uploaded.");
    refreshTree(service);
  });
}

function refreshTree(service) {
  if (state.active === service) loadTree(service, [], document.getElementById("tree"), true);
}

// ---------- grid (tabular / KV-collection / query — ONE renderer) ----------
async function openTable(service, n) {
  state.reopen = () => openTable(service, n);
  const content = document.getElementById("content");
  content.innerHTML = stateLoading("Loading " + n.name);
  const q = new URLSearchParams({ service, segs: JSON.stringify(n.path.segments) });
  let tp;
  try { tp = await apiJSON("/api/table?" + q.toString()); }
  catch (e) { content.innerHTML = gate(service, e); return; }
  renderGrid(content, service, tp, {
    node: n,
    title: n.name,
    paginate: (cursor) => apiJSON("/api/table?" + new URLSearchParams({ service, segs: JSON.stringify(n.path.segments), cursor }).toString()),
  });
  maybeTTL(service, n, () => openTable(service, n));
}

// renderGrid is the ONE grid renderer (U-01): tabular tables, KV collections AND
// SQL query results all draw here. Editability is SERVER truth — a cell is
// interactive only when Column.editable AND the table exposes a row key AND the
// session may write; query columns arrive editable:false with rowKeyCols:null, so
// the same code draws them explicitly read-only, never an editable grid that
// silently ignores clicks. `opts`: {node, title, note, source, paginate}.
function renderGrid(content, service, tp, opts) {
  opts = opts || {};
  const node = opts.node || null; // null for a query result — no row-addressable identity
  const title = opts.title != null ? opts.title : (node ? node.name : "");
  const cols = tp.columns || [];
  const keyCols = tp.rowKeyCols || [];
  const rows = tp.rows || [];
  const usesKVEntry = hasAction(service, ACTION.editKVEntry);
  const editAction = usesKVEntry ? ACTION.editKVEntry : ACTION.editCell;
  const deleteAction = usesKVEntry ? ACTION.editKVEntry : ACTION.deleteRow;
  const noKey = keyCols.length === 0;
  const canWrite = !!(node && editing()); // query (no node) is never writable
  const editEnabled = canWrite && actionEnabled(service, editAction) && !noKey;
  const showDelete = canWrite && actionEnabled(service, deleteAction) && !noKey;
  const gctx = { service, node, editEnabled, showDelete, usesKVEntry };

  const capped = opts.source === "query" && !tp.nextCursor && rows.length >= QUERY_CAP;
  let h = `<div class="toolbar"><b>${esc(title)}</b>`
    + `<span class="meta">${rows.length} row${rows.length === 1 ? "" : "s"}${capped ? " · capped at " + QUERY_CAP : ""}</span>`;
  if (opts.note) {
    h += `<span class="meta note">${esc(opts.note)}</span>`;
  } else if (node && noKey) {
    // view-only-no-key (P.4): a table/collection with no safe row identity — a
    // distinct, visible label, not a shared silence (U-02, D-03).
    h += `<span class="badge view-only" title="No primary key — rows can't be safely edited or deleted.">view-only · no row key</span>`;
  }
  h += `<span class="spacer"></span>`;
  if (canWrite && actionEnabled(service, ACTION.insertRow) && !noKey) h += actionButton("insertrow", "Insert row", "ghost", service, ACTION.insertRow);
  h += `</div><div class="gridwrap"><table class="grid"><thead><tr>`;
  for (const c of cols) {
    const why = (editEnabled && c && !c.editable && c.reason) ? " · " + c.reason : "";
    h += `<th title="${esc((c.dataType || "") + why)}">${esc(c.name)}${c.pk ? " 🔑" : ""}</th>`;
  }
  if (showDelete) h += `<th class="delcol"></th>`;
  h += `</tr></thead><tbody class="gridbody"></tbody></table></div>`;
  content.innerHTML = h;
  const body = content.querySelector("tbody.gridbody");
  if (rows.length === 0) {
    // empty (P.4): a valid grid with zero rows — one honest empty-state, distinct
    // from loading and error.
    const tr = document.createElement("tr");
    const td = document.createElement("td");
    td.colSpan = (cols.length + (showDelete ? 1 : 0)) || 1;
    td.className = "state empty";
    td.textContent = "No rows";
    tr.appendChild(td);
    body.appendChild(tr);
  } else {
    appendGridRows(body, tp, cols, keyCols, gctx);
  }
  if (node) wireAction("insertrow", service, ACTION.insertRow, () => insertRow(service, node, cols));
  gridLoadMore(content, service, tp, cols, keyCols, gctx, opts.paginate);
}

function appendGridRows(body, tp, cols, keyCols, gctx) {
  const { service, node, editEnabled, showDelete, usesKVEntry } = gctx;
  for (const row of (tp.rows || [])) {
    const tr = document.createElement("tr");
    cols.forEach((c, i) => {
      const td = document.createElement("td");
      td.textContent = fmt(row[i]);
      // Editable iff the SERVER says so (Column.editable, PK/query/view-only tiers
      // already false); KV entry cells additionally consult entryEditPlan (a hash
      // field with a sibling, a redis list) for the payload-shape lock (D-02/D-03).
      let interactive = editEnabled && c && c.editable;
      if (interactive && usesKVEntry && entryEditPlan(cols, keyCols, row, i).kind === "locked") interactive = false;
      if (interactive) {
        td.className = "editable";
        td.title = "Click to edit";
        td.onclick = () => editCell(service, node, cols, keyCols, row, c, i, td, usesKVEntry);
      } else if (editEnabled && c && !c.editable) {
        // Explicit "why not" on a locked cell in write mode (U-06) — never a silent
        // no-op that looks identical to an editable one.
        td.className = "locked";
        td.title = c.reason || "Not editable";
      }
      tr.appendChild(td);
    });
    if (showDelete) {
      // showDelete already means "enabled" (FIX 5: a view-only service renders no
      // delete column at all, never a disabled one), so the button is always live.
      const td = document.createElement("td");
      td.className = "delcol";
      const del = document.createElement("button");
      del.className = "rowdel"; del.textContent = "✕"; del.title = "Delete row";
      del.onclick = () => deleteRow(service, node, cols, keyCols, row, usesKVEntry);
      td.appendChild(del); tr.appendChild(td);
    }
    body.appendChild(tr);
  }
}

// gridLoadMore is the grid's slice of the pagination canon (U-09): the SAME "Load
// more…" affordance as the tree, fed by opts.paginate(cursor) so tabular and query
// share one flow.
function gridLoadMore(content, service, tp, cols, keyCols, gctx, paginate) {
  const old = content.querySelector(".loadmore");
  if (old) old.remove();
  if (!tp.nextCursor || !paginate) return;
  const more = document.createElement("button");
  more.className = "loadmore";
  more.textContent = "Load more…";
  more.onclick = async () => {
    more.disabled = true; more.textContent = "Loading…";
    try {
      const next = await paginate(tp.nextCursor);
      appendGridRows(content.querySelector("tbody.gridbody"), next, cols, keyCols, gctx);
      gridLoadMore(content, service, next, cols, keyCols, gctx, paginate);
    } catch (e) { more.disabled = false; more.textContent = "Load more…"; toast(e, true); }
  };
  content.querySelector(".gridwrap").after(more);
}

function editCell(service, n, cols, keyCols, row, col, idx, td, kv) {
  const oldVal = row[idx];
  const input = document.createElement("input");
  input.className = "celledit"; input.value = oldVal == null ? "" : String(oldVal);
  td.textContent = ""; td.appendChild(input); input.focus();
  const commit = async () => {
    const nv = input.value;
    if (nv === String(oldVal == null ? "" : oldVal)) { td.textContent = fmt(oldVal); return; }
    let doReq;
    if (kv) {
      const plan = entryEditPlan(cols, keyCols, row, idx, nv);
      if (plan.kind !== "edit") {
        if (plan.kind === "invalid") {
          // Inline error only: no request is sent and the row/UI is not touched,
          // so the user can correct the value in place.
          input.classList.add("invalid");
          input.title = plan.reason;
          input.focus();
          if (typeof input.select === "function") input.select();
        } else {
          td.textContent = fmt(oldVal); // defensive: a locked cell never wires onclick
        }
        return;
      }
      const body = { path: n.path, field: plan.payload.field };
      if ("score" in plan.payload) body.score = plan.payload.score;
      else body.value = b64(new TextEncoder().encode(plan.payload.value));
      doReq = () => api("/api/entry", { method: "PUT", headers: jsonConfirm(), body: JSON.stringify(body) });
    } else {
      doReq = () => api("/api/cell", { method: "POST", headers: jsonConfirm(),
        body: JSON.stringify({ path: n.path, rowKey: rowKeyOf(cols, keyCols, row), column: col.name, newValue: nv, expectedOld: oldVal }) });
    }
    try {
      await doReq();
      row[idx] = nv; td.textContent = fmt(nv); toast("Saved."); // 200 ⇒ applied (sync families)
    } catch (e) {
      if (e && e.code === "timeout") {
        // accepted, not confirmed (U-14): keep the optimistic value, say so honestly.
        row[idx] = nv; td.textContent = fmt(nv); toast("Accepted — still applying.", "warn");
      } else {
        td.textContent = fmt(oldVal); toast("Save failed: " + errorSummary(e), true);
      }
    }
  };
  input.onblur = commit;
  input.onkeydown = (ev) => {
    if (ev.key === "Enter") input.blur();
    if (ev.key === "Escape") {
      // Unbind BEFORE restoring: clearing td.textContent removes the still-focused
      // input from the DOM, which fires a native blur -- if onblur were still
      // commit, Escape would silently save the typed value instead of cancelling.
      input.onblur = null;
      td.textContent = fmt(oldVal);
    }
  };
}

function deleteRow(service, n, cols, keyCols, row, kv) {
  confirmAction("Delete this row?", "DELETE row", async () => {
    if (kv) {
      await api("/api/entry", { method: "DELETE", headers: jsonConfirm(),
        body: JSON.stringify({ path: n.path, field: String(row[0]) }) });
    } else {
      await api("/api/row", { method: "DELETE", headers: jsonConfirm(),
        body: JSON.stringify({ path: n.path, key: rowKeyOf(cols, keyCols, row) }) });
    }
    toast("Deleted."); openTable(service, n); // re-read to confirm gone (I-1)
  });
}

function insertRow(service, n, cols) {
  const fields = cols.map((c) =>
    `<label>${esc(c.name)}<input data-col="${esc(c.name)}" placeholder="${esc(c.dataType || "")}"></label>`).join("");
  showModal("Insert row into " + n.name, `<div class="insertform">${fields}</div>`, async () => {
    const row = {};
    document.querySelectorAll("#modalbody input[data-col]").forEach((el) => {
      if (el.value !== "") row[el.getAttribute("data-col")] = el.value;
    });
    // Applied.key echoes the new PK (T-AUD-03): show it, and re-read the table so
    // the just-inserted row is visible + addressable (no "can't find the row I made").
    const applied = await apiJSON("/api/row", { method: "POST", headers: jsonConfirm(), body: JSON.stringify({ path: n.path, row }) });
    const keyStr = applied && applied.key
      ? Object.keys(applied.key).map((k) => k + "=" + fmt(applied.key[k])).join(", ")
      : "";
    toast(keyStr ? "Inserted (" + keyStr + ")." : "Inserted.");
    openTable(service, n);
  });
}

// ---------- SQL query console ----------
function openQuery(service) {
  state.reopen = () => openQuery(service);
  const content = document.getElementById("content");
  content.innerHTML = `<div class="toolbar"><b>Query — ${esc(service)}</b>`
    + `<span class="meta">read-only (engine-enforced)</span><span class="spacer"></span>`
    + `<button id="runq">Run</button></div>`
    + `<textarea class="editor query" id="qtext" placeholder="SELECT * FROM ... LIMIT 100"></textarea>`
    + `<div id="qresult"></div>`;
  document.getElementById("runq").onclick = () => runQuery(service);
}

async function runQuery(service) {
  const stmt = document.getElementById("qtext").value.trim();
  if (!stmt) return;
  const res = document.getElementById("qresult");
  res.innerHTML = stateLoading("Running");
  try {
    const tp = await apiJSON("/api/query", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ service, stmt }) });
    // The SAME grid renderer as ReadTable (U-01). Query columns come back
    // editable:false + rowKeyCols:null, so it renders explicitly read-only.
    renderGrid(res, service, tp, {
      title: "",
      source: "query",
      note: "read-only (query result)",
      paginate: (cursor) => apiJSON("/api/query", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ service, stmt, page: { cursor } }) }),
    });
  } catch (e) { res.innerHTML = errorHTML(e); }
}

// ---------- document search (wires /api/search) ----------
// openSearch mirrors the query console: pick an index, enter free text, get
// matching document ids as clickable blob nodes (opened via the normal blob view).
// Search is a read-only GET — no write token, results are ids only (never engine
// highlight HTML), so an untrusted match id is escaped like any other node name.
async function openSearch(service) {
  state.reopen = () => openSearch(service);
  const content = document.getElementById("content");
  content.innerHTML = stateLoading("Loading indices");
  let indices = [];
  try {
    const data = await apiJSON("/api/tree?" + new URLSearchParams({ service, segs: "[]" }));
    indices = (data.nodes || []).filter((n) => n.kind === "container").map((n) => n.name);
  } catch (e) { content.innerHTML = gate(service, e); return; }
  const opts = indices.map((i) => `<option value="${esc(i)}">${esc(i)}</option>`).join("");
  const canCreateDoc = editing() && actionEnabled(service, ACTION.createDoc);
  content.innerHTML = `<div class="toolbar"><b>Search — ${esc(service)}</b>`
    + `<span class="spacer"></span>`
    + (canCreateDoc ? `<button id="adddoc" class="ghost">Add document</button>` : "")
    + `</div>`
    + `<div class="searchbar"><select id="sidx">${opts}</select>`
    + `<input id="sq" class="editor" placeholder="search text…" autocomplete="off">`
    + `<button id="runs">Search</button></div>`
    + `<div id="sresult"></div>`;
  const run = () => runSearch(service);
  document.getElementById("runs").onclick = run;
  document.getElementById("sq").onkeydown = (e) => { if (e.key === "Enter") run(); };
  if (canCreateDoc) document.getElementById("adddoc").onclick = () => createDocForm(service, document.getElementById("sidx").value);
}

async function runSearch(service) {
  const index = document.getElementById("sidx").value;
  const q = document.getElementById("sq").value.trim();
  const res = document.getElementById("sresult");
  if (!index || !q) { res.innerHTML = stateEmpty("Enter search text"); return; }
  res.innerHTML = stateLoading("Searching");
  try {
    const data = await apiJSON("/api/search?" + new URLSearchParams({ service, segs: JSON.stringify([index]), q }));
    const nodes = data.nodes || [];
    if (!nodes.length) { res.innerHTML = stateEmpty("No matches"); return; }
    res.innerHTML = "";
    for (const n of nodes) res.appendChild(renderNode(service, n));
  } catch (e) { res.innerHTML = errorHTML(e); }
}

// createDocForm creates a document into an index. Id optional (engine-assigned when
// blank); on success it opens the new doc. A meili create is task-confirmed
// server-side, so a `timeout` reads "accepted, still applying" (U-14), not success.
function createDocForm(service, index) {
  showModal("Add document to " + index,
    `<label class="modalprompt">Document id (optional)<input id="docid" autocomplete="off"></label>`
    + `<label class="modalprompt">JSON body<textarea id="docbody" class="editor" placeholder='{"title":"…"}'></textarea></label>`,
    async () => {
      const id = document.getElementById("docid").value.trim();
      const bodyText = document.getElementById("docbody").value;
      let parsed;
      try { parsed = JSON.parse(bodyText); } catch (_) { toast("Body is not valid JSON.", true); return; }
      const segs = id ? [index, id] : [index];
      const applied = await apiJSON("/api/document/create", {
        method: "POST", headers: jsonConfirm(),
        body: JSON.stringify({ path: { service, segments: segs }, data: b64(new TextEncoder().encode(JSON.stringify(parsed))) }),
      });
      const newId = applied && applied.id ? applied.id : id;
      toast("Document created.");
      if (newId) openBlob(service, { name: newId, kind: "blob", path: { service, segments: [index, newId] } });
    });
}

// createKeyForm creates a new KV collection or string key. A name collision is
// REFUSED server-side (never a silent clobber — KV-AUD-01/03), surfaced honestly.
// The extra fields are TYPE-DEPENDENT: kv.go's CreateKey requires a distinct
// shape per redis type (hash needs Field; zset needs Field(the member) + Score)
// — sending bare name/type/value for those two 400s deterministically, so
// switching #kvtype re-renders #kvextra to match what the server requires.
// Field/value names in the request body match provider.KVCreate's JSON tags
// (provider/types.go) exactly.
function kvExtraFieldsHTML(type) {
  if (type === "hash") {
    return `<label class="modalprompt">Field (required)<input id="kvfield" autocomplete="off"></label>`
      + `<label class="modalprompt">Value (optional)<input id="kvval" autocomplete="off"></label>`;
  }
  if (type === "zset") {
    return `<label class="modalprompt">Member (required)<input id="kvfield" autocomplete="off"></label>`
      + `<label class="modalprompt">Score (required, numeric)<input id="kvscore" type="number" step="any" autocomplete="off"></label>`;
  }
  return `<label class="modalprompt">First value (optional)<input id="kvval" autocomplete="off"></label>`;
}
function createKeyForm(service) {
  showModal("Add key",
    `<label class="modalprompt">Key name<input id="kvname" autocomplete="off"></label>`
    + `<label class="modalprompt">Type<select id="kvtype"><option>string</option><option>hash</option><option>list</option><option>set</option><option>zset</option></select></label>`
    + `<div id="kvextra">${kvExtraFieldsHTML("string")}</div>`,
    async () => {
      const name = document.getElementById("kvname").value.trim();
      if (!name) { toast("Key name required.", true); return; }
      const type = document.getElementById("kvtype").value;
      const body = { path: { service, segments: [name] }, type: type };
      if (type === "hash") {
        const field = document.getElementById("kvfield").value;
        if (!field) { toast("Field name required for a hash.", true); return; }
        body.field = field;
        const val = document.getElementById("kvval").value;
        if (val !== "") body.value = b64(new TextEncoder().encode(val));
      } else if (type === "zset") {
        const member = document.getElementById("kvfield").value;
        if (!member) { toast("Member required for a zset.", true); return; }
        const score = parseFloat(document.getElementById("kvscore").value);
        if (isNaN(score)) { toast("Numeric score required for a zset.", true); return; }
        body.field = member;
        body.score = score;
      } else {
        const val = document.getElementById("kvval").value;
        if (val !== "") body.value = b64(new TextEncoder().encode(val));
      }
      await apiJSON("/api/kv/create", { method: "POST", headers: jsonConfirm(), body: JSON.stringify(body) });
      toast("Key created.");
      refreshTree(service);
    });
  document.getElementById("kvtype").onchange = (e) => {
    document.getElementById("kvextra").innerHTML = kvExtraFieldsHTML(e.target.value);
  };
}

// ---------- KV TTL control (wires /api/stat) ----------
async function maybeTTL(service, n, reopen) {
  if (!hasAction(service, ACTION.setTTL)) return;
  let node;
  try { node = await apiJSON("/api/stat?" + new URLSearchParams({ service, segs: JSON.stringify(n.path.segments) })); }
  catch (_) { return; }
  const ttl = node.meta ? node.meta.ttlSeconds : null;
  // nil/negative ⇒ "no expiry", NEVER "0s" (KV-AUD-02; the S28 sentinel is nil).
  const cur = (ttl == null || ttl < 0) ? "no expiry" : ttl + "s";
  const bar = document.createElement("div");
  bar.className = "ttlbar";
  bar.innerHTML = `<span class="meta">TTL: ${esc(cur)}</span>`;
  if (editing() && actionEnabled(service, ACTION.setTTL)) {
    bar.innerHTML += " "
      + actionButton("setttl", "Set TTL", "ghost", service, ACTION.setTTL)
      + " "
      + actionButton("clrttl", "Persist", "ghost", service, ACTION.setTTL);
  }
  document.getElementById("content").appendChild(bar);
  wireAction("setttl", service, ACTION.setTTL, () => {
    promptModal(`Set TTL on ${n.name}`, "TTL seconds", ttl > 0 ? String(ttl) : "3600", (v) => {
      const secs = parseInt(v, 10);
      if (isNaN(secs)) return;
      confirmAction(`Set TTL ${secs}s on ${n.name}?`, `EXPIRE ${secs}s`, async () => {
        await api("/api/ttl", { method: "PUT", headers: jsonConfirm(), body: JSON.stringify({ path: n.path, ttlSeconds: secs }) });
        toast("TTL set."); reopen();
      });
    });
  });
  wireAction("clrttl", service, ACTION.setTTL, () => confirmAction(`Clear TTL on ${n.name}?`, "PERSIST", async () => {
    await api("/api/ttl", { method: "PUT", headers: jsonConfirm(), body: JSON.stringify({ path: n.path, ttlSeconds: null }) });
    toast("TTL cleared."); reopen();
  }));
}

// ---------- confirm modal ----------
let modalOK = null;
function showModal(title, bodyHTML, onOK) {
  document.getElementById("modaltitle").textContent = title;
  document.getElementById("modalbody").innerHTML = bodyHTML;
  document.getElementById("modal").classList.remove("hidden");
  modalOK = onOK;
}
function hideModal() { document.getElementById("modal").classList.add("hidden"); modalOK = null; }
function confirmAction(title, actionText, run) {
  showModal(title, `<div class="action">${esc(actionText)}</div>`, run);
}
// promptModal asks for one value through the modal — window.prompt is a no-op in
// a VS Code webview. onValue runs with the entered string when the user confirms.
function promptModal(title, label, defaultValue, onValue) {
  const dv = defaultValue == null ? "" : String(defaultValue);
  showModal(title, `<label class="modalprompt">${esc(label)}<input id="modalinput" value="${esc(dv)}"></label>`, async () => {
    const el = document.getElementById("modalinput");
    await onValue(el ? el.value : "");
  });
  const el = document.getElementById("modalinput");
  if (el) {
    el.focus();
    if (typeof el.select === "function") el.select();
    el.onkeydown = (e) => { if (e.key === "Enter") { e.preventDefault(); const ok = document.getElementById("modalok"); if (ok) ok.click(); } };
  }
}

// ---------- helpers ----------
function jsonConfirm() { return { "Content-Type": "application/json", "X-Confirm": "true" }; }
function gate(service, e) {
  const vpn = DCErrors.vpnGateDecision({
    service,
    project: state.project,
    action: actionOf(service, ACTION.showVPNGate),
    error: e,
  });
  if (vpn.show) {
    return `<div class="vpngate">⚠ <b>${esc(service)}</b> looks unreachable.<br>`
      + `${esc(vpn.reason)}<br><br>`
      + `<code>${esc(vpn.command)}</code><br><br>`
      + `<span class="muted">${esc(vpn.summary)}</span></div>`;
  }
  return errorHTML(e);
}
function renderError(e) { document.getElementById("content").innerHTML = errorHTML(e); }
function wire(id, fn) { const el = document.getElementById(id); if (el) el.onclick = fn; }

// toast surfaces a transient message. kind: falsy/"good" = success; true/"bad" =
// failure; "warn" = accepted-not-confirmed (the `timeout` sentinel, U-14).
function toast(m, kind) {
  const bad = kind === true || kind === "bad";
  const warn = kind === "warn";
  const t = document.createElement("div");
  t.textContent = (bad || warn) ? errorSummary(m) : m;
  t.className = "toast " + (bad ? "bad" : warn ? "warn" : "good");
  document.body.appendChild(t);
  setTimeout(() => t.remove(), 2600);
}

// toastError renders a caught MUTATION error honestly (U-14): the `timeout`
// sentinel is accepted-not-confirmed — a warn, not a hard failure — while every
// other sentinel (conflict / wrong_type / not_found / read_only / …) is a failure.
function toastError(e) {
  if (e && e.code === "timeout") { toast("Accepted — still applying.", "warn"); return; }
  toast(e, true);
}

// ---------- wiring ----------
document.getElementById("refresh").onclick = async () => {
  try {
    const d = await apiJSON("/api/refresh", { method: "POST" });
    state.services = d.services || [];
    renderServices();
    if (state.active) { const s = svcOf(state.active); if (s) selectService(s); }
    else openPendingService(); // a deep link that wasn't discovered before may exist now
  } catch (e) { toast(e, true); }
};
document.getElementById("tokenbtn").onclick = () => {
  const v = document.getElementById("tokeninput").value.trim();
  if (v) { state.token = v; start(); }
};
document.getElementById("editchk").onchange = (e) => onEditToggle(e.target.checked);
document.getElementById("modalcancel").onclick = hideModal;
document.getElementById("modalok").onclick = async () => {
  const run = modalOK; hideModal();
  if (run) { try { await run(); } catch (e) { toastError(e); } }
};
bootAuth();
