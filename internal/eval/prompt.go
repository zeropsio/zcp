package eval

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseRecipeMetadata extracts structured metadata from a recipe's markdown content.
func ParseRecipeMetadata(name, content string) (*RecipeMetadata, error) {
	title := extractMarkdownTitle(content)
	if title == "" {
		return nil, fmt.Errorf("parse recipe %q: missing H1 title", name)
	}

	runtime, services := extractFromImportYml(content)
	if runtime == "" {
		runtime = extractRuntimeFromZeropsYml(content)
	}

	return &RecipeMetadata{
		Name:     name,
		Title:    title,
		Runtime:  runtime,
		Services: services,
	}, nil
}

// extractMarkdownTitle returns the H1 title from markdown content.
func extractMarkdownTitle(content string) string {
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(trimmed, "# "); ok {
			return rest
		}
	}
	return ""
}

// importYmlRoot mirrors the import.yml structure for YAML parsing.
type importYmlRoot struct {
	Services []importYmlService `yaml:"services"`
}

type importYmlService struct {
	Hostname string `yaml:"hostname"`
	Type     string `yaml:"type"`
}

// runtimeTypes are service types that represent the application runtime (not managed services).
var runtimeTypes = map[string]bool{
	"php-nginx":  true,
	"php-apache": true,
	"php":        true,
	"nodejs":     true,
	"bun":        true,
	"python":     true,
	"go":         true,
	"java":       true,
	"dotnet":     true,
	"ruby":       true,
	"rust":       true,
	"elixir":     true,
	"gleam":      true,
	"deno":       true,
	"static":     true,
}

// extractFromImportYml parses import.yml sections to find runtime type and managed services.
// Returns the runtime type and a list of non-runtime service definitions.
func extractFromImportYml(content string) (string, []ServiceDef) {
	blocks := findYAMLBlocksInSections(content, "import.yml")
	if len(blocks) == 0 {
		return "", nil
	}

	// Use the first import.yml block (Full variant if present)
	block := blocks[0]
	var root importYmlRoot
	if err := yaml.Unmarshal([]byte(block), &root); err != nil {
		return "", nil
	}

	var runtime string
	var services []ServiceDef

	for _, svc := range root.Services {
		baseType, _, _ := strings.Cut(svc.Type, "@")
		if runtimeTypes[baseType] {
			if runtime == "" {
				runtime = svc.Type
			}
			continue
		}
		role := inferRole(svc.Type, svc.Hostname)
		services = append(services, ServiceDef{Type: svc.Type, Role: role})
	}

	return runtime, services
}

// zeropsYmlRoot for parsing zerops.yml to extract runtime base.
type zeropsYmlRoot struct {
	Zerops []zeropsYmlEntry `yaml:"zerops"`
}

type zeropsYmlEntry struct {
	Build *zeropsYmlBuild `yaml:"build,omitempty"`
	Run   *zeropsYmlRun   `yaml:"run,omitempty"`
}

type zeropsYmlBuild struct {
	Base any `yaml:"base"`
}

type zeropsYmlRun struct {
	Base string `yaml:"base"`
}

// extractRuntimeFromZeropsYml gets the runtime base from zerops.yml when import.yml isn't available.
func extractRuntimeFromZeropsYml(content string) string {
	blocks := findYAMLBlocksInSections(content, "zerops.yml")
	if len(blocks) == 0 {
		return ""
	}

	var root zeropsYmlRoot
	if err := yaml.Unmarshal([]byte(blocks[0]), &root); err != nil || len(root.Zerops) == 0 {
		return ""
	}

	entry := root.Zerops[0]
	if entry.Run != nil && entry.Run.Base != "" {
		return entry.Run.Base
	}
	if entry.Build != nil {
		if s, ok := entry.Build.Base.(string); ok {
			return s
		}
	}
	return ""
}

// inferRole guesses the role name from service type and hostname.
func inferRole(svcType, hostname string) string {
	baseType, _, _ := strings.Cut(svcType, "@")
	switch baseType {
	case "postgresql", "mariadb":
		return "db"
	case "valkey", "keydb":
		return "cache"
	case "object-storage":
		return "storage"
	case "shared-storage":
		return "sharedstorage"
	case "elasticsearch", "meilisearch", "typesense", "qdrant":
		return "search"
	case "kafka", "nats":
		return "queue"
	case "clickhouse":
		return "analytics"
	}
	if hostname != "" {
		return hostname
	}
	return baseType
}

// RecipeShortName produces a short (1-4 char) abbreviation for hostname generation.
func RecipeShortName(recipe string) string {
	// Split by hyphens: "nextjs-ssr" → ["nextjs", "ssr"]
	parts := strings.Split(recipe, "-")

	switch len(parts) {
	case 1:
		// Single word: take first 2 chars if long enough
		if len(parts[0]) <= 2 {
			return parts[0]
		}
		return parts[0][:1] + parts[0][len(parts[0])-1:]
	default:
		// Multi-word: first char of each part
		var short strings.Builder
		for _, p := range parts {
			if len(p) > 0 {
				short.WriteByte(p[0])
			}
		}
		s := short.String()
		if len(s) > 4 {
			return s[:4]
		}
		return s
	}
}

