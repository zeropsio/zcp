# Implementation Guide: `zcp sync` — Push-First Redesign

## Context

Editing recipe knowledge in ZCP requires a 5-step manual workflow across 3 repos. The push side is the bottleneck: after editing a recipe locally, you have to write fragments to a sibling app repo, cd into it, git add/commit/push, create a PR — all manually. With `gh` CLI and GitHub API, we can do this in one command with zero local clones.

## UX Goal

```bash
# Edit bun knowledge locally
vim internal/knowledge/recipes/bun-hello-world.md

# One command → PR URL
zcp sync push recipes bun-hello-world
# → Created PR: https://github.com/zerops-recipe-apps/bun-hello-world-app/pull/42

# Push a guide
zcp sync push guides choosing-database
# → Created PR: https://github.com/zeropsio/docs/pull/87

# Push all changed recipes
zcp sync push recipes
# → Created PR: .../bun-hello-world-app/pull/42
# → Created PR: .../laravel-app/pull/13
# → Skipped 31 recipes (no knowledge-base content)

# Pull (unchanged from current, just ported to Go)
zcp sync pull recipes
zcp sync pull recipes bun-hello-world
zcp sync pull guides
zcp sync pull --dry-run
```

## Config: `.sync.yaml` (project root, committed)

```yaml
api_url: https://api.zerops.io/api/recipes

slug_remap:
  recipe: nodejs-hello-world

environments:
  dev_stage: "AI Agent"
  small_prod: "Small Production"

exclude_categories:
  - service-utility

push:
  recipes:
    org: zerops-recipe-apps          # GitHub org for app repos
    repo_patterns:                    # slug → repo name resolution
      - "{slug}-app"                  # try first: bun-hello-world-app
      - "{slug}"                      # fallback: bun-hello-world
    branch_prefix: zcp               # branch: zcp/bun-hello-world-20260331
    commit_prefix: "chore(knowledge)" # commit: chore(knowledge): update bun-hello-world
  guides:
    repo: zeropsio/docs
    path: apps/docs/content/guides   # target path within repo
    branch_prefix: zcp
    commit_prefix: "chore(guides)"

paths:
  output: internal/knowledge         # where pull writes to (relative to project root)
```

## Package: `internal/sync/`

### File breakdown

| File | Lines (est) | Purpose |
|------|-------------|---------|
| `config.go` | ~80 | Config struct, LoadConfig, DefaultConfig, env var expansion |
| `pull_recipes.go` | ~120 | Recipe API fetch, JSON structs, markdown generation |
| `pull_guides.go` | ~80 | MDX→MD conversion (port from bash) |
| `push_recipes.go` | ~150 | **Core**: extract fragment, resolve repo, GitHub API push+PR |
| `push_guides.go` | ~100 | Extract guide, GitHub API push+PR |
| `github.go` | ~120 | GitHub operations via `gh` CLI: read file, create branch, update file, create PR |
| `transform.go` | ~80 | Fragment extraction, injection, H2↔H3 promotion, zerops.yml extraction |
| `result.go` | ~30 | Result types (Created/Updated/Skipped/Error per file) |
| `sync_test.go` | ~200 | Table-driven tests |

### `config.go`

```go
type Config struct {
    APIURL            string            `yaml:"api_url"`
    SlugRemap         map[string]string `yaml:"slug_remap"`
    Environments      EnvConfig         `yaml:"environments"`
    ExcludeCategories []string          `yaml:"exclude_categories"`
    Push              PushConfig        `yaml:"push"`
    Paths             PathsConfig       `yaml:"paths"`
}

type EnvConfig struct {
    DevStage  string `yaml:"dev_stage"`   // name substring match
    SmallProd string `yaml:"small_prod"`  // name substring match
}

type PushConfig struct {
    Recipes RecipePushConfig `yaml:"recipes"`
    Guides  GuidePushConfig  `yaml:"guides"`
}

type RecipePushConfig struct {
    Org          string   `yaml:"org"`
    RepoPatterns []string `yaml:"repo_patterns"` // "{slug}-app", "{slug}"
    BranchPrefix string   `yaml:"branch_prefix"`
    CommitPrefix string   `yaml:"commit_prefix"`
}

type GuidePushConfig struct {
    Repo         string `yaml:"repo"`
    Path         string `yaml:"path"`
    BranchPrefix string `yaml:"branch_prefix"`
    CommitPrefix string `yaml:"commit_prefix"`
}

type PathsConfig struct {
    Output    string `yaml:"output"`
    DocsLocal string `yaml:"docs_local"` // for pull guides (optional, env: DOCS_GUIDES)
}
```

