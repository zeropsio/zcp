"use strict";

// Matt Pocock's Skills — the Customize picker (docs/spec-skill-packs.md §3.1,
// §4.2, §6). A jsdom-backed suite that executes the REAL webview inline
// <script> (harness.js's loadWebviewDom) and drives it through its real
// host<->webview seams (postToWebview delivers a {type:"state"}/
// {type:"pack-result"} push exactly like the extension host does;
// sentMessages captures every vscode.postMessage() call) — the only way to
// falsify the picker's conditional rendering against the actual artifact zcp
// ships, never a re-implementation of its logic.
//
// Ported from the archived pack_picker.test.js (archived/welcome-ux-redesign)
// with re-implemented selectors: the picker renders ENTIRELY from the
// `catalog` the CLI's pack-status contract reports on the pack's own state
// entry — never from a second, hard-coded skill list in this file or in
// welcome.html's own script. Test 1 below proves this directly with a
// synthetic catalog that does not overlap the real one; every other test
// uses MATT_CATALOG, a by-hand transcription of docs/spec-skill-packs.md
// §4.1's 21-skill table (never read back from internal/skillpacks/catalog.go)
// as its independent oracle.

const test = require("node:test");
const assert = require("node:assert/strict");
const { loadWebviewDom } = require("./harness.js");

// ---- fixtures --------------------------------------------------------

function baseState(overrides) {
  return Object.assign(
    {
      agents: [],
      anyAuthorized: false,
      guided: { state: "disabled" },
      packs: [],
      dataStudio: { available: false },
      bridge: { status: "unknown" },
      environment: { zembed: false },
      diagnostics: { zembedSeen: false, extensionVersion: "-", serviceId: "-", lastBridgeOutcome: "-", embedded: null },
    },
    overrides
  );
}

// MATT_REQUIRES — the reviewed Matt edge set, docs/spec-skill-packs.md
// §4.2's 15-edge table, transcribed by hand as its OWN independent oracle
// (never derived by reading internal/skillpacks/catalog.go back, same
// discipline as MATT_CATALOG below). A name absent from this map carries no
// `requires` key at all once merged into MATT_CATALOG — exercising the
// "omitted requires = edge-free" contract directly (picker_omitted_requires)
// for the other 13 skills, rather than every entry declaring `requires: []`.
const MATT_REQUIRES = {
  "grill-with-docs": ["grilling", "domain-modeling"],
  "implement": ["tdd", "code-review"],
  "improve-codebase-architecture": ["codebase-design", "grilling"],
  "wayfinder": ["grilling", "domain-modeling", "research", "setup-matt-pocock-skills"],
  "triage": ["grilling", "setup-matt-pocock-skills"],
  "code-review": ["setup-matt-pocock-skills"],
  "to-spec": ["setup-matt-pocock-skills"],
  "to-tickets": ["setup-matt-pocock-skills"],
};
assert.equal(
  Object.values(MATT_REQUIRES).reduce((sum, deps) => sum + deps.length, 0),
  15,
  "sanity: the transcribed edge fixture itself must carry all 15 edges from spec §4.2's table"
);

// MATT_CATALOG — the exact 21 supported skills from
// docs/spec-skill-packs.md §4.1, transcribed by hand (independent oracle:
// never derived by reading internal/skillpacks/catalog.go back). sourcePath/
// description are placeholders — the spec's own table names only name and
// category, and the picker must render whatever the CLI sends regardless of
// those two fields' exact content.
const MATT_CATALOG = [
  ["ask-matt", "Engineering"],
  ["diagnosing-bugs", "Engineering"],
  ["grill-with-docs", "Engineering"],
  ["triage", "Engineering"],
  ["improve-codebase-architecture", "Engineering"],
  ["setup-matt-pocock-skills", "Engineering"],
  ["tdd", "Engineering"],
  ["to-spec", "Engineering"],
  ["to-tickets", "Engineering"],
  ["wayfinder", "Engineering"],
  ["implement", "Engineering"],
  ["prototype", "Engineering"],
  ["research", "Engineering"],
  ["domain-modeling", "Engineering"],
  ["codebase-design", "Engineering"],
  ["code-review", "Engineering"],
  ["resolving-merge-conflicts", "Engineering"],
  ["grilling", "Productivity"],
  ["handoff", "Productivity"],
  ["teach", "Productivity"],
  ["writing-great-skills", "Productivity"],
].map(([name, category]) => {
  const entry = {
    name,
    sourcePath: "skills/" + category.toLowerCase() + "/" + name,
    category,
    description: "Description of " + name + ".",
  };
  if (MATT_REQUIRES[name]) entry.requires = MATT_REQUIRES[name];
  return entry;
});
assert.equal(MATT_CATALOG.length, 21, "sanity: the transcribed fixture itself must carry all 21 supported skills");

