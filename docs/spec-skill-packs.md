# Skill Packs — Selection and Live Discovery

This specification defines the user-visible contract for community skill packs
installed by ZCP. It is authoritative for pack contents, selection granularity,
project skill roots, running-session expectations, and the skills surface's
behavior. The surface itself is the agent panel's skills section
(`docs/spec-welcome-mode.md` §7).

## 1. Product contract

- Skill packs are project-local. ZCP materializes every installed skill under both
  `<workspace>/.agents/skills/<name>/` and
  `<workspace>/.claude/skills/<name>/`.
- Only packs present in ZCP's reviewed catalog are installable. A repository URL,
  path, category, or skill name supplied by the panel webview is never an
  installation authority.
- Review happens at one of two granularities, and which one a pack uses is itself a
  reviewed decision:
  - **Skill-level review** — the catalog enumerates every installable skill by name
    and source path, and nothing outside that list is ever installed, whatever the
    upstream repository contains. Matt Pocock's Skills (§4) and Superpowers (§5) are
    reviewed this way, because their upstreams carry content ZCP deliberately excludes.
  - **Repository-level review** — the whole reviewed repository is the unit, and its
    complete discovered skill set installs together. `andrej-karpathy-skills` is
    reviewed this way. A repository-level pack still passes every
    structural guarantee (containment, collision refusal, atomic publish, reserved
    names, resource caps); what it does not carry is a per-skill curation decision.
  Selection granularity is a separate axis from review granularity: only a skill-level
  pack can offer a subset, and Superpowers is skill-level yet deliberately atomic (§5).
- Matt Pocock's Skills is a **customizable collection**. The user chooses an
  explicit subset of its supported skills.
- Superpowers is an **atomic framework pack**. The user installs or removes the
  complete supported set; ZCP does not offer per-skill or category subsets.
- Installing a pack never enables automatic updates. Changing the selected Matt
  subset and updating upstream content are distinct user actions. A future update
  must retain the existing selection and must not silently install newly published
  skills.

## 2. Skill roots and running sessions

`zcp init` must ensure that both `.agents/skills/` and `.claude/skills/` exist,
even when neither contains an installed community skill. The directories must
exist before ZCP launches a coding-agent session.

This ordering is a functional requirement:

- Claude Code watches an existing project `.claude/skills/` directory and makes
  added, changed, or removed skills available in the current session. Creating the
  top-level directory only after the session started can require a restart.
- Codex discovers project skills through `.agents/skills/` and invalidates its
  skill catalog when watched local skill files change.

ZCP relies on those native filesystem-discovery mechanisms. It does not implement
a custom reload protocol, send reload commands into terminals, signal agent
processes, or claim that an agent acknowledged a reload.

For a supported Claude Code or Codex version started by ZCP after `zcp init`, a
newly installed skill is expected to be available in the same session on a
subsequent user turn. The install does not alter a turn already in progress and
does not remove instructions that an invoked skill already placed into the
conversation.

If a skill does not become visible, or if an agent session predates `zcp init`,
the truthful recovery is to start a new agent session. Other agents may always
require a new session. Panel copy must express this fallback and must not state
that every agent was actively reloaded.

## 3. Catalog and selection rules

ZCP owns the installable surface of each reviewed repository. Upstream directory
layout and plugin manifests may inform that catalog, but they do not dynamically
grant installation eligibility.

Each cataloged skill has:

- one stable name and source path;
- one display category;
- a short user-facing description.

Catalog membership IS the installability decision — there is no separate per-skill
status field; a skill outside the catalog is not installable, whatever the upstream
repository contains.

Categories are presentation and bulk-selection aids, not filesystem paths.
Selecting a category means selecting every currently supported skill in that
category. “Select all” means every supported skill in the ZCP catalog, never every
`SKILL.md` that happens to exist in the upstream repository.

