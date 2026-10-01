package ops

import (
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// IsolationService is the envIsolation mode of a project closed off: a
// service reads only its own variables and the project's, never another
// service's.
const IsolationService = "service"

// ProjectIsolation is the project's envIsolation mode as its variables carry
// it — the first word: the platform may follow "service" with per-service
// exceptions ("service service@zcp"). "" when the project does not say.
func ProjectIsolation(envs []platform.ProjectEnvVar) string {
	for _, e := range envs {
		if e.Key == "envIsolation" {
			if fields := strings.Fields(e.Content); len(fields) > 0 {
				return fields[0]
			}
			return ""
		}
	}
	return ""
}

// ProjectClosedOff reports a project whose envIsolation is "service".
func ProjectClosedOff(envs []platform.ProjectEnvVar) bool {
	return ProjectIsolation(envs) == IsolationService
}
