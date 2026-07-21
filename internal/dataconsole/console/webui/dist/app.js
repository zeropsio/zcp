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
const RELATION_PAGE_SIZES = [25, 50, 100, 250, 500, 1000];
const EXPLORER_MIN = 180;
const DATA_PANE_MIN = 320;
const PANE_DIVIDER_SIZE = 8;
const PANE_KEYBOARD_STEP = 16;
const GRID_COLUMN_MIN = 96;
const GRID_COLUMN_MAX = 640;
const GRID_COLUMN_DEFAULT = 160;
const GRID_COLUMN_KEYBOARD_STEP = 16;
const GRID_ACTION_COLUMN_WIDTH = 44;
const LAYOUT_STORAGE_KEY = "zcp.dataconsole.layout.v1";

// ---------- transport ----------
// One chokepoint, two transports. STANDALONE (own tab): fetch with the fragment
// bearer. EMBEDDED (Studio WebviewPanel): a postMessage RPC to the extension
// host, which holds the bearer and proxies to the loopback console — the bearer
// never enters the webview and the webview CSP has no connect-src (no fetch).
const vscodeApi = (typeof acquireVsCodeApi === "function") ? acquireVsCodeApi() : null;
const rpcPending = {};
let rpcSeq = 0;
const downloadPending = {};
let downloadSeq = 0;

function readLayoutContainer() {
  try {
    if (vscodeApi && typeof vscodeApi.getState === "function") return vscodeApi.getState() || {};
    return JSON.parse(localStorage.getItem(LAYOUT_STORAGE_KEY) || "{}");
  } catch (_) {
    return {};
  }
}

const savedLayoutContainer = readLayoutContainer();
const layoutPrefs = savedLayoutContainer.dataConsoleLayout && typeof savedLayoutContainer.dataConsoleLayout === "object"
  ? savedLayoutContainer.dataConsoleLayout
  : {};
if (!layoutPrefs.columnWidths || typeof layoutPrefs.columnWidths !== "object") layoutPrefs.columnWidths = {};

function persistLayoutPrefs() {
  const container = Object.assign({}, readLayoutContainer(), { dataConsoleLayout: layoutPrefs });
  try {
    if (vscodeApi && typeof vscodeApi.setState === "function") vscodeApi.setState(container);
    else localStorage.setItem(LAYOUT_STORAGE_KEY, JSON.stringify(container));
  } catch (_) {
    // Persistence is an enhancement; a denied storage surface must not break browsing.
  }
}

function measuredMainWidth(main) {
  const rect = main.getBoundingClientRect();
  if (rect && rect.width > 0) return rect.width;
  const rail = document.getElementById("rail");
  const railWidth = rail && !rail.classList.contains("hidden") ? 230 : 0;
  return Math.max(0, window.innerWidth - railWidth);
}

function explorerBounds(main) {
  const max = Math.max(EXPLORER_MIN, Math.floor(measuredMainWidth(main) - DATA_PANE_MIN - PANE_DIVIDER_SIZE));
  return { min: EXPLORER_MIN, max };
}

let refreshSplitPaneLayout = () => {};

function initSplitPane() {
  const main = document.getElementById("main");
  const tree = document.getElementById("tree");
  const divider = document.getElementById("tree-divider");
  if (!main || !tree || !divider) return;

  let width = Number(layoutPrefs.explorerWidth);
  if (!Number.isFinite(width)) width = 320;
  let pointerStart = null;

  const apply = (next, save) => {
    const bounds = explorerBounds(main);
    width = Math.round(Math.max(bounds.min, Math.min(bounds.max, next)));
    tree.style.width = width + "px";
    divider.setAttribute("aria-valuemin", String(bounds.min));
    divider.setAttribute("aria-valuemax", String(bounds.max));
    divider.setAttribute("aria-valuenow", String(width));
    if (save) {
      layoutPrefs.explorerWidth = width;
      persistLayoutPrefs();
    }
  };

  divider.addEventListener("pointerdown", (e) => {
    if (e.button != null && e.button !== 0) return;
    e.preventDefault();
    e.stopPropagation();
    pointerStart = { x: e.clientX, width };
    divider.classList.add("resizing");
  });
  window.addEventListener("pointermove", (e) => {
    if (!pointerStart) return;
    e.preventDefault();
    apply(pointerStart.width + e.clientX - pointerStart.x, false);
  });
  const finishPointerResize = (e) => {
    if (!pointerStart) return;
    e.preventDefault();
    pointerStart = null;
    divider.classList.remove("resizing");
    apply(width, true);
  };
  window.addEventListener("pointerup", finishPointerResize);
  window.addEventListener("pointercancel", finishPointerResize);
  divider.addEventListener("keydown", (e) => {
    let next = null;
    const bounds = explorerBounds(main);
    if (e.key === "ArrowLeft") next = width - PANE_KEYBOARD_STEP;
    else if (e.key === "ArrowRight") next = width + PANE_KEYBOARD_STEP;
    else if (e.key === "Home") next = bounds.min;
    else if (e.key === "End") next = bounds.max;
    if (next == null) return;
    e.preventDefault();
    e.stopPropagation();
    apply(next, true);
  });
  window.addEventListener("resize", () => apply(width, true));
  refreshSplitPaneLayout = (restore) => {
    const restored = restore ? Number(layoutPrefs.explorerWidth) : NaN;
    apply(Number.isFinite(restored) ? restored : width, false);
  };
  apply(width, false);
}

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
  // The host-message channel is only meaningful inside a VS Code webview: a
  // standalone browser tab has no vscodeApi to receive messages FROM (only a
  // real embed host acquires one), so any window holding a handle to this tab
  // (e.g. window.open/opener) could otherwise forge 'dataconsole-init' and
  // hijack the session. Gate registration on vscodeApi, which is acquired at
  // module load — before bootAuth ever runs — so this gate is race-free.
  if (vscodeApi) window.addEventListener("message", onHostMessage);
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
  if (d.type === "dataconsole-download-result") {
    const id = String(d.id || "");
    const pending = downloadPending[id];
    if (!pending) return;
    delete downloadPending[id];
    // A completed browser transfer belongs to the view that initiated it.
    // Navigation must not surface a late success/failure over newer content.
    if (pending.gen !== contentGen) return;
    if (d.ok) toast("Downloaded.");
    else toast(String(d.message || "Download failed."), true);
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
  // The rail changes #main's available width. Re-clamp the persisted split
  // against the post-chrome geometry rather than the pre-init placeholder.
  refreshSplitPaneLayout(true);
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
  // P2: the full word "view-only" (not the abbreviated "view") — matches the
  // Studio sidebar card's own wording (extension/templates/vscode-studio/
  // cards/managed.js) so the two surfaces read as one vocabulary.
  const label = sup === "supported" ? "ready" : sup === "view-only" ? "view-only" : "not yet";
  return `<span class="badge ${cls}">${label}</span>`;
}

function svcOf(hostname) { return state.services.find((x) => x.hostname === hostname); }
function actionOf(hostname, id) {
  return DCActions.actionOf(svcOf(hostname), id);
}
function hasAction(hostname, id) { return DCActions.hasAction(svcOf(hostname), id); }
function actionEnabled(hostname, id) { return DCActions.actionEnabled(svcOf(hostname), id); }
// actionButton renders a mutating control's <button> markup. Every caller
// pre-gates on actionEnabled() before calling this (a view-only or disabled
// action renders no control at all — FIX 5/7), so the action is always both
// present and enabled by the time this runs; it never needs to render disabled.
function actionButton(id, label, cls) {
  const klass = cls ? ` class="${esc(cls)}"` : "";
  return `<button id="${esc(id)}"${klass}>${esc(label)}</button>`;
}
function wireAction(id, service, actionID, fn) {
  if (actionEnabled(service, actionID)) wire(id, fn);
}

