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

func runValidateTest(t *testing.T, hostname, yml string, wantWarnings int, wantContains string, noWarnings bool, createDirs ...string) {
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
