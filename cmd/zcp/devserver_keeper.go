package main

import (
	"context"
	"fmt"
	"io"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/tools"
)

// retireDevServerKeeperAtBoot removes the units an older zcp's dev-server
// keeper left on this container. `zcp init` runs at every container start, so
// a container nobody opens an agent session on (where `zcp serve` retires
// them too) still loses a unit that relaunches a keeper every few seconds.
// Best-effort: a failure is reported and the start goes on.
func retireDevServerKeeperAtBoot(ctx context.Context, rt runtime.Info, units ops.UnitRemover, stderr io.Writer) {
	if !rt.InContainer {
		return
	}
	if err := tools.RetireDevServerKeeper(ctx, units); err != nil {
		fmt.Fprintf(stderr, "  ! %v\n    (continuing — the old keeper's units keep relaunching until the next start)\n", err)
	}
}
