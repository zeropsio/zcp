// Tests for: plans/analysis/ops.md § subdomain
package ops

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestBuildSubdomainURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		hostname      string
		subdomainHost string
		port          int
		want          string
	}{
		{
			name:          "normal with port 3000",
			hostname:      "app",
			subdomainHost: "1df2.prg1.zerops.app",
			port:          3000,
			want:          "https://app-1df2-3000.prg1.zerops.app",
		},
		{
			name:          "port 80 omits port suffix",
			hostname:      "web",
			subdomainHost: "1df2.prg1.zerops.app",
			port:          80,
			want:          "https://web-1df2.prg1.zerops.app",
		},
		{
			name:          "bare prefix without dot returns empty",
			hostname:      "app",
			subdomainHost: "1df2",
			port:          3000,
			want:          "",
		},
		{
			name:          "empty subdomainHost returns empty",
			hostname:      "app",
			subdomainHost: "",
			port:          3000,
			want:          "",
		},
		{
			name:          "trailing dot only returns empty",
			hostname:      "app",
			subdomainHost: "1df2.",
			port:          3000,
			want:          "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := BuildSubdomainURL(tt.hostname, tt.subdomainHost, tt.port)
			if got != tt.want {
				t.Errorf("BuildSubdomainURL(%q, %q, %d) = %q, want %q",
					tt.hostname, tt.subdomainHost, tt.port, got, tt.want)
			}
		})
	}
}

func TestParseSubdomainDomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "full zerops subdomain URL",
			url:  "https://app-1df2-3000.prg1.zerops.app",
			want: "prg1.zerops.app",
		},
		{
			name: "port 80 URL without port suffix",
			url:  "https://web-1df2.prg1.zerops.app",
			want: "prg1.zerops.app",
		},
		{
			name: "URL with trailing slash",
			url:  "https://app-1df2-3000.prg1.zerops.app/",
			want: "prg1.zerops.app",
		},
		{
			name: "empty string",
			url:  "",
			want: "",
		},
		{
			name: "no dot after hostname part",
			url:  "https://app-1df2",
			want: "",
		},
		{
			name: "URL with path",
			url:  "https://app-1df2-3000.prg1.zerops.app/health",
			want: "prg1.zerops.app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseSubdomainDomain(tt.url)
			if got != tt.want {
				t.Errorf("parseSubdomainDomain(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestSubdomain_EnableReturnsUrls(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mock     *platform.Mock
		hostname string
		action   string
		wantUrls []string
	}{
		{
			name: "enable with single port 3000",
			mock: platform.NewMock().
				WithServices([]platform.ServiceStack{
					{ID: "svc-1", Name: "app", ProjectID: "proj-1",
						Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}}},
				}).
				WithService(&platform.ServiceStack{
					ID: "svc-1", Name: "app", ProjectID: "proj-1",
					Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}},
				}).
				WithProject(&platform.Project{
					ID: "proj-1", Name: "myproject", Status: "ACTIVE",
					SubdomainHost: "1df2.prg1.zerops.app",
				}),
			hostname: "app",
			action:   "enable",
			wantUrls: []string{"https://app-1df2-3000.prg1.zerops.app"},
		},
		{
			name: "enable with multiple ports",
			mock: platform.NewMock().
				WithServices([]platform.ServiceStack{
					{ID: "svc-1", Name: "app", ProjectID: "proj-1",
						Ports: []platform.Port{
							{Port: 3000, Protocol: "tcp"},
							{Port: 8080, Protocol: "tcp"},
						}},
				}).
				WithService(&platform.ServiceStack{
					ID: "svc-1", Name: "app", ProjectID: "proj-1",
					Ports: []platform.Port{
						{Port: 3000, Protocol: "tcp"},
						{Port: 8080, Protocol: "tcp"},
					},
				}).
				WithProject(&platform.Project{
					ID: "proj-1", Name: "myproject", Status: "ACTIVE",
					SubdomainHost: "1df2.prg1.zerops.app",
				}),
			hostname: "app",
			action:   "enable",
			wantUrls: []string{
				"https://app-1df2-3000.prg1.zerops.app",
				"https://app-1df2-8080.prg1.zerops.app",
			},
		},
		{
			name: "enable with port 80 omits suffix",
			mock: platform.NewMock().
				WithServices([]platform.ServiceStack{
					{ID: "svc-1", Name: "web", ProjectID: "proj-1",
						Ports: []platform.Port{{Port: 80, Protocol: "tcp"}}},
				}).
				WithService(&platform.ServiceStack{
					ID: "svc-1", Name: "web", ProjectID: "proj-1",
					Ports: []platform.Port{{Port: 80, Protocol: "tcp"}},
				}).
				WithProject(&platform.Project{
					ID: "proj-1", Name: "myproject", Status: "ACTIVE",
					SubdomainHost: "1df2.prg1.zerops.app",
				}),
			hostname: "web",
			action:   "enable",
			wantUrls: []string{"https://web-1df2.prg1.zerops.app"},
		},
		{
			name: "already_enabled still returns URLs",
			mock: platform.NewMock().
				WithServices([]platform.ServiceStack{
					{ID: "svc-1", Name: "app", ProjectID: "proj-1",
						Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}}},
				}).
				WithService(&platform.ServiceStack{
					ID: "svc-1", Name: "app", ProjectID: "proj-1",
					Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}},
				}).
				WithProject(&platform.Project{
					ID: "proj-1", Name: "myproject", Status: "ACTIVE",
					SubdomainHost: "1df2.prg1.zerops.app",
				}).
				WithError("EnableSubdomainAccess", &platform.PlatformError{
					Code:    "SUBDOMAIN_ALREADY_ENABLED",
					Message: "subdomain already enabled",
				}),
			hostname: "app",
			action:   "enable",
			wantUrls: []string{"https://app-1df2-3000.prg1.zerops.app"},
		},
		{
			name: "disable returns nil URLs",
			mock: platform.NewMock().
				WithServices([]platform.ServiceStack{
					{ID: "svc-1", Name: "app", ProjectID: "proj-1",
						Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}}},
				}).
				WithProject(&platform.Project{
					ID: "proj-1", Name: "myproject", Status: "ACTIVE",
					SubdomainHost: "1df2.prg1.zerops.app",
				}),
			hostname: "app",
			action:   "disable",
			wantUrls: nil,
		},
		{
			name: "enable with no ports returns nil URLs",
			mock: platform.NewMock().
				WithServices([]platform.ServiceStack{
					{ID: "svc-1", Name: "app", ProjectID: "proj-1"},
				}).
				WithService(&platform.ServiceStack{
					ID: "svc-1", Name: "app", ProjectID: "proj-1",
				}).
				WithProject(&platform.Project{
					ID: "proj-1", Name: "myproject", Status: "ACTIVE",
					SubdomainHost: "1df2.prg1.zerops.app",
				}),
			hostname: "app",
			action:   "enable",
			wantUrls: nil,
		},
		{
			name: "bare prefix falls back to zeropsSubdomain env var",
			mock: platform.NewMock().
				WithServices([]platform.ServiceStack{
					{ID: "svc-1", Name: "app", ProjectID: "proj-1",
						Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}}},
				}).
				WithService(&platform.ServiceStack{
					ID: "svc-1", Name: "app", ProjectID: "proj-1",
					Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}},
				}).
				WithProject(&platform.Project{
					ID: "proj-1", Name: "myproject", Status: "ACTIVE",
					SubdomainHost: "1df2",
				}).
				WithServiceEnv("svc-1", []platform.EnvVar{
					{ID: "env-1", Key: "zeropsSubdomain", Content: "https://app-1df2-3000.prg1.zerops.app"},
				}),
			hostname: "app",
			action:   "enable",
			wantUrls: []string{"https://app-1df2-3000.prg1.zerops.app"},
		},
		{
			name: "bare prefix with multiple ports uses env var domain",
			mock: platform.NewMock().
				WithServices([]platform.ServiceStack{
					{ID: "svc-1", Name: "app", ProjectID: "proj-1",
						Ports: []platform.Port{
							{Port: 3000, Protocol: "tcp"},
							{Port: 8080, Protocol: "tcp"},
						}},
				}).
				WithService(&platform.ServiceStack{
					ID: "svc-1", Name: "app", ProjectID: "proj-1",
					Ports: []platform.Port{
						{Port: 3000, Protocol: "tcp"},
						{Port: 8080, Protocol: "tcp"},
					},
				}).
				WithProject(&platform.Project{
					ID: "proj-1", Name: "myproject", Status: "ACTIVE",
					SubdomainHost: "1df2",
				}).
				WithServiceEnv("svc-1", []platform.EnvVar{
					{ID: "env-1", Key: "zeropsSubdomain", Content: "https://app-1df2-3000.prg1.zerops.app"},
				}),
			hostname: "app",
			action:   "enable",
			wantUrls: []string{
				"https://app-1df2-3000.prg1.zerops.app",
				"https://app-1df2-8080.prg1.zerops.app",
			},
		},
		{
			name: "bare prefix without env var returns nil URLs",
			mock: platform.NewMock().
				WithServices([]platform.ServiceStack{
					{ID: "svc-1", Name: "app", ProjectID: "proj-1",
						Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}}},
				}).
				WithService(&platform.ServiceStack{
					ID: "svc-1", Name: "app", ProjectID: "proj-1",
					Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}},
				}).
				WithProject(&platform.Project{
					ID: "proj-1", Name: "myproject", Status: "ACTIVE",
					SubdomainHost: "1df2",
				}),
			hostname: "app",
			action:   "enable",
			wantUrls: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Subdomain(context.Background(), tt.mock, "proj-1", tt.hostname, tt.action)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantUrls == nil {
				if result.SubdomainUrls != nil {
					t.Errorf("expected nil SubdomainUrls, got %v", result.SubdomainUrls)
				}
				return
			}

			if len(result.SubdomainUrls) != len(tt.wantUrls) {
				t.Fatalf("expected %d URLs, got %d: %v", len(tt.wantUrls), len(result.SubdomainUrls), result.SubdomainUrls)
			}
			for i, want := range tt.wantUrls {
				if result.SubdomainUrls[i] != want {
					t.Errorf("URL[%d] = %q, want %q", i, result.SubdomainUrls[i], want)
				}
			}
		})
	}
}

