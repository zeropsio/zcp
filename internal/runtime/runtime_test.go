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

// TestDetect_MateEnabled pins the ZCP_MATE_ENABLED gate read. Unlike
// ZCP_AUTHORING (exactly "1"), this one is a service env an operator types by
// hand on a running service, so "true" is accepted next to "1" and the match
// is case-insensitive. Read regardless of container detection; only the
// container path acts on it.
func TestDetect_MateEnabled(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want bool
	}{
		{name: "1 enables", env: "1", want: true},
		{name: "true enables", env: "true", want: true},
		{name: "TRUE enables (case-insensitive)", env: "TRUE", want: true},
		{name: "surrounding space tolerated", env: " 1 ", want: true},
		{name: "unset disables", env: "", want: false},
		{name: "0 disables", env: "0", want: false},
		{name: "false disables", env: "false", want: false},
		{name: "anything else disables", env: "yes please", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("serviceId", "abc")
			t.Setenv("hostname", "zcp")
			t.Setenv("projectId", "pid")
			t.Setenv("ZCP_AUTHORING", "")
			t.Setenv("ZCP_MATE_ENABLED", tt.env)
			if got := Detect().MateEnabled; got != tt.want {
				t.Errorf("MateEnabled = %v, want %v (ZCP_MATE_ENABLED=%q)", got, tt.want, tt.env)
			}
		})
	}
}

// TestDetect_MateDisabled_ByDefault is the acceptance principle in one line: a
// container whose environment never mentions mate reads as disabled, so nothing
// mate-shaped installs, renders or binds.
func TestDetect_MateDisabled_ByDefault(t *testing.T) {
	t.Setenv("serviceId", "abc")
	t.Setenv("hostname", "zcp")
	t.Setenv("projectId", "pid")
	t.Setenv("ZCP_AUTHORING", "")
	t.Setenv("ZCP_MATE_ENABLED", "")
	if Detect().MateEnabled {
		t.Error("MateEnabled = true with ZCP_MATE_ENABLED unset, want false")
	}
}
