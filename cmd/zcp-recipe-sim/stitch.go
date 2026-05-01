// stitch subcommand — assembles the simulated recipe from
// fragments-new/ + plan.json + bare on-disk yaml. Calls the canonical
// engine assembles so simulation output matches production stitch
// shape byte-for-byte (modulo what the agents authored).
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/recipe"
)

// recordFragmentMode is recipe.handlers_fragments.go's mode resolver.
// We avoid importing the package-private logic — agents author every
// fragment via Write, so the simulation just stamps the file body
// directly into Plan.Fragments without append-vs-replace semantics.

func runStitch(args []string) error {
	fs := flag.NewFlagSet("stitch", flag.ContinueOnError)
	dir := fs.String("dir", "", "simulation directory previously populated by `emit` and per-codebase Agent dispatches")
	rounds := fs.Int("rounds", 2, "number of stitch rounds to run; rounds>=2 enable the multi-stitch idempotence assertion (run-20 prep S1)")
	gateSet := fs.String("gates", "", "gate set to run after stitch (run-20 prep S6); 'codebase-content' invokes CodebaseContentGates+DefaultGates against the staged plan + facts + materialized fragments. Empty = skip gate-running.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return errors.New("stitch: -dir is required")
	}
	if *rounds < 1 {
		return fmt.Errorf("stitch: -rounds must be >= 1, got %d", *rounds)
	}

	envDir := filepath.Join(*dir, "environments")
	plan, err := recipe.ReadPlan(envDir)
	if err != nil {
		return fmt.Errorf("read plan from %s: %w", envDir, err)
	}
	if plan.Fragments == nil {
		plan.Fragments = map[string]string{}
	}

	// Load fragments from disk into Plan.Fragments. Per-codebase
	// dispatch wrote fragments under fragments-new/<host>/; env-content
	// wrote under fragments-new/env/. Filename → fragmentId via '__'
	// → '/' (the replay-adapter convention).
	fragsRoot := filepath.Join(*dir, "fragments-new")
	codebaseHosts := map[string]bool{}
	for _, cb := range plan.Codebases {
		codebaseHosts[cb.Hostname] = true
	}
	if err := loadFragmentsTree(fragsRoot, plan, codebaseHosts); err != nil {
		return fmt.Errorf("load fragments: %w", err)
	}
	envCommentCount := 0
	for _, ec := range plan.EnvComments {
		if ec.Project != "" {
			envCommentCount++
		}
		envCommentCount += len(ec.Service)
	}
	fmt.Printf("loaded %d codebase fragments + %d env comments\n", len(plan.Fragments), envCommentCount)

	absDir, err := filepath.Abs(*dir)
	if err != nil {
		return fmt.Errorf("abs(%s): %w", *dir, err)
	}

	// Multi-stitch idempotence (run-20 prep S1) — production runs
	// `stitchCodebases` at every phase boundary (scaffold, feature,
	// finalize). The run-19 inline-yaml block-doubling regression only
	// surfaces on round ≥2, so a single linear pass misses it. This
	// driver runs the assemble+write loop `rounds` times and byte-
	// diffs round N vs round 1; equal = idempotent, unequal = the
	// engine accumulates state across rounds (typically duplicated
	// `# #` comment blocks in codebase READMEs).
	var firstSnapshot map[string]string
	for i := 1; i <= *rounds; i++ {
		fmt.Printf("\n=== stitch round %d/%d ===\n", i, *rounds)
		missing, err := stitchOnce(plan, absDir)
		if err != nil {
			return fmt.Errorf("stitch round %d: %w", i, err)
		}
		if i == 1 {
			if len(missing) > 0 {
				fmt.Printf("\nmissing fragments: %s\n", strings.Join(uniqueSorted(missing), ", "))
			} else {
				fmt.Println("\nall fragments resolved")
			}
		}
		if *rounds < 2 {
			continue
		}
		snap, err := snapshotStitchOutputs(plan, absDir)
		if err != nil {
			return fmt.Errorf("stitch round %d snapshot: %w", i, err)
		}
		if i == 1 {
			firstSnapshot = snap
			continue
		}
		if diff := firstSnapshotDiff(firstSnapshot, snap); diff != "" {
			return fmt.Errorf("multi-stitch idempotence violated (round %d vs round 1):\n%s", i, diff)
		}
		fmt.Printf("round %d byte-equal to round 1 (%d files snapshotted)\n", i, len(snap))
	}

	if *gateSet != "" {
		if err := runGatesAfterStitch(*gateSet, plan, absDir, envDir); err != nil {
			return err
		}
	}

	return nil
}