// A synthetic catalog that shares NO names with MATT_CATALOG above — proves
// the picker is driven by whatever the CLI reports, not a second hard-coded
// list living in the webview.
const SYNTHETIC_CATALOG = [
  { name: "zz-first-synthetic", sourcePath: "skills/zzz/zz-first-synthetic", category: "ZettaCategory", description: "d1" },
  { name: "zz-second-synthetic", sourcePath: "skills/zzz/zz-second-synthetic", category: "ZettaCategory", description: "d2" },
  { name: "zz-third-synthetic", sourcePath: "skills/yyy/zz-third-synthetic", category: "YottaCategory", description: "d3" },
];

function mattPack(overrides) {
  return Object.assign(
    { id: "matt-pocock-skills", state: "installed", managed: true, retired: false, selected: [], revision: "rev:1", catalog: MATT_CATALOG, warnings: [] },
    overrides
  );
}

function superpowersPack(overrides) {
  return Object.assign({ id: "superpowers", state: "absent", managed: false, retired: false, selected: [], revision: "rev:sp1", catalog: [] }, overrides);
}

function otherPacks() {
  return [
    superpowersPack(),
    { id: "andrej-karpathy-skills", state: "absent", managed: false, retired: false, selected: [], revision: "" },
  ];
}

function pushMattState(postToWebview, mattOverrides) {
  postToWebview({ type: "state", payload: baseState({ packs: [mattPack(mattOverrides), ...otherPacks()] }) });
}

function openCustomize(document) {
  document.querySelector('[data-pack-customize="matt-pocock-skills"]').click();
}

function skillCheckbox(document, name) {
  return document.querySelector(`[data-picker-skill="${name}"]`);
}

function categoryCheckbox(document, category) {
  return document.querySelector(`[data-picker-category="${category}"]`);
}

function checkedSkillNames(document) {
  return [...document.querySelectorAll("[data-picker-skill]")]
    .filter((el) => el.checked)
    .map((el) => el.getAttribute("data-picker-skill"))
    .sort();
}

function pickerOverlay(document) {
  return document.querySelector("[data-picker-overlay]");
}

// ---- 1. renders from the reported catalog, never a hard-coded list -------

test("the picker renders groups and skills from the CLI-reported catalog, not a hard-coded list", () => {
  const { document, postToWebview } = loadWebviewDom();
  postToWebview({ type: "state", payload: baseState({ packs: [mattPack({ catalog: SYNTHETIC_CATALOG, selected: [] }), ...otherPacks()] }) });

  openCustomize(document);

  const renderedNames = [...document.querySelectorAll("[data-picker-skill]")].map((el) => el.getAttribute("data-picker-skill")).sort();
  assert.deepStrictEqual(renderedNames, ["zz-first-synthetic", "zz-second-synthetic", "zz-third-synthetic"]);

  const renderedCategories = [...document.querySelectorAll("[data-picker-group]")].map((el) => el.getAttribute("data-picker-group")).sort();
  assert.deepStrictEqual(renderedCategories, ["YottaCategory", "ZettaCategory"]);

  // None of the real Matt catalog's names leaked in from a second,
  // hard-coded source.
  assert.equal(skillCheckbox(document, "tdd"), null, "a real Matt skill name must not appear when the reported catalog never mentioned it");
  assert.equal(skillCheckbox(document, "ask-matt"), null);
});

