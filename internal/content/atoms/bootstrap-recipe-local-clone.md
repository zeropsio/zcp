---
id: bootstrap-recipe-local-clone
priority: 1
phases: [bootstrap-active]
routes: [recipe]
environments: [local]
steps: [discover]
title: "Recipe local — clone repo into CWD"
coverageExempt: "recipe+local+discover — covered by recipe-local-flow design (Theme 1) + flow-eval-local scenarios"
---

### Local mode replaces the dev runtime with your CWD

In local mode there is no SSH-in dev workspace. Your CWD becomes the source-of-truth checkout.

```
1. Inventory existing files:
     ls -A
   The CWD typically has ZCP state — agent context files (CLAUDE.md,
   AGENTS.md), MCP configs (.mcp.json, .claude/, .codex/, .cursor/),
   and ZCP work state (.zcp/). Anything OUTSIDE that set is the user's
   work
   — stop and ask before continuing if you see it.

2. Bring the recipe content in WITHOUT clobbering ZCP state:
     git clone {{recipe.repo}} /tmp/recipe-clone
     rsync -a /tmp/recipe-clone/ ./
     rm -rf /tmp/recipe-clone

   `rsync -a` includes dotfiles (`.gitignore`, `.git`, `.env.example`)
   correctly across both bash and zsh — `cp -r src/*` skips them on
   default zsh. The trailing slashes on both paths matter:
   `/tmp/recipe-clone/` (source contents) and `./` (destination).

3. If the recipe ships its own CLAUDE.md / README.md and you want it
   to win over the ZCP-generated stub, the rsync above already
   overwrote them. If you want to keep both, rename one BEFORE the
   rsync.
```

The upstream remote is in the rsynced `.git` directory. To use your own remote later:

```
git remote set-url origin <your-repo-url>
```

`zerops.yaml` arrives in the cloned tree as-is from the recipe. ZCP transforms the project import.yml separately at provision (drops `zeropsSetup: dev` services); your local `zerops.yaml` is untouched.
