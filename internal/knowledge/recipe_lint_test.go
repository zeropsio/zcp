package knowledge

// Tests for: recipe structural integrity, YAML validity, and content correctness.
//
// Validates every embedded recipe against structural rules (Keywords, TL;DR,
// zerops.yml, Gotchas), YAML parsing rules, and content pattern rules.
//
// Run: go test ./internal/knowledge/ -run TestRecipeLint -v

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// knownValidVersions contains base@version strings known to be valid on Zerops.
// Derived from zerops-docs and platform catalog. Updated when versions change.
// E2E tests (Phase3) verify these against the live platform.
var knownValidVersions = map[string]bool{
	// PHP
	"php@8.4": true, "php@8.3": true, "php@8.2": true,
	"php-apache@8.4": true, "php-apache@8.3": true, "php-apache@8.2": true,
	"php-nginx@8.4": true, "php-nginx@8.3": true, "php-nginx@8.2": true,
	// Node.js
	"nodejs@22": true, "nodejs@20": true, "nodejs@18": true,
	// Bun
	"bun@1.2": true, "bun@1": true,
	// Python
	"python@3.13": true, "python@3.12": true, "python@3.11": true,
	// Go
	"go@1": true,
	// Java
	"java@21": true, "java@17": true,
	// .NET
	"dotnet@9": true, "dotnet@8": true, "dotnet@6": true,
	// Ruby
	"ruby@3.4": true, "ruby@3.3": true,
	// Rust
	"rust@latest": true, "rust@stable": true, "rust@nightly": true, "rust@1": true,
	// Elixir
	"elixir@1.18": true, "elixir@1.17": true, "elixir@1.16": true,
	// Gleam
	"gleam@latest": true, "gleam@1.5": true, "gleam@1": true,
	// Deno
	"deno@latest": true, "deno@2": true,
	// Static / OS
	"static": true, "alpine@latest": true, "ubuntu@latest": true,
	// Databases & managed services
	"postgresql@18": true, "postgresql@17": true, "postgresql@16": true, "postgresql@14": true,
	"mariadb@10.6":      true,
	"valkey@7.2":        true,
	"keydb@6":           true,
	"elasticsearch@9.2": true, "elasticsearch@8.16": true,
	"kafka@3.8": true,
	"nats@2.12": true, "nats@2.10": true,
	"meilisearch@1.20": true, "meilisearch@1.10": true,
	"clickhouse@25.3": true,
	"qdrant@1.12":     true, "qdrant@1.10": true,
	"typesense@27.1": true,
	// Special
	"object-storage": true, "shared-storage": true,
}

// managedServiceTypes are service types that require mode: HA or mode: NON_HA.
var managedServiceTypes = map[string]bool{
	"postgresql": true, "mariadb": true, "valkey": true, "keydb": true,
	"elasticsearch": true, "kafka": true, "nats": true, "meilisearch": true,
	"clickhouse": true, "qdrant": true, "typesense": true,
}

