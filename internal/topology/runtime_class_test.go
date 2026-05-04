package topology

import "testing"

func TestRuntimeClassFor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		typeVersion string
		want        RuntimeClass
	}{
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
