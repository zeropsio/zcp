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

// checkFactualClaims walks the import.yaml line by line, finds declarative
// numeric claims in comment lines, and compares them to the adjacent YAML
// field value within a small forward window. A mismatch is a hard fail —
// the comment is lying about what the file actually provisions.
//
// This catches the v5 "5 GB quota" / v8 and v10 "10 GB quota" regression
// permanently without depending on a specific object-storage field being
// parsed into the importYAMLDoc struct: everything is line-based.
func checkFactualClaims(content, prefix string) []workflow.StepCheck {
	lines := strings.Split(content, "\n")
	var failures []string

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
			actual, found := findAdjacentYAMLValue(lines, i, pat.yamlKey, factualClaimWindow)
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
			failures = append(failures, fmt.Sprintf(
				"line %d: comment claims %d%s %s but adjacent %s is %d",
				i+1, claimed, unit, pat.name, pat.yamlKey, actual,
			))
		}
	}

	if len(failures) == 0 {
		return []workflow.StepCheck{{
			Name:   prefix + "_factual_claims",
			Status: statusPass,
		}}
	}
	detail := strings.Join(failures, "; ")
	if len(failures) > 3 {
		detail = strings.Join(failures[:3], "; ") + fmt.Sprintf("; and %d more", len(failures)-3)
	}
	return []workflow.StepCheck{{
		Name:   prefix + "_factual_claims",
		Status: statusFail,
		Detail: "comment contradicts YAML value — " + detail + ". Either correct the number to match the configured value, drop the number from the comment, or rephrase as aspirational (e.g. 'bump to N GB via the GUI when usage grows').",
	}}
}

// yamlIntFieldRe matches a line of the form `  someKey: 42` and captures
// both the key name and the integer value. Non-integer values (strings,
// mappings) are ignored — factual claims we check are all integers.
var yamlIntFieldRe = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9_]*)\s*:\s*(\d+)\s*$`)

// findAdjacentYAMLValue scans forward from startLine (exclusive) up to
// `window` lines looking for the first YAML integer field named `key`.
// Returns (value, true) on a hit, (0, false) on miss.
//
// Sibling-block safety: the scanner counts `- ` list-item sentinels and
// bails out when it crosses into the second one. The first sentinel is
// the current service's own header (the comment was a header-style
// comment immediately preceding a `- hostname:` line); the second
// sentinel means we've walked past the current service block's end and
// are about to read fields from a different service. Without this guard
// a comment on service A with no matching field would bleed into a
// later sibling's field and flag a spurious contradiction.
func findAdjacentYAMLValue(lines []string, startLine int, key string, window int) (int, bool) {
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
				return 0, false
			}
			continue
		}
		m := yamlIntFieldRe.FindStringSubmatch(lines[i])
		if m == nil {
			// Non-integer field — keep scanning; the field we want may
			// be further down in the same service block.
			continue
		}
		if m[1] == key {
			v, err := strconv.Atoi(m[2])
			if err != nil {
				return 0, false
			}
			return v, true
		}
	}
	return 0, false
}
