# Slice brief: S7 — GUI recipe URL correctness: persist the Strapi slug, hand the agent a ready-made link

Self-contained: no other file is required to execute this. Cite spec §s, never the plan.

**Defect (live, owner-reported 2026-08-04)**: the agent handed out `https://app.zerops.io/recipes/nodejs-hello-world`; the working URL is `https://app.zerops.io/recipes/node-js-hello-world`. Root cause: `.sync.yaml` `slug_remap` renames the Strapi slug `node-js-hello-world` to the corpus slug `nodejs-hello-world` at pull time (`internal/sync/pull_recipes.go:99` — `slug := cfg.RemapSlug(recipe.Slug)`; `recipe.Slug` is the ORIGINAL), while the GUI detail route `/recipes/:slug` matches the STRAPI slug. The onboarding playbook (line ~133) and `docs/spec-onboarding.md` §4 instruct composing the link from `<slug>` — the corpus slug — so the agent produced a dead link for the DEFAULT recipe.

**Outcome** (observable): the corpus records each recipe's original Strapi slug (`guiSlug:` frontmatter); the recipe-route close/ownership guidance renders the COMPLETE, correct GUI URL (`https://app.zerops.io/recipes/<strapi-slug>`) so the agent copy-pastes it; the playbook and spec no longer instruct composing the URL from the corpus slug — they instruct using the surfaced link and forbid composing it. For `nodejs-hello-world` the rendered link ends in `node-js-hello-world`; for non-remapped recipes it equals the corpus slug; for a doc without `guiSlug` frontmatter (force-tracked mailpit) it falls back to the doc slug.

**Allowed scope**
- Files: `internal/sync/pull_recipes.go`, `internal/sync/pull_recipes_test.go`, `internal/knowledge/documents.go`, `internal/knowledge/documents_test.go`, `internal/workflow/recipe_corpus_store.go` (or wherever `RecipeCandidate` is populated from `Document`), `internal/workflow/route.go`/`engine.go` (RecipeMatch field + `resolveRecipeMatch` population — smallest seam that carries it to guide render), `internal/workflow/bootstrap_guide_assembly.go`(+test), `internal/knowledge/playbooks/onboarding.md`, `internal/tools/knowledge_playbook_content_test.go`, `docs/spec-onboarding.md` (§4 ownership line + O4 row wording — spec and pins move in the same commit), `docs/recipes/README.md` (frontmatter table row for `guiSlug`)
- Explicitly excluded: `.sync.yaml` (the remap stays), `internal/sync/transform.go` (frontmatter is not part of push fragments), matcher scoring, orientation playbook.

**Spec citations**: `docs/spec-workflows.md` §8 RCO (guide-injection contract — the close/ownership guidance is the injection point; follow RCO-7's pattern: data surfaced ready-made, agent never composes) · `docs/spec-onboarding.md` §4 (you are amending its ownership line; new wording: link the GUI page URL surfaced by the workflow guidance; NEVER compose it from the corpus slug — corpus slugs can differ from GUI slugs via sync remap) · `docs/spec-knowledge-architecture.md` §2 (Strapi owns the slug; zcp persists it, never re-derives via a second remap table).

**Mechanics (verified pointers)**
- Pull: at `pull_recipes.go:99` keep the original `recipe.Slug`; emit `guiSlug: "<original>"` frontmatter ALWAYS (uniform; equals the corpus slug when no remap applies). Byte-idempotency test must stay green.
- Parse: `documents.go` `parseDocument` reads `guiSlug` → `Document.GUISlug`; when absent, default `GUISlug = doc slug` (covers mailpit).
- Carry: `RecipeCandidate` + `RecipeMatch` gain `GUISlug`; `resolveRecipeMatch` (engine.go:367) and the corpus store adapter populate it.
- Render: the recipe close/ownership guidance in `bootstrap_guide_assembly.go` includes one line with the full URL `https://app.zerops.io/recipes/<GUISlug>`. Base URL as a named const in one place.

**RED test list**
- `TestBuildRecipeMarkdown_EmitsGUISlug` — layer: unit — remapped case: frontmatter `guiSlug: "node-js-hello-world"` while the file/corpus slug is `nodejs-hello-world`; non-remapped case: guiSlug == slug.
- `TestParseDocument_ReadsGUISlug_DefaultsToSlug` — layer: unit.
- `TestBootstrapGuide_OwnershipLink_UsesGUISlug` — layer: unit — for a RecipeMatch with GUISlug `node-js-hello-world`, the rendered close guidance contains the exact URL `https://app.zerops.io/recipes/node-js-hello-world` and does NOT contain `recipes/nodejs-hello-world`.
- Extend `TestPlaybookOnboarding_ContentPins_CoreContract` — layer: tool — required needle: the surfaced-link rule (e.g. `never compose it from the corpus slug` phrasing per the spec wording you land); forbidden needle: `app.zerops.io/recipes/<slug>` (the old compose-it-yourself template).

**Protocol**: RED → GREEN → REFACTOR.
1. RED: `go test ./internal/sync ./internal/knowledge ./internal/workflow -run 'GUISlug|TestBootstrapGuide_OwnershipLink' -short -count=1 -v` and `go test ./internal/tools -run TestPlaybookOnboarding -short -count=1`.
2. Implement; ladder: `go test ./internal/sync/... ./internal/knowledge/... ./internal/workflow/... ./internal/tools/... ./integration/... -short -count=1`; `go test ./internal/content/... -short -count=1` (lints).
3. `make lint-fast`.

**Report contract**: RED + GREEN outputs with exit codes · files touched · layer-matrix lines · independent-oracle note (the literal `node-js-hello-world` comes from `.sync.yaml`'s remap table + the owner's reported working URL, never from calling RemapSlug/StrapiSlugFor in the test).

**Stop conditions**: scope drift · material unknown · AC change · repeated unexplained failure. Note your worktree branches from main (post-v9.140.0) — the correct base; no merge needed. Your fresh worktree lacks the gitignored synced corpus — corpus-floor failures in unrelated packages are environment artifacts.

**Definition of Done**
- [ ] RED replay: fails at base, passes at head
- [ ] Named tests pass with `-count=1 -v`
- [ ] `make lint-fast` clean
- [ ] No file outside Allowed scope touched
- [ ] Report contract filled in full