// ---- 2/3. initial selection: mirrors installed, never invents one -------

test("with nothing installed the picker opens with nothing selected, and singles no skill out", () => {
  const { document, postToWebview } = loadWebviewDom();
  pushMattState(postToWebview, { selected: [] });

  openCustomize(document);

  assert.deepStrictEqual(checkedSkillNames(document), [], "a fresh open must not pre-select anything on the user's behalf");
  assert.equal(document.querySelector("[data-picker-skill-badge]"), null, "no skill carries a recommendation badge — the list is flat");
});

test("with nothing pending, Apply is disabled — there is nothing to apply", () => {
  const { document, postToWebview } = loadWebviewDom();
  pushMattState(postToWebview, { selected: [] });

  openCustomize(document);

  assert.equal(document.querySelector("[data-picker-apply]").disabled, true);
  skillCheckbox(document, "tdd").click();
  assert.equal(document.querySelector("[data-picker-apply]").disabled, false, "one pending addition re-enables Apply");
});

test("Apply is disabled when the pending set is back to exactly what is installed", () => {
  const { document, postToWebview } = loadWebviewDom();
  pushMattState(postToWebview, { selected: ["tdd"] });

  openCustomize(document);
  assert.equal(document.querySelector("[data-picker-apply]").disabled, true, "unchanged selection is not an apply");

  skillCheckbox(document, "tdd").click(); // now empty -> a removal, which IS a change
  assert.equal(document.querySelector("[data-picker-apply]").disabled, false);
});

test("with an existing selection, the picker reproduces exactly that selection", () => {
  const { document, postToWebview } = loadWebviewDom();
  pushMattState(postToWebview, { selected: ["tdd", "handoff", "teach"] });

  openCustomize(document);

  assert.deepStrictEqual(checkedSkillNames(document), ["handoff", "tdd", "teach"]);
});

// ---- 4/5. bulk toggles -----------------------------------------------

test("toggling a category selects every skill in it and only that category", () => {
  const { document, postToWebview } = loadWebviewDom();
  pushMattState(postToWebview, { selected: ["tdd"] }); // tdd is Engineering

  openCustomize(document);
  categoryCheckbox(document, "Productivity").click();

  const productivityNames = MATT_CATALOG.filter((s) => s.category === "Productivity").map((s) => s.name).sort();
  const checked = checkedSkillNames(document);
  for (const name of productivityNames) assert.ok(checked.includes(name), `expected ${name} (Productivity) to be checked`);

  // Only Productivity was touched — Engineering keeps exactly its installed
  // member, neither cleared nor expanded to the rest of the category.
  const engineeringChecked = checked.filter((n) => MATT_CATALOG.find((s) => s.name === n).category === "Engineering");
  assert.deepStrictEqual(engineeringChecked, ["tdd"]);
});

test("the whole-pack control selects exactly the reported catalog size", () => {
  const { document, postToWebview } = loadWebviewDom();
  pushMattState(postToWebview, { selected: ["tdd"] });

  openCustomize(document);
  document.querySelector("[data-picker-select-all]").click();

  assert.equal(checkedSkillNames(document).length, 21);
  assert.deepStrictEqual(checkedSkillNames(document), MATT_CATALOG.map((s) => s.name).sort());
});

test("the whole-pack control is symmetric: a second click clears the entire pack", () => {
  const { document, postToWebview } = loadWebviewDom();
  pushMattState(postToWebview, { selected: ["tdd"] });

  openCustomize(document);
  const control = () => document.querySelector("[data-picker-select-all]");
  control().click();
  assert.equal(checkedSkillNames(document).length, 21);

  control().click();
  assert.deepStrictEqual(checkedSkillNames(document), [], "the same control must turn the whole pack back off");
});

