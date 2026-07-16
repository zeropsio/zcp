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
const { rowKeyOf, keyColScore } = DCRows;
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
    // persistent read-only indicator instead so the posture is unambiguous.
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
function actionReason(hostname, id) { return DCActions.actionReason(svcOf(hostname), id); }
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
  content.innerHTML = `<div class="placeholder">${hint}</div>`;
  const ql = document.getElementById("querylink");
  if (ql) ql.onclick = () => openQuery(s.hostname);
  loadTree(s.hostname, [], document.getElementById("tree"), true);
}

// ---------- tree ----------
async function loadTree(service, segs, container, root) {
  if (root) container.innerHTML = "";
  await appendTreePage(service, segs, container, root, "");
}

// appendTreePage loads one page of nodes and, when the server returns a cursor,
// adds a "load more" button so the whole level is reachable (no silent first-page).
async function appendTreePage(service, segs, container, root, cursor) {
  const q = new URLSearchParams({ service, segs: JSON.stringify(segs) });
  if (cursor) q.set("cursor", cursor);
  let data;
  try { data = await apiJSON("/api/tree?" + q.toString()); }
  catch (e) { container.innerHTML = gate(service, e); return; }
  const old = container.querySelector(":scope > .loadmore");
  if (old) old.remove();
  const nodes = data.nodes || [];
  if (root && nodes.length === 0 && !cursor) {
    container.innerHTML = `<div class="placeholder">Empty.</div>`;
    if (editing() && hasAction(service, ACTION.uploadObject)) addUploadBar(service, segs, container);
    return;
  }
  // Smart auto-expand: a lone container never costs a click — drill through.
  if (!cursor && nodes.length === 1 && nodes[0].kind === "container") {
    const el = renderNode(service, nodes[0]);
    container.appendChild(el);
    expandContainer(service, nodes[0], el);
    return;
  }
  for (const n of nodes) container.appendChild(renderNode(service, n));
  if (data.nextCursor) {
    const more = document.createElement("button");
    more.className = "loadmore";
    more.textContent = "Load more…";
    more.onclick = () => appendTreePage(service, segs, container, false, data.nextCursor);
    container.appendChild(more);
  }
  if (root && editing() && hasAction(service, ACTION.uploadObject)) addUploadBar(service, segs, container);
}

function expandContainer(service, n, el) {
  const kindEl = el.querySelector(":scope > .node .kind");
  let kids = el.querySelector(":scope > .children");
  if (kids) {
    const hidden = kids.classList.toggle("hidden");
    if (kindEl) kindEl.textContent = hidden ? "▸" : "▾";
    return;
  }
  kids = document.createElement("div");
  kids.className = "children";
  el.appendChild(kids);
  if (kindEl) kindEl.textContent = "▾";
  appendTreePage(service, n.path.segments, kids, false, "");
}

