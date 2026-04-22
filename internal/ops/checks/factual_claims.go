package checks

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// factualClaimWindow is how many non-comment lines after a comment line
// to scan for the corresponding YAML field. Service blocks in
// import.yaml are typically < 15 lines, so this covers a full service
// definition without reaching the next block.
const factualClaimWindow = 15

// factualClaimPattern holds a regex for detecting a declarative numeric
// claim in a comment and the YAML field key it must match.
type factualClaimPattern struct {
	name       string         // short label used in the failure detail
	commentRe  *regexp.Regexp // capture group 1 = the claimed number
	yamlKey    string         // YAML key to look for within the window
	unitInText string         // display unit ("GB", "") for failure messages
}

var factualClaimPatterns = []factualClaimPattern{
	{
		name:       "storage quota",
		commentRe:  regexp.MustCompile(`(?i)(\d+)\s*GB\b`),
		yamlKey:    "objectStorageSize",
		unitInText: "GB",
	},
	{
		name:      "minContainers count",
		commentRe: regexp.MustCompile(`(?i)minContainers[^a-z0-9]*(\d+)`),
		yamlKey:   "minContainers",
	},
}

// subjunctiveMarkers are phrases that signal an aspirational/conditional
// claim ("bump to 10 GB when usage grows") rather than a declarative one
// about the current value ("10 GB quota"). A comment line containing any
// marker is skipped by every pattern check — aspirational prose is
// allowed to name values that don't exist yet in the YAML.
//
// Known limitations:
//
//   - Whole-comment granularity: a single comment that mixes declarative
//     and aspirational clauses ("current size is 10 GB; bump to 20 GB
//     via the GUI if needed") skips the check entirely. Tolerable
//     trade-off — real-world comments rarely mix both modes.
//   - Substring matching without word boundaries: `"bump to"` matches
//     any occurrence; contrived in practice.
//   - Trailing spaces are load-bearing: entries like `"when the "` with
//     a trailing space match mid-sentence phrases but not sentence-
//     terminal ones, which is the intended word-boundary approximation.
//     Do NOT normalize by stripping the trailing space.
var subjunctiveMarkers = []string{
	"consider ", "if you", "if needed", "up to ",
	"bump to", "bump via", "bump in", "bump the",
	"upgrade to", "upgrade via",
	"scale to", "grow to", "raise to",
	"when usage", "when traffic", "when the ",
	"as needed",
}

func isSubjunctive(commentBody string) bool {
	low := strings.ToLower(commentBody)
	for _, m := range subjunctiveMarkers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

// factualClaimMismatch records one comment-vs-yaml contradiction with
// enough context for a per-mismatch StepCheck.
type factualClaimMismatch struct {
	commentLine int    // 1-based source line of the offending comment
	yamlLine    int    // 1-based source line of the adjacent YAML field
	patName     string // e.g. "storage quota"
	yamlKey     string // e.g. "objectStorageSize"
	claimed     int
	actual      int
	unit        string // " GB" or ""
}

func (m factualClaimMismatch) detail() string {
	// Cx-ENV-COMMENT-PRINCIPLE (v38): the claimed string is quoted and the
	// actual YAML value is rendered in `<key>: <value>` form so a fix-cycle
	// reader can diff the two side-by-side without parsing prose.
	claimedLabel := strings.TrimSpace(fmt.Sprintf("%d%s", m.claimed, m.unit))
	return fmt.Sprintf(
		`line %d: comment claims "%s" (%s) but adjacent YAML has %s: %d`,
		m.commentLine, claimedLabel, m.patName, m.yamlKey, m.actual,
	)
}

// yamlIntFieldRe matches a line of the form `  someKey: 42` and
// captures both the key name and the integer value. Non-integer values
// (strings, mappings) are ignored — factual claims we check are all
// integers.
var yamlIntFieldRe = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9_]*)\s*:\s*(\d+)\s*$`)

// findAdjacentYAMLValueWithLine is the line-aware variant that returns
// (value, line, true) on a hit so each per-mismatch StepCheck can name
// both the comment line AND the YAML line in its ReadSurface.
func findAdjacentYAMLValueWithLine(lines []string, startLine int, key string, window int) (val, line int, found bool) {
	end := min(startLine+1+window, len(lines))
	listItems := 0
	for i := startLine + 1; i < end; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			listItems++
			if listItems >= 2 {
				return 0, 0, false
			}
			continue
		}
		m := yamlIntFieldRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		if m[1] == key {
			v, err := strconv.Atoi(m[2])
			if err != nil {
				return 0, 0, false
			}
			return v, i + 1, true
		}
	}
	return 0, 0, false
}

// CheckFactualClaims walks the import.yaml line by line, finds
// declarative numeric claims in comment lines, and compares them to
// the adjacent YAML field value within a small forward window. A
// mismatch is a hard fail — the comment is lying about what the file
// actually provisions.
//
// v8.96 §5.3: emits ONE StepCheck per mismatch (instead of an
// aggregated failure detail) so each contradicting line gets its own
// ReadSurface pointing at the exact line + key, and its own HowToFix
// that names the concrete remedy.
func CheckFactualClaims(_ context.Context, content, prefix string) []workflow.StepCheck {
	lines := strings.Split(content, "\n")
	var mismatches []factualClaimMismatch

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		body := strings.TrimLeft(trimmed, "#")
		body = strings.TrimSpace(body)
		if body == "" {
			continue
		}
		if isSubjunctive(body) {
			continue
		}
		for _, pat := range factualClaimPatterns {
			m := pat.commentRe.FindStringSubmatch(body)
			if m == nil {
				continue
			}
			claimed, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			actual, yamlLine, found := findAdjacentYAMLValueWithLine(lines, i, pat.yamlKey, factualClaimWindow)
			if !found {
				continue
			}
			if claimed == actual {
				continue
			}
			unit := ""
			if pat.unitInText != "" {
				unit = " " + pat.unitInText
			}
			mismatches = append(mismatches, factualClaimMismatch{
				commentLine: i + 1,
				yamlLine:    yamlLine,
				patName:     pat.name,
				yamlKey:     pat.yamlKey,
				claimed:     claimed,
				actual:      actual,
				unit:        unit,
			})
		}
	}

	if len(mismatches) == 0 {
		return []workflow.StepCheck{{
			Name:   prefix + "_factual_claims",
			Status: StatusPass,
		}}
	}

	envFolder := strings.TrimSuffix(prefix, "_import")
	out := make([]workflow.StepCheck, 0, len(mismatches))
	for i, m := range mismatches {
		name := prefix + "_factual_claims"
		if i > 0 {
			name = fmt.Sprintf("%s_factual_claims_%d", prefix, i+1)
		}
		out = append(out, workflow.StepCheck{
			Name:         name,
			Status:       StatusFail,
			PreAttestCmd: fmt.Sprintf("zcp check factual-claims --env=%s --path=./", envFolder),
			Detail: fmt.Sprintf(
				"comment contradicts YAML value in %s/import.yaml line %d (vs `%s` on line %d) — %s. Edit line %d so the comment number matches `%s: %d`, drop the number from the comment, or rephrase aspirationally ('bump to N %s via the GUI when usage grows').",
				envFolder, m.commentLine, m.yamlKey, m.yamlLine, m.detail(), m.commentLine, m.yamlKey, m.actual, strings.TrimSpace(m.unit),
			),
		})
	}
	return out
}
