# refactor/recipe-knowledge-system

## What this branch does

Two changes, each solving a distinct problem:

### 1. Recipes come from the API, not hand-written files

All recipe `.md` files are now pulled dynamically from the Zerops Recipe API via `zcp sync pull`. Recipe files are gitignored — no more drift between ZCP's knowledge and the canonical app repos. New recipes appear automatically, no hardcoded lists.

**Before:** 29 hand-written recipe files and 17 runtime guides committed to the repo, manually maintained, drifting from the actual app repos.

**After:** One API call fetches all 33 non-utility recipes. `guides/` and `decisions/` also pulled from the docs repo. Only infrastructure bases (`bases/`) and platform themes (`themes/`) remain committed.

### 2. Each recipe is standalone

Runtime guides no longer get prepended to framework recipes. Laravel doesn't get generic PHP advice injected — it carries its own operational knowledge via the `knowledge-base` fragment system in app READMEs.

**Before:** `GetRecipe("laravel")` prepended the PHP runtime guide. Laravel has its own PHP needs (Composer, Artisan, queue workers) that differ from bare PHP — the generic advice created confusion.

**After:** `GetRecipe` prepends only platform universals (truly universal). Runtime-specific knowledge lives in each recipe's own `knowledge-base` fragment. The hello-world recipe for each runtime IS the runtime guide.
