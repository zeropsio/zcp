package bundle

import "testing"

// TestNewVaultValue_WiringStaysReadable — a reference is never a secret of its own, even where
// zcp judged the key a secret: it is written plain so the wiring stays readable.
func TestNewVaultValue_WiringStaysReadable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key, value string
		secret     bool
		want       bool
	}{
		{"DB_PASSWORD", "${db_password}", false, false},
		{"DB_PASSWORD", "${db_password}", true, false},
		{"APP_SECRET", "<@generateRandomString(<32>)>", true, true},
		{"SUPERADMIN_PASSWORD", "<@generateRandomString(<16>)>", true, true},
		{"STRIPE_PUBLISHABLE_KEY", "<@generateRandomString(<16>)>", true, false},
	}
	for _, tt := range tests {
		if got := newVaultValue(tt.key, tt.value, tt.secret).sensitive; got != tt.want {
			t.Errorf("newVaultValue(%q, %q, %v).sensitive = %v, want %v", tt.key, tt.value, tt.secret, got, tt.want)
		}
	}
}
