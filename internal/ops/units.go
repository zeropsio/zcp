package ops

import "context"

// UnitRegistrar puts a long-lived command under the zcp container's service
// supervisor, idempotently (platform.SystemUnits in a container, nil
// locally). The supervisor restarts the command when it exits and brings it
// back after the zcp container restarts.
type UnitRegistrar interface {
	EnsureUnit(ctx context.Context, name, command string) error
}
