package ops

import "strings"

// DetectSelfShadows returns the keys in an envVariables map whose value is a
// pure `${KEY}` template matching the same key — i.e. `db_hostname: ${db_hostname}`.
//
// Self-shadows are always wrong. The Zerops template interpolator sees the
// service-level variable of that name first, cannot recurse back to the
// auto-injected source (cross-service `${hostname_varname}` or project-level
// `${VAR_NAME}`), and resolves the OS env var to the literal string
// `${varname}`. Applications then try to connect to "${db_hostname}:5432",
// authenticate with password "${db_password}", etc., and crash with cryptic
// DNS/auth errors.
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
