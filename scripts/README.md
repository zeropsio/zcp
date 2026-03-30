# Knowledge Sync

ZCP's `internal/knowledge/` content is synced from canonical external sources.
Docs and app READMEs are the sources of truth — ZCP is a read-only consumer.

## Architecture

```
Canonical Sources                          ZCP (consumer)
────────────────                          ──────────────
docs.zerops.io/guides/*.mdx       pull→   internal/knowledge/guides/
                                  ←push   internal/knowledge/decisions/

recipe-apps/*-hello-world-app/    pull→   internal/knowledge/recipes/*-hello-world.md
  README.md (knowledge-base)              (knowledge-base sections + zerops.yml)
  zerops.yaml                     ←push   (split back to fragment + yaml)
```

## Pull (external → ZCP)

Run **before starting work** to catch external edits:

```bash
./scripts/sync-knowledge.sh pull [guides|runtimes|recipes|all]
```

### What pull does

**`pull guides`** — For each `docs/guides/*.mdx`:
- Strips MDX frontmatter (`---` block) and import statements
- Restores H1 from the `title:` frontmatter field
- Un-escapes MDX backtick wrapping around `zerops://` URIs
- Routes `choose-*` files to `decisions/`, rest to `guides/`

**`pull runtimes`** — For each `recipe-apps/{runtime}-hello-world-app/`:
- Extracts `knowledge-base` fragment from README.md (between `ZEROPS_EXTRACT_START/END` markers)
- Promotes headings: H3 → H2
- Reads `zerops.yaml` (or `zerops.yml`) from the app repo
- Combines into `recipes/{runtime}-hello-world.md`:
  - H1 title
  - Knowledge-base content (binding, sizing, gotchas)
  - `## zerops.yml` section with "> Reference implementation" framing + full YAML in code block

**`pull recipes`** — Same pattern for framework recipes via `scripts/recipe-map.txt`.

## Push (ZCP → external)

Run **after `make test` passes** to distribute tested changes:

```bash
./scripts/sync-knowledge.sh push [guides|runtimes|recipes|all]
```

### What push does

**`push guides`** — For each `internal/knowledge/guides/*.md` and `decisions/*.md`:
- Preserves existing MDX frontmatter if the target file exists (docs owns frontmatter)
- Generates starter frontmatter for new files (title from H1, description from TL;DR)
- Strips H1 (frontmatter `title:` replaces it)
- Wraps bare `{var}` in `zerops://` URIs with backticks for MDX compatibility

**`push runtimes`** — For each `recipes/{runtime}-hello-world.md`:
- Extracts knowledge-base portion (everything before `## zerops.yml`)
- Demotes headings: H2 → H3
- Replaces `knowledge-base` fragment in app README (or appends if new)
- Extracts YAML from the `## zerops.yml` code block
- Writes back to `zerops.yaml` in the app repo

**`push recipes`** — Same pattern for framework recipes (needs `recipe-map.txt`).

## After push

Push only writes to local cloned repos. Review diffs, commit, and push to GitHub:

```bash
# Check what changed
./scripts/sync-knowledge.sh push runtimes

# Review
cd ~/www/recipe-apps/bun-hello-world-app && git diff

# Commit and push
git add -A && git commit -m "sync: update knowledge-base fragment" && git push
```

## Recipe file format

Each hello-world recipe in `internal/knowledge/recipes/` follows this structure:

```markdown
# Bun Hello World on Zerops

## Keywords
bun, bunx, hono, elysia, javascript, typescript

## TL;DR
One-line summary.

## Binding
Runtime-specific binding rules.

## Resource Requirements
Dev vs prod RAM recommendations.

## Common Mistakes
Operational gotchas.

## Gotchas
Detailed gotchas with bold labels.

## zerops.yml
> Reference implementation — learn the patterns, adapt to your project.

​```yaml
zerops:
  - setup: prod
    ...
  - setup: dev
    ...
​```
```

The knowledge-base sections (Keywords through Gotchas) are what gets synced to/from the
app README's `knowledge-base` fragment. The `## zerops.yml` section is synced to/from the
app's `zerops.yaml` file.

## Environment variables

Override sibling repo locations if they're not at `~/www/`:

```bash
DOCS_GUIDES=~/code/docs/guides RECIPE_APPS=~/code/apps ./scripts/sync-knowledge.sh pull all
```
