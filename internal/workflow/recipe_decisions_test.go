package workflow

import "testing"

func TestResolveWebServer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		runtimeType   string
		hasNativeHTTP bool
		want          string
	}{
		{"php-nginx uses nginx-sidecar", "php-nginx@8.4", false, "nginx-sidecar"},
		{"php-apache uses nginx-sidecar", "php-apache@8.4", false, "nginx-sidecar"},
		{"nodejs uses builtin", "nodejs@22", true, "builtin"},
		{"bun uses builtin", "bun@1.1", true, "builtin"},
		{"go uses builtin", "go@1.22", true, "builtin"},
		{"rust uses builtin", "rust@1", true, "builtin"},
		{"python uses builtin", "python@3.12", true, "builtin"},
		{"nginx uses nginx-proxy", "nginx@1.25", false, "nginx-proxy"},
		{"static uses nginx-proxy", "static@1", false, "nginx-proxy"},
		{"unknown without native HTTP uses nginx-proxy", "custom@1", false, "nginx-proxy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveWebServer(tt.runtimeType, tt.hasNativeHTTP)
			if got != tt.want {
				t.Errorf("ResolveWebServer(%q, %v) = %q, want %q", tt.runtimeType, tt.hasNativeHTTP, got, tt.want)
			}
		})
	}
}

func TestResolveOS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		runtimeType string
		want        string
	}{
		{"go prefers alpine", "go@1.22", "alpine"},
		{"rust prefers alpine", "rust@1", "alpine"},
		{"nodejs defaults to ubuntu", "nodejs@22", "ubuntu-22"},
		{"php defaults to ubuntu", "php-nginx@8.4", "ubuntu-22"},
		{"python defaults to ubuntu", "python@3.12", "ubuntu-22"},
		{"bun defaults to ubuntu", "bun@1.1", "ubuntu-22"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveOS(tt.runtimeType)
			if got != tt.want {
				t.Errorf("ResolveOS(%q) = %q, want %q", tt.runtimeType, got, tt.want)
			}
		})
	}
}

func TestResolveDevTooling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		framework   string
		runtimeType string
		want        string
	}{
		{"nodejs hot-reload", "express", "nodejs@22", "hot-reload"},
		{"bun hot-reload", "elysia", "bun@1.1", "hot-reload"},
		{"python watch", "django", "python@3.12", "watch"},
		{"laravel watch", "laravel", "php-nginx@8.4", "watch"},
		{"symfony watch", "symfony", "php-nginx@8.4", "watch"},
		{"generic php manual", "codeigniter", "php-nginx@8.4", "manual"},
		{"go manual", "gin", "go@1.22", "manual"},
		{"rust manual", "actix", "rust@1", "manual"},
		{"java manual", "spring", "java@21", "manual"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveDevTooling(tt.framework, tt.runtimeType)
			if got != tt.want {
				t.Errorf("ResolveDevTooling(%q, %q) = %q, want %q", tt.framework, tt.runtimeType, got, tt.want)
			}
		})
	}
}
