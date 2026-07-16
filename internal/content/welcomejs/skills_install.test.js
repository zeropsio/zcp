"use strict";

// Curated skills install (docs/spec-welcome-mode.md §6, W-SKILLS): the
// webview's {type:"skill-add", slug} click -> host validations (allowlist,
// reserved slug, workspace, trust, containment) -> shipped-vs-installed hash
// compare -> absent installs, identical no-ops, modified asks via a modal
// before replacing. Every validation rejects with an explicit
// {type:"skill-result", status:"error"} — never a silent drop (that's the
// allowlist GATE's job in handleMessage, covered by message_allowlist.test.js).
//
// These tests use an in-memory fs stub (not the real filesystem) for BOTH the
// shipped-content side (ctx.extensionPath/welcome-skills/<slug>/SKILL.md) and
// the workspace destination side (<ws>/.claude/skills/<slug>/SKILL.md):
// welcome.js reads shipped bytes through deps.fs like every other collector
// in this file, so one fake fs can drive shipped/installed/symlink scenarios
// deterministically without real disk I/O or platform symlink permissions.

const test = require("node:test");
const assert = require("node:assert/strict");
const path = require("node:path");
const { loadWelcome, TEST_REGISTRY, TEST_AGENT_IDS } = require("./harness.js");

const WS = "/tmp/zcp-welcomejs-skills-ws";
const SLUG = "tdd-red-green";
const SHIPPED_CONTENT = "---\nname: tdd-red-green\n---\n\nshipped body\n";
const LOCAL_EDIT_CONTENT = "locally edited content\n";

// shippedPath's prefix MUST be the REAL extensionDir loadWelcome() creates
// (never a fabricated path): open()'s readWelcomeHtml reads welcome.html via
// the real fs module from ctx.extensionPath, unconditionally, on every open —
// so ctx.extensionPath has to resolve to an actual directory regardless of
// what deps.fs fakes for the shipped-skill reads below. Only the SKILL.md
// path itself is fake — deps.fs.readFileSync intercepts it before any real
// disk access, so no file needs to actually exist there.
function shippedPath(extensionDir, slug) {
  return path.join(extensionDir, "welcome-skills", slug, "SKILL.md");
}

function destPath(slug) {
  return path.join(WS, ".claude", "skills", slug, "SKILL.md");
}

// makeMemFs is a minimal in-memory stand-in for deps.fs, covering every
// method welcome.js's skill-install code calls: existsSync/lstatSync/
// realpathSync/readFileSync (reads + containment checks), mkdirSync/
// writeFileSync/renameSync (the atomic install write), and watch (so
// startWatchers' zembed/cred/guided watch() calls, made unconditionally on
// every open(), don't throw past their own try/catch).
function makeMemFs() {
  const files = new Map(); // absolute path -> content
  const dirs = new Set(); // absolute path -> is a (plain) directory
  const symlinks = new Set(); // absolute path -> is a symlink
  const calls = { mkdir: [], writeFile: [], rename: [] };

  function has(p) {
    return files.has(p) || dirs.has(p) || symlinks.has(p);
  }

  function markDirs(p) {
    let cur = p;
    for (;;) {
      if (dirs.has(cur)) return;
      dirs.add(cur);
      const parent = path.dirname(cur);
      if (parent === cur) return;
      cur = parent;
    }
  }

  function enoent(op, p) {
    const err = new Error("ENOENT: no such file or directory, " + op + " '" + p + "'");
    err.code = "ENOENT";
    return err;
  }

  return {
    __seedFile(p, content) {
      files.set(p, content);
      markDirs(path.dirname(p));
    },
    __seedSymlink(p) {
      symlinks.add(p);
    },
    __ensureDir(p) {
      markDirs(p);
    },
    __fileContent(p) {
      return files.get(p);
    },
    __calls: calls,

    existsSync(p) {
      return has(p);
    },
    lstatSync(p) {
      if (symlinks.has(p)) return { isSymbolicLink: () => true, isDirectory: () => false, isFile: () => false };
      if (dirs.has(p)) return { isSymbolicLink: () => false, isDirectory: () => true, isFile: () => false };
      if (files.has(p)) return { isSymbolicLink: () => false, isDirectory: () => false, isFile: () => true };
      throw enoent("lstat", p);
    },
    realpathSync(p) {
      if (!has(p)) throw enoent("realpath", p);
      return p; // no real target redirection modeled — lstat already flags symlink-ness
    },
    readFileSync(p) {
      if (!files.has(p)) throw enoent("open", p);
      return files.get(p);
    },
    mkdirSync(p) {
      calls.mkdir.push(p);
      markDirs(p);
    },
    writeFileSync(p, data) {
      calls.writeFile.push({ path: p, data });
      files.set(p, data);
      markDirs(path.dirname(p));
    },
    renameSync(from, to) {
      if (!files.has(from)) throw enoent("rename", from);
      calls.rename.push({ from, to });
      files.set(to, files.get(from));
      files.delete(from);
    },
    watch() {
      return { close() {} };
    },
  };
}