// stitchOnce runs one full assemble+write pass: root README, every
// per-tier README + import.yaml, and every per-codebase
// {README.md, CLAUDE.md, zerops.yaml}. Returns the union of missing
// fragments collected across the assemblers — caller decides what
// (if anything) to print on a partial corpus.
func stitchOnce(plan *recipe.Plan, absDir string) ([]string, error) {
	var missing []string

	// Root README.
	rootBody, m, err := recipe.AssembleRootREADME(plan)
	if err != nil {
		return nil, fmt.Errorf("AssembleRootREADME: %w", err)
	}
	missing = append(missing, m...)
	if err := os.WriteFile(filepath.Join(absDir, "README.md"), []byte(rootBody), 0o600); err != nil {
		return nil, fmt.Errorf("write root README: %w", err)
	}
	fmt.Printf("wrote README.md (%d bytes)\n", len(rootBody))

	// Per-tier env READMEs + import.yamls.
	for i := range recipe.Tiers() {
		envBody, m, err := recipe.AssembleEnvREADME(plan, i)
		if err != nil {
			return nil, fmt.Errorf("AssembleEnvREADME tier=%d: %w", i, err)
		}
		missing = append(missing, m...)
		tier, ok := recipe.TierAt(i)
		if !ok {
			return nil, fmt.Errorf("TierAt(%d): no such tier", i)
		}
		tierDir := filepath.Join(absDir, "environments", tier.Folder)
		if err := os.MkdirAll(tierDir, 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(tierDir, "README.md"), []byte(envBody), 0o600); err != nil {
			return nil, fmt.Errorf("write env %d README: %w", i, err)
		}
		yamlBody, err := recipe.EmitDeliverableYAML(plan, i)
		if err != nil {
			return nil, fmt.Errorf("EmitDeliverableYAML tier=%d: %w", i, err)
		}
		if err := os.WriteFile(filepath.Join(tierDir, "import.yaml"), []byte(yamlBody), 0o600); err != nil {
			return nil, fmt.Errorf("write env %d import.yaml: %w", i, err)
		}
		fmt.Printf("wrote environments/%s/{README.md,import.yaml}\n", tier.Folder)
	}

	// Per-codebase apps-repo: README + CLAUDE.md + commented zerops.yaml.
	for _, cb := range plan.Codebases {
		readmeBody, m, err := recipe.AssembleCodebaseREADME(plan, cb.Hostname)
		if err != nil {
			return nil, fmt.Errorf("AssembleCodebaseREADME %s: %w", cb.Hostname, err)
		}
		missing = append(missing, m...)
		if err := os.WriteFile(filepath.Join(cb.SourceRoot, "README.md"), []byte(readmeBody), 0o600); err != nil {
			return nil, fmt.Errorf("write %s README: %w", cb.Hostname, err)
		}
		claudeBody, m, err := recipe.AssembleCodebaseClaudeMD(plan, cb.Hostname)
		if err != nil {
			return nil, fmt.Errorf("AssembleCodebaseClaudeMD %s: %w", cb.Hostname, err)
		}
		missing = append(missing, m...)
		if err := os.WriteFile(filepath.Join(cb.SourceRoot, "CLAUDE.md"), []byte(claudeBody), 0o600); err != nil {
			return nil, fmt.Errorf("write %s CLAUDE.md: %w", cb.Hostname, err)
		}
		// Strip-then-inject yaml comments back into <SourceRoot>/zerops.yaml.
		if err := recipe.WriteCodebaseYAMLWithComments(plan, cb.Hostname); err != nil {
			return nil, fmt.Errorf("WriteCodebaseYAMLWithComments %s: %w", cb.Hostname, err)
		}
		fmt.Printf("wrote %sdev/{README.md,CLAUDE.md,zerops.yaml}\n", cb.Hostname)
	}

	return missing, nil
}

