package main

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/zeropsio/zcp/internal/sync"
)

const categoryAll = "all"

func runSync(args []string) {
	// Load .env if present (never errors on missing file)
	_ = godotenv.Load()

	if len(args) == 0 {
		printSyncUsage()
		os.Exit(1)
	}

	var dryRun bool
	var configPath string

	// Extract flags from args
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			dryRun = true
		case "--config":
			if i+1 < len(args) {
				configPath = args[i+1]
				i++
			}
		default:
			positional = append(positional, args[i])
		}
	}

	if len(positional) == 0 {
		printSyncUsage()
		os.Exit(1)
	}

	root := "."
	if configPath == "" {
		configPath = root
	}

	cfg, err := sync.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	action := positional[0]
	category := categoryAll
	if len(positional) > 1 {
		category = positional[1]
	}
	filter := ""
	if len(positional) > 2 {
		filter = positional[2]
	}

	switch action {
	case "pull":
		runSyncPull(cfg, root, category, filter, dryRun)
	case "push":
		runSyncPush(cfg, root, category, filter, dryRun)
	case "cache-clear":
		runSyncCacheClear(cfg, positional[1:])
	case "recipe":
		runSyncRecipe(cfg, positional[1:], dryRun)
	default:
		fmt.Fprintf(os.Stderr, "unknown sync action: %s\n", action)
		printSyncUsage()
		os.Exit(1)
	}
}

