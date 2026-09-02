package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/zeropsio/zcp/internal/mate"
)

// restartTimeout bounds the unit restart. `systemctl restart` blocks until
// the unit reports started, and mate's own startup is a node process — generous
// enough not to fail a healthy start, short enough that a wedged unit does
// not hang the command indefinitely.
const restartTimeout = 60 * time.Second

// mateUnitFilePath is where `zsc unit create` lands the mate unit — same check
// internal/init uses to tell "already registered" from "first boot".
// Package-level so a test can point it at a temp path instead of /usr/lib.
var mateUnitFilePath = mate.UnitFilePath

// mateRestartUnit restarts the supervised mate unit so a freshly activated
// version is actually picked up without a container restart. Package-level
// so tests can stub the shell-out.
var mateRestartUnit = defaultMateRestartUnit

func defaultMateRestartUnit(unit string) error {
	ctx, cancel := context.WithTimeout(context.Background(), restartTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sudo", "systemctl", "restart", unit)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runMateCmd implements `zcp mate update [--force]`. args is everything after
// "mate". It runs the same EnsureInstalled pass the init step runs, prints one
// line naming what happened, and — only when the supervised unit is actually
// registered on this container — restarts it so the update takes effect
// without waiting for the next container restart.
func runMateCmd(args []string) int {
	if len(args) == 0 || args[0] != "update" {
		log.Print("usage: zcp mate update [--force]")
		return 1
	}

	force := false
	for _, arg := range args[1:] {
		if arg == "--force" {
			force = true
		}
	}

	result, err := mate.EnsureInstalled(mate.EnsureOptions{Force: force})
	if err != nil {
		log.Printf("mate update: %v", err)
		return 1
	}
	logMateCmdResult(result)

	if _, err := os.Stat(mateUnitFilePath); err != nil {
		// No unit registered on this container — nothing to restart. A
		// following `zcp init` (or the next boot) creates it against
		// whatever EnsureInstalled just activated.
		return 0
	}

	unit := "zerops@" + mate.UnitName + ".service"
	fmt.Fprintf(os.Stderr, "restarting %s...\n", unit)
	if err := mateRestartUnit(unit); err != nil {
		log.Printf("systemctl restart %s: %v", unit, err)
		return 1
	}
	return 0
}

// logMateCmdResult prints one line naming what EnsureInstalled did, mirroring
// the line the init step logs (internal/init/init_mate.go's
// logMateEnsureResult) — the two are the same seam, run from two different
// callers.
func logMateCmdResult(result mate.Result) {
	switch result.Action {
	case mate.ActionNone:
		fmt.Fprintf(os.Stderr, "mate: already at %s, nothing to do\n", result.To)
	case mate.ActionInstalled:
		fmt.Fprintf(os.Stderr, "mate: installed %s\n", result.To)
	case mate.ActionUpdated:
		fmt.Fprintf(os.Stderr, "mate: updated %s -> %s\n", result.From, result.To)
	}
}
