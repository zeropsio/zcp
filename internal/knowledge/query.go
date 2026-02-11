package knowledge

import (
	"fmt"
	"strings"
)

// queryAliases maps common alternative terms to their Zerops equivalents.
var queryAliases = map[string]string{
	"postgres":  "postgres postgresql",
	"redis":     "redis valkey",
	"mysql":     "mysql mariadb",
	"node":      "node nodejs",
	"db":        "db database",
	"ssl":       "ssl tls",
	"env":       "env environment variable",
	"cert":      "cert certificate ssl tls",
	"ha":        "ha high-availability mode",
	"k8s":       "k8s kubernetes",
	"mongo":     "mongo mongodb",
	"docker":    "docker dockerfile",
	"pg":        "pg postgresql postgres",
	"js":        "js nodejs javascript",
	"ts":        "ts nodejs typescript",
	"s3":        "s3 object-storage",
	"cron":      "cron crontab schedule",
	"log":       "log logging logs",
	"logs":      "logs logging log",
	"dns":       "dns domain networking",
	"ci":        "ci ci-cd continuous integration",
	"cd":        "cd ci-cd continuous deployment",
	"dotnet":    "dotnet .net csharp",
	"csharp":    "csharp dotnet .net",
	"memcached": "memcached valkey cache",
}

func expandQuery(query string) string {
	words := strings.Fields(strings.ToLower(query))
	var expanded []string
	for _, w := range words {
		if alias, ok := queryAliases[w]; ok {
			expanded = append(expanded, alias)
		} else {
			expanded = append(expanded, w)
		}
	}
	return strings.Join(expanded, " ")
}

// unsupportedServices provides helpful messages for services not on Zerops.
var unsupportedServices = map[string]string{
	"mongodb":    "Zerops does not support MongoDB. Available databases: postgresql, mariadb",
	"mongo":      "Zerops does not support MongoDB. Available databases: postgresql, mariadb",
	"dynamodb":   "Zerops does not support DynamoDB. Available databases: postgresql, mariadb",
	"mysql":      "Zerops uses MariaDB (MySQL-compatible). Try: 'mariadb'",
	"memcached":  "Zerops uses Valkey (Redis-compatible) for caching. Try: 'valkey'",
	"sqlite":     "SQLite runs within your app container. For managed DB: 'postgresql' or 'mariadb'",
	"kubernetes": "Zerops is a PaaS, not Kubernetes. Try: 'deploy' or 'import.yml'",
	"k8s":        "Zerops is a PaaS, not Kubernetes. Try: 'deploy' or 'import.yml'",
}

// GenerateSuggestions produces helpful suggestions based on query and results.
func (s *Store) GenerateSuggestions(query string, results []SearchResult) []string {
	var suggestions []string

	for word := range strings.FieldsSeq(strings.ToLower(query)) {
		if msg, ok := unsupportedServices[word]; ok {
			suggestions = append(suggestions, msg)
		}
	}

	if len(results) == 0 {
		if len(suggestions) == 0 {
			suggestions = append(suggestions,
				fmt.Sprintf("No results for '%s'. Try broader terms: service name, 'zerops.yml', 'import.yml', 'gotchas'", query))
		}
		return capSuggestions(suggestions, 5)
	}

	if len(results) > 0 {
		topDoc := s.docs[results[0].URI]
		if topDoc != nil {
			seeAlso := extractSeeAlsoSuggestions(topDoc.Content)
			suggestions = append(suggestions, seeAlso...)
		}
	}

	return capSuggestions(suggestions, 5)
}

func extractSeeAlsoSuggestions(content string) []string {
	var suggestions []string
	lines := strings.Split(content, "\n")
	inSeeAlso := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "## See Also" {
			inSeeAlso = true
			continue
		}
		if inSeeAlso {
			if strings.HasPrefix(trimmed, "##") {
				break
			}
			if strings.HasPrefix(trimmed, "- zerops://") {
				uri := strings.TrimPrefix(trimmed, "- ")
				suggestions = append(suggestions, "Related: "+uri)
			}
		}
	}
	return suggestions
}

func capSuggestions(suggestions []string, limit int) []string {
	if len(suggestions) > limit {
		return suggestions[:limit]
	}
	return suggestions
}

func extractSnippet(content, query string, maxLen int) string {
	lower := strings.ToLower(content)
	queryLower := strings.ToLower(query)

	bestPos := -1
	for word := range strings.FieldsSeq(queryLower) {
		pos := strings.Index(lower, word)
		if pos >= 0 && (bestPos < 0 || pos < bestPos) {
			bestPos = pos
		}
	}

	if bestPos < 0 {
		lines := strings.SplitN(content, "\n", 3)
		if len(lines) >= 3 {
			return truncate(lines[2], maxLen)
		}
		return truncate(content, maxLen)
	}

	start := bestPos - maxLen/3
	start = max(start, 0)
	end := start + maxLen
	end = min(end, len(content))

	snippet := content[start:end]

	if start > 0 {
		if idx := strings.IndexByte(snippet, ' '); idx >= 0 {
			snippet = "..." + snippet[idx+1:]
		}
	}
	if end < len(content) {
		if idx := strings.LastIndexByte(snippet, ' '); idx >= 0 {
			snippet = snippet[:idx] + "..."
		}
	}

	return snippet
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
