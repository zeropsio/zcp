package recipe

import (
	"fmt"
	"regexp"
	"strings"
)

// checkSlotShape enforces per-fragment-id structural constraints at
// record-fragment time (run-16 §8.1). Returns "" when the fragment body
// passes; returns a non-empty refusal message naming the offending shape
// when the body violates the slot's contract.
//
// Why record-time refusal beats finalize-validator refusal: same-context
// recovery. The agent that just wrote the fragment is still in the
// conversation that knows why it wrote what it wrote — a refusal at
// record time gives the agent a specific, mechanical reshape instruction
// (e.g. "split this multi-heading IG into per-slot integration-guide/1,
// integration-guide/2 fragments"). A refusal at finalize is cross-phase
// and the agent has to re-load context.
//
// Legacy fragment IDs (back-compat per §6.5) — `codebase/<h>/integration-guide`
// without the `/<n>` slot suffix, `codebase/<h>/claude-md/{service-facts,notes}`
// sub-slots — are NOT subject to the new constraints; they fall through.
func checkSlotShape(fragmentID, body string) string {
	switch {
	case fragmentID == fragmentIDRoot:
		return checkRootIntro(body)
	case envIntroRe.MatchString(fragmentID):
		return checkEnvIntro(body)
	case envImportCommentsRe.MatchString(fragmentID):
		return checkEnvImportComments(body)
	case codebaseIntroRe.MatchString(fragmentID):
		return checkCodebaseIntro(body)
	case slottedIGRe.MatchString(fragmentID):
		return checkSlottedIG(body)
	case codebaseKBRe.MatchString(fragmentID):
		return checkCodebaseKB(body)
	case zeropsYamlCommentsRe.MatchString(fragmentID):
		return checkZeropsYamlComments(body)
	case singleSlotClaudeMDRe.MatchString(fragmentID):
		return checkClaudeMD(body)
	}
	return ""
}

var (
	envIntroRe            = regexp.MustCompile(`^env/[0-9]+/intro$`)
	envImportCommentsRe   = regexp.MustCompile(`^env/[0-9]+/import-comments/[A-Za-z0-9_-]+$`)
	codebaseIntroRe       = regexp.MustCompile(`^codebase/[A-Za-z0-9_-]+/intro$`)
	slottedIGRe           = regexp.MustCompile(`^codebase/[A-Za-z0-9_-]+/integration-guide/[0-9]+$`)
	codebaseKBRe          = regexp.MustCompile(`^codebase/[A-Za-z0-9_-]+/knowledge-base$`)
	zeropsYamlCommentsRe  = regexp.MustCompile(`^codebase/[A-Za-z0-9_-]+/zerops-yaml-comments/[A-Za-z0-9_.-]+$`)
	singleSlotClaudeMDRe  = regexp.MustCompile(`^codebase/[A-Za-z0-9_-]+/claude-md$`)
	headingH2Re           = regexp.MustCompile(`(?m)^##\s+`)
	headingH3Re           = regexp.MustCompile(`(?m)^###\s+`)
	zeropsExtractMarkerRe = regexp.MustCompile(`<!--\s*#ZEROPS_EXTRACT_`)
	zeropsHeadingRe       = regexp.MustCompile(`(?m)^##\s+Zerops\b`)
	zeropsToolRe          = regexp.MustCompile(`\bzerops_[a-z_]+`)
	zscRe                 = regexp.MustCompile(`\bzsc\b`)
	zcpRe                 = regexp.MustCompile(`\bzcp\b`)
	zcliRe                = regexp.MustCompile(`\bzcli\b`)
)

func checkRootIntro(body string) string {
	if len(body) > 500 {
		return fmt.Sprintf("root/intro is a 1-sentence string, %d chars > 500-cap. See spec §Surface 1.", len(body))
	}
	if headingH2Re.MatchString(body) || headingH3Re.MatchString(body) {
		return "root/intro is a 1-sentence string with no markdown headings. See spec §Surface 1."
	}
	return ""
}

func checkEnvIntro(body string) string {
	if len(body) > 350 {
		return fmt.Sprintf("env/<N>/intro is a 1-2 sentence string, %d chars > 350-cap. See spec §Surface 2.", len(body))
	}
	if headingH2Re.MatchString(body) {
		return "env/<N>/intro must not contain `## ` headings. See spec §Surface 2."
	}
	if zeropsExtractMarkerRe.MatchString(body) {
		// R-15-3 closure — duplicate extract markers in env intros came from
		// agents nesting #ZEROPS_EXTRACT_* tokens inside the slot.
		return "env/<N>/intro must not contain `<!-- #ZEROPS_EXTRACT_*` tokens (R-15-3); the extract markers are engine-stamped at stitch time."
	}
	return ""
}

func checkEnvImportComments(body string) string {
	lines := strings.Count(body, "\n")
	if !strings.HasSuffix(body, "\n") && body != "" {
		lines++ // trailing-newline-less body still counts the last line
	}
	if lines > 8 {
		return fmt.Sprintf("env/<N>/import-comments/<host> ≤ 8 lines; got %d. See spec §Surface 3.", lines)
	}
	return ""
}

