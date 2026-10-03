package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// ProjectEnvReader is the platform read a project's isolation comes from.
type ProjectEnvReader interface {
	GetProjectEnv(ctx context.Context, projectID string) ([]platform.ProjectEnvVar, error)
}

// ProjectClosedOff reads from Zerops whether a Mate's project is closed off:
// its envIsolation's first word is `service` — the container recipe's own
// `service service@zcp` as well as a plain `service` — and no ZCP_API_KEY is
// left project-wide, where isolation does not reach. What an import of its
// runtimes waits for: Zerops is the authority, and HQ's mark only a receipt.
//
// An error is "not known", never "open": a read that failed, or one that has
// not caught up with the project's birth — the platform gives every project
// envIsolation, so a list without it trails the writes.
func ProjectClosedOff(ctx context.Context, zerops ProjectEnvReader, projectID string) (bool, error) {
	env, err := zerops.GetProjectEnv(ctx, projectID)
	if err != nil {
		return false, fmt.Errorf("read the project's variables: %w", err)
	}
	isolation, found, keyed := "", false, false
	for _, v := range env {
		switch v.Key {
		case "envIsolation":
			isolation, found = v.Content, true
		case "ZCP_API_KEY":
			keyed = true
		}
	}
	if !found {
		return false, errors.New("the project's variables read without envIsolation yet")
	}
	fields := strings.Fields(isolation)
	return len(fields) > 0 && fields[0] == "service" && !keyed, nil
}
