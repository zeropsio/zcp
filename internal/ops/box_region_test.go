// Tests for: ops/import_targets.go — the region pre-check a Zerops Box's
// import and a bootstrap plan carrying one pass before anything is created
// (docs/spec-workflows.md §8 PS-16).
package ops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// The two project records the check reads, as the live API answers them
// (primaryInstanceLocation, live-verified 2026-10-24).
func prg1Project() *platform.Project {
	return &platform.Project{ID: "proj-1", Name: "lab", LocationID: "eu-central", LocationName: "EU Central (prg1)"}
}

func ny1Project() *platform.Project {
	return &platform.Project{ID: "proj-1", Name: "lab", LocationID: "us-east-1", LocationName: "US East (ny1)"}
}

func TestBoxRegionRefusal(t *testing.T) {
	t.Parallel()
	const outcome = "Nothing was imported."
	tests := []struct {
		name      string
		boxes     []string
		project   *platform.Project
		readErr   error
		wantReads int
		wantNamed []string // nil: no refusal
	}{
		{"no Box: the project is never read", nil, ny1Project(), nil, 0, nil},
		{"a Box in prg1 passes", []string{"box"}, prg1Project(), nil, 1, nil},
		{"Boxes outside prg1 are refused by name", []string{"box", "sandbox"}, ny1Project(), nil, 1,
			[]string{outcome, "box", "sandbox", "Zerops Box", "only in a project in region prg1", "US East (ny1)"}},
		{"a read that fails lets the platform answer", []string{"box"}, nil, errors.New("api down"), 1, nil},
		{"a record naming no location lets the platform answer", []string{"box"}, &platform.Project{ID: "proj-1"}, nil, 1, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock()
			if tt.project != nil {
				mock = mock.WithProject(tt.project)
			}
			if tt.readErr != nil {
				mock = mock.WithError("GetProject", tt.readErr)
			}
			pe := BoxRegionRefusal(context.Background(), mock, "proj-1", tt.boxes, outcome)
			if got := mock.CallCount("GetProject"); got != tt.wantReads {
				t.Errorf("GetProject reads = %d, want %d", got, tt.wantReads)
			}
			if tt.wantNamed == nil {
				if pe != nil {
					t.Fatalf("want no refusal, got %v", pe)
				}
				return
			}
			if pe == nil {
				t.Fatal("want a refusal before anything is created")
			}
			if pe.Code != platform.ErrPrerequisiteMissing {
				t.Errorf("code = %q, want %q", pe.Code, platform.ErrPrerequisiteMissing)
			}
			for _, want := range tt.wantNamed {
				if !strings.Contains(pe.Message, want) {
					t.Errorf("message %q missing %q", pe.Message, want)
				}
			}
			if !strings.Contains(pe.Suggestion, "prg1") {
				t.Errorf("suggestion %q must name a project in prg1 as the way forward", pe.Suggestion)
			}
		})
	}
}
