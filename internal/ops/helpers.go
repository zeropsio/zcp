package ops

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

var durationRegex = regexp.MustCompile(`^(\d+)(m|h|d)$`)

// resolveServiceID resolves a service hostname to its ServiceStack.
// Requires a pre-fetched service list (avoids repeated API calls).
func resolveServiceID(services []platform.ServiceStack, hostname string) (*platform.ServiceStack, error) {
	svc := findServiceByHostname(services, hostname)
	if svc == nil {
		return nil, platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("Service '%s' not found", hostname),
			"Available services: "+listHostnames(services),
		)
	}
	return svc, nil
}

// findServiceByHostname scans a slice for matching hostname.
func findServiceByHostname(services []platform.ServiceStack, hostname string) *platform.ServiceStack {
	for i := range services {
		if services[i].Name == hostname {
			return &services[i]
		}
	}
	return nil
}

// listHostnames returns comma-separated hostnames for error messages.
func listHostnames(services []platform.ServiceStack) string {
	if len(services) == 0 {
		return "(none)"
	}
	names := make([]string, len(services))
	for i, s := range services {
		names[i] = s.Name
	}
	return strings.Join(names, ", ")
}

// parseSince converts user-friendly time strings to time.Time.
// Supports: "30m", "1h", "24h", "7d", ISO 8601 (RFC3339).
// Empty string defaults to 1 hour ago.
func parseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Now().Add(-1 * time.Hour), nil
	}

	matches := durationRegex.FindStringSubmatch(s)
	if len(matches) == 3 {
		n, err := strconv.Atoi(matches[1])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid duration number: %s", s)
		}
		switch matches[2] {
		case "m":
			if n < 1 || n > 1440 {
				return time.Time{}, fmt.Errorf("minutes must be 1-1440")
			}
			return time.Now().Add(-time.Duration(n) * time.Minute), nil
		case "h":
			if n < 1 || n > 168 {
				return time.Time{}, fmt.Errorf("hours must be 1-168")
			}
			return time.Now().Add(-time.Duration(n) * time.Hour), nil
		case "d":
			if n < 1 || n > 30 {
				return time.Time{}, fmt.Errorf("days must be 1-30")
			}
			return time.Now().Add(-time.Duration(n) * 24 * time.Hour), nil
		}
	}

	t, err := time.Parse(time.RFC3339, s)
	if err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("invalid format: %s", s)
}

// envPair holds a parsed KEY=value pair.
type envPair struct {
	Key   string
	Value string
}

// parseEnvPairs splits "KEY=value" strings into key/value pairs.
// Splits on first '=' only (value may contain '=').
func parseEnvPairs(vars []string) ([]envPair, error) {
	pairs := make([]envPair, 0, len(vars))
	for _, v := range vars {
		key, value, ok := strings.Cut(v, "=")
		if !ok {
			return nil, platform.NewPlatformError(
				platform.ErrInvalidEnvFormat,
				fmt.Sprintf("Invalid format '%s', expected KEY=value", v),
				"Format: KEY=value (split on first '=')",
			)
		}
		if key == "" {
			return nil, platform.NewPlatformError(
				platform.ErrInvalidEnvFormat,
				"Empty key in env var",
				"Format: KEY=value",
			)
		}
		pairs = append(pairs, envPair{Key: key, Value: value})
	}
	return pairs, nil
}

// crossRefPattern matches Zerops cross-service env var references like ${db_hostname}.
var crossRefPattern = regexp.MustCompile(`\$\{[a-zA-Z_][a-zA-Z0-9_]*\}`)

// filteredEnvKeys are env vars injected by the platform that should not be
// shown in discover output. Subdomain URLs come from zerops_subdomain enable.
var filteredEnvKeys = map[string]bool{
	"zeropsSubdomain": true,
}

// envVarsToMaps converts platform env vars to a slice of maps for JSON output.
// Values containing ${...} cross-service references are annotated with isReference: true.
// Platform-injected keys (zeropsSubdomain) are filtered out.
func envVarsToMaps(envs []platform.EnvVar) []map[string]any {
	result := make([]map[string]any, 0, len(envs))
	for _, e := range envs {
		if filteredEnvKeys[e.Key] {
			continue
		}
		m := map[string]any{
			"key":   e.Key,
			"value": e.Content,
		}
		if crossRefPattern.MatchString(e.Content) {
			m["isReference"] = true
		}
		result = append(result, m)
	}
	return result
}

// findEnvIDByKey finds an env var ID by key name.
func findEnvIDByKey(envs []platform.EnvVar, key string) string {
	for _, e := range envs {
		if e.Key == key {
			return e.ID
		}
	}
	return ""
}