function selectService(s) {
  state.active = s.hostname;
  state.reopen = null;
  ++contentGen; // mint a new content generation — invalidates any prior in-flight content render
  // Keep the active hostname visible in the topbar — when the rail is hidden
  // (embedded under Studio) it is the only on-screen orientation cue.
  document.getElementById("activesvc").textContent = s.hostname ? "/ " + s.hostname : "";
  // P2: state the active service's view-only posture IN-PANE, not only via
  // the rail pill (which can be scrolled out of view, or hidden entirely
  // when embedded with a deep-linked service — applyChrome()).
  document.getElementById("activesvcbadge").classList.toggle("hidden", s.support !== "view-only");
  renderServices();
  const content = document.getElementById("content");
  setTabularContent(content, false);
  if (s.support === "not yet") {
    content.innerHTML = `<div class="placeholder">${esc(s.hostname)} (${esc(baseType(s.type))}) is discovered but not yet browsable.</div>`;
    document.getElementById("tree").innerHTML = "";
    return;
  }
  let hint = `Browse <b>${esc(s.hostname)}</b> in the tree.`;
  if (actionEnabled(s.hostname, ACTION.querySQL)) hint += ` <button class="link" id="querylink">Run a query ▸</button>`;
  // qdrant advertises searchDocs (a document-family READ action, gated only on
  // support tier — actions.go's familyReadActionIDs/readAction) but its engine
  // implements no free-text searcher: document.go's `searcher` interface's type
  // assertion fails for it (no `search` method), so Provider.Search always
  // returns ErrUnsupported (422) for qdrant. The server does not scope
  // searchDocs to actual capability, so gate here instead of advertising a
  // link that always fails for this one engine (B9; no Go change).
  if (actionEnabled(s.hostname, ACTION.searchDocs) && baseType(s.type) !== "qdrant") {
    hint += ` <button class="link" id="searchlink">Search ▸</button>`;
  }
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
    // This early return would otherwise skip the root-upload-bar branch below
    // entirely (C7): a bucket whose root is a single folder got the
    // auto-expanded folder's own nested bar (B8) but no root-level one. The
    // root bar targets segs (the ROOT's own path, independent of the
    // auto-expanded child), so it belongs here regardless of the auto-expand.
    if (root && editing() && actionEnabled(service, ACTION.uploadObject)) addUploadBar(service, segs, container);
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
  appendTreePage(service, n.path.segments, kids, false, "", gen).then(() => {
    // A nested container gets its OWN upload bar (B8) — appendTreePage's own
    // upload-bar branch is root-only, so without this a folder had no upload
    // path at all. Appended AFTER the page loads (never before: appendTreePage
    // itself appends nodes as they arrive, so adding the bar first would leave
    // it above the listed children instead of after them). Runs at most once:
    // re-expanding an already-loaded container takes the toggle-hidden branch
    // above and never reaches here again.
    if (gen !== treeGen) return; // a newer tree load superseded this one
    if (editing() && actionEnabled(service, ACTION.uploadObject)) addUploadBar(service, n.path.segments, kids);
  });
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
    img.onload = () => setTimeout(() => URL.revokeObjectURL(url), 200);
    // A tiny or corrupt image can fail to decode even though the server
    // declared an image/* content-type (P6) — fall back to the standard type
    // glyph so the tree never shows an invisible chip in its place.
    img.onerror = () => {
      setTimeout(() => URL.revokeObjectURL(url), 200);
      const fallback = document.createElement("span");
      fallback.className = "kind";
      fallback.textContent = glyphFor(n);
      img.replaceWith(fallback);
    };
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

// contentGen mirrors treeGen (KI-2, tree-race.dom.test.js) for #content (B4):
// state.reopen() (re-fired by applyWriteMode) re-fetches the previously-open
// view and, without a guard, would unconditionally overwrite #content when it
// resolved -- if the user had since navigated elsewhere, their new view was
// clobbered and stayed clobbered. Every content-level render entry
// (openBlob, openTable, openQuery, openSearch, selectService's placeholder)
// mints a new generation; every continuation that would touch #content after
// an await -- including maybeTTL's own async TTL-bar append, a second site
// independent of the main render -- checks it first and drops a superseded
// result instead of rendering it.
let contentGen = 0;

function setTabularContent(content, tabular) {
  if (content && content.id === "content") content.classList.toggle("tabular-content", !!tabular);
}

// ---------- blob preview/edit ----------
async function openBlob(service, n) {
  state.reopen = () => openBlob(service, n);
  const gen = ++contentGen;
  const content = document.getElementById("content");
  setTabularContent(content, false);
  content.innerHTML = stateLoading("Loading " + n.name);
  const q = new URLSearchParams({ service, segs: JSON.stringify(n.path.segments) });
  let r;
  try { r = await api("/api/blob?" + q.toString()); }
  catch (e) { if (gen !== contentGen) return; content.innerHTML = gate(service, e); return; }
  if (gen !== contentGen) return; // a newer content render superseded this one — drop the stale render
  const truncated = r.headers.get("X-DataConsole-Truncated") === "true";
  const isVector = r.headers.get("X-DataConsole-Vector") === "true";
  const isStreamMeta = r.headers.get("X-DataConsole-StreamMetadata") === "true";
  const ctype = r.headers.get("X-DataConsole-ContentType") || "";
  const trueSize = parseInt(r.headers.get("X-DataConsole-Size") || "", 10);
  const buf = await r.arrayBuffer();
  if (gen !== contentGen) return; // a newer content render superseded this one — drop the stale render
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
  if (editing() && actionEnabled(service, ACTION.renameObject)) html += actionButton("renameblob", "Rename", "ghost");
  if (editing() && actionEnabled(service, ACTION.deleteNode)) html += actionButton("delblob", "Delete", "danger");
  html += `<button id="dlblob" class="ghost">Download</button></div>`;

  const image = isImage(ctype) && !truncated && size <= IMAGE_CAP;
  if (image) {
    // Render attacker-controlled bytes as an <img> via an object-URL — safe (an
    // image can't execute, and SVG-in-<img> has scripting disabled), and it never
    // navigates the console origin to the raw blob.
    const url = URL.createObjectURL(new Blob([buf], { type: ctype }));
    html += `<div class="imgwrap"><img class="imgpreview" alt="${esc(n.name)}"><div class="imgdim muted"></div></div>`;
    content.innerHTML = html;
    const img = content.querySelector("img.imgpreview");
    const dim = content.querySelector(".imgdim");
    // P6: state the image's actual pixel dimensions once it loads — "how big
    // is this" no longer requires Download-and-inspect.
    img.onload = () => {
      dim.textContent = img.naturalWidth + " × " + img.naturalHeight + " px";
      setTimeout(() => URL.revokeObjectURL(url), 200);
    };
    img.onerror = () => setTimeout(() => URL.revokeObjectURL(url), 200);
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
    document.getElementById("blobedit").value = prettyJSONText(new TextDecoder().decode(buf), ctype);
    // Carry the content-type we read back on Save so a text file stays text on the
    // next open (OBJ-AUD-01) — no silent degrade to application/octet-stream.
    document.getElementById("saveblob").onclick = () =>
      saveBlob(service, n, () => document.getElementById("blobedit").value, ctype);
  } else {
    html += `<pre class="blob"></pre>`;
    content.innerHTML = html;
    content.querySelector("pre.blob").textContent = prettyJSONText(new TextDecoder().decode(buf), ctype);
  }
  wireAction("delblob", service, ACTION.deleteNode, () => confirmAction("Delete " + n.name + "?", `DELETE ${n.name}`, () => deleteNode(service, n), "danger"));
  wireAction("renameblob", service, ACTION.renameObject, () => renameObject(service, n));
  wire("dlblob", () => downloadBlob(service, n));
  maybeTTL(service, n, () => openBlob(service, n), gen);
}

// sortKeysDeep recursively sorts object keys for STABLE, engine-independent
// JSON rendering (P5): the same document's keys arrive in insertion order
// from elasticsearch but alphabetical from typesense — key order is
// non-semantic in JSON, so re-sorting it is safe and makes the SAME document
// render identically regardless of which engine served it. Array ELEMENT
// order is semantic and is never touched.
function sortKeysDeep(v) {
  if (Array.isArray(v)) return v.map(sortKeysDeep);
  if (v && typeof v === "object") {
    const out = {};
    for (const k of Object.keys(v).sort()) out[k] = sortKeysDeep(v[k]);
    return out;
  }
  return v;
}

// prettyJSONText re-renders textual JSON content (doc-detail view AND edit)
// with a stable, sorted key order (P5) — a no-op for any non-JSON
// content-type, and falls back to the raw text verbatim on a parse failure
// (never blanks or throws on unexpected bytes despite a JSON content-type).
function prettyJSONText(text, ctype) {
  if (!/json/i.test(ctype || "")) return text;
  try { return JSON.stringify(sortKeysDeep(JSON.parse(text)), null, 2); }
  catch (_) { return text; }
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
  }, "danger");
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
    }, "danger"); // deletes the source once the copy lands — stays danger even though the prompt above it is primary
  });
}

