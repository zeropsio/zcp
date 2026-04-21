// Package main is the build-time lint for atom-corpus template-variable
// bindings. Enforces B-22 (docs/zcprecipator2/spec-recipe-analysis-harness.md
// §3): every `{{.Field}}` reference in every atom under
// internal/content/workflows/recipe/ must name a field the Go render
// path populates. Prevents re-introduction of F-9-class defects where
// a writer dispatch prompt carries unresolved template syntax and the
// main agent invents values.
//
// Exit code:
//   - 0: every atom template reference binds to an allowed field.
//   - 1: one or more unbound references; stdout lists them as
//     `<atom>:<field>` so a maintainer can locate the offender.
//
// Run via `make lint-local` or directly with `go run
// ./tools/lint/atom_template_vars`.
package main

import (
	"fmt"
	"os"

	"github.com/zeropsio/zcp/internal/analyze"
)

const atomRoot = "internal/content/workflows/recipe"

func main() {
	result := analyze.CheckAtomTemplateVarsBound(atomRoot, analyze.DefaultAllowedAtomFields)
	if result.Status == analyze.StatusSkip {
		fmt.Fprintln(os.Stderr, "atom-template-vars lint: SKIP —", result.Reason)
		os.Exit(0)
	}
	if result.Status == analyze.StatusPass {
		fmt.Fprintln(os.Stderr, "atom-template-vars lint: PASS (0 unbound references)")
		return
	}
	fmt.Fprintf(os.Stderr, "atom-template-vars lint: FAIL (%d unbound references)\n", result.Observed)
	fmt.Fprintln(os.Stderr, "Allowed fields:")
	for f := range analyze.DefaultAllowedAtomFields {
		fmt.Fprintln(os.Stderr, "  ."+f)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Offenders:")
	for _, line := range result.EvidenceFiles {
		fmt.Fprintln(os.Stderr, "  "+line)
	}
	os.Exit(1)
}
