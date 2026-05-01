// emit subcommand — stages frozen scaffold output (zerops.yaml with
// comments stripped, plus plan.json + facts.jsonl) into the simulation
// directory and emits dispatch prompts byte-identical to the production
// engine output.
//
// User then dispatches one Agent per prompt; each agent reads from the
// staged simulation dir and writes fragments to fragments-new/.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/recipe"
)

func runEmit(args []string) error {
	fs := flag.NewFlagSet("emit", flag.ContinueOnError)
	runDir := fs.String("run", "", "frozen run directory containing environments/{plan.json,facts.jsonl} and <host>dev/zerops.yaml")
	outDir := fs.String("out", "", "simulation output directory (typically docs/zcprecipator3/simulations/<N>)")
	mountRoot := fs.String("mount-root", "", "recipes mount root for parent chain resolution (run-20 prep S4); when set, the emit step loads parent via ResolveChain(plan.Slug) and threads it through the brief composers")
	parentSlugOverride := fs.String("parent", "", "parent recipe slug override (run-20 prep S4); when set, plan.Slug is rewritten in-memory to <parent>-showcase before chain resolution; requires -mount-root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *runDir == "" || *outDir == "" {
		return errors.New("emit: -run and -out are required")
	}
	if *parentSlugOverride != "" && *mountRoot == "" {
		return errors.New("emit: -parent requires -mount-root (chain resolver loads the parent tree from <mount-root>/<parent>/)")
	}

	envIn := filepath.Join(*runDir, "environments")
	plan, err := recipe.ReadPlan(envIn)
	if err != nil {
		return fmt.Errorf("read plan from %s: %w", envIn, err)
	}
	facts, err := loadFactsJSONL(filepath.Join(envIn, "facts.jsonl"))
	if err != nil {
		return fmt.Errorf("load facts: %w", err)
	}

	// Stage plan + facts.
	envOut := filepath.Join(*outDir, "environments")
	if err := os.MkdirAll(envOut, 0o755); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(envIn, "plan.json"), filepath.Join(envOut, "plan.json")); err != nil {
		return fmt.Errorf("copy plan.json: %w", err)
	}
	if err := copyFile(filepath.Join(envIn, "facts.jsonl"), filepath.Join(envOut, "facts.jsonl")); err != nil {
		return fmt.Errorf("copy facts.jsonl: %w", err)
	}

	// Stage per-codebase bare yaml — copy from runs/N + strip comments,
	// then rewrite plan.json to point SourceRoot at the simulation
	// directory so engine assembles read from the staged path.
	absOut, err := filepath.Abs(*outDir)
	if err != nil {
		return fmt.Errorf("abs(%s): %w", *outDir, err)
	}
	for i, cb := range plan.Codebases {
		runHostDir := filepath.Join(*runDir, cb.Hostname+"dev")
		runYAML := filepath.Join(runHostDir, "zerops.yaml")
		raw, err := os.ReadFile(runYAML)
		if err != nil {
			return fmt.Errorf("read %s: %w", runYAML, err)
		}
		// SourceRoot must end in `dev` per engine M-1; mirror the
		// frozen run's layout.
		simSrcRoot := filepath.Join(absOut, cb.Hostname+"dev")
		if err := os.MkdirAll(simSrcRoot, 0o755); err != nil {
			return err
		}
		bare := stripYAMLCommentsForEmit(string(raw))
		if err := os.WriteFile(filepath.Join(simSrcRoot, "zerops.yaml"), []byte(bare), 0o600); err != nil {
			return fmt.Errorf("write bare yaml: %w", err)
		}
		// Run-20 prep S3 — codebase-content + claudemd-author briefs
		// reference src/**, package.json, composer.json, app/**. Stage
		// the union from the frozen run dir so the replayed sub-agent
		// runs against the same file shape it would in production.
		// Skips node_modules/, vendor/, .git/ at every depth.
		if err := stageCodebaseArtifacts(runHostDir, simSrcRoot); err != nil {
			return fmt.Errorf("stage codebase %s artifacts: %w", cb.Hostname, err)
		}
		plan.Codebases[i].SourceRoot = simSrcRoot
		fmt.Printf("staged %s/zerops.yaml (%d → %d bytes after strip) + code artifacts\n",
			cb.Hostname, len(raw), len(bare))
	}
	// Persist the SourceRoot-rewritten plan so stitch reads the staged
	// paths, not the frozen run's.
	if err := recipe.WritePlan(envOut, plan); err != nil {
		return fmt.Errorf("write rewritten plan: %w", err)
	}

	// Emit dispatch prompts.
	briefsDir := filepath.Join(*outDir, "briefs")
	if err := os.MkdirAll(briefsDir, 0o755); err != nil {
		return err
	}
	fragsRoot := filepath.Join(*outDir, "fragments-new")
	if err := os.MkdirAll(fragsRoot, 0o755); err != nil {
		return err
	}

	// Run-20 prep S4 — load parent recipe via chain resolver when
	// `-mount-root` is set. Production sessions populate Session.Parent
	// from ResolveChain at session bootstrap; the codebase-content
	// brief composer's parent_recipe_dedup pointer block depends on a
	// non-nil *ParentRecipe. Without this, the sim path verifies the
	// composer at parent=nil only.
	parent, err := loadEmitParent(plan.Slug, *parentSlugOverride, *mountRoot)
	if err != nil {
		return err
	}

	// One prompt per codebase (codebase-content kind).
	for _, cb := range plan.Codebases {
		cbFragDir := filepath.Join(fragsRoot, cb.Hostname)
		if err := os.MkdirAll(cbFragDir, 0o755); err != nil {
			return err
		}
		input := recipe.RecipeInput{
			BriefKind: string(recipe.BriefCodebaseContent),
			Codebase:  cb.Hostname,
			Slug:      plan.Slug,
		}
		canonical, err := recipe.BuildSubagentPromptForReplay(plan, parent, input, recipe.PhaseCodebaseContent, *mountRoot, facts)
		if err != nil {
			return fmt.Errorf("BuildSubagentPromptForReplay codebase=%s: %w", cb.Hostname, err)
		}
		full := replayAdapter(cbFragDir) + canonical
		promptPath := filepath.Join(briefsDir, cb.Hostname+"-prompt.md")
		if err := os.WriteFile(promptPath, []byte(full), 0o600); err != nil {
			return fmt.Errorf("write prompt: %w", err)
		}
		fmt.Printf("%s: %d bytes → %s\n", cb.Hostname, len(full), promptPath)
	}

	// One prompt for env-content (single sub-agent for all env surfaces).
	envFragDir := filepath.Join(fragsRoot, "env")
	if err := os.MkdirAll(envFragDir, 0o755); err != nil {
		return err
	}
	envInput := recipe.RecipeInput{
		BriefKind: string(recipe.BriefEnvContent),
		Slug:      plan.Slug,
	}
	envCanonical, err := recipe.BuildSubagentPromptForReplay(plan, parent, envInput, recipe.PhaseEnvContent, *mountRoot, facts)
	if err != nil {
		return fmt.Errorf("BuildSubagentPromptForReplay env-content: %w", err)
	}
	envFull := replayAdapter(envFragDir) + envCanonical
	envPromptPath := filepath.Join(briefsDir, "env-prompt.md")
	if err := os.WriteFile(envPromptPath, []byte(envFull), 0o600); err != nil {
		return fmt.Errorf("write env prompt: %w", err)
	}
	fmt.Printf("env: %d bytes → %s\n", len(envFull), envPromptPath)

	fmt.Printf("\nready: dispatch %d Agent calls (one per prompt) then run `zcp-recipe-sim stitch -dir %s`\n",
		len(plan.Codebases)+1, *outDir)
	return nil
}