`LoadConfig(root string)`: reads `{root}/.sync.yaml`, falls back to `DefaultConfig()`. Expands `${VAR:-default}` in path fields.

### `github.go` — GitHub operations via `gh` CLI

No Go GitHub SDK — shell out to `gh` CLI. Simpler, no token management (gh handles auth).

```go
// GH wraps gh CLI operations for a single repo.
type GH struct {
    Repo string // "owner/repo" format
}

// ReadFile returns file content and blob SHA from default branch.
func (g *GH) ReadFile(path string) (content string, sha string, err error)
// gh api repos/{repo}/contents/{path} --jq '.content,.sha'
// base64-decode content

// CreateBranch creates a new branch from default branch HEAD.
func (g *GH) CreateBranch(name string) error
// gh api repos/{repo}/git/refs -f ref=refs/heads/{name} -f sha={HEAD}

// UpdateFile commits a file change to a branch.
func (g *GH) UpdateFile(path, branch, message, content, blobSHA string) error
// gh api --method PUT repos/{repo}/contents/{path}
//   -f message={msg} -f content={base64} -f sha={blobSHA} -f branch={branch}

// CreatePR opens a pull request and returns the URL.
func (g *GH) CreatePR(branch, title, body string) (string, error)
// gh pr create --repo {repo} --head {branch} --title {title} --body {body}

// DefaultBranchSHA returns the HEAD SHA of the default branch.
func (g *GH) DefaultBranchSHA() (string, error)
// gh api repos/{repo} --jq '.default_branch'
// → gh api repos/{repo}/git/refs/heads/{branch} --jq '.object.sha'

// RepoExists checks if a repo exists (for repo pattern resolution).
func (g *GH) RepoExists() bool
// gh api repos/{repo} succeeds or not
```

Each method runs `exec.Command("gh", ...)` and parses stdout. Errors include stderr for debugging.

### `transform.go` — Fragment manipulation

Ported from bash `push_recipes()` lines 337-390, but in Go:

```go
// ExtractKnowledgeBase extracts knowledge-base content from a recipe .md file.
// Skips frontmatter, skips H1, stops before "## zerops.yml", demotes H2→H3.
func ExtractKnowledgeBase(recipeContent string) string

// InjectFragment replaces content between ZEROPS_EXTRACT markers in a README.
// If markers don't exist, appends them at the end.
func InjectFragment(readme, fragmentName, fragment string) string

// ExtractZeropsYAML extracts the YAML code block from "## zerops.yml" section.
func ExtractZeropsYAML(recipeContent string) string

// ConvertGuideToMDX converts a guide .md to .mdx format.
// Preserves existing frontmatter if target exists, generates new if not.
func ConvertGuideToMDX(guideContent string, existingMDX string) string

// ConvertMDXToGuide converts a docs .mdx file to guide .md format.
// Strips frontmatter, removes import lines, unwraps zerops:// backticks.
func ConvertMDXToGuide(mdxContent string) string
```

### `push_recipes.go` — The main push workflow

