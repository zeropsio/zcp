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

// detectDroppedEnvReferences walks the project envs whose classification
// DROPS them from the bundle (Infrastructure, Exclude) and surfaces a
// warning for each one whose name still appears in the zerops.yaml
// run.envVariables ref set — re-import would fail to resolve the ref.
//
// Detection only — no auto-reclassification. The agent reads the
// warning, optionally reclassifies the env in the per-env review table
// (or removes the stale ref from zerops.yaml), and re-runs the composer.
func detectDroppedEnvReferences(
	envs []ProjectEnvVar,
	classifications map[string]topology.SecretClassification,
	refs map[string]bool,
) []string {
	if len(refs) == 0 {
		return nil
	}
	var warns []string
	for _, env := range envs {
		if !refs[env.Key] {
			continue
		}
		//nolint:exhaustive // only the two bundle-dropping buckets warn; kept buckets resolve their own refs
		switch classifications[env.Key] {
		case topology.SecretClassInfrastructure:
			warns = append(warns,
				"env "+quoteEnvName(env.Key)+
					": classified Infrastructure (drops from project.envVariables) but zerops.yaml's run.envVariables references ${"+env.Key+"} — re-import will fail to resolve. "+
					"Reclassify as PlainConfig or rewrite zerops.yaml to use managed-service refs (${db_*}/${redis_*}) directly. (plan §3.4 M2)",
			)
		case topology.SecretClassExclude:
			warns = append(warns,
				"env "+quoteEnvName(env.Key)+
					": classified exclude (dropped from the bundle as stale) but zerops.yaml's run.envVariables still references ${"+env.Key+"} — re-import will fail to resolve. "+
					"Remove the stale reference from zerops.yaml, or reclassify the env if it is still in use.",
			)
		}
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

// ManagedDepReference is the structured per-dep wiring state the
// composer derives from the env-ref scan. Surfaced on the launch
// bundle so the ready-to-launch preview can mark `referenced=false`
// deps and recommend exclusion before the launch key is spent.
//
// Atom corpus references-fields entry point:
//   - bundle.ManagedDepReference.Referenced
type ManagedDepReference struct {
	Hostname string `json:"hostname"`
	Type     string `json:"type,omitempty"`
	// Referenced is true when anything in the bundle references
	// ${<host>_*} — a runtime's run.envVariables or a kept project env
	// value. Under the default service isolation an unreferenced dep is
	// unreachable from the promoted runtimes.
	Referenced bool `json:"referenced"`
}

// ManagedDepReferences computes the per-dep wiring state for every
// promoted managed dep (deduped by hostname). Single owner of the
// ${<host>_*} prefix-match — the PR-4 warning derives from this.
func ManagedDepReferences(managed []ManagedServiceEntry, refs map[string]bool) []ManagedDepReference {
	deduped := dedupeManagedByHostname(managed)
	out := make([]ManagedDepReference, 0, len(deduped))
	for _, m := range deduped {
		prefix := strings.ReplaceAll(m.Hostname, "-", "_") + "_"
		referenced := false
		for ref := range refs {
			if strings.HasPrefix(ref, prefix) {
				referenced = true
				break
			}
		}
		out = append(out, ManagedDepReference{Hostname: m.Hostname, Type: m.Type, Referenced: referenced})
	}
	return out
}

// unreferencedManagedDepWarnings renders the PR-4 launch warning for
// every dep whose Referenced is false. Under the default `service`
// isolation a runtime does NOT auto-receive a managed dep's connection
// vars (spec §3/§4) — only an explicit ref resolves — so an
// unreferenced managed dep is unreachable in production. Detection only
// (launch warning, not hard-fail): the operator adds the refs (or
// excludes the dep) and re-composes.
func unreferencedManagedDepWarnings(deps []ManagedDepReference) []string {
	var warns []string
	for _, d := range deps {
		if d.Referenced {
			continue
		}
		canon := strings.ReplaceAll(d.Hostname, "-", "_")
		warns = append(warns,
			"managed service "+quoteEnvName(d.Hostname)+" is promoted but nothing references ${"+canon+
				"_*} in run.envVariables or project envs — under the default service isolation the runtime will NOT "+
				"auto-receive its connection vars (spec §3/§4). Add explicit refs (e.g. ${"+canon+"_hostname}, ${"+canon+
				"_connectionString}) to a runtime's run.envVariables so it can reach "+d.Hostname+" in production. (plan §5 PR-4)")
	}
	return warns
}
