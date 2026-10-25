package tools

import (
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const sourceWithDevOnly = `zerops:
  - setup: dev
    build:
      base: nodejs@22
      buildCommands:
        - npm install
    run:
      base: nodejs@22
      start: node dist/server.js
`

const sourceWithDevAndStage = `zerops:
  - setup: dev
    build:
      base: nodejs@22
      buildCommands:
        - npm install
    run:
      base: nodejs@22
      start: node dist/server.js
  - setup: stage
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
    run:
      base: nodejs@22
      start: node dist/server.js
`

const sourceWithSimpleOnly = `zerops:
  - setup: simple
    build:
      base: bun@1
      buildCommands:
        - bun install
    run:
      base: bun@1
      start: bun run index.ts
`

const sourceWithOnlyProd = `zerops:
  - setup: prod
    build:
      base: nodejs@22
`

// TestDeriveProdSetupBlock_PreferStage pins P3 priority reorder:
// stage wins over dev as the template source. Stage is the validated
// last-known-good copy that runs on Zerops (build-once, immutable);
// dev encodes iteration patterns that don't survive the prod build.
func TestDeriveProdSetupBlock_PreferStage(t *testing.T) {
	t.Parallel()
	got, err := deriveProdSetupBlock(sourceWithDevAndStage)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if !strings.Contains(got, "setup: prod") {
		t.Errorf("output missing setup: prod\n%s", got)
	}
	// dev's buildCommand was `npm install`; stage was `npm ci`.
	// Preferring stage means the proposed block carries `npm ci`.
	if !strings.Contains(got, "npm ci") {
		t.Errorf("expected stage's npm ci (template prefer stage post-P3), got:\n%s", got)
	}
	if strings.Contains(got, "npm install") {
		t.Errorf("expected stage template, not dev:\n%s", got)
	}
}

// TestDeriveProdSetupBlock_FallbackToFirstNonProd uses simple-mode
// source — dev/stage absent — falls back to first non-prod block.
func TestDeriveProdSetupBlock_FallbackToFirstNonProd(t *testing.T) {
	t.Parallel()
	got, err := deriveProdSetupBlock(sourceWithSimpleOnly)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if !strings.Contains(got, "setup: prod") {
		t.Errorf("output missing setup: prod\n%s", got)
	}
	if !strings.Contains(got, "bun@1") {
		t.Errorf("expected bun@1 base copied verbatim:\n%s", got)
	}
}

// TestDeriveProdSetupBlock_DevOnlyCopiesVerbatim verifies the
// derivation copies build + run blocks unchanged.
func TestDeriveProdSetupBlock_DevOnlyCopiesVerbatim(t *testing.T) {
	t.Parallel()
	got, err := deriveProdSetupBlock(sourceWithDevOnly)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	for _, want := range []string{
		"setup: prod",
		"base: nodejs@22",
		"npm install",
		"node dist/server.js",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output:\n%s", want, got)
		}
	}
}

// TestDeriveProdSetupBlock_DropsMatesFramingAllowance pins that a derived
// production setup never lets Mate's preview pane frame production: a dev or
// stage setup written in a Mate may widen `frame-ancestors` by Mate's origins
// (`https://mate.zerops.io https://*.zerops.app`), and production keeps the
// app's own sources — `'none'` where only Mate's were listed. A source list
// that does not name Mate's hosted origin is the app's own and stays.
func TestDeriveProdSetupBlock_DropsMatesFramingAllowance(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		source string
		want   []string
		absent []string
	}{
		{
			name: "a static stage's header keeps the app's own sources",
			source: `zerops:
  - setup: dev
    build:
      base: nodejs@22
    run:
      base: nodejs@22
  - setup: stage
    build:
      base: nodejs@22
      deployFiles: dist/~
    run:
      base: static
      routing:
        headers:
          - for: "/*"
            values:
              Content-Security-Policy: '"default-src ''self''; frame-ancestors ''self'' https://mate.zerops.io https://*.zerops.app" always'
              X-Content-Type-Options: "'nosniff' always"
`,
			want:   []string{`"default-src 'self'; frame-ancestors 'self'" always`, "'nosniff' always"},
			absent: []string{"mate.zerops.io", "*.zerops.app"},
		},
		{
			name: "only Mate's origins leave none",
			source: `zerops:
  - setup: stage
    build:
      base: nodejs@22
    run:
      base: static
      routing:
        headers:
          - for: "/*"
            values:
              Content-Security-Policy: '"frame-ancestors https://mate.zerops.io https://*.zerops.app" always'
`,
			want:   []string{`"frame-ancestors 'none'" always`},
			absent: []string{"mate.zerops.io", "*.zerops.app"},
		},
		{
			name: "a dev setup's variable the app reads",
			source: `zerops:
  - setup: dev
    build:
      base: nodejs@22
    run:
      base: nodejs@22
      start: zsc noop --silent
      envVariables:
        FRAME_ANCESTORS: "'self' https://mate.zerops.io https://*.zerops.app"
        NODE_ENV: development
`,
			want:   []string{"'self'", "development"},
			absent: []string{"mate.zerops.io", "*.zerops.app"},
		},
		{
			name: "the app's own frame sources without Mate's stay",
			source: `zerops:
  - setup: stage
    build:
      base: nodejs@22
    run:
      base: static
      routing:
        headers:
          - for: "/*"
            values:
              Content-Security-Policy: '"frame-ancestors https://partner.example.com https://*.zerops.app" always'
`,
			want: []string{`"frame-ancestors https://partner.example.com https://*.zerops.app" always`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := deriveProdSetupBlock(tc.source)
			if err != nil {
				t.Fatalf("derive: %v", err)
			}
			var setups []any
			if err := yaml.Unmarshal([]byte(got), &setups); err != nil {
				t.Fatalf("derived setup does not parse: %v\n%s", err, got)
			}
			values := yamlStringValues(setups)
			for _, want := range tc.want {
				if !slices.Contains(values, want) {
					t.Errorf("derived setup has no value %q:\n%s", want, got)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(strings.Join(values, "\n"), absent) {
					t.Errorf("derived setup still names %q:\n%s", absent, got)
				}
			}
		})
	}
}

