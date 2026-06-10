package tools

import "testing"

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