test("the whole-pack control reports how much of the pack is selected", () => {
  const { document, postToWebview } = loadWebviewDom();
  pushMattState(postToWebview, { selected: ["tdd"] });

  openCustomize(document);
  const count = document.querySelector("[data-picker-count]");
  assert.ok(count, "expected a selected-count element");
  assert.equal(count.textContent, "1 of 21 selected");

  document.querySelector("[data-picker-select-all]").click();
  assert.equal(document.querySelector("[data-picker-count]").textContent, "21 of 21 selected");
});

// ---- 6. pending additions/removals count ------------------------------

test("the pending additions and removals count is shown before applying", () => {
  const { document, postToWebview } = loadWebviewDom();
  pushMattState(postToWebview, { selected: ["tdd"] });

  openCustomize(document);
  skillCheckbox(document, "ask-matt").click(); // +1 addition
  skillCheckbox(document, "tdd").click(); // -1 removal (deselect the baseline's only member)

  const summary = document.querySelector("[data-picker-pending]");
  assert.ok(summary, "expected a pending-changes summary element");
  assert.equal(summary.textContent, "1 to add, 1 to remove.");
});

// ---- 7. apply --------------------------------------------------------

test("apply posts the desired set with the last-read revision", () => {
  const { document, postToWebview, sentMessages } = loadWebviewDom();
  pushMattState(postToWebview, { selected: ["tdd"], revision: "rev:abc123" });

  openCustomize(document);
  skillCheckbox(document, "handoff").click();
  document.querySelector("[data-picker-apply]").click();

  const applies = sentMessages.filter((m) => m.type === "pack-select");
  assert.equal(applies.length, 1);
  assert.deepStrictEqual(
    { type: applies[0].type, id: applies[0].id, skills: [...applies[0].skills].sort(), expectedRevision: applies[0].expectedRevision },
    { type: "pack-select", id: "matt-pocock-skills", skills: ["handoff", "tdd"], expectedRevision: "rev:abc123" }
  );
});

// ---- 8. conflict -------------------------------------------------------

test("a conflict response surfaces a re-read prompt and does not re-apply the stale set", () => {
  const { document, postToWebview, sentMessages } = loadWebviewDom();
  pushMattState(postToWebview, { selected: ["tdd"], revision: "rev:stale" });

  openCustomize(document);
  skillCheckbox(document, "ask-matt").click(); // a real pending change — Apply only posts a diff
  document.querySelector("[data-picker-apply]").click();
  assert.equal(sentMessages.filter((m) => m.type === "pack-select").length, 1, "expected the first apply to post");

  postToWebview({
    type: "pack-result",
    id: "matt-pocock-skills",
    ok: false,
    code: "conflict",
    message: "the selection has changed since it was last read; re-read pack-status and try again",
  });

  const conflictEl = document.querySelector("[data-picker-conflict]");
  assert.ok(conflictEl, "expected a conflict prompt element in the picker");
  assert.equal(conflictEl.hidden, false, "the conflict prompt must be visible");
  assert.notEqual(conflictEl.textContent, "", "the conflict prompt must carry a re-read message");

  // Clicking Apply again must NOT silently resend the stale request.
  document.querySelector("[data-picker-apply]").click();
  assert.equal(sentMessages.filter((m) => m.type === "pack-select").length, 1, "a conflict must never trigger a silent re-apply of the stale set");
});

// ---- 9. successful apply renders the response's own selection ------------

test("a successful apply renders the resulting selection from the response", () => {
  const { document, postToWebview } = loadWebviewDom();
  pushMattState(postToWebview, { selected: ["tdd"], revision: "rev:1" });

  openCustomize(document);
  skillCheckbox(document, "handoff").click();
  document.querySelector("[data-picker-apply]").click();

  postToWebview({
    type: "pack-result",
    id: "matt-pocock-skills",
    ok: true,
    selected: ["handoff", "tdd"],
    revision: "rev:2",
  });

  assert.equal(pickerOverlay(document).hidden, true, "a successful apply must close the picker");
  const summaryEl = document.querySelector('[data-pack-summary="matt-pocock-skills"]');
  assert.equal(summaryEl.textContent, "2 of 21 installed", "the row summary must render from the response's own selected[], not a stale read");
});

