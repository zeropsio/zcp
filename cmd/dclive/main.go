// Command dclive is the live-lane config generator for the Data Console
// conformance suite. `dclive gen-config --out <path>` runs ON the zcp
// container (or any host with resolvable REST credentials), discovers the
// adopted project's managed services through the SAME adapter path
// production uses (zcpadapter.ManagedServices), fetches each service's own
// env the same way the adapter's ConnectionInfo does (ops.FetchServiceEnv),
// and writes a conformance-shaped DC_LIVE_CONFIG JSON — the input
// scripts/dc-live-remote.sh ships alongside the e2e-tagged conformance test
// binary (docs/spec-dataconsole-testing.md §6). Never prints credentials;
// the output file is 0600.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider/conformance"
	"github.com/zeropsio/zcp/internal/dataconsole/zcpadapter"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "gen-config":
		runGenConfig(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "dclive: unknown subcommand %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: dclive gen-config --out <path>")
}

func runGenConfig(args []string) {
	fs := flag.NewFlagSet("gen-config", flag.ExitOnError)
	out := fs.String("out", "", "path to write the DC_LIVE_CONFIG JSON (required)")
	_ = fs.Parse(args)
	if *out == "" {
		fmt.Fprintln(os.Stderr, "dclive gen-config: --out is required")
		os.Exit(2)
	}

	cfg, n, err := genConfig(context.Background())
	must("gen-config", err)
	must("write config", writeLiveConfig(*out, cfg))
	fmt.Printf("wrote %s: %d services\n", *out, n)
}

// genConfig discovers the adopted project's managed services and their own
// env scalars, and maps each into a conformance.ServiceEntry via
// mapServiceEnv. A service whose type classifies to no supported console
// family is skipped silently (logged to stderr, not fatal) — every other
// managed service present lands in the returned config.
func genConfig(ctx context.Context) (*conformance.LiveConfig, int, error) {
	creds, err := auth.ResolveCredentials()
	if err != nil {
		return nil, 0, fmt.Errorf("resolve credentials: %w", err)
	}
	client, err := platform.NewZeropsClient(creds.Token, creds.APIHost)
	if err != nil {
		return nil, 0, fmt.Errorf("build client: %w", err)
	}
	info, err := auth.Resolve(ctx, client)
	if err != nil {
		return nil, 0, fmt.Errorf("resolve auth: %w", err)
	}
	ad := zcpadapter.New(client, info)
	svcs, err := ad.ManagedServices(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list managed services: %w", err)
	}

	cfg := &conformance.LiveConfig{}
	for _, s := range svcs {
		envs, err := ops.FetchServiceEnv(ctx, client, s.ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] env: %v\n", s.Hostname, err)
			continue
		}
		scalars := make(map[string]string, len(envs))
		for _, e := range envs {
			scalars[e.Key] = e.Content
		}
		entry, ok := mapServiceEnv(s.Hostname, s.Type, scalars)
		if !ok {
			continue
		}
		// Per-entry validation BEFORE the entry enters the config: one
		// degraded service (mid-provisioning, env-key drift) must skip with
		// a named stderr reason — not poison the whole file so that the
		// harness's LoadLiveConfig rejects it and every healthy engine's
		// release proof is blocked at startup (code-review finding 7). The
		// per-service required/skip policy then applies where it belongs:
		// in the harness, per manifest entry.
		if _, err := entry.Descriptor(); err != nil {
			fmt.Fprintf(os.Stderr, "[%s] incomplete descriptor, skipping: %v\n", s.Hostname, err)
			continue
		}
		cfg.Services = append(cfg.Services, entry)
	}
	return cfg, len(cfg.Services), nil
}

// writeLiveConfig serializes cfg to path at mode 0600 — the file carries
// live service credentials and must never land world/group-readable.
func writeLiveConfig(path string, cfg *conformance.LiveConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func must(what string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", what, err)
		os.Exit(1)
	}
}
