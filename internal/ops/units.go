package ops

import "context"

// UnitRemover removes the zcp container's zerops@<name> units whose names
// start with a prefix (platform.SystemUnits in a container).
type UnitRemover interface {
	RemoveUnits(ctx context.Context, prefix string) error
}