func runSyncPull(cfg *sync.Config, root, category, filter string, dryRun bool) {
	if category == categoryAll || category == "recipes" {
		fmt.Fprintln(os.Stderr, "=== Pulling recipes from API ===")
		results, err := sync.PullRecipes(cfg, root, filter, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printPullResults(results)
	}

	if category == categoryAll || category == "guides" {
		fmt.Fprintln(os.Stderr, "=== Pulling guides ===")
		results, err := sync.PullGuides(cfg, root, filter, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printPullResults(results)
	}
}

func runSyncPush(cfg *sync.Config, root, category, filter string, dryRun bool) {
	if category == categoryAll || category == "recipes" {
		fmt.Fprintln(os.Stderr, "=== Pushing recipes ===")
		results, err := sync.PushRecipes(cfg, root, filter, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printPushResults(results)
	}

	if category == categoryAll || category == "guides" {
		fmt.Fprintln(os.Stderr, "=== Pushing guides ===")
		results, err := sync.PushGuides(cfg, root, filter, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printPushResults(results)
	}
}

func printPullResults(results []sync.PullResult) {
	created, skipped := 0, 0
	for _, r := range results {
		switch r.Status {
		case sync.Created, sync.Updated:
			fmt.Fprintf(os.Stderr, "  %s → %s\n", r.Slug, r.Status)
			created++
		case sync.Skipped:
			skipped++
		case sync.DryRun:
			fmt.Fprintf(os.Stderr, "  [dry-run] %s\n", r.Slug)
		case sync.Error:
			fmt.Fprintf(os.Stderr, "  ERROR %s: %s\n", r.Slug, r.Reason)
		}
	}
	fmt.Fprintf(os.Stderr, "Pulled %d files (%d skipped)\n", created, skipped)
}

func runSyncCacheClear(cfg *sync.Config, args []string) {
	fmt.Fprintln(os.Stderr, "=== Clearing Strapi cache ===")
	results, err := sync.CacheClear(cfg, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	cleared, errors := 0, 0
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(os.Stderr, "  ERROR %s: %v\n", r.Slug, r.Err)
			errors++
		} else {
			fmt.Fprintf(os.Stderr, "  %s → cleared (%d)\n", r.Slug, r.Status)
			cleared++
		}
	}
	fmt.Fprintf(os.Stderr, "Cleared %d recipes (%d errors)\n", cleared, errors)
}

func printPushResults(results []sync.PushResult) {
	created, skipped := 0, 0
	for _, r := range results {
		switch r.Status {
		case sync.Created:
			fmt.Fprintf(os.Stderr, "  Created PR: %s → %s\n", r.Slug, r.PRURL)
			created++
		case sync.Updated:
			fmt.Fprintf(os.Stderr, "  Updated PR: %s → %s\n", r.Slug, r.PRURL)
			created++
		case sync.Skipped:
			skipped++
		case sync.DryRun:
			fmt.Fprintf(os.Stderr, "  [dry-run] %s: %s\n", r.Slug, r.Diff)
		case sync.Error:
			fmt.Fprintf(os.Stderr, "  ERROR %s: %v\n", r.Slug, r.Err)
		}
	}
	fmt.Fprintf(os.Stderr, "Pushed %d PRs (%d skipped)\n", created, skipped)
}

func runSyncRecipe(cfg *sync.Config, args []string, dryRun bool) {
	if len(args) == 0 {
		printRecipeUsage()
		os.Exit(1)
	}

	sub := args[0]
	switch sub {
	case "create-repo":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: zcp sync recipe create-repo <slug> [--dry-run]")
			os.Exit(1)
		}
		slug := args[1]
		result, err := sync.CreateRecipeRepo(cfg, slug, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printRecipeResult(result)

	case "publish":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: zcp sync recipe publish <slug> <source-dir> [--dry-run]")
			os.Exit(1)
		}
		slug := args[1]
		sourceDir := args[2]
		result, err := sync.PublishRecipe(cfg, slug, sourceDir, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printRecipeResult(result)

	case "export":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: zcp sync recipe export <source-dir>")
			os.Exit(1)
		}
		sourceDir := args[1]
		outPath, err := sync.ExportRecipe(sourceDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Exported: %s\n", outPath)

	default:
		fmt.Fprintf(os.Stderr, "unknown recipe subcommand: %s\n", sub)
		printRecipeUsage()
		os.Exit(1)
	}
}

func printRecipeResult(r sync.PushResult) {
	switch r.Status {
	case sync.Created:
		fmt.Fprintf(os.Stderr, "  Created: %s → %s\n", r.Slug, r.PRURL)
	case sync.Skipped:
		fmt.Fprintf(os.Stderr, "  Skipped: %s — %s\n", r.Slug, r.Reason)
	case sync.DryRun:
		fmt.Fprintf(os.Stderr, "  [dry-run] %s: %s\n", r.Slug, r.Diff)
	case sync.Error:
		fmt.Fprintf(os.Stderr, "  ERROR %s: %v\n", r.Slug, r.Err)
		os.Exit(1)
	}
}

func printRecipeUsage() {
	fmt.Fprintln(os.Stderr, `Usage: zcp sync recipe <subcommand> [args] [flags]

Subcommands:
  create-repo <slug>                Create app repo in zerops-recipe-apps org
  publish <slug> <source-dir>       Publish environments to zeropsio/recipes
  export <source-dir>               Create .tar.gz archive of recipe output

Flags:
  --dry-run    Show what would change without writing

Examples:
  zcp sync recipe create-repo laravel-minimal
  zcp sync recipe publish laravel-minimal ../zcprecipator/laravel-minimal-v4
  zcp sync recipe export ../zcprecipator/laravel-minimal-v4`)
}

func printSyncUsage() {
	fmt.Fprintln(os.Stderr, `Usage: zcp sync <action> [category] [slug] [flags]

Actions:
  pull          Pull knowledge from external sources into ZCP
  push          Push ZCP knowledge changes as GitHub PRs
  cache-clear   Invalidate Strapi cache for recipes (requires STRAPI_API_TOKEN)
  recipe        Recipe repo management (create-repo, publish, export)

Categories:
  recipes   Recipe knowledge (API for pull, app repos for push)
  guides    Guide knowledge (docs repo)
  all       All categories (default)

Flags:
  --dry-run    Show what would change without writing
  --config     Path to .sync.yaml (default: current directory)

Examples:
  zcp sync pull recipes
  zcp sync pull recipes bun-hello-world
  zcp sync push recipes bun-hello-world --dry-run
  zcp sync push guides
  zcp sync cache-clear bun-hello-world
  zcp sync recipe create-repo laravel-minimal
  zcp sync recipe publish laravel-minimal ./output-dir
  zcp sync recipe export ./output-dir`)
}
