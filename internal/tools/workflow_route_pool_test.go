package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

func poolSandboxStack(id, name, typ, category string) platform.ServiceStack {
	return platform.ServiceStack{
		ID: id, Name: name, Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName:  typ,
			ServiceStackTypeCategoryName: category,
		},
	}
}

// A Swarm pool or Zerops Box is never suggested for adoption by the route
// (spec-workflows.md §8 PS-2): the adopt hint names only real app runtimes.
func TestWorkflowRoute_PoolNotAdoptable(t *testing.T) {
	t.Parallel()

	t.Run("pool and box alone — no adopt hint", func(t *testing.T) {
		t.Parallel()
		mock := platform.NewMock().WithServices([]platform.ServiceStack{
			poolSandboxStack("s1", "pool", "swarm@1", "USER"),
			poolSandboxStack("s2", "poolvm", "swarm-vm@1", "USER"),
			poolSandboxStack("s3", "box", "ubuntu/zbox@1", "STANDARD"),
		})
		result, _, _ := handleRoute(context.Background(), nil, mock, "p1", t.TempDir(), "", runtime.Info{})
		text := getTextContent(t, result)
		if strings.Contains(text, "adopt:") {
			t.Errorf("route must not offer adoption for a pool or Box; got:\n%s", text)
		}
	})

	t.Run("beside an app — adopt hint names only the app", func(t *testing.T) {
		t.Parallel()
		mock := platform.NewMock().WithServices([]platform.ServiceStack{
			poolSandboxStack("s1", "pool", "swarm@1", "USER"),
			poolSandboxStack("s3", "box", "ubuntu/zbox@1", "STANDARD"),
			poolSandboxStack("s4", "appdev", "nodejs@22", "USER"),
		})
		result, _, _ := handleRoute(context.Background(), nil, mock, "p1", t.TempDir(), "", runtime.Info{})
		text := getTextContent(t, result)
		if !strings.Contains(text, "adopt: appdev") {
			t.Fatalf("route should offer adopting appdev; got:\n%s", text)
		}
		for _, h := range []string{"pool", "box"} {
			if strings.Contains(text, "adopt: appdev, "+h) || strings.Contains(text, "adopt: "+h) {
				t.Errorf("route must not list %q for adoption; got:\n%s", h, text)
			}
		}
	})
}
