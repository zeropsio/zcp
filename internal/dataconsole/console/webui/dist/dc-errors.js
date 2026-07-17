"use strict";

(function () {
  const root = typeof window !== "undefined" ? window : null;
  if (root) root.DC = root.DC || {};

  const format = root && root.DC && root.DC.format
    ? root.DC.format
    : (typeof require !== "undefined" ? require("./dc-format") : {});
  const esc = format.esc;

  function errorFromEnvelope(payload, fallbackStatus, fallbackRequestId) {
    const src = payload && typeof payload === "object" ? payload : {};
    const msg = src.message || src.error || (typeof payload === "string" ? payload : String(fallbackStatus || "error"));
    const e = new Error(msg);
    e.status = src.status != null ? src.status : fallbackStatus;
    e.code = src.code || "";
    e.requestId = src.requestId || fallbackRequestId || "";
    e.service = src.service || "";
    e.family = src.family || "";
    e.action = src.action || "";
    return e;
  }

  // CREATE_ACTIONS mirrors provider.ActionCreateKey/ActionCreateDoc
  // (provider/actions.go) — the only two actions where a "conflict" is a
  // collision-refusing CREATE (spec §7.1 I-4) rather than a concurrent edit.
  const CREATE_ACTIONS = { createKey: true, createDoc: true };

  // userErrorMessage maps a typed sentinel (provider/errors.go, spec §P.3) to one
  // sanitized user-facing line. `timeout` is honest about accepted-not-confirmed
  // (U-14); an unknown code falls back to the envelope's already-sanitized message.
  function userErrorMessage(e) {
    if (!e || typeof e === "string") return e || "error";
    if (e.code === "internal") return "Internal error.";
    if (e.code === "unreachable") return "Service unreachable.";
    if (e.code === "upstream") return "Service returned an upstream error.";
    if (e.code === "read_only") return "This session is read-only.";
    if (e.code === "needs_confirm") return "Confirmation required.";
    if (e.code === "not_found") return "Not found.";
    // ErrConflict is shared by two different conditions (I-4): a create refused
    // for an id/name already taken (retrying with the same id always collides
    // again — "reload and retry" would be dishonest) vs an edit refused because
    // the item changed since it was read. The envelope's `action` (populated on
    // every route, §7.1 I-2) picks the honest wording.
    if (e.code === "conflict") {
      return CREATE_ACTIONS[e.action]
        ? "Already exists — choose a different id."
        : "Changed since you loaded it — reload and retry.";
    }
    if (e.code === "wrong_type") return "Refused: this would overwrite a different data type.";
    if (e.code === "too_large") return "Too large to edit here — use Download.";
    if (e.code === "unsupported") return "Not supported for this item.";
    if (e.code === "invalid") return "Invalid request.";
    if (e.code === "timeout") return "Accepted — still applying (not yet confirmed).";
    return e.message || "error";
  }

  function errorSummary(e) {
    const msg = userErrorMessage(e);
    return e && e.requestId ? msg + " \u00b7 request " + e.requestId : msg;
  }

  // errorHTML is the state canon's error surface (P.4): the sanitized message plus
  // the envelope's service·family provenance (I-2) and the request id, one inline
  // pane. Never the raw driver cause.
  function errorHTML(e) {
    const parts = [`<div class="err-msg">${esc(userErrorMessage(e))}</div>`];
    const where = [e && e.service, e && e.family].filter(Boolean).join(" · ");
    if (where) parts.push(`<div class="muted err-where">${esc(where)}</div>`);
    if (e && e.requestId) parts.push(`<div class="muted">Request ID: ${esc(e.requestId)}</div>`);
    return `<div class="err">${parts.join("")}</div>`;
  }

  function vpnGateDecision(opts) {
    const action = opts && opts.action;
    const error = opts && opts.error;
    if (!(action && action.enabled && error && error.code === "unreachable")) return { show: false };
    const projectId = opts.project && opts.project.id ? opts.project.id : "<projectId>";
    return {
      show: true,
      service: opts.service,
      projectId,
      reason: action.reason || "",
      command: "zcli vpn up " + projectId,
      summary: errorSummary(error),
    };
  }

  const api = {
    errorFromEnvelope,
    userErrorMessage,
    errorSummary,
    errorHTML,
    vpnGateDecision,
  };

  if (root) root.DC.errors = api;
  if (typeof module !== "undefined") module.exports = api;
})();
