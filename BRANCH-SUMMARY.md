# refactor/recipe-knowledge-system

## What this branch does

Three changes, each solving a distinct problem:

### 1. Recipes come from the API, not hand-written files

All recipe `.md` files are now pulled dynamically from the Zerops Recipe API via `zcp sync pull`. Recipe files are gitignored — no more drift between ZCP's knowledge and the canonical app repos. New recipes appear automatically, no hardcoded lists.

**Before:** 29 hand-written recipe files and 17 runtime guides committed to the repo, manually maintained, drifting from the actual app repos.

**After:** One API call fetches all 33 non-utility recipes. `guides/` and `decisions/` also pulled from the docs repo. Only infrastructure bases (`bases/`) and platform themes (`themes/`) remain committed.

### 2. Each recipe is standalone

Runtime guides no longer get prepended to framework recipes. Laravel doesn't get generic PHP advice injected — it carries its own operational knowledge via the `knowledge-base` fragment system in app READMEs.

**Before:** `GetRecipe("laravel")` prepended the PHP runtime guide. Laravel has its own PHP needs (Composer, Artisan, queue workers) that differ from bare PHP — the generic advice created confusion.

**After:** `GetRecipe` prepends only platform universals (truly universal). Runtime-specific knowledge lives in each recipe's own `knowledge-base` fragment. The hello-world recipe for each runtime IS the runtime guide.

### 3. Bootstrap gets concrete reference implementations instead of abstract docs

A new service definitions library gives the bootstrap agent access to complete, proven `import.yaml` configurations from the Recipe API.

**Before:** Bootstrap's provision step had nothing — the agent read abstract schema from `core.md` and composed every `import.yaml` from first principles. Every field, every service block, every relationship between services was guessed from documentation.

**After:** For any runtime the agent encounters, it can look up a real, working `import.yaml` that includes full service block structure with correct type versions, priority ordering, autoscaling shape, managed service patterns, and comments explaining why each value was chosen. For composite stacks (bun + nextjs + postgres + valkey), it looks up multiple recipes, extracts the relevant service entries from each, and merges them — instead of inventing everything from abstract rules.

`TransformForBootstrap` adapts recipe imports for interactive use (strips `buildFromGit`/`zeropsSetup`, adds `startWithoutCode` on dev services, keeps all proven scaling values).
