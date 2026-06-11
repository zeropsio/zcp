package recipe

import (
	"strings"
	"testing"
)

// TestParseProdRuntimeBaseFromYaml covers the Vite-SPA asymmetry
// (build nodejs@22, run static), the symmetric case (Laravel:
// php-nginx@8.4 in both), the extends-chain pattern (jetstream:
// prod extends base, base owns run.base), and the missing-setup
// fallbacks (callers fall back to BaseRuntime when this returns "").
func TestParseProdRuntimeBaseFromYaml(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		body      string
		setupName string
		want      string
	}{
		{
			name:      "vite-spa asymmetry — prod runs static",
			setupName: "prod",
			body: `zerops:
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - npm run build
      deployFiles: dist/~
    run:
      base: static
      ports:
        - port: 80
          httpSupport: true
`,
			want: "static",
		},
		{
			name:      "symmetric — laravel matches build and run",
			setupName: "prod",
			body: `zerops:
  - setup: prod
    build:
      base: php-nginx@8.4
    run:
      base: php-nginx@8.4
`,
			want: "php-nginx@8.4",
		},
		{
			name:      "extends chain — prod extends base; base owns run.base",
			setupName: "prod",
			body: `zerops:
  - setup: base
    run:
      base: php-nginx@8.4
      envVariables:
        APP_LOCALE: en
  - setup: prod
    extends: base
    build:
      base:
        - php@8.4
        - nodejs@22
      buildCommands:
        - composer install
`,
			want: "php-nginx@8.4",
		},
		{
			name:      "no prod setup — empty fallback",
			setupName: "prod",
			body: `zerops:
  - setup: dev
    run:
      base: nodejs@22
`,
			want: "",
		},
		{
			name:      "prod has no run.base and no extends — empty fallback",
			setupName: "prod",
			body: `zerops:
  - setup: prod
    build:
      base: nodejs@22
`,
			want: "",
		},
		{
			name:      "run.base sequence — first scalar wins",
			setupName: "prod",
			body: `zerops:
  - setup: prod
    run:
      base:
        - static
        - nodejs@22
`,
			want: "static",
		},
		{
			name:      "extends cycle — defensive empty fallback",
			setupName: "prod",
			body: `zerops:
  - setup: prod
    extends: middle
  - setup: middle
    extends: prod
`,
			want: "",
		},
		{
			name:      "empty body — empty fallback",
			setupName: "prod",
			body:      "",
			want:      "",
		},
		{
			name:      "empty setupName — empty fallback",
			setupName: "",
			body: `zerops:
  - setup: prod
    run:
      base: static
`,
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseProdRuntimeBaseFromYaml(tc.body, tc.setupName)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Errorf("parseProdRuntimeBaseFromYaml = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEmitDeliverableYAML_ProdRuntimeBase_Asymmetry covers the
// run-32 factuality bug — Vite-SPA codebase declared `type: nodejs@22`
// in stage import.yaml even though prod runtime base was `static`.
// With ProdRuntimeBase populated, the deliverable emit MUST use
// `static` for the runtime type at every tier.
func TestEmitDeliverableYAML_ProdRuntimeBase_Asymmetry(t *testing.T) {
	t.Parallel()

	plan := &Plan{
		Slug:      "vite-spa-test",
		Framework: "vite",
		Codebases: []Codebase{
			{
				Hostname:        "app",
				Role:            RoleFrontend,
				BaseRuntime:     "nodejs@22",
				ProdRuntimeBase: "static",
				SourceRoot:      "/var/www/appdev",
			},
		},
	}

	for tierIndex := range 6 {
		body, err := EmitDeliverableYAML(plan, tierIndex)
		if err != nil {
			t.Fatalf("tier %d emit: %v", tierIndex, err)
		}
		// Stage / single slot must use prod base "static", not build
		// base "nodejs@22".
		if !strings.Contains(body, "type: static") {
			t.Errorf("tier %d: import.yaml missing `type: static` (prod runtime base); body:\n%s", tierIndex, body)
		}
		// Tiers 0/1 emit a dev pair — the dev slot retains the build
		// runtime so the SSHFS-mounted dev container has the
		// toolchain. Stage slot in those tiers uses prod base.
		if tierIndex <= 1 {
			if !strings.Contains(body, "type: nodejs@22") {
				t.Errorf("tier %d: dev slot should retain build runtime nodejs@22; body:\n%s", tierIndex, body)
			}
		} else {
			// Tier 2-5 single-slot must NOT carry the build base —
			// only `static` for the prod runtime.
			if strings.Contains(body, "type: nodejs@22") {
				t.Errorf("tier %d: single-slot leaks build base nodejs@22 (should be `static` only); body:\n%s", tierIndex, body)
			}
		}
	}
}

// TestEmitDeliverableYAML_ProdRuntimeBase_FallbackToBaseRuntime covers
// the symmetric case: ProdRuntimeBase empty → emitter falls back to
// BaseRuntime (NestJS / Laravel pattern).
func TestEmitDeliverableYAML_ProdRuntimeBase_FallbackToBaseRuntime(t *testing.T) {
	t.Parallel()

	plan := &Plan{
		Slug:      "nestjs-symmetric",
		Framework: "nestjs",
		Codebases: []Codebase{
			{
				Hostname:    "api",
				Role:        RoleAPI,
				BaseRuntime: "nodejs@22",
				// ProdRuntimeBase intentionally empty — symmetric.
				SourceRoot: "/var/www/apidev",
			},
		},
	}

	body, err := EmitDeliverableYAML(plan, 4)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if !strings.Contains(body, "type: nodejs@22") {
		t.Errorf("symmetric case should fall back to BaseRuntime; body:\n%s", body)
	}
}
