# B2 — recipe schema-invalid YAML: status + remaining cross-repo steps

**Bug (confirmed):** ZCP's own recipe corpus teaches a zerops.yaml the live schema
rejects — `nodejs-hello-world` put `verticalAutoscaling` under `run:` (import-yaml-only;
0 occurrences in the zerops-yml schema, which is `additionalProperties:false` under
`run:`). The platform deploy parser is lenient and ACCEPTS it; export's strict structure
validator REJECTS the bundle. All 3 runs of `export-buildfromgit-self-snapshot` failed
validation on exactly this, costing a 2 KB knowledge query + a 36.5 KB knowledge scope
pull + a 7–10-turn develop detour each.

## Done (staged on disk, this session)

Recipe `.md` files are gitignored (Strapi-synced), so these edits do NOT commit — they
are correct locally and ready for `sync push`. Verified against `TestRecipeLint`.

- `internal/knowledge/recipes/nodejs-hello-world.md` — DELETED the `run.verticalAutoscaling`
  block (scaling intent already lives, platform-honored, in `nodejs-hello-world.import.yml`
  at the prod level: `minRam 0.25`). The `.md` `run:` block now carries only valid keys.
- `internal/knowledge/recipes/gleam-hello-world.md` — `- true` → `- 'true'` in the dev
  `buildCommands` (a bare YAML boolean was being authored where a string command belongs).

Sweep: no OTHER recipe carries `verticalAutoscaling` under `run:` (class is contained to
nodejs).

## Remaining — needs Karel (cross-repo / Aleš coordination)

### 1. Publish the recipe fixes (Karel — `sync push` amplification)
```
zcp sync push recipes nodejs-hello-world --dry-run   # preview full upstream-vs-local diff FIRST
zcp sync push recipes gleam-hello-world  --dry-run
```
Per CLAUDE.md "sync push amplification": these ship the WHOLE file as fragments — if the
diff is larger than the one-block delete (nodejs) / one-line quote (gleam), STOP. Then
push → merge PRs → `zcp sync cache-clear` → `zcp sync pull recipes` → verify diff clean.

### 2. App-repo zerops.yaml (Karel — external repo PR)
`zerops-recipe-apps/nodejs-hello-world-app/zerops.yaml` carries the SAME
`run.verticalAutoscaling` block (co-authored drift). The `sync push` length guard
(`push_recipes.go:218` `len(new) >= len(existing)`) SKIPS shrinking zerops.yaml, so the
automated path won't carry this delete — it needs a manual PR removing the block.

### 3. Aleš items (flag → his OK before edit — recipe-AUTHORING scope)
Both are tracked + in Aleš's scope (recipe-generation teaching/grader content); NOT edited
this session per the "flag + discuss, then implement" protocol:
- `internal/content/workflows/recipe/phases/generate/zerops-yaml/comment-style-positive.md:24`
  — `deployFiles:` under `run:` (belongs under `build:`). Recipe-authoring agents learn a
  wrong placement from this "positive" example.
- `internal/content/examples/zerops_yaml_comment_fail_field_narration.md:22,55` — the
  "Correct shape" block shows `run: httpSupport: true` with NO `ports:` — structurally
  invalid (httpSupport lives on a port entry). The lesson there is comment STYLE; keep it
  on structurally valid yaml: `run: ports: [{port: 3000, httpSupport: true}]`.

### 4. The class-prevention lint (follow-up — AFTER step 1 merges)
Add `internal/schema/snippet.LintMarkdownYAML(file, markdown, tier)` (Complete | Fragment
tiers; placeholder normalization for scaffold templates) validating every zerops.yaml
snippet in recipes/guides/atoms against `ValidateZeropsYAMLStructure` (the schema owner),
and fix the `recipe_lint_test.go` hand-mirror-struct blindness (`yaml.Unmarshal` without
`KnownFields(true)` silently ignores unknown keys — HOW this bug passed the gate).

**Sequencing constraint (why this is a follow-up, not in the bug batch):** `TestRecipeLint`
validates the EMBEDDED store = the on-disk recipes = what CI pulls fresh from Strapi. Wiring
strict structure validation BEFORE step 1 merges would break CI on every fresh checkout
(CI pulls the still-invalid Strapi content). Ship the lint only after the recipe fixes are
live in Strapi. The Fragment-tier placeholder normalization also needs tuning against the
full ~120-atom corpus to prove zero false positives before wiring the atom path.

## Why B2 isn't a single commit like B1/B3/B5/B6/B7/B8/B9/B4

The actual fix (recipe content) lives in gitignored, Strapi-owned files + an external app
repo; the prevention lint depends on that content being live first; two sub-items are
Aleš's authoring scope. The in-repo committable part is the staged content (ephemeral) +
this handoff. Everything else is a Karel/Aleš decision, surfaced rather than silently
dropped (plan-fidelity).
