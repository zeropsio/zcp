package knowledge

import (
	"strings"
)

// Runtime slug constants for repeated string literals.
const (
	runtimeStatic    = "static"
	runtimeNginx     = "nginx"
	runtimePHPNginx  = "php-nginx"
	runtimePHPApache = "php-apache"
)

// parseH2Sections splits markdown content by H2 headers (## ), respecting fenced code blocks.
// Returns map[sectionName]sectionContent.
// If no H2 headers found, returns empty map.
// Code blocks delimited by ``` are tracked to avoid splitting on ## inside YAML/JSON.
func parseH2Sections(content string) map[string]string {
	sections := make(map[string]string)
	var currentSection string
	var currentContent strings.Builder
	inCodeBlock := false

	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)

		// Track fenced code block state (``` toggles)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			if currentSection != "" {
				currentContent.WriteString(line + "\n")
			}
			continue
		}

		// Only process H2 headers outside code blocks
		if !inCodeBlock && strings.HasPrefix(trimmed, "## ") {
			// Save previous section if exists
			if currentSection != "" {
				sections[currentSection] = strings.TrimSpace(currentContent.String())
			}
			// Start new section
			currentSection = strings.TrimPrefix(trimmed, "## ")
			currentContent.Reset()
			continue
		}

		// Accumulate content for current section
		if currentSection != "" {
			currentContent.WriteString(line + "\n")
		}
	}

	// Save final section
	if currentSection != "" {
		sections[currentSection] = strings.TrimSpace(currentContent.String())
	}

	return sections
}

// runtimeNormalizer maps MCP runtime types to runtime guide file slugs.
var runtimeNormalizer = map[string]string{
	"php":            "php",
	runtimePHPNginx:  "php",
	runtimePHPApache: "php",
	"nodejs":         "nodejs",
	"bun":            "bun",
	"deno":           "deno",
	"python":         "python",
	"go":             "go",
	"java":           "java",
	"dotnet":         "dotnet",
	"rust":           "rust",
	"elixir":         "elixir",
	"gleam":          "gleam",
	"ruby":           "ruby",
	"nginx":          runtimeNginx,
	"static":         runtimeStatic,
	"docker":         "docker",
	"alpine":         "alpine",
	"ubuntu":         "ubuntu",
}

// normalizeRuntimeName extracts runtime base name from versioned string and maps to section name.
// Examples:
//
//	"php-nginx@8.4" → "PHP"
//	"nodejs@22" → "Node.js"
//	"unknown@1.0" → "" (not an error, just no exceptions)
func normalizeRuntimeName(runtime string) string {
	base, _, _ := strings.Cut(runtime, "@")
	if normalized, ok := runtimeNormalizer[base]; ok {
		return normalized
	}
	return ""
}

// autoPromoteRuntime scans services for a known runtime name when runtime is empty.
// Returns the promoted runtime string and the remaining services slice.
// Only the first matching runtime is promoted; if none match, returns ("", original services).
func autoPromoteRuntime(services []string) (string, []string) {
	for i, svc := range services {
		base, _, _ := strings.Cut(svc, "@")
		if _, ok := runtimeNormalizer[base]; ok {
			// Promote this entry to runtime, remove from services
			remaining := make([]string, 0, len(services)-1)
			remaining = append(remaining, services[:i]...)
			remaining = append(remaining, services[i+1:]...)
			return svc, remaining
		}
	}
	return "", services
}

// serviceNormalizer maps MCP service types to services.md section names.
var serviceNormalizer = map[string]string{
	"postgresql":     "PostgreSQL",
	"mariadb":        "MariaDB",
	"valkey":         "Valkey",
	"keydb":          "KeyDB",
	"elasticsearch":  "Elasticsearch",
	"object-storage": "Object Storage",
	"shared-storage": "Shared Storage",
	"kafka":          "Kafka",
	"nats":           "NATS",
	"meilisearch":    "Meilisearch",
	"clickhouse":     "ClickHouse",
	"qdrant":         "Qdrant",
	"typesense":      "Typesense",
	"rabbitmq":       "RabbitMQ",
}

// normalizeServiceName extracts service base name from versioned string and maps to section name.
// Examples:
//
//	"postgresql@16" → "PostgreSQL"
//	"valkey@7.2" → "Valkey"
//	"object-storage" → "Object Storage"
//	"unknown-service@1" → "Unknown-Service" (graceful title-case)
func normalizeServiceName(service string) string {
	base, _, _ := strings.Cut(service, "@")
	if normalized, ok := serviceNormalizer[base]; ok {
		return normalized
	}
	if base == "" {
		return ""
	}
	parts := strings.Split(base, "-")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "-")
}

// --- Section extraction helpers for GetBriefing layers ---

