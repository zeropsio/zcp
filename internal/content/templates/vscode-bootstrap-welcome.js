const vscode = require("vscode");
const fs = require("fs");
const os = require("os");
const path = require("path");
const crypto = require("crypto");
const { spawn: defaultSpawn } = require("child_process");

// Zerops "Get Started" welcome panel — the webview host module for the
// zerops.welcome command. extension.js requires this file LAZILY, only
// inside the command handler (see its activate()), so this file's own top
// level must stay side-effect-free beyond plain declarations: simply
// requiring it is what the dark-load tests assert never happens before the
// command runs (docs/spec-welcome-mode.md §1, W-ENTRY / W3).
//
// Collaborators (agent registry, zembed reader, runAgentAction) arrive by
// dependency injection from extension.js's command handler — the
// safety-pinned launch commands stay in exactly one copy. extension.js's
// call site never changes to pass the test-only overrides below (homeDir,
// workspaceRoot, fs) — resolveDeps() fills them in here instead, so a test
// that needs them calls open() directly (see welcomejs/harness.js
// loadWelcome()).

// Placeholder substituted in welcome.html for the per-open crypto nonce
// (CSP script-src/style-src, §8 W-SEC). Every occurrence is replaced with
// the same nonce value — never a hardcoded/derived one.
const NONCE_PLACEHOLDER = "__CSP_NONCE__";

// Exact-match allowlist for the sole outbound host action P1 wires: opening
// an external URL. A webview-supplied url is checked against this SET —
// never used to build a URL, never pattern-matched (§8 W-SEC).
const EXTERNAL_URLS = new Set([
  "https://docs.zerops.io",
  "https://zerops.io", // TODO(welcome): real 5-min walkthrough URL
]);

// Duplicated from extension.js's own ZEMBED_DIR/ENV_FILE: welcome.js only
// needs the raw path to WATCH — the actual read+parse stays the injected,
// safety-owned deps.readZembedEnv() (one copy, in extension.js). Watching
// the wrong/stale path here would just mean no live updates, never a wrong
// read, so this duplication carries none of the risk the "one copy" rule
// (§1) guards against for the launch commands.
const ZEMBED_DIR = "/etc/zerops-zembed";
const ZEMBED_ENV_FILE = path.join(ZEMBED_DIR, "env.json");

// Credential probes exist only for agents with a live-verified artifact path
// (spec §3, v1: claude-code, codex). Every other agent (antigravity, grok,
// cursor) has no verifiable local signal — computeAgentState renders those
// from the platform flag alone.
const CRED_PROBE = {
  "claude-code": path.join(".claude", ".credentials.json"),
  "codex": path.join(".codex", "auth.json"),
};

// The directory each cred probe's file lives under — what the watchers
// below attach to (a dir-level watch survives the file itself not existing
// yet, and catches atomic-replace writes the file-level probe would miss
// between polls).
const CRED_WATCH_DIR = {
  "claude-code": ".claude",
  "codex": ".codex",
};

// The ONE sanctioned .zcp/state read (docs/spec-guided-mode.md §2): presence
// of this file, never its contents.
const GUIDED_MARKER_REL = path.join(".zcp", "state", "guided");

// Shared debounce for every watcher below — a single filesystem write can
// emit more than one fs.watch event (spec §3).
const STATE_PUSH_DEBOUNCE_MS = 400;

// ---- agent authorization (docs/spec-welcome-mode.md §4, W-AUTH) ----------

// Channel name + version the embedding Zerops GUI's receiver validates
// (frontend-legacy prototype/zcp-claude-auth-bridge, treated as fixed —
// see the P3 brief's "Frontend receiver contract"). Also duplicated
// (deliberately, like ZEMBED_DIR above) in welcome.html's inline script: the
// webview executes in a separate JS context with no require() boundary to
// this file, so it carries its own literal copy for its dumb-pipe relay
// filter. This copy is host-side and authoritative; the webview's is
// defense-in-depth only — this file re-validates every relayed message
// (§4, §8 W-SEC: "Host validates relayed messages again").
const BRIDGE_CHANNEL = "@zerops/zcp-agent-auth-bridge";
const BRIDGE_VERSION = 1;

// isAllowedGuiOrigin reports whether a received message's origin is TRUSTED
// for validating an INBOUND bridge ack: prod (app.zerops.io, exact host,
// default port only), a real dot-boundary subdomain of the Zerops-internal
// stage/dev domain (*.zerops.dev, default port only), a local dev server
// (http://localhost, any port — matches nginx's frame-ancestors
// "http://localhost:*"), or an exact origin the container operator opted in
// via ZCP_WELCOME_BRIDGE_ORIGINS (see resolveBridgeExtraOrigins below).
//
// Deliberately does NOT trust *.zerops.app by pattern. *.zerops.app is the
// shared CUSTOMER namespace — every Zerops service gets a public
// *.zerops.app URL, and the code-server's CSP frame-ancestors
// (internal/content/templates/nginx.conf.tmpl) lets ANY *.zerops.app page
// embed a victim's code-server. Trusting the suffix would let a malicious
// evil-….zerops.app page embed an authenticated code-server, receive the
// broadcast bridge trigger (incl. eventId), and forge an accepted:true ack —
// a trusted-response forgery that also suppresses the terminal fallback for
// the 10-minute flow cap. A specific *.zerops.app test/custom GUI is trusted
// ONLY when the operator names its exact origin via the env var above —
// never by suffix.
//
// PARSES the origin and checks scheme + exact host/port, or a real
// dot-boundary suffix — never a substring test, which is bypassable (e.g.
// "https://zerops.app.attacker.com"). Used to validate INBOUND acks; the
// OUTBOUND trigger stays broadcast (target "*", see sendBridgeMessage) since
// the webview can't read the cross-origin parent's origin and the trigger
// carries no secret — the frontend receiver is the confidentiality gate
// there (spec-welcome-mode.md §4 W-AUTH). This function is host-side and the
// SOLE origin authority: welcome.html's webview cannot see
// ZCP_WELCOME_BRIDGE_ORIGINS, so it no longer makes this decision — it only
// relays by channel, forwarding the browser-supplied origin for this
// function to judge.
function isAllowedGuiOrigin(origin, extraOrigins) {
  // Operator-configured exact origins (ZCP_WELCOME_BRIDGE_ORIGINS) — the only
  // way a *.zerops.app test/custom GUI is trusted: by exact origin, never by
  // the shared customer namespace's suffix.
  if (extraOrigins && extraOrigins.indexOf(origin) !== -1) return true;
  let u;
  try { u = new URL(origin); } catch (_) { return false; }
  const h = u.hostname;
  if (u.protocol === "https:") {
    if (u.port !== "") return false; // exact origin: default 443 only
    if (h === "app.zerops.io") return true;
    // real subdomain of the Zerops-exclusive stage/dev domain (non-empty
    // label before ".zerops.dev" — rejects a bare-dot host); NOT *.zerops.app
    // (customer namespace, see comment above).
    return h.length > ".zerops.dev".length && h.endsWith(".zerops.dev");
  }
  if (u.protocol === "http:") {
    return h === "localhost"; // any port, local dev (matches nginx frame-ancestors)
  }
  return false;
}

// resolveBridgeExtraOrigins reads ZCP_WELCOME_BRIDGE_ORIGINS — a comma-
// separated list of exact origins the container operator additionally trusts
// for inbound bridge acks (see isAllowedGuiOrigin above) — from the LIVE
// zembed store ONLY, never the extension host's frozen process.env: a running
// host froze process.env at code-server boot, so a value there would keep
// trusting an origin the operator has since removed from the live store (a
// stale-trust window). A readable store is authoritative (a missing/empty key
// means no extras); an unreadable store (readZembedEnv returns null) fails
// closed to no extras. This is the ONLY way a *.zerops.app test/custom GUI is
// trusted: the operator names its exact origin here, never by pattern.
//
// Resolved FRESH at every ack (handleBridgeWindowMessage), never cached: an
// operator adding OR revoking a trusted origin takes effect immediately,
// without reopening the panel. Each entry is canonicalized through
// new URL().origin so a non-canonical env value (trailing slash, explicit
// :443, uppercase host) still matches the browser-canonical event.origin;
// unparseable or opaque ("null") entries are dropped.
function resolveBridgeExtraOrigins(deps) {
  const env = deps.readZembedEnv();
  const raw = env ? env.ZCP_WELCOME_BRIDGE_ORIGINS : undefined;
  if (typeof raw !== "string" || raw === "") return [];
  const out = [];
  for (const entry of raw.split(",")) {
    const s = entry.trim();
    if (s === "") continue;
    let o;
    try { o = new URL(s).origin; } catch (_) { continue; }
    if (o && o !== "null") out.push(o);
  }
  return out;
}

// Bridge support matrix v1 (spec §4): only claude-code has a receiver on
// the other end. Every other agent's authorize click is rejected with
// phase "unsupported" — its tile keeps the "authorize in the Zerops panel"
// hint instead (welcome.html).
const BRIDGE_SUPPORTED_AGENTS = new Set(["claude-code"]);

// Tier-A terminal fallback v1 (spec §4): login commands taken VERBATIM from
// the frontend registry. An agent absent here has no terminal path — its
// authorize-terminal click is rejected with phase "unsupported".
const LOGIN_COMMANDS = {
  "claude-code": "claude /login",
  "codex": "codex login --device-auth",
};

const ACK_TIMEOUT_MS = 3000; // spec §4: how long we wait for the GUI's open-agent-auth-ack
const AUTH_FLOW_CAP_MS = 10 * 60 * 1000; // spec §4: releases a stuck bridge or terminal flow after 10 minutes

