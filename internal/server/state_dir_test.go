package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
)

// witness: internal/server/server.go | if rt.StateDir != "" { → if rt.StateDir == "" {
func TestStateDir_SDOne_OneDirectoryPerContainer(t *testing.T) {
	// Non-parallel: t.Chdir puts the process in a crewmate's copy of the code.
	lane := filepath.Join(t.TempDir(), "appdev", ".crew", "ada")
	if err := os.MkdirAll(lane, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(lane)
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "a container, started in a crewmate's copy",
			env:  map[string]string{"serviceId": "svc1", "hostname": "zcp", "projectId": "proj1"},
			want: "/var/www/.zcp/state",
		},
		{
			name: "outside a container",
			env:  map[string]string{"hostname": "laptop"},
			want: filepath.Join(lane, ".zcp", "state"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stateDirOf(runtime.DetectFrom(func(key string) string { return tt.env[key] })); got != tt.want {
				t.Errorf("state directory = %q, want %q", got, tt.want)
			}
		})
	}
}