// getRuntimeGuide returns the full content of a per-runtime guide document.
// Resolution order: recipes/{slug}-hello-world → recipes/{slug} → bases/{slug}.
func (s *Store) getRuntimeGuide(slug string) string {
	// Hello-world recipe (primary format for language runtimes)
	if doc, err := s.Get("zerops://recipes/" + slug + "-hello-world"); err == nil {
		return doc.Content
	}
	// Direct recipe match
	if doc, err := s.Get("zerops://recipes/" + slug); err == nil {
		return doc.Content
	}
	// Infrastructure base guide (alpine, docker, nginx, static, ubuntu)
	if doc, err := s.Get("zerops://bases/" + slug); err == nil {
		return doc.Content
	}
	return ""
}

// getServiceCard returns the section content for a normalized service name from services.md.
func (s *Store) getServiceCard(normalizedName string) string {
	doc, err := s.Get("zerops://themes/services")
	if err != nil {
		return ""
	}
	return doc.H2Sections()[normalizedName]
}

// getWiringSyntax returns the "Wiring Syntax" section from services.md.
func (s *Store) getWiringSyntax() string {
	doc, err := s.Get("zerops://themes/services")
	if err != nil {
		return ""
	}
	return doc.H2Sections()["Wiring Syntax"]
}

// decisionFileMap maps decision display names to decisions/ file URIs.
var decisionFileMap = map[string]string{
	"Choose Database":     "zerops://decisions/choose-database",
	"Choose Cache":        "zerops://decisions/choose-cache",
	"Choose Queue":        "zerops://decisions/choose-queue",
	"Choose Search":       "zerops://decisions/choose-search",
	"Choose Runtime Base": "zerops://decisions/choose-runtime-base",
}

// decisionSectionMap maps service base names to decision names.
var decisionSectionMap = map[string]string{
	"postgresql":    "Choose Database",
	"mariadb":       "Choose Database",
	"clickhouse":    "Choose Database",
	"valkey":        "Choose Cache",
	"keydb":         "Choose Cache",
	"kafka":         "Choose Queue",
	"nats":          "Choose Queue",
	"rabbitmq":      "Choose Queue",
	"elasticsearch": "Choose Search",
	"meilisearch":   "Choose Search",
	"qdrant":        "Choose Search",
	"typesense":     "Choose Search",
}

// parseH3Sections splits content by H3 headers (### ), respecting fenced code blocks.
// Returns map[sectionName]sectionContent.
func parseH3Sections(content string) map[string]string {
	sections := make(map[string]string)
	var currentSection string
	var currentContent strings.Builder
	inCodeBlock := false

	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			if currentSection != "" {
				currentContent.WriteString(line + "\n")
			}
			continue
		}

		if !inCodeBlock && strings.HasPrefix(trimmed, "### ") {
			if currentSection != "" {
				sections[currentSection] = strings.TrimSpace(currentContent.String())
			}
			currentSection = strings.TrimPrefix(trimmed, "### ")
			currentContent.Reset()
			continue
		}

		if currentSection != "" {
			currentContent.WriteString(line + "\n")
		}
	}

	if currentSection != "" {
		sections[currentSection] = strings.TrimSpace(currentContent.String())
	}

	return sections
}

// getRelevantDecisions returns compact decision hints based on the runtime and services.
// Reads from authoritative decisions/ files (not operations.md).
func (s *Store) getRelevantDecisions(runtime string, services []string) string {
	var hints []string

	// Runtime-related decisions
	if runtime != "" {
		base, _, _ := strings.Cut(runtime, "@")
		if base == "go" || base == "python" || base == "dotnet" || base == "rust" {
			if summary := s.getDecisionSummary("Choose Runtime Base"); summary != "" {
				hints = append(hints, "- **Choose Runtime Base**: "+summary)
			}
		}
	}

	// Service-related decisions (deduplicate by decision name)
	seen := make(map[string]bool)
	for _, svc := range services {
		base, _, _ := strings.Cut(svc, "@")
		if decisionName, ok := decisionSectionMap[base]; ok && !seen[decisionName] {
			seen[decisionName] = true
			if summary := s.getDecisionSummary(decisionName); summary != "" {
				hints = append(hints, "- **"+decisionName+"**: "+summary)
			}
		}
	}

	if len(hints) == 0 {
		return ""
	}
	return strings.Join(hints, "\n")
}

// getDecisionSummary loads a decision file and extracts the TL;DR as the summary.
func (s *Store) getDecisionSummary(decisionName string) string {
	uri, ok := decisionFileMap[decisionName]
	if !ok {
		return ""
	}
	doc, err := s.Get(uri)
	if err != nil {
		return ""
	}
	// Use TL;DR if available (decisions/ files have it), fall back to first paragraph.
	if doc.TLDR != "" {
		return doc.TLDR
	}
	return doc.Description
}

// extractDecisionSummary extracts the first paragraph (all lines until first blank line)
// from a decision section. This provides richer context than just the first line.
func extractDecisionSummary(content string) string {
	var lines []string
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" && len(lines) > 0 {
			break
		}
		if trimmed != "" && !strings.HasPrefix(trimmed, "##") {
			lines = append(lines, trimmed)
		}
	}
	result := strings.Join(lines, " ")
	if len(result) > 500 {
		return result[:500] + "..."
	}
	return result
}
