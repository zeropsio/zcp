package knowledge

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

func TestPrependModeAdaptation_RuntimeAware(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		mode         topology.Mode
		runtime      string
		wantContains []string
		wantAbsent   []string
	}{
		{
			name:    "dev_standard_points_to_dev_setup",
			mode:    "standard",
			runtime: "nodejs",
			wantContains: []string{
				"`dev`",
				"setup block",
			},
			wantAbsent: []string{
				"zsc noop",
				"deployFiles: [.]",
				"healthCheck",
			},
		},
		{
			name:    "dev_mode_points_to_dev_setup",
			mode:    "dev",
			runtime: "go",
			wantContains: []string{
				"`dev`",
				"setup block",
			},
			wantAbsent: []string{
				"zsc noop",
				"deployFiles: [.]",
			},
		},
		{
			name:    "simple_points_to_prod_with_deployfiles_note",
			mode:    "simple",
			runtime: "nodejs",
			wantContains: []string{
				"`prod`",
				"deployFiles: [.]",
			},
			wantAbsent: []string{
				"zsc noop",
				"healthCheck",
			},
		},
		{
			name:    "empty_runtime_still_works",
			mode:    "standard",
			runtime: "",
			wantContains: []string{
				"`dev`",
			},
		},
		{
			name:         "empty_mode_returns_empty",
			mode:         "",
			runtime:      "nodejs",
			wantContains: []string{},
		},
		{
			name:    "all_modes_are_concise",
			mode:    "dev",
			runtime: "php",
			wantContains: []string{
				"`dev`",
			},
			wantAbsent: []string{
				"Omit",
				"webserver",
				// Mode header should be a single concise line, not multi-line instructions
				"start:",
			},
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
			// Mode adaptation must be concise — single blockquote line, not multi-line instructions.
			lines := strings.Split(strings.TrimSpace(result), "\n")
			nonEmpty := 0
			for _, l := range lines {
				if strings.TrimSpace(l) != "" {
					nonEmpty++
				}
			}
			if nonEmpty > 2 {
				t.Errorf("mode adaptation should be concise (max 2 non-empty lines), got %d:\n%s", nonEmpty, result)
			}
		})
	}
}
