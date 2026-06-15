package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// gate_cross_codebase_env_coherence.go — run-32 Step 2 (detection only).
//
// At scaffold complete-phase, every codebase's zerops.yaml is committed
// independently by a separate sub-agent. There is no inter-codebase
// contract pass: each scaffold sub-agent picks env-var key names in
// isolation. Result observed in run-32: apidev names the S3 access key
// `S3_ACCESS_KEY_ID`/`S3_SECRET_ACCESS_KEY` while workerdev names the
// same source `S3_KEY`/`S3_SECRET`; apidev uses `DB_PASSWORD` while
// workerdev uses `DB_PASS`. Three cross-codebase mismatches in a single
// run, all sourced from identical platform refs (e.g. `${db_password}`).
//
// Porter consequence: cloning the recipe and copying integration code
// between codebases breaks because the env-var keys disagree on the
// left-hand side even though the right-hand source is the same.
//
// This gate detects those mismatches by parsing each codebase's
// `run.envVariables` block, grouping entries by their `${<service>_*}`
// source reference, and flagging any source where the codebase set has
// >1 distinct left-hand key. Severity is `SeverityNotice` (detection
// only) — Step 2 enforcement (refusal) lands once the agent surfaces
// have a place to publish the cross-codebase contract.
//
// Pinned by TestGateCrossCodebaseEnvCoherence_*.

// CrossCodebaseEnvMismatch describes one disagreement: a single source
// reference (`${<service>_<suffix>}`) that maps to different left-hand
// own-key names across two or more codebases.
type CrossCodebaseEnvMismatch struct {
	Source string             // "${db_password}"
	Pairs  []EnvKeyByCodebase // sorted by codebase name
}

// EnvKeyByCodebase is one left-hand own-key as seen in a particular
// codebase's run.envVariables.
type EnvKeyByCodebase struct {
	Codebase string // hostname, e.g. "apidev"
	Key      string // left-hand own-key, e.g. "S3_ACCESS_KEY_ID"
}

// FindCrossCodebaseEnvMismatches scans the supplied codebase → yaml-body
// map for env-var coherence violations across siblings. The map key is
// the codebase hostname (no slot suffix); the value is the codebase's
// zerops.yaml body bytes. Returns mismatches sorted by source for
// deterministic ordering.
//
// Exported so tests can drive it without touching the filesystem; the
// gate wrapper below reads each codebase's `<SourceRoot>/zerops.yaml`
// and delegates here.
func FindCrossCodebaseEnvMismatches(yamlsByCodebase map[string][]byte) []CrossCodebaseEnvMismatch {
	if len(yamlsByCodebase) < 2 {
		return nil // need at least two codebases to disagree
	}
	// source -> codebase -> set of distinct left-hand keys observed in
	// THAT codebase. A codebase can alias the same source under multiple
	// names (e.g. `DB_PASSWORD` AND `DB_AUTH` both bound to `${db_password}`)
	// — the union across the whole codebase set determines whether
	// there's a coherence violation, so we record ALL keys per (source,
	// codebase) rather than first-wins. Pure intra-codebase aliasing
	// (same source under N names within one codebase, same name
	// elsewhere) is still a coherence issue worth flagging.
	type sourceMap = map[string]map[string]bool // codebase -> set of leftKeys
	bySource := map[string]sourceMap{}

	hostnames := make([]string, 0, len(yamlsByCodebase))
	for h := range yamlsByCodebase {
		hostnames = append(hostnames, h)
	}
	sort.Strings(hostnames)

	for _, host := range hostnames {
		body := yamlsByCodebase[host]
		if len(body) == 0 {
			continue
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(body, &doc); err != nil {
			continue // unparseable — schema gate fires elsewhere
		}
		envNodes := collectRunEnvVariables(&doc)
		for _, envNode := range envNodes {
			// envNode is a mapping; pairs at indices 0,2,4...
			for i := 0; i+1 < len(envNode.Content); i += 2 {
				keyNode := envNode.Content[i]
				valNode := envNode.Content[i+1]
				if keyNode == nil || valNode == nil {
					continue
				}
				if keyNode.Kind != yaml.ScalarNode || valNode.Kind != yaml.ScalarNode {
					continue
				}
				leftKey := keyNode.Value
				rawValue := valNode.Value
				// Only track values that are EXACTLY a single
				// `${<service>_<suffix>}` reference (the alias-of-platform
				// shape). Composite values like
				// `${storage_apiUrl}/${storage_bucketName}` aren't aliases —
				// they're computed URLs and don't carry the same coherence
				// expectation.
				ref, ok := matchSingleServiceRef(rawValue)
				if !ok {
					continue
				}
				cbMap, exists := bySource[ref]
				if !exists {
					cbMap = sourceMap{}
					bySource[ref] = cbMap
				}
				keys, hasKeys := cbMap[host]
				if !hasKeys {
					keys = map[string]bool{}
					cbMap[host] = keys
				}
				keys[leftKey] = true
			}
		}
	}

	// Scan for sources where the union set of left-keys across
	// codebases has >1 distinct value (catches both cross-codebase
	// mismatch AND intra-codebase aliasing).
	var out []CrossCodebaseEnvMismatch
	for source, cbMap := range bySource {
		keySet := map[string]bool{}
		for _, keys := range cbMap {
			for k := range keys {
				keySet[k] = true
			}
		}
		if len(keySet) < 2 {
			continue
		}
		var pairs []EnvKeyByCodebase
		for cb, keys := range cbMap {
			for k := range keys {
				pairs = append(pairs, EnvKeyByCodebase{Codebase: cb, Key: k})
			}
		}
		sort.Slice(pairs, func(i, j int) bool {
			if pairs[i].Codebase != pairs[j].Codebase {
				return pairs[i].Codebase < pairs[j].Codebase
			}
			return pairs[i].Key < pairs[j].Key
		})
		out = append(out, CrossCodebaseEnvMismatch{Source: source, Pairs: pairs})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Source < out[j].Source })
	return out
}

