package knowledge

import (
	"strings"
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

// runtimeNormalizer maps MCP runtime types to runtimes.md section names.
var runtimeNormalizer = map[string]string{
	"php":        "PHP",
	"php-nginx":  "PHP",
	"php-apache": "PHP",
	"nodejs":     "Node.js",
	"bun":        "Bun",
	"deno":       "Deno",
	"python":     "Python",
	"go":         "Go",
	"java":       "Java",
	"dotnet":     ".NET",
	"rust":       "Rust",
	"elixir":     "Elixir",
	"gleam":      "Gleam",
	"static":     "Static",
	"docker":     "Docker",
	"alpine":     "Alpine",
	"ubuntu":     "Ubuntu",
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

// getRuntimeException returns the section content for a normalized runtime name from runtimes.md.
func (s *Store) getRuntimeException(normalizedName string) string {
	doc, err := s.Get("zerops://foundation/runtimes")
	if err != nil {
		return ""
	}
	sections := parseH2Sections(doc.Content)
	return sections[normalizedName]
}

// getServiceCard returns the section content for a normalized service name from services.md.
func (s *Store) getServiceCard(normalizedName string) string {
	doc, err := s.Get("zerops://foundation/services")
	if err != nil {
		return ""
	}
	sections := parseH2Sections(doc.Content)
	return sections[normalizedName]
}

// getWiringSyntax returns the "Syntax Rules" section from wiring.md.
func (s *Store) getWiringSyntax() string {
	doc, err := s.Get("zerops://foundation/wiring")
	if err != nil {
		return ""
	}
	sections := parseH2Sections(doc.Content)
	return sections["Syntax Rules"]
}

// getWiringSection returns the wiring template for a normalized service name from wiring.md.
func (s *Store) getWiringSection(normalizedName string) string {
	doc, err := s.Get("zerops://foundation/wiring")
	if err != nil {
		return ""
	}
	sections := parseH2Sections(doc.Content)
	return sections[normalizedName]
}

// serviceDecisionMap maps service base names to decision document names.
var serviceDecisionMap = map[string]string{
	"postgresql":    "choose-database",
	"mariadb":       "choose-database",
	"clickhouse":    "choose-database",
	"valkey":        "choose-cache",
	"keydb":         "choose-cache",
	"kafka":         "choose-queue",
	"nats":          "choose-queue",
	"elasticsearch": "choose-search",
	"meilisearch":   "choose-search",
	"qdrant":        "choose-search",
	"typesense":     "choose-search",
}

// getRelevantDecisions returns compact decision hints based on the runtime and services.
func (s *Store) getRelevantDecisions(runtime string, services []string) string {
	var hints []string

	// Runtime-related decisions
	if runtime != "" {
		base, _, _ := strings.Cut(runtime, "@")
		if base == "go" || base == "python" || base == "dotnet" || base == "rust" {
			if tip := s.getDecisionTLDR("choose-runtime-base"); tip != "" {
				hints = append(hints, tip)
			}
		}
	}

	// Service-related decisions (deduplicate by decision doc)
	seen := make(map[string]bool)
	for _, svc := range services {
		base, _, _ := strings.Cut(svc, "@")
		if decisionDoc, ok := serviceDecisionMap[base]; ok && !seen[decisionDoc] {
			seen[decisionDoc] = true
			if tip := s.getDecisionTLDR(decisionDoc); tip != "" {
				hints = append(hints, tip)
			}
		}
	}

	if len(hints) == 0 {
		return ""
	}
	return strings.Join(hints, "\n")
}

// getDecisionTLDR returns the TL;DR from a decision document as a compact hint.
func (s *Store) getDecisionTLDR(decisionName string) string {
	uri := "zerops://decisions/" + decisionName
	doc, err := s.Get(uri)
	if err != nil {
		return ""
	}
	if doc.TLDR != "" {
		return "- **" + doc.Title + "**: " + doc.TLDR
	}
	return ""
}
