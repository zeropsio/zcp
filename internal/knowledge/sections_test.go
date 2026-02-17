// Tests for: sections.go — H2 section parser and runtime/service normalizers

package knowledge

import (
	"strings"
	"testing"
)

// --- H2 Section Parser Tests ---

func TestParseH2Sections_Basic(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		want    map[string]string
	}{
		{
			name: "two sections",
			content: `# Title
## Section One
Content one.
## Part Two
Content two.
`,
			want: map[string]string{
				"Section One": "Content one.",
				"Part Two":    "Content two.",
			},
		},
		{
			name:    "no headers",
			content: "Just content without headers",
			want:    map[string]string{},
		},
		{
			name: "content before first header ignored",
			content: `# Title
Some intro text
## Initial Header
Body text`,
			want: map[string]string{
				"Initial Header": "Body text",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseH2Sections(tt.content)
			if len(got) != len(tt.want) {
				t.Errorf("parseH2Sections() got %d sections, want %d", len(got), len(tt.want))
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("section %q = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestParseH2Sections_WithCodeBlock(t *testing.T) {
	t.Parallel()
	content := `## YAML Example
Here's some YAML:
` + "```yaml" + `
services:
  - hostname: api
    ## This is NOT a section header
` + "```" + `
## Next Section
More content.
`
	sections := parseH2Sections(content)

	if len(sections) != 2 {
		t.Errorf("expected 2 sections, got %d", len(sections))
	}

	yamlSection := sections["YAML Example"]
	if !strings.Contains(yamlSection, "## This is NOT") {
		t.Error("YAML Example section should contain '## This is NOT' from code block")
	}

	if sections["Next Section"] != "More content." {
		t.Errorf("Next Section = %q, want %q", sections["Next Section"], "More content.")
	}
}

func TestParseH2Sections_NestedCodeBlocks(t *testing.T) {
	t.Parallel()
	content := `## Code Examples
First block:
` + "```" + `
code here
` + "```" + `
Second block:
` + "```go" + `
func main() {}
` + "```" + `
## After Code
Done.`

	sections := parseH2Sections(content)
	if len(sections) != 2 {
		t.Errorf("expected 2 sections, got %d", len(sections))
	}
}

func TestParseH2Sections_EmptySection(t *testing.T) {
	t.Parallel()
	content := `## Empty
## Next
Content here`

	sections := parseH2Sections(content)
	if sections["Empty"] != "" {
		t.Errorf("Empty section should be empty string, got %q", sections["Empty"])
	}
	if sections["Next"] != "Content here" {
		t.Errorf("Next section = %q, want %q", sections["Next"], "Content here")
	}
}

func TestParseH2Sections_MultipleCodeBlocks(t *testing.T) {
	t.Parallel()
	content := `## Section One
Text before code
` + "```" + `
## Not a header
` + "```" + `
Text between
` + "```" + `
## Also not a header
` + "```" + `
Text after
## Section Two
Final content`

	sections := parseH2Sections(content)
	if len(sections) != 2 {
		t.Errorf("expected 2 sections, got %d", len(sections))
	}

	if !strings.Contains(sections["Section One"], "## Not a header") {
		t.Error("Section One should contain code block content")
	}
	if !strings.Contains(sections["Section One"], "## Also not a header") {
		t.Error("Section One should contain second code block content")
	}
}

// --- Decision Summary Tests ---

func TestExtractDecisionSummary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "single line",
			content: "Use PostgreSQL for everything.",
			want:    "Use PostgreSQL for everything.",
		},
		{
			name:    "multi-line paragraph",
			content: "Use PostgreSQL for most use cases.\nMariaDB only when wsrep replication is needed.\nClickHouse for analytics.",
			want:    "Use PostgreSQL for most use cases. MariaDB only when wsrep replication is needed. ClickHouse for analytics.",
		},
		{
			name:    "stops at blank line",
			content: "Use PostgreSQL for most use cases.\n\nDetailed comparison table below:",
			want:    "Use PostgreSQL for most use cases.",
		},
		{
			name:    "skips H2 headers",
			content: "## Subsection\nUse Valkey.\nKeyDB is deprecated.",
			want:    "Use Valkey. KeyDB is deprecated.",
		},
		{
			name:    "empty content",
			content: "",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractDecisionSummary(tt.content)
			if got != tt.want {
				t.Errorf("extractDecisionSummary() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- Runtime Normalizer Tests ---

func TestNormalizeRuntimeName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		// PHP variants
		{"php-nginx@8.4", "PHP"},
		{"php-apache@8.3", "PHP"},
		{"php@8.4", "PHP"},
		{"php", "PHP"},

		// Node.js ecosystem
		{"nodejs@22", "Node.js"},
		{"nodejs@20", "Node.js"},
		{"nodejs", "Node.js"},
		{"bun@1.2", "Bun"},
		{"bun", "Bun"},
		{"deno@2", "Deno"},

		// Python
		{"python@3.12", "Python"},
		{"python@3.11", "Python"},
		{"python", "Python"},

		// Go
		{"go@1", "Go"},
		{"go", "Go"},

		// Java
		{"java@21", "Java"},
		{"java", "Java"},

		// .NET
		{"dotnet@8", ".NET"},
		{"dotnet", ".NET"},

		// Rust
		{"rust@1", "Rust"},
		{"rust", "Rust"},

		// Elixir
		{"elixir@1", "Elixir"},

		// Gleam
		{"gleam@1", "Gleam"},

		// Ruby
		{"ruby@3.4", "Ruby"},
		{"ruby@3.3", "Ruby"},
		{"ruby", "Ruby"},

		// Static
		{"static", "Static"},

		// Docker
		{"docker@26", "Docker"},
		{"docker", "Docker"},

		// Base OS
		{"alpine", "Alpine"},
		{"ubuntu", "Ubuntu"},

		// Unknown
		{"unknown@1.0", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := normalizeRuntimeName(tt.input)
			if got != tt.want {
				t.Errorf("normalizeRuntimeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- Auto-Promote Runtime Tests ---

func TestAutoPromoteRuntime(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		services     []string
		wantRuntime  string
		wantServices []string
	}{
		{
			name:         "promotes first runtime found",
			services:     []string{"python@3.12", "valkey@7.2"},
			wantRuntime:  "python@3.12",
			wantServices: []string{"valkey@7.2"},
		},
		{
			name:         "no runtimes in services",
			services:     []string{"postgresql@16", "valkey@7.2"},
			wantRuntime:  "",
			wantServices: []string{"postgresql@16", "valkey@7.2"},
		},
		{
			name:         "runtime at end of list",
			services:     []string{"mariadb@10.6", "java@21"},
			wantRuntime:  "java@21",
			wantServices: []string{"mariadb@10.6"},
		},
		{
			name:         "runtime only — no remaining services",
			services:     []string{"nodejs@22"},
			wantRuntime:  "nodejs@22",
			wantServices: []string{},
		},
		{
			name:         "empty services",
			services:     []string{},
			wantRuntime:  "",
			wantServices: []string{},
		},
		{
			name:         "multiple runtimes — only first promoted",
			services:     []string{"python@3.12", "nodejs@22", "valkey@7.2"},
			wantRuntime:  "python@3.12",
			wantServices: []string{"nodejs@22", "valkey@7.2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotRuntime, gotServices := autoPromoteRuntime(tt.services)
			if gotRuntime != tt.wantRuntime {
				t.Errorf("runtime = %q, want %q", gotRuntime, tt.wantRuntime)
			}
			if len(gotServices) != len(tt.wantServices) {
				t.Fatalf("services len = %d, want %d (%v vs %v)", len(gotServices), len(tt.wantServices), gotServices, tt.wantServices)
			}
			for i, s := range gotServices {
				if s != tt.wantServices[i] {
					t.Errorf("services[%d] = %q, want %q", i, s, tt.wantServices[i])
				}
			}
		})
	}
}

// --- Service Normalizer Tests ---

func TestNormalizeServiceName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		// Standard services
		{"postgresql@16", "PostgreSQL"},
		{"postgresql@15", "PostgreSQL"},
		{"postgresql", "PostgreSQL"},
		{"mariadb@11", "MariaDB"},
		{"mariadb", "MariaDB"},
		{"valkey@7.2", "Valkey"},
		{"valkey", "Valkey"},
		{"keydb@7", "KeyDB"},
		{"keydb", "KeyDB"},
		{"elasticsearch@8", "Elasticsearch"},
		{"object-storage", "Object Storage"},
		{"shared-storage", "Shared Storage"},
		{"kafka@3", "Kafka"},
		{"nats@2", "NATS"},
		{"meilisearch@1", "Meilisearch"},
		{"clickhouse@24", "ClickHouse"},
		{"qdrant@1", "Qdrant"},
		{"typesense@27", "Typesense"},

		// RabbitMQ
		{"rabbitmq@3.9", "RabbitMQ"},
		{"rabbitmq", "RabbitMQ"},

		// Unknown service - graceful degradation
		{"unknown-service@1", "Unknown-Service"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := normalizeServiceName(tt.input)
			if got != tt.want {
				t.Errorf("normalizeServiceName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