// snapshotStitchOutputs reads every file written by stitchOnce into a
// path → contents map. Used by the multi-stitch idempotence assertion
// to byte-diff round N vs round 1.
func snapshotStitchOutputs(plan *recipe.Plan, absDir string) (map[string]string, error) {
	out := map[string]string{}
	add := func(path string) error {
		body, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		out[path] = string(body)
		return nil
	}
	if err := add(filepath.Join(absDir, "README.md")); err != nil {
		return nil, err
	}
	for _, t := range recipe.Tiers() {
		tierDir := filepath.Join(absDir, "environments", t.Folder)
		if err := add(filepath.Join(tierDir, "README.md")); err != nil {
			return nil, err
		}
		if err := add(filepath.Join(tierDir, "import.yaml")); err != nil {
			return nil, err
		}
	}
	for _, cb := range plan.Codebases {
		if cb.SourceRoot == "" {
			continue
		}
		if err := add(filepath.Join(cb.SourceRoot, "README.md")); err != nil {
			return nil, err
		}
		if err := add(filepath.Join(cb.SourceRoot, "CLAUDE.md")); err != nil {
			return nil, err
		}
		if err := add(filepath.Join(cb.SourceRoot, "zerops.yaml")); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// firstSnapshotDiff returns a human-readable description of the first
// path that differs between two snapshots, or empty when equal.
// Includes byte counts and a short context window so the failure
// message is actionable. Sorts paths so the report is deterministic.
func firstSnapshotDiff(want, got map[string]string) string {
	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		w := want[k]
		g, ok := got[k]
		if !ok {
			return fmt.Sprintf("file %q present round 1, missing later round", k)
		}
		if w == g {
			continue
		}
		// First-divergence offset for an actionable message.
		d := firstDiffOffset(w, g)
		return fmt.Sprintf("file %q diverges (round1=%d bytes, later=%d bytes, first divergence at offset %d)",
			k, len(w), len(g), d)
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			return fmt.Sprintf("file %q appeared in later round but not round 1", k)
		}
	}
	return ""
}

// firstDiffOffset returns the byte offset of the first character at
// which two strings differ, or len(min(a,b)) if one is a prefix.
func firstDiffOffset(a, b string) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// loadFragmentsTree walks fragments-new/ subdirectories and loads
// every `*.md` file into the plan. Codebase fragments land in
// `plan.Fragments`; `env/<N>/import-comments/<target>` fragments route
// into the typed `plan.EnvComments` map via `recipe.ApplyEnvComment`
// (matching the production record-fragment path).
//
// Filename `__`-separator converts back to fragment id via `/` per the
// replay-adapter convention. Directories are accepted regardless of
// name; codebase hosts and the literal "env" subdir are the expected
// layout but unrecognized subdirs aren't an error (just informational).
func loadFragmentsTree(root string, planForRouting *recipe.Plan, codebaseHosts map[string]bool) error {
	frags := planForRouting.Fragments
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("fragments-new/ missing — run `emit` first then dispatch sub-agents")
		}
		return err
	}
	loaded := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := e.Name()
		if !codebaseHosts[sub] && sub != "env" {
			fmt.Printf("(skip) unknown subdir fragments-new/%s\n", sub)
			continue
		}
		subDir := filepath.Join(root, sub)
		files, err := os.ReadDir(subDir)
		if err != nil {
			return fmt.Errorf("read %s: %w", subDir, err)
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
				continue
			}
			fragmentID := strings.ReplaceAll(strings.TrimSuffix(f.Name(), ".md"), "__", "/")
			body, err := os.ReadFile(filepath.Join(subDir, f.Name()))
			if err != nil {
				return fmt.Errorf("read %s: %w", f.Name(), err)
			}
			// env/<N>/import-comments/<target> fragments don't go into
			// plan.Fragments — they route into the typed plan.EnvComments
			// map via the canonical engine helper. The yaml emitter
			// reads from EnvComments, not Fragments, so skipping this
			// step leaves every tier yaml uncommented.
			if strings.HasPrefix(fragmentID, "env/") && strings.Contains(fragmentID, "/import-comments/") {
				if err := recipe.ApplyEnvComment(planForRouting, fragmentID, string(body)); err != nil {
					return fmt.Errorf("ApplyEnvComment %s: %w", fragmentID, err)
				}
				loaded++
				continue
			}
			frags[fragmentID] = string(body)
			loaded++
		}
	}
	if loaded == 0 {
		return errors.New("fragments-new/ is empty — dispatch the prompts emitted by `emit` before stitching")
	}
	return nil
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
