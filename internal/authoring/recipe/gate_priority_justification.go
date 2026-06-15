package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func gateRequirePriorityJustification(ctx GateContext) []Violation {
	var out []Violation
	for _, tier := range Tiers() {
		path := filepath.Join(ctx.OutputRoot, tier.Folder, "import.yaml")
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		body := string(raw)
		if !hasPriorityServices(body) {
			continue
		}
		if !hasPriorityJustificationBlock(body) {
			out = append(out, Violation{
				Code:     "missing-priority-justification-block",
				Path:     path,
				Severity: SeverityBlocking,
				Message: fmt.Sprintf(
					"tier %d (%s) declares `priority: 10` on managed services but has no TY5-class justification comment ahead of them. Add a comment block explaining WHY (canonical form: \"# Set higher priority for databases and storages,\\n# because the app depends on those services.\"). Author in `env/%d/import-comments` and re-close phase=env-content.",
					tier.Index, tier.Folder, tier.Index,
				),
			})
		}
	}
	return out
}

func hasPriorityServices(yaml string) bool {
	for line := range strings.SplitSeq(yaml, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "priority:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "priority:"))
		value = strings.Trim(value, `"'`)
		n, err := strconv.Atoi(value)
		if err == nil && n >= 10 {
			return true
		}
	}
	return false
}

func hasPriorityJustificationBlock(yaml string) bool {
	lines := strings.Split(yaml, "\n")
	firstService := -1
	firstHighPriority := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if firstService == -1 && strings.HasPrefix(trimmed, "- hostname:") {
			firstService = i
		}
		if firstHighPriority == -1 && strings.HasPrefix(trimmed, "priority:") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "priority:"))
			value = strings.Trim(value, `"'`)
			if n, err := strconv.Atoi(value); err == nil && n >= 10 {
				firstHighPriority = i
				break
			}
		}
	}
	if firstHighPriority == -1 {
		return false
	}
	start := 0
	if firstService >= 0 {
		start = firstService + 1
	}
	var block []string
	checkBlock := func() bool {
		if len(block) == 0 {
			return false
		}
		text := strings.ToLower(strings.Join(block, " "))
		hasPriorityNoun := strings.Contains(text, "priority") || strings.Contains(text, "boot-order")
		hasDependencyVerb := strings.Contains(text, "depend") || strings.Contains(text, "requires") || strings.Contains(text, "needs")
		return hasPriorityNoun && hasDependencyVerb
	}
	for _, line := range lines[start:firstHighPriority] {
		trimmed := strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(trimmed, "#"); ok {
			block = append(block, strings.TrimSpace(rest))
			continue
		}
		if checkBlock() {
			return true
		}
		block = nil
	}
	return checkBlock()
}
