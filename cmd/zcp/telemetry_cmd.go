package main

import (
	"fmt"
	"os"
	"time"

	"github.com/zeropsio/zcp/internal/telemetry"
)

// runTelemetryCmd implements `zcp telemetry status|enable|disable|id` (spec
// §3.5). cfg is the SAME Config runCLI already resolved for this process
// (spec §3.1 "resolved ONCE ... never re-read env") — status reads it
// directly instead of re-resolving (a second Resolve() call in one process
// would risk a second pre-disclosure stamp attempt). Human output goes to
// stdout — the `zcp telemetry` subcommand is exempt from the JSON-only rule,
// same as every other CLI subcommand.
func runTelemetryCmd(args []string, cfg telemetry.Config) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: zcp telemetry {status|enable|disable|id}")
		return 1
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "telemetry: resolve home directory: %v\n", err)
		return 1
	}
	internalChannel := telemetry.IsInternalChannel(cfg.Channel)

	switch args[0] {
	case "status":
		state := "disabled"
		if cfg.Enabled {
			state = "enabled"
		}
		fmt.Fprintf(os.Stdout, "Telemetry: %s (channel: %s)\n", state, cfg.Channel)
		fmt.Fprintf(os.Stdout, "Reason: %s\n", cfg.Reason)
		return 0

	case "enable":
		if err := telemetry.Enable(home, internalChannel, time.Now()); err != nil {
			fmt.Fprintf(os.Stderr, "telemetry enable: %v\n", err)
			return 1
		}
		telemetry.PrintDisclosureNotice(os.Stdout)
		// v1 is env-gated default-off: `enable` records consent (clears the
		// disabled flag, stamps disclosure) but does NOT itself turn telemetry
		// on — ZCP_TELEMETRY=1 is still required to send events. Say so
		// truthfully rather than claiming "enabled".
		fmt.Fprintln(os.Stdout, "Consent recorded. Telemetry is off by default — set ZCP_TELEMETRY=1 to send events.")
		return 0

	case "disable":
		if err := telemetry.Disable(home, internalChannel); err != nil {
			fmt.Fprintf(os.Stderr, "telemetry disable: %v\n", err)
			return 1
		}
		fmt.Fprintln(os.Stdout, "Telemetry disabled.")
		return 0

	case "id":
		id, err := telemetry.InstallIDOf(home, internalChannel)
		if err != nil {
			fmt.Fprintf(os.Stderr, "telemetry id: %v\n", err)
			return 1
		}
		fmt.Fprintln(os.Stdout, id)
		return 0

	default:
		fmt.Fprintf(os.Stderr, "unknown telemetry subcommand: %s\n", args[0])
		fmt.Fprintln(os.Stderr, "Usage: zcp telemetry {status|enable|disable|id}")
		return 1
	}
}
