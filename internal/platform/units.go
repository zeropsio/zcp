package platform

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// unitTimeout bounds each supervisor call — a `systemctl cat` or a
// `zsc unit create`, which returns once the unit is registered and started.
const unitTimeout = 30 * time.Second

// unitNameRe bounds a unit name to what `zsc unit create` renders as
// zerops@<name>.service without quoting.
var unitNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// SystemUnits puts long-lived commands under the zcp container's service
// supervisor through `zsc unit create` — the primitive the sshfs mounts and
// the mate server already use. A unit created this way is restarted when its
// process exits (Restart=always) and survives a restart of the container; a
// redeploy of the zcp container drops it.
type SystemUnits struct{}

// NewSystemUnits creates a SystemUnits.
func NewSystemUnits() *SystemUnits {
	return &SystemUnits{}
}

// EnsureUnit registers command as unit zerops@<name> unless it is registered
// already: `zsc unit` has create and remove and no upsert, so the unit's
// presence (`systemctl cat`, no sudo) is the idempotency check, and an
// existing unit keeps the command it was created with.
func (u *SystemUnits) EnsureUnit(ctx context.Context, name, command string) error {
	if !unitNameRe.MatchString(name) {
		return fmt.Errorf("unit name %q: want lowercase letters, digits and dashes", name)
	}
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("unit %s: empty command", name)
	}
	if execWithTimeout(ctx, unitTimeout, "systemctl", "cat", "zerops@"+name+".service") == nil {
		return nil
	}
	if err := execWithTimeout(ctx, unitTimeout, "sudo", "-E", "zsc", "unit", "create", name, command); err != nil {
		return fmt.Errorf("zsc unit create %s: %w", name, err)
	}
	return nil
}
