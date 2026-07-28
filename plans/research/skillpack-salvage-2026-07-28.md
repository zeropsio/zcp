# Research: skill-pack salvage inventory (archived/welcome-ux-redesign)

Ticket: `plans/agent-first-onboarding-2026-07-28/tickets/04-research-skillpack-salvage.md`.
Method: read-only, via `git show archived/welcome-ux-redesign:<path>`, `git log`, and
`git diff main...archived/welcome-ux-redesign` (triple-dot: merge-base..archived tip). The
archived branch was never checked out or merged. Merge-base of `main` and
`archived/welcome-ux-redesign` is `68f78120` — i.e. `main`'s current tip — so `archived` forked
from exactly where `main` sits today; the diff below is a clean "what archived adds" view, not an
approximation.

## 0. FACT — the premise in the ticket/CLAUDE.md needs a correction

CLAUDE.md's map line says "curated skills" for `docs/spec-welcome-mode.md` §6 on main, and the
dispatching message called main's baseline "the older curated-skills model." That description is
**stale**. `main` already ships a full community multi-agent skill-pack installer — landed in one
commit, **independently of the archived branch**, one day before archived's merge-base:

- `d0be6787` "feat(welcome): content redesign v3 + multi-agent skill packs (ext 0.1.8 -> 0.1.13)"
  (2026-07-23), reachable from `main` HEAD. It **deleted** the old embedded-curated-skills code
  (`internal/content/welcome_skills.go`, `internal/content/templates/welcome-skills/*/SKILL.md`,
  `welcomejs/skills_install.test.js`) and added `internal/skillpacks/` (17 non-test .go files,
  ~2,500 lines) plus `zcp skills pack-add`/`pack-remove`/`pack-status` (`cmd/zcp/skills.go`).
- `docs/spec-welcome-mode.md` §6 on `main` (`internal/init/adapters/claude.go` is its code owner)
  still describes the **pre-d0be6787** model ("v1 ships embedded curated skills... Community
  whole-repo packs are out until a trust/update story exists") — `d0be6787` touched no file under
  `docs/`. The spec is out of sync with `main`'s own code, independent of anything on the archived
  branch. There is no `docs/spec-skill-packs.md` on `main`.

So the real starting point for a port is: **main already has atomic, whole-pack community
skill-pack install/remove for 4 packs** (`matt-pocock-skills`, `superpowers`,
`andrej-karpathy-skills`, `anthropic-skills`), each cloned live and installed to both
`.agents/skills/` and `.claude/skills/`, with a manifest v2 (generation, pinned commit, per-skill
digests), cross-process flock, staged-build/no-replace publish, resource caps, and a CLI `--json`
contract already wired into the current welcome webview (`{type:"pack-action", id,
action:"add"|"remove"}` / `{type:"pack-details"}`, host state key `packsStatusCache`). What the
archived branch adds on top is **granular per-skill selection inside that existing system**, not
a new system from scratch.

## 1. FACT — the archived contract (`docs/spec-skill-packs.md`)

Full doc: `archived/welcome-ux-redesign:docs/spec-skill-packs.md` (241 lines, new file — no
predecessor on main). Model:

- **Review granularity** (new axis, `ReviewGranularity`): `ReviewRepositoryLevel` (whole
  discovered repo installs as one unit — `andrej-karpathy-skills`, `anthropic-skills`, unchanged
  from main's current behavior) vs `ReviewSkillLevel` (catalog enumerates every installable skill
  by exact name+path; upstream content outside that list is never installed — `matt-pocock-skills`,
  `superpowers`).
- **Selection granularity** (new axis, `SelectionGranularity`, orthogonal to review):
  `SelectionAtomic` (whole pack or nothing — default, and Superpowers even though it's
  skill-level-reviewed) vs `SelectionSubset` (user picks an explicit subset — Matt only).
- Matt Pocock's Skills: reviewed surface is pinned to **exactly 22 skills** (17 Engineering + 5
  Productivity, spec §4.1), excluding personal/misc/in-progress/deprecated upstream content.
  `setup-matt-pocock-skills` is the default-selected recommended entry point.
