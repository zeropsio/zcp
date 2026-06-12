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
			hostname:      "zcp",
			projectID:     "Ul8Eyr4DTme8fAMKcYSFaw",
			wantContainer: true,
			wantService:   "zcp",
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
			t.Setenv("ZCP_AUTHORING", "")

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
			if got.Authoring {
				t.Errorf("Authoring = true with ZCP_AUTHORING unset, want false")
			}
		})
	}
}

// TestDetect_Authoring pins the ZCP_AUTHORING gate read — exactly "1"
// enables, regardless of container detection (authoring runs locally too).
func TestDetect_Authoring(t *testing.T) {
	tests := []struct {
		name      string
		env       string
		serviceID string
		want      bool
	}{
		{name: "1 enables (local)", env: "1", serviceID: "", want: true},
		{name: "1 enables (container)", env: "1", serviceID: "abc", want: true},
		{name: "empty disables", env: "", serviceID: "abc", want: false},
		{name: "0 disables", env: "0", serviceID: "abc", want: false},
		{name: "true disables (must be exactly 1)", env: "true", serviceID: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("serviceId", tt.serviceID)
			t.Setenv("hostname", "")
			t.Setenv("projectId", "")
			t.Setenv("ZCP_AUTHORING", tt.env)
			if got := Detect().Authoring; got != tt.want {
				t.Errorf("Authoring = %v, want %v (ZCP_AUTHORING=%q)", got, tt.want, tt.env)
			}
		})
	}
}
