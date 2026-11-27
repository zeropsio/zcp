package ops

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// witness: internal/ops/events.go | candidate.Name = version.Name → candidate.Name = ""
func TestEvents_EVName_ServiceVersionsNameTheirHeads(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, processID, version, want string
		readFails                      bool
	}{
		{"review head", "v1", "zcp abcdef123", "zcp abcdef123", false},
		{"branch", "v1", "main 1234567", "main 1234567", false},
		{"another version", "other", "zcp wrong", "", false},
		{"process read fails", "v1", "zcp abcdef123", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc", Name: "stage"}}).
				WithServiceAppVersions("svc", []platform.AppVersionEvent{{ID: "v1", Status: "ACTIVE"}}).
				WithProjectProcesses([]platform.Process{{ActionName: "stack.build", AppVersion: &platform.ProcessAppVersion{ID: tc.processID, Name: tc.version}}})
			if tc.readFails {
				client.WithError("GetProjectProcessesDirect", errors.New("unavailable"))
			}
			result, err := Events(context.Background(), client, nil, "project", "stage", 10)
			if err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			var answer struct {
				AppVersions []struct {
					Name string `json:"name"`
				} `json:"appVersions"`
			}
			if err := json.Unmarshal(body, &answer); err != nil {
				t.Fatal(err)
			}
			if len(answer.AppVersions) != 1 || answer.AppVersions[0].Name != tc.want {
				t.Errorf("versions = %s, want name %q", body, tc.want)
			}
		})
	}
}
