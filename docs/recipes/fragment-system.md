# Fragment System

The recipe system extracts marked sections from `README.md` files using a custom fragment syntax. Fragments allow the same README to serve both as a full document on GitHub and as data source for the recipe GUI/API.

## Syntax

```markdown
Lorem Ipsum ...

<!-- #ZEROPS_EXTRACT_START:intro# -->
**AI agent** environment provides a development space for AI agents to build and version the app.
<!-- #ZEROPS_EXTRACT_END:intro# -->

... Lorem Ipsum
```

Content between `ZEROPS_EXTRACT_START` and `ZEROPS_EXTRACT_END` with a given key (e.g., `intro`) is extracted and made available in the recipe system.

## Supported Fragment Keys

| Key | Description | README.md Location |
|-----|-------------|-------------------|
| `intro` | Intro/description of the entity | app, recipe, recipe environment |
| `name` | Display name (e.g., "Laravel Jetstream") | app |
| `knowledge-base` | Useful info specific to the software/framework/recipe | anywhere |
| `integration-guide` | Guide on modifying the app to run on Zerops (with documented zerops.yaml) | app of a framework |
| `maintenance-guide` | Guides on operating/maintaining the app or recipe on Zerops | app of an OSS, OSS recipe environment |

## Where Fragments Live

### Recipe README.md (`zeropsio/recipes/{recipe-slug}/README.md`)
- `intro` — Recipe description shown on the recipe card and detail page

### Environment README.md (`zeropsio/recipes/{recipe-slug}/{N} — {Environment}/README.md`)
- `intro` — Environment description (e.g., "AI agent environment provides...")

### App README.md (`zerops-recipe-apps/{app-name}/README.md`)
- `intro` — App description
- `name` — Display name
- `integration-guide` — Step-by-step guide for Zerops integration
- `knowledge-base` — Runtime/framework-specific operational knowledge
- `maintenance-guide` — Operations and maintenance instructions

## How Fragments Are Consumed

### Recipe API
```
GET https://api.zerops.io/api/recipes?filters[slug][$eq]=bun-hello-world&populate[*]=*
```

Returns structured JSON with `sourceData.extracts`:
```json
{
  "extracts": {
    "intro": "A Bun application connected to PostgreSQL..."
  },
  "environments": [
    {
      "name": "4 — Small Production",
      "extracts": {
        "intro": "Small production environment offers..."
      }
    }
  ]
}
```

### Recipe .md Page
```
GET https://app.zerops.io/recipes/bun-hello-world.md?environment=small-production
```

Returns a rendered markdown page combining recipe intro, environment details, services, import YAML, and guide flows.

### GUI
The recipe GUI at `https://app.zerops.io/recipes/{slug}?environment={env}` renders fragments in appropriate UI locations.

## Cache Behavior

Recipes use cache for data pulled from GitHub repositories. After pushing changes to GitHub:
1. Changes are NOT immediately visible
2. Go to Strapi admin → recipe detail → click "Refresh Cache"
3. Or wait for automatic cache refresh

Strapi admin: `https://api-d89-1337.prg1.zerops.app/admin/content-manager/collection-types/api::recipe.recipe`
