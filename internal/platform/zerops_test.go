package platform

import (
	"testing"
)

// Tests for: ZeropsClient (internal/platform/client.go)

func TestNewZeropsClient_URLNormalization(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		apiHost string
		wantErr bool
	}{
		{
			name:    "plain host adds https and slash",
			apiHost: "api.app-prg1.zerops.io",
		},
		{
			name:    "already has https",
			apiHost: "https://api.app-prg1.zerops.io",
		},
		{
			name:    "already has trailing slash",
			apiHost: "api.app-prg1.zerops.io/",
		},
		{
			name:    "full url with scheme and slash",
			apiHost: "https://api.app-prg1.zerops.io/",
		},
		{
			name:    "empty host falls back to default",
			apiHost: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client, err := NewZeropsClient("test-token", tt.apiHost)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if client == nil {
				t.Fatal("client is nil")
			}
		})
	}
}

// TestResolveEndpoint pins the empty-host fallback and URL normalization
// invariants. Without the empty-host branch the endpoint became literal
// "https://" (no host) — every SDK request failed with
// `http: no Host in request URL`, which broke the existing-project
// launch path's first call to GetUserInfo.
func TestResolveEndpoint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		apiHost string
		want    string
	}{
		{
			name:    "empty falls back to defaultAPIHost",
			apiHost: "",
			want:    "https://api.app-prg1.zerops.io/",
		},
		{
			name:    "bare hostname gets scheme and trailing slash",
			apiHost: "api.app-prg1.zerops.io",
			want:    "https://api.app-prg1.zerops.io/",
		},
		{
			name:    "scheme preserved",
			apiHost: "https://api.app-prg1.zerops.io",
			want:    "https://api.app-prg1.zerops.io/",
		},
		{
			name:    "trailing slash preserved",
			apiHost: "api.app-prg1.zerops.io/",
			want:    "https://api.app-prg1.zerops.io/",
		},
		{
			name:    "full URL passthrough",
			apiHost: "https://api.app-prg1.zerops.io/",
			want:    "https://api.app-prg1.zerops.io/",
		},
		{
			name:    "custom host fallback shape",
			apiHost: "api.example.com",
			want:    "https://api.example.com/",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := resolveEndpoint(tt.apiHost)
			if got != tt.want {
				t.Errorf("resolveEndpoint(%q) = %q, want %q", tt.apiHost, got, tt.want)
			}
		})
	}
}
