//go:build audit

// Package workflow audit harness — not compiled in normal builds.
//
// TestAuditComposition dumps per-step, per-part, per-subsection byte
// composition across every fixture shape. Use this to measure the effect
// of every phase in the recipe delivery refactor
// (docs/zrecipator-archive/implementation-recipe-size-reduction.md) and to set per-shape caps
// in Phase 11.
//
//	go test -tags audit ./internal/workflow -run TestAuditComposition -v
package workflow

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/knowledge"
)

func TestAuditComposition(t *testing.T) {
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	shapes := []struct {
		name  string
		shape RecipeShape
	}{
		{"hello-world", ShapeHelloWorld},
		{"backend-minimal", ShapeBackendMinimal},
		{"fullstack-showcase", ShapeFullStackShowcase},
		{"dual-runtime-showcase", ShapeDualRuntimeShowcase},
	}
	steps := []string{
		RecipeStepResearch, RecipeStepProvision, RecipeStepGenerate,
		RecipeStepDeploy, RecipeStepFinalize, RecipeStepClose,
	}

	for _, sh := range shapes {
		fmt.Printf("\n\n########## SHAPE: %s ##########\n", sh.name)
		plan := fixtureForShape(sh.shape)
		for _, step := range steps {
			rs := advanceShowcaseStateTo(step, plan)
			resp := rs.BuildResponse("x", "m", 0, EnvLocal, store)
			if resp.Current == nil {
				fmt.Printf("\n=== %s === (no Current)\n", strings.ToUpper(step))
				continue
			}
			guide := resp.Current.DetailedGuide
			fmt.Printf("\n=== %s === %d B (%.1f KB)\n", strings.ToUpper(step), len(guide), float64(len(guide))/1024)
			parts := strings.Split(guide, "\n\n---\n\n")
			for i, p := range parts {
				first := strings.SplitN(p, "\n", 2)[0]
				if len(first) > 80 {
					first = first[:80]
				}
				fmt.Printf("  [part %d] %6d B  %s\n", i, len(p), first)
			}
			if len(parts) > 0 {
				dumpH3Breakdown(parts[0])
			}
		}
	}
}

// dumpH3Breakdown walks an H2-scoped body and prints each H3 (and H4) sub-
// section's byte budget. Subsections under 200 B are suppressed to keep the
// dump readable.
func dumpH3Breakdown(body string) {
	h3re := regexp.MustCompile(`^### `)
	h4re := regexp.MustCompile(`^#### `)
	type bucket struct {
		name  string
		bytes int
	}
	var buckets []bucket
	cur := bucket{name: "[preamble]"}
	for _, l := range strings.Split(body, "\n") {
		switch {
		case h3re.MatchString(l):
			buckets = append(buckets, cur)
			cur = bucket{name: "    ### " + strings.TrimPrefix(l, "### "), bytes: len(l) + 1}
		case h4re.MatchString(l):
			buckets = append(buckets, cur)
			cur = bucket{name: "      #### " + strings.TrimPrefix(l, "#### "), bytes: len(l) + 1}
		default:
			cur.bytes += len(l) + 1
		}
	}
	buckets = append(buckets, cur)
	for _, b := range buckets {
		if b.bytes < 200 {
			continue
		}
		name := b.name
		if len(name) > 80 {
			name = name[:80]
		}
		fmt.Printf("      %-80s  %6d B\n", name, b.bytes)
	}
}
