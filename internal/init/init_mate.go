package init

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/runtime"
)

// mateUnitFilePath is where `zsc unit create` lands the unit. Package-level so
// tests can point the existence check at a temp path instead of /usr/lib.
var mateUnitFilePath = mate.UnitFilePath

// mateUnitFileExists reports whether a previous `zcp init` registered the
// unit — the same check ensureMateUnit uses to tell "already registered" from
// "first boot". init.go's step-registration gate reads this so a container
// that never had mate (flag off, no unit ever created) prints no step line at
// all, while a container that DOES carry a leftover unit still gets a chance
// to reconcile it away even with the flag off.
func mateUnitFileExists() bool {
	_, err := os.Stat(mateUnitFilePath)
	return err == nil
}

// reconcileMate converges this container toward the state rt.MateEnabled asks
// for — a two-directional step, unlike every other init step:
//
//   - enabled: prepare Zerops Mate — the identity contract on disk, a
//     supervised unit whose start job installs the bundle, the keep job's
//     timer, and a start of the unit (enableMate).
//   - disabled: undo exactly what the enabled direction sets up — stop and
//     remove the unit, and drop the identity contract. mate.Prefix() (the
//     downloaded/dev-pushed bundle) is deliberately left alone: re-enabling
//     later must cost no network, only a `zcp init`.
//
// The step is BEST-EFFORT (see step.degraded): `zcp init` is a run.init
// command, so a container with a stubborn unit must still start.
func reconcileMate(_ string, rt runtime.Info) error {
	if !rt.MateEnabled {
		return disableMate()
	}
	return enableMate(rt)
}

// disableMate undoes an enabled container's install steps for a container whose
// flag has since been turned off:
//
//  1. If the unit file is absent, there is nothing to reconcile — return nil
//     having done nothing at all (this is the common case: most containers
//     never had mate enabled in the first place).
//  2. Stop the unit. Best-effort: the unit may already be stopped (a prior
//     crash, a prior partial disable), and refusing to continue over that
//     would strand the unit registered forever.
//  3. Remove the unit via `zsc unit remove` — the one real failure this
//     function returns, because a removal that silently fails leaves a
//     server the operator believes is gone still running.
//  4. Remove the identity contract file. A missing file (the common case —
//     the flag might have been off since before the file was ever written)
//     is not an error.
//
// mate.Prefix() is never touched: the bundle stays on disk by design.
func disableMate() error {
	if !mateUnitFileExists() {
		return nil
	}

	unit := "zerops@" + mate.UnitName + ".service"
	fmt.Fprintf(os.Stderr, "    → stopping %s\n", unit)
	if err := commandRunner("sudo", "systemctl", "stop", unit); err != nil {
		fmt.Fprintf(os.Stderr, "    ! systemctl stop %s failed (continuing — the unit may already be stopped): %v\n", unit, err)
	}

	if err := removeMateFuse(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "    → removing unit zerops@%s\n", mate.UnitName)
	if err := commandRunner("sudo", "-E", "zsc", "unit", "remove", mate.UnitName); err != nil {
		return fmt.Errorf("zsc unit remove %s: %w", mate.UnitName, err)
	}

	if err := os.Remove(mate.EnvFilePath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", mate.EnvFilePath(), err)
	}
	return nil
}

// enableMate is the Mate's prepare job (docs/spec-mate.md §2.1): it writes the
// server's configuration, creates the unit and the keep job's timer, and
// starts the unit. It installs nothing and repairs nothing: the unit's start
// job installs and starts the version to run, and judges it.
//
// The start is requested even when this step fails, provided a unit is
// registered: starting an active unit is a no-op.
func enableMate(rt runtime.Info) (retErr error) {
	defer func() {
		if !mateUnitFileExists() {
			return
		}
		unit := "zerops@" + mate.UnitName + ".service"
		fmt.Fprintf(os.Stderr, "    → ensuring %s is running\n", unit)
		if err := commandRunner("sudo", "systemctl", "start", "--no-block", unit); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("start %s: %w", unit, err))
		}
	}()
	// This init cannot write a valid identity contract without a project ID.
	// The deferred start still starts a previously registered unit.
	if rt.ProjectID == "" {
		return errors.New("no projectId in the container environment — mate has no Zerops project to bind to")
	}
	if err := writeMateEnv(rt); err != nil {
		return err
	}
	if err := ensureMateUnit(); err != nil {
		return fmt.Errorf("register Mate unit: %w", err)
	}
	reloadMateUnitIfChanged()
	return nil
}

