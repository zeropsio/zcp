package ops

import (
	"os"
	"strings"
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
				"API_URL":    "${API_URL}",
				"APP_SECRET": "${APP_SECRET}",
			},
			expect: []string{"API_URL", "APP_SECRET"},
		},
		{
			name: "framework-convention rename — safe (keys differ)",
			env: map[string]string{
				"DB_HOST":           "${db_hostname}",
				"DATABASE_URL":      "${db_connectionString}",
				"VITE_FRONTEND_URL": "${FRONTEND_URL}",
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
				"NODE_ENV":          "production",      // mode flag
				"DB_HOST":           "${db_hostname}",  // rename
				"db_password":       "${db_password}",  // SELF-SHADOW
				"VITE_FRONTEND_URL": "${FRONTEND_URL}", // rename
				"API_URL":           "${API_URL}",      // SELF-SHADOW
			},
			expect: []string{"db_password", "API_URL"},
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

// TestSelfShadowSymptom_AtomMatchesMechanism is a drift detector for the
// env-var audit's primary atom↔code consistency invariant. The gotcha
// example (`internal/content/examples/gotcha_pass_platform_invariant_env_shadow.md`)
// describes the symptom string an agent observes; this file's package doc
// describes the underlying mechanism. If either drifts, the agent loses the
// link between "what I see" and "why it happens".
//
// Pinning: both texts must mention the literal `${...}` string form. The
// gotcha previously said "empty string at runtime" (factually wrong) —
// plans/audit-env-vars-20260515/AUDIT.md §A2 / VERIFY-reserved-names.md.
//
// The check is intentionally loose (keyword-based) so authors can reword
// freely AS LONG AS the link to "literal `${varname}` string" survives.
func TestSelfShadowSymptom_AtomMatchesMechanism(t *testing.T) {
	t.Parallel()

	atomPath := "../content/examples/gotcha_pass_platform_invariant_env_shadow.md"
	atomBody, err := os.ReadFile(atomPath)
	if err != nil {
		t.Fatalf("read gotcha atom: %v", err)
	}
	mechanismPath := "env_shadow.go"
	mechanismBody, err := os.ReadFile(mechanismPath)
	if err != nil {
		t.Fatalf("read env_shadow.go: %v", err)
	}

	atomText := string(atomBody)
	mechText := string(mechanismBody)

	// Both must describe the symptom as the literal `${...}` string.
	for _, label := range []struct {
		name string
		body string
	}{
		{"gotcha atom", atomText},
		{"env_shadow.go", mechText},
	} {
		if !strings.Contains(label.body, "literal") {
			t.Errorf("%s missing 'literal' keyword — env-shadow symptom must describe the literal-string mechanism", label.name)
		}
		if !strings.Contains(label.body, "${") {
			t.Errorf("%s missing `${` token — symptom example must show the dollar-brace form the container actually receives", label.name)
		}
	}

	// Neither side may revert to the historical wrong symptom phrasing.
	const banned = "empty string at runtime"
	if strings.Contains(strings.ToLower(atomText), banned) {
		t.Errorf("gotcha atom contains banned phrase %q — symptom is literal `${var}` string, not empty (verified 2026-05-16, eval-zcp probe P2)", banned)
	}
	if strings.Contains(strings.ToLower(mechText), banned) {
		t.Errorf("env_shadow.go contains banned phrase %q", banned)
	}
}