// ---- 9b. Bug 2 (owner feedback 2026-07-27, reproduced deterministically):
// a successful apply must be authoritative for lastState immediately, with
// no pack-status round-trip — a reopened picker must never show
// pre-mutation state. ------------------------------------------------------

test("a successful apply updates the row summary and a reopened picker from the response, with no pack-status round-trip", () => {
  const { document, postToWebview } = loadWebviewDom();
  // The exact owner repro: tdd installed and selected, deselect it (-> an
  // empty pending selection, so apply takes the two-click removal-confirm
  // path), the host performs the removal and replies ok:true selected:[].
  pushMattState(postToWebview, { selected: ["tdd"], revision: "rev:A" });

  openCustomize(document);
  skillCheckbox(document, "tdd").click(); // pending selection now empty
  document.querySelector("[data-picker-apply]").click(); // first click: arms remove-confirm
  document.querySelector("[data-picker-apply]").click(); // second click: actually applies

  // No {type:"state"} push happens here — the row summary and a reopened
  // picker must reflect the mutation from the pack-result ALONE, never wait
  // for a later pack-status refresh.
  postToWebview({ type: "pack-result", id: "matt-pocock-skills", ok: true, selected: [], revision: "rev:B" });

  const summaryEl = document.querySelector('[data-pack-summary="matt-pocock-skills"]');
  assert.equal(summaryEl.textContent, "Not installed", "expected the row summary to reflect the response's own (empty) selected[], not the pre-mutation count");

  // Reopening must read the MERGED lastState, not the stale pre-apply
  // selection: the picker mirrors what is installed, so an empty selection
  // reopens empty — never the stale pre-mutation ["tdd"].
  openCustomize(document);
  assert.deepStrictEqual(checkedSkillNames(document), [], "expected the reopened picker to reflect the post-apply (empty) selection — never the stale pre-mutation \"tdd\" still checked");
});

test("a conflict or failed apply leaves the cached selection untouched", () => {
  const { document, postToWebview } = loadWebviewDom();
  pushMattState(postToWebview, { selected: ["tdd", "handoff"], revision: "rev:1" });

  openCustomize(document);
  skillCheckbox(document, "ask-matt").click(); // pending edit: +ask-matt
  document.querySelector("[data-picker-apply]").click();

  // Adversarial shape: ok:false but WITH a selected[] present — the real CLI
  // contract never sends selected on a failure/conflict (spec §3.1: "a
  // conflict failure carries neither"), so this defensively proves the ok:
  // true gate itself, not merely that a normal conflict message (which lacks
  // selected entirely) happens to no-op.
  postToWebview({
    type: "pack-result",
    id: "matt-pocock-skills",
    ok: false,
    code: "conflict",
    message: "the selection has changed since it was last read; re-read pack-status and try again",
    selected: ["ask-matt"],
  });

  // The refused apply changed nothing on disk (spec §3.1) — lastState must
  // stay exactly the pre-apply selection, not the attempted pending edit and
  // not empty.
  const summaryEl = document.querySelector('[data-pack-summary="matt-pocock-skills"]');
  assert.equal(summaryEl.textContent, "2 of 21 installed", "expected the row summary to stay at the pre-apply count after a conflict");

  openCustomize(document); // reopen (re-clicking Customize) — must reproduce the ORIGINAL selection, never the attempted/failed one
  assert.deepStrictEqual(checkedSkillNames(document), ["handoff", "tdd"], "expected the reopened picker to reproduce the pre-apply selection exactly");
});

// ---- 10. row summary: partial vs complete ------------------------------

