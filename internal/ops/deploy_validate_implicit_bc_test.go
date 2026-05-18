// Tests for: IsImplicitWebServerType + hasImplicitWebServer BC tolerance.
//
// Sunday-release 2026-05-18 — the live zerops.yaml schema enums non-`static`
// bases as composite-only (`alpine/php-nginx@8.4`, no bare `php-nginx@8.4`).
// IsImplicitWebServerType receives live API type; hasImplicitWebServer
// receives parsed zerops.yaml `run.base` / `build.base`. Both must accept
// composite shapes alongside legacy bare.
package ops

import "testing"

func TestIsImplicitWebServerType_CompositeShapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		serviceType string
		want        bool
	}{
		// Composite — main BC path.
		{"alpine/php-nginx@8.4", true},
		{"alpine/php-apache@8.4", true},
		{"alpine/nginx@1.22", true},
		{"alpine/static@1.0", true},
		{"ubuntu/php-nginx@8.5", true},
		{"ubuntu/static@1.0", true},

		// Bare — legacy clients.
		{"php-nginx@8.4", true},
		{"php-apache@8.3", true},
		{"nginx@1.22", true},
		{"static@1.0", true},

		// Non-implicit runtimes — false in both shapes.
		{"alpine/nodejs@22", false},
		{"nodejs@22", false},
		{"alpine/bun@1.2", false},
		{"ubuntu/go@1.22", false},

		// Managed services — never implicit.
		{"postgresql:single@18", false},
		{"postgresql@18", false},
		{"valkey:single@7.2", false},

		// Edge: empty string falls through to false (no decoration to strip).
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.serviceType, func(t *testing.T) {
			t.Parallel()
			got := IsImplicitWebServerType(tt.serviceType)
			if got != tt.want {
				t.Errorf("IsImplicitWebServerType(%q) = %v, want %v",
					tt.serviceType, got, tt.want)
			}
		})
	}
}

func TestHasImplicitWebServer_CompositeShapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		runBase    string
		buildBases []string
		want       bool
	}{
		// run.base composite implicit-web → true.
		{"run_alpine_php_nginx", "alpine/php-nginx@8.4", nil, true},
		{"run_alpine_nginx", "alpine/nginx@1.22", nil, true},

		// build.base composite implicit-web → true (fallback path).
		{"build_alpine_php_nginx", "", []string{"alpine/php-nginx@8.4", "alpine/nodejs@22"}, true},

		// build.base bare static (special token preserved by live schema).
		{"build_static_bare", "", []string{"static"}, true},

		// Pure dynamic stacks — false.
		{"run_nodejs", "alpine/nodejs@22", nil, false},
		{"run_build_dynamic", "", []string{"alpine/nodejs@22", "alpine/python@3.11"}, false},

		// Empty input — false.
		{"empty", "", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := hasImplicitWebServer(tt.runBase, tt.buildBases)
			if got != tt.want {
				t.Errorf("hasImplicitWebServer(%q, %v) = %v, want %v",
					tt.runBase, tt.buildBases, got, tt.want)
			}
		})
	}
}