// Defense-in-depth size cap on a relayed bridge message's data (spec §8
// W-SEC "size-capped") — generous for {channel,version,type,eventId,
// accepted,reason} but rejects a pathological payload before it is ever
// interpreted. Duplicated in welcome.html for the webview's own pre-filter;
// this copy is the one that actually matters (§8: "Host validates relayed
// messages again").
const BRIDGE_RELAY_MAX_BYTES = 1024;

// ---- curated skills catalog (docs/spec-welcome-mode.md §6, W-SKILLS) -----

// SKILLS is the shipped allowlist for the "Add skills" step: the ONLY slugs
// a {type:"skill-add"} click may install (spec §6: "slug must be in the
// shipped allowlist, never a path from the webview"). Each slug's SKILL.md
// ships embedded in the binary (internal/content/templates/welcome-skills/
// <slug>/SKILL.md) and is materialized into this extension's own versioned
// dir at install (internal/init/adapters/claude.go). title/blurb here are
// DUPLICATED display copy from that content's front-matter (name/
// description) — same reason BRIDGE_CHANNEL is duplicated in welcome.html
// above: the webview has no require() into this file or the content
// package. TestWelcomeSkillsAllowlistMatchesEmbedded
// (internal/content) pins that this list and the embedded slugs never drift.
const SKILLS = [
  { slug: "tdd-red-green", title: "TDD: red → green", blurb: "Drive every behavior change through a failing test first — red, green, then refactor with the tests as a safety net." },
  { slug: "plan-before-code", title: "Plan before code", blurb: "Restate the problem, surface invariants and edge cases, and cut the work into thin verifiable slices before writing any code." },
  { slug: "debug-scientifically", title: "Debug scientifically", blurb: "Debug with hypotheses and cheap experiments instead of shotgun edits — find the root cause, prove it, then fix it with a regression test." },
  { slug: "review-before-done", title: "Review before done", blurb: "Before claiming any task done, re-read the full diff, run everything, hunt orphans, and verify each claim you are about to make." },
  { slug: "ship-small", title: "Ship small", blurb: "Ship the smallest change that delivers value, keep the tree releasable at every step, and let working software drive the next decision." },
];
const SKILL_SLUGS = new Set(SKILLS.map((s) => s.slug));

// "guided" is RESERVED (spec §6): owned by `zcp init --guided`, which writes
// .claude/skills/guided directly (internal/content/guided.go). It must never
// be installable through this generic flow — SKILLS above never lists it (a
// welcomejs test pins that), and handleSkillAdd rejects it explicitly below
// as defense in depth even if that were ever to change.
const RESERVED_SKILL_SLUG = "guided";

let panel = null; // singleton — re-invoking open() reveals this, never recreates it
let disposables = []; // welcome-panel-scoped disposables (watchers, view-state listener): cleared on dispose
let pushTimer = null; // shared debounce timer for schedulePush()

// At most one authorization flow in flight per panel (spec §4), bridge OR
// terminal — module-level like `panel` above, safe for the same reason
// (welcomejs/harness.js gives every test its own uncached module instance).
// Shape: { kind: "bridge", agentId, eventId, ackTimer, capTimer } |
//        { kind: "terminal", agentId, terminal, capTimer, closeDisposable }
let authFlow = null;

// At most one guided toggle in flight per panel (spec §5) — a SEPARATE lock
// from authFlow above: an agent authorization and a guided toggle may run
// concurrently, they just don't share a slot. Cleared ONLY by the spawned
// child's own exit/error handler (finishGuidedToggle) — never by a panel
// dispose, since the child keeps running regardless of whether anything is
// left to show its result to (see disposeWatchers's comment on this).
let guidedFlow = null; // { enable, child? } while a `zcp init [--guided]` run is in progress

// selectedGuidedRoot is the workspace folder the guided toggle last actually
// operated on (spec §3: "guided = presence of the marker in the SELECTED
// workspace folder") — set once selectGuidedFolder resolves a folder,
// whether that's the sole folder or the multi-root quickpick's pick.
// deps.workspaceRoot is fixed to the FIRST folder for the life of the panel
// (resolveDeps), so in a multi-root workspace it can name a DIFFERENT
// folder than the one guided was just toggled in; collectFullState below
// prefers this field once it's set. Deliberately sticky across a panel
// dispose+reopen, like lastBridgeOutcome above: it names "the" guided-
// relevant folder for this window, not in-flight state.
let selectedGuidedRoot = null;

// guidedMarkerWatcher/guidedMarkerWatcherRoot track which folder's marker
// the panel-scoped watcher (see reattachGuidedMarkerWatcher, watchers
// section below) currently points at, so a guided toggle against a NEW
// folder can re-point it live instead of leaving it watching the stale one.
let guidedMarkerWatcher = null;
let guidedMarkerWatcherRoot; // undefined = "never attached yet" — distinct from a real null (no folder)

// lastBridgeOutcome mirrors the BRIDGE flow's own phase transitions ONLY
// (never the Tier-A terminal flow's) — a diagnostics-tile signal for "what
// happened last time this container attempted the bridge", module-level
// like panel/authFlow above (see harness.js's per-test uncached-module
// note). Deliberately survives a panel dispose+reopen (unlike authFlow,
// which is released on dispose): it is a debugging aid about the LAST
// attempt, not live in-flight state.
let lastBridgeOutcome = "-";

// Streams every guided toggle run's stdout/stderr — created ONCE, lazily,
// inside open()'s panel-creation branch (never on a reveal or a dispose+
// reopen) and left in ctx.subscriptions rather than the panel-scoped
// `disposables` above: closing the welcome panel mid-run must not lose
// where the output went (spec §5).
let guidedOutputChannel = null;

const GUIDED_NO_WORKSPACE_MESSAGE = "No workspace folder open — open a folder first.";
const GUIDED_AUTHORING_MESSAGE = "Guided is user-only; authoring mode is active.";
const GUIDED_BUSY_MESSAGE = "A guided toggle is already running.";
const GUIDED_DIRTY_MESSAGE = "Save AGENTS.md/CLAUDE.md first — zcp init rewrites them.";
const GUIDED_ENOENT_MESSAGE = "zcp binary not found in PATH.";
const GUIDED_MARKER_MISMATCH_MESSAGE = "zcp init finished but the guided marker doesn't match — check the Zerops Welcome output.";
// zcp init is non-transactional — the marker is written before the init
// steps run (docs/spec-guided-mode.md §3) — so a part-way failure can leave
// the marker flipped while other surfaces are stale. Report that honestly:
// never a silent success, never a claimed rollback (spec §5).
const GUIDED_PARTIAL_FAILURE_MESSAGE = "zcp init failed part-way — the preference may be recorded but surfaces are partially refreshed. Re-run from the toggle or run zcp init in a terminal (see output).";

// ---- pure state (docs/spec-welcome-mode.md §3, W-STATE / W4) -------------

// computeAgentState implements the §3 matrix EXACTLY: the platform flag and
// the local credential artifact are two INDEPENDENT inputs that compose a
// matrix, never a boolean union. authType (ZCP_AGENT_AUTH_TYPE_<SUFFIX>) is
// accepted because the collector reads it for free alongside
// flagOAuth/flagToken, but every matrix row is already fully determined by
// the other four fields, so it never changes the result.
function computeAgentState({ flagOAuth, flagToken, authType, credPresent, credVerifiable }) {
  if (flagToken) return "authorized-token"; // token env present -> authorized-token, regardless of cred
  if (flagOAuth) {
    if (credPresent) return "authorized";
    if (credVerifiable) return "reconnect"; // flag present, verified probe finds no local cred: rebuild-orphaned flag
    // Agents without a verified probe render from the platform flag alone
    // (spec §3) — there is no local signal to contradict it with.
    return "authorized";
  }
  return credPresent ? "local-only" : "not-authorized";
}

// shortenServiceId renders a service id for the diagnostics tile: enough to
// recognize/compare across a screenshot or a support conversation, never the
// full value (spec §8 W-SEC: no env values/paths beyond what the user
// needs). No serviceId key in the zembed store -> "-", the same sentinel
// every other diagnostics field uses when it has nothing.
function shortenServiceId(id) {
  if (!id) return "-";
  return id.length > 8 ? id.slice(0, 8) + "…" : id;
}

// buildState assembles the full webview payload from already-collected
// inputs — pure, no I/O of its own (collectFullState below does the
// reading). anyAuthorized gates steps 2+ (§7 W-CTA): only "authorized" and
// "authorized-token" unlock it — "local-only" is platform-unverified and
// must NOT unlock.
function buildState(inputs) {
  const {
    registry = {}, agentIds = [], zembedEnv: env = null, creds = {},
    guided = { state: "unknown" }, skills = [],
    extensionVersion = "-", lastBridgeOutcome = "-",
  } = inputs || {};

  const agents = agentIds.map((id) => {
    const reg = registry[id] || {};
    const suffix = reg.suffix || "";
    const flagOAuth = !!env && env["ZCP_AGENT_OAUTH_" + suffix] === "true";
    const flagToken = !!env && !!env["ZCP_AGENT_TOKEN_" + suffix];
    const authType = env ? env["ZCP_AGENT_AUTH_TYPE_" + suffix] : undefined;
    const cred = creds[id] || { present: false, verifiable: false };
    const state = computeAgentState({
      flagOAuth, flagToken, authType,
      credPresent: cred.present, credVerifiable: cred.verifiable,
    });
    return { id, label: reg.label || id, state, probeVerified: cred.verifiable };
  });

  const zembedSeen = !!env;

  return {
    agents,
    anyAuthorized: agents.some((a) => a.state === "authorized" || a.state === "authorized-token"),
    guided,
    skills,
    bridge: { status: "unknown" }, // P3 fills
    environment: { zembed: zembedSeen },
    // Small muted diagnostics tile (welcome.html): container/runtime signal
    // ONLY, never env values/tokens/paths beyond the two fields below that
    // are deliberately truncated (spec §8 W-SEC).
    diagnostics: {
      zembedSeen,
      extensionVersion,
      serviceId: shortenServiceId(env && typeof env.serviceId === "string" ? env.serviceId : null),
      lastBridgeOutcome,
    },
  };
}

