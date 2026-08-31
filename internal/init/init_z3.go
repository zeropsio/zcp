package init

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/z3"
)

// z3EnsureInstalled converges the installed z3 bundle toward the release zcp
// pins — or leaves it alone, when the desired version is already live or a
// dev build is being kept. Package-level so tests can stub the one step that
// may reach the network; production is z3.EnsureInstalled (bounded timeouts
// throughout).
var z3EnsureInstalled = z3.EnsureInstalled

// z3UnitFilePath is where `zsc unit create` lands the unit. Package-level so
// tests can point the existence check at a temp path instead of /usr/lib.
var z3UnitFilePath = z3.UnitFilePath

// z3UnitFileExists reports whether a previous `zcp init` registered the
// unit — the same check ensureZ3Unit uses to tell "already registered" from
// "first boot". init.go's step-registration gate reads this so a container
// that never had z3 (flag off, no unit ever created) prints no step line at
// all, while a container that DOES carry a leftover unit still gets a chance
// to reconcile it away even with the flag off.
func z3UnitFileExists() bool {
	_, err := os.Stat(z3UnitFilePath)
	return err == nil
}

// reconcileZ3 converges this container toward the state rt.Z3Enabled asks
// for — a two-directional step, unlike every other init step:
//
//   - enabled: make Zerops Code available — a bundle at the prefix, the
//     identity contract on disk, and a supervised unit (unchanged from
//     before the flag existed; see the enable-path documentation below).
//   - disabled: undo exactly what the enabled direction sets up — stop and
//     remove the unit, and drop the identity contract. z3.Prefix() (the
//     downloaded/dev-pushed bundle) is deliberately left alone: re-enabling
//     later must cost no network, only a `zcp init`.
//
// The step is BEST-EFFORT (see step.degraded): `zcp init` is a run.init
// command, so a container with no bundle, no reachable release/registry, or
// a stubborn unit must still start.
func reconcileZ3(_ string, rt runtime.Info) error {
	if !rt.Z3Enabled {
		return disableZ3()
	}
	return enableZ3(rt)
}

// disableZ3 undoes an enabled container's install steps for a container whose
// flag has since been turned off:
//
//  1. If the unit file is absent, there is nothing to reconcile — return nil
//     having done nothing at all (this is the common case: most containers
//     never had z3 enabled in the first place).
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
// z3.Prefix() is never touched: the bundle stays on disk by design.
func disableZ3() error {
	if !z3UnitFileExists() {
		return nil
	}

	unit := "zerops@" + z3.UnitName + ".service"
	fmt.Fprintf(os.Stderr, "    → stopping %s\n", unit)
	if err := commandRunner("sudo", "systemctl", "stop", unit); err != nil {
		fmt.Fprintf(os.Stderr, "    ! systemctl stop %s failed (continuing — the unit may already be stopped): %v\n", unit, err)
	}

	fmt.Fprintf(os.Stderr, "    → removing unit zerops@%s\n", z3.UnitName)
	if err := commandRunner("sudo", "-E", "zsc", "unit", "remove", z3.UnitName); err != nil {
		return fmt.Errorf("zsc unit remove %s: %w", z3.UnitName, err)
	}

	if err := os.Remove(z3.EnvFilePath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", z3.EnvFilePath(), err)
	}
	return nil
}

// enableZ3 makes Zerops Code available on this container: a bundle at
// z3.CurrentLink(), the identity contract on disk, and a supervised unit.
//
// The bundle itself is z3EnsureInstalled's job in full — same version already
// live (the common warm-restart case) costs no network, a legacy flat install
// is migrated in place, and a hand-pushed dev build is kept rather than
// silently replaced by the pinned release. See z3.EnsureInstalled for the
// pass in detail.
//
// When the bundle cannot be had, no unit is registered either — a unit whose
// ExecStart cannot resolve crash-loops at every boot and buries the real cause
// under a restart counter.
func enableZ3(rt runtime.Info) error {
	// The z3 server treats a non-empty project id as THE signal that it runs
	// inside a Zerops project. Without one there is nothing to bind to, and
	// starting anyway would leave a plain upstream server on the origin.
	if rt.ProjectID == "" {
		return errors.New("no projectId in the container environment — z3 has no Zerops project to bind to")
	}

	result, err := z3EnsureInstalled(z3.EnsureOptions{})
	if err != nil {
		return fmt.Errorf("ensure z3 bundle: %w", err)
	}
	logZ3EnsureResult(result)

	bin := z3.BinPath()
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("bundle still absent after %s: %w", result.Action, err)
	}

	// Reported here rather than left to the journal: a bundle without
	// --base-path answers under z3.BasePath but emits root-absolute asset
	// URLs, which the code-server cookie gate then redirects — the page loads
	// and nothing works, with no error anywhere to point at.
	if !z3.SupportsBasePath(bin) {
		fmt.Fprintf(os.Stderr, "    ! the installed z3 bundle does not advertise --base-path; z3 will answer under %s/ but its assets will not resolve\n", z3.BasePath)
	}

	if err := writeZ3Env(rt); err != nil {
		return err
	}
	return ensureZ3Unit()
}

// logZ3EnsureResult prints one line naming what z3EnsureInstalled did, so a
// container boot's log tells "nothing changed" from "fetched a new release"
// without having to infer it from the absence of a download line.
func logZ3EnsureResult(result z3.Result) {
	switch result.Action {
	case z3.ActionNone:
		fmt.Fprintf(os.Stderr, "    (z3 %s already installed, no network reached)\n", result.To)
	case z3.ActionMigrated:
		fmt.Fprintf(os.Stderr, "    (migrated z3 %s to the versioned layout)\n", result.To)
	case z3.ActionInstalled:
		fmt.Fprintf(os.Stderr, "    (installed z3 %s)\n", result.To)
	case z3.ActionUpdated:
		fmt.Fprintf(os.Stderr, "    (updated z3 %s -> %s)\n", result.From, result.To)
	}
}

// writeZ3Env records the environment the z3 server needs, for the supervisor
// to merge at launch.
//
// `zcp init` is a run.init command and therefore runs with the container's
// full environment; a unit registered through `zsc unit create` is not
// guaranteed to inherit it. Writing the values here — every boot, so a new key
// never needs the unit re-created — is what makes them deterministic. Only
// non-secret identifiers are written; a token never goes in this file.
func writeZ3Env(rt runtime.Info) error {
	path := z3.EnvFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	lines := z3.EnvLines(
		rt.ProjectID,
		z3.ResolveAPIHost(os.Getenv("ZCP_API_HOST")),
		os.Getenv(z3.SourceAllowedOrigins),
	)
	body := "# Written by `zcp init` on every container boot. Read by `zcp service start " +
		z3.UnitName + "`.\n" + strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// ensureZ3Unit registers the supervised unit when it is not there yet.
//
// `zsc unit` has only create and remove — no idempotent upsert — and a unit
// created this way survives a container restart, while `zcp init` runs on
// every boot. So the unit file's presence is the check.
func ensureZ3Unit() error {
	if z3UnitFileExists() {
		return nil
	}
	fmt.Fprintf(os.Stderr, "    → registering unit zerops@%s\n", z3.UnitName)
	if err := commandRunner("sudo", "-E", "zsc", "unit", "create", z3.UnitName, z3.UnitCommand); err != nil {
		return fmt.Errorf("zsc unit create %s: %w", z3.UnitName, err)
	}
	return nil
}
