# Recipe pipeline refactor — one source of truth instead of zcp + app repo + Strapi

**Surfaced**: 2026-09-13 — owner decision while triaging the eval farm
(`plans/public-access-route-recovery-2026-09-13.md §2`). A recipe today is
assembled from three places that drift independently: the app repo
(`zerops-recipe-apps/<slug>-app`, its `zerops.yaml` is what the platform
actually builds), Strapi (`api.zerops.io/api/recipes`: import yaml, gotchas,
frontmatter tags; edited via PRs to `zeropsio/recipes`) and the zcp binary
(`internal/knowledge/recipes/*.md` + `*.import.yml`, gitignored, embedded from
disk at build time). Measured on 2026-09-13: 29 recipes declare a runtime the
repo no longer uses (the JS ecosystem moved to `nodejs@24` on 2026-09-03/08, bun
1.2→1.3, deno 2.0.0→latest, mailpit alpine 3.20→3.24) and Strapi still says the
old value after a cache clear; farm candidates built from a worktree shipped with
zero recipes and nothing noticed.

**Why deferred**: the immediate defects are closed by narrower fixes (provision
check tolerant of the repo's `run.base`; corpus guard on build and on `farm
push`; the Strapi data bump is a PR outside zcp). The structural question —
which of the three is authoritative and how the other two derive from it — is a
product decision the owner wants to take as one piece, not per symptom.

**Trigger to promote**: the owner schedules the recipe rework; or a fourth
symptom of the same drift class lands on the farm after the narrower fixes.

## Owner intent (verbatim gist)

"Chci obecně řešit celý ten refaktor toho, jak se pracuje s recepty, a tu
komplexnost, kdy to závisí na spoustě věcí (zcp, repo, Strapi)."

## Parked with it

- **Matcher spellings** (`internal/knowledge/recipe_matcher.go`): "NestJS"
  matches, "Nest" / "Nest.js" / "nest app" do not (hand-written 6-entry synonym
  map; tokenizer splits on `.`). Deliberately NOT changed now — the owner's
  concern is that every regex tweak here has historically broken another
  spelling ("kočka a myš"). The refactor should replace the regex/synonym
  approach with matching against frontmatter-derived names plus a table test
  of spellings, not add one more special case.
- **Data bump**: the 29 drifted recipes need their `type:` set to what the
  repo runs (list: session scratchpad `recipe_drift.txt`, script
  `recipe_drift.py` — regenerate by comparing each `*.import.yml` `type:` with
  the repo's `zerops.yaml` `run.base`). Lands as a PR to `zeropsio/recipes`
  under the owner's GitHub identity, then `zcp sync cache-clear` + `pull`.
- Related open entries: `recipe-app-repos-type-drift-qa.md` (repo-side QA),
  `recipe-corpus-legacy-os-form.md`, `recipe-scaffold-tool.md`,
  `recipe-route-shape-contract-worker-and-dev-narrowing.md`.

## Sketch

Pick the app repo as the single source: `zerops.yaml` + a `recipe.yaml`
(frontmatter, gotchas) live next to the code; Strapi and the zcp corpus are
generated from it (sync = fetch repos, not Strapi). Then: one drift check
(`zcp sync lint`), one embed step that fails the build when the corpus is
short, and a matcher that reads names from the same file.