// replayAdapter is the only divergence from the canonical engine
// prompt: it tells the agent to write fragments as plain markdown
// files instead of calling the record-fragment MCP tool. Everything
// the agent should learn about the recipe, voice, surfaces, anti-
// patterns, etc. lives downstream in the engine-emitted brief.
func replayAdapter(fragmentsDir string) string {
	return fmt.Sprintf(`<replay-adapter>
This is an offline replay. There is no live recipe session and no MCP
endpoint. Wherever the engine brief below instructs you to call
`+"`zerops_recipe action=record-fragment`"+`, instead use the `+"`Write`"+`
tool to author the fragment as a markdown file under:

    %s/

One file per fragment id. Filename = the fragment id with every '/'
replaced by '__', plus the '.md' suffix. Example: fragment id
`+"`codebase/api/integration-guide/2`"+` becomes file
`+"`codebase__api__integration-guide__2.md`"+`. Body = the fragment body
verbatim, no JSON wrapper.

Other MCP tools (`+"`zerops_recipe`"+` action=*, `+"`zerops_knowledge`"+`,
`+"`zerops_*`"+`) will fail in this replay — read on-disk content via
`+"`Read`"+` / `+"`Glob`"+` / `+"`Grep`"+` instead. The `+"`complete-phase`"+`
self-validate step has no analog here; just author every required
fragment, then terminate with a final report listing every file you
wrote and any close-criteria you couldn't satisfy.
</replay-adapter>

`, fragmentsDir)
}

// stripYAMLCommentsForEmit mirrors recipe.stripYAMLComments but lives
// here because the engine helper is package-private. Drops `^\s*#`
// lines (preserving the `#zeropsPreprocessor=on` shebang at line 0)
// while leaving inline trailing comments on data lines untouched.
func stripYAMLCommentsForEmit(yamlBody string) string {
	lines := strings.Split(yamlBody, "\n")
	out := make([]string, 0, len(lines))
	for i, ln := range lines {
		trimmed := strings.TrimLeft(ln, " \t")
		switch {
		case i == 0 && strings.HasPrefix(trimmed, "#zeropsPreprocessor="):
			out = append(out, ln)
		case strings.HasPrefix(trimmed, "#"):
			continue
		default:
			out = append(out, ln)
		}
	}
	return strings.Join(out, "\n")
}

func copyFile(src, dst string) error {
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, in, 0o600)
}

func loadFactsJSONL(path string) ([]recipe.FactRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var out []recipe.FactRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<20)
	for sc.Scan() {
		ln := strings.TrimSpace(sc.Text())
		if ln == "" {
			continue
		}
		var r recipe.FactRecord
		if err := json.Unmarshal([]byte(ln), &r); err != nil {
			return nil, fmt.Errorf("unmarshal fact: %w", err)
		}
		out = append(out, r)
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return out, nil
}
