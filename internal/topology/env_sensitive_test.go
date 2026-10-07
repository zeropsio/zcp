package topology

import "testing"

// TestDefaultSensitive — the name rule a value falls back on when nobody says.
func TestDefaultSensitive(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key  string
		want bool
	}{
		{"APP_SECRET", true},
		{"GITHUB_TOKEN", true},
		{"APP_KEY", true},
		{"stripe_api_key", true},
		{"DB_PASSWORD", true},
		{"SMTP_PASS", true},
		{"SENTRY_DSN", true},
		{"PRIVATE_KEY_PEM", true},
		{"GOOGLE_CREDENTIALS", true},
		{"NODE_ENV", false},
		{"LOG_LEVEL", false},
		{"API_BASE_URL", false},
		// Public by design: a browser bundle ships it.
		{"STRIPE_PUBLISHABLE_KEY", false},
		{"NEXT_PUBLIC_STRIPE_KEY", false},
		{"PUBLIC_SECRET_KEY", true},
		// A password the person signs in with stays readable to them.
		{"SUPERADMIN_PASSWORD", false},
		{"ADMIN_PASS", false},
		{"GRAFANA_ADMIN_PASSWORD", false},
		{"ADMIN_API_TOKEN", true},
	}
	for _, tt := range tests {
		if got := DefaultSensitive(tt.key); got != tt.want {
			t.Errorf("DefaultSensitive(%q) = %v, want %v", tt.key, got, tt.want)
		}
	}
}

// TestReadableByDesign — a public name and an admin's sign-in password stay
// readable even when their value is a secret zcp generates.
func TestReadableByDesign(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key  string
		want bool
	}{
		{"STRIPE_PUBLISHABLE_KEY", true},
		{"NEXT_PUBLIC_API_URL", true},
		{"SUPERADMIN_PASSWORD", true},
		{"PUBLIC_SECRET_KEY", false},
		{"ADMIN_API_TOKEN", false},
		{"APP_SALT", false},
		{"NODE_ENV", false},
	}
	for _, tt := range tests {
		if got := ReadableByDesign(tt.key); got != tt.want {
			t.Errorf("ReadableByDesign(%q) = %v, want %v", tt.key, got, tt.want)
		}
	}
}

// TestDefaultSensitiveValue — a value made only of references is wiring: it holds no secret of
// its own, so it stays readable whatever its name, and the agent can still read how it is wired.
func TestDefaultSensitiveValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key, value string
		want       bool
	}{
		{"DB_PASSWORD", "${db_password}", false},
		{"REDIS_PASS", " ${redis_password} ", false},
		{"APP_KEY", "${APP_KEY_SECRET}", false},
		{"DB_PASSWORD", "hunter2", true},
		{"APP_KEY", "base64:${APP_KEY_SECRET}", true},
		{"DATABASE_URL", "postgresql://${db_user}:${db_password}@db/db", false},
		{"API_URL", "${zeropsSubdomainHost}", false},
	}
	for _, tt := range tests {
		if got := DefaultSensitiveValue(tt.key, tt.value); got != tt.want {
			t.Errorf("DefaultSensitiveValue(%q, %q) = %v, want %v", tt.key, tt.value, got, tt.want)
		}
	}
}
