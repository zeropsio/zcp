package platform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRevokeIntegrationToken_SendsDelete pins docs/spec-eval-farm.md §3.4
// FM-23: revoke is DELETE /client/{clientId}/integration-token/{tokenId}
// (SDK DeleteClientIntegrationToken); a {"success":true} body maps to a nil
// error, a 400 to a wrapped error. Independent oracle: the path shape and
// the "revoke" side of FM-23 come from the spec text and the SDK's own
// generated DeleteClientIntegrationToken.go (path.IntegrationTokenId), never
// from this package's own output.
func TestRevokeIntegrationToken_SendsDelete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
	}{
		{"success", http.StatusOK, `{"success":true}`, false},
		{"badRequest", http.StatusBadRequest, `{"error":{"code":"someError","message":"nope"}}`, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var gotMethod, gotPath string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			client, err := NewZeropsClient("test-token", server.URL)
			if err != nil {
				t.Fatalf("NewZeropsClient: %v", err)
			}

			err = client.RevokeIntegrationToken(context.Background(), "client-abc", "token-xyz")
			if tc.wantErr && err == nil {
				t.Fatal("RevokeIntegrationToken: want error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("RevokeIntegrationToken: want nil, got %v", err)
			}

			if gotMethod != http.MethodDelete {
				t.Errorf("method = %q, want DELETE", gotMethod)
			}
			const wantPath = "/api/rest/public/client/client-abc/integration-token/token-xyz"
			if gotPath != wantPath {
				t.Errorf("path = %q, want %q", gotPath, wantPath)
			}
			if strings.Contains(gotPath, "test-token") {
				t.Errorf("path must never carry the bearer token: %q", gotPath)
			}
		})
	}
}