- Superpowers: reviewed at skill level (14 named skills, spec §5) but installs/removes as one
  atomic unit — "a partial on-disk set is incomplete, never a valid custom selection."
- **Applying a selection is declarative and revision-gated** (spec §3.1): caller states the full
  desired set; the implementation derives add/remove itself (never additive). A read
  (`pack-status`) returns an **opaque selection revision**; apply (`pack-set`) requires the
  caller's last-read revision, and a mismatch is a `conflict` result with **zero writes** — closes
  a lost-update race between two open Welcome windows. Additions install from the pack's already-
  pinned manifest commit, never current upstream HEAD. The whole reconciliation is preflighted
  before any mutation (byte-identical workspace on refusal, including mid-way failure).
- **Migration**: a pre-existing whole-repo Matt install (main's current atomic behavior, i.e. any
  install made *before* this port lands) is migrated on first `pack-set`: skills outside the
  reviewed 22 are reported and **detached** (kept on disk, ownership released), never silently
  deleted and never silently kept as "selected."
- `docs/spec-welcome-mode.md` §6 changed on archived (`archived:docs/spec-welcome-mode.md:495-505`)
  from a self-contained ~15-line section to a 10-line pointer that defers entirely to
  `spec-skill-packs.md`, plus one added bullet: `zcp init` must create both skill roots
  **unconditionally**, before any agent session, so Claude Code/Codex's native filesystem watchers
  see them at session start (spec §2). Everything else in archived's spec-welcome-mode.md §0
  (0.1–0.10, "persistent adaptive hub" product model — five-slot shell, agent-onboarding journey,
  Data Studio capability card, manual-tutorial routes) is the **UI redesign**, unrelated to the
  skill-pack contract — see §4 below on what NOT to port.

## 2. FACT — the implementation footprint (Go + extension)