async function downloadBlob(service, n) {
  // Embedded: <a download> is blocked in a webview. The host opens a one-use
  // browser-local streaming URL; only the correlated outcome returns here.
  // Standalone keeps its direct object-URL fallback.
  if (state.embedded) {
    const id = "d" + (++downloadSeq);
    downloadPending[id] = { gen: contentGen };
    hostAction({ type: "dc-download", id: id, service: service, segs: n.path.segments, name: n.name || "object" });
    return;
  }
  try {
    const r = await api("/api/download?" + new URLSearchParams({ service, segs: JSON.stringify(n.path.segments) }));
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
    // Class, not id (P10): a nested container gets its own bar (B8) that can
    // coexist with the root's, and an id would collide across instances.
    bar.innerHTML = `<button class="link uploadbtn">⤒ Upload file</button>`;
    bar.querySelector(".uploadbtn").onclick = () => hostAction({ type: "dc-upload", service: service, segs: segs });
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
  }, "primary");
}

function refreshTree(service) {
  if (state.active === service) loadTree(service, [], document.getElementById("tree"), true);
}

// ---------- grid (tabular / KV-collection / query — ONE renderer) ----------
async function openTable(service, n) {
  state.reopen = () => openTable(service, n);
  const gen = ++contentGen;
  const content = document.getElementById("content");
  setTabularContent(content, true);
  content.innerHTML = stateLoading("Loading " + n.name);
  const initialParams = new URLSearchParams({ service, segs: JSON.stringify(n.path.segments) });
  let initial;
  try { initial = await apiJSON("/api/table?" + initialParams.toString()); }
  catch (e) { if (gen === contentGen) content.innerHTML = gate(service, e); return; }
  if (gen !== contentGen) return;
  if (!initial.numbered) {
    renderGrid(content, service, initial, {
      node: n,
      title: n.name,
      paginate: (cursor) => apiJSON("/api/table?" + new URLSearchParams({ service, segs: JSON.stringify(n.path.segments), cursor }).toString()),
    });
    maybeTTL(service, n, () => openTable(service, n), gen);
    return;
  }
  const relation = {
    service, node: n, gen, page: 0, pageSize: 100, sort: null,
    displayedPage: 0, displayedPageSize: 100,
    total: null, countFailed: false, rowEpoch: 0, countEpoch: 0,
    rowsOnPage: (initial.rows || []).length, hasNext: !!initial.nextCursor,
  };
  renderGrid(content, service, initial, {
    node: n,
    title: n.name,
    relation,
    reload: (withCount) => loadRelationPage(content, relation, !!withCount),
  });
  loadRelationCount(content, relation);
  maybeTTL(service, n, () => openTable(service, n), gen);
}

function relationQuery(relation, request) {
  const params = new URLSearchParams({
    service: relation.service,
    segs: JSON.stringify(relation.node.path.segments),
    cursor: String(request.page * request.pageSize),
    limit: String(request.pageSize),
  });
  if (request.sort) {
    params.set("sort", request.sort.column);
    params.set("direction", request.sort.direction);
  }
  return params;
}

async function loadRelationPage(content, relation, refreshCount) {
  // A mutation can finish after the user has navigated elsewhere. Its captured
  // relation reload must become a complete no-op before it increments epochs,
  // changes #content, or starts row/count traffic for the superseded view.
  if (relation.gen !== contentGen) return;
  const rowEpoch = ++relation.rowEpoch;
  const request = {
    page: relation.page,
    pageSize: relation.pageSize,
    sort: relation.sort ? { column: relation.sort.column, direction: relation.sort.direction } : null,
  };
  if (!content.querySelector(".gridwrap")) content.innerHTML = stateLoading("Loading " + relation.node.name);
  else content.classList.add("relation-loading");
  if (refreshCount) loadRelationCount(content, relation);
  let tp;
  try { tp = await apiJSON("/api/table?" + relationQuery(relation, request).toString()); }
  catch (e) {
    if (relation.gen !== contentGen || rowEpoch !== relation.rowEpoch) return;
    content.innerHTML = gate(relation.service, e);
    return;
  }
  if (relation.gen !== contentGen || rowEpoch !== relation.rowEpoch) return;
  content.classList.remove("relation-loading");
  relation.displayedPage = request.page;
  relation.displayedPageSize = request.pageSize;
  relation.rowsOnPage = (tp.rows || []).length;
  relation.hasNext = !!tp.nextCursor;
  renderGrid(content, relation.service, tp, {
    node: relation.node,
    title: relation.node.name,
    relation,
    reload: (withCount) => loadRelationPage(content, relation, !!withCount),
  });
}

async function loadRelationCount(content, relation) {
  const countEpoch = ++relation.countEpoch;
  relation.total = null;
  relation.countFailed = false;
  try {
    const q = new URLSearchParams({ service: relation.service, segs: JSON.stringify(relation.node.path.segments) });
    const result = await apiJSON("/api/table/count?" + q.toString());
    if (relation.gen !== contentGen || countEpoch !== relation.countEpoch) return;
    const total = Number(result.count);
    if (!Number.isSafeInteger(total) || total < 0) throw new Error("invalid count");
    relation.total = total;
    const lastPage = Math.max(0, Math.ceil(total / relation.pageSize) - 1);
    if (relation.page > lastPage) {
      relation.page = lastPage;
      await loadRelationPage(content, relation, false);
      return;
    }
  } catch (_) {
    if (relation.gen !== contentGen || countEpoch !== relation.countEpoch) return;
    relation.total = null;
    relation.countFailed = true;
  }
  if (relation.gen === contentGen && countEpoch === relation.countEpoch) {
    renderRelationPaginator(content, relation,
      (withCount) => loadRelationPage(content, relation, !!withCount));
  }
}

