// Tests for: ops/deploy_validate.go — zerops.yaml pre-deploy validation.
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
		createDirs   []string
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
			createDirs:   []string{"dist"},
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
			createDirs: []string{"dist"},
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
			createDirs:   []string{"dist"},
		},
		{
			name:     "deployFiles under run instead of build",
			hostname: "appdev",
			yml: `zerops:
  - setup: appdev
    run:
      start: node index.js
      ports:
        - port: 8080
      deployFiles:
        - .
`,
			wantWarnings: 2, // "build.deployFiles empty" + "belongs under build:"
			wantContains: "deployFiles is empty",
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
			createDirs: []string{"dist"},
		},
		{
			name:     "cherry-picked deployFiles with missing paths",
			hostname: "appstage",
			yml: `zerops:
  - setup: appstage
    build:
      deployFiles:
        - app
        - vendor
        - public
        - nonexistent
    run:
      start: php artisan serve
      ports:
        - port: 8080
`,
			wantWarnings: 1,
			wantContains: "deployFiles paths not found: nonexistent",
			createDirs:   []string{"app", "vendor", "public"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runValidateTest(t, tt.hostname, tt.yml, tt.wantWarnings, tt.wantContains, tt.noWarnings, tt.createDirs...)
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
      deployFiles: [.]
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
      deployFiles: [.]
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
      deployFiles: [.]
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
      deployFiles: [.]
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

func TestValidateZeropsYml_MultiBaseType(t *testing.T) {
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
			name:     "array base in build is valid",
			hostname: "appdev",
			yml: `zerops:
  - setup: appdev
    build:
      base: [php-nginx@8.4, nodejs@22]
      deployFiles: [.]
    run:
      base: php-nginx@8.4
`,
			noWarnings: true,
		},
		{
			name:     "string base in build is valid",
			hostname: "appdev",
			yml: `zerops:
  - setup: appdev
    build:
      base: nodejs@22
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 3000
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

func TestValidateZeropsYml_StageZscNoop_Warning(t *testing.T) {
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
			name:     "stage with zsc noop warns",
			hostname: "appstage",
			yml: `zerops:
  - setup: appstage
    build:
      base: nodejs@22
      buildCommands:
        - zsc noop
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 3000
`,
			wantWarnings: 1,
			wantContains: "zsc noop",
		},
		{
			name:     "dev with zsc noop is fine",
			hostname: "appdev",
			yml: `zerops:
  - setup: appdev
    build:
      base: nodejs@22
      buildCommands:
        - zsc noop
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 3000
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

func TestNeedsManualStart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		serviceType string
		want        bool
	}{
		{"nodejs needs start", "nodejs@22", true},
		{"go needs start", "go@1", true},
		{"bun needs start", "bun@1.2", true},
		{"python needs start", "python@3.12", true},
		{"rust needs start", "rust@stable", true},
		{"deno needs start", "deno@2", true},
		{"php-nginx auto-starts", "php-nginx@8.4", false},
		{"php-apache auto-starts", "php-apache@8.3", false},
		{"nginx auto-starts", "nginx@1.22", false},
		{"static auto-starts", "static", false},
		{"empty defaults to needs start", "", true},
		{"bare nodejs", "nodejs", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := NeedsManualStart(tt.serviceType)
			if got != tt.want {
				t.Errorf("NeedsManualStart(%q) = %v, want %v", tt.serviceType, got, tt.want)
			}
		})
	}
}

func TestHasImplicitWebServer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		runBase    string
		buildBases []string
		want       bool
	}{
		{"php-nginx run.base", "php-nginx@8.4", []string{"php@8.4"}, true},
		{"php-apache run.base", "php-apache@8.3", nil, true},
		{"nginx run.base", "nginx@1.22", nil, true},
		{"static run.base with different build", "static", []string{"nodejs@22"}, true},
		{"php-nginx build.base fallback", "", []string{"php-nginx@8.4"}, true},
		{"nginx build.base fallback", "", []string{"nginx@1.22"}, true},
		{"php cli is not implicit", "php@8.4", nil, false},
		{"nodejs is not implicit", "nodejs@22", nil, false},
		{"both empty", "", nil, false},
		{"multi build bases with implicit", "", []string{"php-nginx@8.4", "nodejs@22"}, true},
		{"multi build bases without implicit", "", []string{"nodejs@22", "bun@1.2"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := hasImplicitWebServer(tt.runBase, tt.buildBases)
			if got != tt.want {
				t.Errorf("hasImplicitWebServer(%q, %v) = %v, want %v", tt.runBase, tt.buildBases, got, tt.want)
			}
		})
	}
}