func TestRecipeLint(t *testing.T) {
	t.Parallel()

	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("load store: %v", err)
	}

	recipes := store.ListRecipes()
	if len(recipes) == 0 {
		t.Fatal("no recipes found")
	}

	for _, name := range recipes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			doc, err := store.Get("zerops://recipes/" + name)
			if err != nil {
				t.Fatalf("get recipe: %v", err)
			}
			content := doc.Content

			// --- Structural checks ---

			t.Run("HasTitle", func(t *testing.T) {
				if doc.Title == "" {
					t.Error("missing H1 title")
				}
			})

			t.Run("HasKeywords", func(t *testing.T) {
				if len(doc.Keywords) < 3 {
					t.Errorf("want >= 3 keywords, got %d: %v", len(doc.Keywords), doc.Keywords)
				}
			})

			t.Run("HasTLDR", func(t *testing.T) {
				if doc.TLDR == "" {
					t.Error("missing ## TL;DR section")
				}
			})

			t.Run("HasZeropsYml", func(t *testing.T) {
				blocks := findYAMLBlocksInSections(content, "zerops.yml")
				if len(blocks) == 0 {
					t.Error("no YAML code block found in zerops.yml section")
				}
			})

			t.Run("HasGotchas", func(t *testing.T) {
				sections := parseRecipeSections(content)
				gotchas := findSectionByPrefix(sections, "Gotchas")
				if gotchas == "" {
					t.Error("missing ## Gotchas section")
					return
				}
				if !strings.Contains(gotchas, "- ") {
					t.Error("Gotchas section has no bullet points")
				}
			})

			t.Run("ImportYmlHasContent", func(t *testing.T) {
				if hasH2Section(content, "import.yml") {
					blocks := findYAMLBlocksInSections(content, "import.yml")
					if len(blocks) == 0 {
						t.Error("import.yml section exists but has no YAML code block")
					}
				}
			})

			// --- YAML parsing: zerops.yml ---

			zeropsBlocks := findYAMLBlocksInSections(content, "zerops.yml")
			for i, block := range zeropsBlocks {
				t.Run(fmt.Sprintf("ZeropsYml/%d", i), func(t *testing.T) {
					validateZeropsYml(t, block)
				})
			}

			// --- YAML parsing: import.yml ---

			if hasH2Section(content, "import.yml") {
				importBlocks := findYAMLBlocksInSections(content, "import.yml")
				for i, block := range importBlocks {
					t.Run(fmt.Sprintf("ImportYml/%d", i), func(t *testing.T) {
						// Get the raw section content (including preprocessor comment)
						rawSection := findRawSectionContent(content, "import.yml")
						validateImportYml(t, block, rawSection)
					})
				}
			}

			// --- Content pattern checks ---

			t.Run("NoStaleURIs", func(t *testing.T) {
				if strings.Contains(content, "zerops://foundation/") {
					t.Error("stale zerops://foundation/ URI found (replaced by zerops://themes/)")
				}
			})

			t.Run("NoPreprocessorInZeropsYml", func(t *testing.T) {
				for _, block := range zeropsBlocks {
					if strings.Contains(block, "<@") {
						t.Error("<@...> preprocessor function in zerops.yml (only valid in import.yml)")
					}
				}
			})

			t.Run("NoStaleRecipeRefs", func(t *testing.T) {
				staleRefs := []string{"recipe-react-static"}
				for _, ref := range staleRefs {
					if strings.Contains(content, ref) {
						t.Errorf("reference to non-existent recipe %q", ref)
					}
				}
			})

			t.Run("VersionsKnown", func(t *testing.T) {
				versions := extractVersionRefs(content)
				for _, v := range versions {
					if !knownValidVersions[v] {
						t.Errorf("unknown version %q — verify against platform catalog", v)
					}
				}
			})
		})
	}
}

// --- zerops.yml validation ---

// zeropsYmlRoot represents the top-level zerops.yml structure.
type zeropsYmlRoot struct {
	Zerops []zeropsYmlEntry `yaml:"zerops"`
}

type zeropsYmlEntry struct {
	Setup string          `yaml:"setup"`
	Build *zeropsYmlBuild `yaml:"build,omitempty"`
	Run   *zeropsYmlRun   `yaml:"run,omitempty"`
}

type zeropsYmlBuild struct {
	Base        any `yaml:"base"`        // string or []string
	DeployFiles any `yaml:"deployFiles"` // string, []string, or nil
}

type zeropsYmlRun struct {
	Base          string          `yaml:"base"`
	Start         string          `yaml:"start"`
	StartCommands any             `yaml:"startCommands"`
	Ports         []zeropsYmlPort `yaml:"ports"`
}

type zeropsYmlPort struct {
	Port     int    `yaml:"port"`
	Protocol string `yaml:"protocol"`
}

