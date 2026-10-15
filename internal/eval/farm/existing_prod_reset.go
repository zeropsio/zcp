package farm

import (
	"context"
	"fmt"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// ExistingProdResetFunc empties the standing existing-prod project after a
// run that launched into it (docs/spec-eval-farm.md §2.4): it receives the
// project id and its project-scoped token, the same pair the run was given.
type ExistingProdResetFunc func(ctx context.Context, projectID, token string) error

// NewExistingProdReset returns the real reset: it builds a client from the
// project-scoped token (ADMIN on that one project) against apiHost.
func NewExistingProdReset(apiHost string) ExistingProdResetFunc {
	return func(ctx context.Context, projectID, token string) error {
		client, err := platform.NewZeropsClient(token, apiHost)
		if err != nil {
			return fmt.Errorf("existing-prod reset: client: %w", err)
		}
		return deleteUserServices(ctx, client, projectID, 5*time.Second, 5*time.Minute)
	}
}

// deleteUserServices deletes every non-system service of projectID (the
// project keeps `core`) and waits until the direct read lists none, so the
// next run starts from an empty project.
func deleteUserServices(ctx context.Context, client platform.Client, projectID string, poll, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	deleted := map[string]bool{}
	for {
		services, err := client.ListServicesDirect(ctx, projectID)
		if err != nil {
			return fmt.Errorf("existing-prod reset: list services: %w", err)
		}
		remaining := 0
		for _, svc := range services {
			if svc.IsSystem() {
				continue
			}
			remaining++
			if deleted[svc.ID] {
				continue
			}
			if _, err := client.DeleteService(ctx, svc.ID); err != nil {
				return fmt.Errorf("existing-prod reset: delete %s: %w", svc.Name, err)
			}
			deleted[svc.ID] = true
		}
		if remaining == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("existing-prod reset: %d service(s) still present after %s", remaining, timeout)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("existing-prod reset: %w", ctx.Err())
		case <-time.After(poll):
		}
	}
}