// ---- deps resolution -------------------------------------------------

// resolveDeps fills in every optional collaborator extension.js's fixed call
// site never passes. workspaceRoot uses !== undefined (not ||) so a test can
// explicitly assert "no workspace folder" with workspaceRoot: null, distinct
// from "unspecified, use the real default".
function resolveDeps(deps) {
  const d = deps || {};
  return {
    REGISTRY: d.REGISTRY || {},
    ALL_AGENT_IDS: d.ALL_AGENT_IDS || [],
    readZembedEnv: d.readZembedEnv || (() => null),
    runAgentAction: d.runAgentAction || (() => {}),
    fs: d.fs || fs,
    homeDir: d.homeDir || os.homedir(),
    workspaceRoot: d.workspaceRoot !== undefined ? d.workspaceRoot : defaultWorkspaceRoot(),
    // Every open workspace folder's fsPath, resolved once like workspaceRoot
    // above (folders don't change without a window reload) — the guided
    // toggle's own folder-selection seam (single vs quickpick, spec §5),
    // kept separate from workspaceRoot since that one's only consumer
    // (collectGuided/watchGuidedMarker) is deliberately single-folder.
    workspaceFolders: d.workspaceFolders !== undefined ? d.workspaceFolders : defaultWorkspaceFolders(),
    // Read FRESH at use time (a function, like readZembedEnv), never
    // resolved once: unlike workspaceFolders, which text document is dirty
    // can change at any moment while the panel sits open.
    textDocuments: d.textDocuments || defaultTextDocuments,
    // Timer + spawn seams for the auth flow below (ACK/cap timers, mark-oauth
    // invocation) — real by default, swappable in tests for a fake clock /
    // recorded child-process calls (welcomejs/harness.js makeFakeTimers()).
    setTimeout: d.setTimeout || ((fn, ms) => setTimeout(fn, ms)),
    clearTimeout: d.clearTimeout || ((id) => clearTimeout(id)),
    spawn: d.spawn || defaultSpawn,
    // Multi-root folder picker for the guided toggle (spec §5) — injectable
    // so tests control which folder gets "picked" without a real UI.
    showQuickPick: d.showQuickPick || ((items, options) => vscode.window.showQuickPick(items, options)),
    // Workspace-trust gate for the skills install flow (spec §6: "Untrusted
    // ... contexts refuse writes"). Tri-state like the real API
    // (true/false/undefined) — only a LITERAL false rejects, so an older
    // host (or this stub) that never sets it degrades to "trusted".
    isTrusted: d.isTrusted !== undefined ? d.isTrusted : defaultIsTrusted(),
    // Modal confirmation before replacing a locally-modified skill (spec
    // §6) — same injectable-for-tests treatment as showQuickPick above.
    showWarningMessage: d.showWarningMessage || ((message, options, ...items) => vscode.window.showWarningMessage(message, options, ...items)),
    // Clipboard-first CTA kickoff (spec §7 W-CTA) — the ONLY mechanism
    // handleStartOnboarding may use to hand a kickoff prompt to the agent:
    // NEVER terminal.sendText, NEVER a delayed setTimeout injection (a
    // terminal may not even be running the agent). Injectable for tests,
    // real vscode.env.clipboard by default.
    clipboard: d.clipboard || { writeText: (text) => vscode.env.clipboard.writeText(text) },
    // Post-copy nudge alongside the clipboard write above — same
    // injectable-for-tests treatment as showQuickPick/showWarningMessage.
    showInformationMessage: d.showInformationMessage || ((message, ...items) => vscode.window.showInformationMessage(message, ...items)),
  };
}

function defaultWorkspaceRoot() {
  try {
    const folders = vscode.workspace && vscode.workspace.workspaceFolders;
    if (folders && folders.length > 0) return folders[0].uri.fsPath;
  } catch (_) {}
  return null;
}

function defaultWorkspaceFolders() {
  try {
    const folders = vscode.workspace && vscode.workspace.workspaceFolders;
    if (folders && folders.length > 0) return folders.map((f) => f.uri.fsPath);
  } catch (_) {}
  return [];
}

function defaultTextDocuments() {
  try {
    return (vscode.workspace && vscode.workspace.textDocuments) || [];
  } catch (_) {
    return [];
  }
}

function defaultIsTrusted() {
  try {
    return vscode.workspace && vscode.workspace.isTrusted;
  } catch (_) {
    return undefined;
  }
}

// ---- effectful input collectors -------------------------------------
// Each collector is null/missing-tolerant: a container without zembed, a
// fresh HOME with no agent ever logged in, or a window with no workspace
// folder open all degrade to a safe default, never throw.

// collectCred existence-checks ONE agent's credential artifact — never reads
// its contents (spec §3/§8: presence-only, no credential value ever enters
// this process' view of the world).
function collectCred(fsImpl, homeDir, agentId) {
  const rel = CRED_PROBE[agentId];
  if (!rel) return { present: false, verifiable: false };
  let present = false;
  try { present = fsImpl.existsSync(path.join(homeDir, rel)); } catch (_) { present = false; }
  return { present, verifiable: true };
}

function collectAllCreds(fsImpl, homeDir, agentIds) {
  const out = {};
  for (const id of agentIds) out[id] = collectCred(fsImpl, homeDir, id);
  return out;
}

// collectGuided presence-checks the ONE sanctioned .zcp/state read (spec §3,
// docs/spec-guided-mode.md §2) — never opens or parses the marker file. No
// workspace folder selected -> "unknown" (undecidable without a folder).
function collectGuided(fsImpl, workspaceRoot) {
  if (!workspaceRoot) return { state: "unknown" };
  let present = false;
  try { present = fsImpl.existsSync(path.join(workspaceRoot, GUIDED_MARKER_REL)); } catch (_) { present = false; }
  return { state: present ? "enabled" : "disabled" };
}

// skillDestPath is where an installed curated skill lives in the workspace.
function skillDestPath(workspaceRoot, slug) {
  return path.join(workspaceRoot, ".claude", "skills", slug, "SKILL.md");
}

// shippedSkillPath is where this extension's own versioned install carries
// the curated skill's shipped bytes (internal/init/adapters/claude.go's
// installWelcomeSkills).
function shippedSkillPath(extensionPath, slug) {
  return path.join(extensionPath, "welcome-skills", slug, "SKILL.md");
}

// readIfPresent returns a file's bytes, or null if it GENUINELY doesn't
// exist (including an existsSync-then-readFileSync race where the file is
// removed in between) — mirroring collectCred/collectGuided's
// tolerate-missing-path style above. Unlike those, this one does NOT
// swallow every error: a read failure on a file that DOES exist (EACCES, a
// permissions change, ...) is re-thrown, since callers that decide whether
// to WRITE (handleSkillAdd) must be able to tell "absent" apart from
// "present but unreadable" (spec §6/W7: no silent overwrite — treating an
// unreadable existing file as absent would install-fresh over it with zero
// confirmation). Read-only DISPLAY callers that must never throw use
// readIfPresentTolerant below instead.
function readIfPresent(fsImpl, p) {
  try {
    if (!fsImpl.existsSync(p)) return null;
    return fsImpl.readFileSync(p);
  } catch (err) {
    if (err && err.code === "ENOENT") return null; // raced away between the two calls above
    throw err;
  }
}

// readIfPresentTolerant is readIfPresent's read-only-display sibling: a
// state collector (collectSkillsState below) must never throw — degrading
// an unreadable file to "absent" here is a display-only inaccuracy, not a
// mutation risk, so it stays tolerant. handleSkillAdd must NOT use this: it
// needs readIfPresent's stricter distinction to satisfy W7 above.
function readIfPresentTolerant(fsImpl, p) {
  try {
    return readIfPresent(fsImpl, p);
  } catch (_) {
    return null;
  }
}

function hashBytes(data) {
  return crypto.createHash("sha256").update(data).digest("hex");
}

// collectSkillsState scans .claude/skills/<slug>/SKILL.md for every curated
// skill and classifies it against the shipped-content hash (spec §3/§6):
// absent (no file), installed-current (byte-identical to shipped),
// installed-modified (present but edited locally since install). No
// workspace folder open means nothing could ever have been installed, so it
// reports an empty list rather than five "absent" rows.
function collectSkillsState(deps) {
  if (!deps.workspaceRoot) return [];
  return SKILLS.map((s) => {
    const existing = readIfPresentTolerant(deps.fs, skillDestPath(deps.workspaceRoot, s.slug));
    if (existing === null) return { slug: s.slug, state: "absent" };
    const shipped = readIfPresentTolerant(deps.fs, shippedSkillPath(deps.extensionPath, s.slug));
    const current = shipped !== null && hashBytes(existing) === hashBytes(shipped);
    return { slug: s.slug, state: current ? "installed-current" : "installed-modified" };
  });
}

// Cached module-level like panel/authFlow above (see harness.js's per-test
// uncached-module note): the installed extension dir's OWN package.json
// version cannot change without an upgrade + window reload, which tears
// down this whole module instance anyway — re-reading it on every state
// push (every watcher tick) would be pure waste.
let cachedExtensionVersion = null;

