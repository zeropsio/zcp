package tools

import (
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestContainerCreationAnchor_PriorityOrder(t *testing.T) {
	t.Parallel()

	ccs := time.Date(2026, 4, 22, 6, 5, 0, 0, time.UTC).Format(time.RFC3339Nano)
	pfinish := time.Date(2026, 4, 22, 6, 4, 58, 0, time.UTC).Format(time.RFC3339Nano)
	pfailed := time.Date(2026, 4, 22, 6, 4, 55, 0, time.UTC).Format(time.RFC3339Nano)
	pstart := time.Date(2026, 4, 22, 6, 4, 0, 0, time.UTC).Format(time.RFC3339Nano)

	tests := []struct {
		name  string
		build *platform.BuildInfo
		want  string
	}{
		{
			name:  "nil build returns zero time",
			build: nil,
			want:  "",
		},
		{
			name: "ContainerCreationStart wins over PipelineFinish",
			build: &platform.BuildInfo{
				ContainerCreationStart: &ccs,
				PipelineFinish:         &pfinish,
				PipelineFailed:         &pfailed,
				PipelineStart:          &pstart,
			},
			want: ccs,
		},
		{
			name: "PipelineFinish wins over PipelineFailed",
			build: &platform.BuildInfo{
				PipelineFinish: &pfinish,
				PipelineFailed: &pfailed,
				PipelineStart:  &pstart,
			},
			want: pfinish,
		},
		{
			name: "PipelineFailed wins over PipelineStart",
			build: &platform.BuildInfo{
				PipelineFailed: &pfailed,
				PipelineStart:  &pstart,
			},
			want: pfailed,
		},
		{
			name: "PipelineStart is the last fallback",
			build: &platform.BuildInfo{
				PipelineStart: &pstart,
			},
			want: pstart,
		},
		{
			name:  "nothing set returns zero",
			build: &platform.BuildInfo{},
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ev := &platform.AppVersionEvent{Build: tt.build}
			got := containerCreationAnchor(ev)
			if tt.want == "" {
				if !got.IsZero() {
					t.Errorf("expected zero time, got %v", got)
				}
				return
			}
			wantT, _ := time.Parse(time.RFC3339Nano, tt.want)
			if !got.Equal(wantT) {
				t.Errorf("got %v, want %v", got, wantT)
			}
		})
	}
}