// renderGrid is the ONE grid renderer (U-01): tabular tables, KV collections AND
// SQL query results all draw here. Editability is SERVER truth — a cell is
// interactive only when Column.editable AND the table exposes a row key AND the
// session may write; query columns arrive editable:false with rowKeyCols:null, so
// the same code draws them explicitly read-only, never an editable grid that
// silently ignores clicks. `opts`: {node, title, note, source, paginate}.
function renderGrid(content, service, tp, opts) {
  opts = opts || {};
  setTabularContent(content, true);
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
  const gctx = { service, node, editEnabled, showDelete, usesKVEntry, reload: opts.relation ? opts.reload : null };
  const widthKey = gridColumnWidthKey(service, node, title);

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
  if (canWrite && actionEnabled(service, ACTION.insertRow) && !noKey) h += actionButton("insertrow", "Insert row", "ghost");
  h += `</div><div class="gridwrap"><table class="grid"><thead><tr>`;
  for (let columnIndex = 0; columnIndex < cols.length; columnIndex++) {
    const c = cols[columnIndex];
    const columnLabelID = "grid-column-label-" + columnIndex;
    const why = (editEnabled && c && !c.editable && c.reason) ? " · " + c.reason : "";
    const sorted = opts.relation && opts.relation.sort && opts.relation.sort.column === c.name
      ? opts.relation.sort.direction : null;
    const ariaSort = sorted === "asc" ? "ascending" : (sorted === "desc" ? "descending" : "none");
    const ariaSortAttr = opts.relation && c.sortable ? ` aria-sort="${ariaSort}"` : "";
    const sortWhy = c.sortable === false && c.sortReason ? " · " + c.sortReason : "";
    h += `<th data-column-index="${columnIndex}"${ariaSortAttr} title="${esc((c.dataType || "") + why + sortWhy)}">`;
    if (opts.relation && c.sortable) {
      const marker = sorted === "asc" ? " ▲" : (sorted === "desc" ? " ▼" : "");
      h += `<button type="button" class="sortable" id="${columnLabelID}">${esc(c.name)}${c.pk ? " 🔑" : ""}${marker}</button>`;
    } else {
      h += `<span id="${columnLabelID}">${esc(c.name)}${c.pk ? " 🔑" : ""}</span>`;
    }
    h += `<span class="column-resizer" role="separator" aria-orientation="vertical" tabindex="0"`
      + ` aria-label="Resize data column" aria-describedby="${columnLabelID}" aria-valuemin="${GRID_COLUMN_MIN}"`
      + ` aria-valuemax="${GRID_COLUMN_MAX}" aria-valuenow="${GRID_COLUMN_DEFAULT}"></span></th>`;
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
  if (node) wireAction("insertrow", service, ACTION.insertRow, () => insertRow(service, node, cols, gctx.reload));
  if (opts.relation) {
    for (const button of content.querySelectorAll("th button.sortable")) {
      button.onclick = () => {
        const column = cols[Number(button.closest("th").getAttribute("data-column-index"))].name;
        const current = opts.relation.sort;
        opts.relation.sort = {
          column,
          direction: current && current.column === column && current.direction === "asc" ? "desc" : "asc",
        };
        opts.relation.page = 0;
        opts.reload(false);
      };
    }
    if (tp.bestEffort) {
      const toolbar = content.querySelector(".toolbar");
      const badge = document.createElement("span");
      badge.className = "badge view-only best-effort";
      badge.title = "No primary key — OFFSET pages are a live, best-effort view.";
      badge.textContent = "live · best-effort";
      toolbar.insertBefore(badge, toolbar.querySelector(".spacer"));
    }
    renderRelationPaginator(content, opts.relation, opts.reload);
  } else {
    gridLoadMore(content, service, tp, cols, keyCols, gctx, opts.paginate);
  }
  freezeGridColumns(content.querySelector("table.grid"), cols, widthKey, showDelete);
}

function renderRelationPaginator(content, relation, reload) {
  const gridwrap = content.querySelector(".gridwrap");
  if (!gridwrap) return;
  let bar = content.querySelector(".paginator");
  if (!bar) {
    bar = document.createElement("nav");
    gridwrap.after(bar);
  }
  bar.className = "paginator" + (relation.total == null ? " fallback" : " exact");
  const displayedPage = relation.displayedPage;
  const displayedPageSize = relation.displayedPageSize;
  const start = relation.rowsOnPage ? displayedPage * displayedPageSize + 1 : 0;
  const rawEnd = displayedPage * displayedPageSize + relation.rowsOnPage;
  const end = relation.total == null ? rawEnd : Math.min(rawEnd, relation.total);
  const range = relation.total == null
    ? (relation.rowsOnPage ? `${start}–${end}` : "No rows")
    : `${start}–${end} of ${relation.total.toLocaleString("en-US")}`;
  let html = `<span class="page-range">${range}</span>`;
  if (relation.total != null) html += `<button type="button" class="ghost page-first" ${displayedPage === 0 ? "disabled" : ""} aria-label="First page">«</button>`;
  html += `<button type="button" class="ghost page-prev" ${displayedPage === 0 ? "disabled" : ""} aria-label="Previous page">‹</button>`;
  if (relation.total != null) {
    const pages = Math.max(1, Math.ceil(relation.total / displayedPageSize));
    let first = Math.max(0, displayedPage - 3);
    first = Math.min(first, Math.max(0, pages - 7));
    const last = Math.min(pages, first + 7);
    for (let i = first; i < last; i++) {
      html += `<button type="button" class="ghost page-number${i === displayedPage ? " active" : ""}" data-page="${i}" ${i === displayedPage ? 'aria-current="page"' : ""}>${i + 1}</button>`;
    }
    html += `<button type="button" class="ghost page-next" ${displayedPage >= pages - 1 ? "disabled" : ""} aria-label="Next page">›</button>`;
    html += `<button type="button" class="ghost page-last" ${displayedPage >= pages - 1 ? "disabled" : ""} aria-label="Last page">»</button>`;
  } else {
    html += `<button type="button" class="ghost page-next" ${!relation.hasNext ? "disabled" : ""} aria-label="Next page">›</button>`;
  }
  html += `<label class="page-size">Rows <select aria-label="Rows per page">`;
  for (const size of RELATION_PAGE_SIZES) html += `<option value="${size}" ${size === displayedPageSize ? "selected" : ""}>${size}</option>`;
  html += `</select></label><button type="button" class="ghost page-refresh" aria-label="Refresh rows and total">↻</button>`;
  bar.innerHTML = html;
  const go = (page) => {
    if (!reload || page < 0 || page === relation.page) return;
    relation.page = page;
    reload(false);
  };
  const first = bar.querySelector(".page-first");
  if (first) first.onclick = () => go(0);
  bar.querySelector(".page-prev").onclick = () => go(displayedPage - 1);
  bar.querySelector(".page-next").onclick = () => go(displayedPage + 1);
  for (const button of bar.querySelectorAll(".page-number")) button.onclick = () => go(Number(button.getAttribute("data-page")));
  const last = bar.querySelector(".page-last");
  if (last) last.onclick = () => go(Math.max(0, Math.ceil(relation.total / displayedPageSize) - 1));
  bar.querySelector("select").onchange = (e) => {
    const size = Number(e.target.value);
    if (!RELATION_PAGE_SIZES.includes(size)) return;
    relation.pageSize = size;
    relation.page = 0;
    // The select describes the rows that are currently on screen. Keep its
    // displayed value atomic with those rows while the requested window is
    // in flight; the winning response re-renders it at the new size.
    e.target.value = String(displayedPageSize);
    reload(false);
  };
  bar.querySelector(".page-refresh").onclick = () => reload(true);
}

function gridColumnWidthKey(service, node, title) {
  const identity = node && node.path && Array.isArray(node.path.segments)
    ? node.path.segments
    : ["$result", String(title || "result")];
  return JSON.stringify([service].concat(identity));
}

function clampGridColumnWidth(value) {
  return Math.round(Math.max(GRID_COLUMN_MIN, Math.min(GRID_COLUMN_MAX, value)));
}

// freezeGridColumns keeps edit-time layout stable and owns column resizing.
// Every rendered column gets an explicit width, and the table's own width is
// always their sum, so overflow belongs to .gridwrap rather than the page.
// Only data columns expose separators; the trailing action column stays fixed.
function freezeGridColumns(table, columns, widthKey, showDelete) {
  if (!table) return;
  const headers = Array.from(table.querySelectorAll(":scope > thead th"));
  if (!headers.length) return;
  const saved = layoutPrefs.columnWidths[widthKey] && typeof layoutPrefs.columnWidths[widthKey] === "object"
    ? layoutPrefs.columnWidths[widthKey]
    : {};
  const widths = columns.map((column, index) => {
    const restored = Object.prototype.hasOwnProperty.call(saved, column.name) ? Number(saved[column.name]) : NaN;
    if (Number.isFinite(restored)) return clampGridColumnWidth(restored);
    const measured = headers[index] ? headers[index].offsetWidth : 0;
    return measured > 0 ? clampGridColumnWidth(measured) : GRID_COLUMN_DEFAULT;
  });
  if (showDelete) widths.push(GRID_ACTION_COLUMN_WIDTH);

  const colgroup = document.createElement("colgroup");
  for (const width of widths) {
    const col = document.createElement("col");
    col.style.width = width + "px";
    colgroup.appendChild(col);
  }
  table.insertBefore(colgroup, table.firstChild);
  table.style.tableLayout = "fixed";

  const syncTableWidth = () => {
    table.style.width = widths.reduce((sum, width) => sum + width, 0) + "px";
  };
  const persistColumn = (index) => {
    const next = Object.assign(Object.create(null), layoutPrefs.columnWidths[widthKey] || {});
    next[columns[index].name] = widths[index];
    layoutPrefs.columnWidths[widthKey] = next;
    persistLayoutPrefs();
  };
  const applyColumn = (index, next, save) => {
    widths[index] = clampGridColumnWidth(next);
    colgroup.children[index].style.width = widths[index] + "px";
    const handle = headers[index].querySelector(".column-resizer");
    if (handle) handle.setAttribute("aria-valuenow", String(widths[index]));
    syncTableWidth();
    if (save) persistColumn(index);
  };

  columns.forEach((_column, index) => {
    const handle = headers[index] && headers[index].querySelector(".column-resizer");
    if (!handle) return;
    handle.setAttribute("aria-valuenow", String(widths[index]));
    handle.addEventListener("click", (e) => {
      e.preventDefault();
      e.stopPropagation();
    });
    handle.addEventListener("pointerdown", (e) => {
      if (e.button != null && e.button !== 0) return;
      e.preventDefault();
      e.stopPropagation();
      const startX = e.clientX;
      const startWidth = widths[index];
      handle.classList.add("resizing");
      const move = (moveEvent) => {
        moveEvent.preventDefault();
        applyColumn(index, startWidth + moveEvent.clientX - startX, false);
      };
      const up = (upEvent) => {
        upEvent.preventDefault();
        window.removeEventListener("pointermove", move);
        window.removeEventListener("pointerup", up);
        window.removeEventListener("pointercancel", up);
        handle.classList.remove("resizing");
        applyColumn(index, widths[index], true);
      };
      window.addEventListener("pointermove", move);
      window.addEventListener("pointerup", up);
      window.addEventListener("pointercancel", up);
    });
    handle.addEventListener("keydown", (e) => {
      let next = null;
      if (e.key === "ArrowLeft") next = widths[index] - GRID_COLUMN_KEYBOARD_STEP;
      else if (e.key === "ArrowRight") next = widths[index] + GRID_COLUMN_KEYBOARD_STEP;
      else if (e.key === "Home") next = GRID_COLUMN_MIN;
      else if (e.key === "End") next = GRID_COLUMN_MAX;
      if (next == null) return;
      e.preventDefault();
      e.stopPropagation();
      applyColumn(index, next, true);
    });
  });
  syncTableWidth();
}

function appendGridRows(body, tp, cols, keyCols, gctx) {
  const { service, node, editEnabled, showDelete, usesKVEntry, reload } = gctx;
  for (const row of (tp.rows || [])) {
    const tr = document.createElement("tr");
    cols.forEach((c, i) => {
      const td = document.createElement("td");
      const text = fmt(row[i]);
      td.textContent = text;
      // The full value is always reachable — editable cells via the editor,
      // non-editable ones via a click-to-view modal (B3) — so a truncated cell
      // is never a dead end. The title stays a STATIC affordance hint, never the
      // raw value: a cell value can carry markup, and echoing it into an
      // attribute would put untrusted "<script>…" into the serialized DOM
      // (inert, but the XSS invariant forbids the raw substring outright). The
      // click view escapes the value; the tooltip must not reintroduce it raw.
      // Editable iff the SERVER says so (Column.editable, PK/query/view-only tiers
      // already false); KV entry cells additionally consult entryEditPlan (a hash
      // field with a sibling, a redis list) for the payload-shape lock (D-02/D-03).
      let interactive = editEnabled && c && c.editable;
      if (interactive && usesKVEntry && entryEditPlan(cols, keyCols, row, i).kind === "locked") interactive = false;
      if (interactive) {
        td.className = "editable";
        td.title = "Click to edit";
        td.onclick = () => editCell(service, node, cols, keyCols, row, c, i, td, usesKVEntry, reload);
      } else if (editEnabled && c && !c.editable) {
        // Explicit "why not" on a locked cell in write mode (U-06) — never a silent
        // no-op that looks identical to an editable one. Non-editable is also
        // non-EDITABLE, not non-INTERACTIVE (B3): click opens a read-only full-value
        // view, so a truncated PK/locked cell is never a dead end.
        td.className = "locked";
        td.title = c.reason || "Click to view full value";
        td.onclick = () => openCellView(c.name, text);
      } else {
        // editEnabled is false for the whole grid (view-only service, read-only
        // session, or a query/KV-collection result with no row key) — every cell
        // is a dead end without this (B3): click still opens the full-value view.
        td.className = "viewcell";
        td.title = "Click to view full value";
        td.onclick = () => openCellView(c.name, text);
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
      del.onclick = () => deleteRow(service, node, cols, keyCols, row, usesKVEntry, reload);
      td.appendChild(del); tr.appendChild(td);
    }
    body.appendChild(tr);
  }
}

// openCellView (B3) is the full-value read path for a NON-editable cell — a
// plain info dialog, not a mutation confirm: single OK button (no Cancel, no
// danger styling), and onOK is a no-op so clicking OK simply closes it via
// the normal modal machinery.
function openCellView(colName, text) {
  showModal(colName, `<pre class="blob">${esc(text)}</pre>`, async () => {}, { viewOnly: true });
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
    // Snapshot the content generation AND the exact tbody this click belongs
    // to BEFORE the await (C1): a slower page that resolves after the user
    // has navigated to a different table must never land under the NEW
    // table's tbody -- re-querying content.querySelector("tbody.gridbody")
    // AFTER the await would find whatever tbody is CURRENTLY in #content
    // (the new table's), not the one this button was paginating.
    const gen = contentGen;
    const tbody = content.querySelector("tbody.gridbody");
    more.disabled = true; more.textContent = "Loading…";
    try {
      const next = await paginate(tp.nextCursor);
      if (gen !== contentGen || !tbody.isConnected) return; // a newer content render superseded this pagination — drop the stale append
      appendGridRows(tbody, next, cols, keyCols, gctx);
      gridLoadMore(content, service, next, cols, keyCols, gctx, paginate);
    } catch (e) {
      if (gen !== contentGen || !tbody.isConnected) return; // the button (and any error UI) belongs to a superseded view
      more.disabled = false; more.textContent = "Load more…"; toast(e, true);
    }
  };
  content.querySelector(".gridwrap").after(more);
}

function editCell(service, n, cols, keyCols, row, col, idx, td, kv, reload) {
  const oldVal = row[idx];
  const input = document.createElement("input");
  input.className = "celledit"; input.value = oldVal == null ? "" : String(oldVal);
  // The editor lives in a flex wrapper sized to the TEXT line (input border is
  // offset by a negative margin in CSS) so entering edit mode never changes the
  // row's height — neighbor cells must not move when an editor opens or closes
  // (U-01 layout stability; live-measured: the bare input+button used to grow
  // the row ~3px).
  const wrap = document.createElement("span");
  wrap.className = "celleditwrap";
  wrap.appendChild(input);
  td.textContent = ""; td.appendChild(wrap);
  // NULL affordance (B7): SQL NULL is otherwise unreachable — clearing the
  // input and blurring commits an empty STRING, never null. Tabular-only: a
  // redis field/member has no NULL concept (DEL is the operation for that),
  // so no button renders on the kv path. `nv === null` here is the ONE signal
  // that distinguishes "commit true JSON null" from "commit input.value" —
  // doCommit takes nv directly (never an Event) so a bare `input.onblur =
  // commit` can never be misread as an explicit-null request.
  let nullBtn = null;
  if (!kv) {
    nullBtn = document.createElement("button");
    nullBtn.type = "button";
    nullBtn.className = "ghost cellnull";
    nullBtn.textContent = "∅ NULL";
    nullBtn.title = "Set to SQL NULL";
    // A real browser shifts focus (and so fires blur) on mousedown BEFORE the
    // click handler runs — without this, clicking NULL would first commit
    // whatever is still in the input (via blur -> commit) and only then send
    // the null commit, racing two requests. preventDefault on mousedown keeps
    // focus on the input, so only the click's doCommit(null) ever fires.
    nullBtn.addEventListener("mousedown", (e) => e.preventDefault());
    wrap.appendChild(nullBtn);
  }
  input.focus();
  const doCommit = async (nv) => {
    // NULL and "" are DISTINCT values (C5): a cell that was SQL NULL renders
    // its input as "" (there is nothing else to show), but re-committing ""
    // from there is a real edit, not a no-op — so the string branch only
    // counts as unchanged when oldVal was itself already a (non-null) string
    // equal to nv. Collapsing the two (comparing against "" whenever oldVal
    // was null) made NULL -> "" uncommittable.
    const unchanged = nv === null ? oldVal == null : (oldVal != null && nv === String(oldVal));
    if (unchanged) { td.textContent = fmt(oldVal); return; }
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
      if (!kv && reload) reload(true);
    } catch (e) {
      if (e && e.code === "timeout") {
        // accepted, not confirmed (U-14): keep the optimistic value, say so honestly.
        row[idx] = nv; td.textContent = fmt(nv); toast("Accepted — still applying.", "warn");
      } else {
        td.textContent = fmt(oldVal); toast("Save failed: " + errorSummary(e), true);
      }
    }
  };
  const commit = () => doCommit(input.value);
  // Focus moving from the input straight to the NULL button (Tab, or a real
  // browser's mousedown default action before the mousedown handler above
  // prevents it for the mouse path) fires this blur FIRST (C5). Committing
  // here would either wipe the NULL button out of the DOM before it can ever
  // be activated by keyboard (an unchanged value's commit does
  // `td.textContent = ...`), or race a string write against the button's own
  // null write. relatedTarget names the element RECEIVING focus, so this can
  // tell "leaving to the NULL button" apart from any other blur and defer to
  // the button's own click/keydown instead of committing twice.
  input.onblur = (e) => { if (nullBtn && e && e.relatedTarget === nullBtn) return; commit(); };
  if (nullBtn) nullBtn.onclick = () => doCommit(null);
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

// rowIdentity renders a table row's key columns as "col=value, col2=value2"
// (B6) for an honest delete-confirm — every keyCols entry, comma-joined;
// values truncated so a large column never blows up the modal title.
function rowIdentity(cols, keyCols, row) {
  const key = rowKeyOf(cols, keyCols, row);
  return Object.keys(key).map((k) => k + "=" + truncateForTitle(fmt(key[k]))).join(", ");
}
function truncateForTitle(s) {
  return s.length > 40 ? s.slice(0, 40) + "…" : s;
}

// deleteRow's confirm names its target (B6) — never the generic "this row":
// a tabular row by its key columns ("id=4"), a KV entry by its field/member
// name, matching the house pattern the whole-key delete (openBlob's
// "Delete <name>?") already uses.
function deleteRow(service, n, cols, keyCols, row, kv, reload) {
  if (kv) {
    const field = String(row[0]);
    confirmAction(`Delete ${field}?`, `DELETE ${field}`, async () => {
      await api("/api/entry", { method: "DELETE", headers: jsonConfirm(),
        body: JSON.stringify({ path: n.path, field }) });
      toast("Deleted."); openTable(service, n); // re-read to confirm gone (I-1)
    }, "danger");
    return;
  }
  const ident = rowIdentity(cols, keyCols, row);
  confirmAction(`Delete row ${ident}?`, `DELETE row WHERE ${ident}`, async () => {
    await api("/api/row", { method: "DELETE", headers: jsonConfirm(),
      body: JSON.stringify({ path: n.path, key: rowKeyOf(cols, keyCols, row) }) });
    toast("Deleted.");
    if (reload) reload(true); else openTable(service, n); // re-read to confirm gone (I-1)
  }, "danger");
}

function insertRow(service, n, cols, reload) {
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
    if (reload) reload(true); else openTable(service, n);
  }, { kind: "primary" });
}