// matchSingleServiceRef returns the source reference text (e.g.
// "${db_password}") if the supplied value is EXACTLY a single
// `${<lowerToken>_<suffix>}` reference. Returns empty + false for
// composite values, literals, missing-${} wrappers, or refs whose
// service-segment is uppercase (project-level vars like
// `${APP_SECRET}` aren't service refs).
//
// Suffix shape requirement: the underscore-separated suffix must start
// with a letter (`db_password` ok; `db_` rejected; `db__password`
// rejected). This catches malformed refs as misshapen, not as service
// refs.
func matchSingleServiceRef(value string) (string, bool) {
	v := strings.TrimSpace(value)
	if !strings.HasPrefix(v, "${") || !strings.HasSuffix(v, "}") {
		return "", false
	}
	inner := v[2 : len(v)-1]
	// Reject composite — must not contain another ${} or non-alphanum.
	if strings.ContainsAny(inner, "${}/:") {
		return "", false
	}
	if inner == "" {
		return "", false
	}
	// Service refs are <lowercase-host>_<suffix>; project-scope is
	// uppercase (APP_SECRET, FRONTEND_URL). First char selects the path.
	first := inner[0]
	if first < 'a' || first > 'z' {
		return "", false
	}
	// Split on first underscore; require non-empty alpha-starting suffix.
	underscore := strings.IndexByte(inner, '_')
	if underscore <= 0 {
		return "", false
	}
	suffix := inner[underscore+1:]
	if suffix == "" {
		return "", false // ${db_}
	}
	suffixFirst := suffix[0]
	isAlpha := (suffixFirst >= 'a' && suffixFirst <= 'z') || (suffixFirst >= 'A' && suffixFirst <= 'Z')
	if !isAlpha {
		return "", false // ${db__password} (suffix starts with `_`); ${db_1abc}
	}
	return v, true
}

// gateCrossCodebaseEnvCoherence reads every codebase's
// `<SourceRoot>/zerops.yaml` and emits one Violation per cross-codebase
// env-var mismatch. Severity is `SeverityNotice` (detection only).
// Run-32 Step 2 (detection-only).
func gateCrossCodebaseEnvCoherence(ctx GateContext) []Violation {
	if ctx.Plan == nil || len(ctx.Plan.Codebases) < 2 {
		return nil
	}
	yamlsByCodebase := map[string][]byte{}
	for _, cb := range ctx.Plan.Codebases {
		if cb.SourceRoot == "" || cb.Hostname == "" {
			continue
		}
		yamlPath := filepath.Join(cb.SourceRoot, "zerops.yaml")
		raw, err := os.ReadFile(yamlPath)
		if err != nil {
			continue // pre-scaffold; gateFactsRecorded surfaces this
		}
		yamlsByCodebase[cb.Hostname] = raw
	}
	mismatches := FindCrossCodebaseEnvMismatches(yamlsByCodebase)
	if len(mismatches) == 0 {
		return nil
	}
	out := make([]Violation, 0, len(mismatches))
	for _, m := range mismatches {
		pairsStr := make([]string, 0, len(m.Pairs))
		for _, p := range m.Pairs {
			pairsStr = append(pairsStr, fmt.Sprintf("%s=`%s`", p.Codebase, p.Key))
		}
		out = append(out, Violation{
			Code: "cross-codebase-env-coherence",
			Path: "scaffold/cross-codebase",
			Message: fmt.Sprintf(
				"source %s aliased under different left-hand keys across siblings: %s — porter copying integration code between these codebases will hit env-var key drift. Align names at scaffold time.",
				m.Source,
				strings.Join(pairsStr, ", "),
			),
			Severity: SeverityNotice,
		})
	}
	return out
}
