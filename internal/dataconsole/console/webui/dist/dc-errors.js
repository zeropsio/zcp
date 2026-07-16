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

  function userErrorMessage(e) {
    if (!e || typeof e === "string") return e || "error";
    if (e.code === "internal") return "Internal error.";
    if (e.code === "unreachable") return "Service unreachable.";
    if (e.code === "upstream") return "Service returned an upstream error.";
    if (e.code === "read_only") return "This session is read-only.";
    if (e.code === "needs_confirm") return "Confirmation required.";
    return e.message || "error";
  }

  function errorSummary(e) {
    const msg = userErrorMessage(e);
    return e && e.requestId ? msg + " \u00b7 request " + e.requestId : msg;
  }

  function errorHTML(e) {
    const req = e && e.requestId ? `<div class="muted">Request ID: ${esc(e.requestId)}</div>` : "";
    return `<div class="err">${esc(userErrorMessage(e))}${req}</div>`;
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
