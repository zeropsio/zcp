package platform

import (
	"testing"
)

// Tests for: design/zcp-prd.md section 4.3 (ZeropsClient)

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
