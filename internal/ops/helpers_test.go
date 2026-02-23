// Tests for: plans/analysis/ops.md $ helpers
package ops

import (
	"math"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestResolveServiceID(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING"},
		{ID: "svc-2", Name: "db", ProjectID: "proj-1", Status: "RUNNING"},
	}

	tests := []struct {
		name      string
		services  []platform.ServiceStack
		projectID string
		hostname  string
		wantID    string
		wantErr   string
	}{
		{
			name:      "Found",
			services:  services,
			projectID: "proj-1",
			hostname:  "api",
			wantID:    "svc-1",
		},
		{
			name:      "NotFound",
			services:  services,
			projectID: "proj-1",
			hostname:  "missing",
			wantErr:   platform.ErrServiceNotFound,
		},
		{
			name:      "EmptyList",
			services:  []platform.ServiceStack{},
			projectID: "proj-1",
			hostname:  "api",
			wantErr:   platform.ErrServiceNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, err := resolveServiceID(tt.services, tt.hostname)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				pe, ok := err.(*platform.PlatformError)
				if !ok {
					t.Fatalf("expected *PlatformError, got %T: %v", err, err)
				}
				if pe.Code != tt.wantErr {
					t.Fatalf("expected code %s, got %s", tt.wantErr, pe.Code)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if svc.ID != tt.wantID {
				t.Fatalf("expected ID %s, got %s", tt.wantID, svc.ID)
			}
		})
	}
}

func TestParseSince(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantDelta time.Duration // expected offset from now (negative)
		wantExact time.Time     // for ISO8601 case
		wantErr   bool
	}{
		{
			name:      "Minutes",
			input:     "30m",
			wantDelta: -30 * time.Minute,
		},
		{
			name:      "Hours",
			input:     "1h",
			wantDelta: -1 * time.Hour,
		},
		{
			name:      "Days",
			input:     "7d",
			wantDelta: -7 * 24 * time.Hour,
		},
		{
			name:      "ISO8601",
			input:     "2024-01-01T00:00:00Z",
			wantExact: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "Empty",
			input:     "",
			wantDelta: -1 * time.Hour,
		},
		{
			name:    "Invalid",
			input:   "abc",
			wantErr: true,
		},
		{
			name:    "OutOfRange_Hours",
			input:   "200h",
			wantErr: true,
		},
		{
			name:    "OutOfRange_Minutes",
			input:   "1500m",
			wantErr: true,
		},
		{
			name:    "OutOfRange_Days",
			input:   "31d",
			wantErr: true,
		},
		{
			name:    "Zero_Minutes",
			input:   "0m",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseSince(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !tt.wantExact.IsZero() {
				if !got.Equal(tt.wantExact) {
					t.Fatalf("expected %v, got %v", tt.wantExact, got)
				}
				return
			}

			// For duration-based tests, check that result is within tolerance
			expected := time.Now().Add(tt.wantDelta)
			diff := math.Abs(float64(got.Sub(expected)))
			if diff > float64(2*time.Second) {
				t.Fatalf("result %v too far from expected %v (diff: %v)", got, expected, time.Duration(diff))
			}
		})
	}
}

func TestParseEnvPairs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []string
		wantKeys []string
		wantVals []string
		wantErr  string
	}{
		{
			name:     "Valid",
			input:    []string{"KEY=value", "K2=v=2"},
			wantKeys: []string{"KEY", "K2"},
			wantVals: []string{"value", "v=2"},
		},
		{
			name:     "EmptyValue",
			input:    []string{"KEY="},
			wantKeys: []string{"KEY"},
			wantVals: []string{""},
		},
		{
			name:    "NoEquals",
			input:   []string{"NOVALUE"},
			wantErr: platform.ErrInvalidEnvFormat,
		},
		{
			name:    "EmptyKey",
			input:   []string{"=value"},
			wantErr: platform.ErrInvalidEnvFormat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pairs, err := parseEnvPairs(tt.input)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				pe, ok := err.(*platform.PlatformError)
				if !ok {
					t.Fatalf("expected *PlatformError, got %T: %v", err, err)
				}
				if pe.Code != tt.wantErr {
					t.Fatalf("expected code %s, got %s", tt.wantErr, pe.Code)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(pairs) != len(tt.wantKeys) {
				t.Fatalf("expected %d pairs, got %d", len(tt.wantKeys), len(pairs))
			}

			for i, p := range pairs {
				if p.Key != tt.wantKeys[i] {
					t.Errorf("pair[%d].Key = %q, want %q", i, p.Key, tt.wantKeys[i])
				}
				if p.Value != tt.wantVals[i] {
					t.Errorf("pair[%d].Value = %q, want %q", i, p.Value, tt.wantVals[i])
				}
			}
		})
	}
}

