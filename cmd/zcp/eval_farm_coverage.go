package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/eval/farm"
)

// runFarmCoverage implements `zcp eval farm coverage <dir> [--since <batch>]`
// (docs/spec-eval-farm.md §5.3 FM-39/FM-40): it derives the (scenario,
// workflow step, tool decision) cell table from pulled bundles under dir —
// no network — prints it to stdout, and writes the identical body to
// <dir>/coverage.md. Coverage never grades anything (FM-40): its only exit
// codes are 0 and a usage error.
func runFarmCoverage(args []string) int {
	var dir, since string
	for i := 0; i < len(args); i++ {
		arg := args[i] //nolint:gosec // G602 false positive: i is loop-bounded by i < len(args) each iteration.
		switch arg {
		case "--since":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "error: %s requires a value\n", arg)
				return 1
			}
			since = args[i+1]
			i++
		default:
			if !strings.HasPrefix(arg, "--") && dir == "" {
				dir = arg
			}
		}
	}
	if dir == "" {
		fmt.Fprintln(os.Stderr, "error: a batch dir is required")
		return 1
	}

	report, err := farm.Coverage(dir, since)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	body := report.Markdown()
	fmt.Fprint(os.Stdout, body)

	if err := os.WriteFile(filepath.Join(dir, "coverage.md"), []byte(body), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "error: write coverage.md: %v\n", err)
		return 1
	}
	return 0
}
