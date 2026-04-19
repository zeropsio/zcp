package tools

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// factualClaimWindow is how many non-comment lines after a comment line
// to scan for the corresponding YAML field. Service blocks in import.yaml
// are typically < 15 lines, so this covers a full service definition
// without reaching the next block.
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
// marker is skipped by every pattern check — aspirational prose is allowed
// to name values that don't exist yet in the YAML.
//
// Known limitations:
//
//   - Whole-comment granularity: a single comment that mixes declarative
//     and aspirational clauses ("current size is 10 GB; bump to 20 GB via
//     the GUI if needed") skips the check entirely. The declarative 10 GB
//     goes unchecked. Tolerable trade-off — real-world comments rarely
//     mix both modes, and the alternative (clause-level parsing) has a
//     much higher false-positive rate.
//
//   - Substring matching without word boundaries: `"bump to"` matches
//     any occurrence, so a sentence fragment like "dump to disk bump
//     total 10 GB" would skip. Contrived in practice; real comments
//     don't chain those tokens.
//
//   - Trailing spaces are load-bearing: entries like `"when the "` with
//     a trailing space match mid-sentence phrases ("when the cache is
//     cold") but not sentence-terminal ones ("when the."), which is the
//     intended word-boundary approximation. Do NOT normalize these
//     entries by stripping the trailing space — that would make them
//     match substrings like "whether" or "whenever".
var subjunctiveMarkers = []string{
	"consider ",
	"if you",
	"if needed",
	"up to ",
	"bump to",
	"bump via",
	"bump in",
	"bump the",
	"upgrade to",
	"upgrade via",
	"scale to",
	"grow to",
	"raise to",
	"when usage",
	"when traffic",
	"when the ",
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
	return fmt.Sprintf(
		"line %d: comment claims %d%s %s but adjacent %s is %d",
		m.commentLine, m.claimed, m.unit, m.patName, m.yamlKey, m.actual,
	)
}

// checkFactualClaims walks the import.yaml line by line, finds declarative
// numeric claims in comment lines, and compares them to the adjacent YAML
// field value within a small forward window. A mismatch is a hard fail —
// the comment is lying about what the file actually provisions.
//
// v8.96 §5.3: emits ONE StepCheck per mismatch (instead of an aggregated
// failure detail) so each contradicting line gets its own ReadSurface
// pointing at the exact line + key, and its own HowToFix that names the
// concrete remedy.
//
// This catches the v5 "5 GB quota" / v8 and v10 "10 GB quota" regression
// permanently without depending on a specific object-storage field being
// parsed into the importYAMLDoc struct: everything is line-based.
func checkFactualClaims(content, prefix string) []workflow.StepCheck {
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
		// Try each pattern. A single comment line may assert more than one
		// value (e.g. "minContainers 2 with 1 GB storage") — check them all.
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
				continue // no adjacent field to compare against
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
			Status: statusPass,
		}}
	}

	envFolder := strings.TrimSuffix(prefix, "_import")
	out := make([]workflow.StepCheck, 0, len(mismatches))
	for i, m := range mismatches {
		// Distinct check name per mismatch so the result table shows
		// each individually instead of collapsing into one "factual_claims"
		// entry. The base name stays so the existing aggregation tests
		// that look for prefix+"_factual_claims" still find at least one.
		name := prefix + "_factual_claims"
		if i > 0 {
			name = fmt.Sprintf("%s_factual_claims_%d", prefix, i+1)
		}
		out = append(out, workflow.StepCheck{
			Name:        name,
			Status:      statusFail,
			Detail:      "comment contradicts YAML value — " + m.detail() + ". Either correct the number to match the configured value, drop the number from the comment, or rephrase as aspirational (e.g. 'bump to N GB via the GUI when usage grows').",
			ReadSurface: fmt.Sprintf("%s/import.yaml line %d (comment) vs line %d (`%s` field)", envFolder, m.commentLine, m.yamlLine, m.yamlKey),
			Required:    fmt.Sprintf("comment number matches the adjacent `%s` value", m.yamlKey),
			Actual:      fmt.Sprintf("comment claims %d%s; YAML has %s: %d", m.claimed, m.unit, m.yamlKey, m.actual),
			HowToFix: fmt.Sprintf(
				"Edit %s/import.yaml line %d so the comment number matches the `%s: %d` declaration on line %d. If the right answer is the comment, change the YAML; if the right answer is the YAML, drop the number from the comment or rephrase aspirationally ('bump to N %s via the GUI when usage grows').",
				envFolder, m.commentLine, m.yamlKey, m.actual, m.yamlLine, strings.TrimSpace(m.unit),
			),
		})
	}
	return out
}

// findAdjacentYAMLValueWithLine is the line-aware variant of
// findAdjacentYAMLValue. Returns (value, line, true) on a hit so the
// per-mismatch StepCheck can name both the comment line AND the YAML
// line in its ReadSurface.
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

// yamlIntFieldRe matches a line of the form `  someKey: 42` and captures
// both the key name and the integer value. Non-integer values (strings,
// mappings) are ignored — factual claims we check are all integers.
var yamlIntFieldRe = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9_]*)\s*:\s*(\d+)\s*$`)