func validateZeropsYml(t *testing.T, block string) {
	t.Helper()

	var root zeropsYmlRoot
	if err := yaml.Unmarshal([]byte(block), &root); err != nil {
		t.Fatalf("invalid YAML: %v", err)
	}

	if len(root.Zerops) == 0 {
		t.Fatal("zerops.yml has no entries under 'zerops' key")
	}

	for i, entry := range root.Zerops {
		if entry.Setup == "" {
			t.Errorf("entry[%d]: missing 'setup' field", i)
		}

		if entry.Build != nil {
			if entry.Build.Base == nil {
				t.Errorf("entry[%d]: build exists but missing 'base'", i)
			}
		}

		if entry.Run != nil {
			// Check ports protocol
			for j, p := range entry.Run.Ports {
				if p.Protocol != "" && p.Protocol != "TCP" && p.Protocol != "UDP" {
					t.Errorf("entry[%d].run.ports[%d]: protocol %q not TCP or UDP", i, j, p.Protocol)
				}
			}

			// Check start exists for non-static/non-php bases
			runBase := entry.Run.Base
			if runBase == "" && entry.Build != nil {
				// Inherit from build base
				if s, ok := entry.Build.Base.(string); ok {
					runBase = s
				}
			}
			needsStart := !isImplicitStartBase(runBase)
			if needsStart && entry.Run.Start == "" && entry.Run.StartCommands == nil {
				t.Errorf("entry[%d]: run exists without 'start' (base=%q requires explicit start)", i, runBase)
			}
		}
	}
}

// isImplicitStartBase returns true if the base type has an implicit start
// command (static sites, PHP with Apache/Nginx).
func isImplicitStartBase(base string) bool {
	if base == "" || base == "static" {
		return true
	}
	b, _, _ := strings.Cut(base, "@")
	return b == "php-apache" || b == "php-nginx" || b == "static"
}

// --- import.yml validation ---

type importYmlRoot struct {
	Services []importYmlService `yaml:"services"`
}

type importYmlService struct {
	Hostname string `yaml:"hostname"`
	Type     string `yaml:"type"`
	Mode     string `yaml:"mode"`
}

func validateImportYml(t *testing.T, block, rawSection string) {
	t.Helper()

	var root importYmlRoot
	if err := yaml.Unmarshal([]byte(block), &root); err != nil {
		t.Fatalf("invalid YAML: %v", err)
	}

	if len(root.Services) == 0 {
		t.Fatal("import.yml has no services")
	}

	for i, svc := range root.Services {
		if svc.Hostname == "" {
			t.Errorf("service[%d]: missing 'hostname'", i)
		}
		if svc.Type == "" {
			t.Errorf("service[%d] (%s): missing 'type'", i, svc.Hostname)
		}

		// Check managed services have mode
		baseType, _, _ := strings.Cut(svc.Type, "@")
		if managedServiceTypes[baseType] {
			if svc.Mode != "HA" && svc.Mode != "NON_HA" {
				t.Errorf("service[%d] (%s): managed type %q requires mode HA or NON_HA, got %q",
					i, svc.Hostname, svc.Type, svc.Mode)
			}
		}
	}

	// Check preprocessor directive
	if strings.Contains(block, "<@") || strings.Contains(rawSection, "<@") {
		firstLine := strings.TrimSpace(strings.SplitN(rawSection, "\n", 2)[0])
		// Look for the preprocessor directive in the raw YAML content
		if !containsPreprocessorDirective(rawSection) {
			t.Error("import.yml uses <@...> but missing #yamlPreprocessor=on directive")
		}
		if strings.Contains(rawSection, "#zeropsPreprocessor=on") {
			t.Error("import.yml uses #zeropsPreprocessor=on (wrong — use #yamlPreprocessor=on)")
		}
		_ = firstLine // used above indirectly
	}
}

func containsPreprocessorDirective(section string) bool {
	for line := range strings.SplitSeq(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "#yamlPreprocessor=on" {
			return true
		}
	}
	return false
}

// --- Markdown parsing helpers ---

