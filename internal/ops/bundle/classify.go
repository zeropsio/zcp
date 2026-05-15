package bundle

import (
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/topology"
)

// extractZeropsYAMLRunEnvRefs parses the body of zerops.yaml and
// returns the set of variable names referenced via `${...}` syntax
// inside any setup's run.envVariables map values. The set is used by
// the M2 indirect-reference detector to flag project envs that the
// classification map is about to drop while zerops.yaml still depends
// on them at runtime.
//
// Parse failures return an empty set — silent fallback is intentional;
// verifyZeropsYAMLSetup already rejects unparseable bodies upstream.
func extractZeropsYAMLRunEnvRefs(body string) map[string]bool {
	refs := map[string]bool{}
	if strings.TrimSpace(body) == "" {
		return refs
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		return refs
	}
	setups, ok := doc["zerops"].([]any)
	if !ok {
		return refs
	}
	for _, item := range setups {
		setup, ok := item.(map[string]any)
		if !ok {
			continue
		}
		run, ok := setup["run"].(map[string]any)
		if !ok {
			continue
		}
		envVars, ok := run["envVariables"].(map[string]any)
		if !ok {
			continue
		}
		for _, raw := range envVars {
			s, ok := raw.(string)
			if !ok {
				continue
			}
			for _, name := range parseDollarBraceRefs(s) {
				refs[name] = true
			}
		}
	}
	return refs
}

// parseDollarBraceRefs scans s for `${VAR_NAME}` occurrences and
// returns the unique variable names found. Empty names and unclosed
// patterns are skipped silently.
func parseDollarBraceRefs(s string) []string {
	var out []string
	seen := map[string]bool{}
	for i := 0; i < len(s); {
		idx := strings.Index(s[i:], "${")
		if idx == -1 {
			break
		}
		i += idx + 2
		end := strings.Index(s[i:], "}")
		if end == -1 {
			break
		}
		name := s[i : i+end]
		i += end + 1
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// isLikelySentinel returns true when value matches a known
// "review-required" sentinel pattern (Stripe test keys, common
// disable strings, etc.).
func isLikelySentinel(value string) bool {
	lc := strings.ToLower(strings.TrimSpace(value))
	if lc == "" {
		return false
	}
	switch lc {
	case "disabled", "none", "null", "false", "off", "n/a", "noop":
		return true
	}
	if strings.HasPrefix(lc, "sk_test_") || strings.HasPrefix(lc, "pk_test_") || strings.HasPrefix(lc, "rk_test_") {
		return true
	}
	return false
}

// detectIndirectInfraReferences walks the project envs flagged
// Infrastructure-classified and surfaces a warning for each one whose
// name appears in the zerops.yaml run.envVariables ref set.
//
// Detection only — no auto-reclassification. The agent reads the
// warning, optionally reclassifies the env as PlainConfig in the
// per-env review table, and re-runs the composer.
func detectIndirectInfraReferences(
	envs []ProjectEnvVar,
	classifications map[string]topology.SecretClassification,
	refs map[string]bool,
) []string {
	if len(refs) == 0 {
		return nil
	}
	var warns []string
	for _, env := range envs {
		if classifications[env.Key] != topology.SecretClassInfrastructure {
			continue
		}
		if !refs[env.Key] {
			continue
		}
		warns = append(warns,
			"env "+quoteEnvName(env.Key)+
				": classified Infrastructure (drops from project.envVariables) but zerops.yaml's run.envVariables references ${"+env.Key+"} — re-import will fail to resolve. "+
				"Reclassify as PlainConfig or rewrite zerops.yaml to use managed-service refs (${db_*}/${redis_*}) directly. (plan §3.4 M2)",
		)
	}
	return warns
}

// quoteEnvName wraps an env name in double quotes for warning
// readability.
func quoteEnvName(name string) string {
	return `"` + name + `"`
}
