// Tests for: server.go — every tool call in a Mate is told to the group
// recipe's follower (tools.RecipeFollower), as it starts and once it
// answered, never in the call's response path (docs/spec-mate.md §10.10).
package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// watchedCalls records what a follower is told: each call's tool and
// arguments as it starts, and whether it failed once it answered.
type watchedCalls struct {
	mu       sync.Mutex
	started  []string
	args     []string
	answered []bool
}

func (w *watchedCalls) Watch(tool string, args json.RawMessage) func(failed bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.started = append(w.started, tool)
	w.args = append(w.args, string(args))
	return func(failed bool) {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.answered = append(w.answered, failed)
	}
}

// TestFollowRecipe_ToolCall_ToldAsItStartsAndOnceItAnswered: the follower is
// told of a tool call before its handler runs — what it reads of the records
// is read before the call can move them — and, once the handler returned,
// whether the call failed: an error result, or no result at all. The
// handler's answer goes back as it was. A request that is no tool call is
// told nothing.
func TestFollowRecipe_ToolCall_ToldAsItStartsAndOnceItAnswered(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	tests := []struct {
		name       string
		method     string
		result     mcp.Result
		err        error
		wantTold   bool
		wantFailed bool
	}{
		{name: "answered", method: methodCallTool, result: successResult(), wantTold: true},
		{name: "answered with an error", method: methodCallTool, result: errorResult(t, "INVALID_PARAMETER"), wantTold: true, wantFailed: true},
		{name: "no result", method: methodCallTool, err: boom, wantTold: true, wantFailed: true},
		{name: "no tool call", method: "tools/list", result: &mcp.ListToolsResult{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			watched := &watchedCalls{}
			s := &Server{recipeFollower: watched}
			handler := s.followRecipe()(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
				watched.mu.Lock()
				defer watched.mu.Unlock()
				if tt.wantTold && (len(watched.started) != 1 || len(watched.answered) != 0) {
					t.Errorf("the handler ran with the follower told %v and answered %v, want it told the call started and nothing more", watched.started, watched.answered)
				}
				return tt.result, tt.err
			})

			got, err := handler(t.Context(), tt.method, callToolReq(t, "zerops_import", map[string]string{"content": "services: []"}))

			if got != tt.result || !errors.Is(err, tt.err) {
				t.Errorf("answered %v, %v; want %v, %v", got, err, tt.result, tt.err)
			}
			if !tt.wantTold {
				if len(watched.started) != 0 {
					t.Errorf("a %s request was told to the follower", tt.method)
				}
				return
			}
			if len(watched.started) != 1 || watched.started[0] != "zerops_import" || watched.args[0] != `{"content":"services: []"}` {
				t.Errorf("the follower was told %v %v, want the call of zerops_import with its arguments", watched.started, watched.args)
			}
			if len(watched.answered) != 1 || watched.answered[0] != tt.wantFailed {
				t.Errorf("the follower was told failed %v, want [%v]", watched.answered, tt.wantFailed)
			}
		})
	}
}

// TestNew_RecipeFollower_OnlyInAMate: a Mate's server follows the recipe
// after its tool calls; any other server has no follower (spec-mate §2.0).
func TestNew_RecipeFollower_OnlyInAMate(t *testing.T) {
	tests := []struct {
		name string
		rt   runtime.Info
		want bool
	}{
		{name: "a Mate", rt: runtime.Info{InContainer: true, ServiceID: "s1", ProjectID: "p1", MateEnabled: true,
			HQ: &runtime.HQLink{Origin: "https://hq.invalid", ProjectID: "p1", Key: "the-project-key"}}, want: true},
		{name: "a container that is no Mate", rt: runtime.Info{InContainer: true, ServiceID: "s1", ProjectID: "p1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Non-parallel: t.Chdir (the state dir) and a swap of
			// http.DefaultTransport (no HQ answers the start pass).
			t.Chdir(t.TempDir())
			orig := http.DefaultTransport
			http.DefaultTransport = heldHQ{asked: func() {}, release: closedChannel()}
			t.Cleanup(func() { http.DefaultTransport = orig })
			store, err := knowledge.GetEmbeddedStore()
			if err != nil {
				t.Fatalf("knowledge store: %v", err)
			}
			srv := New(t.Context(), platform.NewMock().WithProject(&platform.Project{ID: "p1", Name: "test"}).WithServices(nil),
				&auth.Info{ProjectID: "p1", Token: "test", APIHost: "localhost"}, store, platform.NewMockLogFetcher(), nil, nil, tt.rt, nil)
			t.Cleanup(srv.Stop)

			if got := srv.recipeFollower != nil; got != tt.want {
				t.Errorf("follower = %v, want %v", got, tt.want)
			}
		})
	}
}

func closedChannel() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
