package main

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/zeropsio/zcp/internal/sync"
)

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
	category := "all"
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
	default:
		fmt.Fprintf(os.Stderr, "unknown sync action: %s\n", action)
		printSyncUsage()
		os.Exit(1)
	}
}

func runSyncPull(cfg *sync.Config, root, category, filter string, dryRun bool) {
	if category == "all" || category == "recipes" {
		fmt.Fprintln(os.Stderr, "=== Pulling recipes from API ===")
		results, err := sync.PullRecipes(cfg, root, filter, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printPullResults(results)
	}

	if category == "all" || category == "guides" {
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
	if category == "all" || category == "recipes" {
		fmt.Fprintln(os.Stderr, "=== Pushing recipes ===")
		results, err := sync.PushRecipes(cfg, root, filter, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printPushResults(results)
	}

	if category == "all" || category == "guides" {
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

func printSyncUsage() {
	fmt.Fprintln(os.Stderr, `Usage: zcp sync <action> [category] [slug] [flags]

Actions:
  pull          Pull knowledge from external sources into ZCP
  push          Push ZCP knowledge changes as GitHub PRs
  cache-clear   Invalidate Strapi cache for recipes (requires STRAPI_API_TOKEN)

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
  zcp sync cache-clear`)
}
