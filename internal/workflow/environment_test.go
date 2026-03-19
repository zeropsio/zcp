package workflow

import (
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
)

func TestDetectEnvironment(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		rt   runtime.Info
		want Environment
	}{
		{
			name: "container",
			rt:   runtime.Info{InContainer: true, ServiceName: "zcpx", ServiceID: "abc"},
			want: EnvContainer,
		},
		{
			name: "local",
			rt:   runtime.Info{},
			want: EnvLocal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := DetectEnvironment(tt.rt); got != tt.want {
				t.Errorf("DetectEnvironment() = %q, want %q", got, tt.want)
			}
		})
	}
}