// readExtensionVersion mirrors readWelcomeHtml's use of the REAL fs module,
// never deps.fs: ctx.extensionPath/package.json is a real, always-present
// sibling of welcome.html in the installed extension dir, regardless of
// what a test fakes deps.fs to be (see skills_install.test.js's comment on
// readWelcomeHtml for the same reasoning).
function readExtensionVersion(extensionPath) {
  if (cachedExtensionVersion !== null) return cachedExtensionVersion;
  try {
    const pkg = JSON.parse(fs.readFileSync(path.join(extensionPath, "package.json"), "utf8"));
    cachedExtensionVersion = typeof pkg.version === "string" && pkg.version ? pkg.version : "-";
  } catch (err) {
    console.warn("[zcp-welcome] reading extension package.json failed:", err);
    cachedExtensionVersion = "-";
  }
  return cachedExtensionVersion;
}

function collectFullState(deps) {
  const env = deps.readZembedEnv();
  return buildState({
    registry: deps.REGISTRY,
    agentIds: deps.ALL_AGENT_IDS,
    zembedEnv: env,
    creds: collectAllCreds(deps.fs, deps.homeDir, deps.ALL_AGENT_IDS),
    // selectedGuidedRoot (the folder a guided toggle actually ran against)
    // takes priority over deps.workspaceRoot's fixed first-folder default —
    // see its own doc-comment above (Finding 4 / spec §3 "selected
    // workspace folder").
    guided: collectGuided(deps.fs, selectedGuidedRoot || deps.workspaceRoot),
    skills: collectSkillsState(deps),
    extensionVersion: readExtensionVersion(deps.extensionPath),
    lastBridgeOutcome,
  });
}

function readWelcomeHtml(ctx, nonce) {
  const htmlPath = path.join(ctx.extensionPath, "welcome.html");
  const raw = fs.readFileSync(htmlPath, "utf8");
  return raw.split(NONCE_PLACEHOLDER).join(nonce);
}

function postState(deps) {
  if (!panel) return;
  const state = collectFullState(deps);
  // Before pushing state, close out any in-flight auth flow whose
  // completion signal this same recompute just observed — so the client
  // sees the idle transition alongside (never strictly after) the state
  // that motivated it. See reconcileAuthFlow.
  reconcileAuthFlow(deps, state);
  try {
    panel.webview.postMessage({ type: "state", payload: state });
  } catch (err) {
    console.error("[zcp-welcome] postMessage failed:", err);
  }
}

// unrefTimer mirrors unrefWatcher (see below) for setTimeout handles: a real
// Node Timeout supports .unref() so it can't block process exit; test-
// injected fake timer ids generally don't, so the check is required, not
// defensive noise.
function unrefTimer(t) {
  if (t && typeof t.unref === "function") t.unref();
}

// postAuth pushes one {type:"auth"} phase message for one agent — the
// per-tile progress line welcome.html renders from (busy / dialog-opening /
// no-dashboard / unsupported / idle, spec §4).
function postAuth(agentId, phase) {
  if (!panel) return;
  try {
    panel.webview.postMessage({ type: "auth", agentId, phase });
  } catch (err) {
    console.error("[zcp-welcome] postMessage failed:", err);
  }
}

// postBridgeAuth is postAuth's bridge-flow-specific sibling: every call site
// that reports a BRIDGE (never Tier-A terminal) flow's phase to the webview
// goes through this one function instead of postAuth directly, so
// lastBridgeOutcome (the diagnostics tile's signal, above) can never drift
// out of sync with what postAuth actually sent.
function postBridgeAuth(agentId, phase) {
  lastBridgeOutcome = phase;
  postAuth(agentId, phase);
}

// releaseAuthFlow clears whichever flow is in flight (bridge or terminal):
// cancels its timers and, for a terminal flow, disposes the
// onDidCloseTerminal listener registered for it. Never posts anything
// itself — every call site decides what (if anything) to tell the UI, since
// the panel-dispose cleanup path (see open()) needs silence.
function releaseAuthFlow(deps) {
  if (!authFlow) return;
  if (authFlow.ackTimer) deps.clearTimeout(authFlow.ackTimer);
  if (authFlow.capTimer) deps.clearTimeout(authFlow.capTimer);
  if (authFlow.closeDisposable) {
    try { authFlow.closeDisposable.dispose(); } catch (_) {}
  }
  authFlow = null;
}

// reconcileAuthFlow runs on every state recompute (postState, above) and
// closes an in-flight flow once its completion signal has arrived:
//   - bridge: the zembed watcher flips the agent's computed state to
//     authorized/authorized-token (the platform flag landed, spec §4).
//   - terminal: the agent's credential ARTIFACT appears (spec §4) — the
//     platform flag is not the signal here, since mark-oauth below is what
//     eventually sets it; waiting on it would deadlock the flow.
// A 10-minute cap (started when each flow either reaches dialog-opening or
// is created, see handleAuthorize/handleAuthorizeTerminal) is the other,
// timer-driven release path for both kinds.
function reconcileAuthFlow(deps, state) {
  if (!authFlow) return;
  if (authFlow.kind === "bridge") {
    const agent = state.agents.find((a) => a.id === authFlow.agentId);
    if (agent && (agent.state === "authorized" || agent.state === "authorized-token")) {
      const agentId = authFlow.agentId;
      releaseAuthFlow(deps);
      postBridgeAuth(agentId, "idle");
    }
    return;
  }
  const cred = collectCred(deps.fs, deps.homeDir, authFlow.agentId);
  if (cred.present) {
    const agentId = authFlow.agentId;
    runMarkOAuth(deps, agentId);
    releaseAuthFlow(deps);
    postAuth(agentId, "idle");
  }
}

// runMarkOAuth invokes `zcp agent mark-oauth <agentId>` once a Tier-A
// terminal login's credential artifact appears (spec §4): reconciles the
// platform flag, the sidebar launcher (env-only), and the Zerops GUI with
// local reality. Fire-and-forget — a failure degrades to the existing
// "Locally logged in — platform sync pending" (local-only) state and must
// NEVER block or throw (spec §4).
function runMarkOAuth(deps, agentId) {
  let child;
  try {
    child = deps.spawn("zcp", ["agent", "mark-oauth", agentId], { shell: false });
  } catch (err) {
    console.warn("[zcp-welcome] zcp agent mark-oauth " + agentId + " failed to start:", err);
    return;
  }
  if (!child || typeof child.on !== "function") return; // a minimal test stub with nothing to observe
  child.on("error", (err) => {
    console.warn("[zcp-welcome] zcp agent mark-oauth " + agentId + " failed:", err);
  });
  child.on("exit", (code) => {
    if (code !== 0) console.warn("[zcp-welcome] zcp agent mark-oauth " + agentId + " exited with code " + code);
  });
}

// sendBridgeMessage instructs the webview to relay `payload` to the
// embedding GUI via window.top.postMessage — see welcome.html's
// "bridge-send" handler. Broadcasts (target "*"): the webview cannot read
// the cross-origin parent's real origin to target it precisely, and
// `payload` carries no secret (§4) — the frontend receiver is the actual
// security gate, validating that the message came from the exact embedded
// iframe. All protocol logic (what to send, when) lives here; the webview
// is a dumb pipe.
function sendBridgeMessage(payload) {
  if (!panel) return;
  try {
    panel.webview.postMessage({ type: "bridge-send", payload, target: "*" });
  } catch (err) {
    console.error("[zcp-welcome] postMessage failed:", err);
  }
}

// handleAuthorize starts (or rejects) the bridge flow for a webview
// {type:"authorize", agentId} click (spec §4). agentId is already known to
// be one of the five launcher-registry ids — handleMessage's allowlist gate
// checked that; a value outside BRIDGE_SUPPORTED_AGENTS (v1: only
// claude-code) is a legitimate "not supported by this mechanism" case,
// answered with phase "unsupported", not a silent drop.
function handleAuthorize(agentId, deps) {
  if (!BRIDGE_SUPPORTED_AGENTS.has(agentId)) {
    postBridgeAuth(agentId, "unsupported");
    return;
  }
  if (authFlow) {
    postBridgeAuth(agentId, "busy");
    return;
  }
  const eventId = crypto.randomUUID();
  const payload = {
    channel: BRIDGE_CHANNEL,
    version: BRIDGE_VERSION,
    type: "open-agent-auth",
    agentType: agentId,
    eventId,
    createdAt: Date.now(),
  };
  authFlow = { kind: "bridge", agentId, eventId, ackTimer: null, capTimer: null };
  const ackTimer = deps.setTimeout(() => {
    if (!authFlow || authFlow.kind !== "bridge" || authFlow.eventId !== eventId) return;
    releaseAuthFlow(deps);
    postBridgeAuth(agentId, "no-dashboard");
  }, ACK_TIMEOUT_MS);
  unrefTimer(ackTimer);
  authFlow.ackTimer = ackTimer;
  sendBridgeMessage(payload);
}

// handleAuthorizeTerminal starts (or rejects) the Tier-A terminal flow for a
// webview {type:"authorize-terminal", agentId} click (spec §4). Shares the
// single `authFlow` slot with the bridge flow above — one authorization in
// flight per panel, of either kind.
function handleAuthorizeTerminal(agentId, deps) {
  const cmd = LOGIN_COMMANDS[agentId];
  if (!cmd) {
    postAuth(agentId, "unsupported");
    return;
  }
  if (authFlow) {
    postAuth(agentId, "busy");
    return;
  }
  const reg = deps.REGISTRY[agentId] || {};
  const label = reg.label || agentId;
  let terminal;
  try {
    terminal = vscode.window.createTerminal({ name: "Zerops: " + label + " login" });
  } catch (err) {
    console.error("[zcp-welcome] createTerminal failed:", err);
    return;
  }
  terminal.sendText(cmd, true);
  terminal.show();

  authFlow = { kind: "terminal", agentId, terminal, capTimer: null, closeDisposable: null };

  const capTimer = deps.setTimeout(() => {
    if (!authFlow || authFlow.kind !== "terminal" || authFlow.agentId !== agentId) return;
    releaseAuthFlow(deps);
    postAuth(agentId, "idle");
  }, AUTH_FLOW_CAP_MS);
  unrefTimer(capTimer);
  authFlow.capTimer = capTimer;

  authFlow.closeDisposable = vscode.window.onDidCloseTerminal((closed) => {
    if (closed !== terminal) return;
    if (!authFlow || authFlow.kind !== "terminal" || authFlow.agentId !== agentId) return;
    releaseAuthFlow(deps);
    postAuth(agentId, "idle");
  });
}

