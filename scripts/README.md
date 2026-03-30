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
  services[].zeropsYaml                   (## zerops.yml section)
                                  ←push   (split back to fragment + yaml in app repos)

docs.zerops.io/guides/*.mdx       pull→   internal/knowledge/guides/
                                  ←push   internal/knowledge/decisions/
```

Recipe `.md` files are **gitignored** — run `scripts/sync-knowledge.sh pull` before build.
Infrastructure bases (`bases/`) are committed (hand-authored, not API-sourced).

## Pull (external → ZCP)

Run **before starting work** to populate recipe knowledge:

```bash
./scripts/sync-knowledge.sh pull [guides|recipes|all]
```

### What pull does

**`pull recipes`** — One API call fetches all recipes (`$ne=service-utility`):
- Extracts `intro` for frontmatter `description:`
- Extracts `knowledge-base` fragment from first service (promotes H3 → H2)
- Extracts `zeropsYaml` from first service
- Writes `recipes/{slug}.md` with description, knowledge-base sections, and `## zerops.yml`
- Handles slug remapping (e.g., API slug `recipe` → `nodejs-hello-world`)

**`pull guides`** — For each `docs/guides/*.mdx`:
- Strips MDX frontmatter and import statements
- Restores H1 from the `title:` frontmatter field
- Routes `choose-*` files to `decisions/`, rest to `guides/`

## Push (ZCP → external)

Run **after `make test` passes** to distribute tested changes:

```bash
./scripts/sync-knowledge.sh push [guides|recipes|all]
```

### What push does

**`push recipes`** — For each `recipes/*.md` with a matching local app clone:
- Extracts knowledge-base portion (everything before `## zerops.yml`)
- Demotes headings: H2 → H3
- Replaces `knowledge-base` fragment in app README (or appends if new)
- Extracts YAML from the `## zerops.yml` code block
- Writes back to `zerops.yaml` in the app repo
- Tries `{slug}-app/` then `{slug}/` naming conventions

**`push guides`** — For each `internal/knowledge/guides/*.md` and `decisions/*.md`:
- Preserves existing MDX frontmatter if the target file exists
- Generates starter frontmatter for new files
- Wraps bare `{var}` in `zerops://` URIs with backticks for MDX compatibility

## After push

Push only writes to local cloned repos. Review diffs, commit, and push to GitHub:

```bash
# Check what changed
./scripts/sync-knowledge.sh push recipes

# Review
cd ~/www/recipe-apps/bun-hello-world-app && git diff

# Commit and push
git add -A && git commit -m "sync: update knowledge-base fragment" && git push
```

Then refresh the Strapi cache so the API serves the updated fragments.

## Environment variables

Override sibling repo locations if they're not at the default:

```bash
DOCS_GUIDES=~/code/docs/guides RECIPE_APPS=~/code/apps ./scripts/sync-knowledge.sh pull all
```
