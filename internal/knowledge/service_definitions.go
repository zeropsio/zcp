package knowledge

import (
	"strings"
)

// ServiceDefinitions holds per-recipe import YAML blocks extracted from battle-tested
// recipe environments. These provide proven scaling values for composing import.yaml.
type ServiceDefinitions struct {
	DevStageImport  string // Full import YAML from "AI Agent" environment (env0)
	SmallProdImport string // Full import YAML from "Small Production" environment (env4)
}

// parseServiceDefinitions extracts service definitions from the ## Service Definitions
// section of a recipe document. Returns nil if no section exists.
func parseServiceDefinitions(content string) *ServiceDefinitions {
	sections := parseH2Sections(content)
	defBlock, ok := sections["Service Definitions"]
	if !ok || defBlock == "" {
		return nil
	}

	h3 := parseH3Sections(defBlock)

	var defs ServiceDefinitions
	for title, body := range h3 {
		yamlBlocks := extractYAMLFromSection(body)
		if len(yamlBlocks) == 0 {
			continue
		}
		yaml := yamlBlocks[0]
		titleLower := strings.ToLower(title)
		switch {
		case strings.Contains(titleLower, "dev") || strings.Contains(titleLower, "stage") || strings.Contains(titleLower, "agent"):
			defs.DevStageImport = yaml
		case strings.Contains(titleLower, "prod"):
			defs.SmallProdImport = yaml
		}
	}

	if defs.DevStageImport == "" && defs.SmallProdImport == "" {
		return nil
	}
	return &defs
}

// extractYAMLFromSection extracts YAML code blocks from markdown content.
func extractYAMLFromSection(section string) []string {
	var blocks []string
	var current strings.Builder
	inBlock := false

	for _, line := range strings.Split(section, "\n") {
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

// GetServiceDefinitions returns parsed service definitions for a recipe.
// Returns nil if the recipe has no Service Definitions section.
func (s *Store) GetServiceDefinitions(name string) *ServiceDefinitions {
	doc, err := s.Get("zerops://recipes/" + name)
	if err != nil {
		return nil
	}
	return parseServiceDefinitions(doc.Content)
}

// TransformForBootstrap adapts a recipe import YAML for bootstrap use.
// Removes buildFromGit and zeropsSetup (bootstrap uses SSHFS + zcli push),
// adds startWithoutCode: true for dev services (bootstrap dev containers start empty),
// keeps enableSubdomainAccess, verticalAutoscaling, minContainers, and other scaling values.
func TransformForBootstrap(importYAML string) string {
	lines := strings.Split(importYAML, "\n")

	// First pass: identify dev services (zeropsSetup: dev) by their entry start line.
	devEntries := make(map[int]bool) // line index of "- hostname:" for dev entries
	currentEntryStart := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- hostname:") {
			currentEntryStart = i
		}
		if strings.HasPrefix(trimmed, "zeropsSetup:") && currentEntryStart >= 0 {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "zeropsSetup:"))
			if val == "dev" {
				devEntries[currentEntryStart] = true
			}
		}
	}

	// Second pass: filter lines and inject startWithoutCode for dev services.
	var sb strings.Builder
	currentEntryStart = -1
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- hostname:") {
			currentEntryStart = sb.Len() // track position, not line index
		}
		if strings.HasPrefix(trimmed, "buildFromGit:") {
			continue
		}
		if strings.HasPrefix(trimmed, "zeropsSetup:") {
			continue
		}
		// Remove enableSubdomainAccess — with startWithoutCode there's no app
		// listening on any port, so subdomain access points at nothing.
		// Developer adds it when they push a working app via zcli push.
		if strings.HasPrefix(trimmed, "enableSubdomainAccess:") {
			continue
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	result := strings.TrimRight(sb.String(), "\n")

	// Third pass: inject startWithoutCode: true after each dev service's hostname line.
	// We do this on the filtered output to get clean indentation.
	if len(devEntries) > 0 {
		outLines := strings.Split(result, "\n")
		var final strings.Builder
		for _, line := range outLines {
			final.WriteString(line)
			final.WriteByte('\n')
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "- hostname:") {
				// Check if this hostname was a dev entry
				hostname := strings.TrimSpace(strings.TrimPrefix(trimmed, "- hostname:"))
				if strings.HasSuffix(hostname, "dev") {
					indent := len(line) - len(strings.TrimLeft(line, " ")) + 2 // align with other fields
					final.WriteString(strings.Repeat(" ", indent))
					final.WriteString("startWithoutCode: true\n")
				}
			}
		}
		result = strings.TrimRight(final.String(), "\n")
	}

	return result
}

// extractServiceEntries splits an import YAML into runtime (USER) and managed (STANDARD)
// service entries based on known managed service type prefixes.
func extractServiceEntries(importYAML string) (runtime []string, managed []string) {
	lines := strings.Split(importYAML, "\n")
	var currentEntry strings.Builder
	var currentType string
	inServices := false
	entryIndent := -1

	flush := func() {
		entry := strings.TrimRight(currentEntry.String(), "\n")
		if entry == "" {
			return
		}
		base, _, _ := strings.Cut(currentType, "@")
		if isManagedServiceType(base) {
			managed = append(managed, entry)
		} else {
			runtime = append(runtime, entry)
		}
		currentEntry.Reset()
		currentType = ""
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect services: block
		if trimmed == "services:" {
			inServices = true
			continue
		}
		if !inServices {
			continue
		}

		// Detect new service entry (- hostname: ...)
		if strings.HasPrefix(trimmed, "- hostname:") || strings.HasPrefix(trimmed, "- type:") {
			flush()
			entryIndent = len(line) - len(strings.TrimLeft(line, " "))
			currentEntry.WriteString(line + "\n")
			if strings.HasPrefix(trimmed, "- type:") {
				currentType = strings.TrimSpace(strings.TrimPrefix(trimmed, "- type:"))
			}
			continue
		}

		// Check if this is a continuation of the current entry
		if entryIndent >= 0 && currentEntry.Len() > 0 {
			lineIndent := len(line) - len(strings.TrimLeft(line, " "))
			if trimmed == "" || lineIndent > entryIndent {
				currentEntry.WriteString(line + "\n")
				// Capture type if on a continuation line
				if strings.HasPrefix(trimmed, "type:") {
					currentType = strings.TrimSpace(strings.TrimPrefix(trimmed, "type:"))
				}
				continue
			}
		}

		// Non-service line (project:, etc.) — reset
		if entryIndent >= 0 {
			flush()
			entryIndent = -1
		}
	}
	flush()
	return runtime, managed
}

// isManagedServiceType returns true for service types that are managed (STANDARD category).
func isManagedServiceType(base string) bool {
	switch base {
	case "postgresql", "mariadb", "valkey", "keydb", "elasticsearch",
		"kafka", "nats", "meilisearch", "clickhouse", "qdrant",
		"typesense", "rabbitmq", "object-storage", "shared-storage":
		return true
	}
	return false
}
