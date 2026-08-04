# Retest pack: skillpack-requires

Zero-context bar: Karel executes every step below with no other file open,
in minutes. Every step ties to an ACx.

## Run
Exact commands, each with the ONE line that means "pass":

| command | expected line |
|---|---|
| `make test-race` | every package `ok`, no `FAIL` / `DATA RACE` |
| `make lint-local` | `0 issues.` |
| `make vet-tags` | no output, exit 0 |
| `make e2e-zcp-fast` | final `PASS`, no `--- FAIL:` lines |
| `node --test internal/content/welcomejs/*.test.js` | `pass 344`, `fail 0` |

## Drive
Steps against a scratch temp dir (no live platform needed for 1-3; a live
`zcp` container for 5), each tied to an ACx.

1. **AC5** — wire shape. In a fresh temp dir: `zcp skills pack-status matt-pocock-skills --json`. Expect:
   `jq '.packs[0].catalog[] | select(.name=="implement").requires'` → `["tdd","code-review"]`; an
   entry with no declared edges (e.g. `ask-matt`) has no `requires` key at all; `jq .version` → `1`.

2. **AC2** — refusal, zero writes. Same temp dir: `zcp skills pack-set matt-pocock-skills --skills implement --expected-revision anything --json`.
   Expect: exit code `1`, `.code == "unclosed-selection"`, message names `tdd`, `code-review`, AND
   `setup-matt-pocock-skills`. Then confirm no `.zcp` directory and no `.agents/skills` /
   `.claude/skills` were created in the temp dir — the refusal is pure pre-lock validation.

3. **AC3** — live closed-set apply, real upstream clone (network required, `github.com/mattpocock/skills`).
   In a fresh temp dir:
   - `zcp skills pack-add matt-pocock-skills --json` → `.ok == true`, `.skillCount == 21`.
   - `zcp skills pack-status matt-pocock-skills --json`, capture `.packs[0].revision` as `$REV`.
   - `zcp skills pack-set matt-pocock-skills --skills "grilling,grill-with-docs,domain-modeling" --expected-revision "$REV" --json`
     → `.ok == true`, `.selected` (sorted) == `["domain-modeling","grill-with-docs","grilling"]`.
   - Then, with the FRESH revision from that response: `zcp skills pack-set matt-pocock-skills --skills "grill-with-docs" --expected-revision "<new-revision>" --json`
     → exit `1`, `.code == "unclosed-selection"`, message names `grilling` and `domain-modeling`; re-run
     `pack-status` and confirm `.selected` is UNCHANGED (still the 3-skill closed set) — the failed
     apply left the workspace untouched.

4. **AC6** — closed installed set carries no closure warning (negative case). Continuing in the same
   temp dir: `zcp skills pack-status matt-pocock-skills --json` → `.packs[0].warnings == []`. (The
   positive migration-warning case — a pre-Requires legacy manifest — is covered by
   `TestPackStatus_NonClosedInstalledSet_Warns` in the Run step above; it needs a manifest fixture
   predating this feature and isn't reachable live from a fresh install.)

5. **AC4/AC7 — FE picker manual drive** (needs a live ZCP container with the welcome panel; open the
   Customize picker for Matt's pack):
   - Check `implement` → `tdd`, `code-review`, AND `setup-matt-pocock-skills` all auto-check
     (transitive: `implement` → `code-review` → `setup-matt-pocock-skills`).
   - Uncheck `setup-matt-pocock-skills` → `code-review` AND `implement` cascade off (their dependent
     chain); `tdd` stays checked (not a dependent of `setup-matt-pocock-skills`).
   - Open the picker on a project with a legacy non-closed selection (e.g. only `implement` installed,
     predating Requires) → picker opens with `tdd`/`code-review`/`setup-matt-pocock-skills` already
     checked, a migration note is shown, and Apply is enabled (real pending diff).
   - Click Apply → the posted `--skills` set is dependency-closed (matches what's checked), and the
     pending "N to add / M to remove" summary shown before Apply already counted the implied additions.

## What changed
- S1: `CatalogSkill.Requires`, the 15-edge Matt catalog, catalog-validity lint, and the shared
  `internal/skillpacks/requirements.go` closure/violations module; `requires` added to the
  `pack-status --json` wire shape (additive, `version` stays 1).
- S2: `zcp skills pack-set` refuses a non-dependency-closed `--skills` set with the new
  `unclosed-selection` code, zero writes, checked before the lock and the revision compare (so a
  stale revision + unclosed set never returns `conflict`).
- S3: `pack-status` reports a warning (not a mutation) when an installed selection predates
  `Requires` and is non-closed; a closed installed set reports no such warning.
- S4: the Customize picker's checkbox/select-all/whole-pack/open-normalization paths all route
  through one pure reducer that auto-includes transitive dependencies on check, cascades
  dependents off (never dependencies) on uncheck, and normalizes the opening pending set to
  `closure(installed ∩ catalog)`; an `unclosed-selection` refusal now renders inside the open
  picker. `BootstrapExtVersion` bumped 0.1.32 → 0.1.33 (required for any template edit to reach a
  running fleet).

## Rollback
`git revert 26577007..3d12dfc8` — range from Run State `integration:` field (this covers every
landed slice commit plus the ASSEMBLE checkpoint doc commit; no follow-up needed after revert, the
feature has no external migration to undo).

## Docs
Spec §§ touched (promoted at GATE 1): `docs/spec-skill-packs.md` §3.1 (closure-validation clause),
§4.2 (Requires admission rule, edge table, picker closure behavior), §7 (proofs 13-16), §8
(non-goal rewrite); `docs/spec-welcome-mode.md` §7 W-SKILLS (picker closure/cascade/normalization
sentence).