```go
func PushRecipes(cfg *Config, filter string, dryRun bool) ([]PushResult, error) {
    recipes := findLocalRecipes(cfg, filter)  // glob internal/knowledge/recipes/*.md

    var results []PushResult
    for _, recipe := range recipes {
        result := pushOneRecipe(cfg, recipe, dryRun)
        results = append(results, result)
    }
    return results, nil
}

func pushOneRecipe(cfg *Config, slug string, dryRun bool) PushResult {
    // 1. Read local recipe .md
    content := readRecipeFile(cfg, slug)

    // 2. Extract knowledge-base fragment
    fragment := ExtractKnowledgeBase(content)
    if fragment == "" {
        return PushResult{Slug: slug, Status: Skipped, Reason: "no knowledge-base content"}
    }

    // 3. Resolve GitHub repo: try each pattern in order
    repo := resolveRecipeRepo(cfg, slug)  // tries org/slug-app, org/slug
    if repo == "" {
        return PushResult{Slug: slug, Status: Skipped, Reason: "no GitHub repo found"}
    }

    gh := &GH{Repo: repo}

    // 4. Read current README.md from GitHub
    readme, sha, err := gh.ReadFile("README.md")

    // 5. Inject fragment
    updated := InjectFragment(readme, "knowledge-base", fragment)

    // 6. Also update zerops.yaml if present
    yamlContent := ExtractZeropsYAML(content)
    // ... similar read/update for zerops.yaml

    if dryRun {
        // Print diff, return
        return PushResult{Slug: slug, Status: DryRun, Diff: diff(readme, updated)}
    }

    // 7. Create branch: zcp/{slug}-{YYYYMMDD}
    branch := fmt.Sprintf("%s/%s-%s", cfg.Push.Recipes.BranchPrefix, slug, today())
    gh.CreateBranch(branch)

    // 8. Commit files
    gh.UpdateFile("README.md", branch, commitMsg, updated, sha)
    if yamlContent != "" {
        gh.UpdateFile("zerops.yaml", branch, commitMsg, yamlContent, yamlSHA)
    }

    // 9. Create PR
    prURL, _ := gh.CreatePR(branch, title, body)

    return PushResult{Slug: slug, Status: Created, PRURL: prURL}
}
```

### `push_guides.go` — Same pattern for guides

```go
func PushGuides(cfg *Config, filter string, dryRun bool) ([]PushResult, error)
```

Guides all go to a single repo (`zeropsio/docs`), so one PR with multiple file changes. Uses the GitHub Trees API for multi-file commits:

```go
// For guides: batch all changes into one PR
// 1. Read all guide .md files from ZCP
// 2. Convert each to .mdx (preserving existing frontmatter from GitHub)
// 3. Create one branch, one commit with all files, one PR
```

### `pull_recipes.go` — Port from bash (mostly unchanged logic)

```go
type APIRecipe struct {
    Slug       string     `json:"slug"`
    Name       string     `json:"name"`
    SourceData SourceData `json:"sourceData"`
}

type SourceData struct {
    Environments []Environment `json:"environments"`
    Extracts     Extracts      `json:"extracts"`
}

type Environment struct {
    Name     string    `json:"name"`
    Import   string    `json:"import"`
    Services []Service `json:"services"`
}

// ... etc

func PullRecipes(cfg *Config, filter string, dryRun bool) ([]PullResult, error) {
    // 1. Fetch from API
    // 2. For each recipe:
    //    a. Remap slug (cfg.SlugRemap)
    //    b. Match environments by name (cfg.Environments.DevStage/SmallProd)
    //    c. Generate .md (same format as bash script)
    //    d. Write to cfg.Paths.Output/recipes/{slug}.md
}
```

**Key improvement**: environment matching by name instead of index:
```go
func findEnvByName(envs []Environment, pattern string) *Environment {
    for i := range envs {
        if strings.Contains(envs[i].Name, pattern) {
            return &envs[i]
        }
    }
    return nil
}
```

### `pull_guides.go` — Port from bash

```go
func PullGuides(cfg *Config, filter string, dryRun bool) ([]PullResult, error)
```

Still reads from local docs clone (MDX parsing needs the files). Path from config.

### `result.go`

```go
type Status int
const (
    Created Status = iota
    Updated
    Skipped
    DryRun
    Error
)

type PushResult struct {
    Slug   string
    Status Status
    Reason string // for Skipped
    PRURL  string // for Created
    Diff   string // for DryRun
    Err    error  // for Error
}

type PullResult struct {
    Slug   string
    Status Status
    Reason string
    Diff   string
}
```

## Subcommand: `cmd/zcp/sync.go`