// yamlStringValues lists every string value under v, depth first.
func yamlStringValues(v any) []string {
	switch v := v.(type) {
	case string:
		return []string{v}
	case []any:
		var out []string
		for _, item := range v {
			out = append(out, yamlStringValues(item)...)
		}
		return out
	case map[string]any:
		var out []string
		for _, item := range v {
			out = append(out, yamlStringValues(item)...)
		}
		return out
	default:
		return nil
	}
}

// TestDeriveProdSetupBlock_OnlyProdReturnsError pins the edge case
// where source has only setup:prod (defensive — caller checks
// hasSetupNamed(prod) first so this shouldn't fire in practice).
func TestDeriveProdSetupBlock_OnlyProdReturnsError(t *testing.T) {
	t.Parallel()
	_, err := deriveProdSetupBlock(sourceWithOnlyProd)
	if err == nil {
		t.Fatal("expected error when only setup:prod is available as template")
	}
}

// TestDeriveProdSetupBlock_MalformedYAMLReturnsError pins yaml parse
// failure handling.
func TestDeriveProdSetupBlock_MalformedYAMLReturnsError(t *testing.T) {
	t.Parallel()
	_, err := deriveProdSetupBlock("zerops:\n  - setup: [unclosed")
	if err == nil {
		t.Fatal("expected error for malformed yaml")
	}
}

// TestProdSetupGuidanceWithBlock_EmbedsBlockAndChecklist verifies the
// response guidance carries the derived block + readiness checklist.
func TestProdSetupGuidanceWithBlock_EmbedsBlockAndChecklist(t *testing.T) {
	t.Parallel()
	proposed, err := deriveProdSetupBlock(sourceWithDevOnly)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	guidance := prodSetupGuidanceWithBlock("prod", []string{"dev"}, proposed)
	for _, want := range []string{
		"setup: prod",
		"Found setups: dev",
		"prodSetupNameOverride",
		"npm install",
		"healthCheck",
		"NODE_ENV=production",
		"```yaml",
	} {
		if !strings.Contains(guidance, want) {
			t.Errorf("guidance missing %q:\n%s", want, guidance)
		}
	}
}

// TestProdSetupGuidanceWithBlock_HonoursOverrideName verifies the
// guidance addresses a non-canonical override setup name (`appprod`)
// when the agent passes ProdSetupNameOverride.
func TestProdSetupGuidanceWithBlock_HonoursOverrideName(t *testing.T) {
	t.Parallel()
	proposed, err := deriveProdSetupBlock(sourceWithDevOnly)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	guidance := prodSetupGuidanceWithBlock("appprod", []string{"dev", "stage"}, proposed)
	if !strings.Contains(guidance, "setup: appprod") {
		t.Errorf("guidance missing override name `setup: appprod`:\n%s", guidance)
	}
	if !strings.Contains(guidance, "Found setups: dev, stage") {
		t.Errorf("guidance missing available-setups list:\n%s", guidance)
	}
}

// TestHasSetupNamed_MatchesArbitraryName pins the generalized hasSetup
// helper. Used by the source-control gate to honor
// WorkflowInput.ProdSetupNameOverride.
func TestHasSetupNamed_MatchesArbitraryName(t *testing.T) {
	t.Parallel()
	body := `zerops:
  - setup: dev
  - setup: appprod
`
	tests := []struct {
		name  string
		setup string
		want  bool
	}{
		{"matches_dev", "dev", true},
		{"matches_appprod", "appprod", true},
		{"missing_prod_canonical", "prod", false},
		{"empty_returns_false", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := hasSetupNamed(body, tt.setup); got != tt.want {
				t.Errorf("hasSetupNamed(%q): got %v, want %v", tt.setup, got, tt.want)
			}
		})
	}
}
