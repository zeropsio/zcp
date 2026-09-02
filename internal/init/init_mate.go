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

// mateEnsureInstalled converges the installed mate bundle toward the release zcp
// pins — or leaves it alone, when the desired version is already live or a
// dev build is being kept. Package-level so tests can stub the one step that
// may reach the network; production is mate.EnsureInstalled (bounded timeouts
// throughout).
var mateEnsureInstalled = mate.EnsureInstalled

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
//   - enabled: make Zerops Mate available — a bundle at the prefix, the
//     identity contract on disk, and a supervised unit (unchanged from
//     before the flag existed; see the enable-path documentation below).
//   - disabled: undo exactly what the enabled direction sets up — stop and
//     remove the unit, and drop the identity contract. mate.Prefix() (the
//     downloaded/dev-pushed bundle) is deliberately left alone: re-enabling
//     later must cost no network, only a `zcp init`.
//
// The step is BEST-EFFORT (see step.degraded): `zcp init` is a run.init
// command, so a container with no bundle, no reachable release/registry, or
// a stubborn unit must still start.
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

	fmt.Fprintf(os.Stderr, "    → removing unit zerops@%s\n", mate.UnitName)
	if err := commandRunner("sudo", "-E", "zsc", "unit", "remove", mate.UnitName); err != nil {
		return fmt.Errorf("zsc unit remove %s: %w", mate.UnitName, err)
	}

	if err := os.Remove(mate.EnvFilePath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", mate.EnvFilePath(), err)
	}
	return nil
}

// enableMate makes Zerops Mate available on this container: a bundle at
// mate.CurrentLink(), the identity contract on disk, and a supervised unit.
//
// The bundle itself is mateEnsureInstalled's job in full — same version already
// live (the common warm-restart case) costs no network, and a hand-pushed dev
// build is kept rather than silently replaced by the pinned release. See
// mate.EnsureInstalled for the pass in detail.
//
// When the bundle cannot be had, no unit is registered either — a unit whose
// ExecStart cannot resolve crash-loops at every boot and buries the real cause
// under a restart counter.
func enableMate(rt runtime.Info) error {
	// The mate server treats a non-empty project id as THE signal that it runs
	// inside a Zerops project. Without one there is nothing to bind to, and
	// starting anyway would leave a plain upstream server on the origin.
	if rt.ProjectID == "" {
		return errors.New("no projectId in the container environment — mate has no Zerops project to bind to")
	}

	result, err := mateEnsureInstalled(mate.EnsureOptions{})
	if err != nil {
		return fmt.Errorf("ensure mate bundle: %w", err)
	}
	logMateEnsureResult(result)

	bin := mate.BinPath()
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("bundle still absent after %s: %w", result.Action, err)
	}

	// Reported here rather than left to the journal: a bundle without
	// --base-path answers under mate.BasePath but emits root-absolute asset
	// URLs, which the code-server cookie gate then redirects — the page loads
	// and nothing works, with no error anywhere to point at.
	if !mate.SupportsBasePath(bin) {
		fmt.Fprintf(os.Stderr, "    ! the installed mate bundle does not advertise --base-path; mate will answer under %s/ but its assets will not resolve\n", mate.BasePath)
	}

	unitExisted := mateUnitFileExists()

	envChanged, err := writeMateEnv(rt)
	if err != nil {
		return err
	}
	if err := ensureMateUnit(); err != nil {
		return err
	}

	// A unit that was ALREADY running is serving whatever was on disk when it
	// started — and it starts at boot on its own, from `WantedBy=multi-user
	// .target`, without waiting for this command. Measured on z3-eval: the
	// unit entered `active` at 16:45:12, two seconds BEFORE install.sh had even
	// replaced the zcp binary, and `zcp init` ran later still. So a bundle this
	// step just changed, or an env contract it just rewrote, reaches the
	// running server only if something restarts it here — otherwise a moved pin
	// lands on disk and serves from the NEXT restart, which is not what
	// "a restart is also an upgrade" promises.
	//
	// Only when the unit pre-existed: `zsc unit create` starts a new one
	// itself, and restarting it again would just cost a second boot.
	if unitExisted && (bundleChanged(result) || envChanged) {
		fmt.Fprintf(os.Stderr, "    → restarting zerops@%s to pick up the change\n", mate.UnitName)
		if err := commandRunner("sudo", "systemctl", "restart", "zerops@"+mate.UnitName+".service"); err != nil {
			// Best-effort: the new bytes are on disk and the next restart
			// serves them. Naming it beats failing a container start.
			fmt.Fprintf(os.Stderr, "    ! restart zerops@%s failed (the change serves from the next restart): %v\n", mate.UnitName, err)
		}
	}
	return nil
}

// bundleChanged reports whether EnsureInstalled actually replaced the bytes
// the server runs. "none" changed nothing at all — not worth a restart.
func bundleChanged(result mate.Result) bool {
	return result.Action == mate.ActionInstalled || result.Action == mate.ActionUpdated
}

// logMateEnsureResult prints one line naming what mateEnsureInstalled did, so a
// container boot's log tells "nothing changed" from "fetched a new release"
// without having to infer it from the absence of a download line.
func logMateEnsureResult(result mate.Result) {
	switch result.Action {
	case mate.ActionNone:
		fmt.Fprintf(os.Stderr, "    (mate %s already installed, no network reached)\n", result.To)
	case mate.ActionInstalled:
		fmt.Fprintf(os.Stderr, "    (installed mate %s)\n", result.To)
	case mate.ActionUpdated:
		fmt.Fprintf(os.Stderr, "    (updated mate %s -> %s)\n", result.From, result.To)
	}
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
// Reports whether the file's CONTENT changed, so the caller can restart the
// unit that reads it: the supervisor merges this file at launch, so a service
// env an operator just edited (ZCP_MATE_ALLOWED_ORIGINS, say) reaches the running
// server no other way.
func writeMateEnv(rt runtime.Info) (bool, error) {
	path := mate.EnvFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	lines := mate.EnvLines(
		rt.ProjectID,
		mate.ResolveAPIHost(os.Getenv("ZCP_API_HOST")),
		os.Getenv(mate.SourceAllowedOrigins),
	)
	body := "# Written by `zcp init` on every container boot. Read by `zcp service start " +
		mate.UnitName + "`.\n" + strings.Join(lines, "\n") + "\n"

	previous, readErr := os.ReadFile(path)
	changed := readErr != nil || string(previous) != body

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return changed, nil
}

// ensureMateUnit registers the supervised unit when it is not there yet.
//
// `zsc unit` has only create and remove — no idempotent upsert — and a unit
// created this way survives a container restart, while `zcp init` runs on
// every boot. So the unit file's presence is the check.
func ensureMateUnit() error {
	if mateUnitFileExists() {
		return nil
	}
	fmt.Fprintf(os.Stderr, "    → registering unit zerops@%s\n", mate.UnitName)
	if err := commandRunner("sudo", "-E", "zsc", "unit", "create", mate.UnitName, mate.UnitCommand); err != nil {
		return fmt.Errorf("zsc unit create %s: %w", mate.UnitName, err)
	}
	return nil
}
