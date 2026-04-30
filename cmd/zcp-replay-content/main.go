// zcp-replay-content composes codebase-content + env-content sub-agent
// dispatch prompts offline from a frozen run's plan.json + facts.jsonl.
// Drives the run-18 replay loop (docs/zcprecipator3/plans/run-18-prep.md
// §5): atom + brief-composer changes can be exercised without running
// provision/scaffold/feature against a live platform.
//
// Critically, the dispatched prompt is BYTE-IDENTICAL to what the
// production engine emits via `zerops_recipe action=build-subagent-
// prompt`. The replay tool prepends a single short adapter that
// redirects record-fragment to file-write — every other instruction
// (header, recipe-level context, brief atoms, close footer) comes from
// the engine. A brief atom or composer edit shows up in simulation and
// production identically.
//
// Usage:
//
//	zcp-replay-content -run docs/zcprecipator3/runs/17 -out /tmp/replay
//
// Reads:
//   - <run>/environments/plan.json
//   - <run>/environments/facts.jsonl
//
// Writes:
//   - <out>/briefs/<codebase>-prompt.md  — full dispatch prompt
//     (canonical engine output + thin replay adapter prefix). One
//     prompt per codebase + one for env-content.
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

func main() {
	runDir := flag.String("run", "docs/zcprecipator3/runs/17", "run directory containing environments/{plan.json,facts.jsonl}")
	outDir := flag.String("out", "/tmp/replay", "output directory for dispatch prompts")
	mountRoot := flag.String("mount-root", "", "recipes mount root (threaded into engine for chain resolution; empty in run-17 simulation)")
	flag.Parse()

	if err := run(*runDir, *outDir, *mountRoot); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(runDir, outDir, mountRoot string) error {
	envDir := filepath.Join(runDir, "environments")
	plan, err := recipe.ReadPlan(envDir)
	if err != nil {
		return fmt.Errorf("read plan: %w", err)
	}
	facts, err := loadFactsJSONL(filepath.Join(envDir, "facts.jsonl"))
	if err != nil {
		return fmt.Errorf("load facts: %w", err)
	}

	briefsDir := filepath.Join(outDir, "briefs")
	if err := os.MkdirAll(briefsDir, 0o755); err != nil {
		return err
	}
	fragsDir := filepath.Join(outDir, "fragments-new")
	if err := os.MkdirAll(fragsDir, 0o755); err != nil {
		return err
	}

	// One prompt per codebase (codebase-content kind).
	for _, cb := range plan.Codebases {
		cbFragDir := filepath.Join(fragsDir, cb.Hostname)
		if err := os.MkdirAll(cbFragDir, 0o755); err != nil {
			return err
		}

		input := recipe.RecipeInput{
			BriefKind: string(recipe.BriefCodebaseContent),
			Codebase:  cb.Hostname,
			Slug:      plan.Slug,
		}
		canonical, err := recipe.BuildSubagentPromptForReplay(plan, nil, input, recipe.PhaseCodebaseContent, mountRoot, facts)
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
	envFragDir := filepath.Join(fragsDir, "env")
	if err := os.MkdirAll(envFragDir, 0o755); err != nil {
		return err
	}
	envInput := recipe.RecipeInput{
		BriefKind: string(recipe.BriefEnvContent),
		Slug:      plan.Slug,
	}
	envCanonical, err := recipe.BuildSubagentPromptForReplay(plan, nil, envInput, recipe.PhaseEnvContent, mountRoot, facts)
	if err != nil {
		return fmt.Errorf("BuildSubagentPromptForReplay env-content: %w", err)
	}
	envFull := replayAdapter(envFragDir) + envCanonical
	envPromptPath := filepath.Join(briefsDir, "env-prompt.md")
	if err := os.WriteFile(envPromptPath, []byte(envFull), 0o600); err != nil {
		return fmt.Errorf("write env prompt: %w", err)
	}
	fmt.Printf("env: %d bytes → %s\n", len(envFull), envPromptPath)
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

func loadFactsJSONL(path string) ([]recipe.FactRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

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
