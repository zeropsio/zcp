package hq

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// selfHQ answers GET /api/mate/self for the credential "good" with closedOff.
func selfHQ(t *testing.T, closedOff bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/mate/self" || r.Header.Get("Authorization") != "Mate good" {
			w.WriteHeader(http.StatusUnauthorized)
			writeJSON(w, map[string]string{"code": "mate_credential_required"})
			return
		}
		writeJSON(w, map[string]any{
			"projectId": "p-mate", "name": "Ada", "face": "face-1",
			"standupRequestedBy": "owner", "closedOff": closedOff,
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestReadClosedOff_FromTheHQItEnrolledWith(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		closedOff  bool
		credential string
		enrolled   bool
		want       bool
		wantErr    func(error) bool
	}{
		{"closed off", true, "good", true, true, nil},
		{"still open", false, "good", true, false, nil},
		{"not enrolled yet", true, "good", false, false, func(err error) bool { return errors.Is(err, ErrNotEnrolled) }},
		{"a credential HQ does not know", true, "revoked", true, false, func(err error) bool { return refusedAs(err, "mate_credential_required") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := selfHQ(t, tt.closedOff)
			path := filepath.Join(t.TempDir(), "enrollment.json")
			if tt.enrolled {
				if err := SaveEnrollment(path, Enrollment{HQ: srv.URL, HQProjectID: "hq1", ProjectID: "p-mate", Credential: tt.credential}); err != nil {
					t.Fatal(err)
				}
			}
			got, err := ReadClosedOff(srv.Client(), path)(context.Background())
			if (tt.wantErr == nil) != (err == nil) || (tt.wantErr != nil && !tt.wantErr(err)) {
				t.Fatalf("err = %v", err)
			}
			if got != tt.want {
				t.Errorf("closed off = %v, want %v", got, tt.want)
			}
		})
	}
}
