package ops

import "strings"

// DetectSelfShadows returns the keys in an envVariables map whose value is a
// pure `${KEY}` template matching the same key — i.e. `API_URL: ${API_URL}`.
//
// Same-key declarations are always wrong. The Zerops template interpolator
// sees the service-level variable of that name first, cannot recurse back
// to the project-level value, and resolves the OS env var to the literal
// string `${varname}`. Applications then try to connect to "${db_hostname}:5432",
// authenticate with password "${API_URL}", etc., and crash with cryptic
// DNS/auth errors.
//
// Failure-mode shape varies by var scope. PROJECT-level vars auto-inherit
// into every container; a same-key declaration in run.envVariables
// produces the literal-string shadow above. CROSS-SERVICE vars do not
// auto-inject under the porter-default isolation — a same-key
// declaration is technically not a "shadow" (there is no auto-injected
// value to shadow) but is still invalid because the right-hand-side
// template cannot resolve to anything useful. Either way the value
// becomes a literal at runtime; flag both shapes uniformly.
//
// Matching rules:
//   - Value must be EXACTLY `${KEY}` (optionally with surrounding whitespace
//     inside the braces, and/or surrounding whitespace outside). Values that
//     contain `${KEY}` inside a larger string (e.g. "postgres://${db_hostname}:5432/app")
//     are NOT self-shadows — they are legitimate interpolation into a
//     composed string.
//   - KEY-vs-key comparison is case-sensitive — environment variable names
//     are case-sensitive on Linux and the platform interpolator treats them
//     as such.
//
// Legitimate non-shadow patterns (not flagged):
//   - Framework-convention renames: `DB_HOST: ${db_hostname}` — keys differ.
//   - Mode flags: `NODE_ENV: production` — no template.
//   - Composed strings: `DATABASE_URL: postgres://${db_hostname}/db` — template
//     is a substring, not the whole value.
func DetectSelfShadows(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	var offenders []string
	for key, value := range env {
		if isSelfShadow(key, value) {
			offenders = append(offenders, key)
		}
	}
	return offenders
}

// isSelfShadow reports whether `value` is exactly `${key}` (with optional
// surrounding whitespace inside the braces and/or outside). Returns false for
// composed strings where the template is only a substring of the value.
func isSelfShadow(key, value string) bool {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "${") || !strings.HasSuffix(trimmed, "}") {
		return false
	}
	inner := strings.TrimSpace(trimmed[2 : len(trimmed)-1])
	return inner == key
}

// LayeredShadow is a key set at a LOWER env layer (project) that a HIGHER
// layer overrides with a DIFFERENT value for a specific service — the lower
// value is stored but the container reads the higher one
// (docs/spec-zerops-env-lifecycle.md §2 precedence: yaml-baked > service >
// project).
//
// Distinct from a self-shadow (DetectSelfShadows): that is one map's
// `key: ${key}` template; this is the SAME key carrying different LITERAL
// values across two real layers, so a project-scope set silently has no
// effect on the affected service.
type LayeredShadow struct {
	Key              string
	Hostname         string // service whose higher-layer value wins
	ShadowedValue    string // value at the lower (project) layer
	ShadowedLayer    EnvLayer
	WinningValue     string // the value the container actually reads
	WinningLayer     EnvLayer
	WinningSensitive bool // winning var is a secret — callers must redact WinningValue
}

// DetectLayeredShadows reports which of the lower-layer vars (e.g. a
// project-scope set) a service's higher layers override with a DIFFERENT
// value. Precedence (spec §2): yaml-baked run.envVariables > service userData
// > project — so yaml-baked wins over service when a key sits in both. Only
// differing values are reported (an equal value is observably indistinguishable,
// not a shadow). hostname labels the service the higher layers belong to.
func DetectLayeredShadows(hostname string, lower, service, yamlBaked []EffectiveEnvVar) []LayeredShadow {
	if len(lower) == 0 {
		return nil
	}
	svc := indexEnvByKey(service)
	yaml := indexEnvByKey(yamlBaked)
	var out []LayeredShadow
	for _, l := range lower {
		w, ok := yaml[l.Key]
		if !ok {
			w, ok = svc[l.Key]
		}
		if !ok || w.Value == l.Value {
			continue
		}
		out = append(out, LayeredShadow{
			Key:              l.Key,
			Hostname:         hostname,
			ShadowedValue:    l.Value,
			ShadowedLayer:    l.Layer,
			WinningValue:     w.Value,
			WinningLayer:     w.Layer,
			WinningSensitive: w.Sensitive,
		})
	}
	return out
}

// LayeredShadows reports the cross-layer shadows within this assembled
// effective env (project values overridden by a higher layer). Convenience
// wrapper over DetectLayeredShadows.
func (e *EffectiveEnv) LayeredShadows() []LayeredShadow {
	return DetectLayeredShadows(e.Hostname, e.Project, e.Service, e.YamlBaked)
}

func indexEnvByKey(vars []EffectiveEnvVar) map[string]EffectiveEnvVar {
	m := make(map[string]EffectiveEnvVar, len(vars))
	for _, v := range vars {
		m[v.Key] = v
	}
	return m
}
