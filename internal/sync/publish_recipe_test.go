package sync

import "testing"

// TestRepoNameForPublish asserts the pure naming helper that drives
// multi-repo recipe publish. Default (empty) suffix preserves the
// {slug}-app backward-compatible name so existing single-codebase
// recipes keep resolving to their original GitHub repo. Explicit
// suffixes allow dual-runtime / separate-worker recipes to land
// each codebase in its own repo (nestjs-showcase-api, -worker, ...).
func TestRepoNameForPublish(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		slug   string
		suffix string
		want   string
	}{
		{"default suffix preserves backward compat", "laravel-minimal", "", "laravel-minimal-app"},
		{"explicit app suffix equals default", "laravel-minimal", "app", "laravel-minimal-app"},
		{"api suffix for dual-runtime backend", "nestjs-showcase", "api", "nestjs-showcase-api"},
		{"worker suffix for separate worker", "nestjs-showcase", "worker", "nestjs-showcase-worker"},
		{"frontend suffix", "svelte-showcase", "app", "svelte-showcase-app"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := repoNameForPublish(tt.slug, tt.suffix)
			if got != tt.want {
				t.Errorf("repoNameForPublish(%q, %q) = %q, want %q", tt.slug, tt.suffix, got, tt.want)
			}
		})
	}
}