// ---------- SQL query console ----------
function openQuery(service) {
  state.reopen = () => openQuery(service);
  ++contentGen; // mint a new content generation — invalidates any prior in-flight content render
  const content = document.getElementById("content");
  setTabularContent(content, false);
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
  const gen = ++contentGen;
  const content = document.getElementById("content");
  setTabularContent(content, false);
  content.innerHTML = stateLoading("Loading indices");
  let indices = [];
  try {
    const data = await apiJSON("/api/tree?" + new URLSearchParams({ service, segs: "[]" }));
    indices = (data.nodes || []).filter((n) => n.kind === "container").map((n) => n.name);
  } catch (e) { if (gen !== contentGen) return; content.innerHTML = gate(service, e); return; }
  if (gen !== contentGen) return; // a newer content render superseded this one — drop the stale render
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
      // A client-side validation failure THROWS (never a bare toast+return):
      // the #modalok completion treats a non-throwing run() as success and
      // hides the modal, discarding the typed input -- throwing routes it
      // through the SAME inline-error, modal-stays-open path a server
      // rejection gets. No .code on this Error, so it is never mistaken for
      // the `timeout` (accepted-not-confirmed) sentinel.
      try { parsed = JSON.parse(bodyText); } catch (_) { throw new Error("Body is not valid JSON."); }
      const segs = id ? [index, id] : [index];
      const applied = await apiJSON("/api/document/create", {
        method: "POST", headers: jsonConfirm(),
        body: JSON.stringify({ path: { service, segments: segs }, data: b64(new TextEncoder().encode(JSON.stringify(parsed))) }),
      });
      const newId = applied && applied.id ? applied.id : id;
      toast("Document created.");
      if (newId) openBlob(service, { name: newId, kind: "blob", path: { service, segments: [index, newId] } });
    }, { kind: "primary" });
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
      // Client-side validation THROWS (see createDocForm's comment above) so
      // the #modalok lifecycle renders it inline and keeps the modal (and the
      // typed input) intact, instead of reading a bare return as success.
      if (!name) throw new Error("Key name required.");
      const type = document.getElementById("kvtype").value;
      const body = { path: { service, segments: [name] }, type: type };
      if (type === "hash") {
        const field = document.getElementById("kvfield").value;
        if (!field) throw new Error("Field name required for a hash.");
        body.field = field;
        const val = document.getElementById("kvval").value;
        if (val !== "") body.value = b64(new TextEncoder().encode(val));
      } else if (type === "zset") {
        const member = document.getElementById("kvfield").value;
        if (!member) throw new Error("Member required for a zset.");
        const score = parseFloat(document.getElementById("kvscore").value);
        if (isNaN(score)) throw new Error("Numeric score required for a zset.");
        body.field = member;
        body.score = score;
      } else {
        const val = document.getElementById("kvval").value;
        if (val !== "") body.value = b64(new TextEncoder().encode(val));
      }
      await apiJSON("/api/kv/create", { method: "POST", headers: jsonConfirm(), body: JSON.stringify(body) });
      toast("Key created.");
      refreshTree(service);
    }, { kind: "primary" });
  document.getElementById("kvtype").onchange = (e) => {
    document.getElementById("kvextra").innerHTML = kvExtraFieldsHTML(e.target.value);
  };
}

