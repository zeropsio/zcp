package hq

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
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

// selfSample is HQ's answer to GET /api/mate/self for a Mate in an
// application, with an open change in one repository and a merged and a
// closed one in another.
const selfSample = `{"projectId":"p-mate","name":"Ada","face":"face-1","standupRequestedBy":null,"closedOff":false,` +
	`"appId":"0b6e7c1e-8f7a-4a52-9d1c-2f1f5d0a3c11","changes":[` +
	`{"repo":"apidev","number":2,"state":"open","head":"1111111111111111111111111111111111111111","mergedSha":null,"landedHead":null},` +
	`{"repo":"appdev","number":2,"state":"merged","head":"2222222222222222222222222222222222222222",` +
	`"mergedSha":"3333333333333333333333333333333333333333","landedHead":"2222222222222222222222222222222222222222"},` +
	`{"repo":"appdev","number":1,"state":"closed","head":null,"mergedSha":null,"landedHead":null}]}`

func TestClient_Self_CarriesTheMatesApplicationAndItsChanges(t *testing.T) {
	t.Parallel()
	client, calls := answering(t, selfSample)

	state, err := client.Self(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sha := func(s string) *string { return &s }
	want := MateState{
		ProjectID: "p-mate", Name: "Ada", Face: "face-1",
		AppID: sha("0b6e7c1e-8f7a-4a52-9d1c-2f1f5d0a3c11"),
		Changes: []MateChange{
			{Repo: "apidev", Number: 2, State: ChangeOpen, Head: sha("1111111111111111111111111111111111111111")},
			{Repo: "appdev", Number: 2, State: ChangeMerged, Head: sha("2222222222222222222222222222222222222222"),
				MergedSha: sha("3333333333333333333333333333333333333333"), LandedHead: sha("2222222222222222222222222222222222222222")},
			{Repo: "appdev", Number: 1, State: ChangeClosed},
		},
	}
	if !reflect.DeepEqual(state, want) {
		t.Errorf("state = %+v\nwant %+v", state, want)
	}
	wantCall := hqCall{Method: http.MethodGet, Path: "/api/mate/self", Authorization: "Mate good"}
	if len(*calls) != 1 || (*calls)[0] != wantCall {
		t.Errorf("calls = %+v, want [%+v]", *calls, wantCall)
	}
}
