// Tests for: ops/deploy_validate.go — zerops.yml pre-deploy validation.
package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateZeropsYml_Parsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		hostname     string
		yml          string // empty = no file created
		wantWarnings int
		wantContains string // first warning must contain this
		noWarnings   bool   // expect zero warnings
	}{
		{
			name:         "missing file",
			hostname:     "appdev",
			yml:          "",
			wantWarnings: 1,
			wantContains: "not found",
		},
		{
			name:         "invalid YAML",
			hostname:     "appdev",
			yml:          "{{invalid yaml",
			wantWarnings: 1,
			wantContains: "invalid YAML",
		},
		{
			name:         "no zerops key",
			hostname:     "appdev",
			yml:          "something: else\n",
			wantWarnings: 1,
			wantContains: "no setup entries",
		},
		{
			name:     "no matching setup entry",
			hostname: "appdev",
			yml: `zerops:
  - setup: other
    build:
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 8080
`,
			wantWarnings: 1,
			wantContains: "no setup entry for hostname",
		},
		{
			name:     "missing run.start",
			hostname: "appdev",
			yml: `zerops:
  - setup: appdev
    build:
      deployFiles: [.]
    run:
      ports:
        - port: 8080
`,
			wantWarnings: 1,
			wantContains: "run.start is empty",
		},
		{
			name:     "missing run.ports",
			hostname: "appdev",
			yml: `zerops:
  - setup: appdev
    build:
      deployFiles: [.]
    run:
      start: node index.js
`,
			wantWarnings: 1,
			wantContains: "run.ports is empty",
		},
		{
			name:     "multiple issues",
			hostname: "appdev",
			yml: `zerops:
  - setup: appdev
    run: {}
`,
			wantWarnings: 3, // missing start, ports, and deployFiles
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runValidateTest(t, tt.hostname, tt.yml, tt.wantWarnings, tt.wantContains, tt.noWarnings)
		})
	}
}

func TestValidateZeropsYml_DeployFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		hostname     string
		yml          string
		wantWarnings int
		wantContains string
		noWarnings   bool
	}{
		{
			name:     "missing deployFiles",
			hostname: "appdev",
			yml: `zerops:
  - setup: appdev
    run:
      start: node index.js
      ports:
        - port: 8080
`,
			wantWarnings: 1,
			wantContains: "deployFiles is empty",
		},
		{
			name:     "dev without dot deployFiles",
			hostname: "appdev",
			yml: `zerops:
  - setup: appdev
    build:
      deployFiles: [dist]
    run:
      start: node dist/index.js
      ports:
        - port: 8080
`,
			wantWarnings: 1,
			wantContains: "dev service should use deployFiles: [.]",
		},
		{
			name:     "stage without dot deployFiles is fine",
			hostname: "appstage",
			yml: `zerops:
  - setup: appstage
    build:
      deployFiles: [dist]
    run:
      start: node dist/index.js
      ports:
        - port: 8080
`,
			noWarnings: true,
		},
		{
			name:     "valid dev config",
			hostname: "appdev",
			yml: `zerops:
  - setup: appdev
    build:
      deployFiles: [.]
    run:
      start: bun run index.ts
      ports:
        - port: 8080
`,
			noWarnings: true,
		},
		{
			name:     "dev with dot-slash deployFiles is valid",
			hostname: "appdev",
			yml: `zerops:
  - setup: appdev
    build:
      deployFiles: [./]
    run:
      start: bun run index.ts
      ports:
        - port: 8080
`,
			noWarnings: true,
		},
		{
			name:     "scalar deployFiles string is valid",
			hostname: "appdev",
			yml: `zerops:
  - setup: appdev
    build:
      deployFiles: ./
    run:
      start: bun run index.ts
      ports:
        - port: 8080
`,
			noWarnings: true,
		},
		{
			name:     "scalar deployFiles non-dot warns for dev",
			hostname: "appdev",
			yml: `zerops:
  - setup: appdev
    build:
      deployFiles: dist
    run:
      start: node dist/index.js
      ports:
        - port: 8080
`,
			wantWarnings: 1,
			wantContains: "dev service should use deployFiles: [.]",
		},
		{
			name:     "valid prod config with build output",
			hostname: "appstage",
			yml: `zerops:
  - setup: appstage
    build:
      buildCommands:
        - bun build src/index.ts --outdir dist
      deployFiles: [dist]
    run:
      start: bun dist/index.js
      ports:
        - port: 8080
`,
			noWarnings: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runValidateTest(t, tt.hostname, tt.yml, tt.wantWarnings, tt.wantContains, tt.noWarnings)
		})
	}
}