// ---------- KV TTL control (wires /api/stat) ----------
// gen (B4) is the caller's contentGen snapshot: this runs as an independent
// async tail AFTER the main blob/table render already completed, so it needs
// its OWN check before appending — a since-superseded view must never gain a
// TTL bar for whatever it used to show.
async function maybeTTL(service, n, reopen, gen) {
  if (!hasAction(service, ACTION.setTTL)) return;
  let node;
  try { node = await apiJSON("/api/stat?" + new URLSearchParams({ service, segs: JSON.stringify(n.path.segments) })); }
  catch (_) { return; }
  if (gen !== contentGen) return; // a newer content render superseded this one — drop the stale append
  const ttl = node.meta ? node.meta.ttlSeconds : null;
  // nil/negative ⇒ "no expiry", NEVER "0s" (KV-AUD-02; the S28 sentinel is nil).
  const cur = (ttl == null || ttl < 0) ? "no expiry" : ttl + "s";
  const bar = document.createElement("div");
  bar.className = "ttlbar";
  bar.innerHTML = `<span class="meta">TTL: ${esc(cur)}</span>`;
  if (editing() && actionEnabled(service, ACTION.setTTL)) {
    bar.innerHTML += " "
      + actionButton("setttl", "Set TTL", "ghost")
      + " "
      + actionButton("clrttl", "Persist", "ghost");
  }
  document.getElementById("content").appendChild(bar);
  wireAction("setttl", service, ACTION.setTTL, () => {
    promptModal(`Set TTL on ${n.name}`, "TTL seconds", ttl > 0 ? String(ttl) : "3600", (v) => {
      const secs = parseInt(v, 10);
      if (isNaN(secs)) return;
      confirmAction(`Set TTL ${secs}s on ${n.name}?`, `EXPIRE ${secs}s`, async () => {
        await api("/api/ttl", { method: "PUT", headers: jsonConfirm(), body: JSON.stringify({ path: n.path, ttlSeconds: secs }) });
        toast("TTL set."); reopen();
      }, "primary");
    });
  });
  wireAction("clrttl", service, ACTION.setTTL, () => confirmAction(`Clear TTL on ${n.name}?`, "PERSIST", async () => {
    await api("/api/ttl", { method: "PUT", headers: jsonConfirm(), body: JSON.stringify({ path: n.path, ttlSeconds: null }) });
    toast("TTL cleared."); reopen();
  }, "primary"));
}