func TestSubdomain(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1"},
		{ID: "svc-2", Name: "db", ProjectID: "proj-1"},
	}

	tests := []struct {
		name       string
		mock       *platform.Mock
		hostname   string
		action     string
		wantStatus string
		wantProc   bool
		wantErr    string
	}{
		{
			name:     "Enable_Success",
			mock:     platform.NewMock().WithServices(services),
			hostname: "api",
			action:   "enable",
			wantProc: true,
		},
		{
			name:     "Disable_Success",
			mock:     platform.NewMock().WithServices(services),
			hostname: "api",
			action:   "disable",
			wantProc: true,
		},
		{
			name: "Enable_AlreadyEnabled",
			mock: platform.NewMock().WithServices(services).
				WithError("EnableSubdomainAccess", &platform.PlatformError{
					Code:    "SUBDOMAIN_ALREADY_ENABLED",
					Message: "subdomain already enabled",
				}),
			hostname:   "api",
			action:     "enable",
			wantStatus: "already_enabled",
		},
		{
			name: "Disable_AlreadyDisabled",
			mock: platform.NewMock().WithServices(services).
				WithError("DisableSubdomainAccess", &platform.PlatformError{
					Code:    "SUBDOMAIN_ALREADY_DISABLED",
					Message: "subdomain already disabled",
				}),
			hostname:   "api",
			action:     "disable",
			wantStatus: "already_disabled",
		},
		{
			name:     "InvalidAction",
			mock:     platform.NewMock().WithServices(services),
			hostname: "api",
			action:   "toggle",
			wantErr:  platform.ErrInvalidParameter,
		},
		{
			name:     "ServiceNotFound",
			mock:     platform.NewMock().WithServices(services),
			hostname: "missing",
			action:   "enable",
			wantErr:  platform.ErrServiceNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Subdomain(context.Background(), tt.mock, "proj-1", tt.hostname, tt.action)

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
			if result.Hostname != tt.hostname {
				t.Errorf("expected hostname=%s, got %s", tt.hostname, result.Hostname)
			}
			if result.Action != tt.action {
				t.Errorf("expected action=%s, got %s", tt.action, result.Action)
			}
			if tt.wantStatus != "" && result.Status != tt.wantStatus {
				t.Errorf("expected status=%s, got %s", tt.wantStatus, result.Status)
			}
			if tt.wantProc && result.Process == nil {
				t.Error("expected non-nil process")
			}
		})
	}
}
