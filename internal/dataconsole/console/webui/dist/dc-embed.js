"use strict";
// Embed chrome — decides what the console shows based on HOW it was launched.
//
// When the host (the Studio "Managed · data" card) embeds the console in a
// webview AND deep-links one service, the host's own managed-service list IS the
// service selector — so the console hides its duplicate left rail and uses the
// full width for that service's data. Standalone (its own browser tab) keeps the
// rail as primary navigation; embedded WITHOUT a usable target (the Studio's
// "Edit data ⚠" entry, which opens write mode with no service so the user must
// pick) keeps the rail too. The deep-link must resolve to a browsable service —
// otherwise hiding the rail would strand the user on an empty pane.

(function () {
  const root = typeof window !== "undefined" ? window : null;
  if (root) root.DC = root.DC || {};

  function shouldHideServiceRail(opts) {
    opts = opts || {};
    if (!opts.embedded || !opts.deepLinkedService) return false;
    const svc = (opts.services || []).find(function (s) {
      return s && s.hostname === opts.deepLinkedService;
    });
    if (!svc) return false;
    const isBrowsable = opts.isBrowsable || function () { return true; };
    return !!isBrowsable(svc);
  }

  const api = { shouldHideServiceRail };

  if (root) root.DC.embed = api;
  if (typeof module !== "undefined") module.exports = api;
})();
