"use strict";
// Embed chrome (docs/spec-dataconsole.md §4.4 — "one tab, in-tab switching").
//
// The SPA's own service rail is the service selector and stays visible
// WHENEVER the console is embedded — a deep link only PRESELECTS a target
// service, it never hides the rail. (The prior rule hid the rail on a
// browsable deep-link because the Studio sidebar's card list acted as the
// selector then; that sidebar no longer exists — the singleton panel is the
// only surface — so hiding the rail would strand the user with no way to
// switch services.) Standalone (its own browser tab) has never hidden the
// rail; that is unchanged.
//
// shouldHideServiceRail is kept (rather than deleted along with its call
// site) as the single, still-named owner of the decision, so a future rule
// change has one seam to edit rather than inlining `false` at the caller.

(function () {
  const root = typeof window !== "undefined" ? window : null;
  if (root) root.DC = root.DC || {};

  function shouldHideServiceRail() {
    return false;
  }

  const api = { shouldHideServiceRail };

  if (root) root.DC.embed = api;
  if (typeof module !== "undefined") module.exports = api;
})();