function openWelcome(extraDeps, fsImpl) {
  const { stub, extensionDir, welcome } = loadWelcome();
  const ctx = { subscriptions: [], extensionPath: extensionDir };
  const deps = Object.assign(
    {
      REGISTRY: TEST_REGISTRY,
      ALL_AGENT_IDS: TEST_AGENT_IDS,
      readZembedEnv: () => null,
      runAgentAction: () => {},
      homeDir: "/nonexistent/zcp-welcomejs-home",
      workspaceRoot: WS,
      workspaceFolders: [WS],
      fs: fsImpl,
      showWarningMessage: async () => undefined,
    },
    extraDeps
  );
  welcome.open(ctx, deps);
  const panel = stub.panels.find((p) => p.viewType === "zeropsWelcome");
  return { stub, panel, welcome, ctx, deps, extensionDir };
}

function skillResults(panel) {
  return panel.postedMessages.filter((m) => m.type === "skill-result");
}

// flush drains a couple of microtask+macrotask rounds — handleSkillAdd
// crosses at most one real await (deps.showWarningMessage), but flushing
// unconditionally keeps every test robust regardless of which branch it hits.
function flush(rounds = 3) {
  let p = Promise.resolve();
  for (let i = 0; i < rounds; i++) p = p.then(() => new Promise((resolve) => setImmediate(resolve)));
  return p;
}

test("an unknown slug is rejected with an error result and no writes", async () => {
  const fsImpl = makeMemFs();
  const { panel } = openWelcome({}, fsImpl);

  panel.webview.__fireMessage({ type: "skill-add", slug: "not-a-real-skill" });
  await flush();

  const results = skillResults(panel);
  assert.equal(results.length, 1);
  assert.equal(results[0].slug, "not-a-real-skill");
  assert.equal(results[0].status, "error");
  assert.equal(typeof results[0].message, "string");
  assert.equal(fsImpl.__calls.writeFile.length, 0);
});

test('"guided" is rejected even though it is never in SKILLS', async () => {
  const fsImpl = makeMemFs();
  const { panel, welcome } = openWelcome({}, fsImpl);

  assert.equal(welcome.SKILLS.some((s) => s.slug === "guided"), false, "guided must never be a member of SKILLS");

  panel.webview.__fireMessage({ type: "skill-add", slug: "guided" });
  await flush();

  const results = skillResults(panel);
  assert.equal(results.length, 1);
  assert.equal(results[0].status, "error");
  assert.equal(fsImpl.__calls.writeFile.length, 0);
});

test("no workspace folder open is rejected", async () => {
  const fsImpl = makeMemFs();
  const { panel, extensionDir } = openWelcome({ workspaceRoot: null }, fsImpl);
  fsImpl.__seedFile(shippedPath(extensionDir, SLUG), SHIPPED_CONTENT);

  panel.webview.__fireMessage({ type: "skill-add", slug: SLUG });
  await flush();

  const results = skillResults(panel);
  assert.equal(results.length, 1);
  assert.equal(results[0].status, "error");
  assert.equal(fsImpl.__calls.writeFile.length, 0);
});

test("an untrusted workspace is rejected", async () => {
  const fsImpl = makeMemFs();
  const { panel, extensionDir } = openWelcome({ isTrusted: false }, fsImpl);
  fsImpl.__seedFile(shippedPath(extensionDir, SLUG), SHIPPED_CONTENT);

  panel.webview.__fireMessage({ type: "skill-add", slug: SLUG });
  await flush();

  const results = skillResults(panel);
  assert.equal(results.length, 1);
  assert.equal(results[0].status, "error");
  assert.equal(fsImpl.__calls.writeFile.length, 0);
});