test("the row summary distinguishes a partial selection from a complete one", () => {
  const { document, postToWebview } = loadWebviewDom();

  pushMattState(postToWebview, { selected: ["tdd", "handoff", "teach", "ask-matt", "grilling", "triage"] }); // 6 of 21
  assert.equal(document.querySelector('[data-pack-summary="matt-pocock-skills"]').textContent, "6 of 21 installed");

  pushMattState(postToWebview, { selected: MATT_CATALOG.map((s) => s.name) }); // all 21
  assert.equal(document.querySelector('[data-pack-summary="matt-pocock-skills"]').textContent, "All 21 installed");

  pushMattState(postToWebview, { selected: [] }); // none
  assert.notEqual(
    document.querySelector('[data-pack-summary="matt-pocock-skills"]').textContent,
    "21 of 21 installed",
    "an empty selection must never be worded as if some/all are installed"
  );
});

// ---- 11. empty selection = removal, with confirmation ---------------------

test("an empty selection is treated as pack removal and asks for the same confirmation", () => {
  const { document, postToWebview, sentMessages } = loadWebviewDom();
  pushMattState(postToWebview, { selected: ["tdd"], revision: "rev:1" });

  openCustomize(document);
  skillCheckbox(document, "tdd").click(); // pending selection now empty

  document.querySelector("[data-picker-apply]").click();
  assert.equal(sentMessages.filter((m) => m.type === "pack-select").length, 0, "the first click on an empty selection must not apply yet");
  const confirmEl = document.querySelector("[data-picker-remove-confirm]");
  assert.ok(confirmEl, "expected a remove-confirmation element");
  assert.equal(confirmEl.hidden, false);
  assert.notEqual(confirmEl.textContent, "");

  document.querySelector("[data-picker-apply]").click(); // second, confirming click
  const applies = sentMessages.filter((m) => m.type === "pack-select");
  assert.equal(applies.length, 1);
  // Spread into a host-realm array: applies[0].skills was built inside the
  // jsdom window's own realm, whose Array constructor differs from Node's —
  // assert.deepStrictEqual's strict prototype check would otherwise flag two
  // structurally-identical empty arrays as unequal across realms.
  assert.deepStrictEqual([...applies[0].skills], []);
});

// ---- 12. superpowers: no subset affordance -------------------------------

test("superpowers exposes exactly one control and no subset affordance", () => {
  const { document, postToWebview } = loadWebviewDom();
  postToWebview({ type: "state", payload: baseState({ packs: [mattPack(), superpowersPack()] }) });

  assert.ok(document.querySelector('[data-pack-toggle="superpowers"]'), "expected superpowers to keep its one toggle control");
  assert.equal(document.querySelector('[data-pack-customize="superpowers"]'), null, "superpowers must never expose a Customize picker");
});

// ---- 13. multi-root safety (webview-side half) ---------------------------
// The host-side half (the pack-set spawn itself must target the folder the
// user actually picked, never folder zero) is pinned in
// pack_install.test.js, mirroring its existing multi-root pack-action test.
// This half proves the WEBVIEW never lets a picker session opened against
// one read silently survive to post against state it was never shown.

test("a selection read for one workspace folder is never applied to another", () => {
  const { document, postToWebview, sentMessages } = loadWebviewDom();
  // Folder A's read.
  pushMattState(postToWebview, { selected: ["tdd"], revision: "rev:folderA" });
  openCustomize(document);
  skillCheckbox(document, "handoff").click();

  // While the picker sits open, a fresh state push arrives — e.g. because the
  // window's selected workspace folder changed to a DIFFERENT project with
  // its own independent revision. The open picker must not go on to apply
  // using folder A's now-superseded revision.
  pushMattState(postToWebview, { selected: [], revision: "rev:folderB" });

  document.querySelector("[data-picker-apply]").click();

  const applies = sentMessages.filter((m) => m.type === "pack-select");
  assert.equal(applies.length, 1);
  assert.notEqual(applies[0].expectedRevision, "rev:folderA", "must never post a stale revision read from a since-superseded folder");
  assert.equal(applies[0].expectedRevision, "rev:folderB", "must post the most recently read revision, never the one captured at open time");
});

// ---- 14. dependency closure (S4, docs/spec-skill-packs.md §4.2, proof 15) -
//
// The picker computes; the CLI only refuses (§3.1). Every case below reads
// `requires` from MATT_CATALOG's entries above (MATT_REQUIRES, its own
// hand-transcribed oracle of spec §4.2's 15-edge table) — never a second
// edge list hard-coded in this file's assertions.

