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

// --- Runtime Normalizer Tests ---

func TestNormalizeRuntimeName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		// PHP variants
		{"php-nginx@8.4", "php"},
		{"php-apache@8.3", "php"},
		{"php@8.4", "php"},
		{"php", "php"},

		// Node.js ecosystem
		{"nodejs@22", "nodejs"},
		{"nodejs@20", "nodejs"},
		{"nodejs", "nodejs"},
		{"bun@1.2", "bun"},
		{"bun", "bun"},
		{"deno@2", "deno"},

		// Python
		{"python@3.12", "python"},
		{"python@3.11", "python"},
		{"python", "python"},

		// Go
		{"go@1", "go"},
		{"go", "go"},

		// Java
		{"java@21", "java"},
		{"java", "java"},

		// .NET
		{"dotnet@8", "dotnet"},
		{"dotnet", "dotnet"},

		// Rust
		{"rust@1", "rust"},
		{"rust", "rust"},

		// Elixir
		{"elixir@1", "elixir"},

		// Gleam
		{"gleam@1", "gleam"},

		// Ruby
		{"ruby@3.4", "ruby"},
		{"ruby@3.3", "ruby"},
		{"ruby", "ruby"},

		// Static
		{"static", "static"},

		// Docker
		{"docker@26", "docker"},
		{"docker", "docker"},

		// Base OS
		{"alpine", "alpine"},
		{"ubuntu", "ubuntu"},

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

// --- H3 Section Parser Tests ---

func TestParseH3Sections_Basic(t *testing.T) {
	t.Parallel()
	content := `Some intro text

### Section A
Content A.
### Section B
Content B.`
	sections := parseH3Sections(content)
	if len(sections) != 2 {
		t.Errorf("expected 2 sections, got %d", len(sections))
	}
	if sections["Section A"] != "Content A." {
		t.Errorf("Section A = %q, want %q", sections["Section A"], "Content A.")
	}
	if sections["Section B"] != "Content B." {
		t.Errorf("Section B = %q, want %q", sections["Section B"], "Content B.")
	}
}

// --- getRelevantDecisions with real embedded store ---

func TestGetRelevantDecisions_WithDecisionFiles(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}

	tests := []struct {
		name         string
		runtime      string
		services     []string
		wantContains []string
		wantEmpty    bool
	}{
		{
			name:         "postgres triggers Choose Database",
			runtime:      "nodejs@22",
			services:     []string{"postgresql@16"},
			wantContains: []string{"Choose Database"},
		},
		{
			name:         "valkey triggers Choose Cache",
			runtime:      "nodejs@22",
			services:     []string{"valkey@7.2"},
			wantContains: []string{"Choose Cache"},
		},
		{
			name:         "go triggers Choose Runtime Base",
			runtime:      "go@1",
			services:     nil,
			wantContains: []string{"Choose Runtime Base"},
		},
		{
			name:         "elasticsearch triggers Choose Search",
			runtime:      "nodejs@22",
			services:     []string{"elasticsearch@8"},
			wantContains: []string{"Choose Search"},
		},
		{
			name:         "kafka triggers Choose Queue",
			runtime:      "nodejs@22",
			services:     []string{"kafka@3"},
			wantContains: []string{"Choose Queue"},
		},
		{
			name:      "no matching services returns empty",
			runtime:   "nodejs@22",
			services:  []string{"shared-storage"},
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := store.getRelevantDecisions(tt.runtime, tt.services)
			if tt.wantEmpty {
				if result != "" {
					t.Errorf("expected empty, got: %s", result)
				}
				return
			}
			if result == "" {
				t.Fatal("expected non-empty decision hints")
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("result missing %q, got: %s", want, result)
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
