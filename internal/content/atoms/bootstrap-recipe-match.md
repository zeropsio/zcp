---
id: bootstrap-recipe-match
priority: 1
phases: [bootstrap-active]
routes: [recipe]
steps: [discover]
title: "Recipe matched — the recipe owns the plan"
coverageExempt: "recipe+discover step — 30 canonical scenarios cover recipe at provision + close; discover step is a one-shot transition before route is committed (<1% session frequency where this atom is the actionable signal)"
---

### The recipe owns the shape — confirm, don't author

The matched recipe's import YAML is the authoritative shape. ZCP builds the plan
from it — you do NOT write the plan or pick the service `type` or mode. Accept
the recipe as-is by completing discover with NO plan:

`zerops_workflow action="complete" step="discover"` — omit `plan`.

### Submit a plan ONLY to adjust what the recipe leaves to you

| You can change | How | Stays the recipe's (→ `route="classic"` to alter) |
|---|---|---|
| A runtime hostname that collides with an existing service | submit that runtime with a non-colliding `devHostname` (and `stageHostname` for a pair) inside `runtime` | `type`, `zeropsSetup` / mode, `buildFromGit`, dev/stage pairing |
| A managed dependency you already have | submit it with `resolution: "EXISTS"`, keeping the recipe's hostname | managed `hostname` — the repo's `${hostname_*}` refs break on rename |

A partial plan is fine — list only the runtime(s) you rename; everything else
fills in from the recipe. A managed service of a different type than the recipe
expects → `route="classic"`.

Do not write code — `buildFromGit` pulls the app repo at import. (Container only;
in local mode the recipe repo is cloned into your working directory instead.)
