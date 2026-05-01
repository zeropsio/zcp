// gate.go — sim-side complete-phase analog. After stitch writes the
// stitched corpus, the sim driver invokes the production gate set
// matching the named phase against the staged plan + facts +
// materialized fragments. Refusals surface as sim failures, mirroring
// `zerops_recipe action=complete-phase` behavior on the real engine.
//
// Spec: docs/zcprecipator3/plans/run-20-prep.md §S6.
//
// Today only `codebase-content` is wired — the spec target for B1's
// slot-floor verification. Other phases (`scaffold`, `feature`,
// `env-content`, `finalize`) are easy follow-ons; the dispatch table
// below is the extension point.
package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/recipe"
)

// gateSetByName maps the `-gates` flag value to the production gate
// set the sim driver invokes. Mirrors `gatesForPhase` (phase_entry.go)
// for the phases the sim path can replay.
//
// Each entry is the union of the phase's gate set + DefaultGates so
// the citations + fact-required-fields rails fire too — production
// always runs DefaultGates as the base; the sim path matches.
var gateSetByName = map[string]func() []recipe.Gate{
	"codebase-content": func() []recipe.Gate {
		return append(recipe.DefaultGates(), recipe.CodebaseContentGates()...)
	},
}

// runGatesAfterStitch dispatches to the named gate set, runs every
// gate against a GateContext built from the staged plan, facts log,
// and on-disk simulation tree, then surfaces blocking violations as
// a returned error. Notice-severity findings print to stderr but do
// not fail the run — matching production's PartitionBySeverity split.
func runGatesAfterStitch(name string, plan *recipe.Plan, absDir, envDir string) error {
	build, ok := gateSetByName[name]
	if !ok {
		known := make([]string, 0, len(gateSetByName))
		for k := range gateSetByName {
			known = append(known, k)
		}
		return fmt.Errorf("stitch: unknown -gates value %q (known: %s)", name, strings.Join(known, ", "))
	}
	gates := build()
	factsLog := recipe.OpenFactsLog(filepath.Join(envDir, "facts.jsonl"))
	ctx := recipe.GateContext{
		Plan:       plan,
		OutputRoot: absDir,
		FactsLog:   factsLog,
	}
	violations := recipe.RunGates(gates, ctx)
	blocking, notices := recipe.PartitionBySeverity(violations)
	for _, n := range notices {
		fmt.Printf("[gate notice] %s: %s (%s)\n", n.Code, n.Message, n.Path)
	}
	if len(blocking) == 0 {
		fmt.Printf("\n=== complete-phase=%s gates ===\n%d gate(s) ran, %d notice(s), 0 blocking\n",
			name, len(gates), len(notices))
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "complete-phase=%s gate refusal: %d blocking violation(s):\n", name, len(blocking))
	for _, v := range blocking {
		fmt.Fprintf(&b, "  - %s: %s", v.Code, v.Message)
		if v.Path != "" {
			fmt.Fprintf(&b, " (%s)", v.Path)
		}
		b.WriteByte('\n')
	}
	return fmt.Errorf("%s", b.String())
}
