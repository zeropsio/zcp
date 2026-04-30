// validate subcommand — runs slot-shape refusals (checkSlotShapeWithPlan
// via recipe.CheckSlotShapeForReplay) over every authored fragment in a
// simulation directory. Mirrors what the production engine refuses at
// `record-fragment` time so simulation output's authoring discipline
// matches dogfood reality.
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

func runValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	dir := fs.String("dir", "", "simulation directory (reads <dir>/environments/plan.json + <dir>/fragments-new/<host>/*.md)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return errors.New("validate: -dir is required")
	}

	envDir := filepath.Join(*dir, "environments")
	plan, err := recipe.ReadPlan(envDir)
	if err != nil {
		return fmt.Errorf("read plan: %w", err)
	}

	fragsRoot := filepath.Join(*dir, "fragments-new")
	totalFiles := 0
	totalFires := 0
	for _, cb := range plan.Codebases {
		cbDir := filepath.Join(fragsRoot, cb.Hostname)
		entries, err := os.ReadDir(cbDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %s (%v)\n", cbDir, err)
			continue
		}
		fmt.Printf("\n=== %s ===\n", cb.Hostname)
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			fragmentID := strings.ReplaceAll(strings.TrimSuffix(e.Name(), ".md"), "__", "/")
			body, err := os.ReadFile(filepath.Join(cbDir, e.Name()))
			if err != nil {
				fmt.Fprintf(os.Stderr, "read %s: %v\n", e.Name(), err)
				continue
			}
			totalFiles++
			refusals := recipe.CheckSlotShapeForReplay(fragmentID, string(body), plan)
			if len(refusals) == 0 {
				fmt.Printf("  ✓ %s (%s)\n", e.Name(), fragmentID)
				continue
			}
			totalFires++
			fmt.Printf("  ✗ %s (%s) — %d refusal(s):\n", e.Name(), fragmentID, len(refusals))
			for _, r := range refusals {
				fmt.Printf("      • %s\n", truncate(r, 200))
			}
		}
	}

	// Env fragments — fragments-new/env/*.md not bound to a codebase.
	envFragsDir := filepath.Join(fragsRoot, "env")
	if entries, err := os.ReadDir(envFragsDir); err == nil {
		fmt.Printf("\n=== env ===\n")
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			fragmentID := strings.ReplaceAll(strings.TrimSuffix(e.Name(), ".md"), "__", "/")
			body, err := os.ReadFile(filepath.Join(envFragsDir, e.Name()))
			if err != nil {
				fmt.Fprintf(os.Stderr, "read %s: %v\n", e.Name(), err)
				continue
			}
			totalFiles++
			refusals := recipe.CheckSlotShapeForReplay(fragmentID, string(body), plan)
			if len(refusals) == 0 {
				fmt.Printf("  ✓ %s (%s)\n", e.Name(), fragmentID)
				continue
			}
			totalFires++
			fmt.Printf("  ✗ %s (%s) — %d refusal(s):\n", e.Name(), fragmentID, len(refusals))
			for _, r := range refusals {
				fmt.Printf("      • %s\n", truncate(r, 200))
			}
		}
	}

	fmt.Printf("\n=== summary ===\n%d fragments scanned, %d fired refusals\n", totalFiles, totalFires)
	if totalFires > 0 {
		return fmt.Errorf("%d fragments failed slot-shape refusal", totalFires)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