// writeMateEnv records the environment the mate server needs, for the supervisor
// to merge at launch.
//
// `zcp init` is a run.init command and therefore runs with the container's
// full environment; a unit registered through `zsc unit create` is not
// guaranteed to inherit it. Writing the values here — every boot, so a new key
// never needs the unit re-created — is what makes them deterministic. Only
// non-secret identifiers are written; a token never goes in this file.
//
// The supervisor merges this file at launch, so a changed value reaches the
// server at its next start.
//
// ZCP_MATE_ALLOWED_ORIGINS goes through mate.ValidateAllowedOrigins first: a
// malformed value (a bare host, a path, a wildcard — see spec-mate.md §3.13)
// can never match a real Origin header, so writing it would silently
// configure an allowlist entry that does nothing. Best-effort like the rest
// of this step: one diagnostic line names the bad entry and the key is left
// unwritten, falling back to the server's own default rather than failing the
// container start.
func writeMateEnv(rt runtime.Info) error {
	path := mate.EnvFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}

	allowedOrigins := os.Getenv(mate.SourceAllowedOrigins)
	if bad, ok := mate.ValidateAllowedOrigins(allowedOrigins); !ok {
		fmt.Fprintf(os.Stderr, "    ! ignoring invalid %s entry %q (want a comma-separated list of https://<host>[:port] origins); mate keeps its default allowlist\n", mate.SourceAllowedOrigins, bad)
		allowedOrigins = ""
	}

	lines := mate.EnvLines(
		rt.ProjectID,
		mate.ResolveAPIHost(os.Getenv("ZCP_API_HOST")),
		allowedOrigins,
	)
	body := "# Written by `zcp init` on every container boot. Read by `zcp service start " +
		mate.UnitName + "`.\n" + strings.Join(lines, "\n") + "\n"

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// reloadMateUnitIfChanged reloads systemd's units when systemd says the mate
// unit's file changed on disk since it loaded it (NeedDaemonReload) — what it
// warns "run 'systemctl daemon-reload'" about. Until then a restart, this
// step's or a later one, may run the old definition. Asked of systemd, not
// guessed from the file: `zsc unit create` writes the file, and whatever
// rewrote it, systemd's view is the one a restart uses. Best-effort like the
// restart: a reload that fails is named and the step goes on.
func reloadMateUnitIfChanged() {
	unit := "zerops@" + mate.UnitName + ".service"
	out, err := commandOutput("systemctl", "show", "--property=NeedDaemonReload", "--value", unit)
	if err != nil || strings.TrimSpace(string(out)) != "yes" {
		return
	}
	fmt.Fprintf(os.Stderr, "    → the unit file of %s changed on disk; reloading systemd units\n", unit)
	if err := commandRunner("sudo", "systemctl", "daemon-reload"); err != nil {
		fmt.Fprintf(os.Stderr, "    ! systemctl daemon-reload failed (a restart of %s may run its old definition): %v\n", unit, err)
	}
}

// ensureMateUnit registers the supervised unit when absent and disables its
// boot start on every enabled init.
//
// `zsc unit` has only create and remove — no idempotent upsert — and a unit
// created this way survives a container restart, while `zcp init` runs on
// every boot. So the unit file's presence is the check.
func ensureMateUnit() error {
	if !mateUnitFileExists() {
		fmt.Fprintf(os.Stderr, "    → registering unit zerops@%s\n", mate.UnitName)
		if err := commandRunner("sudo", "-E", "zsc", "unit", "create", "--enable=false", "--start=false", mate.UnitName, mate.UnitCommand); err != nil {
			return fmt.Errorf("zsc unit create %s: %w", mate.UnitName, err)
		}
	}
	unit := "zerops@" + mate.UnitName + ".service"
	if err := commandRunner("sudo", "systemctl", "disable", unit); err != nil {
		// A failed disable must not stop the rest of the step.
		fmt.Fprintf(os.Stderr, "    ! systemctl disable %s failed (boot start may stay enabled): %v\n", unit, err)
	}
	ensureMateFuse()
	return nil
}