// isWellFormedBridgeRelay is handleMessage's allowlist check (§8 W-SEC) for
// an inbound {type:"bridge-window-message"} from the webview's dumb-pipe
// relay: exact channel/version, primitive-typed fields, size-capped. Origin
// allowlisting and eventId matching are FLOW-state checks, not shape
// checks — they live in handleBridgeWindowMessage below, alongside the
// other flow-dependent decisions (busy/unsupported).
function isWellFormedBridgeRelay(msg) {
  if (typeof msg.origin !== "string") return false;
  if (!msg.data || typeof msg.data !== "object") return false;
  const d = msg.data;
  if (d.channel !== BRIDGE_CHANNEL) return false;
  if (d.version !== BRIDGE_VERSION) return false;
  if (typeof d.type !== "string") return false;
  if (typeof d.eventId !== "string") return false;
  if (d.accepted !== undefined && typeof d.accepted !== "boolean") return false;
  if (d.reason !== undefined && typeof d.reason !== "string") return false;
  let size = 0;
  try { size = JSON.stringify(d).length; } catch (_) { return false; }
  return size <= BRIDGE_RELAY_MAX_BYTES;
}

// handleBridgeWindowMessage re-validates a relayed ack against the LIVE
// flow state (§8 W-SEC: "Host validates relayed messages again — defense in
// depth") — origin allowlisted, eventId matching the in-flight bridge flow
// — before acting on accepted/reason. Anything else (no bridge flow in
// flight, wrong origin, foreign eventId, an unrecognized accepted/reason
// combination) is dropped silently, per spec §4.
function handleBridgeWindowMessage(msg, deps) {
  if (!authFlow || authFlow.kind !== "bridge") {
    console.log("[zcp-welcome] dropped bridge message: no bridge flow in flight");
    return;
  }
  if (!isAllowedGuiOrigin(msg.origin, resolveBridgeExtraOrigins(deps))) {
    console.log("[zcp-welcome] dropped bridge message: origin not allowlisted");
    return;
  }
  const data = msg.data;
  if (data.eventId !== authFlow.eventId) {
    console.log("[zcp-welcome] dropped bridge message: eventId mismatch");
    return;
  }
  if (data.type !== "open-agent-auth-ack") {
    console.log("[zcp-welcome] dropped bridge message: unexpected type " + data.type);
    return;
  }

  const agentId = authFlow.agentId;
  if (data.accepted === true) {
    if (authFlow.ackTimer) { deps.clearTimeout(authFlow.ackTimer); authFlow.ackTimer = null; }
    const capTimer = deps.setTimeout(() => {
      if (!authFlow || authFlow.kind !== "bridge" || authFlow.agentId !== agentId) return;
      releaseAuthFlow(deps);
      postBridgeAuth(agentId, "idle");
    }, AUTH_FLOW_CAP_MS);
    unrefTimer(capTimer);
    authFlow.capTimer = capTimer;
    postBridgeAuth(agentId, "dialog-opening");
    return;
  }
  if (data.accepted === false && data.reason === "unsupported-agent") {
    releaseAuthFlow(deps);
    postBridgeAuth(agentId, "unsupported");
    return;
  }
  console.log("[zcp-welcome] dropped bridge ack: unexpected accepted/reason combination");
}

// ---- guided toggle (docs/spec-welcome-mode.md §5, W-GUIDED) --------------

// isAuthoringMode mirrors extension.js's own agentTypesFrom precedent:
// prefer the LIVE zembed store when it has an opinion, fall back to the
// extension host's frozen process.env only when the store doesn't carry the
// key at all. Go's own gate (internal/runtime/runtime.go) treats exactly
// "1" as authoring — this mirrors that exactly, never a generic truthy check.
function isAuthoringMode(deps) {
  const env = deps.readZembedEnv();
  const zembedVal = env ? env.ZCP_AUTHORING : undefined;
  if (zembedVal !== undefined) return zembedVal === "1";
  return process.env.ZCP_AUTHORING === "1";
}

// anyDirtyGuardedDoc reports whether AGENTS.md or CLAUDE.md directly under
// selectedFolder is open with unsaved changes — zcp init rewrites both, so
// running over an unsaved edit would silently discard it (spec §5).
function anyDirtyGuardedDoc(deps, selectedFolder) {
  const guarded = new Set([path.join(selectedFolder, "AGENTS.md"), path.join(selectedFolder, "CLAUDE.md")]);
  let docs = [];
  try { docs = deps.textDocuments() || []; } catch (_) { docs = []; }
  return docs.some((d) => d && d.isDirty && d.uri && guarded.has(d.uri.fsPath));
}

// selectGuidedFolder resolves which workspace folder a guided toggle runs
// against: the sole folder needs no prompt; multiple folders ask via
// deps.showQuickPick — NEVER a hardcoded path. Returns null when there is no
// workspace, or the user cancels the picker.
async function selectGuidedFolder(deps) {
  const folders = deps.workspaceFolders;
  if (!folders || folders.length === 0) return null;
  if (folders.length === 1) return folders[0];
  const picked = await deps.showQuickPick(folders, { placeHolder: "Select a workspace folder for zcp init" });
  return picked || null;
}

// streamChildOutput pipes a spawned zcp init's stdout/stderr into the guided
// output channel, one line at a time — displayed only, NEVER parsed for
// success (completion is exit-code + marker re-read only, below).
function streamChildOutput(child, channel) {
  if (!channel) return;
  for (const key of ["stdout", "stderr"]) {
    const stream = child[key];
    if (!stream || typeof stream.on !== "function") continue;
    let buffered = "";
    stream.on("data", (chunk) => {
      buffered += chunk.toString();
      const lines = buffered.split("\n");
      buffered = lines.pop(); // keep the trailing partial line for the next chunk
      for (const line of lines) channel.appendLine(line.replace(/\r$/, ""));
    });
  }
}

// postGuidedResult sends a guided toggle run's outcome to the webview (spec
// §5) — the one new outbound message type this slice adds.
function postGuidedResult(result) {
  if (!panel) return;
  try {
    panel.webview.postMessage(Object.assign({ type: "guided-result" }, result));
  } catch (err) {
    console.error("[zcp-welcome] postMessage failed:", err);
  }
}

// finishGuidedToggle releases the guided lock, reports the outcome (a null
// result — the quickpick-cancel case only — still reports a bare {ok:false}
// so the webview's optimistic "running…" toggle always clears), and always
// pushes fresh state afterward: the marker may have moved regardless of
// which path finished (spec §5: "always release the lock; always push
// fresh state after any outcome").
function finishGuidedToggle(deps, result) {
  guidedFlow = null;
  postGuidedResult(result || { ok: false });
  postState(deps);
}

