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
// its envIsolation is a rule set whose default is `service` — every rule
// `service` or `service@<hostname>`, as the web's `service@zcp` or a plain
// `service` — and no ZCP_API_KEY is left project-wide, where isolation does
// not reach. `none` and any rule set it does not know read open. Zerops is
// the authority, never HQ's mark.
//
// An error is "not known", never "closed off": a read that failed, or one
// without envIsolation — the platform gives every project one by the time
// its first services import returns.
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
		return false, errors.New("the project's variables read without envIsolation")
	}
	return serviceDefault(isolation) && !keyed, nil
}

// serviceDefault reports an isolation rule set whose default is `service`:
// at least one rule, each `service` or `service@<hostname>`.
func serviceDefault(rules string) bool {
	fields := strings.Fields(rules)
	for _, rule := range fields {
		host, scoped := strings.CutPrefix(rule, "service@")
		if rule != "service" && (!scoped || host == "") {
			return false
		}
	}
	return len(fields) > 0
}