func checkCodebaseIntro(body string) string {
	if len(body) > 500 {
		return fmt.Sprintf("codebase/<h>/intro is a 1-2 sentence string, %d chars > 500-cap. See spec §Surface 4.", len(body))
	}
	if headingH2Re.MatchString(body) {
		return "codebase/<h>/intro must not contain `## ` headings. See spec §Surface 4."
	}
	return ""
}

func checkSlottedIG(body string) string {
	headings := headingH3Re.FindAllStringIndex(body, -1)
	if len(headings) != 1 {
		// R-15-5 closure: per-slot IG fragments are exactly one item.
		// Multiple ### headings in one slot mean the agent collapsed
		// multiple items into one slot — refuse so they split.
		return fmt.Sprintf(
			"codebase/<h>/integration-guide/<n> is one item: exactly one `### ` heading per slot, got %d. Split into separate slots. See spec §Surface 4.",
			len(headings),
		)
	}
	// Body line cap (excluding the heading line itself).
	lines := strings.Split(body, "\n")
	if len(lines) > 30 {
		return fmt.Sprintf("codebase/<h>/integration-guide/<n> body ≤ 30 lines; got %d. See spec §Surface 4.", len(lines))
	}
	return ""
}

func checkCodebaseKB(body string) string {
	bulletCount := 0
	for line := range strings.SplitSeq(body, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		bulletCount++
		// Each `- ` bullet must start with `**Topic** —` (em-dash, U+2014)
		// or `--` ASCII fallback. Run-15 R-15-6 evidence shows agents
		// occasionally drop the topic-name and write free prose; refusing
		// at record-time forces the structured shape.
		rest := strings.TrimPrefix(trimmed, "- ")
		if !strings.HasPrefix(rest, "**") {
			return "codebase/<h>/knowledge-base bullets must follow `- **Topic** — 2-4 sentences` shape (no leading `**` found). See spec §Surface 5."
		}
	}
	if bulletCount > 8 {
		return fmt.Sprintf("codebase/<h>/knowledge-base ≤ 8 bullets; got %d. See spec §Surface 5.", bulletCount)
	}
	return ""
}

func checkZeropsYamlComments(body string) string {
	lines := strings.Count(body, "\n")
	if !strings.HasSuffix(body, "\n") && body != "" {
		lines++
	}
	if lines > 6 {
		return fmt.Sprintf("codebase/<h>/zerops-yaml-comments/<block> ≤ 6 lines; got %d. See spec §Surface 7.", lines)
	}
	return ""
}

func checkClaudeMD(body string) string {
	// R-15-4 closure — Zerops-flavored content must NOT appear in CLAUDE.md.
	// Surface 6 is the porter's `claude /init` codebase guide; Zerops platform
	// content (managed-service hostnames, `zsc`/`zerops_*`/`zcp`/`zcli` tool
	// references, env-var aliases) belongs in IG / KB / zerops.yaml comments.
	if zeropsHeadingRe.MatchString(body) {
		return "codebase/<h>/claude-md must not contain `## Zerops` headings (R-15-4); Zerops platform content belongs in IG/KB/zerops.yaml comments per spec §Surface 6."
	}
	for _, hit := range []struct {
		re    *regexp.Regexp
		token string
	}{
		{zscRe, "zsc"},
		{zeropsToolRe, "zerops_*"},
		{zcpRe, "zcp"},
		{zcliRe, "zcli"},
	} {
		if hit.re.MatchString(body) {
			return fmt.Sprintf("codebase/<h>/claude-md must not contain `%s` tool references (R-15-4); CLAUDE.md is a Zerops-free codebase guide per spec §Surface 6.", hit.token)
		}
	}
	// 80-line cap.
	lines := strings.Count(body, "\n")
	if !strings.HasSuffix(body, "\n") && body != "" {
		lines++
	}
	if lines > 80 {
		return fmt.Sprintf("codebase/<h>/claude-md ≤ 80 lines; got %d. See spec §Surface 6.", lines)
	}
	// Body must carry exactly the 3-section /init shape (project overview
	// implicit from the H1 + intro; ## Build & run; ## Architecture; one
	// optional extra ## section). The H2 count check is permissive — 2-4
	// H2 sections accepted to allow framework variation.
	h2 := headingH2Re.FindAllStringIndex(body, -1)
	if len(h2) < 2 || len(h2) > 4 {
		return fmt.Sprintf("codebase/<h>/claude-md must have 2-4 `## ` sections (Build & run, Architecture, optional extras); got %d. See spec §Surface 6.", len(h2))
	}
	return ""
}

// claudeMDFragmentRefusalForHostname extends checkClaudeMD with a
// plan-time hostname check: the handler walks plan.Services and calls
// this once per declared hostname. Static-token coverage in checkClaudeMD
// catches `zsc` / `zerops_*` / `zcp` / `zcli` / `## Zerops`; the per-host
// loop catches managed-service hostname leakage like `db`, `cache`,
// `search`, `meilisearch`, etc. that the static list can't enumerate.
func claudeMDFragmentRefusalForHostname(body, hostname string) string {
	if hostname == "" {
		return ""
	}
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(hostname) + `\b`)
	if re.MatchString(body) {
		return fmt.Sprintf("codebase/<h>/claude-md must not reference managed-service hostname `%s` (R-15-4); the Zerops integration belongs in IG/KB/zerops.yaml comments per spec §Surface 6.", hostname)
	}
	return ""
}
