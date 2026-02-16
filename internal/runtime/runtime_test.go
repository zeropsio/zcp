// Tests for: internal/runtime — centralized Zerops container detection.
// NOT parallel — subtests use t.Setenv which modifies process-global state.
package runtime

import "testing"

func TestDetect(t *testing.T) {
	tests := []struct {
		name          string
		serviceID     string
		hostname      string
		projectID     string
		wantContainer bool
		wantService   string
		wantServiceID string
		wantProjectID string
	}{
		{
			name:          "full container env",
			serviceID:     "hffVp74hRXiVpkxyFRRmiQ",
			hostname:      "zcpx",
			projectID:     "Ul8Eyr4DTme8fAMKcYSFaw",
			wantContainer: true,
			wantService:   "zcpx",
			wantServiceID: "hffVp74hRXiVpkxyFRRmiQ",
			wantProjectID: "Ul8Eyr4DTme8fAMKcYSFaw",
		},
		{
			name:          "serviceId present but hostname empty",
			serviceID:     "abc123",
			hostname:      "",
			projectID:     "",
			wantContainer: true,
			wantService:   "",
			wantServiceID: "abc123",
			wantProjectID: "",
		},
		{
			name:          "no env vars (local dev)",
			serviceID:     "",
			hostname:      "macbook",
			projectID:     "",
			wantContainer: false,
			wantService:   "",
			wantServiceID: "",
			wantProjectID: "",
		},
		{
			name:          "hostname set but no serviceId",
			serviceID:     "",
			hostname:      "my-machine",
			projectID:     "",
			wantContainer: false,
			wantService:   "",
			wantServiceID: "",
			wantProjectID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("serviceId", tt.serviceID)
			t.Setenv("hostname", tt.hostname)
			t.Setenv("projectId", tt.projectID)

			got := Detect()

			if got.InContainer != tt.wantContainer {
				t.Errorf("InContainer = %v, want %v", got.InContainer, tt.wantContainer)
			}
			if got.ServiceName != tt.wantService {
				t.Errorf("ServiceName = %q, want %q", got.ServiceName, tt.wantService)
			}
			if got.ServiceID != tt.wantServiceID {
				t.Errorf("ServiceID = %q, want %q", got.ServiceID, tt.wantServiceID)
			}
			if got.ProjectID != tt.wantProjectID {
				t.Errorf("ProjectID = %q, want %q", got.ProjectID, tt.wantProjectID)
			}
		})
	}
}
