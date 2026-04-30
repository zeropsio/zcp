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
	"strings"

	"github.com/zeropsio/zcp/internal/recipe"
)

// recordFragmentMode is recipe.handlers_fragments.go's mode resolver.
// We avoid importing the package-private logic — agents author every
// fragment via Write, so the simulation just stamps the file body
// directly into Plan.Fragments without append-vs-replace semantics.

func runStitch(args []string) error {
	fs := flag.NewFlagSet("stitch", flag.ExitOnError)
	dir := fs.String("dir", "", "simulation directory previously populated by `emit` and per-codebase Agent dispatches")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return errors.New("stitch: -dir is required")
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

	// Root README.
	rootBody, missing, err := recipe.AssembleRootREADME(plan)
	if err != nil {
		return fmt.Errorf("AssembleRootREADME: %w", err)
	}
	if err := os.WriteFile(filepath.Join(absDir, "README.md"), []byte(rootBody), 0o600); err != nil {
		return fmt.Errorf("write root README: %w", err)
	}
	fmt.Printf("wrote README.md (%d bytes)\n", len(rootBody))

	// Per-tier env READMEs + import.yamls.
	for i := range recipe.Tiers() {
		envBody, m, err := recipe.AssembleEnvREADME(plan, i)
		if err != nil {
			return fmt.Errorf("AssembleEnvREADME tier=%d: %w", i, err)
		}
		missing = append(missing, m...)
		tier, ok := recipe.TierAt(i)
		if !ok {
			return fmt.Errorf("TierAt(%d): no such tier", i)
		}
		tierDir := filepath.Join(absDir, "environments", tier.Folder)
		if err := os.MkdirAll(tierDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(tierDir, "README.md"), []byte(envBody), 0o600); err != nil {
			return fmt.Errorf("write env %d README: %w", i, err)
		}
		yamlBody, err := recipe.EmitDeliverableYAML(plan, i)
		if err != nil {
			return fmt.Errorf("EmitDeliverableYAML tier=%d: %w", i, err)
		}
		if err := os.WriteFile(filepath.Join(tierDir, "import.yaml"), []byte(yamlBody), 0o600); err != nil {
			return fmt.Errorf("write env %d import.yaml: %w", i, err)
		}
		fmt.Printf("wrote environments/%s/{README.md,import.yaml}\n", tier.Folder)
	}

	// Per-codebase apps-repo: README + CLAUDE.md + commented zerops.yaml.
	for _, cb := range plan.Codebases {
		readmeBody, m, err := recipe.AssembleCodebaseREADME(plan, cb.Hostname)
		if err != nil {
			return fmt.Errorf("AssembleCodebaseREADME %s: %w", cb.Hostname, err)
		}
		missing = append(missing, m...)
		if err := os.WriteFile(filepath.Join(cb.SourceRoot, "README.md"), []byte(readmeBody), 0o600); err != nil {
			return fmt.Errorf("write %s README: %w", cb.Hostname, err)
		}
		claudeBody, m, err := recipe.AssembleCodebaseClaudeMD(plan, cb.Hostname)
		if err != nil {
			return fmt.Errorf("AssembleCodebaseClaudeMD %s: %w", cb.Hostname, err)
		}
		missing = append(missing, m...)
		if err := os.WriteFile(filepath.Join(cb.SourceRoot, "CLAUDE.md"), []byte(claudeBody), 0o600); err != nil {
			return fmt.Errorf("write %s CLAUDE.md: %w", cb.Hostname, err)
		}
		// Strip-then-inject yaml comments back into <SourceRoot>/zerops.yaml.
		if err := recipe.WriteCodebaseYAMLWithComments(plan, cb.Hostname); err != nil {
			return fmt.Errorf("WriteCodebaseYAMLWithComments %s: %w", cb.Hostname, err)
		}
		fmt.Printf("wrote %sdev/{README.md,CLAUDE.md,zerops.yaml}\n", cb.Hostname)
	}

	if len(missing) > 0 {
		fmt.Printf("\nmissing fragments: %s\n", strings.Join(uniqueSorted(missing), ", "))
	} else {
		fmt.Println("\nall fragments resolved")
	}
	return nil
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
