package topology

import "testing"

func TestRuntimeClassFor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		typeVersion string
		want        RuntimeClass
	}{
		// Empty + bare forms (legacy / pre-Sunday-release shape).
		{"", RuntimeUnknown},
		{"nodejs@22", RuntimeDynamic},
		{"go@1", RuntimeDynamic},
		{"python@3.12", RuntimeDynamic},
		{"bun@1.1", RuntimeDynamic},
		{"rust@1", RuntimeDynamic},
		{"php-nginx@8.4", RuntimeImplicitWeb},
		{"php-apache@8.3", RuntimeImplicitWeb},
		{"PHP-NGINX@8.4", RuntimeImplicitWeb}, // case-insensitive
		{"nginx@1.22", RuntimeStatic},
		{"static", RuntimeStatic},
		{"postgresql@16", RuntimeManaged},
		{"mariadb@10.6", RuntimeManaged},
		{"valkey@7.2", RuntimeManaged},
		{"object-storage", RuntimeManaged},

		// Composite forms — Sunday-release 2026-05-18 moved upstream type
		// identifiers to `<os>/<base>@<version>`. Live API now returns
		// `alpine/php-nginx@8.4`, `ubuntu/nodejs@22`, etc. Classifier MUST
		// strip the OS prefix before the runtime-prefix check, otherwise
		// every composite runtime mis-classifies as RuntimeDynamic.
		// Reference: plans/backlog/zerops-yaml-os-prefix-shape-drift.md.
		{"alpine/nodejs@22", RuntimeDynamic},
		{"ubuntu/nodejs@22", RuntimeDynamic},
		{"alpine/go@1", RuntimeDynamic},
		{"alpine/bun@1.1", RuntimeDynamic},
		{"alpine/php-nginx@8.4", RuntimeImplicitWeb},
		{"alpine/php-apache@8.3", RuntimeImplicitWeb},
		{"ubuntu/php-nginx@8.4", RuntimeImplicitWeb},
		{"alpine/nginx@1.22", RuntimeStatic},
		{"ubuntu/nginx@1.22", RuntimeStatic},
		{"alpine/static@1.0", RuntimeStatic},
		{"ALPINE/PHP-NGINX@8.4", RuntimeImplicitWeb}, // case-insensitive across OS prefix

		// Managed services post-Sunday-release carry mode encoding
		// (`<base>:<mode>@<version>`). IsManagedService prefix-matches the
		// bare base, so mode-encoded composites already classify correctly
		// — pinned here so a future managed-prefix refactor cannot silently
		// regress.
		{"postgresql:single@18", RuntimeManaged},
		{"postgresql:ha@18", RuntimeManaged},
		{"valkey:single@7.2", RuntimeManaged},

		// Unknown OS prefix passes through unchanged — stripKnownOSPrefix
		// only strips `alpine/` and `ubuntu/`. A type with an unrecognized
		// prefix still hits the runtime-class fallback as RuntimeDynamic
		// rather than mis-canonicalizing on a heuristic.
		{"debian/nodejs@22", RuntimeDynamic}, // unknown OS prefix, falls through to dynamic
	}
	for _, tc := range cases {
		t.Run(tc.typeVersion, func(t *testing.T) {
			t.Parallel()
			if got := RuntimeClassFor(tc.typeVersion); got != tc.want {
				t.Errorf("RuntimeClassFor(%q) = %q, want %q", tc.typeVersion, got, tc.want)
			}
		})
	}
}

// TestRuntimeClassFor_BareCompositeSymmetry pins the Sunday-release BC
// contract: bare and composite forms of the same runtime MUST classify
// identically. Without this, callers that receive plan-side (bare) input
// alongside live-API (composite) input would dispatch to different code
// paths for what the platform considers one service kind.
func TestRuntimeClassFor_BareCompositeSymmetry(t *testing.T) {
	t.Parallel()
	pairs := []struct {
		bare, composite string
	}{
		{"nodejs@22", "alpine/nodejs@22"},
		{"nodejs@22", "ubuntu/nodejs@22"},
		{"go@1", "alpine/go@1"},
		{"bun@1.2", "alpine/bun@1.2"},
		{"php-nginx@8.4", "alpine/php-nginx@8.4"},
		{"php-apache@8.3", "ubuntu/php-apache@8.3"},
		{"nginx@1.22", "alpine/nginx@1.22"},
		{"static", "alpine/static@1.0"},
		{"postgresql@18", "postgresql:single@18"},
		{"postgresql@18", "postgresql:ha@18"},
		{"valkey@7.2", "valkey:single@7.2"},
	}
	for _, p := range pairs {
		p := p
		t.Run(p.bare+"_vs_"+p.composite, func(t *testing.T) {
			t.Parallel()
			bareClass := RuntimeClassFor(p.bare)
			compositeClass := RuntimeClassFor(p.composite)
			if bareClass != compositeClass {
				t.Errorf("RuntimeClassFor symmetry broken: bare %q → %q, composite %q → %q",
					p.bare, bareClass, p.composite, compositeClass)
			}
		})
	}
}

func TestIsDeferredStart(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		mode  Mode
		class RuntimeClass
		want  bool
	}{
		// True: dev-mode dynamic runtimes (zsc noop lifecycle)
		{"dev_dynamic", ModeDev, RuntimeDynamic, true},
		{"standard_dynamic", ModeStandard, RuntimeDynamic, true},

		// False: stage / simple / local-stage auto-start via run.start
		{"stage_dynamic", ModeStage, RuntimeDynamic, false},
		{"simple_dynamic", ModeSimple, RuntimeDynamic, false},
		{"local_stage_dynamic", ModeLocalStage, RuntimeDynamic, false},
		{"local_only_dynamic", ModeLocalOnly, RuntimeDynamic, false},

		// False: implicit-webserver auto-starts the webserver
		{"dev_php_nginx", ModeDev, RuntimeImplicitWeb, false},
		{"standard_php_apache", ModeStandard, RuntimeImplicitWeb, false},

		// False: static runtimes auto-serve deployFiles
		{"dev_static", ModeDev, RuntimeStatic, false},
		{"standard_nginx", ModeStandard, RuntimeStatic, false},

		// False: managed services have no L7
		{"dev_managed", ModeDev, RuntimeManaged, false},

		// False: unknown class — fail closed (probe runs)
		{"dev_unknown", ModeDev, RuntimeUnknown, false},

		// False: empty mode — no info, fail closed
		{"empty_dynamic", Mode(""), RuntimeDynamic, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsDeferredStart(tc.mode, tc.class); got != tc.want {
				t.Errorf("IsDeferredStart(%q, %q) = %v, want %v", tc.mode, tc.class, got, tc.want)
			}
		})
	}
}
