package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/zeropsio/zcp/internal/z3"
)

// restartTimeout bounds the unit restart. `systemctl restart` blocks until
// the unit reports started, and z3's own startup is a node process — generous
// enough not to fail a healthy start, short enough that a wedged unit does
// not hang the command indefinitely.
const restartTimeout = 60 * time.Second

// z3UnitFilePath is where `zsc unit create` lands the z3 unit — same check
// internal/init uses to tell "already registered" from "first boot".
// Package-level so a test can point it at a temp path instead of /usr/lib.
var z3UnitFilePath = z3.UnitFilePath

// z3RestartUnit restarts the supervised z3 unit so a freshly activated
// version is actually picked up without a container restart. Package-level
// so tests can stub the shell-out.
var z3RestartUnit = defaultZ3RestartUnit

func defaultZ3RestartUnit(unit string) error {
	ctx, cancel := context.WithTimeout(context.Background(), restartTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sudo", "systemctl", "restart", unit)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runZ3Cmd implements `zcp z3 update [--force]`. args is everything after
// "z3". It runs the same EnsureInstalled pass the init step runs, prints one
// line naming what happened, and — only when the supervised unit is actually
// registered on this container — restarts it so the update takes effect
// without waiting for the next container restart.
func runZ3Cmd(args []string) int {
	if len(args) == 0 || args[0] != "update" {
		log.Print("usage: zcp z3 update [--force]")
		return 1
	}

	force := false
	for _, arg := range args[1:] {
		if arg == "--force" {
			force = true
		}
	}

	result, err := z3.EnsureInstalled(z3.EnsureOptions{Force: force})
	if err != nil {
		log.Printf("z3 update: %v", err)
		return 1
	}
	logZ3CmdResult(result)

	if _, err := os.Stat(z3UnitFilePath); err != nil {
		// No unit registered on this container — nothing to restart. A
		// following `zcp init` (or the next boot) creates it against
		// whatever EnsureInstalled just activated.
		return 0
	}

	unit := "zerops@" + z3.UnitName + ".service"
	fmt.Fprintf(os.Stderr, "restarting %s...\n", unit)
	if err := z3RestartUnit(unit); err != nil {
		log.Printf("systemctl restart %s: %v", unit, err)
		return 1
	}
	return 0
}

// logZ3CmdResult prints one line naming what EnsureInstalled did, mirroring
// the line the init step logs (internal/init/init_z3.go's
// logZ3EnsureResult) — the two are the same seam, run from two different
// callers.
func logZ3CmdResult(result z3.Result) {
	switch result.Action {
	case z3.ActionNone:
		fmt.Fprintf(os.Stderr, "z3: already at %s, nothing to do\n", result.To)
	case z3.ActionMigrated:
		fmt.Fprintf(os.Stderr, "z3: migrated legacy install to the versioned layout at %s\n", result.To)
	case z3.ActionInstalled:
		fmt.Fprintf(os.Stderr, "z3: installed %s\n", result.To)
	case z3.ActionUpdated:
		fmt.Fprintf(os.Stderr, "z3: updated %s -> %s\n", result.From, result.To)
	}
}
