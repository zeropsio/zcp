package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

type unreadableStatusMounter struct{ *stubMounter }

func (*unreadableStatusMounter) CheckMount(context.Context, string) (platform.MountState, error) {
	return platform.MountStateNotMounted, errors.New("mount observation did not answer")
}

// Decision: A mount read that cannot answer is not reported as an absent mount.
func TestMountTool_UnreadableStatus_IsErrorWithoutAbsentMountFacts(t *testing.T) {
	t.Parallel()
	for _, hostname := range []string{"app", "apppool-3", ""} {
		t.Run(hostname, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "app"}})
			srv := mountServer(mock, &unreadableStatusMounter{newStubMounter()})
			result := callTool(t, srv, "zerops_mount", map[string]any{"action": "status", "serviceHostname": hostname})
			text := getTextContent(t, result)
			if !result.IsError || !strings.Contains(text, "mount observation did not answer") {
				t.Fatalf("unreadable status must use the existing error response and preserve its diagnostic: isError %v, text %s", result.IsError, text)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal([]byte(text), &fields); err != nil {
				t.Fatalf("error response is not JSON: %v", err)
			}
			if mounts, ok := fields["mounts"]; ok {
				t.Fatalf("unreadable status must not supply absent mount facts: %s", mounts)
			}
			if result.StructuredContent != nil {
				t.Fatalf("tool result must use the existing text error envelope: structured content %v", result.StructuredContent)
			}
		})
	}
}
