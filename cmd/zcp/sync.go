package main

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/joho/godotenv"
	"github.com/zeropsio/zcp/internal/authoring/publish"
	"github.com/zeropsio/zcp/internal/sync"
)

const categoryAll = "all"

func runSync(args []string) int {
	// Load .env if present (never errors on missing file)
	_ = godotenv.Load()

	if len(args) == 0 {
		printSyncUsage()
		return 1
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
		return 1
	}

	root := "."
	if configPath == "" {
		configPath = root
	}

	cfg, err := sync.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
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
		return runSyncPull(cfg, root, category, filter, dryRun)
	case "push":
		return runSyncPush(cfg, root, category, filter, dryRun)
	case "cache-clear":
		return runSyncCacheClear(cfg, positional[1:])
	case "recipe":
		return runSyncRecipe(cfg, positional[1:], dryRun)
	default:
		fmt.Fprintf(os.Stderr, "unknown sync action: %s\n", action)
		printSyncUsage()
		return 1
	}
}

func runSyncPull(cfg *sync.Config, root, category, filter string, dryRun bool) int {
	failed := 0

	if category == categoryAll || category == "recipes" {
		fmt.Fprintln(os.Stderr, "=== Pulling recipes from API ===")
		results, err := sync.PullRecipes(cfg, root, filter, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		failed += printPullResults(results)
	}

	if category == categoryAll || category == "guides" {
		fmt.Fprintln(os.Stderr, "=== Pulling guides ===")
		results, err := sync.PullGuides(cfg, root, filter, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		failed += printPullResults(results)
	}

	// A partial corpus is never acceptable — a per-item failure that exits 0
	// masks itself until an embed test fails much later (Release v9.125.0/.1:
	// one transient GitHub read lost one random guide per run). Fail HERE,
	// with the ERROR lines above naming exactly what didn't come down.
	if failed > 0 {
		fmt.Fprintf(os.Stderr, "sync pull FAILED: %d item(s) errored\n", failed)
		return 1
	}
	return 0
}

func runSyncPush(cfg *sync.Config, root, category, filter string, dryRun bool) int {
	if category == categoryAll || category == "recipes" {
		fmt.Fprintln(os.Stderr, "=== Pushing recipes ===")
		results, err := sync.PushRecipes(cfg, root, filter, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		printPushResults(results)
	}

	if category == categoryAll || category == "guides" {
		fmt.Fprintln(os.Stderr, "=== Pushing guides ===")
		results, err := sync.PushGuides(cfg, root, filter, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		printPushResults(results)
	}
	return 0
}

// printPullResults prints per-item outcomes and returns the number of
// errored items so the caller can refuse to exit 0 on a partial corpus.
func printPullResults(results []sync.PullResult) int {
	created, skipped, errored := 0, 0, 0
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
			errored++
		}
	}
	fmt.Fprintf(os.Stderr, "Pulled %d files (%d skipped)\n", created, skipped)
	return errored
}

func runSyncCacheClear(cfg *sync.Config, args []string) int {
	fmt.Fprintln(os.Stderr, "=== Clearing Strapi cache ===")
	results, err := sync.CacheClear(cfg, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
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
	if errors > 0 {
		return 1
	}
	return 0
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
			// Surface a non-empty reason (e.g. a divergent/invalid zerops.yaml
			// the recipe push refused) instead of a silent count — the old
			// length-guard skip was invisible (tell "pushed" ≠ check "skipped").
			if r.Reason != "" && r.Reason != "no changes" {
				fmt.Fprintf(os.Stderr, "  Skipped %s: %s\n", r.Slug, r.Reason)
			}
		case sync.DryRun:
			fmt.Fprintf(os.Stderr, "  [dry-run] %s: %s\n", r.Slug, r.Diff)
		case sync.Error:
			fmt.Fprintf(os.Stderr, "  ERROR %s: %v\n", r.Slug, r.Err)
		}
	}
	fmt.Fprintf(os.Stderr, "Pushed %d PRs (%d skipped)\n", created, skipped)
}

// isHelpArg matches the conventional --help / -h / help spellings that
// agents and humans both try first. The sync subcommand tree was built
// with a strict positional parser that had no help affordance, which
// made agents exit-code-trip when they asked for help; this helper is
// the minimum fix to let them discover usage without treating it as an
// error.
func isHelpArg(s string) bool {
	return s == "--help" || s == "-h" || s == "help"
}

// argsContainHelp returns true if any element of args is a help flag.
// Used by subcommand parsers to bail out of arg validation and dump
// the subcommand's usage line cleanly.
func argsContainHelp(args []string) bool {
	return slices.ContainsFunc(args, isHelpArg)
}

//nolint:maintidx // pre-existing complexity; help/flag additions are targeted
func runSyncRecipe(cfg *sync.Config, args []string, dryRun bool) int {
	if len(args) == 0 || isHelpArg(args[0]) {
		printRecipeUsage()
		if len(args) == 0 {
			return 1
		}
		return 0
	}

	sub := args[0]
	switch sub {
	case "create-repo":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: zcp sync recipe create-repo <slug> [--repo-suffix <name>] [--dry-run]")
			return 1
		}
		slug := args[1]
		suffix := ""
		for i := 2; i < len(args); i++ {
			if args[i] == "--repo-suffix" && i+1 < len(args) {
				suffix = args[i+1]
				i++
			}
		}
		result, err := publish.CreateRecipeRepo(cfg, slug, suffix, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		return printRecipeResult(result)

	case "publish":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: zcp sync recipe publish <slug> <source-dir> [--name <pretty>] [--software <name>] [--desc <text>] [--tags <tag>] [--cover <file.svg>] [--dry-run]")
			return 1
		}
		slug := args[1]
		sourceDir := args[2]
		opts := publish.PublishOpts{Slug: slug}
		for i := 3; i < len(args); i++ {
			switch args[i] {
			case "--name":
				if i+1 < len(args) {
					opts.PrettyName = args[i+1]
					i++
				}
			case "--software":
				if i+1 < len(args) {
					opts.Software = args[i+1]
					i++
				}
			case "--desc":
				if i+1 < len(args) {
					opts.Description = args[i+1]
					i++
				}
			case "--tags":
				if i+1 < len(args) {
					opts.Tags = args[i+1]
					i++
				}
			case "--cover":
				if i+1 < len(args) {
					opts.CoverSVG = args[i+1]
					i++
				}
			}
		}
		// Derive defaults from slug if not provided.
		if opts.PrettyName == "" {
			opts.PrettyName = strings.ReplaceAll(strings.ReplaceAll(slug, "-", " "), "  ", " ")
			// Title case.
			words := strings.Fields(opts.PrettyName)
			for j, w := range words {
				if len(w) > 0 {
					words[j] = strings.ToUpper(w[:1]) + w[1:]
				}
			}
			opts.PrettyName = strings.Join(words, " ")
		}
		if opts.Software == "" {
			// First word of pretty name.
			if idx := strings.Index(opts.PrettyName, " "); idx > 0 {
				opts.Software = opts.PrettyName[:idx]
			} else {
				opts.Software = opts.PrettyName
			}
		}
		if opts.Tags == "" {
			opts.Tags = strings.ToLower(opts.Software)
		}
		result, err := publish.Recipe(cfg, slug, sourceDir, opts, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		return printRecipeResult(result)

	case "push-app":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: zcp sync recipe push-app <slug> <app-dir> [--repo-suffix <name>] [--dry-run]")
			return 1
		}
		slug := args[1]
		appDir := args[2]
		suffix := ""
		for i := 3; i < len(args); i++ {
			if args[i] == "--repo-suffix" && i+1 < len(args) {
				suffix = args[i+1]
				i++
			}
		}
		result, err := publish.PushAppSource(cfg, slug, suffix, appDir, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		return printRecipeResult(result)

	case "export":
		const exportUsage = "usage: zcp sync recipe export <recipe-dir> [--app-dir <path>]... [--include-timeline] [--force-export]"
		// Two-pass arg parsing: flags may appear anywhere, positional
		// args collected in order. Fixes the strict-ordering trap that
		// made `export --include-timeline <dir>` fail with "is not a
		// directory" (the flag was parsed as the positional dir).
		if argsContainHelp(args[1:]) {
			fmt.Fprintln(os.Stderr, exportUsage)
			return 0
		}
		var opts publish.ExportOpts
		var positional []string
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--include-timeline":
				opts.IncludeTimeline = true
			case "--app-dir":
				if i+1 >= len(args) {
					fmt.Fprintln(os.Stderr, "error: --app-dir requires a path value")
					fmt.Fprintln(os.Stderr, exportUsage)
					return 1
				}
				opts.AppDirs = append(opts.AppDirs, args[i+1])
				i++
			case "--force-export":
				opts.SkipCloseGate = true
			default:
				if strings.HasPrefix(args[i], "--") {
					fmt.Fprintf(os.Stderr, "error: unknown flag %q\n", args[i])
					fmt.Fprintln(os.Stderr, exportUsage)
					return 1
				}
				positional = append(positional, args[i])
			}
		}
		if len(positional) != 1 {
			fmt.Fprintf(os.Stderr, "error: expected exactly one <recipe-dir> argument, got %d\n", len(positional))
			fmt.Fprintln(os.Stderr, exportUsage)
			return 1
		}
		opts.RecipeDir = positional[0]
		result, err := publish.ExportRecipe(opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		if result.NeedsTimeline {
			fmt.Fprintln(os.Stderr, result.TimelinePrompt)
			return 1
		}
		fmt.Fprintf(os.Stderr, "Exported: %s\n", result.ArchivePath)
		return 0

	default:
		fmt.Fprintf(os.Stderr, "unknown recipe subcommand: %s\n", sub)
		printRecipeUsage()
		return 1
	}
}

