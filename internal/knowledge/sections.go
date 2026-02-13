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

// runtimeNormalizer maps MCP runtime types to runtime-exceptions.md section names.
var runtimeNormalizer = map[string]string{
	// PHP variants
	"php":        "PHP",
	"php-nginx":  "PHP",
	"php-apache": "PHP",

	// Node.js ecosystem
	"nodejs": "Node.js",
	"bun":    "Bun",
	"deno":   "Deno",

	// Python
	"python": "Python",

	// Go
	"go": "Go",

	// Java
	"java": "Java",

	// .NET
	"dotnet": ".NET",

	// Rust
	"rust": "Rust",

	// Elixir
	"elixir": "Elixir",

	// Gleam
	"gleam": "Gleam",

	// Static
	"static": "Static",

	// Docker
	"docker": "Docker",

	// Base OS (no exceptions expected, but map for completeness)
	"alpine": "Alpine",
	"ubuntu": "Ubuntu",
}

// normalizeRuntimeName extracts runtime base name from versioned string and maps to section name.
// Examples:
//
//	"php-nginx@8.4" → "PHP"
//	"nodejs@22" → "Node.js"
//	"unknown@1.0" → "" (not an error, just no exceptions)
func normalizeRuntimeName(runtime string) string {
	// Strip version suffix (@X.Y)
	base, _, _ := strings.Cut(runtime, "@")

	// Lookup normalized name
	if normalized, ok := runtimeNormalizer[base]; ok {
		return normalized
	}

	// Unknown runtime → return empty (graceful degradation)
	return ""
}

// serviceNormalizer maps MCP service types to service-cards.md section names.
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
	// Strip version suffix (@X.Y)
	base, _, _ := strings.Cut(service, "@")

	// Lookup normalized name
	if normalized, ok := serviceNormalizer[base]; ok {
		return normalized
	}

	// Unknown service → title-case each dash-separated word (graceful degradation)
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
