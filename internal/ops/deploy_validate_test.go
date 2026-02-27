// Tests for: ops/deploy_validate.go — zerops.yml pre-deploy validation.
package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateZeropsYml(t *testing.T) {
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

			dir := t.TempDir()
			if tt.yml != "" {
				if err := os.WriteFile(filepath.Join(dir, "zerops.yml"), []byte(tt.yml), 0644); err != nil {
					t.Fatal(err)
				}
			}

			warnings := ValidateZeropsYml(dir, tt.hostname)

			if tt.noWarnings {
				if len(warnings) != 0 {
					t.Errorf("expected no warnings, got %d: %v", len(warnings), warnings)
				}
				return
			}

			if tt.wantWarnings > 0 && len(warnings) < tt.wantWarnings {
				t.Errorf("want >= %d warnings, got %d: %v", tt.wantWarnings, len(warnings), warnings)
			}

			if tt.wantContains != "" && len(warnings) > 0 {
				if !strings.Contains(warnings[0], tt.wantContains) {
					t.Errorf("first warning %q should contain %q", warnings[0], tt.wantContains)
				}
			}
		})
	}
}
