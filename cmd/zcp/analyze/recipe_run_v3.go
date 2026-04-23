package analyze

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/zeropsio/zcp/internal/analyze"
	"github.com/zeropsio/zcp/internal/recipe"
)

const recipeRunV3Usage = `Usage: zcp analyze recipe-run-v3 <run-dir> [flags]

Walks a zcprecipator3 run directory, writes analysis/ outputs — raw
tree, per-agent summaries, dispatch integrity, surface validation,
content authorship. Supports --baseline for delta mode.

Flags:
  --slug <name>           Recipe slug. Default: basename of <run-dir>.
  --run  <label>          Run label. Default: inferred from basename.
  --logs <dir>            Sessions-logs dir. Default: <run-dir>/SESSIONS_LOGS.
  --baseline <run-dir>    Prior run's analysis/ root for delta mode.
  --out <dir>             Override the analysis/ output root. Default:
                          <run-dir>/analysis/.`

func runRecipeRunV3(args []string) {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprintln(os.Stderr, recipeRunV3Usage)
		if len(args) == 0 {
			os.Exit(1)
		}
		return
	}

	known := map[string]bool{
		"--slug": true, "--run": true, "--logs": true,
		"--baseline": true, "--out": true,
	}
	positional, flags, err := parseFlags(args, known)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if len(positional) != 1 {
		fmt.Fprintln(os.Stderr, recipeRunV3Usage)
		os.Exit(1)
	}

	runDir, err := filepath.Abs(positional[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	slug := flags["--slug"]
	if slug == "" {
		slug = filepath.Base(runDir)
	}
	run := flags["--run"]
	if run == "" {
		run = inferRun(filepath.Base(runDir))
	}
	logs := flags["--logs"]
	if logs == "" {
		logs = filepath.Join(runDir, "SESSIONS_LOGS")
	}
	outputDir := flags["--out"]
	if outputDir == "" {
		outputDir = filepath.Join(runDir, "analysis")
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir output: %v\n", err)
		os.Exit(1)
	}

	// Step 1: raw-walk sessions logs → tree / per-agent raw dumps.
	if _, err := os.Stat(logs); err == nil {
		tree, err := analyze.WalkSessionsLogs(logs, outputDir, slug, run)
		if err != nil {
			fmt.Fprintf(os.Stderr, "raw-walk: %v\n", err)
			os.Exit(1)
		}

		// Step 2: per-agent summaries.
		summaries, err := analyze.WriteAgentSummaries(tree, outputDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "agent-summaries: %v\n", err)
			os.Exit(1)
		}

		// Step 3: dispatch integrity (engine-build side is empty for
		// Commission A — Commission B wires the live rebuild).
		if _, err := analyze.WriteDispatchReports(summaries, nil, outputDir); err != nil {
			fmt.Fprintf(os.Stderr, "dispatch-reports: %v\n", err)
			os.Exit(1)
		}

		// Step 4: content authorship with writer-vs-main detection.
		if _, err := analyze.WriteContentAuthorship(runDir, outputDir, tree); err != nil {
			fmt.Fprintf(os.Stderr, "content-authorship: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Fprintf(os.Stderr, "note: logs dir %s not found; skipping raw/agent/dispatch/content steps\n", logs)
	}

	// Step 5: surface validation (independent of logs — reads the recipe
	// output tree directly).
	if _, err := analyze.WriteSurfaceReports(runDir, outputDir, emptySurfaceInputs()); err != nil {
		fmt.Fprintf(os.Stderr, "surface-reports: %v\n", err)
		os.Exit(1)
	}

	// Step 6: delta against baseline if supplied.
	if baseline := flags["--baseline"]; baseline != "" {
		abs, err := filepath.Abs(baseline)
		if err != nil {
			fmt.Fprintf(os.Stderr, "baseline path: %v\n", err)
			os.Exit(1)
		}
		if _, err := analyze.WriteDelta(abs, outputDir); err != nil {
			fmt.Fprintf(os.Stderr, "delta: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Fprintf(os.Stdout, "analysis written to %s\n", outputDir)
}

// emptySurfaceInputs returns SurfaceInputs with zero plan/facts/parent.
// Commission A runs validators against just-the-files; Commission B
// wires the live session's plan + facts log in.
func emptySurfaceInputs() recipe.SurfaceInputs {
	return recipe.SurfaceInputs{}
}