// handleGuidedToggle drives a webview {type:"guided-toggle", enable} click
// (spec §5, W-GUIDED): guards, folder selection, spawn, and an HONEST
// completion report — exit code AND a marker re-read, never a parse of
// output prose.
async function handleGuidedToggle(enable, deps) {
  if (!deps.workspaceFolders || deps.workspaceFolders.length === 0) {
    postGuidedResult({ ok: false, message: GUIDED_NO_WORKSPACE_MESSAGE });
    return;
  }
  if (isAuthoringMode(deps)) {
    postGuidedResult({ ok: false, message: GUIDED_AUTHORING_MESSAGE });
    return;
  }
  if (guidedFlow) {
    postGuidedResult({ ok: false, message: GUIDED_BUSY_MESSAGE });
    return;
  }

  guidedFlow = { enable };

  // Everything past lock acquisition is wrapped in try/catch (Finding-3-class
  // robustness): handleMessage invokes this handler without awaiting it, so
  // ANY unexpected throw here — not just the two spots already guarded below
  // — must still release guidedFlow and report an error, never leave the
  // lock (and the webview's optimistic "running…" toggle) stuck forever.
  try {
    let selectedFolder;
    try {
      selectedFolder = await selectGuidedFolder(deps);
    } catch (err) {
      console.error("[zcp-welcome] guided folder selection failed:", err);
      finishGuidedToggle(deps, null);
      return;
    }
    if (!selectedFolder) {
      finishGuidedToggle(deps, null); // user cancelled the picker — no spawn
      return;
    }

    // Sticky for the panel's lifetime (spec §3 "selected workspace folder"
    // — Finding 4): collectFullState prefers this over deps.workspaceRoot's
    // fixed first-folder default, so a multi-root toggle against folder B
    // doesn't read back folder A's marker and snap the toggle back off. The
    // panel may have been disposed while the picker above was awaited; only
    // a LIVE panel gets its guided-marker watcher re-pointed (a disposed
    // panel's disposables were already torn down by disposeWatchers).
    selectedGuidedRoot = selectedFolder;
    if (panel) reattachGuidedMarkerWatcher(deps, selectedGuidedRoot);

    if (anyDirtyGuardedDoc(deps, selectedFolder)) {
      finishGuidedToggle(deps, { ok: false, message: GUIDED_DIRTY_MESSAGE });
      return;
    }

    if (guidedOutputChannel) {
      const argv = enable ? "zcp init --guided" : "zcp init";
      guidedOutputChannel.appendLine("$ " + argv + " (cwd=" + selectedFolder + ")");
    }

    let child;
    try {
      child = deps.spawn("zcp", enable ? ["init", "--guided"] : ["init"], { cwd: selectedFolder, shell: false });
    } catch (err) {
      finishGuidedToggle(deps, { ok: false, message: GUIDED_ENOENT_MESSAGE });
      return;
    }
    if (!child || typeof child.on !== "function") {
      finishGuidedToggle(deps, { ok: false, message: GUIDED_PARTIAL_FAILURE_MESSAGE });
      return;
    }
    guidedFlow.child = child; // tag the lock with this run's child — see the staleness checks below

    streamChildOutput(child, guidedOutputChannel);

    // Node's own docs don't guarantee "error" and "exit" are mutually
    // exclusive for every failure mode (unlike a plain ENOENT, verified
    // error-only on this runtime) — the guidedFlow.child identity check below
    // mirrors authFlow's eventId/kind checks elsewhere in this file: without
    // it, a second (late, spurious) event for the SAME child could finish a
    // NEWER run that reused the now-released lock. Each callback is ALSO its
    // own try/catch: it runs asynchronously, well outside this function's
    // own try above, and it is the ONLY remaining path back to releasing
    // guidedFlow for this run — an uncaught throw here would leak the lock
    // exactly as an unreleased dispose would (Finding 2's failure mode).
    child.on("error", (err) => {
      try {
        if (!guidedFlow || guidedFlow.child !== child) return;
        if (guidedOutputChannel) guidedOutputChannel.appendLine("[zcp-welcome] zcp init failed to start: " + err);
        const message = err && err.code === "ENOENT" ? GUIDED_ENOENT_MESSAGE : GUIDED_PARTIAL_FAILURE_MESSAGE;
        finishGuidedToggle(deps, { ok: false, message });
      } catch (handlerErr) {
        console.error("[zcp-welcome] guided child 'error' handler failed unexpectedly:", handlerErr);
        finishGuidedToggle(deps, { ok: false, message: GUIDED_PARTIAL_FAILURE_MESSAGE });
      }
    });

    child.on("exit", (code) => {
      try {
        if (!guidedFlow || guidedFlow.child !== child) return;
        const markerEnabled = collectGuided(deps.fs, selectedFolder).state === "enabled";
        if (code === 0 && markerEnabled === enable) {
          finishGuidedToggle(deps, { ok: true, enabled: enable });
        } else if (code === 0) {
          finishGuidedToggle(deps, { ok: false, message: GUIDED_MARKER_MISMATCH_MESSAGE });
        } else {
          finishGuidedToggle(deps, { ok: false, message: GUIDED_PARTIAL_FAILURE_MESSAGE });
        }
      } catch (handlerErr) {
        console.error("[zcp-welcome] guided child 'exit' handler failed unexpectedly:", handlerErr);
        finishGuidedToggle(deps, { ok: false, message: GUIDED_PARTIAL_FAILURE_MESSAGE });
      }
    });
  } catch (err) {
    console.error("[zcp-welcome] guided-toggle failed unexpectedly:", err);
    finishGuidedToggle(deps, { ok: false, message: GUIDED_PARTIAL_FAILURE_MESSAGE });
  }
}

// ---- curated skills install (docs/spec-welcome-mode.md §6, W-SKILLS) -----

const SKILL_UNKNOWN_MESSAGE = "Unknown skill.";
const SKILL_RESERVED_MESSAGE = "\"guided\" is managed by the Zerops Guided toggle above, not skill install.";
const SKILL_NO_WORKSPACE_MESSAGE = "No workspace folder open — open a folder first.";
const SKILL_UNTRUSTED_MESSAGE = "Workspace is not trusted.";
const SKILL_UNSAFE_PATH_MESSAGE = "Refusing to install: a .claude/skills path is a symlink.";
const SKILL_SHIPPED_MISSING_MESSAGE = "Shipped skill content missing from this install.";
const SKILL_UNEXPECTED_ERROR_MESSAGE = "Skill install failed unexpectedly — see the extension host log.";

// postSkillResult sends one {type:"skill-result"} outcome for a single
// {type:"skill-add"} click — the per-row status the skills tile renders from
// (spec §6). message is present only on status "error".
function postSkillResult(slug, status, message) {
  if (!panel) return;
  const msg = { type: "skill-result", slug, status };
  if (message) msg.message = message;
  try {
    panel.webview.postMessage(msg);
  } catch (err) {
    console.error("[zcp-welcome] postMessage failed:", err);
  }
}

function skillModifiedPrompt(slug) {
  return "The \"" + slug + "\" skill has local changes. Replace it with the curated version?";
}

// resolveNearestRealpath realpaths the nearest EXISTING ancestor of target (a
// fresh install's slug dir usually doesn't exist yet) and re-appends the
// remaining, not-yet-existing path segments — resolve what's real, trust the
// rest, so containment can be checked before anything is created.
function resolveNearestRealpath(fsImpl, target) {
  let current = target;
  const remainder = [];
  for (;;) {
    let exists = false;
    try { exists = fsImpl.existsSync(current); } catch (_) { exists = false; }
    if (exists) break;
    const parent = path.dirname(current);
    if (parent === current) break; // reached the filesystem root without finding anything
    remainder.unshift(path.basename(current));
    current = parent;
  }
  let real = current;
  try { real = fsImpl.realpathSync(current); } catch (_) { real = current; }
  return path.join(real, ...remainder);
}

// isSafeSkillDestination rejects a symlinked .claude / .claude/skills /
// .claude/skills/<slug> path component (spec §6: "symlinked path components
// are rejected" — lstat, not stat, so the symlink itself is what's checked,
// never its target) and confirms the resolved skill directory still sits
// under the workspace folder's own realpath (spec §6: "destination
// containment is validated") — defense against a workspace that plants a
// symlink to escape it. A component that doesn't exist yet is fine (it will
// be created by the atomic write below).
function isSafeSkillDestination(fsImpl, wsRoot, slug) {
  const claudeDir = path.join(wsRoot, ".claude");
  const skillsDir = path.join(claudeDir, "skills");
  const slugDir = path.join(skillsDir, slug);
  for (const p of [claudeDir, skillsDir, slugDir]) {
    let st;
    try { st = fsImpl.lstatSync(p); } catch (_) { continue; }
    if (st.isSymbolicLink()) return false;
  }
  let wsReal = wsRoot;
  try { wsReal = fsImpl.realpathSync(wsRoot); } catch (_) { wsReal = wsRoot; }
  const slugDirReal = resolveNearestRealpath(fsImpl, slugDir);
  return slugDirReal === wsReal || slugDirReal.startsWith(wsReal + path.sep);
}

// writeSkillAtomic writes `data` to `dest` via a tmp file in the SAME dir
// followed by a rename (spec §6: "creation is atomic") — a reader never
// observes a half-written SKILL.md. If the rename itself fails (dest dir
// permissions changing mid-flight, ...) the tmp file is removed here, on a
// best-effort basis, before the error propagates — self-contained so a
// caller's failure handling never needs to know a tmp path was ever
// involved.
function writeSkillAtomic(fsImpl, dest, data) {
  fsImpl.mkdirSync(path.dirname(dest), { recursive: true });
  const tmp = dest + ".tmp-" + crypto.randomBytes(6).toString("hex");
  fsImpl.writeFileSync(tmp, data);
  try {
    fsImpl.renameSync(tmp, dest);
  } catch (err) {
    try { fsImpl.unlinkSync(tmp); } catch (_) {}
    throw err;
  }
}

// handleSkillAdd drives a webview {type:"skill-add", slug} click (spec §6):
// every validation below rejects with an explicit skill-result "error" —
// never a silent drop. The allowlist gate in handleMessage only checks
// msg.slug is a string; every semantic check (enum, reserved, workspace,
// trust, containment) lives here, mirroring handleGuidedToggle's own
// gate/handler split. absent -> install; identical -> no-op; locally
// modified -> a modal confirmation gates the overwrite. Always pushes fresh
// state afterward so the tile's status chip reflects the outcome.
//
// Everything past the synchronous validations above is wrapped in try/catch
// (spec W7 + Finding-3-class robustness): readIfPresent now RE-THROWS a
// genuine read failure (never "absent" — see readIfPresent's own doc-comment)
// and writeSkillAtomic/showWarningMessage can throw too, but handleMessage
// invokes this handler without awaiting it — an escaping throw would become
// an unhandled rejection AND leave the webview's optimistic "installing…"
// row with no completion message, stuck forever. An unexpected throw here
// always resolves to an explicit error result, never a silent hang.
async function handleSkillAdd(slug, deps) {
  if (slug === RESERVED_SKILL_SLUG) {
    postSkillResult(slug, "error", SKILL_RESERVED_MESSAGE);
    return;
  }
  if (!SKILL_SLUGS.has(slug)) {
    postSkillResult(slug, "error", SKILL_UNKNOWN_MESSAGE);
    return;
  }
  if (!deps.workspaceRoot) {
    postSkillResult(slug, "error", SKILL_NO_WORKSPACE_MESSAGE);
    return;
  }
  if (deps.isTrusted === false) {
    postSkillResult(slug, "error", SKILL_UNTRUSTED_MESSAGE);
    return;
  }
  if (!isSafeSkillDestination(deps.fs, deps.workspaceRoot, slug)) {
    postSkillResult(slug, "error", SKILL_UNSAFE_PATH_MESSAGE);
    return;
  }

  try {
    const shipped = readIfPresent(deps.fs, shippedSkillPath(deps.extensionPath, slug));
    if (shipped === null) {
      postSkillResult(slug, "error", SKILL_SHIPPED_MISSING_MESSAGE);
      return;
    }

    const dest = skillDestPath(deps.workspaceRoot, slug);
    // A thrown (non-ENOENT) read error is a REFUSAL, never "absent": treating
    // it as absent would install-fresh over a file we couldn't even confirm
    // is safe to touch, silently destroying whatever is actually there.
    const existing = readIfPresent(deps.fs, dest);

    if (existing === null) {
      writeSkillAtomic(deps.fs, dest, shipped);
      postSkillResult(slug, "installed");
    } else if (hashBytes(existing) === hashBytes(shipped)) {
      postSkillResult(slug, "installed-current");
    } else {
      let choice;
      try {
        choice = await deps.showWarningMessage(skillModifiedPrompt(slug), { modal: true }, "Replace");
      } catch (err) {
        console.error("[zcp-welcome] showWarningMessage failed:", err);
        choice = undefined;
      }
      if (choice === "Replace") {
        writeSkillAtomic(deps.fs, dest, shipped);
        postSkillResult(slug, "replaced");
      } else {
        postSkillResult(slug, "kept");
      }
    }
  } catch (err) {
    console.error("[zcp-welcome] skill-add " + slug + " failed unexpectedly:", err);
    postSkillResult(slug, "error", SKILL_UNEXPECTED_ERROR_MESSAGE);
  }
  postState(deps);
}

