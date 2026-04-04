package ops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestEnvGenerateDotenv_ResolvesRefs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		zeropsYml    string
		hostname     string
		serviceEnvs  map[string][]platform.EnvVar
		projectEnvs  []platform.EnvVar
		wantVars     int
		wantServices int
		wantContains []string
		wantErr      string
	}{
		{
			name: "cross-service references resolved",
			zeropsYml: `zerops:
  - setup: app
    envVariables:
      DB_HOST: ${db_hostname}
      DB_PORT: ${db_port}
`,
			hostname: "app",
			serviceEnvs: map[string][]platform.EnvVar{
				"db": {
					{ID: "e1", Key: "hostname", Content: "db"},
					{ID: "e2", Key: "port", Content: "5432"},
				},
			},
			wantVars:     2,
			wantServices: 1,
			wantContains: []string{"DB_HOST=db", "DB_PORT=5432"},
		},
		{
			name: "project-level env vars appended",
			zeropsYml: `zerops:
  - setup: app
    envVariables:
      DB_HOST: ${db_hostname}
`,
			hostname: "app",
			serviceEnvs: map[string][]platform.EnvVar{
				"db": {
					{ID: "e1", Key: "hostname", Content: "db"},
				},
			},
			projectEnvs: []platform.EnvVar{
				{ID: "pe1", Key: "APP_KEY", Content: "base64:secretkey"},
			},
			wantVars:     2, // 1 from zerops.yaml + 1 project
			wantServices: 1,
			wantContains: []string{"DB_HOST=db", "APP_KEY=base64:secretkey"},
		},
		{
			name: "static value passthrough",
			zeropsYml: `zerops:
  - setup: app
    envVariables:
      NODE_ENV: production
      DB_HOST: ${db_hostname}
`,
			hostname: "app",
			serviceEnvs: map[string][]platform.EnvVar{
				"db": {
					{ID: "e1", Key: "hostname", Content: "db"},
				},
			},
			wantVars:     2,
			wantServices: 1,
			wantContains: []string{"NODE_ENV=production", "DB_HOST=db"},
		},
		{
			name: "zerops.yaml envVariable takes precedence over project env",
			zeropsYml: `zerops:
  - setup: app
    envVariables:
      SHARED_KEY: custom_value
`,
			hostname: "app",
			projectEnvs: []platform.EnvVar{
				{ID: "pe1", Key: "SHARED_KEY", Content: "project_value"},
			},
			wantVars:     1,
			wantServices: 0,
			wantContains: []string{"SHARED_KEY=custom_value"},
		},
		{
			name: "missing service hostname",
			zeropsYml: `zerops:
  - setup: app
    envVariables:
      DB_HOST: ${db_hostname}
`,
			hostname: "",
			wantErr:  "serviceHostname is required",
		},
		{
			name: "no setup entry for hostname",
			zeropsYml: `zerops:
  - setup: other
    envVariables:
      FOO: bar
`,
			hostname: "app",
			wantErr:  "no setup entry",
		},
		{
			name: "no envVariables in entry",
			zeropsYml: `zerops:
  - setup: app
    build:
      base: nodejs@22
`,
			hostname: "app",
			wantErr:  "no envVariables",
		},
		{
			name: "unresolved reference",
			zeropsYml: `zerops:
  - setup: app
    envVariables:
      DB_HOST: ${db_hostname}
`,
			hostname: "app",
			serviceEnvs: map[string][]platform.EnvVar{
				"db": {
					{ID: "e1", Key: "port", Content: "5432"},
				},
			},
			wantErr: "could not resolve",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(tmpDir, "zerops.yaml"), []byte(tt.zeropsYml), 0644); err != nil {
				t.Fatalf("write zerops.yaml: %v", err)
			}

			services := make([]platform.ServiceStack, 0, 1+len(tt.serviceEnvs))
			services = append(services, platform.ServiceStack{
				ID: "svc-app", Name: "app", ProjectID: "proj-1", Status: "RUNNING",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
			})
			for svcName := range tt.serviceEnvs {
				services = append(services, platform.ServiceStack{
					ID: "svc-" + svcName, Name: svcName, ProjectID: "proj-1", Status: "RUNNING",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"},
				})
			}

			mock := platform.NewMock().
				WithProject(&platform.Project{ID: "proj-1", Name: "test", Status: statusActive}).
				WithServices(services)
			for svcName, envs := range tt.serviceEnvs {
				mock = mock.WithServiceEnv("svc-"+svcName, envs)
			}
			if tt.projectEnvs != nil {
				mock = mock.WithProjectEnv(tt.projectEnvs)
			}

			result, err := EnvGenerateDotenv(context.Background(), mock, "proj-1", tt.hostname, tmpDir)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got: %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.Variables != tt.wantVars {
				t.Errorf("variables = %d, want %d", result.Variables, tt.wantVars)
			}
			if result.Services != tt.wantServices {
				t.Errorf("services = %d, want %d", result.Services, tt.wantServices)
			}

			envContent, err := os.ReadFile(filepath.Join(tmpDir, ".env"))
			if err != nil {
				t.Fatalf("read .env: %v", err)
			}
			content := string(envContent)

			for _, want := range tt.wantContains {
				if !strings.Contains(content, want) {
					t.Errorf(".env should contain %q, got:\n%s", want, content)
				}
			}

			if !strings.Contains(content, "Generated by ZCP") {
				t.Error(".env should contain header comment")
			}
		})
	}
}
