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
		// the union from the frozen run dir so each replayed sub-agent
		// runs against the same file shape it would in production.
		// Skips node_modules/, vendor/, .git/ at every depth. Both the
		// codebase-content and claudemd-author prompts (run-21 §3) read
		// from this staged tree.
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

	// Run-21 §3 — One prompt per codebase for the claudemd-author sub-
	// agent. Production dispatches this in parallel with codebase-content
	// at phase 5 (briefs_subagent_prompt.go::isCodebaseScopedKind=true).
	// Without it, sim's CLAUDE.md output stays as the 82-byte placeholder
	// the template emits.
	for _, cb := range plan.Codebases {
		cbFragDir := filepath.Join(fragsRoot, cb.Hostname)
		if err := os.MkdirAll(cbFragDir, 0o755); err != nil {
			return err
		}
		input := recipe.RecipeInput{
			BriefKind: string(recipe.BriefClaudeMDAuthor),
			Codebase:  cb.Hostname,
			Slug:      plan.Slug,
		}
		canonical, err := recipe.BuildSubagentPromptForReplay(plan, parent, input, recipe.PhaseCodebaseContent, *mountRoot, facts)
		if err != nil {
			return fmt.Errorf("BuildSubagentPromptForReplay claudemd-author codebase=%s: %w", cb.Hostname, err)
		}
		full := replayAdapter(cbFragDir) + canonical
		promptPath := filepath.Join(briefsDir, cb.Hostname+"-claudemd-prompt.md")
		if err := os.WriteFile(promptPath, []byte(full), 0o600); err != nil {
			return fmt.Errorf("write claudemd prompt: %w", err)
		}
		fmt.Printf("%s claudemd-author: %d bytes → %s\n", cb.Hostname, len(full), promptPath)
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
		2*len(plan.Codebases)+1, *outDir)
	return nil
}

// replayAdapter is the only divergence from the canonical engine
// prompt: it tells the agent to write fragments as plain markdown
// files instead of calling the record-fragment MCP tool, AND it
// substitutes on-disk file reads for the MCP knowledge channels
// (`zerops_knowledge`, `zerops_discover`) that the brief instructs
// the agent to query. Everything else — recipe context, voice,
// surfaces, anti-patterns — lives downstream in the engine-emitted
// brief.
func replayAdapter(fragmentsDir string) string {
	return fmt.Sprintf(`<replay-adapter>
This is an offline replay. There is no live recipe session and no MCP
endpoint. Two substitutions apply for the duration of this prompt:

## 1. record-fragment → write file

Wherever the engine brief below instructs you to call
`+"`zerops_recipe action=record-fragment`"+`, use the `+"`Write`"+`
tool to author the fragment as a markdown file under:

    %s/

One file per fragment id. Filename = the fragment id with every '/'
replaced by '__', plus the '.md' suffix. Example: fragment id
`+"`codebase/api/integration-guide/2`"+` becomes file
`+"`codebase__api__integration-guide__2.md`"+`. Body = the fragment body
verbatim, no JSON wrapper.

## 2. zerops_knowledge → read the on-disk knowledge corpus

Wherever the brief instructs you to call
`+"`zerops_knowledge query=<topic>`"+` or
`+"`zerops_knowledge runtime=<type>`"+`, instead use `+"`Read`"+` /
`+"`Glob`"+` / `+"`Grep`"+` against the on-disk knowledge corpus rooted at:

    /Users/fxck/www/zcp/internal/knowledge/

The corpus layout:

- `+"`internal/knowledge/themes/services.md`"+` — per-service catalog
  (PostgreSQL, MariaDB, Valkey, NATS, Meilisearch, object-storage,
  every managed-service family). Search by service-type heading.
- `+"`internal/knowledge/themes/core.md`"+` — yaml schema reference
  (workspace + deliverable shapes, every directive's allowed values).
- `+"`internal/knowledge/guides/<topic>.md`"+` — platform-wide topic
  guides (deployment-lifecycle, scaling, networking, environment-
  variables, public-access, production-checklist, init-commands,
  rolling-deploys, build-cache, logging, metrics, smtp, vpn,
  zerops-yaml-advanced, etc.). List the directory to see all topics.
- `+"`internal/knowledge/recipes/<slug>.md`"+` — per-recipe markdown
  with framework-specific findings, when present.

If the topic the brief named has no matching file, that's the
"guide silent" fallback — the attestation principle's omit-the-claim
rule applies.

`+"`zerops_discover`"+` and other live-platform MCP tools have no
sim equivalent. The brief's recipe-level metadata (codebases,
services, plan envs) is in the prompt above; use it for any
"discover the env keys this service exposes" type question.

## 3. complete-phase has no analog

Just author every required fragment, then terminate with a final
report listing every file you wrote and any close-criteria you
couldn't satisfy.
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
