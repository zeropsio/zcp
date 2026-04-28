package ops

import (
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/topology"
)

// extractZeropsYAMLRunEnvRefs parses the body of /var/www/zerops.yaml
// and returns the set of variable names referenced via `${...}` syntax
// inside any setup's `run.envVariables` map values. The set is used by
// the M2 indirect-reference detector to flag project envs that the
// classification map is about to drop while zerops.yaml still depends
// on them at runtime.
//
// Parse failures return an empty set — silent fallback is intentional;
// the verifyZeropsYAMLSetup gate already rejects unparseable bodies
// before composeImportYAML runs.
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
// patterns are skipped silently. Nested braces are not yaml-legal
// inside a `${...}` ref, so the inner `}` always terminates the name.
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
// "review-required" sentinel pattern surfaced by Codex Agent C M4 —
// Stripe test keys, common disable strings, etc. Used by
// composeProjectEnvVariables to flag external-secret values that the
// agent likely mis-classified (a sentinel value usually wants
// PlainConfig verbatim, not REPLACE_ME substitution).
//
// Conservative allowlist — false positives waste user attention but
// false negatives miss real M4 cases. New patterns require BOTH a
// known frequency in real apps AND consensus on whether they should
// stay external-secret OR shift to plain-config.
func isLikelySentinel(value string) bool {
	lc := strings.ToLower(strings.TrimSpace(value))
	if lc == "" {
		return false // empty handled separately by the caller
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
// name appears in the zerops.yaml run.envVariables ref set. The bundle
// would otherwise drop the project env while zerops.yaml's
// `${ENV_NAME}` reference at re-import would have nothing to resolve
// against — the M2 case from plan §3.4 amendment 12.
//
// Detection only — no auto-reclassification (agent owns classification
// per Codex Agent B 2c). The agent reads the warning, optionally
// reclassifies the env as PlainConfig in the per-env review table,
// and re-runs BuildBundle.
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
// readability. Pulled out so warnings stay consistent across
// composeProjectEnvVariables + detectIndirectInfraReferences.
func quoteEnvName(name string) string {
	return `"` + name + `"`
}