function renderNode(service, n) {
  const el = document.createElement("div");
  el.className = "node-wrap";
  const row = document.createElement("div");
  row.className = "node";
  const icon = n.kind === "container" ? "▸" : n.kind === "tabular" ? "▦" : "◇";
  row.innerHTML = `<span class="kind">${icon}</span><span class="nname">${esc(n.name || "(root)")}</span>`
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

function metaChip(meta) {
  if (!meta) return "";
  const bits = [];
  if (meta.size != null) bits.push(human(meta.size));
  if (meta.type) bits.push(esc(meta.type));
  if (meta.ttlSeconds != null && meta.ttlSeconds >= 0) bits.push("ttl " + meta.ttlSeconds + "s");
  return bits.length ? `<span class="nmeta">${bits.join(" · ")}</span>` : "";
}

// ---------- blob preview/edit ----------
async function openBlob(service, n) {
  state.reopen = () => openBlob(service, n);
  const content = document.getElementById("content");
  content.innerHTML = `<div class="meta">Loading ${esc(n.name)}…</div>`;
  const q = new URLSearchParams({ service, segs: JSON.stringify(n.path.segments) });
  let r;
  try { r = await api("/api/blob?" + q.toString()); }
  catch (e) { content.innerHTML = gate(service, e); return; }
  const truncated = r.headers.get("X-DataConsole-Truncated") === "true";
  const ctype = r.headers.get("X-DataConsole-ContentType") || "";
  const buf = await r.arrayBuffer();
  const size = buf.byteLength;
  const textual = isTextual(ctype);

  let html = `<div class="toolbar"><b>${esc(n.name)}</b>`
    + `<span class="meta">${human(size)}${truncated ? " · head slice (view-only)" : ""}${ctype ? " · " + esc(ctype) : ""}</span>`
    + `<span class="spacer"></span>`;
  // Affordances from edit-mode toggle AND service.actions + the blob's own state.
  const editable = editing() && actionEnabled(service, ACTION.writeBlob) && !truncated && textual && size <= EDIT_CAP;
  if (editable) html += `<button id="saveblob">Save</button>`;
  if (editing() && hasAction(service, ACTION.renameObject)) html += actionButton("renameblob", "Rename", "ghost", service, ACTION.renameObject);
  if (editing() && hasAction(service, ACTION.deleteNode)) html += actionButton("delblob", "Delete", "danger", service, ACTION.deleteNode);
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
  } else if (size > DISPLAY_CAP || !textual) {
    html += `<div class="placeholder">${!textual ? "Binary content" : "Large content"} — use Download.</div>`;
    content.innerHTML = html;
  } else if (editable) {
    html += `<textarea class="editor" id="blobedit"></textarea>`;
    content.innerHTML = html;
    document.getElementById("blobedit").value = new TextDecoder().decode(buf);
    document.getElementById("saveblob").onclick = () =>
      saveBlob(service, n, () => document.getElementById("blobedit").value);
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

async function saveBlob(service, n, getVal) {
  const data = b64(new TextEncoder().encode(getVal()));
  confirmAction("Overwrite " + n.name + "?", `PUT ${n.name}`, async () => {
    await api("/api/blob", {
      method: "PUT",
      headers: { "Content-Type": "application/json", "X-Confirm": "true" },
      body: JSON.stringify({ path: n.path, data }),
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

function addUploadBar(service, segs, container) {
  const bar = document.createElement("div");
  bar.className = "uploadbar";
  if (actionEnabled(service, ACTION.uploadObject)) {
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
  } else {
    bar.innerHTML = `<button class="ghost" disabled title="${esc(actionReason(service, ACTION.uploadObject))}">Upload file</button>`;
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

// ---------- tabular / KV-collection grid ----------
async function openTable(service, n) {
  state.reopen = () => openTable(service, n);
  const content = document.getElementById("content");
  content.innerHTML = `<div class="meta">Loading ${esc(n.name)}…</div>`;
  const q = new URLSearchParams({ service, segs: JSON.stringify(n.path.segments) });
  let tp;
  try { tp = await apiJSON("/api/table?" + q.toString()); }
  catch (e) { content.innerHTML = gate(service, e); return; }
  renderGrid(content, service, n, tp);
  maybeTTL(service, n, () => openTable(service, n));
}

function renderGrid(content, service, n, tp) {
  const cols = tp.columns || [];
  const keyCols = tp.rowKeyCols || [];
  const usesKVEntry = hasAction(service, ACTION.editKVEntry);
  const editAction = usesKVEntry ? ACTION.editKVEntry : ACTION.editCell;
  const deleteAction = usesKVEntry ? ACTION.editKVEntry : ACTION.deleteRow;
  const hasCellEdit = hasAction(service, editAction);
  const editable = editing() && hasCellEdit && keyCols.length > 0;
  const cellsInteractive = editable && actionEnabled(service, editAction);
  const showDelete = editing() && hasAction(service, deleteAction) && keyCols.length > 0;
  const canDelete = actionEnabled(service, deleteAction);

  let h = `<div class="toolbar"><b>${esc(n.name)}</b>`
    + `<span class="meta">${(tp.rows || []).length} rows${keyCols.length === 0 ? " · view-only (no key)" : ""}</span>`
    + `<span class="spacer"></span>`;
  if (editing() && hasAction(service, ACTION.insertRow)) h += actionButton("insertrow", "Insert row", "ghost", service, ACTION.insertRow);
  h += `</div><div class="gridwrap"><table class="grid"><thead><tr>`;
  for (const c of cols) h += `<th title="${esc(c.dataType || "")}">${esc(c.name)}${c.pk ? " 🔑" : ""}</th>`;
  if (showDelete) h += `<th></th>`;
  h += `</tr></thead><tbody id="gridbody"></tbody></table></div>`;
  content.innerHTML = h;
  const body = document.getElementById("gridbody");
  appendRows(body, service, n, tp, cols, keyCols, cellsInteractive, showDelete, canDelete, usesKVEntry, deleteAction);
  wireAction("insertrow", service, ACTION.insertRow, () => insertRow(service, n, cols));
  attachLoadMore(content, service, n, tp, cols, keyCols, cellsInteractive, showDelete, canDelete, usesKVEntry, deleteAction);
}

function appendRows(body, service, n, tp, cols, keyCols, cellsInteractive, showDelete, canDelete, usesKVEntry, deleteAction) {
  for (const row of (tp.rows || [])) {
    const tr = document.createElement("tr");
    cols.forEach((c, i) => {
      const td = document.createElement("td");
      td.textContent = fmt(row[i]);
      if (cellsInteractive) {
        td.className = "editable";
        td.onclick = () => editCell(service, n, cols, keyCols, row, c, i, td, usesKVEntry);
      }
      tr.appendChild(td);
    });
    if (showDelete) {
      const td = document.createElement("td");
      const del = document.createElement("button");
      del.className = "rowdel"; del.textContent = "✕";
      del.disabled = !canDelete;
      if (!canDelete) del.title = actionReason(service, deleteAction);
      if (canDelete) del.onclick = () => deleteRow(service, n, cols, keyCols, row, usesKVEntry);
      td.appendChild(del); tr.appendChild(td);
    }
    body.appendChild(tr);
  }
}

function attachLoadMore(content, service, n, tp, cols, keyCols, cellsInteractive, showDelete, canDelete, usesKVEntry, deleteAction) {
  const old = content.querySelector(".loadmore");
  if (old) old.remove();
  if (!tp.nextCursor) return;
  const more = document.createElement("button");
  more.className = "loadmore";
  more.textContent = "Load more rows…";
  more.onclick = async () => {
    const q = new URLSearchParams({ service, segs: JSON.stringify(n.path.segments), cursor: tp.nextCursor });
    try {
      const next = await apiJSON("/api/table?" + q.toString());
      appendRows(document.getElementById("gridbody"), service, n, next, cols, keyCols, cellsInteractive, showDelete, canDelete, usesKVEntry, deleteAction);
      attachLoadMore(content, service, n, next, cols, keyCols, cellsInteractive, showDelete, canDelete, usesKVEntry, deleteAction);
    } catch (e) { toast(e, true); }
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
    try {
      if (kv) {
        await api("/api/entry", { method: "PUT", headers: jsonConfirm(),
          body: JSON.stringify({ path: n.path, field: String(row[0]), value: b64(new TextEncoder().encode(nv)), score: keyColScore(cols, row) }) });
      } else {
        await api("/api/cell", { method: "POST", headers: jsonConfirm(),
          body: JSON.stringify({ path: n.path, rowKey: rowKeyOf(cols, keyCols, row), column: col.name, newValue: nv, expectedOld: oldVal }) });
      }
      row[idx] = nv; td.textContent = fmt(nv); toast("Saved.");
    } catch (e) { td.textContent = fmt(oldVal); toast("Save failed: " + errorSummary(e), true); }
  };
  input.onblur = commit;
  input.onkeydown = (ev) => { if (ev.key === "Enter") input.blur(); if (ev.key === "Escape") { td.textContent = fmt(oldVal); } };
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
    toast("Deleted."); openTable(service, n);
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
    await api("/api/row", { method: "POST", headers: jsonConfirm(), body: JSON.stringify({ path: n.path, row }) });
    toast("Inserted."); openTable(service, n);
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
  res.innerHTML = `<div class="meta">Running…</div>`;
  try {
    const tp = await apiJSON("/api/query", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ service, stmt }) });
    const cols = tp.columns || [];
    let h = `<div class="meta">${(tp.rows || []).length} rows${tp.nextCursor ? "" : (tp.rows || []).length >= 2000 ? " · capped at 2000" : ""}</div>`;
    h += `<div class="gridwrap"><table class="grid"><thead><tr>`;
    for (const c of cols) h += `<th>${esc(c.name)}</th>`;
    h += `</tr></thead><tbody>`;
    for (const row of (tp.rows || [])) {
      h += "<tr>";
      for (const v of row) h += `<td>${esc(fmt(v))}</td>`;
      h += "</tr>";
    }
    h += `</tbody></table></div>`;
    res.innerHTML = h;
  } catch (e) { res.innerHTML = errorHTML(e); }
}

// ---------- KV TTL control (wires /api/stat) ----------
async function maybeTTL(service, n, reopen) {
  if (!hasAction(service, ACTION.setTTL)) return;
  let node;
  try { node = await apiJSON("/api/stat?" + new URLSearchParams({ service, segs: JSON.stringify(n.path.segments) })); }
  catch (_) { return; }
  const ttl = node.meta ? node.meta.ttlSeconds : null;
  const cur = (ttl == null || ttl < 0) ? "no expiry" : ttl + "s";
  const bar = document.createElement("div");
  bar.className = "ttlbar";
  bar.innerHTML = `<span class="meta">TTL: ${esc(cur)}</span>`;
  if (editing()) {
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
function toast(m, bad) {
  const t = document.createElement("div");
  t.textContent = bad ? errorSummary(m) : m;
  t.className = "toast " + (bad ? "bad" : "good");
  document.body.appendChild(t);
  setTimeout(() => t.remove(), 2600);
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
  if (run) { try { await run(); } catch (e) { toast(e, true); } }
};
bootAuth();
