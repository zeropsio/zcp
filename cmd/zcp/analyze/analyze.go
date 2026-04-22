// Package analyze wires the mechanical analysis harness into the zcp
// CLI. `zcp analyze recipe-run` produces a machine-report against a
// zcprecipator2 deliverable + session-logs tree; `zcp analyze
// generate-checklist` emits the analyst worksheet. Together they form
// Tier-1 + Tier-2 of docs/zcprecipator2/spec-recipe-analysis-harness.md.
//
// The commands are deliberately free of network access, platform
// auth, or MCP server dependencies. They operate on local artifacts
// only. This keeps the harness invocable from CI, from a clean
// developer checkout, and from post-run analysis scripts without any
// Zerops credential plumbing.
package analyze

import (
	"fmt"
	"os"
	"strings"
)

// Run dispatches the subcommand. Called from cmd/zcp/main.go's switch.
// args is os.Args[2:] — everything after `zcp analyze`.
func Run(args []string) {
	if len(args) == 0 || isHelp(args[0]) {
		printUsage()
		if len(args) == 0 {
			os.Exit(1)
		}
		return
	}
	switch args[0] {
	case "recipe-run":
		runRecipeRun(args[1:])
	case "generate-checklist":
		runGenerateChecklist(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown analyze subcommand: %s\n\n", args[0])
		printUsage()
		os.Exit(1)
	}
}

func isHelp(s string) bool {
	return s == "help" || s == "--help" || s == "-h"
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage: zcp analyze <subcommand> [flags]

Subcommands:
  recipe-run          Measure a zcprecipator2 deliverable tree + session logs;
                      emit machine-report.json.
  generate-checklist  Read machine-report.json; emit the analyst worksheet.

Examples:
  zcp analyze recipe-run \
    /Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v36 \
    /Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v36/SESSIONS_LOGS \
    --run v36 --tier showcase --slug nestjs-showcase \
    --out docs/zcprecipator2/runs/v36/machine-report.json

  zcp analyze generate-checklist docs/zcprecipator2/runs/v36/machine-report.json \
    --out docs/zcprecipator2/runs/v36/verification-checklist.md`)
}

// parseFlags does a minimal double-dash flag split. Returns (positional, flagMap).
// Flags that don't take a value are not supported in this harness; every
// flag requires an argument.
func parseFlags(args []string, knownFlags map[string]bool) ([]string, map[string]string, error) {
	flags := make(map[string]string)
	var positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--") {
			if !knownFlags[a] {
				return nil, nil, fmt.Errorf("unknown flag %s", a)
			}
			if i+1 >= len(args) {
				return nil, nil, fmt.Errorf("%s requires a value", a)
			}
			flags[a] = args[i+1]
			i++
			continue
		}
		positional = append(positional, a)
	}
	return positional, flags, nil
}
