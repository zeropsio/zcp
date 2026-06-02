package topology

import "testing"

func TestCanonicalBareForm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		// Composite runtimes — OS prefix stripped.
		{"alpine/nodejs@22", "nodejs@22"},
		{"ubuntu/nodejs@22", "nodejs@22"},
		{"alpine/php-nginx@8.4", "php-nginx@8.4"},
		{"alpine/php-apache@8.4", "php-apache@8.4"},
		// Bare runtimes — unchanged.
		{"nodejs@22", "nodejs@22"},
		{"php-nginx@8.4", "php-nginx@8.4"},
		// Managed services — mode suffix stripped.
		{"postgresql:single@18", "postgresql@18"},
		{"postgresql:ha@18", "postgresql@18"},
		{"valkey:single@7.2", "valkey@7.2"},
		{"mariadb:ha@10.6", "mariadb@10.6"},
		// Bare managed — unchanged.
		{"postgresql@18", "postgresql@18"},
		// Versionless types — unchanged.
		{"object-storage", "object-storage"},
		// Known mode suffix without a version — `:single`/`:ha` are stripped
		// even with no trailing `@version`.
		{"shared-storage:ha", "shared-storage"},
		{"shared-storage:single", "shared-storage"},
		// Special types — unchanged.
		{"zcp@1", "zcp@1"},
		// Unknown OS prefix — left intact (precise transform, not heuristic).
		{"debian/nodejs@22", "debian/nodejs@22"},
		// Unknown `:suffix` is NOT a mode — left intact, so it can't
		// canonicalize into (and falsely match) a real base.
		{"foo:bar", "foo:bar"},
		{"object-storage:bogus", "object-storage:bogus"},
		{"nodejs:bogus@22", "nodejs:bogus@22"},
		// Empty — unchanged.
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := CanonicalBareForm(tt.in); got != tt.want {
				t.Errorf("CanonicalBareForm(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestTypesAreEquivalent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		a, b string
		want bool
	}{
		// Identity.
		{"nodejs@22", "nodejs@22", true},
		{"alpine/nodejs@22", "alpine/nodejs@22", true},
		// Bare ↔ composite (the core BC pattern).
		{"nodejs@22", "alpine/nodejs@22", true},
		{"alpine/nodejs@22", "nodejs@22", true},
		{"php-nginx@8.4", "alpine/php-nginx@8.4", true},
		// Different OS variants — NOT equivalent (different runtime variants).
		{"alpine/nodejs@22", "ubuntu/nodejs@22", false},
		// Bare ↔ mode-encoded managed (the deps BC pattern).
		{"postgresql@18", "postgresql:single@18", true},
		{"postgresql@18", "postgresql:ha@18", true},
		// Different modes — NOT equivalent (HA and single are different services).
		{"postgresql:ha@18", "postgresql:single@18", false},
		// Storage modes follow the same semantic: bare matches either spelling,
		// two decorated modes are distinct.
		{"shared-storage:ha", "shared-storage", true},
		{"shared-storage:single", "shared-storage", true},
		{"shared-storage:ha", "shared-storage:single", false},
		// Bogus (non-mode) `:suffix` must NOT canonicalize away and match — only
		// known modes (single/ha) are stripped.
		{"nodejs:bogus@22", "nodejs@22", false},
		{"object-storage:bogus", "object-storage", false},
		// Different version — not equivalent.
		{"nodejs@22", "nodejs@24", false},
		{"postgresql@18", "postgresql@17", false},
		{"alpine/nodejs@22", "alpine/nodejs@24", false},
		// Different base — not equivalent.
		{"nodejs@22", "bun@22", false},
		{"postgresql@18", "mariadb@18", false},
		// Empty — equal to empty only.
		{"", "", true},
		{"nodejs@22", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			t.Parallel()
			if got := TypesAreEquivalent(tt.a, tt.b); got != tt.want {
				t.Errorf("TypesAreEquivalent(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
			// Symmetry.
			if got := TypesAreEquivalent(tt.b, tt.a); got != tt.want {
				t.Errorf("TypesAreEquivalent(%q, %q) = %v, want %v (symmetry)", tt.b, tt.a, got, tt.want)
			}
		})
	}
}
