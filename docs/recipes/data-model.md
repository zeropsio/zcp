# Data Model

## Strapi Recipe Entry

Each recipe has a Strapi CMS entry with:

| Field | Example |
|-------|---------|
| `name` | "Bun Hello World" |
| `slug` | "bun-hello-world" |
| `icon` | "bun" |
| `internalType` | "example" |
| `source` | "bun-hello-world" (maps to folder in zeropsio/recipes) |
| `recipeCategories` | ["Hello World Examples"] |
| `recipeLanguageFrameworks` | [{ name: "Bun", type: "language", slug: "bun" }] |
| `iconImage` | SVG icon URL |

## Source Data (from GitHub)

The `sourceData` field is populated from the GitHub repos and contains:

```
sourceData:
  name: "bun-hello-world"
  content: (full README.md)
  extracts:
    intro: (extracted fragment)
  environments:
    - name: "0 — AI Agent"
      content: (environment README.md)
      extracts:
        intro: (extracted fragment)
      import: (full import.yaml content)
      projectName: "bun-hello-world-agent"
      services:
        - name: "app"
          typeId/typeName/typeVersionName: "bun@1.2"
          category: "runtime"
          content: (app README.md)
          zeropsYaml: (full zerops.yaml)
          extracts:
            intro: (app intro)
            integration-guide: (if exists)
            knowledge-base: (if exists)
          autoscaling: { minRam, minFreeRamGB, ... }
          ports: [{ port: 3000, ... }]
        - name: "db"
          typeName: "postgresql@18"
          category: "database"
          mode: "NON_HA"
    - name: "4 — Small Production"
      ...
```

## API Endpoints

### List/Filter Recipes
```
GET https://api.zerops.io/api/recipes
  ?filters[slug][$eq]=bun-hello-world
  &populate[recipeCategories]=true
  &populate[recipeLanguageFrameworks][populate]=*
  &populate[iconImage]=true
```

### Recipe Markdown Page
```
GET https://app.zerops.io/recipes/{slug}.md?environment={env}
```

Returns a full markdown page with:
- Tags, title, description
- Available environments list
- Services in selected environment (with resource totals)
- Import YAML for one-click deploy
- "Template Flow" guide (clone repos → CI/CD → deploy → autoscaling → domain → backups)
- "Integrate Flow" guide (add zerops.yaml → configure → deploy)
- Knowledge base links
- Related recipes

### Environment Slugs
| Environment | Slug |
|-------------|------|
| AI Agent | `ai-agent` |
| Remote (CDE) | `remote-cde` |
| Local | `local` |
| Stage | `stage` |
| Small Production | `small-production` |
| Highly-available Production | `highly-available-production` |

## Recipe .md Page Guide Flows

The recipe .md page supports two guide flows via query parameter `guideFlow=template|integrate`:

### Template Flow
1. Clone template repositories
2. Identify service name in Zerops Dashboard
3. Configure CI/CD pipeline (tag-based triggers)
4. Deploy to production (git tag)
5. Configure autoscaling
6. Enable custom domain access
7. (Optional) Notifications, log forwarding, database backups, diagnostic access

### Integrate Flow
1. Add `zerops.yaml` to existing project
2. Configure environment variables
3. Deploy via zcli or CI/CD