// --- ValidateEnvReferences ---

func TestValidateEnvReferences_ValidRef_NoError(t *testing.T) {
	t.Parallel()

	envVars := map[string]string{
		"DATABASE_URL": "${db_connectionString}",
	}
	discovered := map[string][]string{
		"db": {"connectionString", "host", "port"},
	}
	hostnames := []string{"db", "app"}

	errs := ValidateEnvReferences(envVars, discovered, hostnames)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidateEnvReferences_InvalidHostname_Error(t *testing.T) {
	t.Parallel()

	envVars := map[string]string{
		"DATABASE_URL": "${nonexistent_connectionString}",
	}
	discovered := map[string][]string{
		"db": {"connectionString"},
	}
	hostnames := []string{"db", "app"}

	errs := ValidateEnvReferences(envVars, discovered, hostnames)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Reference != "${nonexistent_connectionString}" {
		t.Errorf("Reference = %q, want ${nonexistent_connectionString}", errs[0].Reference)
	}
	if !strings.Contains(errs[0].Reason, "unknown hostname") {
		t.Errorf("Reason = %q, want to contain 'unknown hostname'", errs[0].Reason)
	}
}

func TestValidateEnvReferences_InvalidVarName_Error(t *testing.T) {
	t.Parallel()

	envVars := map[string]string{
		"DATABASE_URL": "${db_totallyFakeVar}",
	}
	discovered := map[string][]string{
		"db": {"connectionString", "host", "port"},
	}
	hostnames := []string{"db"}

	errs := ValidateEnvReferences(envVars, discovered, hostnames)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Reason, "unknown variable") {
		t.Errorf("Reason = %q, want to contain 'unknown variable'", errs[0].Reason)
	}
}

func TestValidateEnvReferences_CaseSensitive_Error(t *testing.T) {
	t.Parallel()

	envVars := map[string]string{
		"DATABASE_URL": "${db_ConnectionString}", // wrong case
	}
	discovered := map[string][]string{
		"db": {"connectionString"},
	}
	hostnames := []string{"db"}

	errs := ValidateEnvReferences(envVars, discovered, hostnames)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Reason, "unknown variable") {
		t.Errorf("Reason = %q, want to contain 'unknown variable'", errs[0].Reason)
	}
}

func TestValidateEnvReferences_MultipleRefs_AllChecked(t *testing.T) {
	t.Parallel()

	envVars := map[string]string{
		"COMBINED": "${db_host}:${db_port}/${cache_fakeVar}",
	}
	discovered := map[string][]string{
		"db":    {"host", "port"},
		"cache": {"connectionString"},
	}
	hostnames := []string{"db", "cache"}

	errs := ValidateEnvReferences(envVars, discovered, hostnames)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error (cache_fakeVar), got %d: %v", len(errs), errs)
	}
	if errs[0].Reference != "${cache_fakeVar}" {
		t.Errorf("Reference = %q, want ${cache_fakeVar}", errs[0].Reference)
	}
}

