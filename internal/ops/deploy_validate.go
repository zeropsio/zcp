package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// ValidateZeropsYml checks zerops.yml for common issues before deploy.
// Returns a list of warning strings (empty = no issues found).
func ValidateZeropsYml(workingDir, targetHostname string) []string {
	var warnings []string

	ymlPath := filepath.Join(workingDir, "zerops.yml")
	data, err := os.ReadFile(ymlPath)
	if err != nil {
		return []string{fmt.Sprintf("zerops.yml not found at %s", ymlPath)}
	}

	var doc zeropsYmlDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return []string{fmt.Sprintf("zerops.yml invalid YAML: %v", err)}
	}

	if len(doc.Zerops) == 0 {
		return []string{"zerops.yml has no setup entries under 'zerops:' key"}
	}

	// Find matching setup entry.
	var entry *zeropsYmlEntry
	for i := range doc.Zerops {
		if doc.Zerops[i].Setup == targetHostname {
			entry = &doc.Zerops[i]
			break
		}
	}
	if entry == nil {
		warnings = append(warnings, fmt.Sprintf("no setup entry for hostname %q in zerops.yml", targetHostname))
		return warnings
	}

	if entry.Run.Start == "" {
		warnings = append(warnings, "run.start is empty — app will not start after deploy")
	}

	if len(entry.Run.Ports) == 0 {
		warnings = append(warnings, "run.ports is empty — no ports exposed, HTTP checks will fail")
	}

	if len(entry.Build.DeployFiles) == 0 {
		warnings = append(warnings, "build.deployFiles is empty — nothing will be deployed to run container")
	}

	if strings.Contains(targetHostname, "dev") && len(entry.Build.DeployFiles) > 0 {
		if !slices.Contains(entry.Build.DeployFiles, ".") {
			warnings = append(warnings, "dev service should use deployFiles: [.] — ensures source files persist across deploys for continued iteration")
		}
	}

	return warnings
}

// zeropsYmlDoc is the top-level zerops.yml structure (minimal for validation).
type zeropsYmlDoc struct {
	Zerops []zeropsYmlEntry `yaml:"zerops"`
}

type zeropsYmlEntry struct {
	Setup string         `yaml:"setup"`
	Build zeropsYmlBuild `yaml:"build"`
	Run   zeropsYmlRun   `yaml:"run"`
}

type zeropsYmlBuild struct {
	DeployFiles []string `yaml:"deployFiles"`
}

type zeropsYmlRun struct {
	Start string          `yaml:"start"`
	Ports []zeropsYmlPort `yaml:"ports"`
}

type zeropsYmlPort struct {
	Port int `yaml:"port"`
}
