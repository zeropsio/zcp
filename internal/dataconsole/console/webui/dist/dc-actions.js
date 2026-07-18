"use strict";

(function () {
  const root = typeof window !== "undefined" ? window : null;
  if (root) root.DC = root.DC || {};

  function actionList(source) {
    if (Array.isArray(source)) return source;
    if (source && Array.isArray(source.actions)) return source.actions;
    return [];
  }

  function actionOf(source, id) {
    return actionList(source).find((a) => a && a.id === id) || null;
  }

  function hasAction(source, id) {
    return !!actionOf(source, id);
  }

  function actionEnabled(source, id) {
    const a = actionOf(source, id);
    return !!(a && a.enabled);
  }

  function actionReason(source, id) {
    const a = actionOf(source, id);
    return a ? a.reason || "" : "";
  }

  function actionControl(source, id, label) {
    const a = actionOf(source, id);
    if (!a) {
      return { id, label, available: false, enabled: false, reason: "" };
    }
    return { id, label, available: true, enabled: !!a.enabled, reason: a.reason || "" };
  }

  const api = {
    actionOf,
    hasAction,
    actionEnabled,
    actionReason,
    actionControl,
  };

  if (root) root.DC.actions = api;
  if (typeof module !== "undefined") module.exports = api;
})();
