package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// runDryRun is the entry point for `zcp dry-run`. Dispatches to
// subcommand handlers; currently only `recipe` is implemented.
func runDryRun(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: zcp dry-run <subcommand>\n\nSubcommands:\n  recipe    exercise the recipe dispatch stitcher and (optionally) diff against goldens")
		os.Exit(1)
	}
	switch args[0] {
	case "recipe":
		os.Exit(runDryRunRecipe(args[1:], os.Stdout, os.Stderr))
	default:
		fmt.Fprintf(os.Stderr, "unknown dry-run subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

// runDryRunRecipe is the testable core of `zcp dry-run recipe`. It
// composes every dispatch brief for the requested (tier × dual-runtime)
// shape using the live `Build<Role>DispatchBrief` stitchers and reports
// per-role atom-reference markers + byte counts. When `--against=<dir>`
// is provided, each composed brief is diffed against the golden at
// `<dir>/brief-<role>-<tier>-composed.md` and differences are surfaced.
//
// Exit code: 0 when every brief composes successfully AND (if --against
// was given) no golden diff was reported; 1 otherwise. The pre-v35 ship
// gate per rollout-sequence.md §C-14 uses this to confirm the stitcher
// remains aligned with the research-phase goldens.
func runDryRunRecipe(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("zcp dry-run recipe", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tier := fs.String("tier", "showcase", "recipe tier (showcase|minimal)")
	dualRuntime := fs.Bool("dual-runtime", false, "minimal-tier only: include a second runtime codebase (frontend + api)")
	against := fs.String("against", "", "optional: directory containing brief-<role>-<tier>-composed.md golden files to diff against")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *tier != workflow.RecipeTierShowcase && *tier != workflow.RecipeTierMinimal {
		fmt.Fprintf(stderr, "unknown tier %q — use showcase or minimal\n", *tier)
		return 1
	}

	plan := synthesizeDryRunPlan(*tier, *dualRuntime)
	briefs, composeErrs := composeAllBriefs(plan)

	fmt.Fprintf(stdout, "dry-run recipe: tier=%s dual-runtime=%v targets=%d\n", *tier, *dualRuntime, len(plan.Targets))
	for _, name := range sortedBriefNames(briefs) {
		fmt.Fprintf(stdout, "  %s: %d bytes\n", name, len(briefs[name]))
	}

	exit := 0
	if len(composeErrs) > 0 {
		for _, e := range composeErrs {
			fmt.Fprintf(stderr, "compose error: %v\n", e)
		}
		exit = 1
	}
	if *against != "" {
		diffs := diffBriefsAgainstGoldens(briefs, *tier, *against)
		for _, d := range diffs {
			fmt.Fprintf(stdout, "  [diff] %s\n", d)
		}
		if len(diffs) > 0 {
			exit = 1
		}
	}
	if exit == 0 {
		fmt.Fprintln(stdout, "dry-run recipe: ok")
	}
	return exit
}

// synthesizeDryRunPlan builds a minimal RecipePlan matching the given
// tier + dual-runtime shape. Hostnames and service types are canonical
// placeholders — the dry-run exercises composition shape, not plan
// content, so the specific tokens don't matter beyond satisfying the
// stitcher's plan-shape assumptions.
func synthesizeDryRunPlan(tier string, dualRuntime bool) *workflow.RecipePlan {
	p := &workflow.RecipePlan{
		Slug:        "dry-run-" + tier,
		Framework:   "nestjs",
		Tier:        tier,
		RuntimeType: "nodejs@22",
	}
	if tier == workflow.RecipeTierShowcase {
		p.Targets = []workflow.RecipeTarget{
			{Hostname: "app", Type: "static", Role: "app"},
			{Hostname: "api", Type: "nodejs@22", Role: "api"},
			{Hostname: "worker", Type: "nodejs@22", IsWorker: true, Role: "worker"},
			{Hostname: "db", Type: "postgresql@17"},
		}
		return p
	}
	// minimal
	if dualRuntime {
		p.Targets = []workflow.RecipeTarget{
			{Hostname: "app", Type: "static", Role: "app"},
			{Hostname: "api", Type: "nodejs@22", Role: "api"},
			{Hostname: "db", Type: "postgresql@17"},
		}
		return p
	}
	p.Targets = []workflow.RecipeTarget{
		{Hostname: "app", Type: "nodejs@22", Role: "app"},
		{Hostname: "db", Type: "postgresql@17"},
	}
	return p
}

// composeAllBriefs calls every Build<Role>DispatchBrief for the given
// plan and returns a map of brief-name → composed output. Scaffold
// dispatches are keyed per-target (role tag) so the shape proves every
// runtime target gets a brief. Feature + writer + code-review +
// editorial-review are showcase-only; calling them on a minimal plan
// is intentional in dry-run to verify the "empty when not showcase"
// contract holds.
//
// Returns a list of composition errors; a nil error means every brief
// composed successfully.
func composeAllBriefs(plan *workflow.RecipePlan) (map[string]string, []error) {
	briefs := map[string]string{}
	var errs []error

	// Scaffold briefs: one per runtime target (skip managed services +
	// shared-codebase workers — the stitcher doesn't emit for those).
	for _, t := range plan.Targets {
		if !workflow.IsRuntimeType(t.Type) {
			continue
		}
		if t.IsWorker && t.SharesCodebaseWith != "" {
			continue
		}
		role := "app"
		if t.Role != "" {
			role = t.Role
		}
		key := "scaffold-" + t.Hostname + "-" + role
		body, err := workflow.BuildScaffoldDispatchBrief(plan, role)
		if err != nil {
			errs = append(errs, fmt.Errorf("scaffold %s: %w", t.Hostname, err))
			continue
		}
		briefs[key] = body
	}

	if body, err := workflow.BuildFeatureDispatchBrief(plan); err != nil {
		errs = append(errs, fmt.Errorf("feature: %w", err))
	} else if body != "" {
		briefs["feature"] = body
	}

	if body, err := workflow.BuildWriterDispatchBrief(plan, "/tmp/dry-run-facts.jsonl"); err != nil {
		errs = append(errs, fmt.Errorf("writer: %w", err))
	} else if body != "" {
		briefs["writer"] = body
	}

	if body, err := workflow.BuildCodeReviewDispatchBrief(plan, "/tmp/dry-run/ZCP_CONTENT_MANIFEST.json"); err != nil {
		errs = append(errs, fmt.Errorf("code-review: %w", err))
	} else if body != "" {
		briefs["code-review"] = body
	}

	if body, err := workflow.BuildEditorialReviewDispatchBrief(plan, "/tmp/dry-run-facts.jsonl", "/tmp/dry-run/ZCP_CONTENT_MANIFEST.json"); err != nil {
		errs = append(errs, fmt.Errorf("editorial-review: %w", err))
	} else if body != "" {
		briefs["editorial-review"] = body
	}

	return briefs, errs
}

// diffBriefsAgainstGoldens compares each composed brief against the
// corresponding golden at `<againstDir>/brief-<role>-<tier>-composed.md`.
// The step-4 goldens are synthetic reader-facing representations, not
// byte-identical copies of stitcher output, so this check is
// substring-based: it asserts that every major atom-marker heading the
// golden declares in its "Composed prompt" stanza appears in the
// stitcher output. A diff entry is emitted per missing marker.
//
// Returns a list of human-readable diff strings; empty when every
// brief matches every marker. Goldens that don't exist are skipped
// silently (a role with no golden is not a failure — editorial-review's
// goldens may land later than scaffold's, etc.).
func diffBriefsAgainstGoldens(briefs map[string]string, tier, againstDir string) []string {
	var diffs []string
	for role, body := range briefs {
		// Scaffold goldens are keyed by codebase (apidev / appdev / workerdev);
		// the role key in the brief map already names this, but the golden
		// filename convention is brief-scaffold-{host}dev-{tier}-composed.md.
		var goldenPath string
		if scaffoldSuffix, ok := strings.CutPrefix(role, "scaffold-"); ok {
			parts := strings.Split(scaffoldSuffix, "-")
			if len(parts) < 1 {
				continue
			}
			host := parts[0]
			goldenPath = filepath.Join(againstDir, fmt.Sprintf("brief-scaffold-%sdev-%s-composed.md", host, tier))
		} else {
			goldenPath = filepath.Join(againstDir, fmt.Sprintf("brief-%s-%s-composed.md", role, tier))
		}
		data, err := os.ReadFile(goldenPath)
		if err != nil {
			// Golden missing → silent skip (not a failure).
			continue
		}
		golden := string(data)
		// Collect atom-marker headings from the golden: lines starting
		// with "# " that look like atom section names (Mandatory core,
		// Porter premise, etc.).
		goldenHeadings := extractAtomHeadings(golden)
		for _, h := range goldenHeadings {
			// Strip parenthesized suffixes the golden may carry as
			// editorial annotation ("Single-question tests (from §X)")
			// — the atom's H1 is the canonical heading, and the
			// annotation is reader-facing advisory context that need
			// not appear in the transmitted prompt.
			matchable := h
			if paren := strings.Index(matchable, " ("); paren >= 0 {
				matchable = matchable[:paren]
			}
			if !strings.Contains(body, matchable) {
				diffs = append(diffs, fmt.Sprintf("%s: missing atom heading %q (declared in %s)", role, h, filepath.Base(goldenPath)))
			}
		}
	}
	sort.Strings(diffs)
	return diffs
}

// extractAtomHeadings pulls `# <heading>` lines from within the
// "Composed prompt" fenced block of a golden file. Returns the list of
// heading strings (without the `# ` prefix, trimmed).
func extractAtomHeadings(golden string) []string {
	var out []string
	inComposed := false
	for line := range strings.SplitSeq(golden, "\n") {
		if strings.HasPrefix(line, "## Composed prompt") {
			inComposed = true
			continue
		}
		if !inComposed {
			continue
		}
		if strings.HasPrefix(line, "# ") && !strings.HasPrefix(line, "## ") {
			h := strings.TrimSpace(strings.TrimPrefix(line, "# "))
			// Skip non-atom artifacts like comment lines.
			if h == "" || strings.HasPrefix(h, "(") {
				continue
			}
			out = append(out, h)
		}
	}
	return out
}

// sortedBriefNames returns brief-map keys in stable order so stdout
// reports are reproducible across runs.
func sortedBriefNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