// ---- CTA (docs/spec-welcome-mode.md §7, W-CTA) ---------------------------

// The two kickoff prompts, final copy (spec §7): handed to the agent via
// the clipboard, never typed into the DOM/logs, never altered per-agent.
const CTA_PROMPTS = {
  new: "I want to build something new on Zerops. Ask me what I'm building, then plan the smallest working version and get it running on this project's dev runtime.",
  existing: "I have an existing app in this workspace that I want to run on Zerops. Inspect the repo, tell me your integration plan, then wire it up and get it running on the dev runtime.",
};

const CTA_NOT_AUTHORIZED_MESSAGE = "Authorize an agent first.";
const CTA_SELECT_AGENT_MESSAGE = "Select which authorized agent should start.";
const CTA_CLIPBOARD_FAILED_MESSAGE = "Agent opened, but the kickoff prompt could not be copied to the clipboard.";

function ctaKickoffMessage(label) {
  return "Kickoff prompt copied — paste it into " + label + " to start.";
}

// postCTAResult sends a {type:"cta-result"} outcome for a {type:"start-
// onboarding"} click (spec §7 W-CTA) — success or failure, the panel is
// NEVER disposed here: the user may come back to it.
function postCTAResult(ok, message) {
  if (!panel) return;
  try {
    panel.webview.postMessage({ type: "cta-result", ok, message });
  } catch (err) {
    console.error("[zcp-welcome] postMessage failed:", err);
  }
}

// handleStartOnboarding drives a webview {type:"start-onboarding", path,
// agentId} click (spec §7 W-CTA). Re-validates against a FRESH state read —
// never trusts the webview's own idea of who is authorized, and never falls
// back to "first in registry" when the target agent can't be resolved: with
// zero authorized agents it's a plain rejection; with the given agentId
// (missing, unknown, or naming an agent that ISN'T currently authorized)
// failing to resolve to exactly one CURRENTLY authorized agent, it's an
// explicit "select an agent" rejection instead of a silent guess. Launch is
// entirely the injected runAgentAction's call (HOW — plugin command vs
// panel terminal — welcome adds no launch flags); the kickoff prompt is
// clipboard-first — NEVER terminal.sendText, NEVER a delayed setTimeout
// injection, since a terminal may not even be running the agent.
async function handleStartOnboarding(path, agentId, deps) {
  const prompt = CTA_PROMPTS[path];
  const state = collectFullState(deps);
  const authorized = state.agents.filter((a) => a.state === "authorized" || a.state === "authorized-token");

  if (authorized.length === 0) {
    postCTAResult(false, CTA_NOT_AUTHORIZED_MESSAGE);
    return;
  }

  let resolved = typeof agentId === "string" ? authorized.find((a) => a.id === agentId) : undefined;
  if (!resolved && agentId === undefined && authorized.length === 1) resolved = authorized[0]; // the one-authorized-agent implicit case
  if (!resolved) {
    postCTAResult(false, CTA_SELECT_AGENT_MESSAGE);
    return;
  }

  const agentEntry = deps.REGISTRY[resolved.id];
  if (!agentEntry || !Array.isArray(agentEntry.opens) || !agentEntry.opens[0]) {
    // Defensive only: state.agents is built FROM deps.REGISTRY (buildState),
    // so a resolved agent id missing its own registry entry should never
    // happen in practice.
    console.error("[zcp-welcome] start-onboarding: no launch mode registered for " + resolved.id);
    postCTAResult(false, CTA_SELECT_AGENT_MESSAGE);
    return;
  }

  deps.runAgentAction(agentEntry, agentEntry.opens[0].mode);

  try {
    await deps.clipboard.writeText(prompt);
  } catch (err) {
    console.error("[zcp-welcome] clipboard.writeText failed:", err);
    postCTAResult(false, CTA_CLIPBOARD_FAILED_MESSAGE);
    return;
  }

  const message = ctaKickoffMessage(agentEntry.label || resolved.id);
  try {
    deps.showInformationMessage(message);
  } catch (err) {
    console.error("[zcp-welcome] showInformationMessage failed:", err);
  }
  postCTAResult(true, message);
}

// handleMessage is the strict allowlist gate (§8 W-SEC): exactly the shapes
// below do anything; everything else — including a well-formed message of
// an unknown type, or a message whose fields fail their check — is
// silently dropped (counted to console for debugging only), never thrown,
// never surfaced to the user.
function handleMessage(msg, deps) {
  if (!msg || typeof msg.type !== "string") return;
  switch (msg.type) {
    case "ready":
      postState(deps);
      return;
    case "open-url":
      if (typeof msg.url === "string" && EXTERNAL_URLS.has(msg.url)) {
        vscode.env.openExternal(vscode.Uri.parse(msg.url));
      } else {
        console.log("[zcp-welcome] dropped open-url: not on the allowlist");
      }
      return;
    case "authorize":
      if (typeof msg.agentId === "string" && deps.ALL_AGENT_IDS.includes(msg.agentId)) {
        handleAuthorize(msg.agentId, deps);
      } else {
        console.log("[zcp-welcome] dropped authorize: bad agentId");
      }
      return;
    case "authorize-terminal":
      if (typeof msg.agentId === "string" && deps.ALL_AGENT_IDS.includes(msg.agentId)) {
        handleAuthorizeTerminal(msg.agentId, deps);
      } else {
        console.log("[zcp-welcome] dropped authorize-terminal: bad agentId");
      }
      return;
    case "bridge-window-message":
      if (isWellFormedBridgeRelay(msg)) {
        handleBridgeWindowMessage(msg, deps);
      } else {
        console.log("[zcp-welcome] dropped bridge-window-message: malformed");
      }
      return;
    case "guided-toggle":
      if (typeof msg.enable === "boolean") {
        // Both handlers are already self-contained (their own try/catch
        // resolves any unexpected throw to an explicit result — Finding 3),
        // but handleMessage never awaits them either, so this .catch() is a
        // second, independent line of defense: nothing that changes inside
        // the handler later can turn into an unhandled promise rejection.
        handleGuidedToggle(msg.enable, deps).catch((err) => {
          console.error("[zcp-welcome] unhandled guided-toggle error:", err);
        });
      } else {
        console.log("[zcp-welcome] dropped guided-toggle: bad enable");
      }
      return;
    case "skill-add":
      if (typeof msg.slug === "string") {
        handleSkillAdd(msg.slug, deps).catch((err) => {
          console.error("[zcp-welcome] unhandled skill-add error:", err);
        });
      } else {
        console.log("[zcp-welcome] dropped skill-add: bad slug");
      }
      return;
    case "start-onboarding": {
      const pathOk = msg.path === "new" || msg.path === "existing";
      const agentIdOk = msg.agentId === undefined || (typeof msg.agentId === "string" && deps.ALL_AGENT_IDS.includes(msg.agentId));
      if (pathOk && agentIdOk) {
        handleStartOnboarding(msg.path, msg.agentId, deps);
      } else {
        console.log("[zcp-welcome] dropped start-onboarding: bad path/agentId");
      }
      return;
    }
    default:
      console.log("[zcp-welcome] dropped unknown message type: " + msg.type);
      return;
  }
}

// disposeWatchers tears down everything panel-scoped on close: the
// watchers, and — since the ACK/cap timers and the terminal-close listener
// created by an in-flight auth flow are exactly as panel-scoped — that flow
// too (silently: there is no UI left to post an idle transition to).
//
// guidedFlow is DELIBERATELY NOT cleared here (Finding 2, spec §5 "one
// toggle in flight per window"): the spawned `zcp init` keeps running
// regardless of the panel, so dropping the lock here would let a close +
// reopen + toggle spawn a SECOND concurrent `zcp init` against the same
// files. The lock is released ONLY by that child's own exit/error handler
// (finishGuidedToggle, registered in handleGuidedToggle). By the time it
// fires, the panel may be gone, or a DIFFERENT panel may be current —
// postGuidedResult/postState both already guard `if (!panel) return`, so
// that eventual completion is safe either way, landing on whichever panel
// is current if any (guidedOutputChannel outlives the panel too, spec §5).
function disposeWatchers(deps) {
  if (pushTimer) { clearTimeout(pushTimer); pushTimer = null; }
  for (const d of disposables) {
    try { d.dispose(); } catch (_) {}
  }
  disposables = [];
  // Force the next open()'s startWatchers to re-arm the guided-marker
  // watcher unconditionally: the one just disposed above (if any) is gone,
  // but guidedMarkerWatcherRoot would otherwise still name its folder,
  // making reattachGuidedMarkerWatcher wrongly think a matching root is
  // already watched and skip re-attaching on reopen.
  guidedMarkerWatcher = null;
  guidedMarkerWatcherRoot = undefined;
  releaseAuthFlow(deps);
}

