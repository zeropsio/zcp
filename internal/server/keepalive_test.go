// Tests for: server package — keep-alive progress on long tool calls.
package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// beatLog records the progress notifications a client receives.
type beatLog struct {
	mu    sync.Mutex
	times []time.Time
	token []any
}

func (b *beatLog) handle(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.times = append(b.times, time.Now())
	b.token = append(b.token, req.Params.ProgressToken)
}

func (b *beatLog) snapshot() ([]time.Time, []any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]time.Time(nil), b.times...), append([]any(nil), b.token...)
}

// connectSilentTool serves one tool that stays silent for `silent`, behind
// keepAlive(interval, gap), and returns a client session whose progress
// notifications land in the returned log.
func connectSilentTool(t *testing.T, silent, interval, gap time.Duration) (*mcp.ClientSession, *beatLog) {
	t.Helper()
	ctx := t.Context()
	srv := mcp.NewServer(&mcp.Implementation{Name: "zcp", Version: "test"}, nil)
	srv.AddReceivingMiddleware(keepAlive(interval, gap))
	mcp.AddTool(srv, &mcp.Tool{Name: "silent"}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		select {
		case <-time.After(silent):
		case <-ctx.Done():
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "done"}}}, nil, nil
	})
	st, ct := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	log := &beatLog{}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, &mcp.ClientOptions{
		ProgressNotificationHandler: log.handle,
	})
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session, log
}

// A client with a request timeout (OpenCode's MCP SDK default is 60 s) resets
// it on each progress notification. A deploy goes quiet for minutes outside
// its build poll (the zcli push, HQ delivery), so every tool call that asked
// for progress hears from zcp at least once per interval while it runs.
func TestKeepAlive_SilentToolCall_SendsProgressEachInterval(t *testing.T) {
	t.Parallel()
	const interval = 20 * time.Millisecond
	session, log := connectSilentTool(t, 7*interval, interval, 0)

	params := &mcp.CallToolParams{Name: "silent", Arguments: map[string]any{}}
	params.SetProgressToken("deploy-1")
	result, err := session.CallTool(t.Context(), params)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool returned an error result: %+v", result.Content)
	}

	times, tokens := log.snapshot()
	if len(times) < 3 {
		t.Fatalf("got %d progress notifications over a %s silent call, want at least 3", len(times), 7*interval)
	}
	for i, tok := range tokens {
		if tok != "deploy-1" {
			t.Errorf("notification %d carried token %v, want deploy-1", i, tok)
		}
	}
}

// A client that sent no progress token cannot route a notification; zcp sends
// none (the SDK would reject an unknown token).
func TestKeepAlive_NoProgressToken_SendsNothing(t *testing.T) {
	t.Parallel()
	const interval = 20 * time.Millisecond
	session, log := connectSilentTool(t, 5*interval, interval, 0)

	if _, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "silent", Arguments: map[string]any{}}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if times, _ := log.snapshot(); len(times) != 0 {
		t.Fatalf("got %d progress notifications without a progress token, want 0", len(times))
	}
}

// A progress notification and its call's response in one stdin chunk tear
// down Claude Code's MCP transport (see pollProcess). The response therefore
// trails the last keep-alive by at least the gap.
func TestKeepAlive_ResponseAfterBeat_WaitsTheGap(t *testing.T) {
	t.Parallel()
	const (
		interval = 40 * time.Millisecond
		gap      = 60 * time.Millisecond
	)
	// The tool ends a few ms after the second beat.
	session, log := connectSilentTool(t, 2*interval+5*time.Millisecond, interval, gap)

	params := &mcp.CallToolParams{Name: "silent", Arguments: map[string]any{}}
	params.SetProgressToken("deploy-2")
	if _, err := session.CallTool(t.Context(), params); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	answered := time.Now()

	times, _ := log.snapshot()
	if len(times) == 0 {
		t.Fatal("no keep-alive was sent; the test's timing assumption broke")
	}
	if trail := answered.Sub(times[len(times)-1]); trail < gap-10*time.Millisecond {
		t.Fatalf("response came %s after the last keep-alive, want at least %s", trail, gap)
	}
}