`git diff main...archived/welcome-ux-redesign --stat` (58 files, +11096/−208) contains **UI-redesign
noise** (hub shell, journey, Data Studio, package-lock.json, plans/*) mixed with skill-pack work.
Isolating the skill-pack-relevant files:

**Go, `internal/skillpacks/`** (all deltas are additive on top of files `main` already has via
`d0be6787` — no file is wholesale replaced):
| File | Delta | What it adds |
|---|---|---|
| `catalog.go` | +165/−4 | `ReviewGranularity`/`SelectionGranularity` types, `Pack.Review`/`.Selection`/`.Skills` fields, `Pack.IsAtomic()`, the 22-entry `mattPocockSkills` + 14-entry `superpowersSkills` tables |
| `set.go` | **new file**, 586 lines | `PackSet(ctx, cwd, id, desired []string, expectedRevision string) (Result, error)` — the declarative revision-gated apply; quarantine-based staged removal/rollback |
| `discover.go` | +48 | `filterDiscoveredToCatalog` — intersects the walker's raw discovery with a `ReviewSkillLevel` pack's catalog; errors (not silently skips) if a catalogued skill is missing upstream |
| `status.go` | +68 | `PackStatus.Revision`/`.Selected`/`.Catalog` fields; `computeRevision` (pure SHA-256 of id+generation+sorted selected names); `catalogSkillsFor` |
| `errors.go` | +5 | `CodeConflict`, `CodeNotSkillLevel`, `CodeUnknownSkill`, `CodeDuplicateSkill`, `CodeAtomicPartial` |
| `git.go` | +78 | `fetchCommit` (init+remote-add+shallow-fetch-of-one-SHA+checkout) — additions to an existing pack install from its **pinned commit**, distinct from `cloneRepo`'s branch-tip clone used for a fresh install |
| `add.go` | +11 | `Result.Revision`/`.Selected` fields (empty for Add/Remove); wires `filterDiscoveredToCatalog` into the existing fresh-install path |

No manifest schema/version bump — `computeRevision` is a pure function over the existing v2
manifest's `Generation` + skill names, so no on-disk migration of already-installed packs is
needed for the revision/selection mechanism itself (only the review-granularity migration in §1
applies, and only to Matt).

**CLI**, `cmd/zcp/skills.go` (+170/−?, `archived diff` above): new `pack-set <id> --skills <csv>
--expected-revision <rev> [--json]` subcommand; `mutationJSON`/`statusEntryJSON` gain
`revision`/`selected`/`catalog` fields (catalog only for `ReviewSkillLevel` packs).

**`internal/init/init.go`** +27/−0: new unconditional init step `{"Skill roots",
generateSkillRoots}` — `main` has no equivalent; today `.agents/skills/`/`.claude/skills/` on
main are created only as a side effect of the first `pack-add`/guided run, not pre-created.

**Extension (webview), `internal/content/templates/vscode-bootstrap-welcome.{html,js}`**: the
skill-pack-relevant slice is commit `026ddb26` (+512 html / +230 js) — Matt's Customize picker
(category/skill checkboxes, "N to add, M to remove" pending summary, Select-all), atomic
Superpowers copy, honest same-session-discovery copy. This is entangled in the same files as the
hub-shell/journey/Data-Studio UI commits (`1ded6a5c`, `1b473d84`, `756ddd27`, `e92ec6d1`) — no
clean file-level boundary; a port must re-implement the picker's *behavior* against the new UI
shell, not transplant HTML/JS.

**Tests pinning the contract**: `internal/content/welcomejs/pack_picker.test.js` (new, 442 lines —
picker renders from the CLI-reported `catalog` field, never a second hard-coded list; default
selection; category/select-all bulk toggles; pending-count summary; `pack-select` apply message
shape) and `internal/content/welcomejs/pack_install.test.js` (+363 — allowlist gate for the new
`pack-select` message: unknown id, non-array, non-string entry, entry outside catalog, duplicate
entry, over-length array, missing/empty `expectedRevision`, `conflict` result path, busy-lock
sharing, multi-root cwd pin). Go-side: `catalog_test.go` +171, `set_test.go` +611 (new),
`status_test.go` +124, `add_test.go` +109, `git_test.go` +71, `internal/init/init_test.go` +91.

**Version-bump pin**: `main` is currently at extension `0.1.18` (`BootstrapExtVersion` in
`internal/init/adapters/claude.go` / `vscode-bootstrap-package.json`, parity-pinned per
`TestBootstrapExtVersion_ParityWithManifest`, CLAUDE.md trap). Archived's tip is at `0.1.27` (9
bumps across its 27 commits — one per template-touching slice). A port slice that edits either
welcome template file **must** bump `BootstrapExtVersion` in the same commit or the change never
reaches a running fleet (code-server reloads off the versioned extensions.json index, not file
mtimes) — this is an existing, already-documented trap, not a new one.

**Gitignored/synced content**: none specific to skill packs. The only gitignore-adjacent addition
is `internal/content/welcomejs/node_modules/` (`.gitignore` +4, `Makefile` +2 for `npm ci` in
`make setup`) — this is the jsdom test harness's own node_modules, orthogonal to skill-pack
functionality (needed by any port that carries `pack_picker.test.js`/`pack_install.test.js`, since
those are jsdom-backed).

## 3. FACT — the functional contract a new panel UI must design against

**Per-pack operations** (all packs, unchanged from main): `pack-add`/`pack-remove` (atomic,
whole-pack) via `{type:"pack-action", id, action:"add"|"remove"}`; `pack-status` (read-only, no
network) drives all UI state, generation-guarded against stale/superseded runs; `pack-details`
reveals the output channel.

**Additional, for a `ReviewSkillLevel` + `SelectionSubset` pack (Matt only in the current
catalog)**: a picker reads `state.packs[].{selected, revision, catalog}` (catalog = array of
`{name, sourcePath, category, description}`) and posts `{type:"pack-select", id, skills: [...],
expectedRevision}` on Apply. Host spawns `zcp skills pack-set <id> --skills <csv>
--expected-revision <rev> --json`, and the response's own `selected[]`/`revision` are threaded
straight into the pushed `state` (no forced extra `pack-status` round-trip, though one still
happens per the existing 4 refresh triggers: panel-ready, reveal/focus, watcher-debounced FS
change, post-mutation).

**Skill/pack states** (`PackStatus.State`, unchanged enum, now carrying more fields): `absent`,
`installed`, `incomplete` (some files missing), `modified` (local drift, digest mismatch),
`broken` (legacy pre-v2 or corrupt manifest — `pack-set`/picker must refuse and point at
`pack-remove` for manual cleanup, since a `broken` pack's `Selected`/`Catalog` aren't guaranteed
meaningful), plus `retired` (bool: manifest exists for an id no longer in the active catalog —
`pack-set` must still be usable for a retired ReviewSkillLevel pack for **removal**, matching the
existing `pack-remove`-ignores-catalog rule cited in `catalog.go`'s doc comment).

**Failure modes surfaced to the picker specifically**: `conflict` (stale `expectedRevision` —
webview must have been left open while a status changed elsewhere; correct UI response is
"someone else changed the selection, reloading" then a forced `pack-status` re-read, never a
silent retry with the stale revision), `not-skill-level` (picker opened against a repository-level
pack — should be structurally impossible if the picker only renders for packs with a non-empty
`catalog`), `unknown-skill`/`duplicate-skill` (webview-side bug, should never reach the CLI given
the allowlist gate — defense in depth), `atomic-partial` (Superpowers can't take a partial set —
again should be structurally impossible from the atomic-only UI, defense in depth). All are
existing `CodedError` codes (`internal/skillpacks/errors.go`), machine-readable, mapped to fixed
row copy by the (JS-side) panel per the existing `pack-result` convention.

**One caller-visible ordering rule**: a `pack-select` apply must be **preflighted-then-atomic** —
either the full reconciliation lands or the workspace is byte-identical; the picker cannot assume
partial progress on any error path, including a failure in the *removal* half after additions were
already planned (spec §3.1, `TestPackSet_*` proofs in `archived:internal/skillpacks/set_test.go`).

## 4. Port sketch

**Carry forward (skill-pack functionality, largely additive on top of what main already has):**

| Commit | Carries |
|---|---|
| `b07cad18` | `docs/spec-skill-packs.md` (new) — needs a rewrite pass, not a straight cherry-pick: its §1 framing ("two granularities... Matt/Superpowers reviewed this way, `andrej-karpathy-skills`/`anthropic-skills` reviewed that way") already matches what would exist post-port, but its cross-references into `spec-welcome-mode.md` §0 (adaptive-hub) must be re-pointed at whatever the new UI's spec actually calls its skills surface |
| `eaa8f73d` + the catalog.go/errors.go halves of `36d920d2` | `ReviewGranularity`/`SelectionGranularity` axes, `CatalogSkill`, the 22-skill Matt table + 14-skill Superpowers table, the 5 new error codes — this is the foundational data-model change everything else sits on |
| `36d920d2`'s `internal/skillpacks/set.go` + `git.go`'s `fetchCommit` + `status.go`'s revision/selected/catalog fields + `add.go`'s `filterDiscoveredToCatalog` wiring | `PackSet`, the declarative revision-gated apply, migration-safe (`splitReviewedAndLegacyExtra`) |
| `36d920d2`'s `cmd/zcp/skills.go` half | `pack-set` CLI subcommand + JSON contract |
| `b3df531f`'s `internal/init/init.go` half | unconditional skill-roots init step |
| the *behavioral spec* of `026ddb26`'s picker (not its HTML/JS) | Customize picker semantics: default selection, category/select-all bulk toggle, pending add/remove count, `pack-select` message shape, conflict handling — re-implement against the new UI |
| Go-side tests for all the above (`catalog_test.go`, `set_test.go`, `status_test.go`, `add_test.go`, `git_test.go`, `init_test.go` deltas) | replay as RED before the port's GREEN, per `/flow` discipline |
| `.gitignore`/`Makefile` node_modules/npm-ci additions | only if the port carries jsdom-backed JS tests (`pack_picker.test.js`, `pack_install.test.js` rewritten against the new UI) |

**Do NOT port** (UI redesign — explicitly being redone from scratch, no skill-pack functionality):
`1ded6a5c` (adaptive-hub 5-slot shell), `1b473d84`/`16ebb60a` (Data Studio capability card — a
different feature entirely, not skills), `756ddd27`/`97b15991` (agent-onboarding journey +
versioned journey state), `e92ec6d1`/`e30f5636` (s7-ux density pass) — and correspondingly none of
`archived:docs/spec-welcome-mode.md` §0 (0.1–0.10). The plan/journal commits (`171717c1`,
`c045e96f`, `cc89cb92`, `5bc0ed2d`, `d37f3cfa`, `27c4ffb7`, `7cb4eb2b`, `3e59a47d`, `8c971461`,
`e029fc30`'s `plans/welcome-adaptive-hub-2026-07-27.md`) are transient `/flow` journal entries for
the superseded effort — never cite them as a source (CLAUDE.md's own rule), and don't port them.

**Landmines:**
- **The archived spec's §1/§4/§5 text was written assuming main was still on the pre-`d0be6787`
  embedded-curated-skills model.** It isn't — verify every cross-reference in
  `archived:docs/spec-skill-packs.md` against `main`'s *actual current* `internal/skillpacks/`
  code before promoting it, not against what the archived branch's own `docs/spec-welcome-mode.md`
  said main was.
- **Extension version bump is mandatory on the same commit as any welcome-template edit** — carry
  the discipline (bump `BootstrapExtVersion` + `vscode-bootstrap-package.json` together), not any
  specific version number (`0.1.27` is meaningless on a different base — main is at `0.1.18` now
  and will have moved further by port time).
- **The Matt-pack review-granularity migration is a real, live concern, not a hypothetical**: any
  user who has already run `pack-add matt-pocock-skills` on current `main` (or on
  `feat/agent-first-onboarding` before this port lands) has the **whole discovered repo** installed
  under the old repository-level behavior. The first `pack-set` against that installation must hit
  `splitReviewedAndLegacyExtra`'s detach path, not silently delete the user's non-catalogued
  skills or silently claim them as "selected." Write a test for this specific transition on the
  target branch, not just replay the archived one (the archived test fixture may assume different
  starting state than what's actually on `feat/agent-first-onboarding`).
- **No clean file-level cherry-pick exists for the picker UI** — `026ddb26` and every later UI
  commit share the same two template files with the hub/journey/Data-Studio work. Treat
  `pack_picker.test.js`/`pack_install.test.js` as the port's specification-by-test for the new
  UI's picker behavior, and write fresh HTML/JS against it.
- **`internal/skillpacks/` file inventory differs between `main` and what the earlier top-level
  diff stat implies**: `main` already has `copy.go`, `digest.go`, `lock.go`, `manifest.go`,
  `marker.go`, `names.go`, `remove.go`, `targets.go`, `frontmatter.go` (from `d0be6787`) that never
  show up in `git diff main...archived` because archived doesn't touch them — don't mistake their
  absence from the diff for absence from the codebase.

## Assessment

The ticket's framing of this as "port a large standalone effort onto an older baseline" overstates
the remaining work. The expensive part — a working, tested, cross-agent (`.agents/skills` +
`.claude/skills`), manifest-versioned, locked, digest-audited skill-pack installer — already
exists on `main` via `d0be6787`, unrelated to the archived branch. What archived actually built on
top, in skill-pack terms, is narrower than the ticket implies: one new axis pair on the existing
catalog type, one new CLI verb (`pack-set`) with a declarative-apply/revision-conflict algorithm,
one new init step, and one picker UI (whose visual form is explicitly being thrown away). That is
a much smaller, better-bounded port than "salvage 27 commits of a redesign effort" suggests, and
the real risk sits in the parts the ticket didn't ask about: `docs/spec-skill-packs.md`'s prose
was written against a self-image of main (the old embedded-skills model) that main no longer
matches, so it needs an editing pass before promotion, not a copy.
