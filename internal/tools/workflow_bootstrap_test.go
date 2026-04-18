// Tests for: injectStacks — F11 fail-loudly on missing markers.
package tools

import (
	"errors"
	"strings"
	"testing"
)

func TestInjectStacks(t *testing.T) {
	t.Parallel()

	const stackList = "STACK_LIST_PLACEHOLDER"

	tests := []struct {
		name        string
		content     string
		wantInject  bool
		wantErr     error
		wantSubstrs []string
	}{
		{
			name:        "markers present — replaces marker block",
			content:     "header\n<!-- STACKS:BEGIN -->\nOLD\n<!-- STACKS:END -->\nfooter",
			wantInject:  true,
			wantSubstrs: []string{"header", stackList, "footer"},
		},
		{
			name:        "anchor fallback (## Part 1) — inserts before anchor",
			content:     "intro text\n\n## Part 1\nbody",
			wantInject:  true,
			wantSubstrs: []string{"intro text", stackList, "## Part 1"},
		},
		{
			name:    "no markers and no anchor — fails loudly",
			content: "plain content without any hook",
			wantErr: ErrMissingStacksHook,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := injectStacks(tt.content, stackList)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("want error %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantInject && !strings.Contains(got, stackList) {
				t.Errorf("stackList not injected; got: %q", got)
			}
			for _, want := range tt.wantSubstrs {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in:\n%s", want, got)
				}
			}
		})
	}
}