func TestFindServiceByHostname(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api"},
		{ID: "svc-2", Name: "db"},
	}

	tests := []struct {
		name     string
		hostname string
		wantID   string
		wantNil  bool
	}{
		{name: "Found", hostname: "api", wantID: "svc-1"},
		{name: "NotFound", hostname: "missing", wantNil: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := findServiceByHostname(services, tt.hostname)

			if tt.wantNil {
				if svc != nil {
					t.Fatalf("expected nil, got %+v", svc)
				}
				return
			}

			if svc == nil {
				t.Fatal("expected non-nil service")
			}
			if svc.ID != tt.wantID {
				t.Fatalf("expected ID %s, got %s", tt.wantID, svc.ID)
			}
		})
	}
}

func TestListHostnames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		services []platform.ServiceStack
		want     string
	}{
		{
			name:     "Multiple",
			services: []platform.ServiceStack{{Name: "api"}, {Name: "db"}},
			want:     "api, db",
		},
		{
			name:     "Single",
			services: []platform.ServiceStack{{Name: "api"}},
			want:     "api",
		},
		{
			name:     "Empty",
			services: []platform.ServiceStack{},
			want:     "(none)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := listHostnames(tt.services)
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestEnvVarsToMaps_FiltersZeropsSubdomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		envs     []platform.EnvVar
		wantKeys []string
	}{
		{
			name: "filters zeropsSubdomain",
			envs: []platform.EnvVar{
				{ID: "e1", Key: "PORT", Content: "3000"},
				{ID: "e2", Key: "zeropsSubdomain", Content: "https://app-1df2-3000.prg1.zerops.app"},
				{ID: "e3", Key: "HOST", Content: "0.0.0.0"},
			},
			wantKeys: []string{"PORT", "HOST"},
		},
		{
			name: "no zeropsSubdomain present",
			envs: []platform.EnvVar{
				{ID: "e1", Key: "PORT", Content: "3000"},
				{ID: "e2", Key: "HOST", Content: "0.0.0.0"},
			},
			wantKeys: []string{"PORT", "HOST"},
		},
		{
			name:     "empty envs",
			envs:     []platform.EnvVar{},
			wantKeys: []string{},
		},
		{
			name: "only zeropsSubdomain",
			envs: []platform.EnvVar{
				{ID: "e1", Key: "zeropsSubdomain", Content: "https://..."},
			},
			wantKeys: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := envVarsToMaps(tt.envs)
			if len(result) != len(tt.wantKeys) {
				t.Fatalf("expected %d envs, got %d", len(tt.wantKeys), len(result))
			}
			for i, wantKey := range tt.wantKeys {
				if result[i]["key"] != wantKey {
					t.Errorf("env[%d] key = %v, want %s", i, result[i]["key"], wantKey)
				}
			}
		})
	}
}

func TestFindEnvIDByKey(t *testing.T) {
	t.Parallel()

	envs := []platform.EnvVar{
		{ID: "env-1", Key: "DB_HOST", Content: "localhost"},
		{ID: "env-2", Key: "DB_PORT", Content: "5432"},
	}

	tests := []struct {
		name   string
		key    string
		wantID string
	}{
		{name: "Found", key: "DB_HOST", wantID: "env-1"},
		{name: "NotFound", key: "MISSING", wantID: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := findEnvIDByKey(envs, tt.key)
			if got != tt.wantID {
				t.Fatalf("expected %q, got %q", tt.wantID, got)
			}
		})
	}
}
