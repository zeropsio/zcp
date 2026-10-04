package hq

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestClient_Signers_UsesOnlyTheEnrolledProjectAndOneAttempt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, project, answer string
		status                int
		want                  map[string]string
		wantErr               string
		calls                 int
	}{
		{"HQ signers", "p-mate", `{"projectId":"p-mate","signers":{"codex":"u-bo","claude-code":"u-ada"}}`, 200, map[string]string{"codex": "u-bo", "claude-code": "u-ada"}, "", 1},
		{"HQ omits an empty map", "p-mate", `{"projectId":"p-mate"}`, 200, nil, "", 1},
		{"other container project", "p-other", `{}`, 200, nil, "enrollment", 0},
		{"other answer project", "p-mate", `{"projectId":"p-other","signers":{"codex":"u-bo"}}`, 200, nil, "project", 1},
		{"no answer", "p-mate", ``, 204, nil, "project", 1},
		{"HQ unavailable", "p-mate", `{"code":"not_active"}`, 503, nil, "unavailable", 1},
		{"HQ refuses", "p-mate", `{"code":"mate_not_found"}`, 404, nil, "mate_not_found", 1},
		{"malformed answer", "p-mate", `{`, 200, nil, "malformed", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client, calls := answering(t, tt.answer)
			client.call.http = statusDoer{Doer: client.call.http, status: tt.status}
			client.enrollment.ProjectID = "p-mate"
			got, err := client.Signers(context.Background(), tt.project)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("signers = %v, want %v", got, tt.want)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want %q", err, tt.wantErr)
			}
			if len(*calls) != tt.calls {
				t.Fatalf("requests = %d, want %d", len(*calls), tt.calls)
			}
			for _, call := range *calls {
				if call.Method != http.MethodGet || call.Path != "/api/mate/self" || call.Authorization != "Mate good" {
					t.Errorf("request = %+v", call)
				}
			}
		})
	}
}

type statusDoer struct {
	Doer
	status int
}

func TestClient_SignersInput_ChangesOnlyWithItsAuthority(t *testing.T) {
	t.Parallel()
	base := Enrollment{HQ: "https://hq.test", ProjectID: "p-mate", Credential: "private-credential"}
	client := Client{enrollment: base}
	want := client.SignersInput()
	if len(want) != 64 || strings.Contains(want, base.Credential) {
		t.Fatalf("input must be an opaque SHA-256 fingerprint")
	}
	for _, tt := range []struct {
		name       string
		enrollment Enrollment
		changed    bool
	}{
		{"same", base, false},
		{"key named", Enrollment{HQ: base.HQ, ProjectID: base.ProjectID, Credential: base.Credential, KeyTokenID: "key-id"}, false},
		{"credential", Enrollment{HQ: base.HQ, ProjectID: base.ProjectID, Credential: "rotated"}, true},
		{"HQ", Enrollment{HQ: "https://new.test", ProjectID: base.ProjectID, Credential: base.Credential}, true},
		{"project", Enrollment{HQ: base.HQ, ProjectID: "other", Credential: base.Credential}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := (Client{enrollment: tt.enrollment}).SignersInput(); (got != want) != tt.changed {
				t.Errorf("input changed = %v, want %v", got != want, tt.changed)
			}
		})
	}
}

func (d statusDoer) Do(req *http.Request) (*http.Response, error) {
	resp, err := d.Doer.Do(req)
	if resp != nil {
		resp.StatusCode = d.status
	}
	return resp, err
}
