package platform

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// unitTimeout bounds each supervisor call — a `systemctl list-units` or a
// `zsc unit remove`.
const unitTimeout = 30 * time.Second

// unitNameRe bounds a unit name to what `zsc unit` renders as
// zerops@<name>.service without quoting.
var unitNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// SystemUnits manages the zcp container's zerops@<name> units — the units
// `zsc unit create` puts under its service supervisor.
type SystemUnits struct{}

// NewSystemUnits creates a SystemUnits.
func NewSystemUnits() *SystemUnits {
	return &SystemUnits{}
}

// RemoveUnits removes every zerops@<name> unit whose name starts with prefix
// through `zsc unit remove`; none is not an error.
func (u *SystemUnits) RemoveUnits(ctx context.Context, prefix string) error {
	if !unitNameRe.MatchString(strings.TrimSuffix(prefix, "-")) {
		return fmt.Errorf("unit prefix %q: want lowercase letters, digits and dashes", prefix)
	}
	listCtx, cancel := context.WithTimeout(ctx, unitTimeout)
	defer cancel()
	out, err := exec.CommandContext(listCtx, "systemctl", "list-units", "--all", "--plain", "--no-legend", "--type=service", "zerops@"+prefix+"*").Output() //nolint:gosec // prefix is checked against unitNameRe above
	if err != nil {
		return fmt.Errorf("list zerops@%s* units: %w", prefix, err)
	}
	for line := range strings.Lines(string(out)) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(fields[0], "zerops@"), ".service")
		if !strings.HasPrefix(name, prefix) || !unitNameRe.MatchString(name) {
			continue
		}
		if err := execWithTimeout(ctx, unitTimeout, "sudo", "-E", "zsc", "unit", "remove", name); err != nil {
			return fmt.Errorf("zsc unit remove %s: %w", name, err)
		}
	}
	return nil
}
