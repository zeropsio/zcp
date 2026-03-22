package knowledge

import (
	"strings"
	"testing"
)

func TestPrependModeAdaptation_RuntimeAware(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		mode         string
		runtime      string
		wantContains []string
		wantAbsent   []string
	}{
		{
			name:    "standard_nodejs_uses_zsc_noop",
			mode:    "standard",
			runtime: "nodejs",
			wantContains: []string{
				"zsc noop --silent",
				"Mode: dev",
			},
			wantAbsent: []string{
				"webserver",
				"Omit `start:`",
			},
		},
		{
			name:    "standard_go_uses_zsc_noop",
			mode:    "standard",
			runtime: "go",
			wantContains: []string{
				"zsc noop --silent",
			},
		},
		{
			name:    "standard_php_omits_start",
			mode:    "standard",
			runtime: "php",
			wantContains: []string{
				"Omit",
				"start:",
				"webserver",
			},
			wantAbsent: []string{
				"zsc noop",
			},
		},
		{
			name:    "dev_php_omits_start",
			mode:    "dev",
			runtime: "php",
			wantContains: []string{
				"Omit",
			},
			wantAbsent: []string{
				"zsc noop",
			},
		},
		{
			name:    "simple_nodejs_uses_real_start",
			mode:    "simple",
			runtime: "nodejs",
			wantContains: []string{
				"start command",
				"healthCheck",
				"deployFiles: [.]",
			},
			wantAbsent: []string{
				"zsc noop",
			},
		},
		{
			name:    "simple_php_omits_start_and_ports",
			mode:    "simple",
			runtime: "php",
			wantContains: []string{
				"Omit",
				"start:",
				"webserver",
			},
			wantAbsent: []string{
				"zsc noop",
			},
		},
		{
			name:    "empty_runtime_defaults_to_dynamic",
			mode:    "standard",
			runtime: "",
			wantContains: []string{
				"zsc noop --silent",
			},
		},
		{
			name:         "empty_mode_returns_empty",
			mode:         "",
			runtime:      "nodejs",
			wantContains: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := prependModeAdaptation(tt.mode, tt.runtime)
			if tt.mode == "" {
				if result != "" {
					t.Errorf("expected empty for empty mode, got: %s", result)
				}
				return
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("should contain %q, got:\n%s", want, result)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(result, absent) {
					t.Errorf("should NOT contain %q, got:\n%s", absent, result)
				}
			}
		})
	}
}

func TestFilterDeployPatterns_SimpleModeShowsBoth(t *testing.T) {
	t.Parallel()
	guide := `# Go on Zerops

### Deploy Patterns

**Dev deploy**: deployFiles: [.], start: zsc noop --silent
**Prod deploy**: buildCommands: [go build -o app], deployFiles: app, start: ./app

### Common Mistakes
`
	tests := []struct {
		name         string
		mode         string
		wantContains []string
		wantAbsent   []string
	}{
		{
			name:         "dev_keeps_only_dev",
			mode:         "dev",
			wantContains: []string{"Dev deploy"},
			wantAbsent:   []string{"Prod deploy"},
		},
		{
			name:         "standard_keeps_only_dev",
			mode:         "standard",
			wantContains: []string{"Dev deploy"},
			wantAbsent:   []string{"Prod deploy"},
		},
		{
			name:         "simple_shows_both_patterns",
			mode:         "simple",
			wantContains: []string{"Dev deploy", "Prod deploy"},
		},
		{
			name:         "stage_keeps_only_prod",
			mode:         "stage",
			wantContains: []string{"Prod deploy"},
			wantAbsent:   []string{"Dev deploy"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := filterDeployPatterns(guide, tt.mode)
			for _, want := range tt.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("should contain %q, got:\n%s", want, result)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(result, absent) {
					t.Errorf("should NOT contain %q, got:\n%s", absent, result)
				}
			}
		})
	}
}

func TestIsImplicitWebserverRuntime(t *testing.T) {
	t.Parallel()
	tests := []struct {
		runtime string
		want    bool
	}{
		{"php", true},
		{"php-nginx", true},
		{"php-apache", true},
		{"nginx", true},
		{"static", true},
		{"nodejs", false},
		{"go", false},
		{"python", false},
		{"bun", false},
		{"java", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.runtime, func(t *testing.T) {
			t.Parallel()
			got := isImplicitWebserverRuntime(tt.runtime)
			if got != tt.want {
				t.Errorf("isImplicitWebserverRuntime(%q) = %v, want %v", tt.runtime, got, tt.want)
			}
		})
	}
}