```go
func runSync(args []string) {
    // Parse: sync {pull|push} [recipes|guides|all] [slug] [--dry-run] [--config path]
    if len(args) == 0 { printSyncUsage(); os.Exit(1) }

    var dryRun bool
    var configPath string
    var filter string
    // ... flag parsing (manual, matching eval.go pattern)

    cfg, err := sync.LoadConfig(configPath)
    // ...

    switch args[0] {
    case "pull":
        runSyncPull(cfg, category, filter, dryRun)
    case "push":
        runSyncPush(cfg, category, filter, dryRun)
    }
}
```

Wire into `main.go`:
```go
case "sync":
    runSync(os.Args[2:])
    return
```

## Makefile additions

```makefile
sync: build ## Pull all knowledge from external sources
	./bin/zcp sync pull

sync-recipes: build ## Pull recipes from API
	./bin/zcp sync pull recipes

sync-push: build ## Push knowledge changes as GitHub PRs
	./bin/zcp sync push
```

## CI: `.github/workflows/ci.yml`

Add before Build step:
```yaml
- name: Ensure knowledge directories
  run: mkdir -p internal/knowledge/{recipes,guides,decisions}
```

## Tests: `internal/sync/sync_test.go`

Table-driven, per CLAUDE.md:

| Test | What it verifies |
|------|-----------------|
| `TestLoadConfig_Defaults` | Default config matches current bash behavior |
| `TestLoadConfig_FromYAML` | YAML parsing works |
| `TestLoadConfig_EnvExpansion` | `${VAR:-default}` expansion |
| `TestSlugRemap` | "recipe" → "nodejs-hello-world", passthrough |
| `TestEnvMatchByName` | Finds "AI Agent" env by substring |
| `TestEnvMatchByName_NoMatch` | Returns nil when no match |
| `TestExtractKnowledgeBase` | Correct extraction from bun recipe format |
| `TestExtractKnowledgeBase_Empty` | Returns "" for recipes with no KB |
| `TestInjectFragment_Existing` | Replaces between markers |
| `TestInjectFragment_New` | Appends markers when none exist |
| `TestExtractZeropsYAML` | Extracts YAML block from recipe |
| `TestConvertMDXToGuide` | Frontmatter strip, import removal |
| `TestConvertGuideToMDX` | Frontmatter preservation |
| `TestResolveRecipeRepo` | Pattern resolution: slug-app first, then slug |
| `TestPullRecipeMarkdown` | Generated .md matches expected format |
| `TestDryRun_NoSideEffects` | Nothing written, diff returned |

## Implementation order

1. **`config.go` + `config_test.go`** — no deps, establish test patterns
2. **`result.go`** — types only
3. **`transform.go` + `transform_test.go`** — pure functions, no I/O
4. **`github.go`** — gh CLI wrapper (test with mock exec or integration test)
5. **`pull_recipes.go` + tests** — port bash, use config + name-based env matching
6. **`pull_guides.go` + tests** — port bash
7. **`push_recipes.go` + tests** — core push flow using github.go + transform.go
8. **`push_guides.go` + tests** — same pattern
9. **`cmd/zcp/sync.go` + main.go** — wire up subcommand
10. **`.sync.yaml`** — default config committed
11. **Makefile + CI** — integration
12. **Deprecate bash script** — add stderr warning

## Verification

1. `go test ./internal/sync/... -v` — unit tests pass
2. `go build ./cmd/zcp` — builds
3. `./bin/zcp sync pull recipes` — produces same output as bash script
4. `./bin/zcp sync pull recipes bun-hello-world` — selective pull works
5. `./bin/zcp sync push recipes bun-hello-world --dry-run` — shows diff, no PR created
6. `./bin/zcp sync push recipes bun-hello-world` — creates PR, prints URL
7. `./bin/zcp sync push guides --dry-run` — shows what would change
8. `make sync && make build` — full pipeline
9. `go test ./... -count=1 -short` — existing tests still pass

## What does NOT change

- `internal/knowledge/` — entire package untouched (embed, parsing, search, briefing)
- `.gitignore` — same patterns
- `themes/*.md`, `bases/*.md` — committed, unaffected
- All existing tests
- The markdown format contract