// parseRecipeSections extracts H2 sections from markdown as title→content map.
func parseRecipeSections(content string) map[string]string {
	sections := make(map[string]string)
	var currentTitle string
	var currentContent strings.Builder
	inCodeBlock := false

	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			if currentTitle != "" {
				currentContent.WriteString(line + "\n")
			}
			continue
		}

		if !inCodeBlock && strings.HasPrefix(trimmed, "## ") {
			if currentTitle != "" {
				sections[currentTitle] = currentContent.String()
			}
			currentTitle = strings.TrimPrefix(trimmed, "## ")
			currentContent.Reset()
			continue
		}

		if currentTitle != "" {
			currentContent.WriteString(line + "\n")
		}
	}

	if currentTitle != "" {
		sections[currentTitle] = currentContent.String()
	}

	return sections
}

// findSectionByPrefix returns the content of the first H2 section whose title contains prefix.
func findSectionByPrefix(sections map[string]string, prefix string) string {
	for title, content := range sections {
		if strings.Contains(title, prefix) {
			return content
		}
	}
	return ""
}

// hasH2Section checks if content has an H2 section whose title contains the given substring.
func hasH2Section(content, substr string) bool {
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") && strings.Contains(trimmed, substr) {
			return true
		}
	}
	return false
}

// findYAMLBlocksInSections extracts YAML code blocks from H2 sections matching sectionSubstr.
func findYAMLBlocksInSections(content, sectionSubstr string) []string {
	sections := parseRecipeSections(content)
	var blocks []string
	for title, body := range sections {
		if strings.Contains(title, sectionSubstr) {
			blocks = append(blocks, extractYAMLBlocks(body)...)
		}
	}
	return blocks
}

// findRawSectionContent returns the raw text under the first H2 section matching substr.
// Includes the YAML code block and any text before/after it (like preprocessor comments).
func findRawSectionContent(content, substr string) string {
	sections := parseRecipeSections(content)
	for title, body := range sections {
		if strings.Contains(title, substr) {
			return body
		}
	}
	return ""
}

// extractYAMLBlocks extracts content from ```yaml ... ``` fenced code blocks.
func extractYAMLBlocks(section string) []string {
	var blocks []string
	var current strings.Builder
	inBlock := false

	for line := range strings.SplitSeq(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if !inBlock && (trimmed == "```yaml" || trimmed == "```yml") {
			inBlock = true
			continue
		}
		if inBlock && trimmed == "```" {
			blocks = append(blocks, current.String())
			current.Reset()
			inBlock = false
			continue
		}
		if inBlock {
			current.WriteString(line + "\n")
		}
	}

	return blocks
}

// extractVersionRefs extracts version references (type: X@Y, base: X@Y) from recipe content.
var versionRefPattern = regexp.MustCompile(`(?:type|base):\s*(\S+@\S+)`)
var versionRefArrayPattern = regexp.MustCompile(`(?:type|base):\s*\[([^\]]+)\]`)

func extractVersionRefs(content string) []string {
	seen := make(map[string]bool)
	var refs []string

	// First, extract array values: base: [php@8.3, nodejs@18]
	// Process these first and mark positions to skip for single-value matching.
	for _, match := range versionRefArrayPattern.FindAllStringSubmatch(content, -1) {
		for part := range strings.SplitSeq(match[1], ",") {
			v := strings.TrimSpace(part)
			if v != "" && !seen[v] {
				seen[v] = true
				refs = append(refs, v)
			}
		}
	}

	// Match single values: type: nodejs@20, base: php@8.3
	// Skip values that start with [ (those are array values handled above).
	for _, match := range versionRefPattern.FindAllStringSubmatch(content, -1) {
		v := strings.TrimRight(match[1], ",]")
		if strings.HasPrefix(v, "[") {
			continue // Skip array values
		}
		if v != "" && !seen[v] {
			seen[v] = true
			refs = append(refs, v)
		}
	}

	return refs
}
