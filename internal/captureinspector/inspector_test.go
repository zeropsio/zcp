package captureinspector

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestFacade_StartAndCloseOwnsOnlyExplicitInspectorLifecycle(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	server, err := Start(ctx, Config{
		ListenAddr:      "127.0.0.1:0",
		CaptureRoot:     t.TempDir(),
		CapabilityToken: "capability",
		RevealToken:     "reveal",
	})
	if err != nil {
		cancel()
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		cancel()
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
		defer closeCancel()
		if err := server.Close(closeCtx); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	if !strings.HasPrefix(server.URL(), "http://127.0.0.1:") {
		t.Fatalf("URL() = %q, want loopback", server.URL())
	}
	if server.LaunchURL() != server.URL()+"/launch/capability" {
		t.Fatalf("LaunchURL() = %q", server.LaunchURL())
	}
}
