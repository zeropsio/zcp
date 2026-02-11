//go:build api

package apitest

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

const defaultAPIHost = "api.app-prg1.zerops.io"

// APIHarness provides a real Zerops API client for contract tests.
type APIHarness struct {
	t         *testing.T
	client    platform.Client
	projectID string
	ctx       context.Context
	cancel    context.CancelFunc
	cleanups  []func()
}

// New creates an APIHarness. It skips the test if ZCP_API_KEY is not set.
func New(t *testing.T) *APIHarness {
	t.Helper()

	token := os.Getenv("ZCP_API_KEY")
	if token == "" {
		t.Skip("ZCP_API_KEY not set")
	}

	apiHost := os.Getenv("ZCP_API_HOST")
	if apiHost == "" {
		apiHost = defaultAPIHost
	}

	client, err := platform.NewZeropsClient(token, apiHost)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)

	info, err := client.GetUserInfo(ctx)
	if err != nil {
		cancel()
		t.Fatalf("GetUserInfo failed: %v", err)
	}

	projects, err := client.ListProjects(ctx, info.ID)
	if err != nil {
		cancel()
		t.Fatalf("ListProjects failed: %v", err)
	}
	if len(projects) == 0 {
		cancel()
		t.Fatal("no projects found for this token")
	}

	h := &APIHarness{
		t:         t,
		client:    client,
		projectID: projects[0].ID,
		ctx:       ctx,
		cancel:    cancel,
	}

	t.Cleanup(func() {
		for i := len(h.cleanups) - 1; i >= 0; i-- {
			h.cleanups[i]()
		}
		cancel()
	})

	return h
}

// Client returns the real Zerops API client.
func (h *APIHarness) Client() platform.Client {
	return h.client
}

// Ctx returns the timeout-bounded context.
func (h *APIHarness) Ctx() context.Context {
	return h.ctx
}

// ProjectID returns the discovered project ID.
func (h *APIHarness) ProjectID() string {
	return h.projectID
}

// Cleanup registers a cleanup function to run after the test.
func (h *APIHarness) Cleanup(fn func()) {
	h.cleanups = append(h.cleanups, fn)
}