func printRecipeResult(r sync.PushResult) int {
	switch r.Status {
	case sync.Created:
		fmt.Fprintf(os.Stderr, "  Created: %s → %s\n", r.Slug, r.PRURL)
	case sync.Updated:
		fmt.Fprintf(os.Stderr, "  Updated: %s → %s\n", r.Slug, r.PRURL)
	case sync.Skipped:
		fmt.Fprintf(os.Stderr, "  Skipped: %s — %s\n", r.Slug, r.Reason)
	case sync.DryRun:
		fmt.Fprintf(os.Stderr, "  [dry-run] %s: %s\n", r.Slug, r.Diff)
	case sync.Error:
		fmt.Fprintf(os.Stderr, "  ERROR %s: %v\n", r.Slug, r.Err)
		return 1
	}
	return 0
}

func printRecipeUsage() {
	fmt.Fprintln(os.Stderr, `Usage: zcp sync recipe <subcommand> [args] [flags]

Subcommands:
  create-repo <slug> [--repo-suffix <name>]            Create app repo in zerops-recipe-apps org (suffix defaults to "app")
  push-app    <slug> <app-dir> [--repo-suffix <name>]  Push app source to the app repo (suffix must match create-repo)
  publish     <slug> <source-dir>                      Publish environments to zeropsio/recipes
  export      <recipe-dir>                             Create .tar.gz archive of recipe output

Flags:
  --dry-run              Show what would change without writing
  --repo-suffix <name>   Codebase suffix for create-repo/push-app (default "app")
  --app-dir <path>       App source dir to include (repeatable for dual-runtime)
  --include-timeline     Prompt for TIMELINE.md if missing (export only)

Examples:
  # Single-codebase recipe (backward compat — resolves to {slug}-app):
  zcp sync recipe create-repo laravel-minimal
  zcp sync recipe push-app    laravel-minimal /var/www/appdev

  # Dual-runtime showcase with separate worker (3 repos):
  zcp sync recipe create-repo nestjs-showcase --repo-suffix app
  zcp sync recipe push-app    nestjs-showcase /var/www/appdev    --repo-suffix app
  zcp sync recipe create-repo nestjs-showcase --repo-suffix api
  zcp sync recipe push-app    nestjs-showcase /var/www/apidev    --repo-suffix api
  zcp sync recipe create-repo nestjs-showcase --repo-suffix worker
  zcp sync recipe push-app    nestjs-showcase /var/www/workerdev --repo-suffix worker

  zcp sync recipe publish laravel-minimal /var/www/zcprecipator/laravel-minimal
  zcp sync recipe export  /var/www/zcprecipator/laravel-minimal --app-dir /var/www/appdev
  zcp sync recipe export  /var/www/zcprecipator/nestjs-showcase \
    --app-dir /var/www/apidev --app-dir /var/www/appdev --include-timeline`)
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
