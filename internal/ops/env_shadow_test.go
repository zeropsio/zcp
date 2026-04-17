package ops

import (
	"testing"
)

// TestDetectSelfShadows — v8.85. A self-shadow is any run.envVariables entry
// where the key on the left exactly matches the `${...}` template on the
// right. The platform interpolator sees the service-level var first, cannot
// recurse back to the auto-injected source, and the OS env var resolves to
// the literal string `${varname}`.
//
// Session-log 16 shipped workerdev/zerops.yaml with 8 self-shadows across
// db_* and queue_* vars. The worker then tried to connect to
// "${db_hostname}:5432" and crashed. This detector is the structural check
// that catches the pattern at generate-step completion.
func TestDetectSelfShadows(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		env    map[string]string
		expect []string // offending KEYS; order-independent
	}{
		{
			name:   "nil map is empty",
			env:    nil,
			expect: nil,
		},
		{
			name:   "empty map is empty",
			env:    map[string]string{},
			expect: nil,
		},
		{
			name: "cross-service self-shadow — the session-log-16 pattern",
			env: map[string]string{
				"db_hostname":    "${db_hostname}",
				"db_password":    "${db_password}",
				"queue_user":     "${queue_user}",
				"storage_apiUrl": "${storage_apiUrl}",
			},
			expect: []string{"db_hostname", "db_password", "queue_user", "storage_apiUrl"},
		},
		{
			name: "project-level self-shadow",
			env: map[string]string{
				"STAGE_API_URL": "${STAGE_API_URL}",
				"APP_SECRET":    "${APP_SECRET}",
			},
			expect: []string{"STAGE_API_URL", "APP_SECRET"},
		},
		{
			name: "framework-convention rename — safe (keys differ)",
			env: map[string]string{
				"DB_HOST":      "${db_hostname}",
				"DATABASE_URL": "${db_connectionString}",
				"FRONTEND_URL": "${STAGE_FRONTEND_URL}",
			},
			expect: nil,
		},
		{
			name: "mode flags — safe (no template)",
			env: map[string]string{
				"NODE_ENV":  "production",
				"APP_ENV":   "local",
				"LOG_LEVEL": "debug",
			},
			expect: nil,
		},
		{
			name: "mixed safe + unsafe",
			env: map[string]string{
				"NODE_ENV":      "production",            // mode flag
				"DB_HOST":       "${db_hostname}",        // rename
				"db_password":   "${db_password}",        // SELF-SHADOW
				"FRONTEND_URL":  "${STAGE_FRONTEND_URL}", // rename
				"STAGE_API_URL": "${STAGE_API_URL}",      // SELF-SHADOW
			},
			expect: []string{"db_password", "STAGE_API_URL"},
		},
		{
			name: "whitespace in template does not bypass",
			env: map[string]string{
				"db_hostname": "${ db_hostname }",
			},
			expect: []string{"db_hostname"},
		},
		{
			name: "interpolation inside larger value — not a self-shadow",
			env: map[string]string{
				// The value contains the key name inside a larger string.
				// This is not the pattern we flag — the value is not a
				// pure `${KEY}` template, just a concatenation.
				"DATABASE_URL": "postgres://${db_hostname}:5432/app",
			},
			expect: nil,
		},
		{
			name: "empty value — not a self-shadow",
			env: map[string]string{
				"db_hostname": "",
			},
			expect: nil,
		},
		{
			name: "non-template value — not a self-shadow",
			env: map[string]string{
				"db_hostname": "localhost",
			},
			expect: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := DetectSelfShadows(tc.env)
			if !sameSet(got, tc.expect) {
				t.Errorf("DetectSelfShadows(%v) = %v; want set %v", tc.env, got, tc.expect)
			}
		})
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]int, len(a))
	for _, s := range a {
		m[s]++
	}
	for _, s := range b {
		if m[s] == 0 {
			return false
		}
		m[s]--
	}
	return true
}
