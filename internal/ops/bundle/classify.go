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

// unionEnvRefs returns every ${...} ref name visible to a promoted runtime:
// the run.envVariables refs (already extracted from the bundled zerops.yaml)
// plus refs embedded in kept project env VALUES. Project envs auto-inject and
// their refs resolve regardless of isolation (spec §3), so a managed dep wired
// through a project env (DB_URL=${db_hostname}) is reachable too.
func unionEnvRefs(zeropsRefs map[string]bool, projectEnvs map[string]string) map[string]bool {
	out := make(map[string]bool, len(zeropsRefs))
	for r := range zeropsRefs {
		out[r] = true
	}
	for _, v := range projectEnvs {
		for _, name := range parseDollarBraceRefs(v) {
			out[name] = true
		}
	}
	return out
}

// detectUnreferencedManagedDeps warns when a promoted managed dep has no
// explicit ${<host>_*} reference anywhere in the bundle (runtime
// run.envVariables or a kept project env value). Under the default `service`
// isolation a runtime does NOT auto-receive a managed dep's connection vars
// (spec §3/§4) — only an explicit ref resolves — so an unreferenced managed
// dep is unreachable in production. Detection only (launch warning, not
// hard-fail): the operator adds the refs and re-composes.
func detectUnreferencedManagedDeps(managed []ManagedServiceEntry, refs map[string]bool) []string {
	var warns []string
	for _, m := range dedupeManagedByHostname(managed) {
		canon := strings.ReplaceAll(m.Hostname, "-", "_")
		prefix := canon + "_"
		referenced := false
		for ref := range refs {
			if strings.HasPrefix(ref, prefix) {
				referenced = true
				break
			}
		}
		if referenced {
			continue
		}
		warns = append(warns,
			"managed service "+quoteEnvName(m.Hostname)+" is promoted but nothing references ${"+canon+
				"_*} in run.envVariables or project envs — under the default service isolation the runtime will NOT "+
				"auto-receive its connection vars (spec §3/§4). Add explicit refs (e.g. ${"+canon+"_hostname}, ${"+canon+
				"_connectionString}) to a runtime's run.envVariables so it can reach "+m.Hostname+" in production. (plan §5 PR-4)")
	}
	return warns
}
