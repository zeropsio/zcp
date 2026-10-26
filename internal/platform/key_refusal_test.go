package platform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestZeropsClient_KeyRefusal_ReportsOnlyAnAuthenticationRefusal: the hook
// hears Zerops refusing the key itself (401: the token is unknown, expired or
// revoked), never a 403 or 404 about the resource a valid key asked for
// (docs/spec-mate.md §3.3, TC-21). The statuses are Zerops' own, the
// expected counts the spec's sentence.
func TestZeropsClient_KeyRefusal_ReportsOnlyAnAuthenticationRefusal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status int
		want   int
	}{
		{"401 refuses the key", http.StatusUnauthorized, 1},
		{"403 refuses a resource", http.StatusForbidden, 0},
		{"404 finds no resource", http.StatusNotFound, 0},
		{"500 is Zerops failing", http.StatusInternalServerError, 0},
		{"200 answers", http.StatusOK, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"error":{"code":"someCode","message":"no"}}`))
			}))
			defer server.Close()
			client, err := NewZeropsClient("test-token", server.URL)
			if err != nil {
				t.Fatal(err)
			}
			heard := 0
			client.OnKeyRefused(func() { heard++ })

			_, _ = client.GetUserInfo(context.Background())

			if heard != tt.want {
				t.Errorf("hook heard %d refusals, want %d", heard, tt.want)
			}
		})
	}
}
