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

	if !hasImplicitWebServer(entry.Run.Base, entry.Build.Base) {
		if entry.Run.Start == "" {
			warnings = append(warnings, "run.start is empty — app will not start after deploy")
		}

		if len(entry.Run.Ports) == 0 {
			warnings = append(warnings, "run.ports is empty — no ports exposed, HTTP checks will fail")
		}
	}

	deployFiles := entry.Build.deployFilesList()
	if len(deployFiles) == 0 {
		warnings = append(warnings, "build.deployFiles is empty — nothing will be deployed to run container")
	}

	if strings.Contains(targetHostname, "dev") && len(deployFiles) > 0 {
		if !slices.Contains(deployFiles, ".") && !slices.Contains(deployFiles, "./") {
			warnings = append(warnings, "dev service should use deployFiles: [.] — ensures source files persist across deploys for continued iteration")
		}
	}

	if strings.Contains(targetHostname, "dev") && entry.Run.HealthCheck != nil {
		warnings = append(warnings, fmt.Sprintf(
			"setup %q: dev service has run.healthCheck — this causes unwanted container restarts during iteration. Remove healthCheck from dev entries (keep it on stage only).",
			entry.Setup))
	}
	if strings.Contains(targetHostname, "dev") && entry.Deploy.ReadinessCheck != nil {
		warnings = append(warnings, fmt.Sprintf(
			"setup %q: dev service has deploy.readinessCheck — unnecessary for dev (agent verifies manually). Remove readinessCheck from dev entries.",
			entry.Setup))
	}

	return warnings
}

// zeropsYmlDoc is the top-level zerops.yml structure (minimal for validation).
type zeropsYmlDoc struct {
	Zerops []zeropsYmlEntry `yaml:"zerops"`
}

type zeropsYmlEntry struct {
	Setup  string          `yaml:"setup"`
	Build  zeropsYmlBuild  `yaml:"build"`
	Deploy zeropsYmlDeploy `yaml:"deploy"`
	Run    zeropsYmlRun    `yaml:"run"`
}

type zeropsYmlDeploy struct {
	ReadinessCheck any `yaml:"readinessCheck"`
}

type zeropsYmlBuild struct {
	Base        string `yaml:"base"`
	DeployFiles any    `yaml:"deployFiles"` // string or []string — Zerops accepts both
}

// deployFilesList normalizes DeployFiles to []string regardless of YAML format.
func (b zeropsYmlBuild) deployFilesList() []string {
	switch v := b.DeployFiles.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

type zeropsYmlRun struct {
	Base        string          `yaml:"base"`
	Start       string          `yaml:"start"`
	Ports       []zeropsYmlPort `yaml:"ports"`
	HealthCheck any             `yaml:"healthCheck"`
}

type zeropsYmlPort struct {
	Port int `yaml:"port"`
}

// hasImplicitWebServer returns true if the runtime has a built-in web server
// that starts automatically (no run.start or run.ports needed).
// Checks run.base first, falls back to build.base.
func hasImplicitWebServer(runBase, buildBase string) bool {
	for _, base := range []string{runBase, buildBase} {
		if base == "" {
			continue
		}
		if base == "static" {
			return true
		}
		b, _, _ := strings.Cut(base, "@")
		switch b {
		case "php-apache", "php-nginx", "nginx", "static":
			return true
		}
	}
	return false
}
