package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// unicodeBoxDrawingLow and unicodeBoxDrawingHigh bracket the Box Drawing
// Unicode block (U+2500..U+257F). Presence of any codepoint in this range
// inside a published zerops.yaml is the v33 invention class: the LLM
// renders an "ascii-art separator" using real box-drawing glyphs that look
// like ASCII on the author's terminal but leak as mojibake when the file
// is re-opened on a different locale. Principle P8: positive allow-list =
// ASCII only for visual separators.
const (
	unicodeBoxDrawingLow  = 0x2500
	unicodeBoxDrawingHigh = 0x257F
)

// checkVisualStyleASCIIOnly scans {ymlDir}/zerops.yaml (with zerops.yml
// fallback) for any codepoint in the Box Drawing Unicode block. A single
// offending rune fails the check; the detail lists each distinct codepoint
// with its line number so the author can locate and replace with plain
// ASCII (|, -, +, =).
//
// No-op-pass when the yaml file is absent — a missing zerops.yaml is the
// upstream concern of checkRecipeGenerateCodebase's `_zerops_yml_exists`
// floor, not this check's surface.
func checkVisualStyleASCIIOnly(ymlDir, hostname string) []workflow.StepCheck {
	content, err := os.ReadFile(filepath.Join(ymlDir, "zerops.yaml"))
	if err != nil {
		content, err = os.ReadFile(filepath.Join(ymlDir, "zerops.yml"))
		if err != nil {
			return []workflow.StepCheck{{
				Name:   hostname + "_visual_style_ascii_only",
				Status: statusPass,
			}}
		}
	}

	type finding struct {
		codepoint rune
		line      int
	}
	var findings []finding
	seen := map[rune]bool{}
	lineNo := 1
	for _, r := range string(content) {
		if r == '\n' {
			lineNo++
			continue
		}
		if r >= unicodeBoxDrawingLow && r <= unicodeBoxDrawingHigh {
			if !seen[r] {
				seen[r] = true
				findings = append(findings, finding{codepoint: r, line: lineNo})
			}
		}
	}

	if len(findings) == 0 {
		return []workflow.StepCheck{{
			Name:   hostname + "_visual_style_ascii_only",
			Status: statusPass,
		}}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].line != findings[j].line {
			return findings[i].line < findings[j].line
		}
		return findings[i].codepoint < findings[j].codepoint
	})
	labels := make([]string, 0, len(findings))
	for _, f := range findings {
		labels = append(labels, fmt.Sprintf("U+%04X at line %d", f.codepoint, f.line))
	}
	return []workflow.StepCheck{{
		Name:   hostname + "_visual_style_ascii_only",
		Status: statusFail,
		Detail: fmt.Sprintf(
			"%s/zerops.yaml contains Unicode Box Drawing codepoints (U+2500..U+257F): %s. Replace these glyphs with plain ASCII (|, -, +, =) — the v33 class shipped yaml files whose ascii-art separators rendered as box-drawing in some terminals and as mojibake in others. Principle P8: visual separators are ASCII-only.",
			hostname, strings.Join(labels, ", "),
		),
	}}
}
