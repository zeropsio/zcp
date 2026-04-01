# Knowledge Sync

ZCP's `internal/knowledge/` content is synced from canonical external sources.
The Recipe API and docs repo are the sources of truth — ZCP is a read-only consumer.

## Architecture

```
Canonical Sources                          ZCP (consumer)
────────────────                          ──────────────
Recipe API (Strapi)               pull→   internal/knowledge/recipes/*.md
  extracts.intro                          (frontmatter description)
  extracts.knowledge-base                 (operational knowledge sections)
  extracts.integration-guide              (zerops.yml + integration steps)
  environments[].import                   (Service Definitions section)
                                  ←push   (decomposed back to fragments via GitHub API PRs)

docs.zerops.io/guides/*.mdx       pull→   internal/knowledge/guides/
                                  ←push   internal/knowledge/decisions/
```

Recipe `.md` files are **gitignored** — run `zcp sync pull` (or `make sync`) before build.
Infrastructure bases (`bases/`) are committed (hand-authored, not API-sourced).

## Pull (external → ZCP)

Run **before starting work** to populate recipe knowledge:

```bash
zcp sync pull [recipes|guides|all]
zcp sync pull recipes bun-hello-world   # single recipe
zcp sync pull --dry-run                 # show what would change
```

### What pull does

**`pull recipes`** — One API call fetches all recipes (`$ne=service-utility`):
- Extracts `intro` for frontmatter `description:`
- Extracts `knowledge-base` fragment from first service (promotes H3 → H2)
- Extracts `integration-guide` (or falls back to raw `zeropsYaml`)
- Extracts environment imports for Service Definitions section
- Writes `recipes/{slug}.md`
- Handles slug remapping via `.sync.yaml` (e.g., API slug `recipe` → `nodejs-hello-world`)

**`pull guides`** — For each `docs/guides/*.mdx`:
- Strips MDX frontmatter and import statements
- Restores H1 from the `title:` frontmatter field
- Routes `choose-*` files to `decisions/`, rest to `guides/`

## Push (ZCP → external)

Run **after `make test` passes** to distribute tested changes:

```bash
zcp sync push recipes                           # all recipes → GitHub PRs
zcp sync push recipes bun-hello-world           # single recipe → one PR
zcp sync push recipes bun-hello-world --dry-run # show what would change
zcp sync push guides                            # all guides → one PR to docs repo
```

### What push does

**`push recipes`** — For each `recipes/*.md`, decomposes the monolithic file into fragments and pushes them to the app repo via GitHub API:

| Fragment | Push target | README marker |
|---|---|---|
| frontmatter `description:` | README.md | `ZEROPS_EXTRACT:intro` |
| knowledge-base (## Base Image, etc.) | README.md | `ZEROPS_EXTRACT:knowledge-base` (H2→H3) |
| integration-guide (## zerops.yml + prose) | README.md | `ZEROPS_EXTRACT:integration-guide` (H2→H3) |
| zerops.yml YAML block | `zerops.yaml` file | — |
| Service Definitions | **NOT pushed** | — (read-only reference) |

The zerops.yaml file is always derived from the integration-guide's YAML code block — single source of truth. Repo resolution tries `{slug}-app` then `{slug}` under the configured org.

**`push guides`** — Batches all guide changes into a single PR to `zeropsio/docs`:
- Preserves existing MDX frontmatter if the target file exists
- Generates starter frontmatter for new files
- Wraps bare `{var}` in `zerops://` URIs with backticks for MDX compatibility

### After push

Push creates GitHub PRs directly — no local clones needed. Review the PR, merge, then refresh the Strapi cache so the API serves the updated fragments.

## Configuration

Sync is configured via `.sync.yaml` at the project root (committed):

```yaml
api_url: https://api.zerops.io/api/recipes
slug_remap:
  recipe: nodejs-hello-world
push:
  recipes:
    org: zerops-recipe-apps
    repo_patterns: ["{slug}-app", "{slug}"]
    branch_prefix: zcp
  guides:
    repo: zeropsio/docs
    path: apps/docs/content/guides
paths:
  output: internal/knowledge
  docs_local: ""  # for pull guides (set DOCS_GUIDES env or this field)
```

