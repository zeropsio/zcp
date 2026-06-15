package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	tradeoffLeadRestore  = "restoring from snapshot"
	tradeoffLeadTolerate = "tolerate a brief restart window"
	tradeoffSuffixGrade  = "tradeoff"
)

type tradeoffHit struct {
	hostname string
	line     int
	lead     string
}

func gateForbidTradeoffLeadOnDbComment(ctx GateContext) []Violation {
	var out []Violation
	for _, tier := range Tiers() {
		path := filepath.Join(ctx.OutputRoot, tier.Folder, "import.yaml")
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, hit := range scanTradeoffLeadComments(string(raw)) {
			out = append(out, Violation{
				Code:     "managed-service-comment-tradeoff-lead",
				Path:     path,
				Severity: SeverityBlocking,
				Message: fmt.Sprintf(
					"tier %d (%s) %s comment at L%d leads with operational-tradeoff voice (%q). The lead clause must be role + relationship (e.g. \"Single-instance NON_HA Postgres — used by the api codebase to store items + uploads\"); durability/restoration tradeoffs belong in supporting clauses. Edit the comment block in `env/%d/import-comments` (or rerun env-content if you didn't author it) and re-close phase=env-content.",
					tier.Index, tier.Folder, hit.hostname, hit.line, hit.lead, tier.Index,
				),
			})
		}
	}
	return out
}

func scanTradeoffLeadComments(yaml string) []tradeoffHit {
	lines := strings.Split(yaml, "\n")
	var out []tradeoffHit
	for i, line := range lines {
		hostname, ok := serviceHostnameFromLine(line)
		if !ok {
			continue
		}
		comment, start := precedingCommentBlock(lines, i)
		if comment == "" {
			continue
		}
		lead := commentLeadClause(comment)
		if forbiddenTradeoffLead(lead) {
			out = append(out, tradeoffHit{
				hostname: hostname,
				line:     start + 1,
				lead:     lead,
			})
		}
	}
	return out
}

func serviceHostnameFromLine(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "- hostname:") {
		return "", false
	}
	host := strings.TrimSpace(strings.TrimPrefix(trimmed, "- hostname:"))
	host = strings.Trim(host, `"'`)
	if host == "" {
		return "", false
	}
	return host, true
}

func precedingCommentBlock(lines []string, serviceLine int) (string, int) {
	var parts []string
	start := serviceLine
	for i := serviceLine - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			if len(parts) == 0 {
				continue
			}
			break
		}
		if !strings.HasPrefix(trimmed, "#") {
			break
		}
		start = i
		text := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		parts = append(parts, text)
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.TrimSpace(strings.Join(parts, " ")), start
}

func commentLeadClause(comment string) string {
	lead := strings.TrimSpace(comment)
	if before, after, ok := strings.Cut(lead, "—"); ok {
		_ = before
		lead = strings.TrimSpace(after)
	}
	if before, _, ok := strings.Cut(lead, "."); ok {
		lead = strings.TrimSpace(before)
	}
	return lead
}

func forbiddenTradeoffLead(lead string) bool {
	lower := strings.ToLower(lead)
	if strings.Contains(lower, tradeoffLeadTolerate) {
		return true
	}
	return strings.Contains(lower, tradeoffLeadRestore) && strings.Contains(lower, tradeoffSuffixGrade)
}