test("a fresh install mkdirs, writes via tmp+rename, and reports installed", async () => {
  const fsImpl = makeMemFs();
  const { panel, extensionDir } = openWelcome({}, fsImpl);
  fsImpl.__seedFile(shippedPath(extensionDir, SLUG), SHIPPED_CONTENT);

  panel.webview.__fireMessage({ type: "skill-add", slug: SLUG });
  await flush();

  assert.deepStrictEqual(skillResults(panel), [{ type: "skill-result", slug: SLUG, status: "installed" }]);
  assert.ok(fsImpl.__calls.mkdir.length >= 1, "expected at least one mkdir call");
  assert.equal(fsImpl.__calls.writeFile.length, 1, "expected exactly one write (the tmp file)");
  assert.equal(fsImpl.__calls.rename.length, 1, "expected exactly one rename (tmp -> final)");
  assert.equal(fsImpl.__calls.rename[0].to, destPath(SLUG));
  assert.notEqual(fsImpl.__calls.writeFile[0].path, destPath(SLUG), "must write to a TEMP path, never the final path directly");
  assert.equal(fsImpl.__fileContent(destPath(SLUG)), SHIPPED_CONTENT);
});

test("installing again over an identical file makes zero writes and reports installed-current", async () => {
  const fsImpl = makeMemFs();
  const { panel, extensionDir } = openWelcome({}, fsImpl);
  fsImpl.__seedFile(shippedPath(extensionDir, SLUG), SHIPPED_CONTENT);
  fsImpl.__seedFile(destPath(SLUG), SHIPPED_CONTENT); // already installed, byte-identical

  panel.webview.__fireMessage({ type: "skill-add", slug: SLUG });
  await flush();

  assert.deepStrictEqual(skillResults(panel), [{ type: "skill-result", slug: SLUG, status: "installed-current" }]);
  assert.equal(fsImpl.__calls.writeFile.length, 0);
  assert.equal(fsImpl.__calls.rename.length, 0);
});

test("a locally modified file is not overwritten without confirmation", async () => {
  const fsImpl = makeMemFs();
  let asked = 0;
  let askedModal = null;
  const { panel, extensionDir } = openWelcome(
    {
      showWarningMessage: async (_message, options) => {
        asked++;
        askedModal = options;
        return undefined; // dismissed
      },
    },
    fsImpl
  );
  fsImpl.__seedFile(shippedPath(extensionDir, SLUG), SHIPPED_CONTENT);
  fsImpl.__seedFile(destPath(SLUG), LOCAL_EDIT_CONTENT);

  panel.webview.__fireMessage({ type: "skill-add", slug: SLUG });
  await flush();

  assert.equal(asked, 1, "expected exactly one modal confirmation");
  assert.deepStrictEqual(askedModal, { modal: true });
  assert.equal(fsImpl.__calls.writeFile.length, 0, "must not write before/without confirmation");
  assert.deepStrictEqual(skillResults(panel), [{ type: "skill-result", slug: SLUG, status: "kept" }]);
  assert.equal(fsImpl.__fileContent(destPath(SLUG)), LOCAL_EDIT_CONTENT, "file must be untouched");
});

test('the modal returning "Replace" performs an atomic replace and reports replaced', async () => {
  const fsImpl = makeMemFs();
  const { panel, extensionDir } = openWelcome({ showWarningMessage: async () => "Replace" }, fsImpl);
  fsImpl.__seedFile(shippedPath(extensionDir, SLUG), SHIPPED_CONTENT);
  fsImpl.__seedFile(destPath(SLUG), LOCAL_EDIT_CONTENT);

  panel.webview.__fireMessage({ type: "skill-add", slug: SLUG });
  await flush();

  assert.deepStrictEqual(skillResults(panel), [{ type: "skill-result", slug: SLUG, status: "replaced" }]);
  assert.equal(fsImpl.__calls.rename.length, 1);
  assert.equal(fsImpl.__fileContent(destPath(SLUG)), SHIPPED_CONTENT);
});