test("picker_closure: checking a skill auto-includes its direct requires", () => {
  const { document, postToWebview } = loadWebviewDom();
  pushMattState(postToWebview, { selected: [] });

  openCustomize(document);
  skillCheckbox(document, "grill-with-docs").click();

  assert.deepStrictEqual(checkedSkillNames(document), ["domain-modeling", "grill-with-docs", "grilling"]);
});

test("picker_closure_transitive: checking a skill auto-includes its full transitive requires chain", () => {
  const { document, postToWebview } = loadWebviewDom();
  pushMattState(postToWebview, { selected: [] });

  openCustomize(document);
  skillCheckbox(document, "implement").click();

  assert.deepStrictEqual(checkedSkillNames(document), ["code-review", "implement", "setup-matt-pocock-skills", "tdd"]);
});

test("picker_cascade: unchecking a direct dependency cascades its dependent off; the dependency's OWN dependency stays", () => {
  const { document, postToWebview } = loadWebviewDom();
  pushMattState(postToWebview, { selected: ["implement", "tdd", "code-review", "setup-matt-pocock-skills"] });

  openCustomize(document);
  skillCheckbox(document, "code-review").click(); // uncheck

  const checked = checkedSkillNames(document);
  assert.equal(checked.includes("implement"), false, "implement requires code-review — it must cascade off");
  assert.equal(checked.includes("code-review"), false);
  assert.equal(checked.includes("setup-matt-pocock-skills"), true, "setup-matt-pocock-skills is code-review's OWN dependency, not a dependent — it stays checked (no orphan cleanup)");
  assert.equal(checked.includes("tdd"), true, "tdd is unrelated to code-review's cascade and stays checked");
});

test("picker_cascade_transitive: unchecking a transitive dependency cascades every dependent off, up the whole chain", () => {
  const { document, postToWebview } = loadWebviewDom();
  pushMattState(postToWebview, { selected: ["implement", "tdd", "code-review", "setup-matt-pocock-skills", "handoff"] });

  openCustomize(document);
  skillCheckbox(document, "setup-matt-pocock-skills").click(); // uncheck

  // implement and code-review are both transitive dependents of
  // setup-matt-pocock-skills (implement -> code-review -> setup-matt-pocock-skills)
  // and must cascade off together; tdd and the unrelated handoff stay.
  assert.deepStrictEqual(checkedSkillNames(document), ["handoff", "tdd"]);
});

test("picker_cascade_last_dependent: removing the only dependent of a skill leaves that skill selected", () => {
  const { document, postToWebview } = loadWebviewDom();
  // code-review is the ONLY installed skill here that requires
  // setup-matt-pocock-skills.
  pushMattState(postToWebview, { selected: ["code-review", "setup-matt-pocock-skills"] });

  openCustomize(document);
  skillCheckbox(document, "code-review").click(); // uncheck the last (only) dependent

  assert.deepStrictEqual(checkedSkillNames(document), ["setup-matt-pocock-skills"], "the dependency stays selected even though its last dependent left");
});

test("picker_category_selectall: a category select-all pulls in a cross-category dependency, but only that dependency", () => {
  const { document, postToWebview } = loadWebviewDom();
  pushMattState(postToWebview, { selected: [] });

  openCustomize(document);
  categoryCheckbox(document, "Engineering").click();

  const checked = checkedSkillNames(document);
  const engineeringNames = MATT_CATALOG.filter((s) => s.category === "Engineering").map((s) => s.name);
  for (const name of engineeringNames) assert.ok(checked.includes(name), `expected ${name} (Engineering) to be checked`);
  assert.ok(checked.includes("grilling"), "grilling (Productivity) is required by several Engineering skills and must be pulled in");

  const productivityChecked = checked.filter((n) => MATT_CATALOG.find((s) => s.name === n).category === "Productivity");
  assert.deepStrictEqual(productivityChecked, ["grilling"], "no other Productivity skill is pulled in — Productivity declares no edges of its own");
});

