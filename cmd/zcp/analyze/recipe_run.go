package analyze

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/analyze"
)

const recipeRunUsage = `Usage: zcp analyze recipe-run <deliverable-dir> <sessions-logs-dir> [flags]

Flags:
  --tier <showcase|minimal>         Default: showcase.
  --slug <name>                     Recipe slug. Default: basename of deliverable.
  --run  <label>                    Run label (e.g. v36, v37). Default: inferred from basename.
  --app-codebases <a,b,c>           Comma-separated codebase subdirs. Default: auto-discover.
  --out <file>                      Write the JSON report here. Default: stdout.`

func runRecipeRun(args []string) {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprintln(os.Stderr, recipeRunUsage)
		if len(args) == 0 {
			os.Exit(1)
		}
		return
	}
	known := map[string]bool{
		"--tier": true, "--slug": true, "--run": true,
		"--app-codebases": true, "--out": true,
	}
	positional, flags, err := parseFlags(args, known)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if len(positional) < 1 || len(positional) > 2 {
		fmt.Fprintf(os.Stderr, "error: expected <deliverable-dir> [sessions-logs-dir], got %d positional args\n", len(positional))
		fmt.Fprintln(os.Stderr, recipeRunUsage)
		os.Exit(1)
	}
	deliverable, err := filepath.Abs(positional[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	var sessionsLogs string
	if len(positional) == 2 {
		sessionsLogs, err = filepath.Abs(positional[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Auto-discover SESSIONS_LOGS under deliverable if present.
		candidate := filepath.Join(deliverable, "SESSIONS_LOGS")
		if _, err := os.Stat(candidate); err == nil {
			sessionsLogs = candidate
		}
	}

	tier := flags["--tier"]
	if tier == "" {
		tier = "showcase"
	}
	slug := flags["--slug"]
	if slug == "" {
		slug = filepath.Base(deliverable)
	}
	run := flags["--run"]
	if run == "" {
		run = inferRun(filepath.Base(deliverable))
	}
	var appCodebases []string
	if raw := flags["--app-codebases"]; raw != "" {
		for cb := range strings.SplitSeq(raw, ",") {
			cb = strings.TrimSpace(cb)
			if cb != "" {
				appCodebases = append(appCodebases, cb)
			}
		}
	}

	report, err := analyze.RunRecipeAnalysis(analyze.ReportInput{
		DeliverableDir:  deliverable,
		SessionsLogsDir: sessionsLogs,
		Tier:            tier,
		Slug:            slug,
		Run:             run,
		AppCodebases:    appCodebases,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	var buf bytes.Buffer
	if err := analyze.WriteReport(&buf, report); err != nil {
		fmt.Fprintf(os.Stderr, "error: encode report: %v\n", err)
		os.Exit(2)
	}

	out := flags["--out"]
	if out == "" {
		if _, err := os.Stdout.Write(buf.Bytes()); err != nil {
			fmt.Fprintf(os.Stderr, "error: write stdout: %v\n", err)
			os.Exit(2)
		}
		printSummary(os.Stderr, report)
		return
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error: mkdir out dir: %v\n", err)
		os.Exit(2)
	}
	if err := os.WriteFile(out, buf.Bytes(), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "error: write %s: %v\n", out, err)
		os.Exit(2)
	}
	fmt.Fprintf(os.Stderr, "machine-report written: %s\n", out)
	printSummary(os.Stderr, report)
}

// inferRun pulls the run label from a deliverable basename like
// "nestjs-showcase-v36" or "bun-hello-world-v8". Falls back to the
// full basename when no -vNN suffix is found.
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

// printSummary writes a one-line-per-bar status dump to w. Designed
// for quick human review during validation runs.
func printSummary(w *os.File, r *analyze.MachineReport) {
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "=== analyze recipe-run summary ===")
	type row struct {
		id, name string
		res      analyze.BarResult
	}
	rows := []row{
		{"B-15", "ghost_env_dirs", r.StructuralIntegrity.GhostEnvDirs},
		{"B-16", "tarball_per_codebase_md", r.StructuralIntegrity.TarballPerCodebaseMd},
		{"B-17", "marker_exact_form", r.StructuralIntegrity.MarkerExactForm},
		{"B-18", "standalone_duplicate_files", r.StructuralIntegrity.StandaloneDuplicateFiles},
		{"B-22", "atom_template_vars_bound", r.StructuralIntegrity.AtomTemplateVarsBound},
		{"B-20", "deploy_readmes_retry_rounds", r.SessionMetrics.DeployReadmesRetryRounds},
		{"B-21", "sessionless_export_attempts", r.SessionMetrics.SessionlessExportAttempts},
		{"B-23", "writer_first_pass_failures", r.SessionMetrics.WriterFirstPassFailures},
	}
	for _, row := range rows {
		fmt.Fprintf(w, "%s %-32s threshold=%d observed=%d status=%s\n",
			row.id, row.name, row.res.Threshold, row.res.Observed, row.res.Status)
	}
	fmt.Fprintf(w, "sub_agent_count=%d close_complete=%v editorial=%v code_review=%v\n",
		r.SessionMetrics.SubAgentCount,
		r.SessionMetrics.CloseStepCompleted,
		r.SessionMetrics.EditorialReviewDispatched,
		r.SessionMetrics.CodeReviewDispatched,
	)
}