// GenerateHostname creates a valid Zerops hostname for an eval service.
// Format: ev{recipe_short}{random_hex}, max 25 chars, lowercase alphanumeric only.
func GenerateHostname(recipe, role string) string {
	short := RecipeShortName(recipe)
	prefix := "ev" + short

	// Random suffix: 6 hex chars
	randBytes := make([]byte, 3)
	if _, err := rand.Read(randBytes); err != nil {
		// Fallback to timestamp-based suffix
		return prefix + "000000"
	}
	suffix := hex.EncodeToString(randBytes)

	hostname := prefix + suffix
	if len(hostname) > 25 {
		hostname = hostname[:25]
	}
	return hostname
}

// GenerateHostnames creates hostnames for all services in a recipe.
// Returns map[role]hostname (e.g., "runtime" → "evgo1a2b3c", "db" → "evgo4d5e6f").
func GenerateHostnames(meta *RecipeMetadata) map[string]string {
	hostnames := make(map[string]string)
	hostnames["runtime"] = GenerateHostname(meta.Name, "runtime")

	for _, svc := range meta.Services {
		hostnames[svc.Role] = GenerateHostname(meta.Name, svc.Role)
	}

	return hostnames
}

// BuildTaskPrompt generates the task portion of the eval prompt (what to deploy).
func BuildTaskPrompt(meta *RecipeMetadata, hostnames map[string]string) string {
	var b strings.Builder

	// Extract framework name from title (remove " on Zerops" suffix)
	framework := meta.Title
	if idx := strings.Index(framework, " on Zerops"); idx > 0 {
		framework = framework[:idx]
	}

	fmt.Fprintf(&b, "Deploy a %s application on Zerops.\n\n", framework)
	b.WriteString("Use these hostnames:\n")
	fmt.Fprintf(&b, "- %s as runtime (%s)\n", hostnames["runtime"], meta.Runtime)

	for _, svc := range meta.Services {
		fmt.Fprintf(&b, "- %s as %s (%s)\n", hostnames[svc.Role], svc.Role, svc.Type)
	}

	b.WriteString("\nVerify the runtime is working — the app should respond with HTTP 200.\n")
	b.WriteString("Do NOT use tools outside of zerops_* MCP tools.")

	return b.String()
}

// assessmentInstructions is the self-assessment prompt appended after the task.
const assessmentInstructions = `
---

AFTER completing the deployment (or when you cannot proceed further), write a structured
evaluation report. You are providing feedback on the ZEROPS MCP TOOLS AND KNOWLEDGE BASE
that assisted you. This report will be used to improve the MCP server so that future LLM
agents can deploy more successfully.

Start your report with "## EVAL REPORT" and follow this EXACT structure:

### Deployment outcome
State: SUCCESS / PARTIAL (what worked, what didn't) / FAILURE (at which step)

### Workflow execution
- Steps completed: [list of step names completed successfully]
- Steps skipped: [list of steps skipped and why]
- Iterations: [number of iterate cycles needed]
- Gate failures: [list of gate failures encountered]
- Strategy chosen: [strategy per service, or "none" if step was skipped]

### Failure chains
For EACH problem you encountered, trace the full cause chain:
- **Step**: Which bootstrap/workflow step were you in?
- **What you received**: Quote or summarize the specific tool response or knowledge content
  that contributed to the problem (include the tool name and key input parameters)
- **What you did with it**: How you used that information
- **What went wrong**: The concrete error or unexpected behavior
- **How you recovered**: What you did to fix it (or couldn't)
- **Root cause**: One of: WRONG_KNOWLEDGE (info was factually incorrect),
  MISSING_KNOWLEDGE (info wasn't available), UNCLEAR_GUIDANCE (info existed but
  was ambiguous/confusing), PLATFORM_ISSUE (Zerops itself, not the MCP)

If you had no problems, write "No failure chains."

### Information gaps
List each point where you needed information but couldn't find it through any
zerops_* tool. For each:
- What you were trying to do
- What query/tool you tried
- What you had to guess or figure out on your own
- What the knowledge base SHOULD contain to prevent this

### Wasted steps
List tool calls you made that turned out to be unnecessary or had to be repeated
because of wrong/missing information. Include the tool name and why it was wasted.
Total wasted tool calls: [number]

### What worked well
List specific knowledge docs or tool responses that led to correct actions on
the first try. Reference them by tool name and query/mode used.

DO NOT include:
- Apologies or self-blame for your own mistakes
- Comments about platform speed, timeouts, or infrastructure issues
- Generic suggestions without referencing a specific tool response or knowledge doc
- Speculation about what "might" help — only report what you actually experienced`

// BuildFullPrompt combines task + self-assessment into the complete eval prompt.
func BuildFullPrompt(meta *RecipeMetadata, hostnames map[string]string) string {
	task := BuildTaskPrompt(meta, hostnames)
	return task + "\n" + assessmentInstructions
}
