package analyze

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/authoring/analyze"
	"github.com/zeropsio/zcp/internal/authoring/recipe"
)

// inferRun derives a run label from a run-directory basename of the form
// "<slug>-v<N>" (e.g. "nestjs-showcase-v36" -> "v36"). Falls back to the
// whole basename when no "-v" marker is present.
func inferRun(basename string) string {
	idx := strings.LastIndex(basename, "-v")
	if idx < 0 || idx+2 >= len(basename) {
		return basename
	}
	candidate := basename[idx+1:] // "v36"
	if len(candidate) < 2 {
		return basename
	}
	return candidate
}

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

func runRecipeRunV3(args []string) int {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprintln(os.Stderr, recipeRunV3Usage)
		if len(args) == 0 {
			return 1
		}
		return 0
	}

	known := map[string]bool{
		"--slug": true, "--run": true, "--logs": true,
		"--baseline": true, "--out": true,
	}
	positional, flags, err := parseFlags(args, known)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if len(positional) != 1 {
		fmt.Fprintln(os.Stderr, recipeRunV3Usage)
		return 1
	}

	runDir, err := filepath.Abs(positional[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
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
		return 1
	}

	// Step 1: raw-walk sessions logs → tree / per-agent raw dumps.
	if _, err := os.Stat(logs); err == nil {
		tree, err := analyze.WalkSessionsLogs(logs, outputDir, slug, run)
		if err != nil {
			fmt.Fprintf(os.Stderr, "raw-walk: %v\n", err)
			return 1
		}

		// Step 2: per-agent summaries.
		summaries, err := analyze.WriteAgentSummaries(tree, outputDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "agent-summaries: %v\n", err)
			return 1
		}

		// Step 3: dispatch integrity (engine-build side is empty for
		// Commission A — Commission B wires the live rebuild).
		if _, err := analyze.WriteDispatchReports(summaries, nil, outputDir); err != nil {
			fmt.Fprintf(os.Stderr, "dispatch-reports: %v\n", err)
			return 1
		}

		// Step 4: content authorship with writer-vs-main detection.
		if _, err := analyze.WriteContentAuthorship(runDir, outputDir, tree); err != nil {
			fmt.Fprintf(os.Stderr, "content-authorship: %v\n", err)
			return 1
		}
	} else {
		fmt.Fprintf(os.Stderr, "note: logs dir %s not found; skipping raw/agent/dispatch/content steps\n", logs)
	}

	// Step 5: surface validation (independent of logs — reads the recipe
	// output tree directly).
	if _, err := analyze.WriteSurfaceReports(runDir, outputDir, emptySurfaceInputs()); err != nil {
		fmt.Fprintf(os.Stderr, "surface-reports: %v\n", err)
		return 1
	}

	// Step 6: delta against baseline if supplied.
	if baseline := flags["--baseline"]; baseline != "" {
		abs, err := filepath.Abs(baseline)
		if err != nil {
			fmt.Fprintf(os.Stderr, "baseline path: %v\n", err)
			return 1
		}
		if _, err := analyze.WriteDelta(abs, outputDir); err != nil {
			fmt.Fprintf(os.Stderr, "delta: %v\n", err)
			return 1
		}
	}

	fmt.Fprintf(os.Stdout, "analysis written to %s\n", outputDir)
	return 0
}

// emptySurfaceInputs returns SurfaceInputs with zero plan/facts/parent.
// Commission A runs validators against just-the-files; Commission B
// wires the live session's plan + facts log in.
func emptySurfaceInputs() recipe.SurfaceInputs {
	return recipe.SurfaceInputs{}
}
