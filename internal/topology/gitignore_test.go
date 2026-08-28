// Tests for: topology/gitignore.go — the pure runtime-type → .gitignore-body
// mapping every host-side git-init self-heal site (ops.GitEnsureRepoHeadCommand,
// BuildGitOriginSyncCommand, BuildGitReconstructCommand) composes from.
package topology

import (
	"slices"
	"strings"
	"testing"
)

func TestGitignoreFor_BaseAlwaysPresent(t *testing.T) {
	t.Parallel()

	base := []string{".env", ".zcp/", ".DS_Store", "*.log"}
	cases := []string{"", "nodejs@22", "unknown-service@1", "postgresql@18"}
	for _, serviceType := range cases {
		lines := GitignoreFor(serviceType)
		for _, want := range base {
			if !containsLine(lines, want) {
				t.Errorf("GitignoreFor(%q) missing base line %q, got %v", serviceType, want, lines)
			}
		}
	}
}

func TestGitignoreFor_LanguageBlocks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		serviceType string
		wantExtra   []string
		wantAbsent  []string
	}{
		{
			name:        "nodejs bare",
			serviceType: "nodejs@22",
			wantExtra:   []string{"node_modules/", "dist/", ".next/", ".nuxt/", ".output/"},
		},
		{
			name:        "nodejs composite OS-prefixed",
			serviceType: "alpine/nodejs@22",
			wantExtra:   []string{"node_modules/"},
		},
		{
			name:        "bun classifies as node family",
			serviceType: "bun@1.1",
			wantExtra:   []string{"node_modules/"},
		},
		{
			name:        "deno classifies as node family",
			serviceType: "deno@1.4",
			wantExtra:   []string{"node_modules/"},
		},
		{
			name:        "python",
			serviceType: "python@3.12",
			wantExtra:   []string{"__pycache__/", ".venv/", "venv/"},
			wantAbsent:  []string{"node_modules/"},
		},
		{
			name:        "php-apache",
			serviceType: "php-apache@8.3",
			wantExtra:   []string{"vendor/"},
		},
		{
			name:        "php-nginx",
			serviceType: "php-nginx@8.3",
			wantExtra:   []string{"vendor/"},
		},
		{
			name:        "rust",
			serviceType: "rust@1.75",
			wantExtra:   []string{"target/"},
		},
		{
			name:        "dotnet",
			serviceType: "dotnet@8.0",
			wantExtra:   []string{"bin/", "obj/"},
		},
		{
			name:        "java",
			serviceType: "java@21",
			wantExtra:   []string{"target/", "build/"},
		},
		{
			name:        "ruby",
			serviceType: "ruby@3.3",
			wantExtra:   []string{"vendor/bundle/"},
		},
		{
			name:        "go stays base-only (conservative — no standard build-output dir)",
			serviceType: "go@1.22",
			wantAbsent:  []string{"bin/", "target/"},
		},
		{
			name:        "empty type stays base-only",
			serviceType: "",
			wantAbsent:  []string{"node_modules/", "vendor/", "target/"},
		},
		{
			name:        "unrecognized type stays base-only",
			serviceType: "some-future-runtime@1",
			wantAbsent:  []string{"node_modules/", "vendor/", "target/"},
		},
		{
			name:        "managed service stays base-only",
			serviceType: "postgresql@18",
			wantAbsent:  []string{"node_modules/", "vendor/", "target/"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lines := GitignoreFor(tt.serviceType)
			for _, want := range tt.wantExtra {
				if !containsLine(lines, want) {
					t.Errorf("GitignoreFor(%q) missing %q, got %v", tt.serviceType, want, lines)
				}
			}
			for _, absent := range tt.wantAbsent {
				if containsLine(lines, absent) {
					t.Errorf("GitignoreFor(%q) should NOT contain %q, got %v", tt.serviceType, absent, lines)
				}
			}
		})
	}
}

// TestGitignoreFor_NeverEmpty pins the "unknown type ⇒ base only, never
// nothing" contract — every input, however unrecognized, still returns the
// base hygiene lines.
func TestGitignoreFor_NeverEmpty(t *testing.T) {
	t.Parallel()
	if lines := GitignoreFor("totally-unknown@0"); len(lines) == 0 {
		t.Error("GitignoreFor should never return an empty slice")
	}
}

// TestGitignoreFor_NoDuplicateLines guards against a language block
// accidentally repeating a base line (which would double-print in the
// generated file).
func TestGitignoreFor_NoDuplicateLines(t *testing.T) {
	t.Parallel()
	for _, serviceType := range []string{"nodejs@22", "python@3.12", "php-apache@8.3", "rust@1.75", "dotnet@8.0", "java@21", "ruby@3.3", "go@1.22", ""} {
		seen := map[string]bool{}
		for _, line := range GitignoreFor(serviceType) {
			if seen[line] {
				t.Errorf("GitignoreFor(%q) has duplicate line %q", serviceType, line)
			}
			seen[line] = true
		}
	}
}

func containsLine(lines []string, want string) bool {
	return slices.Contains(lines, want)
}

// TestGitignoreFor_JoinsCleanly proves the returned lines are ready to be
// written one-per-line with no embedded newlines/blank entries that would
// corrupt the generated .gitignore.
func TestGitignoreFor_JoinsCleanly(t *testing.T) {
	t.Parallel()
	for _, line := range GitignoreFor("nodejs@22") {
		if line == "" {
			t.Error("GitignoreFor returned an empty line")
		}
		if strings.ContainsAny(line, "\n\r") {
			t.Errorf("GitignoreFor line contains a newline: %q", line)
		}
	}
}