The installed Matt selection is durable project state. Reopening the picker shows
the exact current selection. Applying a changed selection adds newly selected
skills and removes deselected ZCP-owned skills. A deselected skill with local
changes is preserved and detached from pack ownership rather than deleted.
Foreign destinations and name collisions are never silently overwritten.

An empty Matt selection is removal of the pack and requires the same confirmation
as removing the pack directly.

### 3.1 Applying a selection

Selection is applied **declaratively**: the caller states the desired installed set and
the implementation derives the additions and removals. An additive verb cannot express
this, because one apply must both add and remove.

- Reading the current selection and applying a new one are separate operations. The read
  reports the exact selected skill names, the pack state, the catalog metadata the picker
  needs, and an **opaque selection revision**.
- Applying requires the revision the caller last read. Under the pack lock the
  implementation compares it against the stored one and, on mismatch, returns a stable
  `conflict` result **with zero writes**. Without this, a panel left open while
  another surface changed the selection would silently uninstall skills its user never
  deselected. The revision is its own value: the per-copy ownership marker's generation
  records installation ownership, not selection history, and must not be reused for this.
- Additions install from the **commit already pinned in the pack manifest**. Changing a
  selection is not an update: it must never implicitly pull newer upstream content.
  Updating pinned content is a separate, explicit user action (§1).
- The complete reconciliation — additions, destination collisions, locally-modified
  copies, removals — is **preflighted before any mutation**, and a preflight refusal
  leaves the workspace byte-identical. A plan that mutates as it walks can fail after
  earlier entries already changed, which would leave a selection that matches neither the
  old nor the new one.
- The caller-stated set must be **dependency-closed** over the pack's declared
  `Requires` edges (§4.2). A non-closed set is refused with a stable
  `unclosed-selection` result and zero writes. The check is pure input
  validation (desired set + catalog only): it runs with the request-shape
  validations, before the lock and the revision compare — so a stale revision
  combined with an unclosed set returns `unclosed-selection`, never
  `conflict`. The implementation never expands the caller's set: visible
  expansion is the picker's job, so the user always sees the full set before
  Apply and `--skills` keeps meaning "exactly this".
- A manifest written before skill-level review existed (a whole-repository install of a
  now skill-level pack) is migrated explicitly: skills outside the reviewed catalog are
  reported and detached rather than silently deleted or silently kept as selected.
- An installed selection that predates a pack's `Requires` edges and violates
  them (in-catalog skill present, its dependency absent) is a distinct third
  bucket: `pack-status` reports it as a warning; nothing is auto-installed or
  detached. It heals on the next picker Apply, because the picker's opening
  normalization (§4.2) produces a closed pending set.

## 4. Matt Pocock's Skills

### 4.1 Supported surface

Matt's upstream repository contains additional personal, miscellaneous,
in-progress, and deprecated skills. They are outside ZCP's supported surface and
must not appear in the standard picker or be installed by “Select all”.

ZCP exposes the 21 skills promoted by Matt's upstream plugin manifest:

| Category | Supported skills |
|---|---|
| Engineering | `ask-matt`, `diagnosing-bugs`, `grill-with-docs`, `triage`, `improve-codebase-architecture`, `setup-matt-pocock-skills`, `tdd`, `to-spec`, `to-tickets`, `wayfinder`, `implement`, `prototype`, `research`, `domain-modeling`, `codebase-design`, `code-review`, `resolving-merge-conflicts` |
| Productivity | `grilling`, `handoff`, `teach`, `writing-great-skills` |

`grill-me` is deliberately excluded from this surface even though Matt's
upstream carries it: its entire body is "run a `/grilling` session", so as a
peer checkbox next to `grilling` it duplicates a near-identical description
and, selected without `grilling`, installs a broken skill. `grilling` itself
is both model- and user-invocable upstream, so nothing of substance is lost.

This explicit catalog is the boundary. A newly added upstream skill requires a
reviewed ZCP catalog change before it becomes selectable.

### 4.2 Selection experience

- Opening Matt's pack presents the two categories and their individual skills.
- No category and no complete pack is implicitly installed. The picker's
  opening selection is derived from what is installed (see the dependency
  normalization below); no skill is singled out as recommended.
