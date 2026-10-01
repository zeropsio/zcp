package ops

import (
	"context"
	"errors"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestReadProjectClosedOff: a Mate's project is closed off once it carries
// the tag the press writes after it has closed the project off and read the
// isolation back — never from the isolation itself, which a new project
// reads as closed before its recipe opens it.
func TestReadProjectClosedOff(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		project *platform.Project
		err     error
		want    bool
		wantErr bool
	}{
		{"tagged", &platform.Project{ID: "p1", Tags: []string{"mate", ClosedOffTag}}, nil, true, false},
		{"not tagged", &platform.Project{ID: "p1", Tags: []string{"mate"}}, nil, false, false},
		{"a tag that only starts the same", &platform.Project{ID: "p1", Tags: []string{ClosedOffTag + "-not"}}, nil, false, false},
		{"unreadable", nil, errors.New("401"), false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock()
			if tt.project != nil {
				mock.WithProject(tt.project)
			}
			if tt.err != nil {
				mock.WithError("GetProject", tt.err)
			}
			got, err := ReadProjectClosedOff(context.Background(), mock, "p1")
			if got != tt.want || (err != nil) != tt.wantErr {
				t.Errorf("ReadProjectClosedOff = %v, %v; want %v, err=%v", got, err, tt.want, tt.wantErr)
			}
		})
	}
	if ClosedOffTag != "mate:closed-off" {
		t.Errorf("ClosedOffTag = %q, the press writes mate:closed-off", ClosedOffTag)
	}
}
