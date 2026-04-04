package workflow

import "strings"

// ResolveWebServer determines the web server strategy for a recipe.
// Inputs: runtimeType (e.g. "php-nginx@8.4"), hasNativeHTTP (framework has built-in server).
// Returns: "builtin", "nginx-sidecar", or "nginx-proxy".
func ResolveWebServer(runtimeType string, hasNativeHTTP bool) string {
	base, _, _ := strings.Cut(runtimeType, "@")

	// PHP runtimes use nginx-sidecar (php-fpm behind nginx).
	if strings.HasPrefix(base, "php-nginx") || strings.HasPrefix(base, "php-apache") {
		return "nginx-sidecar"
	}

	// Static runtimes (nginx, static) use nginx-proxy.
	if base == "nginx" || base == "static" {
		return "nginx-proxy"
	}

	// Runtimes with native HTTP (Node, Bun, Go, Rust, Python, etc.) use builtin.
	if hasNativeHTTP {
		return "builtin"
	}

	// Fallback for runtimes without native HTTP.
	return "nginx-proxy"
}

// ResolveOS determines the base OS preference for the recipe.
// Most Zerops runtimes default to ubuntu-22. Alpine is preferred for Go/Rust (static binaries).
func ResolveOS(runtimeType string) string {
	base, _, _ := strings.Cut(runtimeType, "@")
	switch base {
	case "go", "rust":
		return "alpine"
	default:
		return "ubuntu-22"
	}
}

// ResolveDevTooling determines the development iteration strategy.
// Returns: "hot-reload", "watch", or "manual".
func ResolveDevTooling(framework, runtimeType string) string {
	base, _, _ := strings.Cut(runtimeType, "@")

	// Node/Bun frameworks typically support hot-reload.
	switch base {
	case "nodejs", "bun": //nolint:goconst // runtime name, not a shared constant
		return "hot-reload"
	}

	// Python frameworks (Django, Flask) support watch mode.
	if strings.HasPrefix(base, "python") {
		return "watch"
	}

	// PHP frameworks: artisan serve can watch, but PHP-FPM doesn't.
	if strings.HasPrefix(base, "php") {
		if framework == "laravel" || framework == "symfony" {
			return "watch"
		}
		return "manual"
	}

	// Go, Rust, Java: manual redeploy.
	return "manual"
}
