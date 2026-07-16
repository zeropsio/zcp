"use strict";

(function () {
  const root = typeof window !== "undefined" ? window : null;
  if (root) root.DC = root.DC || {};

  function rowKeyOf(cols, keyCols, row) {
    const key = {};
    (cols || []).forEach((c, i) => {
      if ((keyCols || []).includes(c.name)) key[c.name] = row[i];
    });
    return key;
  }

  function keyColScore(cols, row) {
    const i = (cols || []).findIndex((c) => c.name === "score");
    return i >= 0 ? Number(row[i]) : null;
  }

  const api = {
    rowKeyOf,
    keyColScore,
  };

  if (root) root.DC.rows = api;
  if (typeof module !== "undefined") module.exports = api;
})();