- The user can toggle one skill, one complete category, or all 21 supported
  skills.
- Before applying, the picker shows the resulting number of additions and removals.
- A successful apply reports the installed count and renders the resulting
  partial or complete selection. “Installed” alone must not imply that all 21 are
  present.

#### Dependencies (`Requires`)

ZCP does not infer dependencies from references in skill prose, and never at
runtime. A dependency exists only as a **reviewed catalog edge**: a
`Requires` list on a catalog skill, admitted per edge on the rule — *exclude
a pure wrapper (no standalone value, e.g. `grill-me` §4.1); declare an edge
only when the target is proven mandatory by an upstream contract or
behavioral proof; leave optional references unmodeled.* Edges are curation
data on the catalog only: never persisted in the manifest, never a
filesystem or execution input, and only a `SelectionSubset` pack may declare
any (an atomic pack's edges would be dead data).

The reviewed Matt edge set (15 edges):

| Skill | Requires |
|---|---|
| `grill-with-docs` | `grilling`, `domain-modeling` |
| `implement` | `tdd`, `code-review` |
| `improve-codebase-architecture` | `codebase-design`, `grilling` |
| `wayfinder` | `grilling`, `domain-modeling`, `research`, `setup-matt-pocock-skills` |
| `triage` | `grilling`, `setup-matt-pocock-skills` |
| `code-review` | `setup-matt-pocock-skills` |
| `to-spec` | `setup-matt-pocock-skills` |
| `to-tickets` | `setup-matt-pocock-skills` |

Closure is transitive (`implement` → `code-review` → `setup-matt-pocock-skills`).

Picker behavior over the edges — the picker computes, the CLI only refuses
(§3.1):

- Checking a skill auto-includes its transitive `Requires`, visibly, before
  Apply.
- Unchecking a dependency cascades its (transitive) dependents off.
  Dependencies stay checked — flat selection state cannot distinguish an
  auto-added dependency from one the user independently wanted, so orphan
  cleanup would remove explicit intent; the user unchecks leftovers
  manually.
- On open, the pending selection is normalized to
  `closure(installed ∩ catalog)`: a legacy non-closed installation opens
  with its dependencies visibly added and Apply enabled, and an installed
  out-of-catalog leftover drops out of pending so an Apply detaches it
  (§3.1 migration) instead of reposting it as an unknown skill.
- An `unclosed-selection` refusal (defense in depth — e.g. a stale extension
  host) renders its message inside the open picker.

## 5. Superpowers

Superpowers is presented as one complete framework pack. Its Testing, Debugging,
Collaboration, and Meta categories may be described to explain its contents, but
they are not selectable installation groups.

The supported set is:

| Category | Included skills |
|---|---|
| Testing | `test-driven-development` |
| Debugging | `systematic-debugging`, `verification-before-completion` |
| Collaboration | `brainstorming`, `writing-plans`, `executing-plans`, `dispatching-parallel-agents`, `requesting-code-review`, `receiving-code-review`, `using-git-worktrees`, `finishing-a-development-branch`, `subagent-driven-development` |
| Meta | `writing-skills`, `using-superpowers` |

The pack has one install/remove control. Installation succeeds only when the
complete supported set is installed; a partial on-disk set is incomplete and is
never presented as a valid selectable subset.

ZCP installs the Agent Skills content, not an agent-specific native plugin.
Agent-specific Superpowers plugin hooks, including the Claude SessionStart
bootstrap that injects `using-superpowers`, are not installed or emulated by the
ZCP skill-pack flow. The panel must therefore describe this as the complete
Superpowers skill set, not as the native Superpowers plugin.

A changed upstream Superpowers set requires a reviewed catalog update. Existing
installations remain pinned to their recorded content until an explicit future
update action.

## 6. Panel behavior (`spec-welcome-mode.md` §6/§7)

- Matt renders a **Customize** action with selection summary, such as
  “6 of 21 installed”.
- Superpowers renders one whole-pack install/remove toggle.
- Only one pack mutation may be in flight per panel window.
- Install/remove success is determined from pack state read back from the
  workspace, not from process output prose.
- The status model distinguishes absent, installed, incomplete, and locally
  modified content. Matt additionally exposes its selected count.
- Post-install copy says that running Claude Code and Codex sessions should
  discover the change for a subsequent turn because the roots existed at session
  start. It also gives “start a new session if it is not visible” as the fallback.
- The panel does not offer a “reload agents” action and does not claim that all
  running agents were reloaded.

## 7. Required behavioral proofs

The implementation and task breakdown are incomplete until tests prove:

1. Plain `zcp init` and `zcp init --guided` both leave `.agents/skills/` and
   `.claude/skills/` present before an agent can be launched.
2. Matt's picker exposes exactly the 21 supported skills above, grouped as
   Engineering and Productivity; personal, miscellaneous, in-progress, and
   deprecated upstream skills never appear.
3. A Matt selection installs exactly the selected supported skills; “Select all”
   installs 21, not every upstream `SKILL.md`.
4. Reopening the picker reproduces the installed Matt selection, and changing it
   applies the correct additions and removals without deleting local edits.
5. Superpowers offers no subset control and installs all 14 supported skills as
   one pack.
6. A partial Superpowers installation is reported as incomplete, not as a valid
   custom selection.
7. A Claude Code session and a Codex session started after the empty roots were
   created can use a subsequently installed test skill on a later turn without
   ZCP sending a reload command.
8. Copy and state never promise live discovery for unsupported agents and always
   provide the new-session fallback.
9. Applying a selection against a stale revision returns `conflict` and leaves the
   workspace byte-identical — no skill added, none removed, no manifest write.
10. Adding a skill to an existing selection installs it from the manifest's pinned
    commit, not from current upstream `HEAD`.
11. A reconciliation that fails partway leaves the workspace byte-identical, including
    the case where the failure falls in the removal half after additions were planned.
12. A whole-repository manifest for a skill-level pack migrates without data loss: skills
    outside the reviewed catalog are reported and detached, never silently deleted and
    never silently carried forward as selected.
13. The declared `Requires` graph is valid by construction: every edge targets a skill in
    the same pack's catalog, the graph is acyclic, no skill lists a duplicate edge, and
    only a `SelectionSubset` pack declares any edge.
14. `pack-set` refuses a non-dependency-closed desired set with `unclosed-selection` and a
    byte-identical workspace, including no lock or state artifacts; the refusal is pure
    input validation — a stale revision combined with an unclosed set returns
    `unclosed-selection`, and a closed set losing the revision race still returns
    `conflict` byte-identically.
15. The picker auto-includes transitive dependencies on check, cascades dependents off when
    a dependency is unchecked (dependencies stay), and normalizes its opening selection to
    `closure(installed ∩ catalog)` — a legacy non-closed installation opens healed with
    Apply enabled, and an out-of-catalog leftover is dropped from pending so Apply
    detaches it.
16. `pack-status` reports a warning naming the missing dependencies of a non-closed
    installed selection, in the same rendered wording as the `pack-set` refusal (one
    shared closure implementation), and reports no such warning for a closed selection.

## 8. Non-goals

- Arbitrary Git repositories or user-supplied skill paths.
- Matt's personal, miscellaneous, in-progress, or deprecated skills.
- Superpowers subsets.
- Dependency closure inferred from skill prose or computed at runtime — dependency
  edges exist only as reviewed catalog data (§4.2).
- Per-install dependency provenance, orphan cleanup of no-longer-required
  dependencies, or structured migration fields — the picker's visible closure and
  the status warning are the whole surface.
- Subset selection for repository-level packs (§1) — they install as a whole or not at all.
- Installation of native Claude, Codex, or other agent plugins and hooks.
- A ZCP agent-session registry or custom reload/acknowledgement protocol.
- Automatic pack updates.
