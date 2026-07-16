"use strict";

// E1 — card enumerator.
//
// Discovers cards by scanning the cards/ directory at ACTIVATION time, never at
// module load (R-FLEET: module load has zero side effects). Each cards/<name>.js
// exports a descriptor:
//
//   module.exports = {
//     id:    string,                       // stable id (also the dataset hook prefix)
//     title: string,                       // human label
//     render(uiMap) -> htmlFragmentString, // pure, host-side; returns the card's HTML
//     clientScript?: string,               // optional webview-side JS, run under the shell nonce
//   }
//
// There is NO central registration array: a card is discovered purely by dropping
// one file into cards/. *.test.js files are skipped so a co-located test never
// registers as a card.
//
// ORDER is product-owned, not filename-owned. Each descriptor carries an integer
// `order`; cards render ascending by it (filename as the stable tiebreak, and a
// card without `order` sorts last). This replaces the old pure-alphabetical order
// from the parallel-slice era — the panel's top-to-bottom IA is now a deliberate
// choice, not an accident of how files happen to be named.

const fs = require("fs");
const path = require("path");

function enumerateCards(cardsDir) {
  let files;
  try {
    files = fs.readdirSync(cardsDir);
  } catch (_) {
    return [];
  }
  const found = [];
  for (const f of files) {
    if (!f.endsWith(".js") || f.endsWith(".test.js")) continue;
    const mod = require(path.join(cardsDir, f));
    const card = mod && mod.default ? mod.default : mod;
    if (!card || typeof card.render !== "function" || !card.id) {
      throw new Error("invalid card module " + f + ": must export { id, title, render }");
    }
    found.push({ card: card, file: f });
  }
  found.sort(function (a, b) {
    const oa = typeof a.card.order === "number" ? a.card.order : 1e9;
    const ob = typeof b.card.order === "number" ? b.card.order : 1e9;
    if (oa !== ob) return oa - ob;
    return a.file < b.file ? -1 : a.file > b.file ? 1 : 0;
  });
  return found.map(function (e) { return e.card; });
}

module.exports = { enumerateCards };