function schedulePush(deps) {
  if (pushTimer) clearTimeout(pushTimer);
  pushTimer = setTimeout(() => { pushTimer = null; postState(deps); }, STATE_PUSH_DEBOUNCE_MS);
  if (typeof pushTimer.unref === "function") pushTimer.unref(); // see unrefWatcher() above
}

// ---- watchers (docs/spec-welcome-mode.md §3) -------------------------

// unrefWatcher marks a watch handle as NOT keeping its process alive by
// itself (real fs.FSWatcher supports this; test stubs generally don't, so
// the check is required, not defensive noise). The extension host process
// is kept alive by VS Code itself regardless, so this only matters for
// letting a process exit cleanly when nothing else is pending — e.g. a
// welcomejs test's `node --test` run, which would otherwise hang on a real,
// never-closed watcher (a fresh HOME can genuinely already have ~/.claude).
function unrefWatcher(w) {
  if (w && typeof w.unref === "function") w.unref();
}

// attachWatcherErrorHandler is the ONE place every fs.watch() call below
// registers its 'error' listener (spec §3). An async watcher error (EMFILE
// "too many open files", ENOSPC) is delivered on Node's 'error' event, never
// as a thrown exception from the watch() call itself — an FSWatcher with NO
// 'error' listener makes Node's default EventEmitter behavior throw it
// straight into the extension host on the next tick. Every occurrence here
// must degrade quietly instead: log + close, never escape uncaught. Guarded
// by typeof w.on: a real FSWatcher always has it, a minimal test stub's
// watch() return value may not (existing simpler test doubles keep working
// unchanged).
function attachWatcherErrorHandler(w, label) {
  if (!w || typeof w.on !== "function") return;
  w.on("error", (err) => {
    console.warn("[zcp-welcome] fs.watch(" + label + ") error, closing:", err);
    try { w.close(); } catch (_) {}
  });
}

// Zerops rewrites env.json IN PLACE (stable inode) on every env change, so
// watching the FILE (not its directory) — the same pattern as extension.js's
// launcher watcher — means we wake only on real env changes.
function watchZembedEnv(deps) {
  try {
    const w = deps.fs.watch(ZEMBED_ENV_FILE, () => schedulePush(deps));
    unrefWatcher(w);
    attachWatcherErrorHandler(w, "zembed");
    return { dispose() { try { w.close(); } catch (_) {} } };
  } catch (err) {
    console.warn("[zcp-welcome] fs.watch(zembed) unavailable:", err);
    return null;
  }
}

// watchCredDir watches an agent's credential DIR so a login (which creates
// the dir + writes the artifact, however the CLI does it) is caught. The dir
// may not exist yet (agent never logged in) — fs.watch on a missing path
// throws immediately, so we watch HOME instead (non-recursive) until the
// target dir appears, then swap. Every event on either watcher — rename or
// change, whatever the platform reports — just re-triggers a full recompute;
// there is no cheaper reliable way to notice an atomic-replace write landing
// (spec §3: "survive atomic rename writes").
//
// `generation` guards the HOME->target swap: fs.watch callbacks are
// delivered asynchronously and can queue up, so a SECOND (stale) HOME event
// — already in flight when the first one closed HOME and attached the
// target watcher — must not re-fire the swap (closing the freshly attached
// target watcher out from under itself, then re-attaching and double-firing
// onEvent). Every (re)attach mints a new generation and captures it in its
// own callback's closure; only a callback whose captured generation still
// matches the current one is live.
function watchCredDir(fsImpl, homeDir, dirName, onEvent) {
  const target = path.join(homeDir, dirName);
  let watcher = null;
  let generation = 0;

  function attachTarget() {
    const myGen = ++generation;
    try {
      const w = fsImpl.watch(target, () => {
        if (myGen !== generation) return; // superseded — see dispose()/attachHome() below
        onEvent();
      });
      unrefWatcher(w);
      attachWatcherErrorHandler(w, dirName);
      watcher = w;
    } catch (_) {
      watcher = null;
    }
  }

  function attachHome() {
    const myGen = ++generation;
    try {
      const w = fsImpl.watch(homeDir, () => {
        if (myGen !== generation) return; // this HOME watcher has already been superseded
        let exists = false;
        try { exists = fsImpl.existsSync(target); } catch (_) { exists = false; }
        if (!exists) return;
        if (watcher) { try { watcher.close(); } catch (_) {} }
        attachTarget();
        onEvent();
      });
      unrefWatcher(w);
      attachWatcherErrorHandler(w, dirName + "(home fallback)");
      watcher = w;
    } catch (_) {
      watcher = null;
    }
  }

  let targetExists = false;
  try { targetExists = fsImpl.existsSync(target); } catch (_) { targetExists = false; }
  if (targetExists) attachTarget(); else attachHome();

  return {
    dispose() {
      generation++; // invalidate any callback still queued for the current watcher
      if (watcher) { try { watcher.close(); } catch (_) {} watcher = null; }
    },
  };
}

// watchGuidedMarker follows the .zcp/state -> .zcp -> none fallback chain
// (spec §3). The marker's write is a single fast `zcp init --guided` run
// rather than a long-lived interactive login, so even the coarser .zcp-level
// fallback is enough: the recompute it triggers re-checks the marker fresh,
// by which time the run has finished. No folder open, or neither directory
// exists yet, means no watcher — the caller relies on the reveal/focus
// recompute instead (no polling loop).
function watchGuidedMarker(fsImpl, workspaceRoot, onEvent) {
  if (!workspaceRoot) return null;
  const stateDir = path.join(workspaceRoot, ".zcp", "state");
  const zcpDir = path.join(workspaceRoot, ".zcp");
  let target = null;
  try {
    if (fsImpl.existsSync(stateDir)) target = stateDir;
    else if (fsImpl.existsSync(zcpDir)) target = zcpDir;
  } catch (_) { target = null; }
  if (!target) return null;
  try {
    const w = fsImpl.watch(target, () => onEvent());
    unrefWatcher(w);
    attachWatcherErrorHandler(w, "guided-marker");
    return { dispose() { try { w.close(); } catch (_) {} } };
  } catch (_) {
    return null;
  }
}

// reattachGuidedMarkerWatcher (re)points the guided-marker watcher at `root`
// — called once at startWatchers() time (root = the panel's default guided
// folder, before any toggle has run) and again whenever a guided toggle
// resolves a DIFFERENT folder (Finding 4, spec §3): the panel must reflect
// live changes to the folder the user actually operated on. A no-op when
// `root` already matches what's watched, so repeat toggles against the same
// folder don't churn the watcher.
function reattachGuidedMarkerWatcher(deps, root) {
  if (guidedMarkerWatcherRoot === root) return;
  if (guidedMarkerWatcher) {
    const idx = disposables.indexOf(guidedMarkerWatcher);
    if (idx >= 0) disposables.splice(idx, 1);
    try { guidedMarkerWatcher.dispose(); } catch (_) {}
    guidedMarkerWatcher = null;
  }
  guidedMarkerWatcherRoot = root;
  const w = watchGuidedMarker(deps.fs, root, () => schedulePush(deps));
  if (w) {
    guidedMarkerWatcher = w;
    disposables.push(w);
  }
}

// startWatchers runs ONCE per panel (only from open()'s creation branch,
// never on reveal), so re-invoking the command never accumulates watchers
// (spec §1, W-ENTRY).
function startWatchers(deps) {
  const zembed = watchZembedEnv(deps);
  if (zembed) disposables.push(zembed);

  for (const dirName of Object.values(CRED_WATCH_DIR)) {
    const w = watchCredDir(deps.fs, deps.homeDir, dirName, () => schedulePush(deps));
    if (w) disposables.push(w);
  }

  reattachGuidedMarkerWatcher(deps, selectedGuidedRoot || deps.workspaceRoot);
}

// open shows the singleton welcome panel: creates it (and starts its
// watchers) on the first call, reveals + re-reads state (never
// disposes/recreates) on every call after — see docs/spec-welcome-mode.md
// §1. No serializer is registered: after a window reload the panel is gone
// until the user re-invokes the command.
function open(ctx, deps) {
  const resolved = resolveDeps(deps);
  resolved.extensionPath = ctx.extensionPath; // skill installs read shipped bytes from here (spec §6)
  if (panel) {
    panel.reveal();
    postState(resolved); // re-invoking the command re-reads state (missed watcher events must not leave stale UI)
    return;
  }
  if (!guidedOutputChannel) {
    guidedOutputChannel = vscode.window.createOutputChannel("Zerops Welcome");
    ctx.subscriptions.push(guidedOutputChannel);
  }
  const nonce = crypto.randomBytes(16).toString("base64url");
  const newPanel = vscode.window.createWebviewPanel(
    "zeropsWelcome",
    "Get Started with Zerops",
    vscode.ViewColumn.One,
    { enableScripts: true, retainContextWhenHidden: true }
  );
  newPanel.webview.html = readWelcomeHtml(ctx, nonce);
  newPanel.webview.onDidReceiveMessage((msg) => handleMessage(msg, resolved));
  newPanel.onDidDispose(() => {
    if (panel === newPanel) panel = null;
    disposeWatchers(resolved);
  });
  // Switching back to this panel's tab (no command re-invocation) must also
  // re-read state — the panel may have been hidden through an entire login
  // flow or guided toggle run.
  disposables.push(newPanel.onDidChangeViewState((e) => {
    const visible = e && e.webviewPanel ? e.webviewPanel.visible : newPanel.visible;
    if (visible) postState(resolved);
  }));
  startWatchers(resolved);
  panel = newPanel;
}

module.exports = { open, computeAgentState, buildState, SKILLS, isAllowedGuiOrigin };