func TestValidateEnvReferences_NoRefs_NoError(t *testing.T) {
	t.Parallel()

	envVars := map[string]string{
		"PORT":       "3000",
		"NODE_ENV":   "production",
		"PLAIN_TEXT": "hello world",
	}
	discovered := map[string][]string{
		"db": {"connectionString"},
	}
	hostnames := []string{"db"}

	errs := ValidateEnvReferences(envVars, discovered, hostnames)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidateEnvReferences_LiteralDollar_Ignored(t *testing.T) {
	t.Parallel()

	envVars := map[string]string{
		"ESCAPED": "$$dollar",
		"PARTIAL": "$notaref",
		"CURLY":   "${incomplete",
	}
	discovered := map[string][]string{}
	hostnames := []string{"db"}

	errs := ValidateEnvReferences(envVars, discovered, hostnames)
	if len(errs) != 0 {
		t.Errorf("expected no errors for non-ref dollar signs, got %v", errs)
	}
}

func TestParseZeropsYml_ExtensionFallback(t *testing.T) {
	t.Parallel()

	validYAML := `zerops:
  - setup: appdev
    build:
      base: bun@1.2
      buildCommands: ["bun install"]
      deployFiles: [.]
    run:
      start: bun run src/index.ts
`

	tests := []struct {
		name         string
		files        map[string]string // filename → content
		wantSetup    string            // expected first entry setup name
		wantErr      bool
		wantContains string // error message must contain
	}{
		{
			name:      "yaml extension found",
			files:     map[string]string{"zerops.yaml": validYAML},
			wantSetup: "appdev",
		},
		{
			name:      "yml fallback",
			files:     map[string]string{"zerops.yml": validYAML},
			wantSetup: "appdev",
		},
		{
			name:      "yaml takes priority over yml",
			files:     map[string]string{"zerops.yaml": validYAML, "zerops.yml": "zerops:\n  - setup: other\n"},
			wantSetup: "appdev",
		},
		{
			name:         "neither extension found",
			files:        map[string]string{},
			wantErr:      true,
			wantContains: "zerops.yaml",
		},
		{
			name:         "neither extension — error mentions yml fallback",
			files:        map[string]string{},
			wantErr:      true,
			wantContains: "zerops.yml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for name, content := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}

			doc, err := ParseZeropsYml(dir)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.wantContains != "" && !strings.Contains(err.Error(), tt.wantContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(doc.Zerops) == 0 {
				t.Fatal("expected at least one entry")
			}
			if doc.Zerops[0].Setup != tt.wantSetup {
				t.Errorf("got setup %q, want %q", doc.Zerops[0].Setup, tt.wantSetup)
			}
		})
	}
}

// validateTestOpts extends runValidateTest with optional serviceType.
type validateTestOpts struct {
	serviceType string
}

func runValidateTest(t *testing.T, hostname, yml string, wantWarnings int, wantContains string, noWarnings bool, createDirs ...string) {
	t.Helper()
	runValidateTestWithOpts(t, hostname, yml, wantWarnings, wantContains, noWarnings, validateTestOpts{}, createDirs...)
}

func runValidateTestWithOpts(t *testing.T, hostname, yml string, wantWarnings int, wantContains string, noWarnings bool, opts validateTestOpts, createDirs ...string) {
	t.Helper()

	dir := t.TempDir()
	if yml != "" {
		if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(yml), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range createDirs {
		if err := os.MkdirAll(filepath.Join(dir, d), 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Phase B.4 removed the hostname-substring role fallback in
	// ValidateZeropsYml; callers pass the role explicitly. The test
	// fixtures name hostnames with dev/stage suffixes for readability,
	// so the test helper derives the role from the suffix and hands it
	// through as the explicit role param. Production callers read role
	// from ServiceMeta, not from hostname — see deploy_preflight.go.
	role := ""
	switch {
	case strings.HasSuffix(hostname, "dev"):
		role = "dev"
	case strings.HasSuffix(hostname, "stage"):
		role = "stage"
	}
	warnings := ValidateZeropsYml(dir, hostname, opts.serviceType, role)

	if noWarnings {
		if len(warnings) != 0 {
			t.Errorf("expected no warnings, got %d: %v", len(warnings), warnings)
		}
		return
	}

	if wantWarnings > 0 && len(warnings) < wantWarnings {
		t.Errorf("want >= %d warnings, got %d: %v", wantWarnings, len(warnings), warnings)
	}

	if wantContains != "" {
		found := false
		for _, w := range warnings {
			if strings.Contains(w, wantContains) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no warning contains %q, got: %v", wantContains, warnings)
		}
	}
}

func TestValidateZeropsYml_ServiceTypeImplicitWebServer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		hostname    string
		serviceType string
		yml         string
		noWarnings  bool
	}{
		{
			name:        "php-nginx service type suppresses start/ports warnings even with php build base",
			hostname:    "kanboard",
			serviceType: "php-nginx@8.4",
			yml: `zerops:
  - setup: kanboard
    build:
      base: php@8.4
      deployFiles: [.]
`,
			noWarnings: true,
		},
		{
			name:        "php-apache service type suppresses warnings",
			hostname:    "app",
			serviceType: "php-apache@8.3",
			yml: `zerops:
  - setup: app
    build:
      base: php@8.3
      deployFiles: [.]
`,
			noWarnings: true,
		},
		{
			name:        "nginx service type suppresses warnings",
			hostname:    "web",
			serviceType: "nginx@1.26",
			yml: `zerops:
  - setup: web
    build:
      deployFiles: [.]
`,
			noWarnings: true,
		},
		{
			name:        "static service type suppresses warnings",
			hostname:    "web",
			serviceType: "static",
			yml: `zerops:
  - setup: web
    build:
      deployFiles: [.]
`,
			noWarnings: true,
		},
		{
			name:        "nodejs service type does not suppress warnings",
			hostname:    "appdev",
			serviceType: "nodejs@22",
			yml: `zerops:
  - setup: appdev
    build:
      deployFiles: [.]
`,
			noWarnings: false,
		},
		{
			name:        "empty service type falls back to yaml bases only",
			hostname:    "appdev",
			serviceType: "",
			yml: `zerops:
  - setup: appdev
    build:
      base: php@8.4
      deployFiles: [.]
`,
			noWarnings: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			wantWarnings := 0
			if !tt.noWarnings {
				wantWarnings = 1
			}
			runValidateTestWithOpts(t, tt.hostname, tt.yml, wantWarnings, "", tt.noWarnings, validateTestOpts{serviceType: tt.serviceType})
		})
	}
}

func TestValidateZeropsYml_PrepareCommandsSudo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		hostname     string
		yml          string
		wantContains string
		noWarnings   bool
	}{
		{
			name:     "run.prepareCommands with sudo is fine",
			hostname: "app",
			yml: `zerops:
  - setup: app
    build:
      deployFiles: [.]
    run:
      base: php-nginx@8.4
      prepareCommands:
        - sudo apk add --no-cache php84-ctype
`,
			noWarnings: true,
		},
		{
			name:     "run.prepareCommands without sudo warns",
			hostname: "app",
			yml: `zerops:
  - setup: app
    build:
      deployFiles: [.]
    run:
      base: php-nginx@8.4
      prepareCommands:
        - apk add --no-cache php84-ctype
`,
			wantContains: "sudo",
		},
		{
			name:     "run.prepareCommands apt-get without sudo warns",
			hostname: "app",
			yml: `zerops:
  - setup: app
    build:
      deployFiles: [.]
    run:
      base: php-nginx@8.4
      prepareCommands:
        - apt-get install -y libcurl4
`,
			wantContains: "sudo",
		},
		{
			name:     "build.prepareCommands without sudo warns",
			hostname: "app",
			yml: `zerops:
  - setup: app
    build:
      base: nodejs@22
      prepareCommands:
        - apk add --no-cache python3
      buildCommands:
        - npm install
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 3000
`,
			wantContains: "sudo",
		},
		{
			name:     "build.prepareCommands with sudo is fine",
			hostname: "app",
			yml: `zerops:
  - setup: app
    build:
      base: nodejs@22
      prepareCommands:
        - sudo apk add --no-cache python3
      buildCommands:
        - npm install
      deployFiles: [.]
    run:
      start: node index.js
      ports:
        - port: 3000
`,
			noWarnings: true,
		},
		{
			name:     "non-package commands dont need sudo",
			hostname: "app",
			yml: `zerops:
  - setup: app
    build:
      deployFiles: [.]
    run:
      base: php-nginx@8.4
      prepareCommands:
        - echo "hello"
`,
			noWarnings: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			wantWarnings := 0
			if !tt.noWarnings {
				wantWarnings = 1
			}
			runValidateTestWithOpts(t, tt.hostname, tt.yml, wantWarnings, tt.wantContains, tt.noWarnings, validateTestOpts{})
		})
	}
}
