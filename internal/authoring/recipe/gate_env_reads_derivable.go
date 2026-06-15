package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
)

// Run-40 B1 — env-reads-derivable gate.
//
// Refuses scaffold/feature complete-phase when a codebase's
// `<SourceRoot>/zerops.yaml` declares run.envVariables keys the
// source can't read (via process.env.<KEY> / import.meta.env.<KEY>).
// Run-39 surfaced three dead envs:
//
//   - apidev/zerops.yaml SEARCH_PUBLIC_HOST   — no source read
//   - apidev/zerops.yaml SEARCH_SEARCH_KEY    — no source read
//   - workerdev/zerops.yaml NATS_QUEUE_GROUP  — source hardcodes literal
//
// Each declaration carries a downstream cost: the codebase-content
// composer authors KB framing around an env-driven knob the runtime
// can't actually rename, and the porter is told a yaml field they
// don't need. Refusing close at the authoring sub-agent's same-
// context window puts the fix where it's cheapest.
//
// Per-codebase scope: a violation cites the codebase hostname,
// declared KEY, and yaml path. Source-of-truth is
// plan.ObservedFacts.EnvReads[hostname], populated by feature-phase
// source-grep (populateEnvReadsFromSource). Codebases without an
// EnvReads entry (sim path, pre-feature, unreadable tree) get a
// notice instead of a blocking violation — the absence is
// indistinguishable from "nothing was scanned" so the gate refuses
// to weaponize an unfilled fact.
//
// Carve-out: build-time pseudo-vars. Vite/Astro bake
// `${import.meta.env.VITE_<KEY>}` references at build time. The
// gate doesn't require source reads when the declared key is
// `VITE_*` AND the codebase has at least one `import.meta.env.*`
// read in EnvReads — that signals the codebase IS using
// import.meta.env idiomatically, and the specific VITE_ key may be
// read from a config file the regex didn't scan (vite.config.ts
// pulls them through Vite's own machinery). Conservative: leaves
// the carve-out narrow so the SEARCH_* / NATS_QUEUE_GROUP cases
// still fire.
//
// Diagnosed in plans/run-40-evidence-grounded-plan.md §"S0-6" + §"B1".

// gateEnvReadsDerivable runs the env-reads consistency check across
// every codebase. Returns blocking violations for declared-but-not-
// read envs and notice violations for missing source-grep data.
func gateEnvReadsDerivable(ctx GateContext) []Violation {
	if ctx.Plan == nil {
		return nil
	}
	var out []Violation
	for _, cb := range ctx.Plan.Codebases {
		if cb.SourceRoot == "" {
			continue
		}
		yamlPath := filepath.Join(cb.SourceRoot, "zerops.yaml")
		body, err := os.ReadFile(yamlPath)
		if err != nil {
			continue
		}
		declared := parseDeclaredRunEnvKeys(string(body))
		if len(declared) == 0 {
			continue
		}
		reads, hasReads := envReadsForCodebase(ctx.Plan, cb.Hostname)
		if !hasReads {
			// Run-40 fix-up #6 — Blocking (was Notice). The gate's
			// "no env-reads entry" branch is the bypass path through
			// the dead-env refusal: if the populate step skipped this
			// codebase (any reason — missing dir, walk failure, race),
			// the gate would soft-notice instead of refusing close and
			// the dead-env declarations would ship anyway. Codex code
			// review flagged this as the gate's silent bypass.
			// Recovery: re-run feature complete-phase (which fires
			// populateEnvReadsFromSource synchronously now per
			// fix-up #5), or populate the entry manually via
			// update-plan.
			out = append(out, Violation{
				Code:     "env-reads-not-populated",
				Path:     fmt.Sprintf("codebase/%s/observedFacts.envReads", cb.Hostname),
				Severity: SeverityBlocking,
				Message: fmt.Sprintf(
					"codebase %q has declared run.envVariables but plan.ObservedFacts.EnvReads carries no entry — feature-phase populateEnvReadsFromSource did not run for this codebase. The dead-env refusal can't fire without source-grep data and must NOT be bypassed silently. Re-run feature complete-phase or populate the entry manually via update-plan.",
					cb.Hostname),
			})
			continue
		}
		readsSet := make(map[string]struct{}, len(reads))
		for _, k := range reads {
			readsSet[k] = struct{}{}
		}
		hasViteRead := codebaseReadsViteEnv(reads)
		var orphans []string
		for _, key := range declared {
			if _, ok := readsSet[key]; ok {
				continue
			}
			if hasViteRead && isViteEnvKey(key) {
				// Carve-out for build-time bake — declared VITE_ keys
				// when the codebase already idiomatically reads
				// import.meta.env in source.
				continue
			}
			orphans = append(orphans, key)
		}
		if len(orphans) == 0 {
			continue
		}
		sort.Strings(orphans)
		for _, key := range orphans {
			out = append(out, Violation{
				Code:     "env-declared-not-read",
				Path:     fmt.Sprintf("codebase/%s/zerops.yaml::run.envVariables.%s", cb.Hostname, key),
				Severity: SeverityBlocking,
				Message: fmt.Sprintf(
					"codebase %q declares run.envVariables.%s in %s but source-grep found no `process.env.%s` or `import.meta.env.%s` read across the tree. The variable is dead at runtime — downstream briefs would author KB framing around an env-driven knob the runtime can't reach. Recovery: either remove the declaration from the codebase yaml (the source decided to not use it) or restore the consuming code path (the source forgot to read it).",
					cb.Hostname, key, yamlPath, key, key),
			})
		}
	}
	return out
}

// envReadsForCodebase returns the EnvReads slice for a hostname plus
// whether the plan has an entry at all. Distinguishes empty slice
// (scanned, no reads) from missing key (not yet scanned).
func envReadsForCodebase(plan *Plan, host string) ([]string, bool) {
	if plan == nil || plan.ObservedFacts.EnvReads == nil {
		return nil, false
	}
	reads, ok := plan.ObservedFacts.EnvReads[host]
	return reads, ok
}

// codebaseReadsViteEnv reports whether the codebase has any
// import.meta.env.* read in its source. Used by the Vite carve-out
// in gateEnvReadsDerivable.
func codebaseReadsViteEnv(reads []string) bool {
	return slices.ContainsFunc(reads, isViteEnvKey)
}

// isViteEnvKey reports whether key matches Vite's VITE_-prefixed
// naming convention. Vite only exposes `import.meta.env.VITE_*` to
// the client bundle; non-VITE_-prefixed entries are server-only.
func isViteEnvKey(key string) bool {
	const prefix = "VITE_"
	if len(key) < len(prefix) {
		return false
	}
	return key[:len(prefix)] == prefix
}
