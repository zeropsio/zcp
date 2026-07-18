// Package analyze wires the recipe run-analysis harness into the zcp
// CLI. `zcp analyze recipe-run-v3` walks a zcprecipator3 run directory
// and writes the analysis/ outputs (raw tree, per-agent summaries,
// dispatch integrity, surface validation, content authorship, delta).
//
// The command is deliberately free of network access, platform auth, or
// MCP server dependencies. It operates on local artifacts only, so the
// harness is invocable from CI, from a clean developer checkout, and
// from post-run analysis scripts without any Zerops credential plumbing.
package analyze

import (
	"fmt"
	"os"
	"strings"
)

// Run dispatches the subcommand and returns the process exit code. Called
// from cmd/zcp/main.go's dispatch table. args is os.Args[2:] — everything
// after `zcp analyze`.
func Run(args []string) int {
	if len(args) == 0 || isHelp(args[0]) {
		printUsage()
		if len(args) == 0 {
			return 1
		}
		return 0
	}
	switch args[0] {
	case "recipe-run-v3":
		return runRecipeRunV3(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown analyze subcommand: %s\n\n", args[0])
		printUsage()
		return 1
	}
}

func isHelp(s string) bool {
	return s == "help" || s == "--help" || s == "-h"
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage: zcp analyze <subcommand> [flags]

Subcommands:
  recipe-run-v3       zcprecipator3 harness — raw walk + per-agent summaries +
                      dispatch integrity + surface validation + content
                      authorship + delta mode.

Example:
  zcp analyze recipe-run-v3 <run-dir> \
    --slug nestjs-showcase --baseline <prior-run-dir>/analysis`)
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
