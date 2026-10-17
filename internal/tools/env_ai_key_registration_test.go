package tools_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
)

// non-parallel: listAllTools builds the whole server, which reads HOME.
func TestEnvAIKey_OnlyAMatesServerOffersIt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	for _, tc := range []struct {
		name string
		rt   runtime.Info
		want bool
	}{
		{name: "a Mate", rt: runtime.Info{InContainer: true, ServiceName: "zcp", MateEnabled: true}, want: true},
		{name: "a container that is not a Mate", rt: runtime.Info{InContainer: true, ServiceName: "zcp"}},
		{name: "local", rt: runtime.Info{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := listAllTools(t, tc.rt)["zerops_env"]
			if tool == nil {
				t.Fatal("zerops_env is not registered")
			}
			schema, err := json.Marshal(tool.InputSchema)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(string(schema), `"ai-key"`); got != tc.want {
				t.Errorf("zerops_env offers ai-key = %v, want %v", got, tc.want)
			}
		})
	}
}
