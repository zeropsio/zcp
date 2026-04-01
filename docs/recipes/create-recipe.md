# How to Create a Recipe

## Prerequisites
- Read through similar recipes in both the [GUI](https://app.zerops.io/recipes) and source code
- Pay attention to structure; important files: `README.md`, `zerops.yaml`, `import.yaml`

## Procedure

### Step 1: Create App Repository

Migrate/create app(s) in [zerops-recipe-apps](https://github.com/zerops-recipe-apps):

1. [Create new repo](https://github.com/organizations/zerops-recipe-apps/repositories/new) → name it `{slug}-app` (e.g., `bun-hello-world-app`)
   - Check "Public template" in repo Settings
2. If migrating from old recipe:
   ```bash
   git clone git@github.com:zeropsio/recipe-{old}.git
   mv recipe-{old} {slug}-app
   cd {slug}-app
   git remote set-url origin git@github.com:zerops-recipe-apps/{slug}-app.git
   git push -u origin main
   ```
3. Remove `import.yaml` from app repo (it belongs in the recipe repo)
4. Add fragment-marked sections to `README.md` (see [fragment-system.md](../00-architecture/fragment-system.md))
5. Edit and thoroughly comment `zerops.yaml`

### Step 2: Create Recipe Folder

Copy `_template` (or `_template_oss`) in [zeropsio/recipes](https://github.com/zeropsio/recipes) and replace placeholders:

| Placeholder | Example |
|-------------|---------|
| `PLACEHOLDER_PROJECT_NAME` | `go-hello-world` |
| `PLACEHOLDER_PRETTY_RECIPE_NAME` | `Go Hello World` |
| `PLACEHOLDER_RECIPE_DIRECTORY` | `go-hello-world` |
| `PLACEHOLDER_RECIPE_SOFTWARE` | `[Go](https://go.dev) applications` |
| `PLACEHOLDER_RECIPE_DESCRIPTION` | `Simple Go API with single endpoint that reads from and writes to a PostgreSQL database.` |
| `PLACEHOLDER_COVER_SVG` | `cover-go.svg` |
| `PLACEHOLDER_RECIPE_TAGS` | `golang,echo` |
| `PLACEHOLDER_PRETTY_RECIPE_TAGS` | `Go` |

### Step 3: Create import.yaml Per Environment

Create and comment `import.yaml` for all 6 environments (or 2 for OSS). Each environment needs:
- Project name with environment suffix (`-agent`, `-remote`, `-local`, `-stage`, `-prod`, `-ha-prod`)
- Service definitions with appropriate scaling per environment
- `corePackage: SERIOUS` for HA production

### Step 4: Register in Strapi

[Add the recipe to Strapi](https://api-d89-1337.prg1.zerops.app/admin/content-manager/collection-types/api::recipe.recipe/create):
- Name, slug, icon
- Categories and language/framework associations

### Step 5: Test All Environments

Launch every environment and verify:
- Dev services have relevant technology commands (`go`, `bun`, `cargo`, etc.)
- Applications do what they're supposed to
- All service connections work

## Result Checklist

- [ ] Strapi entry with logo and correct categories
- [ ] Recipe folder in `zeropsio/recipes` with all environments
  - [ ] Each environment has documented `import.yaml`
  - [ ] Each environment has `README.md` with `intro` fragment
  - [ ] Main `README.md` with `intro` fragment
- [ ] App repo(s) in `zerops-recipe-apps`
  - [ ] Thoroughly documented `zerops.yaml`
  - [ ] `README.md` with appropriate extract fragments
  - [ ] README makes sense when visiting the repo directly on GitHub

## YAML Documentation Notes

- Include implementation notes in YAML comments
- Every non-obvious part should explain **why it's there** and **how it works**
- This serves both user education and LLM training

## Two Types of OSS

1. **Pure Zerops YAML** with installation commands (e.g., Umami)
2. **npm prepare** which outputs the source code (e.g., Strapi)
