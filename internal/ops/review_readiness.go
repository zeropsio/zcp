package ops

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ReviewStageSetup gives a public stage the same readiness path its external
// probe uses. Until it succeeds the platform keeps the old version serving.
func ReviewStageSetup(body, setup string, port int, public bool) (string, string, error) {
	var raw struct {
		Zerops []map[string]any `yaml:"zerops"`
		Other  map[string]any   `yaml:",inline"`
	}
	if err := yaml.Unmarshal([]byte(body), &raw); err != nil {
		return "", "", fmt.Errorf("read stage setup: %w", err)
	}
	resolved := resolveExtends(raw.Zerops)
	for i, entry := range resolved {
		if entry["setup"] != setup {
			continue
		}
		if !public {
			// Worker readiness belongs to the application; preserve it and
			// its inherited settings, while keeping its old version alive.
			entry = raw.Zerops[i]
			deploy, _ := entry["deploy"].(map[string]any)
			if deploy == nil {
				deploy = make(map[string]any)
			}
			deploy["temporaryShutdown"] = false
			entry["deploy"] = deploy
			raw.Zerops[i] = entry
			encoded, err := yaml.Marshal(raw)
			if err != nil {
				return "", "", fmt.Errorf("write stage setup: %w", err)
			}
			return string(encoded), "", nil
		}
		if port <= 0 {
			return "", "", fmt.Errorf("setup %s has no HTTP port for its readiness check", setup)
		}
		deploy, _ := entry["deploy"].(map[string]any)
		if deploy == nil {
			deploy = make(map[string]any)
		}
		check, _ := deploy["readinessCheck"].(map[string]any)
		if check == nil {
			check = make(map[string]any)
		}
		get, _ := check["httpGet"].(map[string]any)
		if get == nil {
			run, _ := entry["run"].(map[string]any)
			health, _ := run["healthCheck"].(map[string]any)
			get, _ = health["httpGet"].(map[string]any)
		}
		path, _ := get["path"].(string)
		path = strings.TrimSpace(path)
		if path == "" {
			path = "/"
		}
		check["httpGet"] = map[string]any{"path": path, "port": port}
		delete(check, "exec")
		deploy["readinessCheck"] = check
		deploy["temporaryShutdown"] = false
		entry["deploy"] = deploy
		raw.Zerops[i] = entry
		encoded, err := yaml.Marshal(raw)
		if err != nil {
			return "", "", fmt.Errorf("write stage setup: %w", err)
		}
		return string(encoded), path, nil
	}
	return "", "", fmt.Errorf("setup %s is not in the version's zerops.yaml", setup)
}