// ---------- confirm modal ----------
// Lifecycle (B1): Confirm disables both buttons + shows "Working…" for the
// duration of run(); SUCCESS closes the modal (the run's own toast still
// fires); a REJECTION keeps the modal open — typed values intact, an inline
// .err line inside .modalbox, buttons re-enabled — so a failed write never
// throws away what the user typed. `timeout` (accepted-not-confirmed, U-14)
// is not a failure: it still closes the modal via the existing warn toast.
//
// Keyboard/focus (B2): Escape and a backdrop click both cancel — routed
// through cancelModal() so they honor the SAME in-flight guard as the Cancel
// button (disabled while a B1 write is pending). Opening focuses the first
// focusable control in .modalbox and traps Tab/Shift+Tab within it; closing
// restores focus to whatever was focused before the modal opened. The
// document-level keydown handler no-ops whenever #modal is hidden, so it
// never reaches into the grid cell editor's own (unrelated) Escape handling.
let modalOK = null;
let modalPrevFocus = null;
// modalEpoch identifies the CURRENT modal "instance". showModal() bumps it.
// A prompt-to-confirm chain (promptModal's onValue calling confirmAction,
// e.g. Rename/Set TTL) opens a SECOND modal on the shared #modal while the
// first one's own #modalok completion is still in flight (its run() awaited
// onValue(), which synchronously showModal()'d the second one) -- without
// this, that outer completion's hideModal() tears down the second modal it
// never even knew existed. #modalok's handler snapshots the epoch when it
// starts and only closes the modal if it is still current.
let modalEpoch = 0;

function focusablesIn(container) {
  if (!container) return [];
  return Array.from(container.querySelectorAll('a[href], button, textarea, input, select, [tabindex]'))
    .filter((el) => !el.disabled && el.tabIndex !== -1 && !el.closest(".hidden"));
}

function clearModalError() {
  const err = document.getElementById("modalerr");
  if (err) err.remove();
}

// showModalError renders a rejected run() inline (B1) — the same mapping the
// toast would have shown (errorSummary), so the message a user sees does not
// change shape just because the modal stayed open to show it.
function showModalError(e) {
  let err = document.getElementById("modalerr");
  if (!err) {
    err = document.createElement("div");
    err.id = "modalerr";
    err.className = "err";
    document.querySelector(".modalbox .modalbtns").before(err);
  }
  err.textContent = errorSummary(e);
}