func TestValidateZeropsYml_HealthChecks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		hostname     string
		yml          string
		wantWarnings int
		wantContains string
		noWarnings   bool
	}{
		{
			name:     "dev with healthCheck warns",
			hostname: "appdev",
			yml: `zerops:
  - setup: appdev
    build:
      deployFiles: [.]
    run:
      start: bun run index.ts
      ports:
        - port: 8080
      healthCheck:
        httpGet:
          port: 8080
          path: /
`,
			wantWarnings: 1,
			wantContains: "dev service has run.healthCheck",
		},
		{
			name:     "dev with readinessCheck warns",
			hostname: "appdev",
			yml: `zerops:
  - setup: appdev
    build:
      deployFiles: [.]
    deploy:
      readinessCheck:
        httpGet:
          port: 8080
          path: /
    run:
      start: bun run index.ts
      ports:
        - port: 8080
`,
			wantWarnings: 1,
			wantContains: "dev service has deploy.readinessCheck",
		},
		{
			name:     "stage with healthCheck is fine",
			hostname: "appstage",
			yml: `zerops:
  - setup: appstage
    build:
      deployFiles: [dist]
    run:
      start: node dist/index.js
      ports:
        - port: 8080
      healthCheck:
        httpGet:
          port: 8080
          path: /
`,
			noWarnings: true,
		},
		{
			name:     "stage with readinessCheck is fine",
			hostname: "appstage",
			yml: `zerops:
  - setup: appstage
    build:
      deployFiles: [dist]
    deploy:
      readinessCheck:
        httpGet:
          port: 8080
          path: /
    run:
      start: node dist/index.js
      ports:
        - port: 8080
`,
			noWarnings: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runValidateTest(t, tt.hostname, tt.yml, tt.wantWarnings, tt.wantContains, tt.noWarnings)
		})
	}
}

func TestValidateZeropsYml_ImplicitWebServer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		hostname     string
		yml          string
		wantWarnings int
		wantContains string
		noWarnings   bool
	}{
		{
			name:     "php-nginx no start/ports is fine",
			hostname: "appdev",
			yml: `zerops:
  - setup: appdev
    build:
      deployFiles: [.]
    run:
      base: php-nginx@8.4
`,
			noWarnings: true,
		},
		{
			name:     "php-apache no start/ports is fine",
			hostname: "appdev",
			yml: `zerops:
  - setup: appdev
    build:
      deployFiles: [.]
    run:
      base: php-apache@8.3
`,
			noWarnings: true,
		},
		{
			name:     "nginx no start/ports is fine",
			hostname: "appstage",
			yml: `zerops:
  - setup: appstage
    build:
      deployFiles: [dist]
    run:
      base: nginx@1.22
`,
			noWarnings: true,
		},
		{
			name:     "static no start/ports is fine",
			hostname: "appstage",
			yml: `zerops:
  - setup: appstage
    build:
      deployFiles: [dist]
    run:
      base: static
`,
			noWarnings: true,
		},
		{
			name:     "php-nginx build.base fallback no start/ports is fine",
			hostname: "appdev",
			yml: `zerops:
  - setup: appdev
    build:
      base: php-nginx@8.4
      deployFiles: [.]
    run: {}
`,
			noWarnings: true,
		},
		{
			name:     "nodejs without start still warns",
			hostname: "appdev",
			yml: `zerops:
  - setup: appdev
    build:
      deployFiles: [.]
    run:
      base: nodejs@22
      ports:
        - port: 3000
`,
			wantWarnings: 1,
			wantContains: "run.start is empty",
		},
		{
			name:     "nodejs without ports still warns",
			hostname: "appdev",
			yml: `zerops:
  - setup: appdev
    build:
      deployFiles: [.]
    run:
      base: nodejs@22
      start: node index.js
`,
			wantWarnings: 1,
			wantContains: "run.ports is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runValidateTest(t, tt.hostname, tt.yml, tt.wantWarnings, tt.wantContains, tt.noWarnings)
		})
	}
}

func TestHasImplicitWebServer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		runBase   string
		buildBase string
		want      bool
	}{
		{"php-nginx run.base", "php-nginx@8.4", "php@8.4", true},
		{"php-apache run.base", "php-apache@8.3", "", true},
		{"nginx run.base", "nginx@1.22", "", true},
		{"static run.base with different build", "static", "nodejs@22", true},
		{"php-nginx build.base fallback", "", "php-nginx@8.4", true},
		{"nginx build.base fallback", "", "nginx@1.22", true},
		{"php cli is not implicit", "php@8.4", "", false},
		{"nodejs is not implicit", "nodejs@22", "", false},
		{"both empty", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := hasImplicitWebServer(tt.runBase, tt.buildBase)
			if got != tt.want {
				t.Errorf("hasImplicitWebServer(%q, %q) = %v, want %v", tt.runBase, tt.buildBase, got, tt.want)
			}
		})
	}
}

func runValidateTest(t *testing.T, hostname, yml string, wantWarnings int, wantContains string, noWarnings bool) {
	t.Helper()

	dir := t.TempDir()
	if yml != "" {
		if err := os.WriteFile(filepath.Join(dir, "zerops.yml"), []byte(yml), 0644); err != nil {
			t.Fatal(err)
		}
	}

	warnings := ValidateZeropsYml(dir, hostname)

	if noWarnings {
		if len(warnings) != 0 {
			t.Errorf("expected no warnings, got %d: %v", len(warnings), warnings)
		}
		return
	}

	if wantWarnings > 0 && len(warnings) < wantWarnings {
		t.Errorf("want >= %d warnings, got %d: %v", wantWarnings, len(warnings), warnings)
	}

	if wantContains != "" && len(warnings) > 0 {
		if !strings.Contains(warnings[0], wantContains) {
			t.Errorf("first warning %q should contain %q", warnings[0], wantContains)
		}
	}
}
