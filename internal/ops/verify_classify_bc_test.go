// Tests for: classifyRuntime BC tolerance against post-Sunday-release
// composite OS-prefixed service type identifiers.
package ops

import "testing"

func TestClassifyRuntime_CompositeShapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		serviceType string
		hasPorts    bool
		want        RuntimeClass
	}{
		// Composite implicit-web shapes — must match the same case as bare
		// `php-nginx@8.4` etc. Pre-fix these fell through to RuntimeDynamic
		// because strings.Cut(@) left `alpine/php-nginx` which matched no
		// case.
		{"alpine_php_nginx", "alpine/php-nginx@8.4", true, RuntimeImplicit},
		{"alpine_php_apache", "alpine/php-apache@8.4", true, RuntimeImplicit},
		{"ubuntu_php_nginx", "ubuntu/php-nginx@8.5", true, RuntimeImplicit},

		// Composite static shapes.
		{"alpine_static", "alpine/static@1.0", true, RuntimeStatic},
		{"alpine_nginx", "alpine/nginx@1.22", true, RuntimeStatic},
		{"ubuntu_static", "ubuntu/static@1.0", true, RuntimeStatic},

		// Composite dynamic shapes — has ports → RuntimeDynamic (no change
		// from legacy behavior, but confirm OS-prefix doesn't accidentally
		// short-circuit).
		{"alpine_nodejs", "alpine/nodejs@22", true, RuntimeDynamic},
		{"ubuntu_bun", "ubuntu/bun@1.2.2", true, RuntimeDynamic},

		// Composite dynamic without ports → worker.
		{"alpine_nodejs_no_ports", "alpine/nodejs@22", false, RuntimeWorker},

		// Bare shapes still work (BC the other direction — legacy clients).
		{"bare_php_nginx", "php-nginx@8.4", true, RuntimeImplicit},
		{"bare_static", "static@1.0", true, RuntimeStatic},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := classifyRuntime(tt.serviceType, tt.hasPorts, nil)
			if got != tt.want {
				t.Errorf("classifyRuntime(%q, %v) = %v, want %v",
					tt.serviceType, tt.hasPorts, got, tt.want)
			}
		})
	}
}
