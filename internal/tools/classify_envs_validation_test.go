package tools

import (
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestValidateEnvClassifications pins B12: typo'd buckets are rejected at the
// boundary, valid buckets and empty (unclassified) pass.
func TestValidateEnvClassifications(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      map[string]string
		wantErr bool
	}{
		{"valid buckets", map[string]string{"A": "infrastructure", "B": "auto-secret", "C": "external-secret", "D": "plain-config"}, false},
		{"exclude is a valid bucket", map[string]string{"APP_KEY": "exclude"}, false},
		{"empty is unclassified", map[string]string{"A": ""}, false},
		{"typo secret", map[string]string{"APP_KEY": "secret"}, true},
		{"typo autosecret", map[string]string{"X": "autosecret"}, true},
		{"nil map", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateEnvClassifications(tt.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateEnvClassifications(%v) err=%v, wantErr=%v", tt.in, err, tt.wantErr)
			}
		})
	}
}

// TestBundleProjectEnvsFromSource_CarriesSensitive — the platform's
// sensitive flag survives into the bundle composer input, so a sensitive
// value never rides the bundle verbatim (the composer swaps a placeholder).
func TestBundleProjectEnvsFromSource_CarriesSensitive(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		sensitive bool
	}{
		{"sensitive", true},
		{"plain", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := bundleProjectEnvsFromSource([]platform.ProjectEnvVar{
				{Key: "STRIPE_SECRET", Content: "x", Type: platform.ProjectEnvUser, Sensitive: tt.sensitive},
			})
			if len(got) != 1 {
				t.Fatalf("got %d envs, want 1", len(got))
			}
			if got[0].Sensitive != tt.sensitive {
				t.Errorf("Sensitive = %v, want %v", got[0].Sensitive, tt.sensitive)
			}
		})
	}
}