function showModal(title, bodyHTML, onOK, opts) {
  modalPrevFocus = document.activeElement;
  document.getElementById("modaltitle").textContent = title;
  document.getElementById("modalbody").innerHTML = bodyHTML;
  clearModalError();
  const okBtn = document.getElementById("modalok");
  const cancelBtn = document.getElementById("modalcancel");
  okBtn.disabled = false; cancelBtn.disabled = false;
  okBtn.textContent = "Confirm";
  // Confirm severity (P1): a destructive action (delete/overwrite/rename's
  // COPY+DELETE) gets the red danger styling; a create/insert/TTL-set/
  // data-entry prompt gets the standard button/accent look instead, so "Add
  // key" no longer reads as alarming as "Delete". A caller that omits `kind`
  // defaults to danger (fail loud on an unclassified action, never fail
  // quiet on one that turns out destructive). No CSS is needed for the
  // primary case: dropping .danger already falls back to the base `button` look.
  okBtn.classList.toggle("danger", !(opts && opts.kind === "primary"));
  cancelBtn.classList.remove("hidden");
  if (opts && opts.viewOnly) {
    // A read-only info dialog (openCellView's full-value view): single OK
    // button, no Cancel, no destructive styling. Set BEFORE the focus call
    // below (C6) — focusablesIn() excludes hidden controls, so computing
    // focusables AFTER Cancel is hidden here (rather than by the caller,
    // post-hoc) keeps the initial focus off a control that is about to
    // disappear.
    cancelBtn.classList.add("hidden");
    okBtn.textContent = "OK";
    okBtn.classList.remove("danger");
  }
  document.getElementById("modal").classList.remove("hidden");
  modalOK = onOK;
  ++modalEpoch;
  const box = document.querySelector(".modalbox");
  const focusables = focusablesIn(box);
  // No focusable control (e.g. opening straight into a state with nothing to
  // focus) — park on the box itself (tabindex="-1" in index.html) rather than
  // leaving focus wherever it was before the modal opened.
  if (focusables.length) focusables[0].focus(); else box.focus();
}
function hideModal() {
  document.getElementById("modal").classList.add("hidden");
  modalOK = null;
  clearModalError();
  if (modalPrevFocus && typeof modalPrevFocus.focus === "function") modalPrevFocus.focus();
  modalPrevFocus = null;
}
// cancelModal is Escape/backdrop-click/#modalcancel's one shared path — a
// no-op while a B1 write is in flight (Cancel carries that disabled state).
function cancelModal() {
  if (document.getElementById("modalcancel").disabled) return;
  hideModal();
}
function onModalKeydown(e) {
  const modal = document.getElementById("modal");
  if (modal.classList.contains("hidden")) return; // untouched: e.g. the grid cell editor's own Escape
  if (e.key === "Escape") { cancelModal(); return; }
  if (e.key === "Tab") {
    // Claim Tab unconditionally while the modal is open (C6) — returning
    // early without preventDefault (the old zero-focusables path) handed Tab
    // back to the page's native tab order, letting it escape the modal
    // entirely during a B1 in-flight write (both buttons disabled, no other
    // form fields — a real, reachable zero-focusables state).
    e.preventDefault();
    const box = document.querySelector(".modalbox");
    const focusables = focusablesIn(box);
    if (!focusables.length) { box.focus(); return; } // nothing to focus — park on the box itself rather than let focus drift
    const idx = focusables.indexOf(document.activeElement);
    const next = e.shiftKey
      ? (idx <= 0 ? focusables.length - 1 : idx - 1)
      : (idx === -1 || idx === focusables.length - 1 ? 0 : idx + 1);
    focusables[next].focus();
  }
}
document.addEventListener("keydown", onModalKeydown);
document.getElementById("modal").addEventListener("click", (e) => {
  if (e.target === e.currentTarget) cancelModal(); // backdrop only, never a .modalbox descendant
});
function confirmAction(title, actionText, run, kind) {
  showModal(title, `<div class="action">${esc(actionText)}</div>`, run, { kind: kind || "danger" });
}
// promptModal asks for one value through the modal — window.prompt is a no-op in
// a VS Code webview. onValue runs with the entered string when the user confirms.
// Enter-to-submit comes from the general #modalbody wiring (B5); this only adds
// the select-all-on-open convenience on top of showModal's own focus-first-field.
// Always `kind: "primary"` (P1): gathering a value is never itself destructive
// -- a caller that DOES lead to a destructive act (Rename) makes that call on
// its own nested confirmAction(), independently of this prompt step.
function promptModal(title, label, defaultValue, onValue) {
  const dv = defaultValue == null ? "" : String(defaultValue);
  showModal(title, `<label class="modalprompt">${esc(label)}<input id="modalinput" value="${esc(dv)}"></label>`, async () => {
    const el = document.getElementById("modalinput");
    await onValue(el ? el.value : "");
  }, { kind: "primary" });
  const el = document.getElementById("modalinput");
  if (el && typeof el.select === "function") el.select();
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
function renderError(e) {
  const content = document.getElementById("content");
  setTabularContent(content, false);
  content.innerHTML = errorHTML(e);
}
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
  const gen = contentGen; // snapshot before the await — a stale refresh must not overwrite a navigation that happened while it was in flight
  try {
    const d = await apiJSON("/api/refresh", { method: "POST" });
    state.services = d.services || [];
    renderServices();
    if (gen !== contentGen) return; // the user navigated while refresh was in flight — the rail above is current, but #content now belongs to a newer view
    if (state.active) { const s = svcOf(state.active); if (s) selectService(s); }
    else openPendingService(); // a deep link that wasn't discovered before may exist now
  } catch (e) { toast(e, true); }
};
document.getElementById("tokenbtn").onclick = () => {
  const v = document.getElementById("tokeninput").value.trim();
  if (v) { state.token = v; start(); }
};
document.getElementById("editchk").onchange = (e) => onEditToggle(e.target.checked);
initSplitPane();
// B5: Enter on any <input> inside the modal submits — never a <textarea>
// (which uses Enter for a newline) or a <select>. Delegated on #modalbody
// itself (never recreated across showModal() calls, only its children are)
// so this wires once and covers every modal form (insert-row/add-key/
// add-document/…), not just promptModal's single input.
document.getElementById("modalbody").addEventListener("keydown", (e) => {
  if (e.key === "Enter" && e.target && e.target.tagName === "INPUT") {
    e.preventDefault();
    document.getElementById("modalok").click();
  }
});
document.getElementById("modalcancel").onclick = cancelModal;
document.getElementById("modalok").onclick = async () => {
  const run = modalOK;
  const epoch = modalEpoch; // this click's OWN modal instance
  const okBtn = document.getElementById("modalok");
  const cancelBtn = document.getElementById("modalcancel");
  if (!run || okBtn.disabled) return;
  okBtn.disabled = true; cancelBtn.disabled = true; okBtn.textContent = "Working…";
  try {
    await run();
    // A nested confirmAction() opened DURING run() (prompt-to-confirm, e.g.
    // Rename/Set TTL) shows a SECOND modal and bumps modalEpoch -- that
    // modal is live now and owns the UI; this (outer, already-resolved)
    // completion must not hideModal() out from under it.
    if (modalEpoch === epoch) hideModal();
  } catch (e) {
    if (modalEpoch !== epoch) return; // the nested modal owns the UI now
    if (e && e.code === "timeout") { hideModal(); toastError(e); return; }
    showModalError(e);
    okBtn.disabled = false; cancelBtn.disabled = false; okBtn.textContent = "Confirm";
  }
};
bootAuth();