test("dismissing the modal keeps the file untouched and reports kept", async () => {
  const fsImpl = makeMemFs();
  const { panel, extensionDir } = openWelcome({ showWarningMessage: async () => undefined }, fsImpl);
  fsImpl.__seedFile(shippedPath(extensionDir, SLUG), SHIPPED_CONTENT);
  fsImpl.__seedFile(destPath(SLUG), LOCAL_EDIT_CONTENT);

  panel.webview.__fireMessage({ type: "skill-add", slug: SLUG });
  await flush();

  assert.deepStrictEqual(skillResults(panel), [{ type: "skill-result", slug: SLUG, status: "kept" }]);
  assert.equal(fsImpl.__fileContent(destPath(SLUG)), LOCAL_EDIT_CONTENT);
});

test("a symlinked .claude component is rejected and nothing is written", async () => {
  const fsImpl = makeMemFs();
  const { panel, extensionDir } = openWelcome({}, fsImpl);
  fsImpl.__seedFile(shippedPath(extensionDir, SLUG), SHIPPED_CONTENT);
  fsImpl.__seedSymlink(path.join(WS, ".claude"));

  panel.webview.__fireMessage({ type: "skill-add", slug: SLUG });
  await flush();

  const results = skillResults(panel);
  assert.equal(results.length, 1);
  assert.equal(results[0].status, "error");
  assert.equal(fsImpl.__calls.writeFile.length, 0);
});

test("a symlinked .claude/skills/<slug> component is rejected", async () => {
  const fsImpl = makeMemFs();
  const { panel, extensionDir } = openWelcome({}, fsImpl);
  fsImpl.__seedFile(shippedPath(extensionDir, SLUG), SHIPPED_CONTENT);
  fsImpl.__ensureDir(path.join(WS, ".claude"));
  fsImpl.__ensureDir(path.join(WS, ".claude", "skills"));
  fsImpl.__seedSymlink(path.join(WS, ".claude", "skills", SLUG));

  panel.webview.__fireMessage({ type: "skill-add", slug: SLUG });
  await flush();

  const results = skillResults(panel);
  assert.equal(results.length, 1);
  assert.equal(results[0].status, "error");
  assert.equal(fsImpl.__calls.writeFile.length, 0);
});

test("state reports absent/installed-current/installed-modified per slug via the shipped-content hash", async () => {
  const fsImpl = makeMemFs();
  const { panel, extensionDir } = openWelcome({}, fsImpl);
  const slugs = ["tdd-red-green", "plan-before-code", "debug-scientifically", "review-before-done", "ship-small"];
  for (const s of slugs) fsImpl.__seedFile(shippedPath(extensionDir, s), "shipped-" + s);
  fsImpl.__seedFile(destPath("plan-before-code"), "shipped-plan-before-code"); // current
  fsImpl.__seedFile(destPath("debug-scientifically"), "edited locally"); // modified

  panel.webview.__fireMessage({ type: "ready" });

  const payload = panel.postedMessages.find((m) => m.type === "state").payload;
  const bySlug = Object.fromEntries(payload.skills.map((s) => [s.slug, s.state]));
  assert.equal(bySlug["tdd-red-green"], "absent");
  assert.equal(bySlug["plan-before-code"], "installed-current");
  assert.equal(bySlug["debug-scientifically"], "installed-modified");
  assert.equal(bySlug["review-before-done"], "absent");
  assert.equal(bySlug["ship-small"], "absent");
});

test("state reports an empty skills list when no workspace folder is open", () => {
  const fsImpl = makeMemFs();
  const { panel } = openWelcome({ workspaceRoot: null }, fsImpl);

  panel.webview.__fireMessage({ type: "ready" });

  const payload = panel.postedMessages.find((m) => m.type === "state").payload;
  assert.deepStrictEqual(payload.skills, []);
});

test("a successful install pushes fresh state reflecting the new installed-current status", async () => {
  const fsImpl = makeMemFs();
  const { panel, extensionDir } = openWelcome({}, fsImpl);
  fsImpl.__seedFile(shippedPath(extensionDir, SLUG), SHIPPED_CONTENT);

  panel.webview.__fireMessage({ type: "skill-add", slug: SLUG });
  await flush();

  const stateMsgs = panel.postedMessages.filter((m) => m.type === "state");
  assert.ok(stateMsgs.length >= 1, "expected a state push after the install action");
  const last = stateMsgs[stateMsgs.length - 1];
  const row = last.payload.skills.find((s) => s.slug === SLUG);
  assert.equal(row.state, "installed-current");
});