test("picker_open_normalization: a legacy non-closed install opens healed, with Apply enabled and a migration note", () => {
  const { document, postToWebview } = loadWebviewDom();
  pushMattState(postToWebview, { selected: ["implement"] }); // legacy: installed without its requires

  openCustomize(document);

  assert.deepStrictEqual(checkedSkillNames(document), ["code-review", "implement", "setup-matt-pocock-skills", "tdd"], "opening must heal the selection to its transitive closure");
  assert.equal(document.querySelector("[data-picker-apply]").disabled, false, "the healed selection differs from what is installed, so Apply must be enabled");
  const noteEl = document.querySelector("[data-picker-migration-note]");
  assert.ok(noteEl, "expected a migration-note element");
  assert.notEqual(noteEl.textContent, "", "expected the migration note to explain what changed");
});

test("picker_open_normalization: an out-of-catalog leftover is dropped from the opening pending set", () => {
  const { document, postToWebview } = loadWebviewDom();
  pushMattState(postToWebview, { selected: ["grill-me", "tdd"] }); // grill-me is excluded from ZCP's catalog (spec §4.1)

  openCustomize(document);

  assert.equal(skillCheckbox(document, "grill-me"), null, "grill-me is not in the reported catalog and can never be rendered");
  assert.deepStrictEqual(checkedSkillNames(document), ["tdd"], "tdd has no requires and needed no healing; grill-me simply drops out");
  assert.equal(document.querySelector("[data-picker-apply]").disabled, false, "dropping the leftover is itself a pending change (a removal), so Apply must be enabled");
});

test("picker_omitted_requires: a catalog entry with no requires key behaves as edge-free", () => {
  const { document, postToWebview } = loadWebviewDom();
  pushMattState(postToWebview, { selected: [] });

  openCustomize(document);
  assert.doesNotThrow(() => skillCheckbox(document, "ask-matt").click());

  assert.deepStrictEqual(checkedSkillNames(document), ["ask-matt"], "ask-matt declares no requires — checking it must not pull in anything else");
});

test("picker_refusal_visible: an unclosed-selection pack-set refusal renders its message inside the open picker", () => {
  const { document, postToWebview } = loadWebviewDom();
  pushMattState(postToWebview, { selected: ["tdd"], revision: "rev:1" });

  openCustomize(document);
  skillCheckbox(document, "ask-matt").click();
  document.querySelector("[data-picker-apply]").click();

  postToWebview({
    type: "pack-result",
    id: "matt-pocock-skills",
    ok: false,
    code: "unclosed-selection",
    message: "the desired selection is missing required dependencies",
  });

  assert.equal(pickerOverlay(document).hidden, false, "the picker must stay open on a refusal");
  const refusalEl = document.querySelector("[data-picker-refusal]");
  assert.ok(refusalEl, "expected an in-picker refusal element");
  assert.equal(refusalEl.textContent, "the desired selection is missing required dependencies", "an unknown code must fall back to the backend message verbatim");
});

test("picker_apply_closed: Apply posts the dependency-closed set, and the pending summary already counted the implied changes", () => {
  const { document, postToWebview, sentMessages } = loadWebviewDom();
  pushMattState(postToWebview, { selected: [], revision: "rev:9" });

  openCustomize(document);
  skillCheckbox(document, "implement").click(); // pulls in tdd, code-review, setup-matt-pocock-skills too

  const summary = document.querySelector("[data-picker-pending]");
  assert.equal(summary.textContent, "4 to add, 0 to remove.", "the 3 implied dependency additions must be counted alongside the explicit check");

  document.querySelector("[data-picker-apply]").click();

  const applies = sentMessages.filter((m) => m.type === "pack-select");
  assert.equal(applies.length, 1);
  assert.deepStrictEqual(
    [...applies[0].skills].sort(),
    ["code-review", "implement", "setup-matt-pocock-skills", "tdd"],
    "Apply must post the full transitively-closed set, not just the explicitly checked skill"
  );
});
